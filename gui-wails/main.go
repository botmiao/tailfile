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
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var embedded embed.FS

func main() {
	agentURL := flag.String("agent", "http://127.0.0.1:8645", "local tailfile agent")
	flag.Parse()

	dist, err := fs.Sub(embedded, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}

	app := application.New(application.Options{
		Name:        "Tailfile",
		Description: "Personal distributed file manager",
		Assets: application.AssetOptions{
			Handler:        application.BundledAssetFileServer(dist),
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
		go importPaths(*agentURL, files)
	})

	logMode(*agentURL)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func importPaths(agent string, paths []string) {
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
