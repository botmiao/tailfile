package gui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"tailfile.local/internal/agent"
)

// SidebarItemKind classifies rows shown in the sidebar ui.Outline tree.
type SidebarItemKind uint8

const (
	SidebarNone SidebarItemKind = iota
	SidebarNode
	SidebarDir
	SidebarPlaceholder
)

// SidebarKey uniquely identifies a device, cached directory, or load-status row in ui.Outline.
// Value comparability allows direct use as ui.OutlineState[SidebarKey].
type SidebarKey struct {
	Kind   SidebarItemKind
	NodeID string
	Path   string
}

// DirLoadPhase tracks lazy subdirectory loading for one directory node.
type DirLoadPhase uint8

const (
	DirUnloaded DirLoadPhase = iota
	DirLoading
	DirLoaded
	DirError
)

// DirCacheEntry holds cached subdirectories (folders only) for one node directory.
type DirCacheEntry struct {
	Phase   DirLoadPhase
	Subdirs []agent.Entry
	Err     string
	Gen     uint64
}

// SidebarTree owns lazy-loaded directory cache and programmatic Outline open/close deltas.
type SidebarTree struct {
	Cache        map[SidebarKey]*DirCacheEntry
	Expanded     map[SidebarKey]bool
	PendingOpen  []SidebarKey
	PendingClose []SidebarKey
	Gen          uint64
	nodesSig     string
}

func (s *Session) initSidebarTree() {
	if s.Tree.Cache == nil {
		s.Tree.Cache = map[SidebarKey]*DirCacheEntry{}
	}
	if s.Tree.Expanded == nil {
		s.Tree.Expanded = map[SidebarKey]bool{}
	}
	if s.Tree.Gen == 0 {
		s.Tree.Gen = 1
	}
}

func (s *Session) bumpTreeGen() {
	s.initSidebarTree()
	s.Tree.Gen++
}

func (s *Session) queueTreeOpen(k SidebarKey) {
	s.initSidebarTree()
	s.Tree.Expanded[k] = true
	s.Tree.PendingOpen = append(s.Tree.PendingOpen, k)
}

func (s *Session) queueTreeClose(k SidebarKey) {
	s.initSidebarTree()
	delete(s.Tree.Expanded, k)
	s.Tree.PendingClose = append(s.Tree.PendingClose, k)
}

// CanonicalNodeRoots returns deduplicated canonical remote roots preserving order.
func CanonicalNodeRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	seen := make(map[string]bool, len(roots))
	for _, r := range roots {
		cr := CanonicalizeRemote(r)
		if cr == "" {
			continue
		}
		cmp := remoteCompareForm(cr)
		if seen[cmp] {
			continue
		}
		seen[cmp] = true
		out = append(out, cr)
	}
	return out
}

// RemoteAncestorPaths finds the deepest root containing target and returns the
// root and inclusive ancestor chain [root, ..., target].
func RemoteAncestorPaths(target string, roots []string) (string, []string) {
	cp := CanonicalizeRemote(target)
	if cp == "" {
		return "", nil
	}
	canonRoots := CanonicalNodeRoots(roots)
	var matchedRoot string
	for _, cr := range canonRoots {
		if PathIsUnder(cp, cr) && len(cr) > len(matchedRoot) {
			matchedRoot = cr
		}
	}
	if matchedRoot == "" {
		return "", nil
	}
	crumbs := BuildCrumbs(cp, []string{matchedRoot})
	chain := make([]string, 0, len(crumbs))
	for _, cr := range crumbs {
		chain = append(chain, CanonicalizeRemote(cr.Path))
	}
	return matchedRoot, chain
}

// SidebarRoots returns the top-level device keys shown in the sidebar Outline.
func (s *Session) SidebarRoots() []SidebarKey {
	roots := make([]SidebarKey, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		roots = append(roots, SidebarKey{Kind: SidebarNode, NodeID: n.ID})
	}
	return roots
}

// SidebarDirLabel returns the concise display name for a shared root or subdirectory.
func (s *Session) SidebarDirLabel(k SidebarKey) string {
	if label := BaseRemote(k.Path); label != "" {
		return label
	}
	return k.Path
}

