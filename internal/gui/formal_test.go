package gui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"tailfile.local/internal/agent"
)

func fixtureNodes() ([]agent.NodeInfo, map[string][]string, map[string][]agent.Entry) {
	nodes := []agent.NodeInfo{
		{
			ID:        "alpha",
			Name:      "alpha",
			OS:        "macos",
			Online:    true,
			AgentOK:   true,
			Self:      true,
			ReadOnly:  false,
			ReadRoots: []string{"/srv/alpha"},
		},
		{
			ID:        "gamma",
			Name:      "gamma",
			OS:        "linux",
			Online:    true,
			AgentOK:   true,
			Self:      false,
			ReadOnly:  true,
			ReadRoots: []string{"/srv/gamma"},
		},
	}
	writeRoots := map[string][]string{
		"alpha": {"/srv/alpha"},
		"gamma": nil,
	}
	tree := map[string][]agent.Entry{
		"alpha|/srv/alpha": {
			{Name: "notes", Path: "/srv/alpha/notes", IsDir: true},
			{Name: "hello.txt", Path: "/srv/alpha/hello.txt", Size: 12},
		},
		"alpha|/srv/alpha/notes": {
			{Name: "sub", Path: "/srv/alpha/notes/sub", IsDir: true},
			{Name: "todo.md", Path: "/srv/alpha/notes/todo.md", Size: 20},
		},
		"alpha|/srv/alpha/notes/sub": {
			{Name: "deep.txt", Path: "/srv/alpha/notes/sub/deep.txt", Size: 8},
		},
		"gamma|/srv/gamma": {
			{Name: "docs", Path: "/srv/gamma/docs", IsDir: true},
			{Name: "spec.txt", Path: "/srv/gamma/spec.txt", Size: 30},
		},
		"gamma|/srv/gamma/docs": {
			{Name: "readme.md", Path: "/srv/gamma/docs/readme.md", Size: 16},
		},
	}
	return nodes, writeRoots, tree
}

func hydrateModelListing(s *Session, tree map[string][]agent.Entry) {
	t := s.Active()
	if entries, ok := tree[t.Node+"|"+t.Path]; ok {
		cp := make([]agent.Entry, len(entries))
		copy(cp, entries)
		t.Entries = cp
	} else {
		t.Entries = nil
	}
}

func snapshotSession(s *Session) string {
	var b strings.Builder
	fmt.Fprintf(&b, "idx=%d|tabs=%d|closed=%d|filter=%q|ed=%d:%s|modal=%d|del=%d|clip=%v|confl=%v|",
		s.ActiveIdx, len(s.Tabs), len(s.ClosedTabs), s.Filter, s.Editor.Kind, s.Editor.Text,
		s.Modal, len(s.DeleteTargets), s.Clipboard != nil, s.ActiveConflict() != nil)
	for i, t := range s.Tabs {
		fmt.Fprintf(&b, "[%d:%s:%s:h%d/%d:sel%d]", i, t.Node, t.Path, t.HistIdx, len(t.History), len(t.Selected))
	}
	return b.String()
}

