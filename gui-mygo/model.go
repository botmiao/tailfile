// Tailfile's MyGo prototype: a native file manager over the local agent.
package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"tailfile.local/agent"
)

type clip struct {
	Node  string
	Path  string
	Name  string
	IsDir bool
}

type app struct {
	client *agent.Client
	addr   string

	mu       sync.Mutex
	win      *mygo.Window
	nodes    []agent.NodeInfo
	subdirs  map[string][]string
	loading  map[string]bool
	entries  []agent.Entry
	folderN  string
	folderP  string
	listTok  string
	listBusy bool

	tree    ui.OutlineState[string]
	list    ui.ListState
	treeSel int
	listSel int

	clip       *clip
	mode       string
	status     string
	err        string
	newName    string
	renameTo   string
	importPath string

	previewKind string
	previewText string
	previewPath string
	previewImg  *ui.Bitmap
	pdfView     *mygo.WebView
	pdfURL      string
	pdfErr      string
	dragPath    string
	dragNote    string

	transfers []agent.Transfer
	imports   []agent.Job
	seenJob   map[string]string
	alive     bool
	stopPoll  chan struct{}

	sideW float32
	listH float32
}

func newApp(addr string) *app {
	if addr == "" {
		addr = "http://127.0.0.1:8645"
	}
	a := &app{
		client:   agent.NewClient(addr),
		addr:     addr,
		subdirs:  map[string][]string{},
		loading:  map[string]bool{},
		seenJob:  map[string]string{},
		treeSel:  -1,
		listSel:  -1,
		mode:     "push",
		status:   "Not connected",
		sideW:    260,
		listH:    360,
		stopPoll: make(chan struct{}),
	}
	a.tree.List.Selected = &a.treeSel
	a.list.Selected = &a.listSel
	a.list.Label = func(i int) string {
		if i < 0 || i >= len(a.entries) {
			return ""
		}
		return a.entries[i].Name
	}
	return a
}

func (a *app) onMain(fn func()) {
	mygo.RunOnMain(func() {
		fn()
		if a.win != nil {
			a.win.Invalidate()
		}
	})
}

func (a *app) refresh() {
	a.status = "Contacting the agent…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := a.client.Health(ctx)
		var nodes []agent.NodeInfo
		if err == nil {
			nodes, err = a.client.Nodes(ctx)
		}
		a.onMain(func() {
			if err != nil {
				a.alive = false
				a.err = err.Error()
				a.status = "Agent is not running. Start tailfile-agent (or tailfile-agent -demo) and press Refresh."
				return
			}
			a.alive = true
			a.err = ""
			a.nodes = nodes
			a.subdirs = map[string][]string{}
			a.status = fmt.Sprintf("%d nodes", len(nodes))
			if a.folderN == "" && len(nodes) > 0 && len(nodes[0].ReadRoots) > 0 {
				a.folderN = nodes[0].ID
				a.folderP = nodes[0].ReadRoots[0]
				a.loadList()
			}
		})
	}()
}

func (a *app) loadList() {
	if a.folderN == "" || a.folderP == "" {
		return
	}
	key := a.folderN + "\n" + a.folderP
	if a.listBusy && a.listTok == key {
		return
	}
	a.listTok = key
	a.listBusy = true
	node, path := a.folderN, a.folderP
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		listing, err := a.client.List(ctx, node, path)
		_ = a.client.SetDropTarget(ctx, node, path)
		a.onMain(func() {
			if a.folderN != node || a.folderP != path {
				return
			}
			a.listBusy = false
			if err != nil {
				a.err = err.Error()
				a.entries = nil
				return
			}
			a.err = ""
			a.entries = listing.Entries
			if a.listSel >= len(a.entries) {
				a.listSel = -1
			}
		})
	}()
}

func (a *app) ensureDirs(node, path string) {
	key := dirKey(node, path)
	if a.loading[key] {
		return
	}
	if _, ok := a.subdirs[key]; ok {
		return
	}
	a.loading[key] = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		listing, err := a.client.List(ctx, node, path)
		kids := []string{}
		if err == nil {
			for _, e := range listing.Entries {
				if e.IsDir {
					kids = append(kids, dirKey(node, e.Path))
				}
			}
		}
		a.onMain(func() {
			a.loading[key] = false
			if err != nil {
				a.err = err.Error()
				return
			}
			a.subdirs[key] = kids
		})
	}()
}

func (a *app) selectedEntry() (agent.Entry, bool) {
	if a.listSel < 0 || a.listSel >= len(a.entries) {
		return agent.Entry{}, false
	}
	return a.entries[a.listSel], true
}