// SidebarRowLabel returns the accessible label / type-select text for an Outline row.
func (s *Session) SidebarRowLabel(k SidebarKey) string {
	switch k.Kind {
	case SidebarNode:
		if n, ok := s.NodeByID(k.NodeID); ok {
			return n.Name
		}
		return k.NodeID
	case SidebarDir:
		return s.SidebarDirLabel(k)
	case SidebarPlaceholder:
		s.initSidebarTree()
		entry := s.Tree.Cache[SidebarKey{Kind: SidebarDir, NodeID: k.NodeID, Path: CanonicalizeRemote(k.Path)}]
		if entry != nil && entry.Phase == DirError {
			return "加载失败（点击重试）"
		}
		return "正在加载…"
	default:
		return ""
	}
}

// SidebarChildren returns ui.Outline children for key k:
// nil = leaf (no chevron), empty slice = unloaded expandable branch, populated = children.
func (s *Session) SidebarChildren(k SidebarKey) []SidebarKey {
	s.initSidebarTree()
	switch k.Kind {
	case SidebarNode:
		n, ok := s.NodeByID(k.NodeID)
		if !ok || !n.Online || !n.AgentOK {
			return nil
		}
		roots := CanonicalNodeRoots(n.ReadRoots)
		if len(roots) == 0 {
			return nil
		}
		if s.AutoEnterSingleRoot && len(roots) == 1 {
			return s.sidebarDirChildren(n.ID, roots[0], s.Tree.Expanded[k])
		}
		out := make([]SidebarKey, 0, len(roots))
		for _, r := range roots {
			out = append(out, SidebarKey{Kind: SidebarDir, NodeID: n.ID, Path: r})
		}
		return out
	case SidebarDir:
		return s.sidebarDirChildren(k.NodeID, k.Path, s.Tree.Expanded[k])
	default:
		return nil
	}
}

func (s *Session) sidebarDirChildren(nodeID, dirPath string, isOpen bool) []SidebarKey {
	canon := CanonicalizeRemote(dirPath)
	if canon == "" {
		return nil
	}
	dirKey := SidebarKey{Kind: SidebarDir, NodeID: nodeID, Path: canon}
	entry := s.Tree.Cache[dirKey]
	if entry == nil || entry.Phase == DirUnloaded {
		if isOpen {
			return []SidebarKey{{Kind: SidebarPlaceholder, NodeID: nodeID, Path: canon}}
		}
		// Non-nil empty slice tells ui.Outline to render a disclosure chevron prior to lazy load.
		return []SidebarKey{}
	}
	if entry.Phase == DirLoading || entry.Phase == DirError {
		return []SidebarKey{{Kind: SidebarPlaceholder, NodeID: nodeID, Path: canon}}
	}
	if len(entry.Subdirs) == 0 {
		return nil
	}
	out := make([]SidebarKey, 0, len(entry.Subdirs))
	for _, sub := range entry.Subdirs {
		subPath := CanonicalizeRemote(sub.Path)
		if subPath == "" {
			continue
		}
		out = append(out, SidebarKey{Kind: SidebarDir, NodeID: nodeID, Path: subPath})
	}
	return out
}

func filterSortedDirs(entries []agent.Entry) []agent.Entry {
	var dirs []agent.Entry
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		cp := e
		cp.Path = CanonicalizeRemote(e.Path)
		if cp.Path == "" {
			continue
		}
		if cp.Name == "" {
			cp.Name = BaseRemote(cp.Path)
		}
		dirs = append(dirs, cp)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		an, bn := strings.ToLower(dirs[i].Name), strings.ToLower(dirs[j].Name)
		if an != bn {
			return an < bn
		}
		if dirs[i].Name != dirs[j].Name {
			return dirs[i].Name < dirs[j].Name
		}
		return dirs[i].Path < dirs[j].Path
	})
	return dirs
}