func checkModelInvariants(t *testing.T, s *Session, nodes []agent.NodeInfo) {
	t.Helper()
	if len(s.Tabs) == 0 {
		t.Fatal("INV violation: len(Tabs) == 0")
	}
	if s.ActiveIdx < 0 || s.ActiveIdx >= len(s.Tabs) {
		t.Fatalf("INV violation: ActiveIdx %d out of [0, %d)", s.ActiveIdx, len(s.Tabs))
	}
	nodeRoots := map[string][]string{}
	nodeMap := map[string]agent.NodeInfo{}
	for _, n := range nodes {
		nodeRoots[n.ID] = n.ReadRoots
		nodeMap[n.ID] = n
	}
	for i, tab := range s.Tabs {
		if tab.Node == "" {
			if tab.Path != "" || len(tab.History) != 0 || len(tab.Selected) != 0 {
				t.Fatalf("tab %d blank node invariant violation: path=%q hist=%d sel=%d", i, tab.Path, len(tab.History), len(tab.Selected))
			}
			continue
		}
		if len(tab.History) == 0 {
			if tab.HistIdx != 0 || tab.Path != "" {
				t.Fatalf("tab %d empty history mismatch: HistIdx=%d Path=%q", i, tab.HistIdx, tab.Path)
			}
		} else {
			if tab.HistIdx < 0 || tab.HistIdx >= len(tab.History) {
				t.Fatalf("tab %d HistIdx=%d out of [0,%d)", i, tab.HistIdx, len(tab.History))
			}
			if CanonicalizeRemote(tab.History[tab.HistIdx]) != CanonicalizeRemote(tab.Path) {
				t.Fatalf("tab %d History[HistIdx]=%q != Path=%q", i, tab.History[tab.HistIdx], tab.Path)
			}
		}
		if tab.Path != "" {
			if !PathIsUnderAny(tab.Path, nodeRoots[tab.Node]) {
				t.Fatalf("tab %d path %q escaped ReadRoots %v", i, tab.Path, nodeRoots[tab.Node])
			}
			for selPath := range tab.Selected {
				if !PathIsUnder(selPath, tab.Path) || CanonicalizeRemote(selPath) == CanonicalizeRemote(tab.Path) {
					t.Fatalf("tab %d stale/foreign selection %q outside folder %q", i, selPath, tab.Path)
				}
			}
		}
	}
	if act := s.Active(); act.Node == "" {
		if s.CanWriteActive() {
			t.Fatal("blank tab reported CanWriteActive() == true")
		}
	} else if n, ok := nodeMap[act.Node]; ok && n.ReadOnly {
		if s.CanWriteActive() {
			t.Fatal("read-only node reported CanWriteActive() == true")
		}
		if s.Editor.Kind == EditorMkdir || s.Editor.Kind == EditorRename {
			t.Fatalf("write editor %d opened on read-only node", s.Editor.Kind)
		}
		if s.Modal == ModalDelete {
			t.Fatal("delete modal opened on read-only node")
		}
	}
	if s.Modal == ModalDocEdit && s.DocEditor.Node != "" {
		if n, ok := nodeMap[s.DocEditor.Node]; ok && n.ReadOnly && !s.DocEditor.ReadOnly {
			t.Fatal("read-only node opened DocEditor in writable mode")
		}
	}
}

