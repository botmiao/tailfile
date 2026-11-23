package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tailfile.local/internal/agent"
)

// EditorKind identifies which inline bar editor is open.
type EditorKind uint8

const (
	EditorNone EditorKind = iota
	EditorMkdir
	EditorRename
	EditorGoto
)

// EditorState holds the active inline editor state.
type EditorState struct {
	Kind       EditorKind
	Text       string
	Err        string
	TargetPath string
	NeedFocus  bool
}

// ModalKind identifies the active modal dialog (other than file conflicts,
// which are dynamically derived from paused jobs/transfers and preempt all modals).
type ModalKind uint8

const (
	ModalNone ModalKind = iota
	ModalSettings
	ModalDelete
	ModalDaemon
	ModalClosePrompt
	ModalDocEdit
)

// CloseOutcome describes how a desktop window close request is handled.
type CloseOutcome uint8

const (
	CloseAllowQuit CloseOutcome = iota
	CloseHideToTray
	ClosePromptUser
)

// DecideWindowClose resolves window close behavior from tray/quit state and user preference.
func DecideWindowClose(trayActive, quitting bool, pref CloseActionPref) CloseOutcome {
	if !trayActive || quitting {
		return CloseAllowQuit
	}
	switch sanitizeCloseAction(pref) {
	case CloseActionTray:
		return CloseHideToTray
	case CloseActionQuit:
		return CloseAllowQuit
	default:
		return ClosePromptUser
	}
}

// ClipItem stores copied remote paths for cross-node paste.
type ClipItem struct {
	Node  string
	Paths []string
}

// EntryPreview holds preview data for a single selected file.
type EntryPreview struct {
	Node       string
	Path       string
	Name       string
	Kind       string
	Size       int64
	Mime       string
	Text       string
	ImageBytes []byte
	Image      image.Image
	Truncated  bool
	Loading    bool
	Err        string
}

// SettingsDraft buffers unapplied text changes made in the Settings modal until Save.
type SettingsDraft struct {
	AuthKeyInput  string
	CacheDirInput string
	Saving        bool
	Err           string
	ActionMsg     string
}

// ConflictPrompt describes a paused transfer or import waiting for user decision.
type ConflictPrompt struct {
	IsJob    bool
	ID       string
	Subtitle string
	File     agent.FileConflict
}

// Tab owns all per-tab navigation and folder listing state.
// Single source of truth: Session never duplicates Tab fields at the top level.
type Tab struct {
	ID           uint64
	Node         string
	Path         string
	Entries      []agent.Entry
	Selected     map[string]bool
	SelectAnchor string
	History      []string
	HistIdx      int
	Loading      bool
	Err          string
	Preview      *EntryPreview
	ListGen      uint64
	PreviewGen   uint64
	CachedPaths  map[string]string
}

// GUIOptions configures a live or test Session.
type GUIOptions struct {
	TokenFile string
}

// Session is the complete UI-agnostic state machine for Tailfile.
// All mutations happen on the UI thread (or synchronously in unit/formal tests).
type Session struct {
	Conn       *agent.Conn
	Client     *agent.Client
	StateDir   string
	LocalURL   string
	TokenFile  string
	Offline    bool
	OfflineErr string
	Guide      InstallGuide

	Nodes      []agent.NodeInfo
	WriteRoots map[string][]string
	Info       *agent.Info
	Settings   *agent.Settings
	Transfers  []agent.Transfer
	Jobs       []agent.Job

	Tabs       []*Tab
	ActiveIdx  int
	ClosedTabs []SavedTab
	nextTabID  uint64

	ViewMode            ViewMode
	SortKey             SortCol
	SortAsc             bool
	DetailsOpen         bool
	SidebarCollapsed    bool
	SidebarWidth        float32
	DetailsWidth        float32
	CloseAction         CloseActionPref
	CloseRemember       bool
	Theme               ThemePref
	Density             DensityPref
	SyncSidebarTree     bool
	AutoEnterSingleRoot bool
	Tree                SidebarTree
	initialNodeBound    bool

	Filter           string
	Clipboard        *ClipItem
	Editor           EditorState
	DocEditor        DocEditorState
	Modal            ModalKind
	DeleteTargets    []agent.Entry
	SettingsDraft    SettingsDraft
	ConflictApplyAll bool
	ConflictHold     map[string]time.Time

	StatusMsg   string
	StatusIsErr bool

	// Context menu target entry path + persistent *bool states for mygo/ui overlays.
	CtxTargetPath      string
	conflictModalOpen  bool
	settingsModalOpen  bool
	deleteModalOpen    bool
	daemonModalOpen    bool
	closeModalOpen     bool
	docModalOpen       bool
	ctxMenuOpen        bool
	mobileDrawerOpen   bool
	mobileDetailsOpen  bool
	mobileTransferOpen bool

	// Last drop-target pushed to agent, so poll only sends updates when changed.
	lastDropNode string
	lastDropDir  string

	// View-layer poll & transfer bar UI helpers.
	lastPollAt        time.Time
	transfersExpanded bool

	// UI thread mailbox for background HTTP results.
	ctx           context.Context
	cancel        context.CancelFunc
	inboxMu       sync.Mutex
	inbox         []func()
	wakeFn        func()
	onThemeChange func(ThemePref)
	onHideWindow  func()
	onQuitApp     func()
	pendingOps    atomic.Int64
	nowFn         func() time.Time
	pollCount     atomic.Int64
	skipPersist   bool
}

// NewSession constructs a live Session backed by conn (may be ConnOffline).
func NewSession(conn *agent.Conn, opts GUIOptions) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	var stateDir, localURL string
	var client *agent.Client
	var offline bool
	var probeErr string
	if conn != nil {
		stateDir = conn.StateDir
		localURL = conn.LocalURL
		client = conn.Client
		offline = conn.Mode == agent.ConnOffline
		if conn.ProbeErr != nil {
			probeErr = conn.ProbeErr.Error()
		}
	}
	if localURL == "" {
		localURL = "http://127.0.0.1:8645"
	}

	p := LoadPersist(stateDir)
	s := &Session{
		Conn:                conn,
		Client:              client,
		StateDir:            stateDir,
		LocalURL:            localURL,
		TokenFile:           opts.TokenFile,
		Offline:             offline,
		OfflineErr:          probeErr,
		WriteRoots:          map[string][]string{},
		ConflictHold:        map[string]time.Time{},
		ViewMode:            p.View,
		SortKey:             p.SortKey,
		SortAsc:             p.SortAsc,
		DetailsOpen:         p.Details,
		SidebarCollapsed:    p.SidebarCollapsed,
		SidebarWidth:        p.SidebarWidth,
		DetailsWidth:        p.DetailsWidth,
		CloseAction:         p.CloseAction,
		Theme:               p.Theme,
		Density:             p.Density,
		SyncSidebarTree:     p.SyncSidebarTree,
		AutoEnterSingleRoot: p.AutoEnterSingleRootEnabled(),
		ctx:                 ctx,
		cancel:              cancel,
		nowFn:               time.Now,
	}
	s.initSidebarTree()

	if offline {
		s.Guide = ForUnreachable(localURL, probeErr)
		s.Modal = ModalDaemon
		s.daemonModalOpen = true
	}

	for _, st := range p.Tabs {
		node := strings.TrimSpace(st.Node)
		path := CanonicalizeRemote(st.Path)
		if node == "" {
			continue
		}
		tab := s.newTabRecord(node, path)
		s.Tabs = append(s.Tabs, tab)
	}
	if len(s.Tabs) > 0 {
		s.initialNodeBound = true
	} else {
		s.Tabs = []*Tab{s.newTabRecord("", "")}
	}
	if p.ActiveTab >= 0 && p.ActiveTab < len(s.Tabs) {
		s.ActiveIdx = p.ActiveTab
	}
	if s.Client != nil && !s.Offline {
		s.RefreshAll()
	}
	return s
}

