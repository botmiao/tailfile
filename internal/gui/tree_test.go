package gui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"

	"tailfile.local/internal/agent"
)

func TestRemoteAncestorPaths(t *testing.T) {
	cases := []struct {
		name      string
		target    string
		roots     []string
		wantRoot  string
		wantChain []string
	}{
		{
			name:      "posix_nested",
			target:    "/srv/alpha/notes/sub",
			roots:     []string{"/srv/alpha"},
			wantRoot:  "/srv/alpha",
			wantChain: []string{"/srv/alpha", "/srv/alpha/notes", "/srv/alpha/notes/sub"},
		},
		{
			name:      "windows_drive_case_normalized",
			target:    `c:\Tailfile\docs\v1`,
			roots:     []string{`C:\Tailfile`},
			wantRoot:  "C:/Tailfile",
			wantChain: []string{"C:/Tailfile", "C:/Tailfile/docs", "C:/Tailfile/docs/v1"},
		},
		{
			name:      "unc_root_and_subdir",
			target:    `\\srv\share\team\q4`,
			roots:     []string{`//srv/share`},
			wantRoot:  "//srv/share",
			wantChain: []string{"//srv/share", "//srv/share/team", "//srv/share/team/q4"},
		},
		{
			name:      "longest_root_wins",
			target:    "/srv/work/proj/src",
			roots:     []string{"/srv", "/srv/work/proj"},
			wantRoot:  "/srv/work/proj",
			wantChain: []string{"/srv/work/proj", "/srv/work/proj/src"},
		},
		{
			name:      "outside_roots_rejected",
			target:    "/etc/passwd",
			roots:     []string{"/srv/alpha"},
			wantRoot:  "",
			wantChain: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRoot, gotChain := RemoteAncestorPaths(tc.target, tc.roots)
			if gotRoot != tc.wantRoot || !reflect.DeepEqual(gotChain, tc.wantChain) {
				t.Fatalf("got (%q, %v), want (%q, %v)", gotRoot, gotChain, tc.wantRoot, tc.wantChain)
			}
		})
	}
}