func TestFormalTabAndNavigationInvariants(t *testing.T) {
	nodes, writeRoots, tree := fixtureNodes()
	rootSess := NewModelSession(nodes, writeRoots)
	hydrateModelListing(rootSess, tree)

	type stateNode struct {
		sess  *Session
		depth int
	}
	cloneSess := func(src *Session) *Session {
		wr := make(map[string][]string, len(src.WriteRoots))
		for k, v := range src.WriteRoots {
			wr[k] = append([]string(nil), v...)
		}
		cp := &Session{
			LocalURL:            src.LocalURL,
			Nodes:               append([]agent.NodeInfo(nil), src.Nodes...),
			WriteRoots:          wr,
			ActiveIdx:           src.ActiveIdx,
			ClosedTabs:          append([]SavedTab(nil), src.ClosedTabs...),
			nextTabID:           src.nextTabID,
			ViewMode:            src.ViewMode,
			SortKey:             src.SortKey,
			SortAsc:             src.SortAsc,
			DetailsOpen:         src.DetailsOpen,
			SidebarCollapsed:    src.SidebarCollapsed,
			SidebarWidth:        src.SidebarWidth,
			DetailsWidth:        src.DetailsWidth,
			CloseAction:         src.CloseAction,
			CloseRemember:       src.CloseRemember,
			closeModalOpen:      src.closeModalOpen,
			Theme:               src.Theme,
			Density:             src.Density,
			SyncSidebarTree:     src.SyncSidebarTree,
			AutoEnterSingleRoot: src.AutoEnterSingleRoot,
			initialNodeBound:    src.initialNodeBound,
			Filter:              src.Filter,
			Editor:              src.Editor,
			DocEditor:           src.DocEditor,
			Modal:               src.Modal,
			docModalOpen:        src.docModalOpen,
			DeleteTargets:       append([]agent.Entry(nil), src.DeleteTargets...),
			SettingsDraft:       src.SettingsDraft,
			ConflictApplyAll:    src.ConflictApplyAll,
			ConflictHold:        map[string]time.Time{},
			nowFn:               src.nowFn,
			skipPersist:         true,
		}
		cp.initSidebarTree()
		cp.noteNodesUpdated()
		if src.Clipboard != nil {
			clip := *src.Clipboard
			clip.Paths = append([]string(nil), src.Clipboard.Paths...)
			cp.Clipboard = &clip
		}
		cp.Tabs = make([]*Tab, len(src.Tabs))
		for i, tab := range src.Tabs {
			tc := *tab
			tc.History = append([]string(nil), tab.History...)
			tc.Entries = append([]agent.Entry(nil), tab.Entries...)
			tc.Selected = map[string]bool{}
			for k, v := range tab.Selected {
				tc.Selected[k] = v
			}
			tc.CachedPaths = map[string]string{}
			cp.Tabs[i] = &tc
		}
		return cp
	}

	visited := map[string]bool{}
	queue := []stateNode{{sess: rootSess, depth: 0}}
	visited[snapshotSession(rootSess)] = true

	maxStates := 600
	explored := 0
	for len(queue) > 0 && explored < maxStates {
		cur := queue[0]
		queue = queue[1:]
		explored++
		checkModelInvariants(t, cur.sess, nodes)
		if cur.depth >= 5 {
			continue
		}

		steps := []func(s *Session){
			func(s *Session) { s.NewTab(); hydrateModelListing(s, tree) },
			func(s *Session) { s.OpenNodeInNewTab("gamma", "/srv/gamma/docs"); hydrateModelListing(s, tree) },
			func(s *Session) { s.CloseTab(s.ActiveIdx); hydrateModelListing(s, tree) },
			func(s *Session) { s.CloseTabByID(s.Active().ID); hydrateModelListing(s, tree) },
			func(s *Session) {
				if len(s.Tabs) > 1 {
					s.SwitchTab((s.ActiveIdx + 1) % len(s.Tabs))
					hydrateModelListing(s, tree)
				}
			},
			func(s *Session) { s.ReopenClosedTab(); hydrateModelListing(s, tree) },
			func(s *Session) { s.SelectNode("alpha"); hydrateModelListing(s, tree) },
			func(s *Session) { _ = s.SelectNodePath("alpha", "/srv/alpha/notes"); hydrateModelListing(s, tree) },
			func(s *Session) { s.SelectNode("gamma"); hydrateModelListing(s, tree) },
			func(s *Session) {
				for _, e := range s.Active().Entries {
					if e.IsDir {
						_ = s.Navigate(e.Path)
						hydrateModelListing(s, tree)
						break
					}
				}
			},
			func(s *Session) { s.GoBack(); hydrateModelListing(s, tree) },
			func(s *Session) { s.GoForward(); hydrateModelListing(s, tree) },
			func(s *Session) { s.GoUp(); hydrateModelListing(s, tree) },
		}

		for _, act := range steps {
			if len(cur.sess.Tabs) >= 3 && cur.depth >= 3 {
				// Bound tab proliferation in BFS without limiting operations.
			}
			next := cloneSess(cur.sess)
			act(next)
			if len(next.Tabs) > 3 {
				continue
			}
			key := snapshotSession(next)
			if !visited[key] {
				visited[key] = true
				queue = append(queue, stateNode{sess: next, depth: cur.depth + 1})
			}
		}
	}
	if explored < 40 {
		t.Fatalf("unexpectedly small BFS graph: %d states", explored)
	}
}