// NewModelSession constructs a pure synchronous Session (no network, no disk persist)
// for formal state-machine checking and unit tests.
func NewModelSession(nodes []agent.NodeInfo, writeRoots map[string][]string) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	wr := map[string][]string{}
	for k, v := range writeRoots {
		wr[k] = append([]string(nil), v...)
	}
	s := &Session{
		LocalURL:            "http://127.0.0.1:8645",
		Nodes:               append([]agent.NodeInfo(nil), nodes...),
		WriteRoots:          wr,
		ConflictHold:        map[string]time.Time{},
		ViewMode:            ViewList,
		SortKey:             SortName,
		SortAsc:             true,
		DetailsOpen:         true,
		SidebarWidth:        DefaultSidebarWidth,
		DetailsWidth:        DefaultDetailsWidth,
		CloseAction:         CloseActionAsk,
		Theme:               ThemePrefSystem,
		Density:             DensityPrefAuto,
		SyncSidebarTree:     false,
		AutoEnterSingleRoot: true,
		ctx:                 ctx,
		cancel:              cancel,
		nowFn:               time.Now,
		skipPersist:         true,
	}
	s.initSidebarTree()
	s.noteNodesUpdated()
	tab := s.newTabRecord("", "")
	s.Tabs = []*Tab{tab}
	if len(nodes) > 0 {
		s.SelectNode(nodes[0].ID)
	}
	return s
}

func (s *Session) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Session) SetWake(fn func()) {
	s.inboxMu.Lock()
	s.wakeFn = fn
	s.inboxMu.Unlock()
}

// DrainPending runs queued background UI updates on the caller's thread.
func (s *Session) DrainPending() int {
	s.inboxMu.Lock()
	items := s.inbox
	s.inbox = nil
	s.inboxMu.Unlock()
	for _, fn := range items {
		fn()
	}
	return len(items)
}