func (a *app) copySelected() {
	e, ok := a.selectedEntry()
	if !ok || a.folderN == "" {
		a.status = "Select a file or folder to copy"
		return
	}
	a.clip = &clip{Node: a.folderN, Path: e.Path, Name: e.Name, IsDir: e.IsDir}
	a.status = "Copied " + e.Name
}

func (a *app) paste() {
	if a.clip == nil {
		a.status = "Clipboard is empty"
		return
	}
	if a.folderN == "" || a.folderP == "" {
		a.status = "Open a destination folder"
		return
	}
	req := agent.TransferRequest{
		Mode:     a.mode,
		FromNode: a.clip.Node,
		FromPath: a.clip.Path,
		ToNode:   a.folderN,
		ToPath:   agent.JoinRemote(a.folderP, a.clip.Name),
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		tr, err := a.client.StartTransfer(ctx, req)
		a.onMain(func() {
			if err != nil {
				a.err = err.Error()
				a.status = "Paste failed"
				return
			}
			a.err = ""
			a.status = fmt.Sprintf("%s %s → %s", a.mode, tr.FromNode, tr.ToNode)
		})
		a.pollOnce()
	}()
}

func (a *app) mkdir() {
	if a.newName == "" || a.folderN == "" {
		a.status = "Type a folder name first"
		return
	}
	path := agent.JoinRemote(a.folderP, a.newName)
	node, name := a.folderN, a.newName
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := a.client.Mkdir(ctx, node, path)
		a.onMain(func() {
			if err != nil {
				a.err = err.Error()
				return
			}
			a.newName = ""
			a.status = "Created " + name
			delete(a.subdirs, dirKey(node, a.folderP))
			a.loadList()
		})
	}()
}

func (a *app) rename() {
	e, ok := a.selectedEntry()
	if !ok || a.renameTo == "" {
		a.status = "Select an item and type the new name"
		return
	}
	dest := agent.JoinRemote(a.folderP, a.renameTo)
	node := a.folderN
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := a.client.Rename(ctx, node, e.Path, dest)
		a.onMain(func() {
			if err != nil {
				a.err = err.Error()
				return
			}
			a.status = "Renamed"
			a.loadList()
		})
	}()
}

func (a *app) cacheSelected() {
	e, ok := a.selectedEntry()
	if !ok || e.IsDir {
		a.status = "Select a file to cache for dragging out"
		return
	}
	node, path, name := a.folderN, e.Path, e.Name
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		res, err := a.client.Cache(ctx, node, path)
		a.onMain(func() {
			if err != nil {
				a.err = err.Error()
				return
			}
			a.dragPath = res.LocalPath
			if res.Cached {
				a.dragNote = "Cached " + name + ". Drag the handle into the file manager."
			} else {
				a.dragNote = "This file is already on this machine. Drag the handle out."
			}
			a.status = a.dragNote
		})
	}()
}

func (a *app) revealDrag() {
	if a.dragPath == "" {
		a.status = "Cache a file first"
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := a.client.Reveal(ctx, a.dragPath)
		a.onMain(func() {
			if err != nil {
				a.err = err.Error()
			}
		})
	}()
}

func (a *app) importTyped() {
	p := strings.TrimSpace(a.importPath)
	if p == "" {
		a.status = "Type a local path to import"
		return
	}
	a.importDropped([]string{p})
	a.importPath = ""
}

func (a *app) importDropped(paths []string) {
	if a.folderN == "" || len(paths) == 0 {
		return
	}
	req := agent.ImportRequest{Node: a.folderN, DestDir: a.folderP, Paths: paths}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := a.client.Import(ctx, req)
		a.onMain(func() {
			if err != nil {
				a.err = err.Error()
				return
			}
			a.status = fmt.Sprintf("Importing %d item(s)", len(paths))
		})
	}()
}

func (a *app) loadPreview(e agent.Entry) {
	if e.IsDir || e.Path == a.previewPath {
		return
	}
	a.previewPath = e.Path
	a.previewKind = agent.PreviewKind(e.Name)
	a.previewText = ""
	a.previewImg = nil
	node, path, kind := a.folderN, e.Path, a.previewKind
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		prev, err := a.client.Preview(ctx, node, path)
		var bmp *ui.Bitmap
		if err == nil && kind == "image" && e.Size <= 20<<20 {
			raw, rerr := a.client.Open(ctx, node, path, 20<<20)
			if rerr == nil {
				bmp, _ = ui.DecodeBitmap(raw)
			}
		}
		pdfURL := ""
		if kind == "pdf" {
			q := url.Values{}
			q.Set("node", node)
			q.Set("path", path)
			pdfURL = a.client.Base + "/v1/fs/read?" + q.Encode()
		}
		a.onMain(func() {
			if a.previewPath != path {
				return
			}
			if err != nil {
				a.previewText = err.Error()
				return
			}
			a.previewKind = prev.Kind
			a.previewText = prev.Text
			a.previewImg = bmp
			if pdfURL != "" && pdfURL != a.pdfURL && a.win != nil {
				a.pdfURL = pdfURL
				if a.pdfView == nil {
					wv, werr := a.win.NewWebView(mygo.WebViewOptions{URL: pdfURL})
					a.pdfView = wv
					if werr != nil {
						a.pdfErr = werr.Error()
					}
				} else if a.pdfView.Page() != nil {
					if lerr := a.pdfView.Page().LoadURL(pdfURL); lerr != nil {
						a.pdfErr = lerr.Error()
					}
				}
			}
		})
	}()
}