func TestFormalFolderChangeClearsTransientState(t *testing.T) {
	nodes, writeRoots, tree := fixtureNodes()
	transitions := []struct {
		name string
		fn   func(s *Session)
	}{
		{"SelectNode", func(s *Session) { s.SelectNode("gamma"); hydrateModelListing(s, tree) }},
		{"SelectNodePath", func(s *Session) { _ = s.SelectNodePath("gamma", "/srv/gamma/docs"); hydrateModelListing(s, tree) }},
		{"OpenNodeInNewTab", func(s *Session) { s.OpenNodeInNewTab("gamma", "/srv/gamma/docs"); hydrateModelListing(s, tree) }},
		{"Navigate", func(s *Session) { _ = s.Navigate("/srv/alpha/notes"); hydrateModelListing(s, tree) }},
		{"GoBack", func(s *Session) {
			_ = s.Navigate("/srv/alpha/notes")
			hydrateModelListing(s, tree)
			s.Filter = "todo"
			s.GoBack()
			hydrateModelListing(s, tree)
		}},
		{"GoForward", func(s *Session) {
			_ = s.Navigate("/srv/alpha/notes")
			s.GoBack()
			s.Filter = "hello"
			s.GoForward()
			hydrateModelListing(s, tree)
		}},
		{"GoUp", func(s *Session) { s.GoUp(); hydrateModelListing(s, tree) }},
		{"SwitchTab", func(s *Session) {
			s.NewTab()
			s.SelectNode("gamma")
			s.Filter = "dirty"
			s.BeginGoto()
			s.SwitchTab(0)
			hydrateModelListing(s, tree)
		}},
		{"CloseTab", func(s *Session) {
			s.NewTab()
			s.SelectNode("gamma")
			s.Filter = "dirty"
			s.CloseTab(1)
		}},
		{"ReopenClosedTab", func(s *Session) {
			s.NewTab()
			s.SelectNode("gamma")
			s.CloseTab(1)
			s.Filter = "dirty"
			s.BeginMkdir()
			s.ReopenClosedTab()
		}},
	}

	editors := []func(s *Session){
		func(s *Session) { s.Filter = "abc" },
		func(s *Session) { s.BeginMkdir() },
		func(s *Session) {
			if len(s.Active().Entries) > 0 {
				s.SelectEntry(s.Active().Entries[0].Path, false, false)
				s.BeginRename()
			}
		},
		func(s *Session) { s.BeginGoto() },
		func(s *Session) {
			if len(s.Active().Entries) > 0 {
				s.SelectEntry(s.Active().Entries[0].Path, false, false)
				s.BeginDelete()
			}
		},
	}

	for _, tr := range transitions {
		for ei, ed := range editors {
			s := NewModelSession(nodes, writeRoots)
			_ = s.Navigate("/srv/alpha/notes")
			hydrateModelListing(s, tree)
			ed(s)
			tr.fn(s)
			if s.Filter != "" {
				t.Fatalf("%s + editor[%d]: Filter not cleared (%q)", tr.name, ei, s.Filter)
			}
			if s.Editor.Kind != EditorNone {
				t.Fatalf("%s + editor[%d]: Editor not cleared (%d)", tr.name, ei, s.Editor.Kind)
			}
			if s.Modal == ModalDelete || len(s.DeleteTargets) != 0 {
				t.Fatalf("%s + editor[%d]: ModalDelete not cleared", tr.name, ei)
			}
			checkModelInvariants(t, s, nodes)
		}
	}
}

func TestFormalModalAndWriteGating(t *testing.T) {
	nodes, writeRoots, tree := fixtureNodes()
	s := NewModelSession(nodes, writeRoots)
	hydrateModelListing(s, tree)

	// Switch to read-only node gamma: every write entry point must refuse.
	s.SelectNode("gamma")
	hydrateModelListing(s, tree)
	if len(s.Active().Entries) > 0 {
		s.SelectEntry(s.Active().Entries[0].Path, false, false)
	}
	s.Clipboard = &ClipItem{Node: "alpha", Paths: []string{"/srv/alpha/hello.txt"}}

	if s.BeginMkdir() || s.BeginRename() || s.BeginDelete() || s.Paste() {
		t.Fatal("read-only node allowed a write entry point")
	}
	checkModelInvariants(t, s, nodes)

	// Switch back to writable alpha and verify conflict modal preempts overlays
	// and cannot be dismissed via CloseModal / SyncOverlays.
	s.SelectNode("alpha")
	hydrateModelListing(s, tree)
	s.OpenSettings()
	s.Transfers = []agent.Transfer{{
		ID:       "t-1",
		FromNode: "alpha",
		ToNode:   "alpha",
		Job: agent.Job{
			ID:       "j-1",
			Status:   "conflict",
			Conflict: &agent.FileConflict{Rel: "hello.txt", Path: "/srv/alpha/hello.txt", Size: 12},
		},
	}}
	if s.ActiveConflict() == nil {
		t.Fatal("expected active conflict")
	}
	s.conflictModalOpen = false
	s.CloseModal()
	s.SyncOverlays()
	if !s.conflictModalOpen || s.ActiveConflict() == nil {
		t.Fatal("conflict dialog was wrongly dismissed without user decision")
	}

	s.ConflictApplyAll = true
	s.AnswerConflict("overwrite")
	if s.ActiveConflict() != nil || s.conflictModalOpen {
		t.Fatal("conflict should resolve after AnswerConflict")
	}
}