// WaitIdle waits until background HTTP calls complete and drains their updates.
// Intended for tests using ui.NewTester.
func (s *Session) WaitIdle(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		s.DrainPending()
		if s.pendingOps.Load() == 0 {
			s.DrainPending()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %d async ops", s.pendingOps.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *Session) enqueueUI(fn func()) {
	s.inboxMu.Lock()
	s.inbox = append(s.inbox, fn)
	wake := s.wakeFn
	s.inboxMu.Unlock()
	if wake != nil {
		wake()
	}
}

func (s *Session) launchAsync(work func() func()) {
	if s.Client == nil {
		return
	}
	s.pendingOps.Add(1)
	go func() {
		defer s.pendingOps.Add(-1)
		if s.ctx.Err() != nil {
			return
		}
		uiUpdate := work()
		if uiUpdate != nil && s.ctx.Err() == nil {
			s.enqueueUI(uiUpdate)
		}
	}()
}

func (s *Session) newTabRecord(node, path string) *Tab {
	s.nextTabID++
	t := &Tab{
		ID:          s.nextTabID,
		Node:        node,
		Path:        path,
		Selected:    map[string]bool{},
		CachedPaths: map[string]string{},
	}
	if path != "" {
		t.History = []string{path}
	}
	return t
}

// Active returns the currently selected tab, initializing one if needed.
func (s *Session) Active() *Tab {
	if len(s.Tabs) == 0 {
		s.Tabs = []*Tab{s.newTabRecord("", "")}
		s.ActiveIdx = 0
	}
	if s.ActiveIdx < 0 || s.ActiveIdx >= len(s.Tabs) {
		s.ActiveIdx = 0
	}
	return s.Tabs[s.ActiveIdx]
}

// TabLabel returns a concise human label for a tab.
func (s *Session) TabLabel(t *Tab) string {
	if t == nil || t.Node == "" {
		return "新标签页"
	}
	nodeLabel := t.Node
	if n, ok := s.NodeByID(t.Node); ok && n.Name != "" {
		nodeLabel = n.Name
	}
	if t.Path == "" {
		return nodeLabel
	}
	base := BaseRemote(t.Path)
	if base == "" {
		return nodeLabel
	}
	return nodeLabel + " · " + base
}

func (s *Session) NodeByID(id string) (agent.NodeInfo, bool) {
	for _, n := range s.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return agent.NodeInfo{}, false
}

func (s *Session) ActiveNode() (agent.NodeInfo, bool) {
	return s.NodeByID(s.Active().Node)
}

// CanWriteActive checks whether the active tab's current folder accepts writes.
func (s *Session) CanWriteActive() bool {
	tab := s.Active()
	return s.CanWritePath(tab.Node, tab.Path)
}

// CanWritePath checks whether nodeID:remotePath accepts writes under the known roots.
func (s *Session) CanWritePath(nodeID, remotePath string) bool {
	if s.Offline || nodeID == "" || remotePath == "" {
		return false
	}
	n, ok := s.NodeByID(nodeID)
	if !ok || n.ReadOnly || !n.AgentOK {
		return false
	}
	if roots, ok := s.WriteRoots[n.ID]; ok {
		return PathIsUnderAny(remotePath, roots)
	}
	return PathIsUnderAny(remotePath, n.ReadRoots)
}

func (s *Session) CanGoBack() bool {
	t := s.Active()
	return t.HistIdx > 0 && len(t.History) > 1
}

func (s *Session) CanGoForward() bool {
	t := s.Active()
	return t.HistIdx+1 < len(t.History)
}

func (s *Session) CanGoUp() bool {
	t := s.Active()
	n, ok := s.NodeByID(t.Node)
	if !ok || t.Path == "" {
		return false
	}
	_, can := ParentRemote(t.Path, n.ReadRoots)
	return can
}

// resetFolderTransientState clears ephemeral folder UI state on any folder or tab change.
func (s *Session) resetFolderTransientState() {
	s.Filter = ""
	s.Editor = EditorState{}
	s.DeleteTargets = nil
	s.ctxMenuOpen = false
	s.CtxTargetPath = ""
	if s.Modal == ModalDelete {
		s.Modal = ModalNone
		s.deleteModalOpen = false
	}
}

func (s *Session) persistSave() {
	if s.skipPersist || s.StateDir == "" {
		return
	}
	savedTabs := make([]SavedTab, 0, len(s.Tabs))
	savedActive := 0
	for i, t := range s.Tabs {
		if t.Node == "" {
			continue
		}
		if i == s.ActiveIdx {
			savedActive = len(savedTabs)
		}
		savedTabs = append(savedTabs, SavedTab{Node: t.Node, Path: t.Path})
	}
	if savedActive >= len(savedTabs) {
		savedActive = max(0, len(savedTabs)-1)
	}
	_ = SavePersist(s.StateDir, UiPersist{
		Theme:               s.Theme,
		Density:             s.Density,
		View:                s.ViewMode,
		Details:             s.DetailsOpen,
		SidebarCollapsed:    s.SidebarCollapsed,
		SidebarWidth:        s.SidebarWidth,
		DetailsWidth:        s.DetailsWidth,
		CloseAction:         s.CloseAction,
		SyncSidebarTree:     s.SyncSidebarTree,
		AutoEnterSingleRoot: boolPtr(s.AutoEnterSingleRoot),
		Tabs:                savedTabs,
		ActiveTab:           savedActive,
		SortKey:             s.SortKey,
		SortAsc:             s.SortAsc,
	})
}

func (s *Session) syncSidebarAfterNav() {
	if s.SyncSidebarTree {
		s.SyncSidebarToActive()
	}
}

func (s *Session) SetStatus(msg string, isErr bool) {
	s.StatusMsg = msg
	s.StatusIsErr = isErr
}

// SyncOverlays keeps Modal enum and persistent *bool flags synchronized after a frame.
func (s *Session) SyncOverlays() {
	if s.ActiveConflict() != nil {
		// Conflict dialog is strictly non-dismissible until user picks Skip/Overwrite/Cancel.
		s.conflictModalOpen = true
		return
	}
	s.conflictModalOpen = false
	switch s.Modal {
	case ModalSettings:
		if !s.settingsModalOpen {
			s.CancelSettings()
		}
	case ModalDelete:
		if !s.deleteModalOpen {
			s.Modal = ModalNone
			s.DeleteTargets = nil
		}
	case ModalDaemon:
		if !s.daemonModalOpen {
			s.Modal = ModalNone
		}
	case ModalClosePrompt:
		if !s.closeModalOpen {
			s.CancelClosePrompt()
		}
	case ModalDocEdit:
		if !s.docModalOpen {
			s.RequestCloseDocEditor()
		}
	}
}

// NewTab opens an empty picker tab (Scheme 1) so the user can choose any device.
func (s *Session) NewTab() {
	s.initialNodeBound = true
	nt := s.newTabRecord("", "")
	s.Tabs = append(s.Tabs, nt)
	s.ActiveIdx = len(s.Tabs) - 1
	s.resetFolderTransientState()
	s.SetStatus("", false)
	s.persistSave()
}

// OpenNodeInNewTab opens nodeID (optionally rooted at path) directly in a new active tab.
func (s *Session) OpenNodeInNewTab(nodeID, path string) bool {
	n, ok := s.NodeByID(nodeID)
	if !ok {
		return false
	}
	targetPath := ""
	if strings.TrimSpace(path) != "" {
		canon := CanonicalizeRemote(path)
		if canon == "" || (len(n.ReadRoots) > 0 && !PathIsUnderAny(canon, n.ReadRoots)) {
			s.SetStatus("该路径不在共享目录中", true)
			return false
		}
		targetPath = canon
	} else if len(n.ReadRoots) > 0 {
		targetPath = CanonicalizeRemote(n.ReadRoots[0])
	}

	s.initialNodeBound = true
	nt := s.newTabRecord(nodeID, targetPath)
	s.Tabs = append(s.Tabs, nt)
	s.ActiveIdx = len(s.Tabs) - 1
	s.resetFolderTransientState()
	s.SetStatus("", false)
	if targetPath != "" {
		s.loadActiveListing()
	} else {
		nt.Err = "该节点尚未配置共享目录"
	}
	s.loadNodeInfo(nodeID)
	s.syncSidebarAfterNav()
	s.persistSave()
	return true
}

// SwitchTab activates tab idx.
func (s *Session) SwitchTab(idx int) {
	if idx < 0 || idx >= len(s.Tabs) || idx == s.ActiveIdx {
		return
	}
	s.ActiveIdx = idx
	s.resetFolderTransientState()
	t := s.Active()
	if t.Node != "" && t.Path != "" && len(t.Entries) == 0 && !t.Loading {
		s.loadActiveListing()
		s.loadNodeInfo(t.Node)
	}
	s.syncSidebarAfterNav()
	s.persistSave()
}

// CloseTabByID closes the tab identified by id.
func (s *Session) CloseTabByID(id uint64) {
	for i, t := range s.Tabs {
		if t.ID == id {
			s.CloseTab(i)
			return
		}
	}
}

// CloseTab closes tab idx (keeping at least one tab open) and pushes to reopen stack.
func (s *Session) CloseTab(idx int) {
	if idx < 0 || idx >= len(s.Tabs) {
		return
	}
	closing := s.Tabs[idx]
	if closing.Node != "" && len(s.Tabs) > 1 {
		s.ClosedTabs = append(s.ClosedTabs, SavedTab{Node: closing.Node, Path: closing.Path})
		if len(s.ClosedTabs) > 20 {
			s.ClosedTabs = s.ClosedTabs[len(s.ClosedTabs)-20:]
		}
	}
	if len(s.Tabs) == 1 {
		// Replace single tab with clean reset on first available node root.
		if closing.Node != "" {
			s.ClosedTabs = append(s.ClosedTabs, SavedTab{Node: closing.Node, Path: closing.Path})
		}
		var node, path string
		if len(s.Nodes) > 0 {
			node = s.Nodes[0].ID
			if len(s.Nodes[0].ReadRoots) > 0 {
				path = CanonicalizeRemote(s.Nodes[0].ReadRoots[0])
			}
		}
		s.Tabs[0] = s.newTabRecord(node, path)
		s.ActiveIdx = 0
		s.resetFolderTransientState()
		if node != "" && path != "" {
			s.loadActiveListing()
			s.loadNodeInfo(node)
		}
		s.syncSidebarAfterNav()
		s.persistSave()
		return
	}

	s.Tabs = append(s.Tabs[:idx], s.Tabs[idx+1:]...)
	if idx < s.ActiveIdx {
		s.ActiveIdx--
	} else if s.ActiveIdx >= len(s.Tabs) {
		s.ActiveIdx = len(s.Tabs) - 1
	}
	s.resetFolderTransientState()
	t := s.Active()
	if t.Node != "" && t.Path != "" && len(t.Entries) == 0 && !t.Loading {
		s.loadActiveListing()
		s.loadNodeInfo(t.Node)
	}
	s.syncSidebarAfterNav()
	s.persistSave()
}

// ReopenClosedTab restores the most recently closed tab (Mod+Shift+T).
func (s *Session) ReopenClosedTab() bool {
	if len(s.ClosedTabs) == 0 {
		return false
	}
	last := s.ClosedTabs[len(s.ClosedTabs)-1]
	s.ClosedTabs = s.ClosedTabs[:len(s.ClosedTabs)-1]

	path := CanonicalizeRemote(last.Path)
	if n, ok := s.NodeByID(last.Node); ok && len(n.ReadRoots) > 0 && !PathIsUnderAny(path, n.ReadRoots) {
		path = CanonicalizeRemote(n.ReadRoots[0])
	}
	s.initialNodeBound = true
	nt := s.newTabRecord(last.Node, path)
	s.Tabs = append(s.Tabs, nt)
	s.ActiveIdx = len(s.Tabs) - 1
	s.resetFolderTransientState()
	s.loadActiveListing()
	s.loadNodeInfo(last.Node)
	s.syncSidebarAfterNav()
	s.persistSave()
	return true
}

// SelectNode switches the active tab to nodeID's first shared root.
func (s *Session) SelectNode(nodeID string) {
	tab := s.Active()
	n, ok := s.NodeByID(nodeID)
	root := ""
	if ok && len(n.ReadRoots) > 0 {
		root = CanonicalizeRemote(n.ReadRoots[0])
	}
	s.initialNodeBound = true
	tab.Node = nodeID
	tab.Path = root
	tab.Entries = nil
	tab.Selected = map[string]bool{}
	tab.SelectAnchor = ""
	tab.Preview = nil
	tab.Err = ""
	tab.ListGen++
	tab.PreviewGen++
	if root != "" {
		tab.History = []string{root}
	} else {
		tab.History = nil
		tab.Err = "该节点尚未配置共享目录"
	}
	tab.HistIdx = 0
	s.resetFolderTransientState()
	s.SetStatus("", false)
	if root != "" {
		s.loadActiveListing()
	}
	s.loadNodeInfo(nodeID)
	s.syncSidebarAfterNav()
	s.persistSave()
}

// SelectNodePath switches the active tab directly to a folder on nodeID, preserving
// Back/Forward history when remaining on the same node.
func (s *Session) SelectNodePath(nodeID, path string) error {
	canon := CanonicalizeRemote(path)
	if canon == "" {
		s.SelectNode(nodeID)
		return nil
	}
	tab := s.Active()
	if tab.Node == nodeID && tab.Path != "" {
		return s.Navigate(canon)
	}
	n, ok := s.NodeByID(nodeID)
	if !ok {
		return errors.New("当前节点不可用")
	}
	if len(n.ReadRoots) > 0 && !PathIsUnderAny(canon, n.ReadRoots) {
		msg := "该路径不在共享目录中"
		s.SetStatus(msg, true)
		return errors.New(msg)
	}
	s.initialNodeBound = true
	tab.Node = nodeID
	tab.Path = canon
	tab.Entries = nil
	tab.Selected = map[string]bool{}
	tab.SelectAnchor = ""
	tab.Preview = nil
	tab.Err = ""
	tab.ListGen++
	tab.PreviewGen++
	tab.History = []string{canon}
	tab.HistIdx = 0
	s.resetFolderTransientState()
	s.SetStatus("", false)
	s.loadActiveListing()
	s.loadNodeInfo(nodeID)
	s.syncSidebarAfterNav()
	s.persistSave()
	return nil
}

// Navigate moves the active tab to path inside the current node's readRoots.
func (s *Session) Navigate(path string) error {
	tab := s.Active()
	if tab.Node == "" {
		return errors.New("请先选择一个设备节点")
	}
	n, ok := s.NodeByID(tab.Node)
	if !ok {
		return errors.New("当前节点不可用")
	}
	canon := CanonicalizeRemote(path)
	if canon == "" || (len(n.ReadRoots) > 0 && !PathIsUnderAny(canon, n.ReadRoots)) {
		msg := "该路径不在共享目录中"
		s.SetStatus(msg, true)
		return errors.New(msg)
	}

	s.resetFolderTransientState()
	s.SetStatus("", false)
	if canon != tab.Path {
		if len(tab.History) == 0 || tab.HistIdx < 0 || tab.HistIdx >= len(tab.History) {
			tab.History = []string{canon}
			tab.HistIdx = 0
		} else {
			tab.History = append(tab.History[:tab.HistIdx+1], canon)
			tab.HistIdx = len(tab.History) - 1
		}
		tab.Path = canon
		tab.Entries = nil
	}
	tab.Selected = map[string]bool{}
	tab.SelectAnchor = ""
	tab.Preview = nil
	tab.Err = ""
	tab.ListGen++
	tab.PreviewGen++
	s.loadActiveListing()
	s.syncSidebarAfterNav()
	s.persistSave()
	return nil
}

func (s *Session) GoBack() {
	if !s.CanGoBack() {
		return
	}
	tab := s.Active()
	tab.HistIdx--
	tab.Path = tab.History[tab.HistIdx]
	tab.Entries = nil
	tab.Selected = map[string]bool{}
	tab.SelectAnchor = ""
	tab.Preview = nil
	tab.Err = ""
	tab.ListGen++
	tab.PreviewGen++
	s.resetFolderTransientState()
	s.SetStatus("", false)
	s.loadActiveListing()
	s.syncSidebarAfterNav()
	s.persistSave()
}

func (s *Session) GoForward() {
	if !s.CanGoForward() {
		return
	}
	tab := s.Active()
	tab.HistIdx++
	tab.Path = tab.History[tab.HistIdx]
	tab.Entries = nil
	tab.Selected = map[string]bool{}
	tab.SelectAnchor = ""
	tab.Preview = nil
	tab.Err = ""
	tab.ListGen++
	tab.PreviewGen++
	s.resetFolderTransientState()
	s.SetStatus("", false)
	s.loadActiveListing()
	s.syncSidebarAfterNav()
	s.persistSave()
}

func (s *Session) GoUp() {
	tab := s.Active()
	n, ok := s.NodeByID(tab.Node)
	if !ok || tab.Path == "" {
		return
	}
	parent, can := ParentRemote(tab.Path, n.ReadRoots)
	if !can {
		return
	}
	_ = s.Navigate(parent)
}

func (s *Session) Refresh() {
	tab := s.Active()
	if tab.Node != "" && tab.Path != "" {
		s.loadActiveListing()
	}
	if s.Client != nil && !s.Offline {
		s.RefreshAll()
	}
}

func (s *Session) ToggleSort(col SortCol) {
	if s.SortKey == col {
		s.SortAsc = !s.SortAsc
	} else {
		s.SortKey = col
		s.SortAsc = true
	}
	s.persistSave()
}

func (s *Session) SetViewMode(mode ViewMode) {
	s.ViewMode = mode
	s.persistSave()
}

func (s *Session) ToggleDetails() {
	s.DetailsOpen = !s.DetailsOpen
	s.persistSave()
}

func (s *Session) ToggleSidebar() {
	s.SidebarCollapsed = !s.SidebarCollapsed
	s.persistSave()
}

// SortedEntries returns the filtered and sorted directory entries for the active tab.
func (s *Session) SortedEntries() []agent.Entry {
	tab := s.Active()
	q := strings.ToLower(strings.TrimSpace(s.Filter))
	out := make([]agent.Entry, 0, len(tab.Entries))
	for _, e := range tab.Entries {
		if q != "" && !strings.Contains(strings.ToLower(e.Name), q) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		var cmp int
		switch s.SortKey {
		case SortSize:
			if a.Size < b.Size {
				cmp = -1
			} else if a.Size > b.Size {
				cmp = 1
			}
		case SortTime:
			if a.ModTime.Before(b.ModTime) {
				cmp = -1
			} else if a.ModTime.After(b.ModTime) {
				cmp = 1
			}
		}
		if cmp == 0 {
			an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
			if an < bn {
				cmp = -1
			} else if an > bn {
				cmp = 1
			} else if a.Name < b.Name {
				cmp = -1
			} else if a.Name > b.Name {
				cmp = 1
			}
		}
		if !s.SortAsc {
			cmp = -cmp
		}
		return cmp < 0
	})
	return out
}