func sameDirPaths(a, b []agent.Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

// UpdateSidebarDirCache updates the folder-only cache for nodeID:dirPath and bumps
// Tree.Gen only when load state or subdirectory list changes.
func (s *Session) UpdateSidebarDirCache(nodeID, dirPath string, entries []agent.Entry) {
	s.initSidebarTree()
	canon := CanonicalizeRemote(dirPath)
	if nodeID == "" || canon == "" {
		return
	}
	dirKey := SidebarKey{Kind: SidebarDir, NodeID: nodeID, Path: canon}
	dirs := filterSortedDirs(entries)
	entry := s.Tree.Cache[dirKey]
	if entry != nil && entry.Phase == DirLoaded && sameDirPaths(entry.Subdirs, dirs) {
		return
	}
	if entry == nil {
		entry = &DirCacheEntry{}
		s.Tree.Cache[dirKey] = entry
	}
	entry.Gen++
	entry.Phase = DirLoaded
	entry.Err = ""
	entry.Subdirs = dirs
	if len(dirs) == 0 {
		s.queueTreeClose(dirKey)
		if n, ok := s.NodeByID(nodeID); ok && s.AutoEnterSingleRoot {
			roots := CanonicalNodeRoots(n.ReadRoots)
			if len(roots) == 1 && remoteCompareForm(roots[0]) == remoteCompareForm(canon) {
				s.queueTreeClose(SidebarKey{Kind: SidebarNode, NodeID: nodeID})
			}
		}
	}
	s.bumpTreeGen()
}

// LoadSidebarDir triggers an async lazy fetch of subdirectories under nodeID:dirPath.
func (s *Session) LoadSidebarDir(nodeID, dirPath string, force bool) {
	s.initSidebarTree()
	canon := CanonicalizeRemote(dirPath)
	if nodeID == "" || canon == "" {
		return
	}
	n, ok := s.NodeByID(nodeID)
	if !ok || !n.Online || !n.AgentOK {
		return
	}
	if len(n.ReadRoots) > 0 && !PathIsUnderAny(canon, n.ReadRoots) {
		return
	}
	dirKey := SidebarKey{Kind: SidebarDir, NodeID: nodeID, Path: canon}
	entry := s.Tree.Cache[dirKey]
	if !force && entry != nil && (entry.Phase == DirLoading || entry.Phase == DirLoaded) {
		return
	}
	if s.Client == nil {
		return
	}
	if entry == nil {
		entry = &DirCacheEntry{}
		s.Tree.Cache[dirKey] = entry
	}
	entry.Gen++
	reqGen := entry.Gen
	entry.Phase = DirLoading
	entry.Err = ""
	s.bumpTreeGen()

	s.launchAsync(func() func() {
		listing, err := s.Client.List(s.ctx, nodeID, canon)
		return func() {
			cur := s.Tree.Cache[dirKey]
			if cur == nil || cur.Gen != reqGen {
				return
			}
			if err != nil {
				cur.Phase = DirError
				cur.Err = err.Error()
				s.bumpTreeGen()
				return
			}
			s.UpdateSidebarDirCache(nodeID, canon, listing.Entries)
		}
	})
}

// SyncFromOutlineOpen mirrors ui.Outline's Open keys and kicks off lazy fetches for newly opened branches.
func (s *Session) SyncFromOutlineOpen(open *ui.Selection[SidebarKey]) {
	s.initSidebarTree()
	if open == nil {
		return
	}
	next := make(map[SidebarKey]bool, open.Len())
	changed := false
	for k := range open.All() {
		next[k] = true
		if !s.Tree.Expanded[k] {
			changed = true
		}
		switch k.Kind {
		case SidebarNode:
			if s.AutoEnterSingleRoot {
				if n, ok := s.NodeByID(k.NodeID); ok {
					roots := CanonicalNodeRoots(n.ReadRoots)
					if len(roots) == 1 {
						s.LoadSidebarDir(k.NodeID, roots[0], false)
					}
				}
			}
		case SidebarDir:
			s.LoadSidebarDir(k.NodeID, k.Path, false)
		}
	}
	for k := range s.Tree.Expanded {
		if !next[k] {
			changed = true
			break
		}
	}
	s.Tree.Expanded = next
	if changed {
		s.bumpTreeGen()
	}
}

// SyncSidebarToActive expands the sidebar tree path to reveal the active tab's folder.
func (s *Session) SyncSidebarToActive() {
	s.initSidebarTree()
	tab := s.Active()
	if tab.Node == "" {
		return
	}
	n, ok := s.NodeByID(tab.Node)
	if !ok || !n.Online || !n.AgentOK {
		return
	}
	roots := CanonicalNodeRoots(n.ReadRoots)
	if len(roots) == 0 {
		return
	}
	nodeKey := SidebarKey{Kind: SidebarNode, NodeID: n.ID}
	s.queueTreeOpen(nodeKey)

	if s.AutoEnterSingleRoot && len(roots) == 1 {
		s.LoadSidebarDir(n.ID, roots[0], false)
		if _, chain := RemoteAncestorPaths(tab.Path, roots); len(chain) > 1 {
			for i := 1; i < len(chain)-1; i++ {
				k := SidebarKey{Kind: SidebarDir, NodeID: n.ID, Path: chain[i]}
				s.queueTreeOpen(k)
				s.LoadSidebarDir(n.ID, chain[i], false)
			}
		}
	} else {
		_, chain := RemoteAncestorPaths(tab.Path, roots)
		for i := 0; i < len(chain)-1; i++ {
			k := SidebarKey{Kind: SidebarDir, NodeID: n.ID, Path: chain[i]}
			s.queueTreeOpen(k)
			s.LoadSidebarDir(n.ID, chain[i], false)
		}
	}
	s.bumpTreeGen()
}

// VisibleSidebarKeys computes the currently expanded flattened key order matching ui.Outline.
func (s *Session) VisibleSidebarKeys(open *ui.Selection[SidebarKey]) []SidebarKey {
	roots := s.SidebarRoots()
	out := make([]SidebarKey, 0, len(roots))
	var walk func(items []SidebarKey)
	walk = func(items []SidebarKey) {
		for _, k := range items {
			out = append(out, k)
			if open != nil && open.Has(k) {
				if kids := s.SidebarChildren(k); len(kids) > 0 {
					walk(kids)
				}
			}
		}
	}
	walk(roots)
	return out
}

// DesiredSidebarKey picks the visible tree row key matching the active tab (folder if visible, else device).
func (s *Session) DesiredSidebarKey(rowAt func(int) SidebarKey, rowCount int) SidebarKey {
	tab := s.Active()
	if tab.Node == "" {
		return SidebarKey{}
	}
	canonPath := CanonicalizeRemote(tab.Path)
	if canonPath != "" {
		wantDir := SidebarKey{Kind: SidebarDir, NodeID: tab.Node, Path: canonPath}
		for i := range rowCount {
			cur := rowAt(i)
			if cur.Kind == SidebarDir && cur.NodeID == wantDir.NodeID && remoteCompareForm(cur.Path) == remoteCompareForm(wantDir.Path) {
				return cur
			}
		}
	}
	return SidebarKey{Kind: SidebarNode, NodeID: tab.Node}
}

// ActivateSidebarKey applies a left-click/cursor activation on a sidebar Outline row.
func (s *Session) ActivateSidebarKey(k SidebarKey) {
	switch k.Kind {
	case SidebarNode:
		s.SelectNode(k.NodeID)
	case SidebarDir:
		_ = s.SelectNodePath(k.NodeID, k.Path)
	case SidebarPlaceholder:
		s.initSidebarTree()
		dirKey := SidebarKey{Kind: SidebarDir, NodeID: k.NodeID, Path: CanonicalizeRemote(k.Path)}
		if entry := s.Tree.Cache[dirKey]; entry != nil && entry.Phase == DirError {
			s.LoadSidebarDir(k.NodeID, k.Path, true)
		}
	}
}

// OpenKeyInNewTab opens the clicked node or folder directly in a new tab.
func (s *Session) OpenKeyInNewTab(k SidebarKey) bool {
	switch k.Kind {
	case SidebarNode:
		return s.OpenNodeInNewTab(k.NodeID, "")
	case SidebarDir:
		return s.OpenNodeInNewTab(k.NodeID, k.Path)
	default:
		return false
	}
}

// RefreshSidebarKey invalidates and refetches cached subdirectories for a node or folder.
func (s *Session) RefreshSidebarKey(k SidebarKey) {
	s.initSidebarTree()
	switch k.Kind {
	case SidebarNode:
		n, ok := s.NodeByID(k.NodeID)
		if !ok {
			return
		}
		roots := CanonicalNodeRoots(n.ReadRoots)
		if len(roots) == 0 {
			return
		}
		if s.AutoEnterSingleRoot && len(roots) == 1 {
			s.LoadSidebarDir(n.ID, roots[0], true)
			return
		}
		refreshedAny := false
		for _, r := range roots {
			dk := SidebarKey{Kind: SidebarDir, NodeID: n.ID, Path: r}
			if s.Tree.Cache[dk] != nil {
				s.LoadSidebarDir(n.ID, r, true)
				refreshedAny = true
			}
		}
		if !refreshedAny {
			s.LoadSidebarDir(n.ID, roots[0], true)
		}
	case SidebarDir:
		s.LoadSidebarDir(k.NodeID, k.Path, true)
	}
}

func (s *Session) noteNodesUpdated() {
	s.initSidebarTree()
	var b strings.Builder
	for _, n := range s.Nodes {
		fmt.Fprintf(&b, "%s|%s|%s|%t|%t|%t|%t|", n.ID, n.Name, n.OS, n.Online, n.AgentOK, n.Self, n.ReadOnly)
		for _, r := range n.ReadRoots {
			b.WriteString(r)
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	sig := b.String()
	if sig != s.Tree.nodesSig {
		s.Tree.nodesSig = sig
		s.bumpTreeGen()
	}
}
