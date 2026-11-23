package agent

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type demoCluster struct {
	root  string
	lb    *Loopback
	token string
}

func (d *demoCluster) Close() {
	if d == nil {
		return
	}
	if d.lb != nil {
		if d.lb.Orch != nil {
			_ = d.lb.Orch.Close()
		}
		for _, n := range d.lb.nodes {
			if n.srv != nil {
				_ = n.srv.Close()
			}
		}
		d.lb.Close()
	}
	if d.root != "" {
		_ = os.RemoveAll(d.root)
	}
}

func startDemoCluster(cfg Config, writeTokenFile bool) (*demoCluster, error) {
	root, err := os.MkdirTemp("", "tailfile-demo-")
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	log.Printf("tailfile demo fixtures in %s", root)
	specs := make([]LoopSpec, 0, 5)
	for _, name := range []string{"alpha", "beta", "gamma", "pixel", "phone"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		spec := LoopSpec{ID: name, Read: []string{dir}, OS: demoOS(name)}
		if name == "alpha" || name == "beta" {
			spec.Write = []string{dir}
		}
		specs = append(specs, spec)
	}
	if err := seedDemo(root); err != nil {
		return nil, err
	}
	owner := cfg.Owner
	if owner == "" {
		owner = "ada@example.com"
	}
	lb, err := StartLoopback(owner, cfg.ChunkSize, specs)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cleanup {
			lb.Close()
		}
	}()
	lb.Orch.noteNet(cfg.Net, "demo")
	cacheDir := filepath.Join(root, "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, err
	}
	if err := lb.Orch.attachState(cfg.StateDir); err != nil {
		return nil, err
	}
	lb.Orch.initCacheDir(cacheDir)
	var token string
	if writeTokenFile {
		token, err = writeGUIToken(cfg.StateDir)
	} else {
		token, err = randomHexToken()
	}
	if err != nil {
		return nil, err
	}
	lb.Orch.guiToken = token
	lb.Token = token
	cleanup = false
	return &demoCluster{root: root, lb: lb, token: token}, nil
}

func runDemo(ctx context.Context, cfg Config) error {
	if strings.TrimSpace(cfg.LocalAddr) == "" {
		cfg.LocalAddr = "127.0.0.1:8645"
	}
	if strings.TrimSpace(cfg.StateDir) == "" {
		tempState, err := os.MkdirTemp("", "tailfile-demo-state-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tempState)
		cfg.StateDir = tempState
	}
	ln, err := listenLoopback(cfg.LocalAddr)
	if err != nil {
		return err
	}
	lock, err := acquireStateLock(cfg.StateDir)
	if err != nil {
		_ = ln.Close()
		return err
	}
	defer lock.Close()

	dc, err := startDemoCluster(cfg, true)
	if err != nil {
		_ = ln.Close()
		return err
	}
	defer dc.Close()

	log.Printf("tailfile demo orchestrator on http://%s (owner %s)", ln.Addr().String(), dc.lb.Owner)
	log.Printf("drag-out cache: %s", dc.lb.Orch.currentCacheDir())
	log.Printf("nodes: alpha and beta accept writes; gamma, pixel, and phone are read-only")
	return serveListener(ctx, ln, dc.lb.Handler())
}

// demoOS labels the fixture nodes so the GUI can show every host glyph.
// It does not change what alpha and beta can write.
func demoOS(name string) string {
	switch name {
	case "alpha":
		return "macos"
	case "beta":
		return "windows"
	case "gamma":
		return "linux"
	case "pixel":
		return "android"
	case "phone":
		return "ios"
	default:
		return "linux"
	}
}

func seedDemo(root string) error {
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	gamma := filepath.Join(root, "gamma")
	if err := os.MkdirAll(filepath.Join(alpha, "notes"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(alpha, "photos"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		filepath.Join(alpha, "hello.txt"):        "Hello from alpha.\nCopy this file onto beta to watch a direct transfer.\n",
		filepath.Join(alpha, "notes", "week.md"): "# Week notes\n\n- Tailnet nodes are the roots of the tree.\n- Paste writes only where a node allows it.\n",
		filepath.Join(beta, "inbox.txt"):         "Beta's inbox is empty aside from this note.\n",
		filepath.Join(gamma, "readme.txt"):       "Gamma is read-only. Paste here fails on purpose.\n",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := writeDemoPNG(filepath.Join(alpha, "photos", "swatch.png")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(alpha, "notes", "sample.pdf"), simplePDF("Tailfile PDF preview"), 0o644); err != nil {
		return err
	}
	// A few megabytes so cancel and resume are visible.
	blob := filepath.Join(alpha, "notes", "payload.bin")
	f, err := os.Create(blob)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	for i := range buf {
		buf[i] = byte(i)
	}
	for n := 0; n < 32; n++ { // 2 MiB
		if _, err := f.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

func writeDemoPNG(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(40 + x/2),
				G: uint8(80 + y/2),
				B: uint8(160 - x/4),
				A: 255,
			})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func simplePDF(line string) []byte {
	line = strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(line)
	content := fmt.Sprintf("BT /F1 18 Tf 36 90 Td (%s) Tj ET", line)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 160] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, o := range objs {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", len(objs)+1)
	b.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return []byte(b.String())
}
