package gui

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"tailfile.local/internal/agent"
)

func newDemoTester(t *testing.T, w, h int) (*Session, *ui.Tester) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	state := t.TempDir()
	conn, err := agent.StartEmbedded(ctx, agent.Config{Demo: true, StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	sess := NewSession(conn, GUIOptions{})
	t.Cleanup(sess.Close)

	tt := ui.NewTester(sess.View, w, h)
	settleSession(t, sess, tt)
	return sess, tt
}

func settleSession(t *testing.T, s *Session, tt *ui.Tester) {
	t.Helper()
	for i := 0; i < 6; i++ {
		if err := s.WaitIdle(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if s.pendingOps.Load() == 0 {
			s.DrainPending()
			tt.Frame()
			if s.pendingOps.Load() == 0 {
				return
			}
		}
	}
}

func TestVerifyINV1DesktopNavigationTabsAndViews(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)

	for _, name := range []string{"alpha", "beta", "gamma", "pixel", "phone", "notes", "hello.txt"} {
		if !tt.HasText(name) {
			t.Fatalf("missing text %q in initial desktop view: %v", name, tt.Texts())
		}
	}

	// Open notes via double click (or context menu "打开").
	if err := tt.Click("notes"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(40 * time.Millisecond)
	if err := tt.Click("notes"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)

	if !sess.CanGoBack() || !sess.CanGoUp() {
		t.Fatalf("expected back & up available inside notes, path=%q", sess.Active().Path)
	}
	if err := tt.Click("后退"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if !tt.HasText("hello.txt") {
		t.Fatal("expected alpha root listing after GoBack")
	}

	if err := tt.Click("前进"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)

	// Open new tab (Mod+T -> Scheme 1 device picker), bind card, close (Mod+W), reopen (Mod+Shift+T), then middle-click to close.
	tt.Key(ui.Cmd, ui.KeyT)
	settleSession(t, sess, tt)
	if len(sess.Tabs) != 2 || sess.Active().Node != "" || !tt.HasText("新建标签页") {
		t.Fatalf("expected blank Scheme 1 new tab, tabs=%d node=%q texts=%v", len(sess.Tabs), sess.Active().Node, tt.Texts())
	}
	if err := tt.Click("在线 · 只读"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if sess.Active().Node != "gamma" || !tt.HasText("readme.txt") {
		t.Fatalf("expected clicking gamma picker card to bind tab, node=%q texts=%v", sess.Active().Node, tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyW)
	settleSession(t, sess, tt)
	if len(sess.Tabs) != 1 {
		t.Fatalf("tabs=%d, want 1", len(sess.Tabs))
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyT)
	settleSession(t, sess, tt)
	if len(sess.Tabs) != 2 || sess.Active().Node != "gamma" {
		t.Fatalf("tabs=%d node=%q after reopen, want 2 gamma", len(sess.Tabs), sess.Active().Node)
	}
	tabR, ok := tt.Find(sess.TabLabel(sess.Active()))
	if !ok {
		t.Fatalf("missing active tab label %q", sess.TabLabel(sess.Active()))
	}
	tt.PressButton(2, tabR.X+tabR.W/2, tabR.Y+tabR.H/2)
	tt.ReleaseButton(2, tabR.X+tabR.W/2, tabR.Y+tabR.H/2)
	settleSession(t, sess, tt)
	if len(sess.Tabs) != 1 {
		t.Fatalf("middle-click on tab did not close tab: tabs=%d", len(sess.Tabs))
	}

	// Toggle Icons view and back to List view.
	if err := tt.Click("切换图标视图"); err != nil {
		t.Fatal(err)
	}
	if sess.ViewMode != ViewIcons {
		t.Fatalf("viewMode = %s, want icons", sess.ViewMode)
	}
	if err := tt.Click("切换列表视图"); err != nil {
		t.Fatal(err)
	}
	if sess.ViewMode != ViewList {
		t.Fatalf("viewMode = %s, want list", sess.ViewMode)
	}
}

func TestVerifyINV2PreviewTextAndImage(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)

	if err := tt.Click("hello.txt"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if !tt.HasText("Hello from alpha.") {
		t.Fatalf("expected text preview of hello.txt, got %v", tt.Texts())
	}

	// Open photos folder and preview swatch.png
	if err := tt.Click("photos"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(40 * time.Millisecond)
	if err := tt.Click("photos"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)

	if err := tt.Click("swatch.png"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	prev := sess.Active().Preview
	if prev == nil || prev.Kind != "image" || prev.Image == nil {
		t.Fatalf("expected decoded image preview for swatch.png, got %+v", prev)
	}
}

func TestVerifyINV3WriteOpsTransferAndReadOnlyGate(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)

	// Create folder on writable alpha
	if err := tt.Click("新建文件夹"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.Editor.Kind != EditorMkdir {
		t.Fatalf("expected EditorMkdir, got %d", sess.Editor.Kind)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("mydir")
	tt.Frame()
	if err := tt.Click("创建"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if !tt.HasText("mydir") {
		t.Fatalf("created folder missing from alpha listing: %v", tt.Texts())
	}

	// Rename folder in-place (no top "确定" banner button)
	if err := tt.Click("mydir"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("重命名"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("确定") {
		t.Fatalf("unexpected top banner confirm button during in-place rename: %v", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("工作区")
	tt.Key(0, ui.KeyEnter)
	settleSession(t, sess, tt)
	if !tt.HasText("工作区") {
		t.Fatalf("renamed folder missing: %v", tt.Texts())
	}

	// Copy hello.txt from alpha and paste onto beta
	if err := tt.Click("hello.txt"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("beta"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if err := tt.Click("粘贴"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)

	// Wait for transfer completion and refresh beta listing
	deadline := time.Now().Add(5 * time.Second)
	for !tt.HasText("hello.txt") && time.Now().Before(deadline) {
		sess.Refresh()
		settleSession(t, sess, tt)
		time.Sleep(25 * time.Millisecond)
	}
	if !tt.HasText("hello.txt") {
		t.Fatalf("pasted hello.txt did not appear on beta: %v", tt.Texts())
	}

	// Wait for transfer bar to reach completed state and verify "清除记录" clears it.
	for !tt.HasText("清除记录") && time.Now().Before(deadline) {
		sess.Refresh()
		settleSession(t, sess, tt)
		time.Sleep(25 * time.Millisecond)
	}
	if err := tt.Click("清除记录"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if len(sess.Transfers) != 0 || tt.HasText("清除记录") {
		t.Fatalf("expected transfer bar cleared, transfers=%d", len(sess.Transfers))
	}

	// Switch to read-only gamma: write gates must disable write actions.
	if err := tt.Click("gamma"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if sess.CanWriteActive() || sess.BeginMkdir() {
		t.Fatal("read-only node gamma allowed write action")
	}
}

func TestVerifyINV4NonDismissibleConflictModal(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)

	// Modify destination hello.txt on beta (or copy different contents) so beta already has hello.txt,
	// then paste alpha/hello.txt onto beta to trigger conflict!
	betaRoot := sess.Nodes[1].ReadRoots[0]
	for _, n := range sess.Nodes {
		if n.ID == "beta" {
			betaRoot = n.ReadRoots[0]
		}
	}
	if err := os.WriteFile(filepath.Join(betaRoot, "hello.txt"), []byte("existing beta bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := tt.Click("hello.txt"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("beta"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if err := tt.Click("粘贴"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for sess.ActiveConflict() == nil && time.Now().Before(deadline) {
		sess.PollTick(false)
		settleSession(t, sess, tt)
		time.Sleep(25 * time.Millisecond)
	}
	if sess.ActiveConflict() == nil || !tt.HasText("目标文件已存在") {
		t.Fatalf("expected non-dismissible conflict modal, got conflict=%+v texts=%v", sess.ActiveConflict(), tt.Texts())
	}

	// Escape MUST NOT dismiss the conflict modal.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if sess.ActiveConflict() == nil || !sess.conflictModalOpen || !tt.HasText("目标文件已存在") {
		t.Fatal("Escape wrongly dismissed ConflictDialog")
	}

	// Choosing "覆盖" resolves the conflict cleanly.
	if err := tt.Click("覆盖"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if sess.ActiveConflict() != nil || sess.conflictModalOpen {
		t.Fatal("conflict dialog remained open after clicking 覆盖")
	}
}

func TestVerifyINV5MobileResponsiveLayoutAndThemes(t *testing.T) {
	sess, tt := newDemoTester(t, 390, 844)

	// Open mobile drawer and pick beta
	if err := tt.Click("菜单"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !sess.mobileDrawerOpen || !tt.HasText("标签页") {
		t.Fatalf("mobile drawer did not open cleanly: %v", tt.Texts())
	}
	if err := tt.Click("beta"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	settleSession(t, sess, tt)
	if sess.Active().Node != "beta" {
		t.Fatalf("expected mobile node switch to beta, got %q", sess.Active().Node)
	}

	// Tap inbox.txt on mobile -> opens bottom sheet details
	if err := tt.Click("inbox.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	settleSession(t, sess, tt)
	if !sess.mobileDetailsOpen || !tt.HasText("详情与预览") {
		t.Fatalf("mobile details sheet did not open: open=%v texts=%v", sess.mobileDetailsOpen, tt.Texts())
	}

	// Verify dark & light theme switch renders cleanly with no panic
	tt.SetDark(true)
	tt.Frame()
	sess.Theme = ThemePrefLight
	tt.Frame()
}

func TestVerifyINV6WindowHiddenPausesPolling(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)

	// Visible: advancing clock by 2.2s should advance pollCount.
	beforeVisible := sess.PollCount()
	tt.Advance(2200 * time.Millisecond)
	settleSession(t, sess, tt)
	if sess.PollCount() <= beforeVisible {
		t.Fatalf("expected polls while visible, before=%d after=%d", beforeVisible, sess.PollCount())
	}

	// Hidden: advancing clock by 5s MUST make zero polls and leave no pending frames.
	tt.SetWindowHidden(true)
	tt.Frame()
	beforeHidden := sess.PollCount()
	tt.Advance(5 * time.Second)
	if sess.PollCount() != beforeHidden {
		t.Fatalf("polled while hidden: before=%d after=%d", beforeHidden, sess.PollCount())
	}
	if tt.FramePending() {
		t.Fatal("hidden window kept pending poll frames")
	}

	// Restored: polling resumes immediately.
	tt.SetWindowHidden(false)
	tt.Frame()
	tt.Advance(1100 * time.Millisecond)
	settleSession(t, sess, tt)
	if sess.PollCount() <= beforeHidden {
		t.Fatalf("polling did not resume after unhide: hidden=%d resumed=%d", beforeHidden, sess.PollCount())
	}
}

func TestVerifySettingsHidesAuthKeyOutsideTsnet(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)

	for _, want := range []string{"主题外观", "布局密度", "自动", "紧凑", "标准", "宽松", "当前采用标准", "关闭窗口", "每次询问", "最小化到托盘", "直接退出", "目录树", "自动展开左侧目录树到当前文件夹", "单根设备展开时直接显示子目录", "缓存目录", "打开缓存目录", "关闭", "保存"} {
		if !tt.HasText(want) {
			t.Fatalf("missing settings item %q in %v", want, tt.Texts())
		}
	}
	if sess.SyncSidebarTree || !sess.AutoEnterSingleRoot {
		t.Fatalf("unexpected default tree settings: sync=%v autoSingle=%v", sess.SyncSidebarTree, sess.AutoEnterSingleRoot)
	}
	if err := tt.Click("自动展开左侧目录树到当前文件夹"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !sess.SyncSidebarTree {
		t.Fatal("clicking sync-sidebar-tree checkbox did not enable SyncSidebarTree")
	}
	if err := tt.Click("单根设备展开时直接显示子目录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.AutoEnterSingleRoot {
		t.Fatal("clicking auto-enter-single-root checkbox did not disable AutoEnterSingleRoot")
	}
	if err := tt.Click("单根设备展开时直接显示子目录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !sess.AutoEnterSingleRoot {
		t.Fatal("second click did not re-enable AutoEnterSingleRoot")
	}
	for _, hidden := range []string{"Agent 地址", "Token 文件", "测试连接", "AuthKey", "清除 AuthKey"} {
		if tt.HasText(hidden) {
			t.Fatalf("unexpected removed/tsnet control %q in settings modal", hidden)
		}
	}

	// Close-window preference buttons apply immediately.
	if err := tt.Click("最小化到托盘"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.CloseAction != CloseActionTray {
		t.Fatalf("clicking Tray in Settings did not apply immediately: close=%q", sess.CloseAction)
	}
	if err := tt.Click("每次询问"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.CloseAction != CloseActionAsk {
		t.Fatalf("clicking Ask in Settings did not apply immediately: close=%q", sess.CloseAction)
	}

	// Density buttons in Settings apply immediately without pressing Save.
	if err := tt.Click("紧凑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.Density != DensityPrefCompact || tt.HasText("当前采用标准") {
		t.Fatalf("clicking Compact in Settings failed: density=%s texts=%v", sess.Density, tt.Texts())
	}
	if err := tt.Click("宽松"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.Density != DensityPrefComfortable {
		t.Fatalf("clicking Comfortable in Settings failed: density=%s", sess.Density)
	}
	if err := tt.Click("自动"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.Density != DensityPrefAuto || !tt.HasText("当前采用标准") {
		t.Fatalf("clicking Auto in Settings failed: density=%s texts=%v", sess.Density, tt.Texts())
	}

	// Theme buttons in Settings apply immediately without pressing Save.
	if err := tt.Click("浅色"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.Theme != ThemePrefLight {
		t.Fatalf("clicking Light in Settings did not apply immediately: theme=%s", sess.Theme)
	}
	if err := tt.Click("深色"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.Theme != ThemePrefDark {
		t.Fatalf("clicking Dark in Settings did not apply immediately: theme=%s", sess.Theme)
	}

	// Clicking Save with no text edits acts as an immediate close without failing or keeping the modal open.
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sess.Modal != ModalNone || sess.settingsModalOpen || sess.Theme != ThemePrefDark {
		t.Fatalf("Save with no dirty text fields did not close cleanly: modal=%d open=%v theme=%s", sess.Modal, sess.settingsModalOpen, sess.Theme)
	}

	// Experimental --net tsnet reveals AuthKey settings controls.
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	sess.Settings = &agent.Settings{Net: "tsnet", AuthKeyConfigured: true, CacheDir: "/tmp/c"}
	tt.Frame()
	for _, want := range []string{"AuthKey", "清除 AuthKey"} {
		if !tt.HasText(want) {
			t.Fatalf("expected tsnet control %q when running in tsnet mode, texts=%v", want, tt.Texts())
		}
	}
}

func TestVerifyCollapsedLogoAndClosePrompt(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)
	if !tt.HasText("演示模式") || tt.HasText("内置模式") || tt.HasText("本地后台") {
		t.Fatalf("unexpected sidebar status texts: %v", tt.Texts())
	}

	// Collapse sidebar -> "Tailfile" title hides, "展开侧边栏" Logo button expands back.
	sess.ToggleSidebar()
	tt.Frame()
	if !sess.SidebarCollapsed {
		t.Fatal("expected sidebar collapsed")
	}
	sess.ToggleSidebar()
	tt.Frame()
	if sess.SidebarCollapsed {
		t.Fatal("expected sidebar expanded")
	}

	// Open close prompt, verify checkbox default + click "最小化到托盘".
	hid := false
	sess.onHideWindow = func() { hid = true }
	sess.OpenClosePrompt()
	tt.Frame()
	for _, want := range []string{"关闭主窗口", "记住我的选择，不再提醒", "取消", "直接退出", "最小化到托盘"} {
		if !tt.HasText(want) {
			t.Fatalf("missing close prompt text %q in %v", want, tt.Texts())
		}
	}
	if err := tt.Click("最小化到托盘"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hid || sess.CloseAction != CloseActionTray || sess.Modal != ModalNone {
		t.Fatalf("close prompt Tray action failed: hid=%v closeAction=%q modal=%d", hid, sess.CloseAction, sess.Modal)
	}
}

func TestVerifyInPlaceRenameListAndGrid(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)

	// 1. List view: F2 triggers in-place rename with stem selection ("hello" in "hello.txt") and no top banner.
	if err := tt.Click("hello.txt"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	if sess.Editor.Kind != EditorRename {
		t.Fatalf("expected EditorRename after F2, got %d", sess.Editor.Kind)
	}
	if tt.HasText("确定") || tt.HasText("取消") {
		t.Fatalf("unexpected top banner buttons during in-place rename: %v", tt.Texts())
	}
	tt.Type("world")
	tt.Key(0, ui.KeyEnter)
	settleSession(t, sess, tt)
	if !tt.HasText("world.txt") || tt.HasText("hello.txt") {
		t.Fatalf("expected hello.txt renamed to world.txt keeping extension, texts=%v", tt.Texts())
	}

	// 2. Invalid name keeps EditorRename open with error, then Escape cancels cleanly.
	if err := tt.Click("world.txt"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("bad/name")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if sess.Editor.Kind != EditorRename || sess.Editor.Err == "" {
		t.Fatalf("expected EditorRename to remain open with Err on invalid name, got kind=%d err=%q", sess.Editor.Kind, sess.Editor.Err)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if sess.Editor.Kind != EditorNone || !tt.HasText("world.txt") {
		t.Fatalf("expected Escape to cancel invalid rename, kind=%d texts=%v", sess.Editor.Kind, tt.Texts())
	}

	// 3. Clicking another row commits the pending in-place rename and switches selection.
	if err := tt.Click("world.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("hello.txt")
	tt.Frame()
	if err := tt.Click("notes"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if !tt.HasText("hello.txt") || tt.HasText("world.txt") {
		t.Fatalf("expected blur/row-click to commit hello.txt rename, texts=%v", tt.Texts())
	}
	if sel := sess.SelectedEntries(); len(sel) != 1 || sel[0].Name != "notes" {
		t.Fatalf("expected notes selected after click-away commit, got %+v", sel)
	}

	// 4. Icon Grid view: F2 triggers in-place rename without top banner, Escape cancels cleanly.
	if err := tt.Click("切换图标视图"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if err := tt.Click("notes"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	if sess.Editor.Kind != EditorRename {
		t.Fatalf("expected EditorRename in grid view, got %d", sess.Editor.Kind)
	}
	if tt.HasText("确定") || tt.HasText("取消") {
		t.Fatalf("unexpected top banner buttons in grid rename: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if sess.Editor.Kind != EditorNone {
		t.Fatalf("expected Escape to cancel grid rename, got %d", sess.Editor.Kind)
	}
}

func TestVerifyMarqueeSelectionListAndGrid(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)
	selectedNames := func() []string {
		sel := sess.SelectedEntries()
		out := make([]string, len(sel))
		for i, e := range sel {
			out[i] = e.Name
		}
		return out
	}

	notesR, ok := tt.Find("notes")
	if !ok {
		t.Fatal("missing notes row")
	}
	photosR, ok := tt.Find("photos")
	if !ok {
		t.Fatal("missing photos row")
	}
	helloR, ok := tt.Find("hello.txt")
	if !ok {
		t.Fatal("missing hello.txt row")
	}

	// 1. List view: drag from empty space below rows upward across hello.txt and photos, then shrink back off photos.
	emptyX := helloR.X + 120
	emptyY := helloR.Y + 60
	tt.Press(emptyX, emptyY)
	tt.Move(emptyX-60, photosR.Y+photosR.H/2)
	tt.Frame()
	if got := selectedNames(); len(got) != 2 || got[0] != "photos" || got[1] != "hello.txt" {
		t.Fatalf("mid-drag expected [photos hello.txt], got %v", got)
	}
	tt.Move(emptyX-60, helloR.Y+helloR.H/2)
	tt.Release(emptyX-60, helloR.Y+helloR.H/2)
	settleSession(t, sess, tt)
	if got := selectedNames(); len(got) != 1 || got[0] != "hello.txt" {
		t.Fatalf("after shrinking drag back off photos expected [hello.txt], got %v", got)
	}

	// Empty space click clears selection.
	tt.Press(emptyX, emptyY)
	tt.Release(emptyX, emptyY)
	settleSession(t, sess, tt)
	if got := selectedNames(); len(got) != 0 {
		t.Fatalf("expected empty space click to clear selection, got %v", got)
	}

	// 2. List view: click vs drag starting in right metadata whitespace of notes.
	metaX := notesR.X + notesR.W - 80
	tt.Press(metaX, notesR.Y+notesR.H/2)
	tt.Release(metaX, notesR.Y+notesR.H/2)
	settleSession(t, sess, tt)
	if got := selectedNames(); len(got) != 1 || got[0] != "notes" {
		t.Fatalf("click on row right whitespace expected [notes], got %v", got)
	}
	tt.Advance(600 * time.Millisecond)
	tt.Press(metaX, notesR.Y+notesR.H/2)
	tt.Move(metaX, helloR.Y+helloR.H/2)
	tt.Release(metaX, helloR.Y+helloR.H/2)
	settleSession(t, sess, tt)
	if got := selectedNames(); len(got) != 3 {
		t.Fatalf("drag from row right whitespace expected all 3 entries, got %v", got)
	}

	// 3. List view: Cmd click toggles on right metadata whitespace; Escape mid-drag cancels and restores base selection.
	if err := tt.Click("hello.txt"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	tt.ClickAtWith(ui.Cmd, metaX, photosR.Y+photosR.H/2)
	settleSession(t, sess, tt)
	if got := selectedNames(); len(got) != 2 || got[0] != "photos" || got[1] != "hello.txt" {
		t.Fatalf("Cmd-click right whitespace expected [photos hello.txt], got %v", got)
	}

	tt.Advance(600 * time.Millisecond)
	tt.Press(emptyX, emptyY)
	tt.Move(emptyX, notesR.Y+notesR.H/2)
	tt.Key(0, ui.KeyEscape)
	tt.Release(emptyX, notesR.Y+notesR.H/2)
	settleSession(t, sess, tt)
	if got := selectedNames(); len(got) != 2 || got[0] != "photos" || got[1] != "hello.txt" {
		t.Fatalf("Escape mid-drag should restore [photos hello.txt], got %v", got)
	}

	// 4. Icon Grid view: drag across notes and photos tiles, then empty click clears.
	if err := tt.Click("切换图标视图"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	notesGridR, ok := tt.Find("notes")
	if !ok {
		t.Fatal("missing notes tile in grid view")
	}
	photosGridR, ok := tt.Find("photos")
	if !ok {
		t.Fatal("missing photos tile in grid view")
	}
	gridEmptyX := notesGridR.X + 10
	gridEmptyY := notesGridR.Y + notesGridR.H + 20
	tt.Press(gridEmptyX, gridEmptyY)
	tt.Move(photosGridR.X+photosGridR.W-10, photosGridR.Y+photosGridR.H-10)
	tt.Release(photosGridR.X+photosGridR.W-10, photosGridR.Y+photosGridR.H-10)
	settleSession(t, sess, tt)
	if got := selectedNames(); len(got) != 2 || got[0] != "notes" || got[1] != "photos" {
		t.Fatalf("grid marquee expected [notes photos], got %v", got)
	}
	tt.Press(gridEmptyX, gridEmptyY)
	tt.Release(gridEmptyX, gridEmptyY)
	settleSession(t, sess, tt)
	if got := selectedNames(); len(got) != 0 {
		t.Fatalf("grid empty space click expected clear selection, got %v", got)
	}
}

func TestVerifyINV7DirectTextEditorAndReadOnlyView(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)

	var alphaRoot string
	for _, n := range sess.Nodes {
		if n.ID == "alpha" && len(n.ReadRoots) > 0 {
			alphaRoot = n.ReadRoots[0]
		}
	}
	if alphaRoot == "" {
		t.Fatal("missing alpha root")
	}
	alphaFile := filepath.Join(alphaRoot, "hello.txt")

	t.Run("edit_save_find_and_refresh_preview", func(t *testing.T) {
		if err := tt.Click("hello.txt"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if err := tt.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if sess.Modal != ModalDocEdit || sess.DocEditor.Phase != DocReady || sess.DocEditor.ReadOnly {
			t.Fatalf("unexpected editor open state: modal=%d phase=%d ro=%v err=%q", sess.Modal, sess.DocEditor.Phase, sess.DocEditor.ReadOnly, sess.DocEditor.Err)
		}
		if !strings.Contains(sess.DocEditor.Buffer.String(), "Hello from alpha.") {
			t.Fatalf("unexpected initial buffer: %q", sess.DocEditor.Buffer.String())
		}

		const editedContent = "Edited directly in Tailfile!\nLine 2.\n"
		sess.DocEditor.Buffer.Set(editedContent)
		tt.Frame()
		if !sess.DocEditor.Dirty() {
			t.Fatal("expected Dirty() after buffer edit")
		}

		tt.Key(ui.Cmd, ui.KeyF)
		tt.Frame()
		if !sess.DocEditor.FindOpen {
			t.Fatal("expected FindBar open on Cmd+F")
		}
		tt.Type("Line")
		tt.Frame()
		if len(sess.DocEditor.FindMatches) != 1 {
			t.Fatalf("expected 1 find match, got %+v", sess.DocEditor.FindMatches)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if sess.DocEditor.FindOpen || sess.Modal != ModalDocEdit {
			t.Fatalf("expected Escape to close FindBar while keeping ModalDocEdit open: find=%v modal=%d", sess.DocEditor.FindOpen, sess.Modal)
		}

		if err := tt.Click("保存"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if sess.DocEditor.Dirty() || sess.DocEditor.StatusNote != "已保存" {
			t.Fatalf("save did not settle cleanly: dirty=%v note=%q err=%q", sess.DocEditor.Dirty(), sess.DocEditor.StatusNote, sess.DocEditor.Err)
		}
		diskBytes, err := os.ReadFile(alphaFile)
		if err != nil || string(diskBytes) != editedContent {
			t.Fatalf("disk mismatch after save: got %q (%v)", diskBytes, err)
		}

		if err := tt.Click("关闭"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if sess.Modal != ModalNone {
			t.Fatalf("expected clean close to ModalNone, got %d", sess.Modal)
		}
		if !tt.HasText("Edited directly in Tailfile!") {
			t.Fatalf("expected details preview refreshed after save, texts=%v", tt.Texts())
		}
	})

	const resolvedContent = "Resolved local edit\n"
	t.Run("conflict_detection_and_overwrite", func(t *testing.T) {
		if err := tt.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if err := os.WriteFile(alphaFile, []byte("External update\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		future := time.Now().Add(2 * time.Second)
		if err := os.Chtimes(alphaFile, future, future); err != nil {
			t.Fatal(err)
		}
		sess.DocEditor.Buffer.Set(resolvedContent)
		tt.Frame()
		if err := tt.Click("保存"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if sess.DocEditor.Phase != DocConflict || !tt.HasText("覆盖保存") {
			t.Fatalf("expected DocConflict prompt, got phase=%d texts=%v", sess.DocEditor.Phase, tt.Texts())
		}
		if err := tt.Click("覆盖保存"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if sess.DocEditor.Phase != DocReady || sess.DocEditor.Dirty() {
			t.Fatalf("expected DocReady clean after force overwrite, got phase=%d dirty=%v", sess.DocEditor.Phase, sess.DocEditor.Dirty())
		}
		if diskBytes, _ := os.ReadFile(alphaFile); string(diskBytes) != resolvedContent {
			t.Fatalf("disk mismatch after overwrite: got %q", diskBytes)
		}
	})

	t.Run("dirty_close_guard", func(t *testing.T) {
		sess.DocEditor.Buffer.Set("unsaved discard draft")
		tt.Frame()
		if err := tt.Click("关闭"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if sess.DocEditor.Phase != DocConfirmClose || !tt.HasText("放弃修改") {
			t.Fatalf("expected DocConfirmClose prompt, got phase=%d texts=%v", sess.DocEditor.Phase, tt.Texts())
		}
		if err := tt.Click("放弃修改"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if sess.Modal != ModalNone {
			t.Fatalf("expected ModalNone after discard, got %d", sess.Modal)
		}
		if diskBytes, _ := os.ReadFile(alphaFile); string(diskBytes) != resolvedContent {
			t.Fatalf("discard altered disk: got %q", diskBytes)
		}
	})

	t.Run("readonly_full_viewer", func(t *testing.T) {
		if err := tt.Click("gamma"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if err := tt.Click("readme.txt"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if err := tt.Click("查看全文"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sess, tt)
		if sess.Modal != ModalDocEdit || !sess.DocEditor.ReadOnly || !tt.HasText("只读模式，仅支持查看与复制") {
			t.Fatalf("expected read-only viewer on gamma, modal=%d ro=%v texts=%v", sess.Modal, sess.DocEditor.ReadOnly, tt.Texts())
		}
		if !strings.Contains(sess.DocEditor.Buffer.String(), "Gamma is read-only.") {
			t.Fatalf("unexpected gamma content: %q", sess.DocEditor.Buffer.String())
		}
		if sess.SaveDocEditor(false) || tt.HasText("保存") {
			t.Fatal("read-only viewer must not allow save")
		}
		if err := tt.Click("关闭"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	})

	t.Run("mobile_editor_modal", func(t *testing.T) {
		sessM, ttM := newDemoTester(t, 390, 844)
		if err := ttM.Click("hello.txt"); err != nil {
			t.Fatal(err)
		}
		ttM.Advance(time.Second)
		settleSession(t, sessM, ttM)
		if err := ttM.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settleSession(t, sessM, ttM)
		if sessM.Modal != ModalDocEdit || sessM.mobileDetailsOpen || !ttM.HasText("关闭") {
			t.Fatalf("mobile editor open failed: modal=%d sheet=%v texts=%v", sessM.Modal, sessM.mobileDetailsOpen, ttM.Texts())
		}
	})
}

func TestVerifyINV8SidebarOutlineTreeAndNewTabUX(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)

	// 1. + button vertical center matches active tab pill label midline.
	plusR, ok := tt.Find("新建标签页")
	if !ok {
		t.Fatal("missing new-tab-btn")
	}
	tabR, ok := tt.Find(sess.TabLabel(sess.Active()))
	if !ok {
		t.Fatal("missing active tab label")
	}
	plusMid := plusR.Y + plusR.H/2
	tabMid := tabR.Y + tabR.H/2
	if math.Abs(float64(plusMid-tabMid)) > 2.0 {
		t.Fatalf("+ button vertical midline %.1f misaligned with tab midline %.1f", plusMid, tabMid)
	}

	// 2. Expand alpha chevron in ui.Outline: verify standard row height (30..34pt) and lazy folder filtering.
	stdMetrics := MetricsFor(DensityStandard)
	alphaR, ok := tt.Find("alpha")
	if !ok {
		t.Fatal("missing alpha in sidebar")
	}
	if alphaR.H < 30 || alphaR.H > 34 {
		t.Fatalf("sidebar outline row height = %.1f, want standard 30..34pt", alphaR.H)
	}
	tt.ClickAt(alphaR.X+stdMetrics.OutlineArrowCenterX(0), alphaR.Y+alphaR.H/2)
	settleSession(t, sess, tt)

	alphaRoot := CanonicalizeRemote(sess.Nodes[0].ReadRoots[0])
	alphaKids := sess.SidebarChildren(SidebarKey{Kind: SidebarNode, NodeID: "alpha"})
	if len(alphaKids) != 2 {
		t.Fatalf("expected 2 folder children under alpha, got %#v", alphaKids)
	}
	if !tt.HasText("notes") || !tt.HasText("photos") {
		t.Fatalf("expanded alpha folders missing in tree: %v", tt.Texts())
	}

	// Expanding file-only folder photos confirms lazy load marks it leaf (chevron removed).
	photosR, ok := tt.Find("photos")
	if !ok {
		t.Fatal("missing photos folder row in sidebar")
	}
	tt.ClickAt(photosR.X+stdMetrics.OutlineArrowCenterX(1), photosR.Y+photosR.H/2)
	settleSession(t, sess, tt)
	photosPath := JoinRemote(alphaRoot, "photos")
	if got := sess.SidebarChildren(SidebarKey{Kind: SidebarDir, NodeID: "alpha", Path: photosPath}); got != nil {
		t.Fatalf("expected empty folder photos to return nil children, got %#v", got)
	}

	// 3. Single click on sidebar folder notes navigates right pane immediately.
	if err := tt.Click("notes"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if BaseRemote(sess.Active().Path) != "notes" || !tt.HasText("week.md") {
		t.Fatalf("single click on sidebar folder did not navigate right pane: path=%q texts=%v", sess.Active().Path, tt.Texts())
	}

	// 4. Toggling AutoEnterSingleRoot off wraps alpha root folder explicitly.
	sess.SetAutoEnterSingleRoot(false)
	settleSession(t, sess, tt)
	wrapped := sess.SidebarChildren(SidebarKey{Kind: SidebarNode, NodeID: "alpha"})
	if len(wrapped) != 1 || wrapped[0].Path != alphaRoot {
		t.Fatalf("AutoEnterSingleRoot=false kids = %#v, want [%s]", wrapped, alphaRoot)
	}
	sess.SetAutoEnterSingleRoot(true)
	settleSession(t, sess, tt)

	// 5. Middle-click beta in sidebar opens it in a new tab.
	betaR, ok := tt.Find("beta")
	if !ok {
		t.Fatal("missing beta in sidebar")
	}
	tt.PressButton(2, betaR.X+betaR.W/2, betaR.Y+betaR.H/2)
	tt.ReleaseButton(2, betaR.X+betaR.W/2, betaR.Y+betaR.H/2)
	settleSession(t, sess, tt)
	if len(sess.Tabs) != 2 || sess.Active().Node != "beta" || !tt.HasText("inbox.txt") {
		t.Fatalf("middle-click on beta sidebar row failed: tabs=%d node=%q texts=%v", len(sess.Tabs), sess.Active().Node, tt.Texts())
	}

	// 6. Right-click gamma row opens PopoverMenu and "在新标签页打开" opens tab 3.
	gammaR, ok := tt.Find("gamma")
	if !ok {
		t.Fatal("missing gamma in sidebar")
	}
	tt.RightClickAt(gammaR.X+gammaR.W/2, gammaR.Y+gammaR.H/2)
	menuItems := tt.Menu()
	if len(menuItems) < 2 || menuItems[0] != "在新标签页打开" {
		t.Fatalf("unexpected sidebar PopoverMenu items: %v", menuItems)
	}
	if err := tt.ChooseMenuItem("在新标签页打开"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if len(sess.Tabs) != 3 || sess.Active().Node != "gamma" || !tt.HasText("readme.txt") {
		t.Fatalf("context menu Open in New Tab failed: tabs=%d node=%q", len(sess.Tabs), sess.Active().Node)
	}

	// 7. Keyboard navigation in ui.Outline (Down moves selection, Left collapses open branch).
	sess.SetSyncSidebarTree(false)
	if err := tt.Click("alpha"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	alphaKey := SidebarKey{Kind: SidebarNode, NodeID: "alpha"}
	if !sess.Tree.Expanded[alphaKey] {
		t.Fatal("expected alpha to remain expanded before KeyLeft")
	}
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if sess.Tree.Expanded[alphaKey] {
		t.Fatal("KeyLeft did not collapse open alpha branch in ui.Outline")
	}
	tt.Key(0, ui.KeyDown)
	settleSession(t, sess, tt)
	if sess.Active().Node != "beta" {
		t.Fatalf("KeyDown in ui.Outline moved to node %q, want beta", sess.Active().Node)
	}

	// 8. SyncSidebarTree=false leaves unvisited path unexpanded until enabled.
	_ = sess.SelectNodePath("gamma", JoinRemote(CanonicalizeRemote(sess.Nodes[2].ReadRoots[0]), "docs"))
	settleSession(t, sess, tt)
	gammaKey := SidebarKey{Kind: SidebarNode, NodeID: "gamma"}
	if sess.Tree.Expanded[gammaKey] {
		t.Fatal("SyncSidebarTree=false unexpectedly expanded gamma")
	}
	sess.SetSyncSidebarTree(true)
	settleSession(t, sess, tt)
	if !sess.Tree.Expanded[gammaKey] {
		t.Fatal("SetSyncSidebarTree(true) did not expand gamma to reveal active docs folder")
	}
}

func TestVerifyLayoutDensitySwitchingAndAuto(t *testing.T) {
	sess, tt := newDemoTester(t, 1220, 780)

	measureRows := func() (treeH, listH float32, helloR ui.Rect) {
		t.Helper()
		alphaR, ok := tt.Find("alpha")
		if !ok {
			t.Fatal("missing alpha sidebar row")
		}
		hR, ok := tt.Find("hello.txt")
		if !ok {
			t.Fatal("missing hello.txt file row")
		}
		return alphaR.H, hR.H, hR
	}

	verifyMarqueeDrag := func() {
		t.Helper()
		photosR, ok := tt.Find("photos")
		if !ok {
			t.Fatal("missing photos row for marquee check")
		}
		hR, ok := tt.Find("hello.txt")
		if !ok {
			t.Fatal("missing hello.txt row for marquee check")
		}
		emptyX := hR.X + 120
		emptyY := hR.Y + hR.H + 24
		tt.Press(emptyX, emptyY)
		tt.Move(emptyX-60, photosR.Y+photosR.H/2)
		tt.Release(emptyX-60, photosR.Y+photosR.H/2)
		settleSession(t, sess, tt)
		sel := sess.SelectedEntries()
		if len(sel) != 2 || sel[0].Name != "photos" || sel[1].Name != "hello.txt" {
			t.Fatalf("marquee drag mismatch under density %s: got %#v", sess.Density, sel)
		}
		tt.Press(emptyX, emptyY)
		tt.Release(emptyX, emptyY)
		settleSession(t, sess, tt)
	}

	stdTreeH, stdListH, _ := measureRows()
	if math.Abs(float64(stdTreeH-MetricsFor(DensityStandard).TreeRowTotalH())) > 0.5 ||
		math.Abs(float64(stdListH-MetricsFor(DensityStandard).ListRowH)) > 0.5 {
		t.Fatalf("1220x780 auto resolved row heights = (tree %.1f, list %.1f), want (%.1f, %.1f)",
			stdTreeH, stdListH, MetricsFor(DensityStandard).TreeRowTotalH(), MetricsFor(DensityStandard).ListRowH)
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if err := tt.Click("紧凑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)

	cmpTreeH, cmpListH, _ := measureRows()
	if math.Abs(float64(cmpTreeH-MetricsFor(DensityCompact).TreeRowTotalH())) > 0.5 ||
		math.Abs(float64(cmpListH-MetricsFor(DensityCompact).ListRowH)) > 0.5 {
		t.Fatalf("compact row heights = (tree %.1f, list %.1f), want (%.1f, %.1f)",
			cmpTreeH, cmpListH, MetricsFor(DensityCompact).TreeRowTotalH(), MetricsFor(DensityCompact).ListRowH)
	}
	verifyMarqueeDrag()

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)
	if err := tt.Click("宽松"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	settleSession(t, sess, tt)

	comfTreeH, comfListH, _ := measureRows()
	if math.Abs(float64(comfTreeH-MetricsFor(DensityComfortable).TreeRowTotalH())) > 0.5 ||
		math.Abs(float64(comfListH-MetricsFor(DensityComfortable).ListRowH)) > 0.5 {
		t.Fatalf("comfortable row heights = (tree %.1f, list %.1f), want (%.1f, %.1f)",
			comfTreeH, comfListH, MetricsFor(DensityComfortable).TreeRowTotalH(), MetricsFor(DensityComfortable).ListRowH)
	}
	verifyMarqueeDrag()

	if !(cmpTreeH < stdTreeH && stdTreeH < comfTreeH) {
		t.Fatalf("sidebar tree height not strictly increasing: compact=%.1f standard=%.1f comfortable=%.1f", cmpTreeH, stdTreeH, comfTreeH)
	}
	if !(cmpListH < stdListH && stdListH < comfListH) {
		t.Fatalf("file list height not strictly increasing: compact=%.1f standard=%.1f comfortable=%.1f", cmpListH, stdListH, comfListH)
	}

	// Cramped desktop viewport (900x700) under Auto resolves to Compact.
	narrowSess, narrowTT := newDemoTester(t, 900, 700)
	if narrowSess.Density != DensityPrefAuto {
		t.Fatalf("expected default Auto density, got %q", narrowSess.Density)
	}
	alphaN, ok := narrowTT.Find("alpha")
	if !ok {
		t.Fatal("missing alpha on narrow desktop")
	}
	helloN, ok := narrowTT.Find("hello.txt")
	if !ok {
		t.Fatal("missing hello.txt on narrow desktop")
	}
	if math.Abs(float64(alphaN.H-MetricsFor(DensityCompact).TreeRowTotalH())) > 0.5 ||
		math.Abs(float64(helloN.H-MetricsFor(DensityCompact).ListRowH)) > 0.5 {
		t.Fatalf("900x700 auto resolved row heights = (tree %.1f, list %.1f), want compact (%.1f, %.1f)",
			alphaN.H, helloN.H, MetricsFor(DensityCompact).TreeRowTotalH(), MetricsFor(DensityCompact).ListRowH)
	}
}