// SelectEntry handles single, Ctrl/Cmd toggle, and Shift range selection on path.
func (s *Session) SelectEntry(path string, ctrl, shift bool) {
	if s.Editor.Kind == EditorRename && s.Editor.TargetPath != path {
		s.CommitOrCancelRenameOnBlur()
	}
	tab := s.Active()
	if tab.Selected == nil {
		tab.Selected = map[string]bool{}
	}
	if shift && tab.SelectAnchor != "" {
		items := s.SortedEntries()
		anchorIdx, targetIdx := -1, -1
		for i, e := range items {
			if e.Path == tab.SelectAnchor {
				anchorIdx = i
			}
			if e.Path == path {
				targetIdx = i
			}
		}
		if anchorIdx >= 0 && targetIdx >= 0 {
			if !ctrl {
				tab.Selected = map[string]bool{}
			}
			lo, hi := anchorIdx, targetIdx
			if lo > hi {
				lo, hi = hi, lo
			}
			for i := lo; i <= hi; i++ {
				tab.Selected[items[i].Path] = true
			}
		} else {
			tab.Selected = map[string]bool{path: true}
			tab.SelectAnchor = path
		}
	} else if ctrl {
		if tab.Selected[path] {
			delete(tab.Selected, path)
		} else {
			tab.Selected[path] = true
			tab.SelectAnchor = path
		}
	} else {
		tab.Selected = map[string]bool{path: true}
		tab.SelectAnchor = path
	}
	s.syncPreviewWithSelection()
}

func (s *Session) ClearSelection() {
	tab := s.Active()
	tab.Selected = map[string]bool{}
	tab.SelectAnchor = ""
	tab.Preview = nil
	tab.PreviewGen++
}

