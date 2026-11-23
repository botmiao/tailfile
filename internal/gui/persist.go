package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// ThemePref selects light, dark, or system theme.
type ThemePref string

const (
	ThemePrefSystem ThemePref = "system"
	ThemePrefLight  ThemePref = "light"
	ThemePrefDark   ThemePref = "dark"
)

// ViewMode selects list or icon grid layout.
type ViewMode string

const (
	ViewList  ViewMode = "list"
	ViewIcons ViewMode = "icons"
)

// SortCol selects which file column drives sorting.
type SortCol string

const (
	SortName SortCol = "name"
	SortSize SortCol = "size"
	SortTime SortCol = "time"
)

// CloseActionPref selects what closing the main desktop window does.
type CloseActionPref string

const (
	CloseActionAsk  CloseActionPref = "ask"
	CloseActionTray CloseActionPref = "tray"
	CloseActionQuit CloseActionPref = "quit"
)

const (
	DefaultSidebarWidth float32 = 262
	MinSidebarWidth     float32 = 180
	MaxSidebarWidth     float32 = 400
	DefaultDetailsWidth float32 = 300
	MinDetailsWidth     float32 = 220
	MaxDetailsWidth     float32 = 520
)

// ClampSidebarWidth constrains expanded sidebar width to safe desktop bounds.
func ClampSidebarWidth(w float32) float32 {
	if w <= 0 {
		return DefaultSidebarWidth
	}
	if w < MinSidebarWidth {
		return MinSidebarWidth
	}
	if w > MaxSidebarWidth {
		return MaxSidebarWidth
	}
	return w
}

// ClampDetailsWidth constrains details pane width to safe desktop bounds.
func ClampDetailsWidth(w float32) float32 {
	if w <= 0 {
		return DefaultDetailsWidth
	}
	if w < MinDetailsWidth {
		return MinDetailsWidth
	}
	if w > MaxDetailsWidth {
		return MaxDetailsWidth
	}
	return w
}

// SavedTab stores one persisted node/path pair.
type SavedTab struct {
	Node string `json:"node"`
	Path string `json:"path"`
}

// UiPersist is the persisted desktop/mobile layout & tab snapshot.
type UiPersist struct {
	Theme               ThemePref       `json:"theme"`
	Density             DensityPref     `json:"density,omitempty"`
	View                ViewMode        `json:"view"`
	Details             bool            `json:"details"`
	SidebarCollapsed    bool            `json:"sidebarCollapsed"`
	SidebarWidth        float32         `json:"sidebarWidth,omitempty"`
	DetailsWidth        float32         `json:"detailsWidth,omitempty"`
	CloseAction         CloseActionPref `json:"closeAction,omitempty"`
	SyncSidebarTree     bool            `json:"syncSidebarTree,omitempty"`
	AutoEnterSingleRoot *bool           `json:"autoEnterSingleRoot,omitempty"`
	Tabs                []SavedTab      `json:"tabs"`
	ActiveTab           int             `json:"activeTab"`
	SortKey             SortCol         `json:"sortKey"`
	SortAsc             bool            `json:"sortAsc"`
}

func boolPtr(v bool) *bool { return &v }

// AutoEnterSingleRootEnabled returns true unless explicitly disabled in persisted config.
func (p UiPersist) AutoEnterSingleRootEnabled() bool {
	if p.AutoEnterSingleRoot == nil {
		return true
	}
	return *p.AutoEnterSingleRoot
}

func defaultPersist() UiPersist {
	return UiPersist{
		Theme:               ThemePrefSystem,
		Density:             DensityPrefAuto,
		View:                ViewList,
		Details:             true,
		SidebarWidth:        DefaultSidebarWidth,
		DetailsWidth:        DefaultDetailsWidth,
		CloseAction:         CloseActionAsk,
		SyncSidebarTree:     false,
		AutoEnterSingleRoot: boolPtr(true),
		SortKey:             SortName,
		SortAsc:             true,
	}
}

func sanitizeCloseAction(pref CloseActionPref) CloseActionPref {
	switch pref {
	case CloseActionAsk, CloseActionTray, CloseActionQuit:
		return pref
	default:
		return CloseActionAsk
	}
}

func (p *UiPersist) sanitize() {
	switch p.Theme {
	case ThemePrefLight, ThemePrefDark, ThemePrefSystem:
	default:
		p.Theme = ThemePrefSystem
	}
	p.Density = sanitizeDensityPref(p.Density)
	switch p.View {
	case ViewList, ViewIcons:
	default:
		p.View = ViewList
	}
	switch p.SortKey {
	case SortName, SortSize, SortTime:
	default:
		p.SortKey = SortName
	}
	p.SidebarWidth = ClampSidebarWidth(p.SidebarWidth)
	p.DetailsWidth = ClampDetailsWidth(p.DetailsWidth)
	p.CloseAction = sanitizeCloseAction(p.CloseAction)
	if p.ActiveTab < 0 {
		p.ActiveTab = 0
	}
}

func legacyPersistPaths(stateDir string) []string {
	var paths []string
	if stateDir != "" {
		paths = append(paths,
			filepath.Join(stateDir, "ui.json"),
			filepath.Join(stateDir, "nsh-ui.json"),
		)
	}
	if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
		paths = append(paths, filepath.Join(cfgDir, "tailfile", "nsh-preview", "ui.json"))
	}
	return paths
}

// LoadPersist loads UI preferences from <StateDir>/ui.json, falling back to
// legacy nsh-ui.json / nsh-preview/ui.json if present.
func LoadPersist(stateDir string) UiPersist {
	out := defaultPersist()
	for _, p := range legacyPersistPaths(stateDir) {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			continue
		}
		out.sanitize()
		return out
	}
	return out
}

// SavePersist atomically persists UI state to <StateDir>/ui.json with mode 0600.
func SavePersist(stateDir string, p UiPersist) error {
	if stateDir == "" {
		return nil
	}
	p.sanitize()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(stateDir, "ui-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(0o600); err != nil {
			return err
		}
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	dst := filepath.Join(stateDir, "ui.json")
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	cleanup = false
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dst, 0o600)
	}
	return nil
}