func TestSidebarChildrenLazyCacheAndFilters(t *testing.T) {
	nodes := []agent.NodeInfo{
		{ID: "single", Name: "single", Online: true, AgentOK: true, ReadRoots: []string{"/srv/single"}},
		{ID: "multi", Name: "multi", Online: true, AgentOK: true, ReadRoots: []string{"/srv/a", "/srv/b"}},
		{ID: "down", Name: "down", Online: false, AgentOK: true, ReadRoots: []string{"/srv/down"}},
		{ID: "noroot", Name: "noroot", Online: true, AgentOK: true},
	}
	s := NewModelSession(nodes, nil)

	// Offline and rootless devices are leaves (nil).
	if kids := s.SidebarChildren(SidebarKey{Kind: SidebarNode, NodeID: "down"}); kids != nil {
		t.Fatalf("offline device returned children %v, want nil", kids)
	}
	if kids := s.SidebarChildren(SidebarKey{Kind: SidebarNode, NodeID: "noroot"}); kids != nil {
		t.Fatalf("rootless device returned children %v, want nil", kids)
	}

	// Single-root device collapsed + unloaded -> non-nil empty slice draws disclosure arrow.
	singleKey := SidebarKey{Kind: SidebarNode, NodeID: "single"}
	if kids := s.SidebarChildren(singleKey); kids == nil || len(kids) != 0 {
		t.Fatalf("unloaded single-root collapsed kids = %#v, want non-nil empty", kids)
	}

	// Unloaded while marked open shows placeholder row.
	s.Tree.Expanded[singleKey] = true
	kids := s.SidebarChildren(singleKey)
	if len(kids) != 1 || kids[0].Kind != SidebarPlaceholder {
		t.Fatalf("unloaded expanded kids = %#v, want 1 placeholder", kids)
	}

	// Loaded dir filters out files and sorts subdirectories alphabetically.
	s.UpdateSidebarDirCache("single", "/srv/single", []agent.Entry{
		{Name: "readme.md", Path: "/srv/single/readme.md", IsDir: false},
		{Name: "Zed", Path: "/srv/single/Zed", IsDir: true},
		{Name: "alpha", Path: "/srv/single/alpha", IsDir: true},
	})
	kids = s.SidebarChildren(singleKey)
	want := []SidebarKey{
		{Kind: SidebarDir, NodeID: "single", Path: "/srv/single/alpha"},
		{Kind: SidebarDir, NodeID: "single", Path: "/srv/single/Zed"},
	}
	if !reflect.DeepEqual(kids, want) {
		t.Fatalf("loaded folder-only kids = %#v, want %#v", kids, want)
	}

	// Turning AutoEnterSingleRoot off wraps the sole root beneath single node.
	s.SetAutoEnterSingleRoot(false)
	kids = s.SidebarChildren(singleKey)
	wantRoot := []SidebarKey{{Kind: SidebarDir, NodeID: "single", Path: "/srv/single"}}
	if !reflect.DeepEqual(kids, wantRoot) {
		t.Fatalf("flattened off single kids = %#v, want %#v", kids, wantRoot)
	}

	// Multi-root device always exposes its roots directly regardless of AutoEnterSingleRoot.
	s.SetAutoEnterSingleRoot(true)
	multiKids := s.SidebarChildren(SidebarKey{Kind: SidebarNode, NodeID: "multi"})
	if len(multiKids) != 2 || multiKids[0].Path != "/srv/a" || multiKids[1].Path != "/srv/b" {
		t.Fatalf("multi-root kids = %#v", multiKids)
	}

	// File-only folder returns nil after load and queues PendingClose to remove disclosure arrow.
	emptyKey := SidebarKey{Kind: SidebarDir, NodeID: "single", Path: "/srv/single/alpha"}
	s.UpdateSidebarDirCache("single", "/srv/single/alpha", []agent.Entry{
		{Name: "only.txt", Path: "/srv/single/alpha/only.txt", IsDir: false},
	})
	if kids := s.SidebarChildren(emptyKey); kids != nil {
		t.Fatalf("file-only dir kids = %#v, want nil to hide chevron", kids)
	}
	if len(s.Tree.PendingClose) == 0 || s.Tree.PendingClose[len(s.Tree.PendingClose)-1] != emptyKey {
		t.Fatalf("expected PendingClose for %#v, got %#v", emptyKey, s.Tree.PendingClose)
	}

	// Error phase exposes placeholder for retry.
	errKey := SidebarKey{Kind: SidebarDir, NodeID: "single", Path: "/srv/single/Zed"}
	s.Tree.Cache[errKey] = &DirCacheEntry{Phase: DirError, Err: "boom"}
	errKids := s.SidebarChildren(errKey)
	if len(errKids) != 1 || errKids[0].Kind != SidebarPlaceholder || s.SidebarRowLabel(errKids[0]) != "加载失败（点击重试）" {
		t.Fatalf("error placeholder mismatch: %#v", errKids)
	}
}

func TestSyncSidebarToActiveAndSetters(t *testing.T) {
	nodes, wr, tree := fixtureNodes()
	s := NewModelSession(nodes, wr)
	hydrateModelListing(s, tree)

	// Default SyncSidebarTree is false: navigating deeper leaves Tree.Expanded empty.
	if s.SyncSidebarTree {
		t.Fatal("default SyncSidebarTree = true, want false")
	}
	if !s.AutoEnterSingleRoot {
		t.Fatal("default AutoEnterSingleRoot = false, want true")
	}
	if err := s.Navigate("/srv/alpha/notes/sub"); err != nil {
		t.Fatal(err)
	}
	if len(s.Tree.Expanded) != 0 {
		t.Fatalf("SyncSidebarTree=false unexpectedly expanded %#v", s.Tree.Expanded)
	}

	// Enabling SyncSidebarTree immediately expands ancestors up to sub without opening sub itself.
	s.SetSyncSidebarTree(true)
	alphaNode := SidebarKey{Kind: SidebarNode, NodeID: "alpha"}
	notesDir := SidebarKey{Kind: SidebarDir, NodeID: "alpha", Path: "/srv/alpha/notes"}
	subDir := SidebarKey{Kind: SidebarDir, NodeID: "alpha", Path: "/srv/alpha/notes/sub"}
	if !s.Tree.Expanded[alphaNode] || !s.Tree.Expanded[notesDir] || s.Tree.Expanded[subDir] {
		t.Fatalf("unexpected Expanded after SetSyncSidebarTree(true): %#v", s.Tree.Expanded)
	}

	// VisibleSidebarKeys + DesiredSidebarKey picks notes/sub once dir cache is populated.
	s.UpdateSidebarDirCache("alpha", "/srv/alpha", tree["alpha|/srv/alpha"])
	s.UpdateSidebarDirCache("alpha", "/srv/alpha/notes", tree["alpha|/srv/alpha/notes"])
	var open ui.Selection[SidebarKey]
	for k, on := range s.Tree.Expanded {
		if on {
			open.Add(k)
		}
	}
	vis := s.VisibleSidebarKeys(&open)
	got := s.DesiredSidebarKey(func(i int) SidebarKey { return vis[i] }, len(vis))
	if got != subDir {
		t.Fatalf("DesiredSidebarKey = %#v, want %#v (visible=%#v)", got, subDir, vis)
	}
}

func TestPersistSidebarDefaultsAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Legacy ui.json without new fields must default AutoEnterSingleRoot=true, SyncSidebarTree=false.
	if err := os.WriteFile(filepath.Join(dir, "ui.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := LoadPersist(dir)
	if !loaded.AutoEnterSingleRootEnabled() || loaded.SyncSidebarTree {
		t.Fatalf("legacy defaults mismatch: auto=%v sync=%v", loaded.AutoEnterSingleRootEnabled(), loaded.SyncSidebarTree)
	}

	// Explicit false for AutoEnterSingleRoot and true for SyncSidebarTree must round-trip faithfully.
	loaded.AutoEnterSingleRoot = boolPtr(false)
	loaded.SyncSidebarTree = true
	if err := SavePersist(dir, loaded); err != nil {
		t.Fatal(err)
	}
	reloaded := LoadPersist(dir)
	if reloaded.AutoEnterSingleRootEnabled() || !reloaded.SyncSidebarTree {
		t.Fatalf("round-trip mismatch: auto=%v sync=%v", reloaded.AutoEnterSingleRootEnabled(), reloaded.SyncSidebarTree)
	}
}

func TestNewTabBlankAndOpenNodeInNewTab(t *testing.T) {
	nodes, wr, tree := fixtureNodes()
	s := NewModelSession(nodes, wr)
	hydrateModelListing(s, tree)

	// NewTab creates Scheme 1 blank tab.
	s.NewTab()
	if len(s.Tabs) != 2 || s.Active().Node != "" || s.Active().Path != "" {
		t.Fatalf("NewTab state mismatch: tabs=%d active=%+v", len(s.Tabs), s.Active())
	}
	if s.CanWriteActive() {
		t.Fatal("blank tab must not report CanWriteActive()")
	}

	// Closing a blank tab must not push an empty entry to ClosedTabs.
	blankID := s.Active().ID
	s.CloseTabByID(blankID)
	if len(s.Tabs) != 1 || len(s.ClosedTabs) != 0 {
		t.Fatalf("after closing blank tab: tabs=%d closed=%d", len(s.Tabs), len(s.ClosedTabs))
	}

	// OpenNodeInNewTab opens requested subfolder and enforces sandbox on invalid paths.
	if s.OpenNodeInNewTab("alpha", "/srv/alpha2/secret") {
		t.Fatal("OpenNodeInNewTab accepted out-of-root path")
	}
	if len(s.Tabs) != 1 {
		t.Fatalf("failed OpenNodeInNewTab altered tab count: %d", len(s.Tabs))
	}
	if !s.OpenNodeInNewTab("gamma", "/srv/gamma/docs") {
		t.Fatal("OpenNodeInNewTab rejected valid gamma docs path")
	}
	if len(s.Tabs) != 2 || s.Active().Node != "gamma" || s.Active().Path != "/srv/gamma/docs" {
		t.Fatalf("OpenNodeInNewTab result mismatch: tabs=%d active=%+v", len(s.Tabs), s.Active())
	}

	// SelectNodePath on same active node navigates (preserving Back history).
	if err := s.SelectNodePath("gamma", "/srv/gamma"); err != nil {
		t.Fatal(err)
	}
	if !s.CanGoBack() || s.Active().Path != "/srv/gamma" {
		t.Fatalf("SelectNodePath same-node navigation did not record Back history: %+v", s.Active())
	}
}