func (a *app) pollOnce() {
	if !a.alive {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		trs, err1 := a.client.Transfers(ctx)
		jobs, err2 := a.client.Jobs(ctx)
		a.onMain(func() {
			reload := false
			if err1 == nil {
				for _, tr := range trs {
					if tr.Job.Status == "completed" && a.seenJob[tr.ID] != "completed" {
						reload = true
					}
					a.seenJob[tr.ID] = tr.Job.Status
				}
				a.transfers = trs
			}
			if err2 == nil {
				var imports []agent.Job
				for _, j := range jobs {
					if j.Mode == "import" {
						imports = append(imports, j)
						if j.Status == "completed" && a.seenJob[j.ID] != "completed" {
							reload = true
						}
						a.seenJob[j.ID] = j.Status
					}
				}
				a.imports = imports
			}
			if reload && a.folderN != "" {
				delete(a.subdirs, dirKey(a.folderN, a.folderP))
				a.loadList()
			}
		})
	}()
}

func (a *app) startPoll() {
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-a.stopPoll:
				return
			case <-t.C:
				a.pollOnce()
			}
		}
	}()
}

func (a *app) cancelTransfer(id string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := a.client.CancelTransfer(ctx, id)
		a.onMain(func() {
			if err != nil {
				a.err = err.Error()
			}
		})
		a.pollOnce()
	}()
}

func (a *app) resumeTransfer(id string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := a.client.ResumeTransfer(ctx, id)
		a.onMain(func() {
			if err != nil {
				a.err = err.Error()
			}
		})
		a.pollOnce()
	}()
}

func (a *app) followTree() {
	if a.treeSel < 0 || a.treeSel >= a.tree.Rows() {
		return
	}
	key := a.tree.Item(a.treeSel)
	kind, node, path := splitKey(key)
	if kind != "d" {
		return
	}
	if a.folderN == node && a.folderP == path {
		return
	}
	a.folderN = node
	a.folderP = path
	a.listSel = -1
	a.previewPath = ""
	a.loadList()
}

func (a *app) openSelectedDir() {
	e, ok := a.selectedEntry()
	if !ok || !e.IsDir {
		return
	}
	a.folderP = e.Path
	a.tree.Open.Add(dirKey(a.folderN, e.Path))
	a.listSel = -1
	a.previewPath = ""
	delete(a.subdirs, dirKey(a.folderN, parentPath(e.Path)))
	a.loadList()
}

func nodeKey(id string) string { return "n\x1f" + id }

func dirKey(node, path string) string { return "d\x1f" + node + "\x1f" + path }

func splitKey(key string) (kind, a, b string) {
	if len(key) < 2 || key[1] != '\x1f' {
		return "", key, ""
	}
	kind = key[:1]
	rest := key[2:]
	if kind == "n" {
		return kind, rest, ""
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] == '\x1f' {
			return kind, rest[:i], rest[i+1:]
		}
	}
	return kind, rest, ""
}

func parentPath(p string) string {
	s := p
	for len(s) > 1 && (s[len(s)-1] == '/' || s[len(s)-1] == '\\') {
		s = s[:len(s)-1]
	}
	i := len(s) - 1
	for i >= 0 && s[i] != '/' && s[i] != '\\' {
		i--
	}
	if i <= 0 {
		return s
	}
	return s[:i]
}

func formatSize(n int64, dir bool) string {
	if dir {
		return "—"
	}
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
	}
}

func jobLine(j agent.Job) string {
	pct := ""
	if j.BytesTotal > 0 {
		pct = fmt.Sprintf(" %d%%", j.BytesDone*100/j.BytesTotal)
	}
	name := agent.BaseRemote(j.Current)
	if name == "" {
		name = agent.BaseRemote(j.DestPath)
	}
	msg := j.Status + pct + "  " + name
	if j.Error != "" && j.Status != "completed" {
		msg += "  " + j.Error
	}
	return msg
}

// dragData builds a file-list drag payload when a cache path exists.
func dragData(path string) (transfer.Data, bool) {
	if path == "" {
		return transfer.Data{}, false
	}
	d, err := transfer.FileData(path)
	return d, err == nil
}
