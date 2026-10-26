package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"tailfile.local/agent/guitoken"
)

//go:embed all:frontend/dist
var embedded embed.FS

func main() {
	agentURL := flag.String("agent", "http://127.0.0.1:8645", "local tailfile agent")
	tokenFile := flag.String("token-file", "", "path to the agent GUI token (default: the state dir gui-token file)")
	stateDir := flag.String("state", "", "agent state dir used to find gui-token")
	flag.Parse()

	token, err := guitoken.Load(*tokenFile, *stateDir)
	if err != nil {
		log.Printf("tailfile: %v", err)
		token = ""
	}

	dist, err := fs.Sub(embedded, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}

	app := application.New(application.Options{
		Name:        "Tailfile",
		Description: "Personal distributed file manager",
		Assets: application.AssetOptions{
			Handler:        injectToken(application.BundledAssetFileServer(dist), token),
			DisableLogging: true,
		},
		Server: application.ServerOptions{
			Host: "127.0.0.1",
			Port: serverPort,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Tailfile",
		Width:            1200,
		Height:           780,
		MinWidth:         860,
		MinHeight:        520,
		EnableFileDrop:   true,
		BackgroundColour: application.NewRGB(236, 236, 236),
		URL:              "/",
	})
	// Desktop builds receive OS paths here. Server mode compiles the same
	// call; a browser never raises WindowFilesDropped, so the page also
	// accepts a typed path.
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		files := event.Context().DroppedFiles()
		go importPaths(*agentURL, token, files)
	})

	logMode(*agentURL)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func importPaths(agent, token string, paths []string) {
	if len(paths) == 0 {
		return
	}
	body, err := json.Marshal(map[string]any{"paths": paths})
	if err != nil {
		log.Printf("import: %v", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(agent, "/")+"/v1/local/import", bytes.NewReader(body))
	if err != nil {
		log.Printf("import: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("import: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("import: %s %s", resp.Status, bytes.TrimSpace(msg))
	}
}

// injectToken puts the localhost bearer into index.html as window.TAILFILE_TOKEN.
// The page is loopback-only and marked no-store. The token value is not logged.
func injectToken(inner http.Handler, token string) http.Handler {
	script := []byte("<script>window.TAILFILE_TOKEN=" + string(mustJSON(token)) + ";</script>")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			inner.ServeHTTP(w, r)
			return
		}
		rec := &bodyCapture{header: make(http.Header), code: http.StatusOK}
		inner.ServeHTTP(rec, r)
		body := bytes.Replace(rec.buf.Bytes(), []byte("</head>"), append(script, []byte("</head>")...), 1)
		for k, vals := range rec.header {
			if strings.EqualFold(k, "Content-Length") {
				continue
			}
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(rec.code)
		_, _ = w.Write(body)
	})
}

func mustJSON(v string) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(`""`)
	}
	return raw
}

type bodyCapture struct {
	header http.Header
	code   int
	buf    bytes.Buffer
}

func (b *bodyCapture) Header() http.Header { return b.header }

func (b *bodyCapture) WriteHeader(code int) { b.code = code }

func (b *bodyCapture) Write(p []byte) (int, error) { return b.buf.Write(p) }