// ApplyMarqueeSelection replaces the active tab's selection from a marquee computation, keeping only entries in the active listing.
func (s *Session) ApplyMarqueeSelection(selected map[string]bool, anchor string) {
	if s.Editor.Kind == EditorRename {
		s.CommitOrCancelRenameOnBlur()
	}
	tab := s.Active()
	valid := make(map[string]bool, len(tab.Entries))
	for _, e := range tab.Entries {
		valid[e.Path] = true
	}
	clean := make(map[string]bool, len(selected))
	for p, ok := range selected {
		if ok && valid[p] {
			clean[p] = true
		}
	}
	if anchor != "" && !valid[anchor] {
		anchor = ""
	}
	if len(clean) == len(tab.Selected) && tab.SelectAnchor == anchor {
		same := true
		for p := range clean {
			if !tab.Selected[p] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	tab.Selected = clean
	tab.SelectAnchor = anchor
	s.syncPreviewWithSelection()
}

func (s *Session) SelectAll() {
	tab := s.Active()
	tab.Selected = map[string]bool{}
	items := s.SortedEntries()
	for _, e := range items {
		tab.Selected[e.Path] = true
	}
	if len(items) > 0 {
		tab.SelectAnchor = items[0].Path
	}
	s.syncPreviewWithSelection()
}

func (s *Session) SelectedEntries() []agent.Entry {
	tab := s.Active()
	if len(tab.Selected) == 0 {
		return nil
	}
	out := make([]agent.Entry, 0, len(tab.Selected))
	for _, e := range tab.Entries {
		if tab.Selected[e.Path] {
			out = append(out, e)
		}
	}
	return out
}

func (s *Session) ActivateEntry(e agent.Entry) {
	if e.IsDir {
		_ = s.Navigate(e.Path)
		return
	}
	s.SelectEntry(e.Path, false, false)
	if s.DetailsOpen {
		return
	}
	s.DetailsOpen = true
	s.persistSave()
}

func (s *Session) syncPreviewWithSelection() {
	tab := s.Active()
	sel := s.SelectedEntries()
	if len(sel) != 1 {
		tab.Preview = nil
		tab.PreviewGen++
		return
	}
	e := sel[0]
	if tab.Preview != nil && tab.Preview.Node == tab.Node && tab.Preview.Path == e.Path && !tab.Preview.Loading {
		return
	}
	tab.PreviewGen++
	gen := tab.PreviewGen
	if e.IsDir {
		tab.Preview = &EntryPreview{
			Node: tab.Node,
			Path: e.Path,
			Name: e.Name,
			Kind: "dir",
			Size: e.Size,
		}
		return
	}
	kind := agent.PreviewKind(e.Name)
	tab.Preview = &EntryPreview{
		Node:    tab.Node,
		Path:    e.Path,
		Name:    e.Name,
		Kind:    kind,
		Size:    e.Size,
		Loading: s.Client != nil,
	}
	if s.Client == nil {
		return
	}
	tabID := tab.ID
	nodeID := tab.Node
	filePath := e.Path
	s.launchAsync(func() func() {
		if kind == "image" {
			data, err := s.Client.Open(s.ctx, nodeID, filePath, 8<<20)
			var img image.Image
			if err == nil && len(data) > 0 {
				if decoded, _, derr := image.Decode(bytes.NewReader(data)); derr == nil {
					img = decoded
				}
			}
			return func() {
				t := s.findTab(tabID)
				if t == nil || t.PreviewGen != gen || t.Node != nodeID {
					return
				}
				t.Preview = &EntryPreview{
					Node:       nodeID,
					Path:       filePath,
					Name:       e.Name,
					Kind:       "image",
					Size:       e.Size,
					ImageBytes: data,
					Image:      img,
					Loading:    false,
					Err:        errStr(err),
				}
			}
		}
		prev, err := s.Client.Preview(s.ctx, nodeID, filePath)
		return func() {
			t := s.findTab(tabID)
			if t == nil || t.PreviewGen != gen || t.Node != nodeID {
				return
			}
			t.Preview = &EntryPreview{
				Node:      nodeID,
				Path:      filePath,
				Name:      e.Name,
				Kind:      prev.Kind,
				Size:      prev.Size,
				Mime:      prev.Mime,
				Text:      prev.Text,
				Truncated: prev.Truncated,
				Loading:   false,
				Err:       errStr(err),
			}
			if prev.Kind == "" {
				t.Preview.Kind = kind
			}
		}
	})
	// Pre-warm drag-out cache for remote files when single file is selected.
	if n, ok := s.NodeByID(nodeID); ok && !n.Self {
		s.launchAsync(func() func() {
			res, err := s.Client.Cache(s.ctx, nodeID, filePath)
			if err != nil || res.LocalPath == "" {
				return nil
			}
			return func() {
				t := s.findTab(tabID)
				if t != nil {
					t.CachedPaths[filePath] = res.LocalPath
				}
			}
		})
	}
}

func (s *Session) BeginMkdir() bool {
	if !s.CanWriteActive() {
		return false
	}
	tab := s.Active()
	s.Modal = ModalNone
	s.ctxMenuOpen = false
	s.Editor = EditorState{
		Kind:      EditorMkdir,
		Text:      UniqueFolderName(tab.Entries),
		NeedFocus: true,
	}
	return true
}

func (s *Session) BeginRename() bool {
	if !s.CanWriteActive() {
		return false
	}
	sel := s.SelectedEntries()
	if len(sel) != 1 {
		return false
	}
	s.Modal = ModalNone
	s.ctxMenuOpen = false
	s.Editor = EditorState{
		Kind:       EditorRename,
		Text:       sel[0].Name,
		TargetPath: sel[0].Path,
		NeedFocus:  true,
	}
	return true
}

func (s *Session) BeginGoto() bool {
	tab := s.Active()
	if tab.Node == "" {
		return false
	}
	s.ctxMenuOpen = false
	s.Editor = EditorState{
		Kind:      EditorGoto,
		Text:      tab.Path,
		NeedFocus: true,
	}
	return true
}

func (s *Session) CancelEditor() {
	s.Editor = EditorState{}
}

// CommitOrCancelRenameOnBlur finalizes or discards an active in-place rename when focus moves away.
func (s *Session) CommitOrCancelRenameOnBlur() {
	if s.Editor.Kind != EditorRename {
		return
	}
	if strings.TrimSpace(s.Editor.Text) == "" {
		s.CancelEditor()
		return
	}
	if err := s.CommitEditor(); err != nil {
		s.SetStatus("重命名失败: "+err.Error(), true)
		s.CancelEditor()
	}
}

func (s *Session) CommitEditor() error {
	tab := s.Active()
	switch s.Editor.Kind {
	case EditorNone:
		return nil
	case EditorGoto:
		target := s.Editor.Text
		if err := s.Navigate(target); err != nil {
			s.Editor.Err = err.Error()
			return err
		}
		return nil
	case EditorMkdir:
		if !s.CanWriteActive() {
			err := errors.New("当前目录只读，无法新建文件夹")
			s.Editor.Err = err.Error()
			return err
		}
		clean, err := ValidateName(s.Editor.Text)
		if err != nil {
			s.Editor.Err = err.Error()
			return err
		}
		newPath := JoinRemote(tab.Path, clean)
		nodeID, dir := tab.Node, tab.Path
		s.Editor = EditorState{}
		if s.Client == nil {
			tab.Entries = append(tab.Entries, agent.Entry{Name: clean, Path: newPath, IsDir: true, ModTime: s.nowFn()})
			s.SetStatus("已创建文件夹 "+clean, false)
			return nil
		}
		s.launchAsync(func() func() {
			err := s.Client.Mkdir(s.ctx, nodeID, newPath)
			return func() {
				if err != nil {
					s.SetStatus("创建失败: "+err.Error(), true)
					return
				}
				s.SetStatus("已创建 "+clean, false)
				if s.Active().Node == nodeID && s.Active().Path == dir {
					s.loadActiveListing()
				}
			}
		})
		return nil
	case EditorRename:
		if !s.CanWriteActive() {
			err := errors.New("当前目录只读，无法重命名")
			s.Editor.Err = err.Error()
			return err
		}
		clean, err := ValidateName(s.Editor.Text)
		if err != nil {
			s.Editor.Err = err.Error()
			return err
		}
		oldPath := s.Editor.TargetPath
		newPath := JoinRemote(tab.Path, clean)
		s.Editor = EditorState{}
		if newPath == oldPath {
			return nil
		}
		nodeID, dir := tab.Node, tab.Path
		if s.Client == nil {
			for i := range tab.Entries {
				if tab.Entries[i].Path == oldPath {
					tab.Entries[i].Name = clean
					tab.Entries[i].Path = newPath
				}
			}
			if tab.Selected[oldPath] {
				delete(tab.Selected, oldPath)
				tab.Selected[newPath] = true
			}
			if tab.SelectAnchor == oldPath {
				tab.SelectAnchor = newPath
			}
			s.SetStatus("已重命名为 "+clean, false)
			return nil
		}
		s.launchAsync(func() func() {
			err := s.Client.Rename(s.ctx, nodeID, oldPath, newPath)
			return func() {
				if err != nil {
					s.SetStatus("重命名失败: "+err.Error(), true)
					return
				}
				s.SetStatus("已重命名为 "+clean, false)
				if t := s.Active(); t.Node == nodeID && t.Path == dir {
					if t.Selected[oldPath] {
						delete(t.Selected, oldPath)
						t.Selected[newPath] = true
					}
					if t.SelectAnchor == oldPath {
						t.SelectAnchor = newPath
					}
					s.loadActiveListing()
				}
			}
		})
		return nil
	}
	return nil
}

func (s *Session) BeginDelete() bool {
	if !s.CanWriteActive() {
		return false
	}
	sel := s.SelectedEntries()
	if len(sel) == 0 {
		return false
	}
	s.ctxMenuOpen = false
	s.DeleteTargets = sel
	s.Modal = ModalDelete
	s.deleteModalOpen = true
	return true
}

func (s *Session) CancelDelete() {
	if s.Modal == ModalDelete {
		s.Modal = ModalNone
	}
	s.deleteModalOpen = false
	s.DeleteTargets = nil
}

func (s *Session) ConfirmDelete() {
	if !s.CanWriteActive() {
		s.CancelDelete()
		return
	}
	targets := append([]agent.Entry(nil), s.DeleteTargets...)
	s.CancelDelete()
	if len(targets) == 0 {
		return
	}
	tab := s.Active()
	nodeID, dir := tab.Node, tab.Path
	if s.Client == nil {
		drop := map[string]bool{}
		for _, tgt := range targets {
			drop[tgt.Path] = true
			delete(tab.Selected, tgt.Path)
		}
		kept := tab.Entries[:0]
		for _, e := range tab.Entries {
			if !drop[e.Path] {
				kept = append(kept, e)
			}
		}
		tab.Entries = kept
		tab.Preview = nil
		s.SetStatus(fmt.Sprintf("已删除 %d 项", len(targets)), false)
		return
	}
	s.launchAsync(func() func() {
		var firstErr error
		deleted := 0
		for _, tgt := range targets {
			if err := s.Client.Delete(s.ctx, nodeID, tgt.Path); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				deleted++
			}
		}
		return func() {
			if firstErr != nil {
				s.SetStatus("删除失败: "+firstErr.Error(), true)
			} else {
				s.SetStatus(fmt.Sprintf("已删除 %d 项", deleted), false)
			}
			if t := s.Active(); t.Node == nodeID && t.Path == dir {
				t.Selected = map[string]bool{}
				t.Preview = nil
				s.loadActiveListing()
			}
		}
	})
}

func (s *Session) CopySelected() bool {
	sel := s.SelectedEntries()
	if len(sel) == 0 {
		return false
	}
	tab := s.Active()
	paths := make([]string, 0, len(sel))
	for _, e := range sel {
		paths = append(paths, e.Path)
	}
	s.Clipboard = &ClipItem{Node: tab.Node, Paths: paths}
	s.SetStatus(fmt.Sprintf("已复制 %d 项", len(paths)), false)
	return true
}

func (s *Session) Paste() bool {
	if s.Clipboard == nil || len(s.Clipboard.Paths) == 0 || !s.CanWriteActive() {
		return false
	}
	tab := s.Active()
	clip := *s.Clipboard
	mode := "push"
	if n, ok := s.NodeByID(tab.Node); ok && n.Self && clip.Node != tab.Node {
		mode = "pull"
	}
	if s.Client == nil {
		s.SetStatus(fmt.Sprintf("已开始粘贴 %d 项到 %s", len(clip.Paths), tab.Node), false)
		return true
	}
	toNode, toDir := tab.Node, tab.Path
	s.launchAsync(func() func() {
		var firstErr error
		started := 0
		for _, src := range clip.Paths {
			destPath := JoinRemote(toDir, BaseRemote(src))
			if _, err := s.Client.StartTransfer(s.ctx, agent.TransferRequest{
				Mode:     mode,
				FromNode: clip.Node,
				FromPath: src,
				ToNode:   toNode,
				ToPath:   destPath,
			}); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				started++
			}
		}
		transfers, _ := s.Client.Transfers(s.ctx)
		return func() {
			if transfers != nil {
				s.Transfers = transfers
			}
			if firstErr != nil {
				s.SetStatus("粘贴失败: "+firstErr.Error(), true)
			} else {
				s.SetStatus(fmt.Sprintf("已提交 %d 项传输", started), false)
			}
			s.loadActiveListing()
		}
	})
	return true
}