func TestFormalPathSandboxBoundedInputs(t *testing.T) {
	nodes, writeRoots, _ := fixtureNodes()
	s := NewModelSession(nodes, writeRoots)

	badPaths := []string{
		"",
		"relative/file",
		"/srv/alpha/..",
		"/srv/alpha/../../../etc/passwd",
		"/srv/alpha2",
		"/srv/alpha2/secret",
		`\srv\alpha\..\beta`,
		`//srv/alpha2/file`,
		`C:\Windows\System32`,
		`C:\Tailfile\..\Tailfile2\secret`,
		`file:///etc/passwd`,
	}
	for _, p := range badPaths {
		if err := s.Navigate(p); err == nil {
			t.Fatalf("sandbox accepted bad path %q", p)
		}
		if s.Active().Path != "/srv/alpha" {
			t.Fatalf("active path mutated on bad Navigate(%q): got %q", p, s.Active().Path)
		}
	}

	goodPaths := []string{
		`/srv/alpha`,
		`/srv/alpha/notes`,
		`/srv/alpha/notes/../notes/./sub`,
		`\srv\alpha\notes\sub`,
	}
	for _, p := range goodPaths {
		if err := s.Navigate(p); err != nil {
			t.Fatalf("sandbox rejected valid path %q: %v", p, err)
		}
	}
	checkModelInvariants(t, s, nodes)
}

func TestFormalSettingsDraftIsolation(t *testing.T) {
	nodes, writeRoots, _ := fixtureNodes()
	s := NewModelSession(nodes, writeRoots)
	s.Theme = ThemePrefLight
	s.Settings = &agent.Settings{Net: "system", CacheDir: "/tmp/orig-cache"}

	s.OpenSettings()
	if s.SettingsDirty() {
		t.Fatal("freshly opened settings modal should not be dirty")
	}

	// Theme and density toggles take immediate effect and persist across CancelSettings.
	s.SetTheme(ThemePrefDark)
	if s.Theme != ThemePrefDark {
		t.Fatalf("SetTheme did not apply immediately: got %s", s.Theme)
	}
	s.SetDensity(DensityPrefCompact)
	if s.Density != DensityPrefCompact {
		t.Fatalf("SetDensity did not apply immediately: got %s", s.Density)
	}

	// Unsaved text edits remain in SettingsDraft only and mark SettingsDirty.
	s.SettingsDraft.CacheDirInput = "/tmp/new-cache"
	if !s.SettingsDirty() {
		t.Fatal("editing CacheDirInput should mark SettingsDirty")
	}
	if s.Settings.CacheDir != "/tmp/orig-cache" {
		t.Fatal("draft CacheDirInput edit leaked before SaveSettings")
	}

	// Cancel drops text draft while preserving immediate SetTheme / SetDensity choices.
	s.CancelSettings()
	if s.Theme != ThemePrefDark || s.Density != DensityPrefCompact || s.Settings.CacheDir != "/tmp/orig-cache" || s.Modal != ModalNone {
		t.Fatalf("CancelSettings unexpected state: theme=%s density=%s cache=%s modal=%d", s.Theme, s.Density, s.Settings.CacheDir, s.Modal)
	}

	// Re-open, edit CacheDirInput, SaveSettings commits cleanly.
	s.OpenSettings()
	s.SettingsDraft.CacheDirInput = "/tmp/saved-cache"
	s.SaveSettings()
	if s.Settings.CacheDir != "/tmp/saved-cache" || s.Modal != ModalNone {
		t.Fatalf("SaveSettings did not commit CacheDir cleanly: cache=%s modal=%d", s.Settings.CacheDir, s.Modal)
	}
}

func TestDecideWindowClose(t *testing.T) {
	for _, tc := range []struct {
		tray, quitting bool
		pref           CloseActionPref
		want           CloseOutcome
	}{
		{true, false, CloseActionAsk, ClosePromptUser},
		{true, false, "", ClosePromptUser},
		{true, false, "invalid", ClosePromptUser},
		{true, false, CloseActionTray, CloseHideToTray},
		{true, false, CloseActionQuit, CloseAllowQuit},
		{true, true, CloseActionAsk, CloseAllowQuit},
		{true, true, CloseActionTray, CloseAllowQuit},
		{false, false, CloseActionAsk, CloseAllowQuit},
		{false, false, CloseActionTray, CloseAllowQuit},
	} {
		if got := DecideWindowClose(tc.tray, tc.quitting, tc.pref); got != tc.want {
			t.Fatalf("DecideWindowClose(tray=%v, quitting=%v, pref=%q) = %v, want %v", tc.tray, tc.quitting, tc.pref, got, tc.want)
		}
	}
}

func TestPersistClampsWidthsAndCloseAction(t *testing.T) {
	p := UiPersist{
		SidebarWidth: 40,
		DetailsWidth: 900,
		CloseAction:  "unknown",
	}
	p.sanitize()
	if p.SidebarWidth != MinSidebarWidth {
		t.Fatalf("SidebarWidth = %v, want %v", p.SidebarWidth, MinSidebarWidth)
	}
	if p.DetailsWidth != MaxDetailsWidth {
		t.Fatalf("DetailsWidth = %v, want %v", p.DetailsWidth, MaxDetailsWidth)
	}
	if p.CloseAction != CloseActionAsk {
		t.Fatalf("CloseAction = %q, want %q", p.CloseAction, CloseActionAsk)
	}

	pZero := UiPersist{}
	pZero.sanitize()
	if pZero.SidebarWidth != DefaultSidebarWidth || pZero.DetailsWidth != DefaultDetailsWidth {
		t.Fatalf("zero widths sanitized to %v / %v, want %v / %v", pZero.SidebarWidth, pZero.DetailsWidth, DefaultSidebarWidth, DefaultDetailsWidth)
	}

	nodes, wr, _ := fixtureNodes()
	s := NewModelSession(nodes, wr)
	s.SetSidebarWidth(999, false)
	s.SetDetailsWidth(50, false)
	if s.SidebarWidth != MaxSidebarWidth || s.DetailsWidth != MinDetailsWidth {
		t.Fatalf("clamped session widths = %v / %v", s.SidebarWidth, s.DetailsWidth)
	}

	hid, quit := false, false
	s.onHideWindow = func() { hid = true }
	s.onQuitApp = func() { quit = true }

	s.OpenClosePrompt()
	if s.Modal != ModalClosePrompt || !s.CloseRemember {
		t.Fatalf("OpenClosePrompt unexpected state: modal=%d remember=%v", s.Modal, s.CloseRemember)
	}
	s.CloseRemember = false
	s.ChooseCloseAction(CloseActionQuit)
	if !quit || s.CloseAction != CloseActionAsk || s.Modal != ModalNone {
		t.Fatalf("ChooseCloseAction(quit, remember=false): quit=%v pref=%q modal=%d", quit, s.CloseAction, s.Modal)
	}
	s.OpenClosePrompt()
	s.ChooseCloseAction(CloseActionTray)
	if !hid || s.CloseAction != CloseActionTray || s.Modal != ModalNone {
		t.Fatalf("ChooseCloseAction(tray, remember=true): hid=%v pref=%q modal=%d", hid, s.CloseAction, s.Modal)
	}
}