// HandleDroppedFiles imports local OS paths onto the active writable directory.
func (s *Session) HandleDroppedFiles(paths []string) {
	if len(paths) == 0 {
		return
	}
	if !s.CanWriteActive() {
		s.SetStatus("当前目录只读，无法拖入文件", true)
		return
	}
	tab := s.Active()
	nodeID, dir := tab.Node, tab.Path
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		p = FileURIToPath(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 || s.Client == nil {
		return
	}
	s.launchAsync(func() func() {
		_, err := s.Client.Import(s.ctx, agent.ImportRequest{
			Node:    nodeID,
			DestDir: dir,
			Paths:   cleaned,
		})
		jobs, _ := s.Client.Jobs(s.ctx)
		return func() {
			if jobs != nil {
				s.Jobs = jobs
			}
			if err != nil {
				s.SetStatus("拖入失败: "+err.Error(), true)
			} else {
				s.SetStatus(fmt.Sprintf("正在导入 %d 个文件", len(cleaned)), false)
			}
			s.loadActiveListing()
		}
	})
}

// PrepareDragFiles returns local OS file paths ready for transfer.FileData drag-out.
func (s *Session) PrepareDragFiles() []string {
	tab := s.Active()
	n, ok := s.NodeByID(tab.Node)
	if !ok {
		return nil
	}
	sel := s.SelectedEntries()
	if len(sel) == 0 {
		return nil
	}
	out := make([]string, 0, len(sel))
	for _, e := range sel {
		if e.IsDir {
			continue
		}
		if n.Self {
			out = append(out, filepath.FromSlash(e.Path))
		} else if cached := tab.CachedPaths[e.Path]; cached != "" {
			out = append(out, cached)
		}
	}
	return out
}

func (s *Session) OpenSelectedInOS() {
	tab := s.Active()
	sel := s.SelectedEntries()
	if len(sel) != 1 || sel[0].IsDir || s.Client == nil {
		return
	}
	e := sel[0]
	n, ok := s.NodeByID(tab.Node)
	if !ok {
		return
	}
	s.launchAsync(func() func() {
		localPath := filepath.FromSlash(e.Path)
		if !n.Self {
			res, err := s.Client.Cache(s.ctx, tab.Node, e.Path)
			if err != nil {
				return func() { s.SetStatus("缓存失败: "+err.Error(), true) }
			}
			localPath = res.LocalPath
		}
		err := s.Client.Reveal(s.ctx, localPath)
		return func() {
			if err != nil {
				s.SetStatus("打开失败: "+err.Error(), true)
			}
		}
	})
}

// ActiveConflict returns the highest-priority non-suppressed file conflict.
func (s *Session) ActiveConflict() *ConflictPrompt {
	now := s.nowFn()
	for _, tr := range s.Transfers {
		if tr.Job.Status == "conflict" && tr.Job.Conflict != nil {
			if until, held := s.ConflictHold[tr.ID]; held && now.Before(until) {
				continue
			}
			return &ConflictPrompt{
				IsJob:    false,
				ID:       tr.ID,
				Subtitle: fmt.Sprintf("%s → %s", tr.FromNode, tr.ToNode),
				File:     *tr.Job.Conflict,
			}
		}
	}
	for _, j := range s.Jobs {
		if j.Status == "conflict" && j.Conflict != nil {
			key := "job:" + j.ID
			if until, held := s.ConflictHold[key]; held && now.Before(until) {
				continue
			}
			return &ConflictPrompt{
				IsJob:    true,
				ID:       j.ID,
				Subtitle: "本地拖入导入",
				File:     *j.Conflict,
			}
		}
	}
	return nil
}

// AnswerConflict resolves the active conflict with "overwrite", "skip", or "cancel".
func (s *Session) AnswerConflict(action string) {
	cp := s.ActiveConflict()
	if cp == nil {
		return
	}
	holdKey := cp.ID
	if cp.IsJob {
		holdKey = "job:" + cp.ID
	}
	s.ConflictHold[holdKey] = s.nowFn().Add(2 * time.Second)
	applyAll := s.ConflictApplyAll

	if s.Client == nil {
		// Synchronous test update
		for i := range s.Transfers {
			if !cp.IsJob && s.Transfers[i].ID == cp.ID {
				s.Transfers[i].Job.Conflict = nil
				if action == "cancel" {
					s.Transfers[i].Job.Status = "canceled"
				} else {
					s.Transfers[i].Job.Status = "running"
				}
			}
		}
		for i := range s.Jobs {
			if cp.IsJob && s.Jobs[i].ID == cp.ID {
				s.Jobs[i].Conflict = nil
				s.Jobs[i].Status = "running"
			}
		}
		if s.ActiveConflict() == nil {
			s.ConflictApplyAll = false
			s.conflictModalOpen = false
		}
		return
	}

	s.launchAsync(func() func() {
		var err error
		if action == "cancel" && !cp.IsJob {
			_, err = s.Client.CancelTransfer(s.ctx, cp.ID)
		} else if cp.IsJob {
			_, err = s.Client.AnswerJobConflict(s.ctx, cp.ID, action, applyAll)
		} else {
			_, err = s.Client.AnswerTransferConflict(s.ctx, cp.ID, action, applyAll)
		}
		transfers, _ := s.Client.Transfers(s.ctx)
		jobs, _ := s.Client.Jobs(s.ctx)
		return func() {
			if err != nil {
				s.SetStatus("冲突处理失败: "+err.Error(), true)
			}
			if transfers != nil {
				s.Transfers = transfers
			}
			if jobs != nil {
				s.Jobs = jobs
			}
			if s.ActiveConflict() == nil {
				s.ConflictApplyAll = false
				s.conflictModalOpen = false
			}
			s.loadActiveListing()
		}
	})
}

func (s *Session) OpenSettings() {
	s.ctxMenuOpen = false
	var cacheDir string
	if s.Settings != nil {
		cacheDir = s.Settings.CacheDir
	}
	s.SettingsDraft = SettingsDraft{
		CacheDirInput: cacheDir,
	}
	s.Modal = ModalSettings
	s.settingsModalOpen = true
}

func (s *Session) CancelSettings() {
	if s.Modal == ModalSettings {
		s.Modal = ModalNone
	}
	s.settingsModalOpen = false
	s.SettingsDraft = SettingsDraft{}
}

// SetTheme applies the selected theme immediately and persists it.
func (s *Session) SetTheme(pref ThemePref) {
	switch pref {
	case ThemePrefLight, ThemePrefDark, ThemePrefSystem:
	default:
		pref = ThemePrefSystem
	}
	s.Theme = pref
	if s.onThemeChange != nil {
		s.onThemeChange(pref)
	}
	s.persistSave()
}

// SetDensity applies the selected layout density immediately and persists it.
func (s *Session) SetDensity(pref DensityPref) {
	s.Density = sanitizeDensityPref(pref)
	s.persistSave()
}

// SetSidebarWidth updates the expanded sidebar width, clamping to bounds and optionally saving.
func (s *Session) SetSidebarWidth(w float32, persist bool) {
	s.SidebarWidth = ClampSidebarWidth(w)
	if persist {
		s.persistSave()
	}
}

// SetDetailsWidth updates the details pane width, clamping to bounds and optionally saving.
func (s *Session) SetDetailsWidth(w float32, persist bool) {
	s.DetailsWidth = ClampDetailsWidth(w)
	if persist {
		s.persistSave()
	}
}

// SetCloseAction updates the close-window preference immediately and persists it.
func (s *Session) SetCloseAction(pref CloseActionPref) {
	s.CloseAction = sanitizeCloseAction(pref)
	s.persistSave()
}

// SetSyncSidebarTree toggles automatic left-tree expansion to the active folder and persists immediately.
func (s *Session) SetSyncSidebarTree(enabled bool) {
	s.SyncSidebarTree = enabled
	s.persistSave()
	if enabled {
		s.SyncSidebarToActive()
	}
}

// SetAutoEnterSingleRoot toggles single-root flattening in the sidebar tree and persists immediately.
func (s *Session) SetAutoEnterSingleRoot(enabled bool) {
	if s.AutoEnterSingleRoot == enabled {
		return
	}
	s.AutoEnterSingleRoot = enabled
	s.bumpTreeGen()
	s.persistSave()
	if s.SyncSidebarTree {
		s.SyncSidebarToActive()
	}
}

// OpenClosePrompt opens the first-close confirmation dialog with remember enabled by default.
func (s *Session) OpenClosePrompt() {
	s.ctxMenuOpen = false
	s.CloseRemember = true
	s.Modal = ModalClosePrompt
	s.closeModalOpen = true
}

// CancelClosePrompt dismisses the close-window dialog without quitting or hiding.
func (s *Session) CancelClosePrompt() {
	if s.Modal == ModalClosePrompt {
		s.Modal = ModalNone
	}
	s.closeModalOpen = false
}

// ChooseCloseAction resolves the close prompt ("tray" or "quit"), persisting when remembered.
func (s *Session) ChooseCloseAction(action CloseActionPref) {
	action = sanitizeCloseAction(action)
	if s.CloseRemember && (action == CloseActionTray || action == CloseActionQuit) {
		s.CloseAction = action
		s.persistSave()
	}
	if s.Modal == ModalClosePrompt {
		s.Modal = ModalNone
	}
	s.closeModalOpen = false
	switch action {
	case CloseActionTray:
		if s.onHideWindow != nil {
			s.onHideWindow()
		}
	case CloseActionQuit:
		if s.onQuitApp != nil {
			s.onQuitApp()
		}
	}
}

// OnlineNodeCounts returns the count of reachable online peers and total discovered peers.
func (s *Session) OnlineNodeCounts() (online, total int) {
	for _, n := range s.Nodes {
		total++
		if n.Online && n.AgentOK {
			online++
		}
	}
	return online, total
}

// SettingsDirty reports whether text fields in the Settings modal have unsaved edits.
func (s *Session) SettingsDirty() bool {
	cacheDir := strings.TrimSpace(s.SettingsDraft.CacheDirInput)
	currentCache := ""
	if s.Settings != nil {
		currentCache = strings.TrimSpace(s.Settings.CacheDir)
	}
	if cacheDir != "" && cacheDir != currentCache {
		return true
	}
	if s.Settings != nil && s.Settings.Net == "tsnet" && strings.TrimSpace(s.SettingsDraft.AuthKeyInput) != "" {
		return true
	}
	return false
}

// SaveSettings commits pending cache dir (and tsnet authkey if active) and closes the modal.
// When no text fields were modified, it closes cleanly without sending backend RPCs.
func (s *Session) SaveSettings() {
	if s.SettingsDraft.Saving {
		return
	}
	if !s.SettingsDirty() {
		s.CancelSettings()
		return
	}
	authKey := strings.TrimSpace(s.SettingsDraft.AuthKeyInput)
	cacheDir := strings.TrimSpace(s.SettingsDraft.CacheDirInput)

	if s.Client == nil {
		if cacheDir != "" {
			if s.Settings == nil {
				s.Settings = &agent.Settings{}
			}
			s.Settings.CacheDir = cacheDir
		}
		s.SettingsDraft.AuthKeyInput = ""
		s.Modal = ModalNone
		s.settingsModalOpen = false
		s.persistSave()
		return
	}

	s.SettingsDraft.Saving = true
	s.SettingsDraft.Err = ""
	s.launchAsync(func() func() {
		var saveErr error
		var newSettings *agent.Settings
		if authKey != "" {
			if _, err := s.Client.SetAuthKey(s.ctx, authKey); err != nil {
				saveErr = err
			}
		}
		if saveErr == nil && cacheDir != "" && (s.Settings == nil || cacheDir != s.Settings.CacheDir) {
			st, err := s.Client.SetCacheDir(s.ctx, cacheDir)
			if err != nil {
				saveErr = err
			} else {
				newSettings = &st
			}
		}
		if saveErr == nil && newSettings == nil {
			if st, err := s.Client.Settings(s.ctx); err == nil {
				newSettings = &st
			}
		}
		return func() {
			s.SettingsDraft.Saving = false
			if saveErr != nil {
				s.SettingsDraft.Err = saveErr.Error()
				return
			}
			if newSettings != nil {
				s.Settings = newSettings
			}
			s.SettingsDraft.AuthKeyInput = ""
			s.Modal = ModalNone
			s.settingsModalOpen = false
			s.persistSave()
			s.RefreshAll()
		}
	})
}

func (s *Session) ClearStoredAuthKey() {
	if s.Client == nil {
		return
	}
	s.launchAsync(func() func() {
		_, err := s.Client.ClearAuthKey(s.ctx)
		st, _ := s.Client.Settings(s.ctx)
		return func() {
			if err != nil {
				s.SettingsDraft.Err = err.Error()
				return
			}
			s.Settings = &st
			s.SettingsDraft.ActionMsg = "已清除 AuthKey（重启后生效）"
		}
	})
}

func (s *Session) OpenCacheDirFolder() {
	if s.Client == nil {
		return
	}
	s.launchAsync(func() func() {
		err := s.Client.OpenCacheDir(s.ctx)
		return func() {
			if err != nil {
				s.SettingsDraft.Err = err.Error()
			}
		}
	})
}

func (s *Session) OpenDaemonGuide() {
	s.Guide = ForUnreachable(s.LocalURL, s.OfflineErr)
	s.Modal = ModalDaemon
	s.daemonModalOpen = true
}

func (s *Session) CloseModal() {
	switch s.Modal {
	case ModalSettings:
		s.CancelSettings()
	case ModalDelete:
		s.CancelDelete()
	case ModalDaemon:
		s.Modal = ModalNone
		s.daemonModalOpen = false
	case ModalDocEdit:
		s.RequestCloseDocEditor()
	}
}

func (s *Session) CancelTransfer(id string) {
	if s.Client == nil {
		return
	}
	s.launchAsync(func() func() {
		_, err := s.Client.CancelTransfer(s.ctx, id)
		transfers, _ := s.Client.Transfers(s.ctx)
		return func() {
			if transfers != nil {
				s.Transfers = transfers
			}
			if err != nil {
				s.SetStatus("取消失败: "+err.Error(), true)
			}
		}
	})
}

func (s *Session) ResumeTransfer(id string) {
	if s.Client == nil {
		return
	}
	s.launchAsync(func() func() {
		_, err := s.Client.ResumeTransfer(s.ctx, id)
		transfers, _ := s.Client.Transfers(s.ctx)
		return func() {
			if transfers != nil {
				s.Transfers = transfers
			}
			if err != nil {
				s.SetStatus("恢复失败: "+err.Error(), true)
			}
		}
	})
}

func (s *Session) DismissTransfer(id string) {
	if s.Client == nil {
		return
	}
	s.launchAsync(func() func() {
		err := s.Client.DismissTransfer(s.ctx, id)
		transfers, listErr := s.Client.Transfers(s.ctx)
		return func() {
			if listErr == nil && transfers != nil {
				s.Transfers = transfers
			}
			if err != nil {
				s.SetStatus("移除记录失败: "+err.Error(), true)
			}
		}
	})
}

func (s *Session) ClearFinishedTransfers() {
	if s.Client == nil {
		return
	}
	s.launchAsync(func() func() {
		remaining, err := s.Client.ClearTransfers(s.ctx)
		return func() {
			if err != nil {
				s.SetStatus("清除失败: "+err.Error(), true)
				return
			}
			s.Transfers = remaining
		}
	})
}

// RefreshAll fetches nodes, settings, info, and active transfers once.
func (s *Session) RefreshAll() {
	if s.Client == nil {
		return
	}
	s.pollCount.Add(1)
	s.launchAsync(func() func() {
		nodes, nodesErr := s.Client.Nodes(s.ctx)
		settings, _ := s.Client.Settings(s.ctx)
		transfers, _ := s.Client.Transfers(s.ctx)
		jobs, _ := s.Client.Jobs(s.ctx)
		return func() {
			if nodesErr != nil {
				if s.Conn != nil && s.Conn.Mode == agent.ConnAttached {
					s.Offline = true
					s.OfflineErr = nodesErr.Error()
				}
				return
			}
			s.Offline = false
			s.OfflineErr = ""
			s.Nodes = nodes
			s.Settings = &settings
			s.Transfers = transfers
			s.Jobs = jobs
			s.noteNodesUpdated()

			// Auto-bind initial empty tab only before any explicit tab/node choice was made.
			tab := s.Active()
			if !s.initialNodeBound && tab.Node == "" && len(s.Tabs) == 1 && len(nodes) > 0 {
				s.SelectNode(nodes[0].ID)
			} else if tab.Node != "" {
				s.loadNodeInfo(tab.Node)
				if tab.Path == "" {
					if n, ok := s.NodeByID(tab.Node); ok && len(n.ReadRoots) > 0 {
						s.SelectNode(tab.Node)
					}
				} else if len(tab.Entries) == 0 && !tab.Loading {
					s.loadActiveListing()
				}
			}
			s.syncDropTarget()
		}
	})
}

// PollTick executes one 1s background poll if window is visible and client is active.
func (s *Session) PollTick(hidden bool) {
	if hidden || s.Client == nil {
		return
	}
	s.pollCount.Add(1)
	s.launchAsync(func() func() {
		if err := s.Client.Health(s.ctx); err != nil {
			return func() {
				if s.Conn != nil && s.Conn.Mode != agent.ConnEmbedded && s.Conn.Mode != agent.ConnDemo {
					s.Offline = true
					s.OfflineErr = err.Error()
				}
			}
		}
		nodes, _ := s.Client.Nodes(s.ctx)
		transfers, _ := s.Client.Transfers(s.ctx)
		jobs, _ := s.Client.Jobs(s.ctx)
		return func() {
			prevConflicts := s.ActiveConflict() != nil
			if nodes != nil {
				s.Nodes = nodes
				s.noteNodesUpdated()
			}
			if transfers != nil {
				s.Transfers = transfers
			}
			if jobs != nil {
				s.Jobs = jobs
			}
			if !prevConflicts && s.ActiveConflict() != nil {
				s.conflictModalOpen = true
			}
			s.syncDropTarget()
		}
	})
}

// PollCount returns the number of poll/refresh network ticks initiated (used in INV-6 test).
func (s *Session) PollCount() int64 {
	return s.pollCount.Load()
}

func (s *Session) syncDropTarget() {
	if s.Client == nil || !s.CanWriteActive() {
		return
	}
	t := s.Active()
	if t.Node == s.lastDropNode && t.Path == s.lastDropDir {
		return
	}
	s.lastDropNode = t.Node
	s.lastDropDir = t.Path
	nodeID, dir := t.Node, t.Path
	s.launchAsync(func() func() {
		_ = s.Client.SetDropTarget(s.ctx, nodeID, dir)
		return nil
	})
}

func (s *Session) loadNodeInfo(nodeID string) {
	if s.Client == nil || nodeID == "" {
		return
	}
	s.launchAsync(func() func() {
		info, err := s.Client.Info(s.ctx, nodeID)
		if err != nil {
			return nil
		}
		return func() {
			s.WriteRoots[nodeID] = append([]string(nil), info.WriteRoots...)
			if s.Active().Node == nodeID {
				s.Info = &info
			}
		}
	})
}

func (s *Session) loadActiveListing() {
	tab := s.Active()
	if s.Client == nil || tab.Node == "" || tab.Path == "" {
		return
	}
	tabID := tab.ID
	gen := tab.ListGen
	nodeID := tab.Node
	path := tab.Path
	tab.Loading = true
	tab.Err = ""
	s.launchAsync(func() func() {
		listing, err := s.Client.List(s.ctx, nodeID, path)
		return func() {
			t := s.findTab(tabID)
			if t == nil || t.ListGen != gen || t.Node != nodeID || CanonicalizeRemote(t.Path) != CanonicalizeRemote(path) {
				return
			}
			t.Loading = false
			if err != nil {
				t.Err = err.Error()
				return
			}
			t.Entries = listing.Entries
			s.UpdateSidebarDirCache(nodeID, path, listing.Entries)
			// Prune any selection that no longer exists in listing.
			present := make(map[string]bool, len(listing.Entries))
			for _, e := range listing.Entries {
				present[e.Path] = true
			}
			for selPath := range t.Selected {
				if !present[selPath] {
					delete(t.Selected, selPath)
				}
			}
			s.syncDropTarget()
		}
	})
}

func (s *Session) findTab(id uint64) *Tab {
	for _, t := range s.Tabs {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