func TestFormalDocEditorInvariants(t *testing.T) {
	nodes, writeRoots, tree := fixtureNodes()
	s := NewModelSession(nodes, writeRoots)
	hydrateModelListing(s, tree)

	// Read-only node gamma must open in read-only mode and refuse saves.
	s.SelectNode("gamma")
	hydrateModelListing(s, tree)
	s.SelectEntry("/srv/gamma/spec.txt", false, false)
	if !s.OpenDocEditor() {
		t.Fatal("expected OpenDocEditor to open read-only viewer on gamma")
	}
	if s.Modal != ModalDocEdit || !s.DocEditor.ReadOnly {
		t.Fatalf("expected ReadOnly DocEditor on gamma, got modal=%d ro=%v", s.Modal, s.DocEditor.ReadOnly)
	}
	if s.SaveDocEditor(false) || s.SaveDocEditor(true) {
		t.Fatal("read-only DocEditor allowed save")
	}
	checkModelInvariants(t, s, nodes)
	s.RequestCloseDocEditor()
	if s.Modal != ModalNone {
		t.Fatalf("clean DocEditor did not close: modal=%d", s.Modal)
	}

	// Writable node alpha tracks dirty state, blocks accidental close, and survives ConflictPrompt preemption.
	s.SelectNode("alpha")
	hydrateModelListing(s, tree)
	s.SelectEntry("/srv/alpha/hello.txt", false, false)
	if !s.OpenDocEditor() {
		t.Fatal("expected OpenDocEditor on alpha")
	}
	if s.DocEditor.ReadOnly || s.DocEditor.Dirty() {
		t.Fatalf("expected clean writable editor, got ro=%v dirty=%v", s.DocEditor.ReadOnly, s.DocEditor.Dirty())
	}
	s.DocEditor.Buffer.Set("unsaved edit")
	if !s.DocEditor.Dirty() {
		t.Fatal("expected Dirty() == true after buffer edit")
	}

	// Simulate Escape / backdrop dismiss via SyncOverlays -> must enter DocConfirmClose without losing text.
	s.docModalOpen = false
	s.SyncOverlays()
	if s.Modal != ModalDocEdit || !s.docModalOpen || s.DocEditor.Phase != DocConfirmClose {
		t.Fatalf("expected DocConfirmClose guard, got modal=%d open=%v phase=%d", s.Modal, s.docModalOpen, s.DocEditor.Phase)
	}
	if s.DocEditor.Buffer.String() != "unsaved edit" {
		t.Fatalf("draft text lost on guard: %q", s.DocEditor.Buffer.String())
	}
	s.DismissDocSubPrompt()
	if s.DocEditor.Phase != DocReady {
		t.Fatalf("expected DocReady after DismissDocSubPrompt, got %d", s.DocEditor.Phase)
	}

	// ConflictPrompt preemption must keep ModalDocEdit and its unsaved buffer intact underneath.
	s.Transfers = []agent.Transfer{{
		ID:       "t-doc",
		FromNode: "alpha",
		ToNode:   "alpha",
		Job: agent.Job{
			ID:       "j-doc",
			Status:   "conflict",
			Conflict: &agent.FileConflict{Rel: "hello.txt", Path: "/srv/alpha/hello.txt", Size: 12},
		},
	}}
	s.SyncOverlays()
	if !s.conflictModalOpen || s.ActiveConflict() == nil {
		t.Fatal("expected ConflictPrompt to take top priority")
	}
	if s.Modal != ModalDocEdit || s.DocEditor.Buffer.String() != "unsaved edit" {
		t.Fatalf("ConflictPrompt wiped DocEditor draft: modal=%d text=%q", s.Modal, s.DocEditor.Buffer.String())
	}
	s.AnswerConflict("skip")
	if s.ActiveConflict() != nil || s.Modal != ModalDocEdit || s.DocEditor.Buffer.String() != "unsaved edit" {
		t.Fatalf("after conflict resolved, DocEditor draft not preserved: modal=%d", s.Modal)
	}

	// Clean save marks buffer non-dirty; discard force-closes.
	if !s.SaveDocEditor(false) || s.DocEditor.Dirty() {
		t.Fatalf("expected SaveDocEditor to clear dirty flag")
	}
	s.DocEditor.Buffer.Set("discard me")
	s.RequestCloseDocEditor()
	if s.DocEditor.Phase != DocConfirmClose {
		t.Fatalf("expected DocConfirmClose before force close")
	}
	s.CloseDocEditorForce()
	if s.Modal != ModalNone || s.DocEditor.Buffer != nil {
		t.Fatalf("expected clean close after CloseDocEditorForce")
	}
}
