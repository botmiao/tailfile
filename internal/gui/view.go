package gui

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"tailfile.local/internal/agent"
)

func setupDesktopTray(win *mygo.Window, showMain func()) bool {
	if runtime.GOOS == "ios" || runtime.GOOS == "android" {
		return false
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "显示主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { showMain() }},
		mygo.Separator(),
		{Label: "退出 Tailfile", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
	opts := mygo.TrayOptions{
		ID:      "tailfile.tray",
		Icon:    AppIconPNG,
		ToolTip: "Tailfile",
	}
	// macOS and Linux AppIndicator attach the menu directly; Windows uses left-click restore + right-click popup.
	if runtime.GOOS != "windows" {
		opts.Menu = menu
	}
	tray, err := mygo.NewTray(opts)
	if err != nil {
		log.Printf("tailfile: tray unavailable (%v)", err)
		return false
	}
	if runtime.GOOS == "windows" {
		tray.OnClick(showMain)
		tray.OnRightClick(func() {
			tray.SetMenu(menu)
			tray.PopUpMenu()
			tray.SetMenu(nil)
		})
	}
	return true
}

type marqueeState struct {
	active       bool
	dragging     bool
	node         string
	path         string
	viewMode     ViewMode
	mode         MarqueeMode
	pressMods    ui.Modifiers
	pressClicks  int
	hitEntry     *agent.Entry
	startX       float32
	startY       float32
	curX         float32
	curY         float32
	baseSelected map[string]bool
	baseAnchor   string
}

type viewLocalState struct {
	filter              ui.Handle
	bitmapKey           string
	bitmap              *ui.Bitmap
	editorText          ui.Handle
	docArea             ui.Handle
	docInsertTab        bool
	docWasFindOpen      bool
	sidebarDragging     bool
	detailsDragging     bool
	listScroll          ui.ScrollState
	gridScroll          ui.ScrollState
	listNameDir         string
	listNameRight       map[string]float32
	gridItems           []MarqueeItem
	marquee             marqueeState
	sidebarOutline      ui.OutlineState[SidebarKey]
	sidebarSelected     int
	sidebarOutlineReady bool
	lastOutlineDensity  ResolvedDensity
	lastListDensity     ResolvedDensity
	sidebarSyncNode     string
	sidebarSyncPath     string
	sidebarSyncGen      uint64
	sidebarMiddleKey    SidebarKey
	sidebarMiddleDown   bool
	tabMiddleDownID     uint64
}

// Run launches the Tailfile desktop or mobile UI window and runs mygo.App.
func Run(conn *agent.Conn, opts GUIOptions, singleInstance bool) int {
	if singleInstance && runtime.GOOS != "ios" && runtime.GOOS != "android" {
		if !mygo.App.RequestSingleInstanceLock() {
			return 0
		}
	}

	mygo.App.SetName("Tailfile")
	sess := NewSession(conn, opts)
	sess.onThemeChange = applyAppThemeSource
	defer sess.Close()

	mygo.App.WhenReady(func() {
		applyAppThemeSource(sess.Theme)
		titleBarStyle := mygo.TitleBarHidden
		if runtime.GOOS == "ios" || runtime.GOOS == "android" {
			titleBarStyle = mygo.TitleBarDefault
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Tailfile",
			Width:           1220,
			Height:          780,
			MinWidth:        680,
			MinHeight:       480,
			StateKey:        "main",
			TitleBarStyle:   titleBarStyle,
			BackgroundColor: "light-dark(#e8ebf0, #121419)",
			Content:         ui.View(sess.View),
		})
		if len(AppIconPNG) > 0 {
			if runtime.GOOS == "darwin" {
				_ = mygo.App.Dock.SetIcon(AppIconPNG)
			} else if runtime.GOOS != "ios" && runtime.GOOS != "android" {
				_ = win.SetIcon(AppIconPNG)
			}
		}
		sess.SetWake(func() {
			win.Update(func() {
				sess.DrainPending()
			})
		})
		showMain := func() {
			win.Show()
			win.Focus()
		}
		sess.onHideWindow = func() { win.Hide() }
		sess.onQuitApp = func() { mygo.App.Quit() }
		mygo.App.OnSecondInstance(func([]string, string) {
			showMain()
		})
		if singleInstance {
			quitting := false
			mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
			trayActive := setupDesktopTray(win, showMain)
			if trayActive {
				win.OnClose(func(e *mygo.CloseEvent) {
					switch DecideWindowClose(trayActive, quitting, sess.CloseAction) {
					case CloseHideToTray:
						e.PreventDefault()
						win.Hide()
					case ClosePromptUser:
						e.PreventDefault()
						sess.OpenClosePrompt()
						win.Update(func() {})
					}
				})
			}
		}
	})

	mygo.App.Run()
	return 0
}

func applyAppThemeSource(pref ThemePref) {
	switch pref {
	case ThemePrefLight:
		mygo.Theme.SetSource(mygo.ThemeLight)
	case ThemePrefDark:
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// View builds the full Tailfile interface (responsive between desktop and <820pt mobile).
func (s *Session) View(c *ui.Context) {
	s.DrainPending()
	w, h := c.Size()
	prefs := c.Preferences()
	resolvedDensity := ResolveDensity(s.Density, DensityEnv{
		Width:      w,
		Height:     h,
		AnyPointer: prefs.AnyPointer,
		GOOS:       runtime.GOOS,
	})
	metrics := MetricsFor(resolvedDensity)
	pal := ApplyTheme(c, s.Theme, metrics)
	loc := ui.Local(c.Root(), "tailfile-view-local", func() viewLocalState {
		return viewLocalState{}
	})

	// 1s polling ONLY while window is visible and client is attached/embedded.
	if !c.WindowHidden() && s.Client != nil {
		if s.lastPollAt.IsZero() {
			s.lastPollAt = c.Now()
		}
		if c.Now().Sub(s.lastPollAt) >= time.Second {
			s.lastPollAt = c.Now()
			s.PollTick(false)
		}
		c.After(time.Second)
	}

	s.handleShortcuts(c, loc)

	isMobile := runtime.GOOS == "ios" || runtime.GOOS == "android" || (w > 0 && w < 820)
	if isMobile {
		s.viewMobile(c, pal, metrics, loc)
	} else {
		s.viewDesktop(c, pal, metrics, loc)
	}

	s.renderModals(c, pal, metrics, loc)
	s.SyncOverlays()
}

func (s *Session) handleShortcuts(c *ui.Context, loc *viewLocalState) {
	if loc.marquee.dragging && c.Shortcut(0, ui.KeyEscape) {
		loc.marquee.active = false
		loc.marquee.dragging = false
		s.ApplyMarqueeSelection(loc.marquee.baseSelected, loc.marquee.baseAnchor)
		return
	}
	if s.Editor.Kind != EditorNone && c.Shortcut(0, ui.KeyEscape) {
		s.CancelEditor()
		return
	}
	if s.Modal != ModalNone || s.ActiveConflict() != nil {
		return
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyT) {
		s.ReopenClosedTab()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyT) {
		s.NewTab()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyW) {
		s.CloseTab(s.ActiveIdx)
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyL) {
		s.BeginGoto()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		loc.filter.Focus()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyR) || c.Shortcut(0, ui.KeyF5) {
		s.Refresh()
		return
	}
	if c.Shortcut(ui.Alt, ui.KeyLeft) {
		s.GoBack()
		return
	}
	if c.Shortcut(ui.Alt, ui.KeyRight) {
		s.GoForward()
		return
	}
	if c.Shortcut(ui.Alt, ui.KeyUp) {
		s.GoUp()
		return
	}
	if c.FocusTakesText() {
		return
	}
	if c.Shortcut(0, ui.KeyF2) {
		s.BeginRename()
		return
	}
	if c.Shortcut(0, ui.KeyDelete) || c.Shortcut(ui.Cmd, ui.KeyBackspace) {
		s.BeginDelete()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyC) {
		s.CopySelected()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyV) {
		s.Paste()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyA) {
		s.SelectAll()
		return
	}
}

func (s *Session) viewDesktop(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState) {
	tab := s.Active()
	bar := c.TitleBar()
	chromeH := float32(46)
	if bar.Height > chromeH {
		chromeH = bar.Height
	}

	ui.Row(c).Fill().Background(pal.Card).Children(func() {
		// Left sidebar (resizable when expanded, 56pt when collapsed) with continuous 1px border.
		sideW := ClampSidebarWidth(s.SidebarWidth)
		if s.SidebarCollapsed {
			sideW = 56
		}
		ui.Column(c).Width(sideW).FillHeight().Shrink(0).Background(pal.Sidebar).BorderWidth(0, 1, 0, 0).BorderColor(pal.Border).Children(func() {
			// Sidebar header / drag region aligned 1px-for-1px with tab strip border.
			if !s.SidebarCollapsed {
				headPadL := float32(14)
				if bar.Left > 0 {
					headPadL = bar.Left + 8
				}
				head := ui.Row(c).Height(chromeH).FillWidth().Padding(0, 10, 0, headPadL).AlignItems(ui.Center).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).DragWindow()
				head.Children(func() {
					LogoMark(c, 22)
					ui.Text(c, "Tailfile").FontSize(14).FontWeight(600).TextColor(pal.SidebarFg).Grow(1).SingleLine()
					if iconBtn(c, "collapse-sidebar", "收起侧边栏", PathPanelLeftClose, pal.MutedFg, pal.RowHover, false).Clicked() {
						s.ToggleSidebar()
					}
				})
			} else {
				head := ui.Row(c).Height(chromeH).FillWidth().Justify(ui.Center).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).DragWindow()
				head.Children(func() {
					expandBtn := ui.Button(c.Key("expand-sidebar"), "").Size(34, 34).Padding(0).Center().Background(ui.Transparent).BorderWidth(0).Radius(8).Label("展开侧边栏").Tooltip("展开侧边栏")
					if expandBtn.Hovered() {
						expandBtn.Background(pal.RowHover)
					}
					expandBtn.Children(func() {
						LogoMark(c, 22)
					})
					if expandBtn.Clicked() {
						s.ToggleSidebar()
					}
				})
			}

			if !s.SidebarCollapsed {
				ui.Row(c).Padding(12, 14, 6, 14).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "设备").FontSize(11).FontWeight(600).TextColor(pal.MutedFg).Grow(1)
					ui.Text(c, fmt.Sprintf("%d", len(s.Nodes))).FontSize(11).TextColor(pal.MutedFg)
				})
			}

			if s.SidebarCollapsed {
				ui.Scroll(c).Grow(1).Padding(4, 8, 8, 8).Gap(metrics.RailGap).Children(func() {
					for _, n := range s.Nodes {
						active := tab.Node == n.ID
						nodeOS := ResolveNodeOS(n.OS, n.ID, n.Name)
						rowBg := ui.Transparent
						fg := pal.SidebarFg
						if active {
							rowBg = pal.Accent
							fg = pal.AccentFg
						}
						row := ui.Row(c.Key("node-"+n.ID)).Height(metrics.RailRowH).FillWidth().Padding(0, 10).Radius(5).Gap(8).AlignItems(ui.Center).Background(rowBg).Justify(ui.Center)
						row.Label(n.Name).Role(ui.RoleListItem).Tooltip(n.Name)
						if row.Hovered() && !active {
							row.Background(pal.RowHover)
						}
						row.Children(func() {
							OSIcon(c, nodeOS, 16, fg)
						})
						nodeID := n.ID
						nodeKey := SidebarKey{Kind: SidebarNode, NodeID: nodeID}
						row.ObserveInput(func(ev ui.InputEvent) {
							if ev.Button != 2 {
								return
							}
							switch ev.Kind {
							case ui.InputPointerDown:
								loc.sidebarMiddleDown = true
								loc.sidebarMiddleKey = nodeKey
							case ui.InputPointerUp:
								if loc.sidebarMiddleDown && loc.sidebarMiddleKey == nodeKey {
									loc.sidebarMiddleDown = false
									if !ev.Canceled {
										s.OpenNodeInNewTab(nodeID, "")
									}
								}
							}
						})
						row.PopoverMenu(func(m *ui.Menu) {
							if m.Item("在新标签页打开").Chosen() {
								s.OpenNodeInNewTab(nodeID, "")
							}
						})
						if row.Clicked() {
							s.SelectNode(n.ID)
						}
					}
				})
			} else if len(s.Nodes) == 0 {
				ui.Column(c).Grow(1).FillWidth().Padding(4, 8, 8, 8).Children(func() {
					ui.Text(c, "暂无设备").FontSize(12).TextColor(pal.MutedFg).Padding(12, 10)
				})
			} else {
				s.renderDesktopSidebarOutline(c, pal, metrics, loc, tab)
			}

			// Sidebar footer: aligned at exact 34pt height with right-hand status bar.
			if s.SidebarCollapsed {
				ui.Row(c).Height(34).FillWidth().Justify(ui.Center).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Children(func() {
					if iconBtn(c, "sidebar-settings", "设置", PathSettings, pal.MutedFg, pal.RowHover, false).Clicked() {
						s.OpenSettings()
					}
				})
			} else {
				ui.Row(c).Height(34).FillWidth().Padding(0, 10).Gap(8).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Children(func() {
					StatusDot(c, !s.Offline, false)
					ui.Text(c, s.sidebarStatusText()).FontSize(12).TextColor(pal.MutedFg).Grow(1).SingleLine().Ellipsis("…")
					if iconBtn(c, "sidebar-settings", "设置", PathSettings, pal.MutedFg, pal.RowHover, false).Clicked() {
						s.OpenSettings()
					}
				})
				renderPaneResizeHandle(c, "resize-sidebar", pal, &loc.sidebarDragging, false, 1, sideW, DefaultSidebarWidth, s.SetSidebarWidth)
			}
		})

		// Right workspace
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			// 1. 46pt Tab Strip
			tabH := chromeH - 6
			tabStrip := ui.Row(c).Height(chromeH).FillWidth().Background(pal.Background).Padding(0, bar.Right+8, 0, 8).Gap(4).AlignItems(ui.Center).Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, pal.Border, 0)
			}).DragWindow()
			tabStrip.Children(func() {
				ui.ScrollHorizontal(c).Grow(1).FillHeight().Children(func() {
					ui.Row(c).FillHeight().Gap(4).AlignItems(ui.End).Padding(6, 0, 0, 0).Children(func() {
						for i, t := range s.Tabs {
							active := i == s.ActiveIdx
							tabBg := pal.Muted
							tabFg := pal.MutedFg
							pillH := tabH - 1
							pillBottomMargin := float32(1)
							if active {
								tabBg = pal.Card
								tabFg = pal.Foreground
								pillH = tabH
								pillBottomMargin = 0
							}
							tKey := fmt.Sprintf("tab-%d", t.ID)
							pill := ui.Row(c.Key(tKey)).Height(pillH).Margin(0, 0, pillBottomMargin, 0).MaxWidth(220).Padding(0, 8, 0, 10).Gap(6).AlignItems(ui.Center).Background(tabBg).Radius(8, 8, 0, 0)
							if active {
								pill.BorderWidth(1, 1, 0, 1).BorderColor(pal.Border)
							} else if pill.Hovered() {
								pill.Background(pal.RowHover)
							}
							var nodeOS string
							if t.Node != "" {
								if n, ok := s.NodeByID(t.Node); ok {
									nodeOS = ResolveNodeOS(n.OS, n.ID, n.Name)
								}
							}
							var closeClicked bool
							pill.Children(func() {
								if t.Node == "" {
									StrokeIcon(c, PathLayoutGrid, 13, tabFg)
								} else {
									OSIcon(c, nodeOS, 13, tabFg)
								}
								ui.Text(c, s.TabLabel(t)).FontSize(12).FontWeight(500).TextColor(tabFg).Grow(1).SingleLine().Ellipsis("…")
								closeBtn := ui.Box(c.Key(tKey+"-close")).Size(18, 18).Radius(4).Center().Label("关闭标签页").Tooltip("关闭标签页")
								if closeBtn.Hovered() {
									closeBtn.Background(pal.RowHover)
								}
								closeBtn.Children(func() {
									StrokeIcon(c, PathX, 11, pal.MutedFg)
								})
								closeClicked = closeBtn.Clicked()
							})
							tabID := t.ID
							pill.ObserveInput(func(ev ui.InputEvent) {
								if ev.Button != 2 {
									return
								}
								switch ev.Kind {
								case ui.InputPointerDown:
									loc.tabMiddleDownID = tabID
								case ui.InputPointerUp:
									if loc.tabMiddleDownID == tabID {
										loc.tabMiddleDownID = 0
										if !ev.Canceled {
											s.CloseTabByID(tabID)
										}
									}
								}
							})
							if closeClicked {
								s.CloseTab(i)
							} else if pill.Clicked() {
								s.SwitchTab(i)
							}
						}
						ui.Row(c).Height(tabH).AlignItems(ui.Center).Children(func() {
							if iconBtn(c, "new-tab-btn", "新建标签页", PathPlus, pal.MutedFg, pal.RowHover, false).Clicked() {
								s.NewTab()
							}
						})
					})
				})
			})

			// 2. 40pt Action Toolbar
			canWrite := s.CanWriteActive()
			sel := s.SelectedEntries()
			hasSel := len(sel) > 0
			singleSel := len(sel) == 1
			canEditSel := singleSel && !sel[0].IsDir && IsLikelyEditableName(sel[0].Name)
			canPaste := canWrite && s.Clipboard != nil && len(s.Clipboard.Paths) > 0

			ui.Row(c).Height(40).FillWidth().Padding(0, 12).Gap(4).AlignItems(ui.Center).Background(pal.Card).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Children(func() {
				if iconBtn(c, "tb-back", "后退", PathChevronLeft, pal.Foreground, pal.RowHover, !s.CanGoBack()).Clicked() {
					s.GoBack()
				}
				if iconBtn(c, "tb-fwd", "前进", PathChevronRight, pal.Foreground, pal.RowHover, !s.CanGoForward()).Clicked() {
					s.GoForward()
				}
				if iconBtn(c, "tb-up", "上一级", PathArrowUp, pal.Foreground, pal.RowHover, !s.CanGoUp()).Clicked() {
					s.GoUp()
				}
				if iconBtn(c, "tb-refresh", "刷新", PathRefresh, pal.Foreground, pal.RowHover, s.Offline || tab.Node == "").Clicked() {
					s.Refresh()
				}

				ui.Divider(c).Height(20).Margin(0, 4).Background(pal.Border)

				if iconBtn(c, "tb-copy", "复制", PathCopy, pal.Foreground, pal.RowHover, !hasSel).Clicked() {
					s.CopySelected()
				}
				if iconBtn(c, "tb-paste", "粘贴", PathClipboard, pal.Foreground, pal.RowHover, !canPaste).Clicked() {
					s.Paste()
				}
				if iconBtn(c, "tb-mkdir", "新建文件夹", PathFolderPlus, pal.Foreground, pal.RowHover, !canWrite).Clicked() {
					s.BeginMkdir()
				}
				editTip := "编辑文件"
				if !canWrite {
					editTip = "查看文件"
				}
				if iconBtn(c, "tb-edit", editTip, PathPencil, pal.Foreground, pal.RowHover, !canEditSel).Clicked() {
					s.OpenDocEditor()
				}
				if iconBtn(c, "tb-rename", "重命名", PathRename, pal.Foreground, pal.RowHover, !canWrite || !singleSel).Clicked() {
					s.BeginRename()
				}
				if iconBtn(c, "tb-delete", "删除", PathTrash, pal.Destructive, pal.RowHover, !canWrite || !hasSel).Clicked() {
					s.BeginDelete()
				}

				ui.Spacer(c)

				viewIcon := PathLayoutGrid
				viewTip := "切换图标视图"
				if s.ViewMode == ViewIcons {
					viewIcon = PathList
					viewTip = "切换列表视图"
				}
				if iconBtn(c, "tb-view", viewTip, viewIcon, pal.Foreground, pal.RowHover, false).Clicked() {
					if s.ViewMode == ViewList {
						s.SetViewMode(ViewIcons)
					} else {
						s.SetViewMode(ViewList)
					}
				}
				detColor := pal.MutedFg
				if s.DetailsOpen {
					detColor = pal.Primary
				}
				if iconBtn(c, "tb-details", "详情面板", PathPanelRight, detColor, pal.RowHover, false).Clicked() {
					s.ToggleDetails()
				}
				themeIcon := PathMoon
				if pal.Dark {
					themeIcon = PathSun
				}
				if iconBtn(c, "tb-theme", "切换深浅主题", themeIcon, pal.MutedFg, pal.RowHover, false).Clicked() {
					if pal.Dark {
						s.SetTheme(ThemePrefLight)
					} else {
						s.SetTheme(ThemePrefDark)
					}
				}
			})

			// 3. 44pt Breadcrumbs + Goto + Filter Bar
			ui.Row(c).Height(44).FillWidth().Padding(0, 12).Gap(8).AlignItems(ui.Center).Background(pal.Muted).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Children(func() {
				var roots []string
				if n, ok := s.NodeByID(tab.Node); ok {
					roots = n.ReadRoots
				}
				crumbs := BuildCrumbs(tab.Path, roots)
				ui.ScrollHorizontal(c).Grow(1).FillHeight().Children(func() {
					ui.Row(c).FillHeight().Gap(0).AlignItems(ui.Center).Children(func() {
						if tab.Node == "" {
							ui.Text(c, "新标签页 · 请选择设备").FontSize(12).TextColor(pal.MutedFg)
							return
						}
						renderBreadcrumbCrumbs(c, pal, crumbs, "crumb", func(p string) {
							_ = s.Navigate(p)
						})
					})
				})

				if tab.Node != "" {
					if ui.Button(c.Key("goto-btn"), "前往路径").Padding(4, 10).Height(28).FontSize(12).Clicked() {
						s.BeginGoto()
					}
				}
				ui.Row(c).Width(210).Height(28).AlignItems(ui.Center).Gap(6).Padding(0, 8).Background(pal.Card).BorderWidth(1).BorderColor(pal.Input).Radius(6).Children(func() {
					StrokeIcon(c, PathSearch, 13, pal.MutedFg)
					ui.TextInput(c.Key("filter-input"), &s.Filter).Placeholder("筛选当前目录…").Label("筛选当前目录").Bind(&loc.filter).Grow(1).BorderWidth(0).Background(ui.Transparent).Height(24).FontSize(12)
				})
			})

			// 4. Conditional Inline Editor Bar (Mkdir / Goto; Rename renders in-place)
			if s.Editor.Kind == EditorMkdir || s.Editor.Kind == EditorGoto {
				s.renderInlineEditor(c, pal, loc)
			}

			// 5. Center File Content + Right Details Pane
			ui.Row(c).Grow(1).FillWidth().Children(func() {
				zone := ui.Column(c).Grow(1).FillHeight().Background(pal.Card)
				if files := zone.DroppedFiles(); len(files) > 0 {
					s.HandleDroppedFiles(files)
				}
				if zone.FileDragOver() && canWrite {
					zone.BorderWidth(2).BorderColor(pal.Primary)
				}
				zone.Children(func() {
					s.renderFileBrowser(c, pal, metrics, loc, tab, canWrite, false)
				})

				if s.DetailsOpen {
					detW := ClampDetailsWidth(s.DetailsWidth)
					ui.Column(c).Width(detW).FillHeight().Shrink(0).Background(pal.Sidebar).BorderWidth(0, 0, 0, 1).BorderColor(pal.Border).Children(func() {
						s.renderDetailsPane(c, pal, metrics, loc, canWrite)
						renderPaneResizeHandle(c, "resize-details", pal, &loc.detailsDragging, true, -1, detW, DefaultDetailsWidth, s.SetDetailsWidth)
					})
				}
			})

			// 6. Transfer Progress Bar
			if len(s.Transfers) > 0 {
				s.renderTransferBar(c, pal, metrics)
			}

			// 7. 34pt Bottom Status Bar (aligned with left sidebar footer)
			s.renderStatusBar(c, pal, tab)
		})
	})
}

func (s *Session) renderInlineEditor(c *ui.Context, pal Palette, loc *viewLocalState) {
	label := "新建文件夹"
	btnLabel := "创建"
	if s.Editor.Kind == EditorGoto {
		label = "前往路径"
		btnLabel = "前往"
	}
	ui.Row(c).FillWidth().Padding(8, 12).Gap(8).AlignItems(ui.Center).Background(pal.Accent).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(pal.AccentFg).Shrink(0)
		in := ui.TextInput(c.Key("inline-editor-input"), &s.Editor.Text).Label(label).Bind(&loc.editorText).Grow(1).Height(28).FontSize(12)
		if s.Editor.NeedFocus {
			in.AutoFocus()
			s.Editor.NeedFocus = false
		}
		if in.Submitted() {
			_ = s.CommitEditor()
		}
		if s.Editor.Err != "" {
			ui.Text(c, s.Editor.Err).FontSize(12).TextColor(pal.Destructive)
		}
		if ui.PrimaryButton(c.Key("inline-editor-submit"), btnLabel).Height(28).Padding(0, 12).FontSize(12).Clicked() {
			_ = s.CommitEditor()
		}
		if ui.Button(c.Key("inline-editor-cancel"), "取消").Height(28).Padding(0, 10).FontSize(12).Clicked() {
			s.CancelEditor()
		}
	})
}

func (s *Session) renderFileBrowser(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState, tab *Tab, canWrite, mobileTouch bool) {
	if s.Offline {
		ui.Column(c).Fill().Center().Gap(12).Padding(24).Children(func() {
			StrokeIcon(c, PathServer, 36, pal.MutedFg)
			ui.Text(c, "未连接到后台 Agent").FontSize(15).FontWeight(600).TextColor(pal.Foreground)
			if s.OfflineErr != "" {
				ui.Text(c, s.OfflineErr).FontSize(12).TextColor(pal.MutedFg).MaxWidth(420)
			}
			ui.Row(c).Gap(8).Children(func() {
				if ui.PrimaryButton(c, "查看启动指南").Clicked() {
					s.OpenDaemonGuide()
				}
				if ui.Button(c, "打开设置").Clicked() {
					s.OpenSettings()
				}
			})
		})
		return
	}
	if tab.Node == "" {
		s.renderNewTabPicker(c, pal, loc, mobileTouch)
		return
	}
	if tab.Err != "" {
		ui.Column(c).Fill().Center().Gap(10).Padding(24).Children(func() {
			ui.Text(c, tab.Err).FontSize(13).TextColor(pal.Destructive)
			if ui.Button(c, "重试").Clicked() {
				s.Refresh()
			}
		})
		return
	}
	if tab.Loading && len(tab.Entries) == 0 {
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "正在加载目录…").FontSize(13).TextColor(pal.MutedFg)
		})
		return
	}

	if loc.marquee.active && (loc.marquee.node != tab.Node || loc.marquee.path != tab.Path || loc.marquee.viewMode != s.ViewMode) {
		loc.marquee.active = false
		loc.marquee.dragging = false
	}

	entries := s.SortedEntries()
	if len(entries) == 0 {
		msg := "此文件夹为空"
		if strings.TrimSpace(s.Filter) != "" {
			msg = "没有匹配的筛选结果"
		}
		if mobileTouch {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, msg).FontSize(13).TextColor(pal.MutedFg)
				if canWrite && strings.TrimSpace(s.Filter) == "" {
					ui.Text(c, "可从系统文件管理器直接拖入文件，或点击新建文件夹").FontSize(12).TextColor(pal.MutedFg)
				}
			})
			return
		}
		area := ui.Box(c.Key("empty-browser-area")).Grow(1).FillWidth()
		s.attachMarquee(c, pal, metrics, loc, area, &loc.listScroll, entries, metrics.ListRowH, false)
		area.Children(func() {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, msg).FontSize(13).TextColor(pal.MutedFg)
				if canWrite && strings.TrimSpace(s.Filter) == "" {
					ui.Text(c, "可从系统文件管理器直接拖入文件，或点击新建文件夹").FontSize(12).TextColor(pal.MutedFg)
				}
			})
		})
		return
	}

	if s.ViewMode == ViewIcons && !mobileTouch {
		cols := metrics.GridColumns(0)
		if w, _ := c.Size(); w > 0 {
			sideW := ClampSidebarWidth(s.SidebarWidth)
			if s.SidebarCollapsed {
				sideW = 56
			}
			avail := w - sideW
			if s.DetailsOpen {
				avail -= ClampDetailsWidth(s.DetailsWidth)
			}
			cols = metrics.GridColumns(avail)
		}
		area := ui.Box(c.Key("icon-grid-area")).Grow(1).FillWidth()
		vp := area.Bounds()
		s.stepMarqueeAutoScroll(c, loc, vp, &loc.gridScroll, entries, 0, true)
		loc.gridItems = loc.gridItems[:0]
		area.Children(func() {
			ui.Scroll(c.Key("icon-grid-scroll")).Fill().Padding(metrics.GridPad).TrackScroll(&loc.gridScroll).Children(func() {
				ui.Grid(c).Columns(cols).Gap(metrics.GridGap).Children(func() {
					for _, e := range entries {
						s.renderGridTile(c, pal, metrics, loc, e, canWrite, vp.X, vp.Y)
					}
				})
			})
			s.renderMarqueeOverlay(c, pal, loc, vp, &loc.gridScroll)
		})
		s.attachMarquee(c, pal, metrics, loc, area, &loc.gridScroll, entries, 0, true)
		return
	}

	if !mobileTouch {
		ui.Row(c).Height(metrics.ListHeaderH).FillWidth().Padding(0, metrics.ListPadX).Gap(metrics.ListGap).AlignItems(ui.Center).Background(pal.Muted).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Children(func() {
			nameTitle := "名称"
			sizeTitle := "大小"
			timeTitle := "修改时间"
			arrow := " ↑"
			if !s.SortAsc {
				arrow = " ↓"
			}
			switch s.SortKey {
			case SortName:
				nameTitle += arrow
			case SortSize:
				sizeTitle += arrow
			case SortTime:
				timeTitle += arrow
			}
			if ui.Button(c.Key("sort-name"), nameTitle).Background(ui.Transparent).BorderWidth(0).Padding(0, 4).FontSize(11).TextColor(pal.MutedFg).Grow(1).Justify(ui.Start).Clicked() {
				s.ToggleSort(SortName)
			}
			if ui.Button(c.Key("sort-size"), sizeTitle).Width(metrics.ListSizeColW).Background(ui.Transparent).BorderWidth(0).Padding(0, 4).FontSize(11).TextColor(pal.MutedFg).Justify(ui.End).Clicked() {
				s.ToggleSort(SortSize)
			}
			if ui.Button(c.Key("sort-time"), timeTitle).Width(metrics.ListTimeColW).Background(ui.Transparent).BorderWidth(0).Padding(0, 4).FontSize(11).TextColor(pal.MutedFg).Justify(ui.End).Clicked() {
				s.ToggleSort(SortTime)
			}
		})
	}

	rowH := metrics.ListRowH
	if mobileTouch {
		rowH = metrics.MobileListRowH
	}
	listState := ui.Local(c.Root(), "file-list-state", func() ui.ListState {
		return ui.ListState{}
	})
	if loc.lastListDensity != "" && loc.lastListDensity != metrics.Resolved {
		// Flush MyGo's 64-row height chunk cache so scroll estimates match the new rowH immediately.
		*listState = ui.ListState{}
	}
	loc.lastListDensity = metrics.Resolved
	listState.Key = func(i int) any {
		if i >= 0 && i < len(entries) {
			return entries[i].Path
		}
		return i
	}
	listState.Label = func(i int) string {
		if i >= 0 && i < len(entries) {
			return entries[i].Name
		}
		return ""
	}
	if mobileTouch {
		ui.List(c.Key("file-list"), listState, len(entries), func(i int) {
			if i < 0 || i >= len(entries) {
				return
			}
			e := entries[i]
			s.renderFileRow(c, pal, metrics, loc, e, rowH, canWrite, mobileTouch, 0, 0)
		}).Grow(1).FillWidth()
		return
	}

	dirKey := tab.Node + "\x00" + tab.Path
	if loc.listNameDir != dirKey {
		loc.listNameDir = dirKey
		loc.listNameRight = map[string]float32{}
	}
	area := ui.Box(c.Key("file-list-area")).Grow(1).FillWidth()
	vp := area.Bounds()
	s.stepMarqueeAutoScroll(c, loc, vp, &loc.listScroll, entries, rowH, false)
	area.Children(func() {
		ui.List(c.Key("file-list"), listState, len(entries), func(i int) {
			if i < 0 || i >= len(entries) {
				return
			}
			e := entries[i]
			s.renderFileRow(c, pal, metrics, loc, e, rowH, canWrite, false, vp.X, vp.W)
		}).Grow(1).FillWidth().TrackScroll(&loc.listScroll)
		s.renderMarqueeOverlay(c, pal, loc, vp, &loc.listScroll)
	})
	s.attachMarquee(c, pal, metrics, loc, area, &loc.listScroll, entries, rowH, false)
}

func (s *Session) marqueeItemsForView(loc *viewLocalState, entries []agent.Entry, rowH, vpW float32, isGrid bool) []MarqueeItem {
	if isGrid {
		return loc.gridItems
	}
	return ListMarqueeItems(entries, rowH, vpW)
}

func (s *Session) stepMarqueeAutoScroll(c *ui.Context, loc *viewLocalState, vp ui.Rect, scroll *ui.ScrollState, entries []agent.Entry, rowH float32, isGrid bool) {
	if !loc.marquee.dragging || vp.H <= 0 || scroll == nil {
		return
	}
	edge := min(float32(28), vp.H/3)
	dy := float32(0)
	if loc.marquee.curY < edge && scroll.Y > 0 {
		dy = -min(max(1, (edge-loc.marquee.curY)*0.5), 16)
	} else if loc.marquee.curY > vp.H-edge && scroll.Y < scroll.MaxY {
		dy = min(max(1, (loc.marquee.curY-(vp.H-edge))*0.5), 16)
	}
	if dy == 0 {
		return
	}
	next := max(0, min(scroll.Y+dy, scroll.MaxY))
	if next == scroll.Y {
		return
	}
	scroll.Y = next
	box := DragHitBox(loc.marquee.startX, loc.marquee.startY, loc.marquee.curX+scroll.X, loc.marquee.curY+scroll.Y)
	items := s.marqueeItemsForView(loc, entries, rowH, vp.W, isGrid)
	sel, anchor := ComputeMarqueeSelection(loc.marquee.baseSelected, loc.marquee.mode, box, items)
	s.ApplyMarqueeSelection(sel, anchor)
	c.After(16 * time.Millisecond)
}

func (s *Session) renderMarqueeOverlay(c *ui.Context, pal Palette, loc *viewLocalState, vp ui.Rect, scroll *ui.ScrollState) {
	if !loc.marquee.dragging || vp.W <= 0 || vp.H <= 0 {
		return
	}
	sx, sy := float32(0), float32(0)
	if scroll != nil {
		sx, sy = scroll.X, scroll.Y
	}
	vis := ClampRect(DragHitBox(loc.marquee.startX-sx, loc.marquee.startY-sy, loc.marquee.curX, loc.marquee.curY), vp.W, vp.H)
	if vis.W < 1 || vis.H < 1 {
		return
	}
	fill := ui.RGBA(37, 99, 235, 0.14)
	if pal.Dark {
		fill = ui.RGBA(79, 141, 247, 0.20)
	}
	ui.Box(c.Key("marquee-box")).
		Absolute().
		Left(vis.X).
		Top(vis.Y).
		Size(vis.W, vis.H).
		Background(fill).
		BorderWidth(1).
		BorderColor(pal.Primary).
		Radius(2).
		IgnoreInput(true).
		ZIndex(10)
}

func (s *Session) attachMarquee(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState, area ui.Element, scroll *ui.ScrollState, entries []agent.Entry, rowH float32, isGrid bool) {
	vp := area.Bounds()
	area.HandleInput(func(ev ui.InputEvent) bool {
		sx, sy := float32(0), float32(0)
		if scroll != nil {
			sx, sy = scroll.X, scroll.Y
		}
		updateSelection := func(m marqueeState) {
			box := DragHitBox(m.startX, m.startY, m.curX+sx, m.curY+sy)
			items := s.marqueeItemsForView(loc, entries, rowH, vp.W, isGrid)
			sel, anchor := ComputeMarqueeSelection(m.baseSelected, m.mode, box, items)
			s.ApplyMarqueeSelection(sel, anchor)
		}
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button != 0 || s.Modal != ModalNone || s.ActiveConflict() != nil {
				return false
			}
			cx := ev.X + sx
			cy := ev.Y + sy
			var hitEntry *agent.Entry
			if isGrid {
				for _, it := range loc.gridItems {
					if RectContainsPoint(it.Rect, cx, cy) {
						return false
					}
				}
			} else if rowH > 0 && cy >= 0 {
				idx := int(cy / rowH)
				if idx >= 0 && idx < len(entries) {
					e := entries[idx]
					if s.Editor.Kind == EditorRename && s.Editor.TargetPath == e.Path {
						return false
					}
					nameEnd := loc.listNameRight[e.Path] + 8
					if loc.listNameRight[e.Path] <= 0 {
						nameEnd = max(metrics.ListPadX, vp.W-metrics.ListMetaReservedW())
					}
					if ev.X >= metrics.ListPadX && ev.X <= nameEnd {
						return false
					}
					hitCopy := e
					hitEntry = &hitCopy
				}
			}
			tab := s.Active()
			base := make(map[string]bool, len(tab.Selected))
			for p, ok := range tab.Selected {
				if ok {
					base[p] = true
				}
			}
			mode := MarqueeReplace
			if ev.Mods&ui.Cmd != 0 {
				mode = MarqueeToggle
			} else if ev.Mods&ui.Shift != 0 {
				mode = MarqueeAdd
			}
			loc.marquee = marqueeState{
				active:       true,
				dragging:     false,
				node:         tab.Node,
				path:         tab.Path,
				viewMode:     s.ViewMode,
				mode:         mode,
				pressMods:    ev.Mods,
				pressClicks:  ev.Clicks,
				hitEntry:     hitEntry,
				startX:       cx,
				startY:       cy,
				curX:         ev.X,
				curY:         ev.Y,
				baseSelected: base,
				baseAnchor:   tab.SelectAnchor,
			}
			return true
		case ui.InputPointerMove:
			if !loc.marquee.active {
				return false
			}
			loc.marquee.curX = ev.X
			loc.marquee.curY = ev.Y
			dx := (ev.X + sx) - loc.marquee.startX
			dy := (ev.Y + sy) - loc.marquee.startY
			if !loc.marquee.dragging && dx*dx+dy*dy >= 16 {
				loc.marquee.dragging = true
			}
			if loc.marquee.dragging {
				updateSelection(loc.marquee)
			}
			return true
		case ui.InputPointerUp:
			if !loc.marquee.active {
				return false
			}
			m := loc.marquee
			loc.marquee.active = false
			loc.marquee.dragging = false
			if ev.Canceled {
				if m.dragging {
					s.ApplyMarqueeSelection(m.baseSelected, m.baseAnchor)
				}
				return true
			}
			if m.dragging {
				m.curX = ev.X
				m.curY = ev.Y
				updateSelection(m)
				return true
			}
			if m.hitEntry != nil {
				if m.pressClicks >= 2 {
					s.ActivateEntry(*m.hitEntry)
				} else {
					s.SelectEntry(m.hitEntry.Path, m.pressMods&ui.Cmd != 0, m.pressMods&ui.Shift != 0)
				}
			} else {
				if s.Editor.Kind == EditorRename {
					s.CommitOrCancelRenameOnBlur()
				}
				if m.pressMods&(ui.Cmd|ui.Shift) == 0 {
					s.ClearSelection()
				}
			}
			return true
		}
		return false
	})
}

func (s *Session) renderInPlaceRenameInput(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState, e agent.Entry, compactGrid, mobileTouch bool) {
	borderCol := pal.Primary
	if s.Editor.Err != "" {
		borderCol = pal.Destructive
	}
	wrap := ui.Row(c.Key("rename-wrap-" + e.Path)).AlignItems(ui.Center).Gap(4)
	if compactGrid {
		wrap.FillWidth()
	} else {
		wrap.Grow(1)
	}
	acted := false
	wrap.Children(func() {
		in := ui.TextInput(c.Key("inline-editor-input"), &s.Editor.Text).
			Label("重命名").
			Bind(&loc.editorText).
			Background(pal.Card).
			BorderWidth(1.5).
			BorderColor(borderCol)
		if compactGrid {
			in.FillWidth().Height(24).Padding(0, 4).FontSize(12).Radius(4)
		} else if mobileTouch {
			in.Grow(1).Height(32).Padding(0, 8).FontSize(13).Radius(6)
		} else {
			in.Grow(1).Height(metrics.ListRenameH).Padding(0, 6).FontSize(13).Radius(4)
		}
		if s.Editor.NeedFocus {
			in.AutoFocus()
			in.SetTextSelection(0, RenameStemRuneLen(e.Name, e.IsDir))
			s.Editor.NeedFocus = false
		}
		if in.Changed() && s.Editor.Err != "" {
			s.Editor.Err = ""
		}
		if mobileTouch {
			if ui.PrimaryButton(c.Key("inline-editor-submit"), "✓").Height(28).Padding(0, 8).FontSize(12).Clicked() {
				_ = s.CommitEditor()
				acted = true
			}
			if ui.Button(c.Key("inline-editor-cancel"), "✕").Height(28).Padding(0, 8).FontSize(12).Clicked() {
				s.CancelEditor()
				acted = true
			}
		}
		if !acted && in.Submitted() {
			_ = s.CommitEditor()
			acted = true
		}
	})
	if !acted && s.Editor.Kind == EditorRename && (wrap.PressedOutside() || wrap.FocusLeft()) {
		s.CommitOrCancelRenameOnBlur()
	}
}

func (s *Session) renderFileRow(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState, e agent.Entry, rowH float32, canWrite, mobileTouch bool, listVPX, listVPW float32) {
	tab := s.Active()
	selected := tab.Selected[e.Path]
	isRenaming := s.Editor.Kind == EditorRename && s.Editor.TargetPath == e.Path
	bg := ui.Transparent
	if selected {
		bg = pal.Selection
	}
	row := ui.Row(c.Key("row-"+e.Path)).Height(rowH).FillWidth().Padding(0, metrics.ListPadX).Gap(metrics.ListGap).AlignItems(ui.Center).Background(bg)
	row.Label(e.Name).Role(ui.RoleListItem)
	if row.Hovered() && !selected {
		row.Background(pal.RowHover)
	}
	kind := ClassifyFile(e.Name, e.IsDir)
	row.Children(func() {
		TypedFileIcon(c, kind, metrics.ListIconSize)
		if isRenaming {
			s.renderInPlaceRenameInput(c, pal, metrics, loc, e, false, mobileTouch)
		} else {
			var nameEl ui.Element
			ui.Row(c).Grow(1).AlignItems(ui.Center).Children(func() {
				nameEl = ui.Text(c, e.Name).FontSize(13).TextColor(pal.Foreground).Shrink(1).SingleLine().Ellipsis("…")
			})
			if !mobileTouch && listVPW > 0 {
				if nb := nameEl.Bounds(); nb.W > 0 {
					if loc.listNameRight == nil {
						loc.listNameRight = map[string]float32{}
					}
					loc.listNameRight[e.Path] = (nb.X + nb.W) - listVPX
				}
			}
		}
		if !mobileTouch {
			sz := "--"
			if !e.IsDir {
				sz = FormatSize(e.Size)
			}
			ui.Text(c, sz).Width(metrics.ListSizeColW).TextAlign(ui.End).FontSize(12).TextColor(pal.MutedFg).SingleLine()
			ui.Text(c, FormatTime(e.ModTime)).Width(metrics.ListTimeColW).TextAlign(ui.End).FontSize(12).TextColor(pal.MutedFg).SingleLine()
		} else {
			if !e.IsDir {
				ui.Text(c, FormatSize(e.Size)).FontSize(12).TextColor(pal.MutedFg).SingleLine()
			}
		}
	})

	if isRenaming {
		return
	}

	row.DragDataFrom(func() transfer.Data {
		if !tab.Selected[e.Path] {
			s.SelectEntry(e.Path, false, false)
		}
		paths := s.PrepareDragFiles()
		if len(paths) == 0 {
			return transfer.Data{}
		}
		data, err := transfer.FileData(paths...)
		if err != nil {
			return transfer.Data{}
		}
		return data
	})
	row.PopoverMenu(func(m *ui.Menu) {
		s.buildEntryContextMenu(m, e, canWrite)
	})

	if row.DoubleClicked() {
		s.ActivateEntry(e)
		return
	}
	if row.Clicked() {
		mods := row.ClickModifiers()
		ctrl := mods&ui.Cmd != 0
		shift := mods&ui.Shift != 0
		if mobileTouch && !ctrl && !shift {
			if e.IsDir {
				_ = s.Navigate(e.Path)
			} else {
				s.SelectEntry(e.Path, false, false)
				s.mobileDetailsOpen = true
			}
			return
		}
		s.SelectEntry(e.Path, ctrl, shift)
	}
}

func (s *Session) renderGridTile(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState, e agent.Entry, canWrite bool, gridVPX, gridVPY float32) {
	tab := s.Active()
	selected := tab.Selected[e.Path]
	isRenaming := s.Editor.Kind == EditorRename && s.Editor.TargetPath == e.Path
	bg := ui.Transparent
	if selected {
		bg = pal.Selection
	}
	tile := ui.Column(c.Key("tile-"+e.Path)).Size(metrics.GridTileW, metrics.GridTileH).Padding(metrics.GridTilePadV, metrics.GridTilePadH).Gap(metrics.GridTilePadH).Radius(8).AlignItems(ui.Center).Justify(ui.Center).Background(bg)
	tile.Label(e.Name).Role(ui.RoleListItem)
	if tile.Hovered() && !selected {
		tile.Background(pal.RowHover)
	}
	kind := ClassifyFile(e.Name, e.IsDir)
	tile.Children(func() {
		TypedFileIcon(c, kind, metrics.GridIconSize)
		if isRenaming {
			s.renderInPlaceRenameInput(c, pal, metrics, loc, e, true, false)
		} else {
			ui.Text(c, e.Name).FontSize(12).TextColor(pal.Foreground).MaxLines(2).TextAlign(ui.Center).FillWidth()
		}
	})
	if tb := tile.Bounds(); tb.W > 0 && tb.H > 0 {
		loc.gridItems = append(loc.gridItems, MarqueeItem{
			Path: e.Path,
			Rect: MarqueeRect{
				X: tb.X - gridVPX + loc.gridScroll.X,
				Y: tb.Y - gridVPY + loc.gridScroll.Y,
				W: tb.W,
				H: tb.H,
			},
		})
	}
	if isRenaming {
		return
	}
	tile.DragDataFrom(func() transfer.Data {
		if !tab.Selected[e.Path] {
			s.SelectEntry(e.Path, false, false)
		}
		paths := s.PrepareDragFiles()
		if len(paths) == 0 {
			return transfer.Data{}
		}
		data, err := transfer.FileData(paths...)
		if err != nil {
			return transfer.Data{}
		}
		return data
	})
	tile.PopoverMenu(func(m *ui.Menu) {
		s.buildEntryContextMenu(m, e, canWrite)
	})
	if tile.DoubleClicked() {
		s.ActivateEntry(e)
		return
	}
	if tile.Clicked() {
		mods := tile.ClickModifiers()
		s.SelectEntry(e.Path, mods&ui.Cmd != 0, mods&ui.Shift != 0)
	}
}

func (s *Session) buildEntryContextMenu(m *ui.Menu, e agent.Entry, canWrite bool) {
	tab := s.Active()
	if !tab.Selected[e.Path] {
		s.SelectEntry(e.Path, false, false)
	}
	if m.Item("打开").Chosen() {
		s.ActivateEntry(e)
	}
	if !e.IsDir && IsLikelyEditableName(e.Name) {
		editLabel := "编辑"
		if !canWrite {
			editLabel = "查看全文"
		}
		if m.Item(editLabel).Chosen() {
			s.OpenDocEditor()
		}
	}
	if m.Item("复制").Shortcut(ui.Cmd, ui.KeyC).Chosen() {
		s.CopySelected()
	}
	canPaste := canWrite && s.Clipboard != nil && len(s.Clipboard.Paths) > 0
	if m.Item("粘贴到此目录").Disabled(!canPaste).Shortcut(ui.Cmd, ui.KeyV).Chosen() {
		s.Paste()
	}
	m.Separator()
	if m.Item("新建文件夹").Disabled(!canWrite).Chosen() {
		s.BeginMkdir()
	}
	if m.Item("重命名").Disabled(!canWrite).Shortcut(0, ui.KeyF2).Chosen() {
		s.BeginRename()
	}
	if m.Item("删除").Disabled(!canWrite).Shortcut(0, ui.KeyDelete).Chosen() {
		s.BeginDelete()
	}
	m.Separator()
	if m.Item("刷新").Shortcut(ui.Cmd, ui.KeyR).Chosen() {
		s.Refresh()
	}
}

func (s *Session) renderDetailsPane(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState, canWrite bool) {
	sel := s.SelectedEntries()
	tab := s.Active()
	ui.Column(c).Fill().Padding(metrics.DetailsPad).Gap(metrics.DetailsGap).Children(func() {
		ui.Text(c, "详情与预览").FontSize(12).FontWeight(600).TextColor(pal.MutedFg)
		if len(sel) == 0 {
			ui.Column(c).Grow(1).Center().Gap(6).Children(func() {
				ui.Text(c, "未选择项目").FontSize(13).TextColor(pal.MutedFg)
				ui.Text(c, fmt.Sprintf("当前目录共 %d 项", len(tab.Entries))).FontSize(12).TextColor(pal.MutedFg)
			})
			return
		}
		if len(sel) > 1 {
			var total int64
			for _, item := range sel {
				total += item.Size
			}
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, fmt.Sprintf("已选择 %d 项", len(sel))).FontSize(14).FontWeight(600)
				ui.Text(c, "总大小 "+FormatSize(total)).FontSize(12).TextColor(pal.MutedFg)
			})
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "复制").Clicked() {
					s.CopySelected()
				}
				if ui.Button(c, "删除").Disabled(!canWrite).Clicked() {
					s.BeginDelete()
				}
			})
			return
		}

		e := sel[0]
		kind := ClassifyFile(e.Name, e.IsDir)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			TypedFileIcon(c, kind, 34)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, e.Name).FontSize(14).FontWeight(600).MaxLines(2)
				ui.Text(c, TypeLabel(kind)).FontSize(11).TextColor(pal.MutedFg)
			})
		})

		ui.Divider(c).Background(pal.Border)

		// Preview block
		if prev := tab.Preview; prev != nil && prev.Path == e.Path {
			switch {
			case prev.Loading:
				ui.Text(c, "正在加载预览…").FontSize(12).TextColor(pal.MutedFg)
			case prev.Err != "":
				ui.Text(c, prev.Err).FontSize(12).TextColor(pal.Destructive)
			case prev.Kind == "image" && prev.Image != nil:
				bKey := fmt.Sprintf("%s|%s|%d", prev.Node, prev.Path, len(prev.ImageBytes))
				if loc.bitmapKey != bKey || loc.bitmap == nil {
					loc.bitmap = ui.NewBitmap(prev.Image)
					loc.bitmapKey = bKey
				}
				if loc.bitmap != nil {
					ui.Image(c, loc.bitmap).FillWidth().Height(180).Fit(ui.Contain).Radius(8).Background(pal.Card)
				}
			case prev.Text != "":
				ui.Scroll(c).MaxHeight(220).FillWidth().Padding(8).Radius(6).Background(pal.Card).BorderWidth(1).BorderColor(pal.Border).Children(func() {
					ui.Text(c, prev.Text).FontSize(11).TextColor(pal.Foreground).Selectable()
				})
				if prev.Truncated {
					ui.Text(c, "仅显示前段预览内容").FontSize(11).TextColor(pal.MutedFg)
				}
			}
		}

		ui.Column(c).Gap(metrics.DetailsMetaGap).Children(func() {
			if !e.IsDir {
				detailRow(c, "大小", FormatSize(e.Size), pal)
			}
			detailRow(c, "修改时间", FormatTime(e.ModTime), pal)
			detailRow(c, "路径", e.Path, pal)
		})

		ui.Spacer(c)

		ui.Row(c).Wrap().Gap(6).Children(func() {
			if !e.IsDir {
				if ui.PrimaryButton(c.Key("det-open"), "打开").Height(30).Padding(0, 12).FontSize(12).Clicked() {
					s.OpenSelectedInOS()
				}
				if IsLikelyEditableName(e.Name) {
					editLabel := "编辑"
					if !canWrite {
						editLabel = "查看全文"
					}
					if ui.Button(c.Key("det-edit"), editLabel).Height(30).Padding(0, 10).FontSize(12).Clicked() {
						s.OpenDocEditor()
					}
				}
			}
			if ui.Button(c.Key("det-copy"), "复制").Height(30).Padding(0, 10).FontSize(12).Clicked() {
				s.CopySelected()
			}
			if ui.Button(c.Key("det-rename"), "重命名").Disabled(!canWrite).Height(30).Padding(0, 10).FontSize(12).Clicked() {
				s.BeginRename()
			}
			if ui.Button(c.Key("det-delete"), "删除").Disabled(!canWrite).Height(30).Padding(0, 10).FontSize(12).Clicked() {
				s.BeginDelete()
			}
		})
	})
}

func detailRow(c *ui.Context, k, v string, pal Palette) {
	ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Text(c, k).Width(56).FontSize(11).TextColor(pal.MutedFg).Shrink(0)
		ui.Text(c, v).FontSize(11).TextColor(pal.Foreground).Grow(1)
	})
}

func (s *Session) renderTransferBar(c *ui.Context, pal Palette, metrics LayoutMetrics) {
	ui.Column(c).FillWidth().Background(pal.Sidebar).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Padding(metrics.TransferPadV, metrics.TransferPadH).Gap(metrics.TransferGap).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			activeCnt := 0
			for _, tr := range s.Transfers {
				if tr.Job.Status == "running" || tr.Job.Status == "queued" || tr.Job.Status == "conflict" {
					activeCnt++
				}
			}
			summary := fmt.Sprintf("传输队列：%d 个任务（%d 进行中）", len(s.Transfers), activeCnt)
			ui.Text(c, summary).FontSize(12).FontWeight(600).TextColor(pal.Foreground).Grow(1)
			if activeCnt < len(s.Transfers) {
				if ui.Button(c.Key("clear-transfers"), "清除记录").Height(24).Padding(0, 8).FontSize(11).Clicked() {
					s.ClearFinishedTransfers()
				}
			}
			btnLabel := "展开"
			if s.transfersExpanded {
				btnLabel = "收起"
			}
			if ui.Button(c.Key("toggle-transfers"), btnLabel).Height(24).Padding(0, 8).FontSize(11).Clicked() {
				s.transfersExpanded = !s.transfersExpanded
			}
		})

		limit := len(s.Transfers)
		if !s.transfersExpanded && limit > 2 {
			limit = 2
		}
		for i := len(s.Transfers) - 1; i >= len(s.Transfers)-limit; i-- {
			tr := s.Transfers[i]
			pct := 0.0
			if tr.Job.BytesTotal > 0 {
				pct = float64(tr.Job.BytesDone) / float64(tr.Job.BytesTotal)
			} else if tr.Job.Status == "completed" {
				pct = 1.0
			}
			ui.Row(c.Key("tr-" + tr.ID)).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("%s → %s · %s", tr.FromNode, tr.ToNode, filepath.Base(tr.FromPath))).FontSize(11).TextColor(pal.Foreground).Width(220).SingleLine().Ellipsis("…")
				ui.Progress(c, pct).Grow(1)
				ui.Text(c, transferStatusLabel(tr.Job.Status)).FontSize(11).TextColor(pal.MutedFg).Width(64).TextAlign(ui.End)
				if tr.Job.Status == "running" || tr.Job.Status == "queued" {
					if ui.Button(c.Key("tr-cancel-"+tr.ID), "取消").Height(22).Padding(0, 8).FontSize(11).Clicked() {
						s.CancelTransfer(tr.ID)
					}
				} else {
					if tr.Job.Status == "canceled" || tr.Job.Status == "error" || tr.Job.Status == "interrupted" {
						if ui.Button(c.Key("tr-resume-"+tr.ID), "恢复").Height(22).Padding(0, 8).FontSize(11).Clicked() {
							s.ResumeTransfer(tr.ID)
						}
					}
					if tr.Job.Status != "conflict" {
						if iconBtn(c, "tr-dismiss-"+tr.ID, "移除记录", PathX, pal.MutedFg, pal.RowHover, false).Clicked() {
							s.DismissTransfer(tr.ID)
						}
					}
				}
			})
		}
	})
}

func transferStatusLabel(status string) string {
	switch status {
	case "queued":
		return "排队中"
	case "running":
		return "传输中"
	case "completed":
		return "已完成"
	case "canceled":
		return "已取消"
	case "error":
		return "失败"
	case "interrupted":
		return "已中断"
	case "conflict":
		return "待确认"
	default:
		return status
	}
}

func (s *Session) renderStatusBar(c *ui.Context, pal Palette, tab *Tab) {
	ui.Row(c).Height(34).FillWidth().Padding(0, 12).Gap(12).AlignItems(ui.Center).Background(pal.Muted).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Children(func() {
		left := fmt.Sprintf("%d 项", len(s.SortedEntries()))
		if selCnt := len(s.SelectedEntries()); selCnt > 0 {
			left += fmt.Sprintf(" · 已选 %d 项", selCnt)
		}
		ui.Text(c, left).FontSize(11).TextColor(pal.MutedFg)

		if s.Editor.Kind == EditorRename && s.Editor.Err != "" {
			ui.Text(c, s.Editor.Err).FontSize(11).TextColor(pal.Destructive).Grow(1).SingleLine().Ellipsis("…")
		} else if s.StatusMsg != "" {
			col := pal.MutedFg
			if s.StatusIsErr {
				col = pal.Destructive
			}
			ui.Text(c, s.StatusMsg).FontSize(11).TextColor(col).Grow(1).SingleLine().Ellipsis("…")
		} else {
			ui.Spacer(c)
		}

		right := s.sidebarStatusText()
		if n, ok := s.ActiveNode(); ok {
			perm := "只读"
			if !n.ReadOnly {
				perm = "可写"
			}
			right = fmt.Sprintf("%s (%s)", n.Name, perm)
		}
		ui.Text(c, right).FontSize(11).TextColor(pal.MutedFg)
	})
}

func (s *Session) sidebarStatusText() string {
	if s.Offline || s.Conn == nil {
		return "未连接"
	}
	if s.Conn.Mode == agent.ConnDemo {
		return "演示模式"
	}
	online, total := s.OnlineNodeCounts()
	if total > 0 {
		return fmt.Sprintf("已连接 · %d/%d 在线", online, total)
	}
	return "已连接"
}

func renderPaneResizeHandle(c *ui.Context, key string, pal Palette, dragging *bool, anchorStart bool, deltaSign float32, currentWidth, defaultWidth float32, onResize func(w float32, persist bool)) {
	edge := ui.Box(c.Key(key)).Absolute().Top(0).Bottom(0).Width(6).ZIndex(2).Cursor(ui.CursorResizeEW)
	if anchorStart {
		edge.Start(0)
	} else {
		edge.End(0)
	}
	dx, _, held := edge.Dragged()
	if held || edge.Hovered() {
		edge.BorderColor(pal.Primary)
		if anchorStart {
			edge.BorderWidth(0, 0, 0, 2)
		} else {
			edge.BorderWidth(0, 2, 0, 0)
		}
	}
	if edge.DoubleClicked() {
		*dragging = false
		onResize(defaultWidth, true)
		return
	}
	if held {
		*dragging = true
		if dx != 0 {
			onResize(currentWidth+deltaSign*dx, false)
		}
	} else if *dragging {
		*dragging = false
		onResize(currentWidth, true)
	}
}

func (s *Session) renderModals(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState) {
	if s.Modal != ModalDocEdit {
		loc.docWasFindOpen = false
		loc.docInsertTab = false
	}

	// 1. Non-dismissible Conflict Dialog (highest priority)
	if cp := s.ActiveConflict(); cp != nil {
		s.conflictModalOpen = true
		m := ui.Modal(c, &s.conflictModalOpen, func() {
			ui.Column(c).Width(420).Gap(12).Children(func() {
				ui.Text(c, "目标文件已存在").FontSize(16).FontWeight(600).TextColor(pal.Foreground)
				ui.Text(c, cp.Subtitle).FontSize(12).TextColor(pal.MutedFg)
				ui.Column(c).Padding(10).Radius(8).Background(pal.Muted).Gap(4).Children(func() {
					ui.Text(c, cp.File.Path).FontSize(12).FontWeight(600).TextColor(pal.Foreground)
					if cp.File.Size > 0 {
						ui.Text(c, "目标大小: "+FormatSize(cp.File.Size)).FontSize(11).TextColor(pal.MutedFg)
					}
				})
				ui.Checkbox(c.Key("conflict-apply-all"), &s.ConflictApplyAll, "对后续冲突应用相同操作")
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c.Key("conflict-cancel"), "取消任务").Clicked() {
						s.AnswerConflict("cancel")
					}
					if ui.Button(c.Key("conflict-skip"), "跳过").Clicked() {
						s.AnswerConflict("skip")
					}
					if ui.PrimaryButton(c.Key("conflict-overwrite"), "覆盖").Clicked() {
						s.AnswerConflict("overwrite")
					}
				})
			})
		})
		if m.Dismissed() || !s.conflictModalOpen {
			s.conflictModalOpen = true
		}
		return
	}

	// 2. Delete Confirmation Modal
	if s.Modal == ModalDelete {
		s.deleteModalOpen = true
		ui.Modal(c, &s.deleteModalOpen, func() {
			ui.Column(c).Width(380).Gap(12).Children(func() {
				ui.Text(c, "确认删除").FontSize(16).FontWeight(600).TextColor(pal.Foreground)
				msg := fmt.Sprintf("确定要删除选中的 %d 个项目吗？此操作不可撤销。", len(s.DeleteTargets))
				if len(s.DeleteTargets) == 1 {
					msg = fmt.Sprintf("确定要永久删除 “%s” 吗？", s.DeleteTargets[0].Name)
				}
				ui.Text(c, msg).FontSize(13).TextColor(pal.MutedFg)
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c.Key("del-cancel"), "取消").Clicked() {
						s.CancelDelete()
					}
					if ui.PrimaryButton(c.Key("del-confirm"), "删除").Background(pal.Destructive).Clicked() {
						s.ConfirmDelete()
					}
				})
			})
		})
		return
	}

	// 3. Built-in Text Editor / Full Viewer Modal
	if s.Modal == ModalDocEdit {
		s.renderDocEditModal(c, pal, loc)
		return
	}

	// 4. Settings Modal
	if s.Modal == ModalSettings {
		s.settingsModalOpen = true
		modalW := float32(460)
		if w, _ := c.Size(); w > 0 && w-48 < modalW {
			modalW = max(float32(300), w-48)
		}
		ui.Modal(c, &s.settingsModalOpen, func() {
			ui.Column(c).Width(modalW).Gap(12).Children(func() {
				ui.Text(c, "设置").FontSize(16).FontWeight(600).TextColor(pal.Foreground)

				// Theme selector (applies immediately)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "主题外观").Width(90).FontSize(12).TextColor(pal.MutedFg)
					for _, opt := range []struct {
						pref  ThemePref
						label string
					}{
						{ThemePrefSystem, "跟随系统"},
						{ThemePrefLight, "浅色"},
						{ThemePrefDark, "深色"},
					} {
						b := ui.Button(c.Key("theme-"+string(opt.pref)), opt.label).FontSize(12).Padding(4, 10)
						if s.Theme == opt.pref {
							b.Background(pal.Primary).TextColor(pal.PrimaryFg)
						}
						if b.Clicked() {
							s.SetTheme(opt.pref)
						}
					}
				})

				// Layout density selector (applies immediately)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "布局密度").Width(90).FontSize(12).TextColor(pal.MutedFg).Shrink(0)
					ui.Row(c).Grow(1).Wrap().Gap(6).AlignItems(ui.Center).Children(func() {
						for _, opt := range []DensityPref{
							DensityPrefAuto,
							DensityPrefCompact,
							DensityPrefStandard,
							DensityPrefComfortable,
						} {
							b := ui.Button(c.Key("density-"+string(opt)), opt.Label()).FontSize(12).Padding(4, 10)
							if s.Density == opt {
								b.Background(pal.Primary).TextColor(pal.PrimaryFg)
							}
							if b.Clicked() {
								s.SetDensity(opt)
							}
						}
						if s.Density == DensityPrefAuto {
							ui.Text(c, "当前采用"+metrics.Resolved.Label()).FontSize(11).TextColor(pal.MutedFg)
						}
					})
				})

				if runtime.GOOS != "ios" && runtime.GOOS != "android" {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "关闭窗口").Width(90).FontSize(12).TextColor(pal.MutedFg)
						for _, opt := range []struct {
							pref  CloseActionPref
							label string
						}{
							{CloseActionAsk, "每次询问"},
							{CloseActionTray, "最小化到托盘"},
							{CloseActionQuit, "直接退出"},
						} {
							b := ui.Button(c.Key("close-pref-"+string(opt.pref)), opt.label).FontSize(12).Padding(4, 10)
							if s.CloseAction == opt.pref {
								b.Background(pal.Primary).TextColor(pal.PrimaryFg)
							}
							if b.Clicked() {
								s.SetCloseAction(opt.pref)
							}
						}
					})
				}

				ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Text(c, "目录树").Width(90).Padding(2, 0, 0, 0).FontSize(12).TextColor(pal.MutedFg).Shrink(0)
					ui.Column(c).Grow(1).Gap(6).Children(func() {
						syncTree := s.SyncSidebarTree
						if ui.Checkbox(c.Key("set-sync-sidebar-tree"), &syncTree, "自动展开左侧目录树到当前文件夹").Changed() {
							s.SetSyncSidebarTree(syncTree)
						}
						autoSingle := s.AutoEnterSingleRoot
						if ui.Checkbox(c.Key("set-auto-enter-single-root"), &autoSingle, "单根设备展开时直接显示子目录").Changed() {
							s.SetAutoEnterSingleRoot(autoSingle)
						}
					})
				})

				showTsnet := s.Settings != nil && s.Settings.Net == "tsnet"

				settingField(c, "cache-dir", "缓存目录", &s.SettingsDraft.CacheDirInput, false, pal)
				if showTsnet {
					settingField(c, "auth-key", "AuthKey", &s.SettingsDraft.AuthKeyInput, true, pal)
				}

				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if ui.Button(c.Key("set-open-cache"), "打开缓存目录").Disabled(s.Offline || s.Client == nil).FontSize(12).Clicked() {
						s.OpenCacheDirFolder()
					}
					if showTsnet && s.Settings.AuthKeyConfigured {
						if ui.Button(c.Key("set-clear-key"), "清除 AuthKey").FontSize(12).Clicked() {
							s.ClearStoredAuthKey()
						}
					}
				})

				if s.SettingsDraft.ActionMsg != "" {
					ui.Text(c, s.SettingsDraft.ActionMsg).FontSize(12).TextColor(pal.AccentFg)
				}
				if s.SettingsDraft.Err != "" {
					ui.Text(c, s.SettingsDraft.Err).FontSize(12).TextColor(pal.Destructive)
				}

				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c.Key("set-cancel"), "关闭").Clicked() {
						s.CancelSettings()
					}
					if ui.PrimaryButton(c.Key("set-save"), "保存").Disabled(s.SettingsDraft.Saving).Clicked() {
						s.SaveSettings()
					}
				})
			})
		})
		return
	}

	// 5. First-Close Window Behavior Prompt Modal
	if s.Modal == ModalClosePrompt {
		s.closeModalOpen = true
		ui.Modal(c, &s.closeModalOpen, func() {
			ui.Column(c).Width(420).Gap(12).Children(func() {
				ui.Text(c, "关闭主窗口").FontSize(16).FontWeight(600).TextColor(pal.Foreground)
				ui.Text(c, "最小化到系统托盘后，Tailfile 会在后台继续共享文件夹并完成正在进行的传输。").FontSize(13).TextColor(pal.MutedFg)
				ui.Checkbox(c.Key("close-remember"), &s.CloseRemember, "记住我的选择，不再提醒")
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c.Key("close-cancel"), "取消").Clicked() {
						s.CancelClosePrompt()
					}
					if ui.Button(c.Key("close-quit"), "直接退出").Clicked() {
						s.ChooseCloseAction(CloseActionQuit)
					}
					if ui.PrimaryButton(c.Key("close-tray"), "最小化到托盘").Clicked() {
						s.ChooseCloseAction(CloseActionTray)
					}
				})
			})
		})
		return
	}

	// 6. Daemon Unreachable Guide Modal
	if s.Modal == ModalDaemon {
		s.daemonModalOpen = true
		ui.Modal(c, &s.daemonModalOpen, func() {
			ui.Column(c).Width(460).Gap(10).Children(func() {
				ui.Text(c, s.Guide.Headline).FontSize(16).FontWeight(600).TextColor(pal.Foreground)
				ui.Text(c, s.Guide.Summary).FontSize(12).TextColor(pal.MutedFg)
				ui.Column(c).Padding(10).Radius(8).Background(pal.Muted).Gap(4).Children(func() {
					for _, cmd := range s.Guide.Commands {
						ui.Text(c, cmd).FontSize(12).TextColor(pal.Foreground)
					}
				})
				ui.Text(c, s.Guide.Footnote).FontSize(11).TextColor(pal.MutedFg)
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.PrimaryButton(c.Key("daemon-close"), "知道了").Clicked() {
						s.CloseModal()
					}
				})
			})
		})
	}
}

func (s *Session) renderDocEditModal(c *ui.Context, pal Palette, loc *viewLocalState) {
	d := &s.DocEditor
	s.docModalOpen = true
	prevPhase := d.Phase

	w, h := c.Size()
	modalW, editorH := float32(720), float32(420)
	if w > 0 {
		if w >= 820 {
			modalW = max(float32(560), min(float32(860), w-120))
		} else {
			modalW = max(float32(300), min(float32(780), w-24))
		}
	}
	if h > 0 {
		if w > 0 && w < 820 {
			editorH = max(float32(160), min(float32(380), h-230))
		} else {
			editorH = max(float32(200), min(float32(480), h-260))
		}
	}

	m := ui.Modal(c, &s.docModalOpen, func() {
		if c.Shortcut(ui.Cmd, ui.KeyS) {
			if !d.ReadOnly && d.Phase == DocReady && d.Dirty() {
				s.SaveDocEditor(false)
			}
		}
		if c.Shortcut(ui.Cmd, ui.KeyF) {
			if d.Buffer != nil && (d.Phase == DocReady || d.Phase == DocConflict || d.Phase == DocConfirmClose) {
				d.FindOpen = true
			}
		}

		ui.Column(c).Width(modalW).Gap(10).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				title := d.Name
				if d.Dirty() {
					title += " · 未保存"
				}
				ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(pal.Foreground).Grow(1).SingleLine()
				if d.Node != "" {
					pillBadge(c, d.Node, pal.BadgeBg, pal.BadgeFg)
				}
				if d.ReadOnly {
					pillBadge(c, "只读", pal.BadgeBg, pal.Destructive)
				}
				if d.Buffer != nil {
					eolLabel := d.LineEnding.Label()
					if d.HasBOM {
						eolLabel += " BOM"
					}
					pillBadge(c, eolLabel, pal.BadgeBg, pal.BadgeFg)
					if ui.Button(c.Key("doc-editor-find-btn"), "查找").Height(26).Padding(0, 10).FontSize(12).Clicked() {
						d.FindOpen = !d.FindOpen
					}
				}
			})

			var jumpToMatch bool
			if d.Buffer != nil && d.FindOpen {
				prevQuery := d.FindQuery
				d.SyncFindMatches()
				fb := ui.FindBar(c.Key("doc-editor-findbar"), &d.FindOpen, &d.FindQuery, len(d.FindMatches), &d.FindCurrent)
				d.SyncFindMatches()
				jumpToMatch = fb.Changed() || d.FindQuery != prevQuery
			}

			switch d.Phase {
			case DocConflict:
				ui.Column(c).FillWidth().Padding(10).Radius(8).Background(pal.Muted).BorderWidth(1).BorderColor(pal.Destructive).Gap(8).Children(func() {
					ui.Text(c, d.ConflictMsg).FontSize(12).FontWeight(600).TextColor(pal.Destructive)
					if d.ConflictCur != nil && !d.ConflictCur.ModTime.IsZero() {
						ui.Text(c, fmt.Sprintf("远端当前版本: %s · %s", FormatSize(d.ConflictCur.Size), FormatTime(d.ConflictCur.ModTime))).FontSize(11).TextColor(pal.MutedFg)
					}
					ui.Row(c).Wrap().Gap(8).Justify(ui.End).Children(func() {
						if ui.Button(c.Key("doc-conflict-cancel"), "取消").Height(28).Padding(0, 10).FontSize(12).Clicked() {
							s.DismissDocSubPrompt()
						}
						if ui.Button(c.Key("doc-conflict-reload"), "重新加载").Height(28).Padding(0, 10).FontSize(12).Clicked() {
							s.ReloadDocEditor()
						}
						if ui.PrimaryButton(c.Key("doc-conflict-overwrite"), "覆盖保存").Height(28).Padding(0, 12).FontSize(12).Background(pal.Destructive).Clicked() {
							s.SaveDocEditor(true)
						}
					})
				})
			case DocConfirmClose:
				ui.Row(c).FillWidth().Wrap().Padding(10).Radius(8).Background(pal.Accent).BorderWidth(1).BorderColor(pal.Primary).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "文件有未保存的修改，是否先保存？").FontSize(12).FontWeight(600).TextColor(pal.Foreground).Grow(1)
					ui.Row(c).Gap(6).Children(func() {
						if ui.Button(c.Key("doc-discard-cancel"), "继续编辑").Height(28).Padding(0, 10).FontSize(12).Clicked() {
							s.DismissDocSubPrompt()
						}
						if ui.Button(c.Key("doc-discard-confirm"), "放弃修改").Height(28).Padding(0, 10).FontSize(12).TextColor(pal.Destructive).Clicked() {
							s.CloseDocEditorForce()
						}
						if ui.PrimaryButton(c.Key("doc-discard-save"), "保存并关闭").Height(28).Padding(0, 12).FontSize(12).Clicked() {
							d.CloseAfterSave = true
							s.SaveDocEditor(false)
						}
					})
				})
			}
			if d.Err != "" && d.Phase == DocReady {
				ui.Text(c, d.Err).FontSize(12).TextColor(pal.Destructive)
			}

			switch {
			case d.Phase == DocLoading:
				ui.Column(c).FillWidth().Height(editorH).Center().Gap(10).Background(pal.Muted).Radius(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "正在加载文件内容…").FontSize(12).TextColor(pal.MutedFg)
				})
			case d.Phase == DocError:
				ui.Column(c).FillWidth().Height(editorH).Center().Gap(10).Padding(16).Background(pal.Muted).Radius(8).Children(func() {
					ui.Text(c, d.Err).FontSize(12).TextColor(pal.Destructive)
					if ui.Button(c.Key("doc-editor-retry"), "重试").Height(30).Padding(0, 14).FontSize(12).Clicked() {
						s.ReloadDocEditor()
					}
				})
			case d.Buffer != nil:
				var hlRanges []ui.TextRange
				if d.FindOpen && len(d.FindMatches) > 0 {
					hlRanges = make([]ui.TextRange, len(d.FindMatches))
					for i, mr := range d.FindMatches {
						col := pal.AccentFg
						weight := 600
						if i == d.FindCurrent {
							col = pal.Primary
							weight = 700
						}
						hlRanges[i] = ui.TextRange{Start: mr.Start, End: mr.End, Color: col, Weight: weight}
					}
				}
				ta := ui.TextAreaBuffer(c.Key("doc-editor-input"), d.Buffer).
					Label("文件内容").
					Bind(&loc.docArea).
					AutoFocus().
					Font("monospace").
					FontSize(13).
					LineHeight(1.45).
					FillWidth().
					Height(editorH).
					ReadOnly(d.ReadOnly || d.Phase == DocSaving)
				if len(hlRanges) > 0 {
					ta.TextRanges(hlRanges...)
				}
				if !d.ReadOnly && d.Phase == DocReady {
					ta.OnTextKey(func(k ui.TextKey) bool {
						if k.Key == ui.KeyTab && k.Mods == 0 {
							loc.docInsertTab = true
							return true
						}
						return false
					})
				} else {
					ta.OnTextKey(nil)
				}
				if loc.docInsertTab {
					loc.docInsertTab = false
					start, end := ta.TextSelection()
					ta.ReplaceText(start, end, "\t")
				}
				if jumpToMatch && len(d.FindMatches) > 0 && d.FindCurrent >= 0 && d.FindCurrent < len(d.FindMatches) {
					mr := d.FindMatches[d.FindCurrent]
					ta.SetTextSelection(mr.Start, mr.End)
				}
				if loc.docWasFindOpen && !d.FindOpen {
					ta.Focus()
				}
			}

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				var footerText string
				footerColor := pal.MutedFg
				switch {
				case !d.Dirty() && d.StatusNote != "":
					footerText = d.StatusNote
					footerColor = pal.AccentFg
				case d.ReadOnly:
					footerText = "只读模式，仅支持查看与复制"
				case d.Buffer != nil:
					footerText = fmt.Sprintf("%d 行 · %s", d.Buffer.LineCount(), FormatSize(int64(d.Buffer.ByteLen())))
				}
				ui.Text(c, footerText).FontSize(11).TextColor(footerColor).Grow(1).SingleLine()

				if ui.Button(c.Key("doc-editor-close"), "关闭").Height(30).Padding(0, 12).FontSize(12).Clicked() {
					s.RequestCloseDocEditor()
				}
				if !d.ReadOnly && d.Buffer != nil {
					saveLabel := "保存"
					if d.Phase == DocSaving {
						saveLabel = "保存中…"
					}
					if ui.PrimaryButton(c.Key("doc-editor-save"), saveLabel).
						Disabled(d.Phase != DocReady || !d.Dirty()).
						Height(30).Padding(0, 14).FontSize(12).Clicked() {
						s.SaveDocEditor(false)
					}
				}
			})
		})
	})

	if m.Dismissed() || !s.docModalOpen {
		switch {
		case loc.docWasFindOpen && !d.FindOpen:
			s.docModalOpen = true
		case prevPhase == DocConfirmClose || prevPhase == DocConflict:
			s.DismissDocSubPrompt()
		default:
			s.RequestCloseDocEditor()
		}
	}
	loc.docWasFindOpen = d.FindOpen
}

func settingField(c *ui.Context, key, label string, val *string, password bool, pal Palette) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).Width(90).FontSize(12).TextColor(pal.MutedFg).Shrink(0)
		in := ui.TextInput(c.Key("set-"+key), val).Label(label).Grow(1).Height(30).FontSize(12)
		if password {
			in.Password()
		}
	})
}

func renderBreadcrumbCrumbs(c *ui.Context, pal Palette, crumbs []Crumb, keyPrefix string, onNavigate func(string)) {
	for i, cr := range crumbs {
		if i > 0 {
			ui.Text(c, "/").FontSize(12).TextColor(pal.MutedFg).Padding(0, 1)
		}
		isLast := i == len(crumbs)-1
		cb := ui.Button(c.Key(fmt.Sprintf("%s-%d-%s", keyPrefix, i, cr.Path)), cr.Label).
			Padding(2, 4).Height(24).Radius(4).FontSize(12).
			Background(ui.Transparent).BorderWidth(0)
		if isLast {
			cb.FontWeight(600).TextColor(pal.Foreground)
		} else {
			cb.TextColor(pal.MutedFg)
			if cb.Hovered() {
				cb.Background(pal.RowHover)
			}
		}
		if cb.Clicked() && !isLast {
			onNavigate(cr.Path)
		}
	}
}

func iconBtn(c *ui.Context, key, label, iconPath string, iconColor, hoverBg ui.Color, disabled bool) ui.Element {
	btn := ui.Button(c.Key(key), "").Size(30, 30).Padding(0).Center().Background(ui.Transparent).BorderWidth(0).Radius(6).Label(label).Tooltip(label).Disabled(disabled)
	if !disabled && btn.Hovered() {
		btn.Background(hoverBg)
	}
	col := iconColor
	if disabled {
		col = ui.RGBA(iconColor.R, iconColor.G, iconColor.B, 0.35)
	}
	btn.Children(func() {
		StrokeIcon(c, iconPath, 16, col)
	})
	return btn
}

func pillBadge(c *ui.Context, text string, bg, fg ui.Color) {
	ui.Row(c).Padding(1, 6).Radius(4).Background(bg).Children(func() {
		ui.Text(c, text).FontSize(10).FontWeight(600).TextColor(fg)
	})
}

func sidebarOutlineTheme(c *ui.Context, pal Palette, metrics LayoutMetrics) *ui.Theme {
	base := c.Theme()
	if base == nil {
		base = ui.LightTheme()
	}
	th := *base
	th.Accent = pal.Accent
	th.InactiveAccent = pal.Accent
	th.AccentText = pal.AccentFg
	th.SurfaceHover = pal.RowHover
	th.Radius = 4
	th.Spacing = metrics.TreeOutlineSpacing
	return &th
}

func sidebarRowKey(k SidebarKey) string {
	switch k.Kind {
	case SidebarNode:
		return "node-" + k.NodeID
	case SidebarDir:
		return "tree-dir-" + k.NodeID + "-" + k.Path
	case SidebarPlaceholder:
		return "tree-load-" + k.NodeID + "-" + k.Path
	default:
		return "tree-item"
	}
}

func (s *Session) renderDesktopSidebarOutline(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState, tab *Tab) {
	s.initSidebarTree()
	if !loc.sidebarOutlineReady {
		loc.sidebarSelected = -1
		for k := range s.Tree.Expanded {
			loc.sidebarOutline.Open.Add(k)
		}
		loc.sidebarOutlineReady = true
	}
	if loc.lastOutlineDensity != "" && loc.lastOutlineDensity != metrics.Resolved {
		// Flush cached row heights in Outline.List without dropping expanded branch state.
		loc.sidebarOutline.List = ui.ListState{}
		loc.sidebarSelected = -1
		loc.sidebarSyncGen = 0
	}
	loc.lastOutlineDensity = metrics.Resolved
	loc.sidebarOutline.Indent = metrics.TreeIndent
	for _, k := range s.Tree.PendingOpen {
		loc.sidebarOutline.Open.Add(k)
	}
	s.Tree.PendingOpen = s.Tree.PendingOpen[:0]
	for _, k := range s.Tree.PendingClose {
		loc.sidebarOutline.Open.Remove(k)
	}
	s.Tree.PendingClose = s.Tree.PendingClose[:0]

	loc.sidebarOutline.Generation = s.Tree.Gen
	loc.sidebarOutline.List.Selected = &loc.sidebarSelected
	loc.sidebarOutline.List.Label = func(i int) string {
		if i < 0 || i >= loc.sidebarOutline.Rows() {
			return ""
		}
		return s.SidebarRowLabel(loc.sidebarOutline.Item(i))
	}

	if loc.sidebarSyncNode != tab.Node || loc.sidebarSyncPath != tab.Path || loc.sidebarSyncGen != s.Tree.Gen {
		visible := s.VisibleSidebarKeys(&loc.sidebarOutline.Open)
		wantKey := s.DesiredSidebarKey(func(i int) SidebarKey { return visible[i] }, len(visible))
		wantIdx := -1
		for i, k := range visible {
			if k == wantKey {
				wantIdx = i
				break
			}
		}
		if loc.sidebarSelected != wantIdx {
			loc.sidebarOutline.List.Choose(wantIdx)
			if s.SyncSidebarTree && wantIdx >= 0 {
				loc.sidebarOutline.List.ScrollIntoView(wantIdx)
			}
		}
		loc.sidebarSyncNode = tab.Node
		loc.sidebarSyncPath = tab.Path
		loc.sidebarSyncGen = s.Tree.Gen
	}

	roots := s.SidebarRoots()
	var outlineEl ui.Element
	ui.Themed(c, sidebarOutlineTheme(c, pal, metrics)).Grow(1).FillWidth().Padding(metrics.TreeOuterPadV, metrics.TreeOuterPadH, metrics.TreeOuterPadV*2, metrics.TreeOuterPadH).Children(func() {
		outlineEl = ui.Outline(c.Key("sidebar-outline"), &loc.sidebarOutline, roots, s.SidebarChildren, func(k SidebarKey) {
			s.renderSidebarOutlineRow(c, pal, metrics, loc, tab, k)
		}).Grow(1).FillWidth()
	})

	changed := outlineEl.Changed()
	submitted := outlineEl.Submitted()
	s.SyncFromOutlineOpen(&loc.sidebarOutline.Open)

	if changed && loc.sidebarSelected >= 0 && loc.sidebarSelected < loc.sidebarOutline.Rows() {
		k := loc.sidebarOutline.Item(loc.sidebarSelected)
		s.ActivateSidebarKey(k)
		act := s.Active()
		loc.sidebarSyncNode = act.Node
		loc.sidebarSyncPath = act.Path
		loc.sidebarSyncGen = s.Tree.Gen
	} else if submitted && loc.sidebarSelected >= 0 && loc.sidebarSelected < loc.sidebarOutline.Rows() {
		k := loc.sidebarOutline.Item(loc.sidebarSelected)
		if k.Kind == SidebarNode || k.Kind == SidebarDir {
			if loc.sidebarOutline.Open.Has(k) {
				loc.sidebarOutline.Open.Remove(k)
			} else if s.SidebarChildren(k) != nil {
				loc.sidebarOutline.Open.Add(k)
			}
			s.SyncFromOutlineOpen(&loc.sidebarOutline.Open)
			s.ActivateSidebarKey(k)
			act := s.Active()
			loc.sidebarSyncNode = act.Node
			loc.sidebarSyncPath = act.Path
			loc.sidebarSyncGen = s.Tree.Gen
		} else if k.Kind == SidebarPlaceholder {
			s.ActivateSidebarKey(k)
		}
	}
}

func (s *Session) renderSidebarOutlineRow(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState, tab *Tab, k SidebarKey) {
	rows := loc.sidebarOutline.Rows()
	selected := loc.sidebarSelected >= 0 && loc.sidebarSelected < rows && loc.sidebarOutline.Item(loc.sidebarSelected) == k
	fg := pal.SidebarFg
	if selected {
		fg = pal.AccentFg
	}
	content := ui.Row(c.Key(sidebarRowKey(k))).Grow(1).Height(metrics.TreeRowContentH).AlignItems(ui.Center).Gap(metrics.TreeRowGap)

	switch k.Kind {
	case SidebarNode:
		n, ok := s.NodeByID(k.NodeID)
		name := k.NodeID
		var nodeOS string
		var online, self, readOnly bool
		if ok {
			if n.Name != "" {
				name = n.Name
			}
			nodeOS = ResolveNodeOS(n.OS, n.ID, n.Name)
			online = n.Online && n.AgentOK
			self = n.Self
			readOnly = n.ReadOnly
		}
		content.Children(func() {
			OSIcon(c, nodeOS, metrics.TreeIconSize, fg)
			ui.Text(c, name).FontSize(metrics.TreeNodeFontSize).FontWeight(500).TextColor(fg).Grow(1).SingleLine().Ellipsis("…")
			if self {
				pillBadge(c, "本机", pal.BadgeBg, pal.BadgeFg)
			}
			if readOnly {
				StrokeIcon(c, PathLock, 11, pal.MutedFg)
			}
			StatusDot(c, online, readOnly)
		})
	case SidebarDir:
		content.Children(func() {
			TypedFileIcon(c, FileFolder, metrics.TreeIconSize)
			ui.Text(c, s.SidebarDirLabel(k)).FontSize(metrics.TreeDirFontSize).FontWeight(500).TextColor(fg).Grow(1).SingleLine().Ellipsis("…")
		})
	case SidebarPlaceholder:
		msg := "正在加载…"
		col := pal.MutedFg
		dirKey := SidebarKey{Kind: SidebarDir, NodeID: k.NodeID, Path: CanonicalizeRemote(k.Path)}
		if entry := s.Tree.Cache[dirKey]; entry != nil && entry.Phase == DirError {
			msg = "加载失败（点击重试）"
			col = pal.Destructive
		}
		content.Children(func() {
			ui.Text(c, msg).FontSize(11.5).TextColor(col).Grow(1).SingleLine().Ellipsis("…")
		})
	}

	content.ObserveInput(func(ev ui.InputEvent) {
		switch ev.Button {
		case 2:
			if k.Kind != SidebarNode && k.Kind != SidebarDir {
				return
			}
			switch ev.Kind {
			case ui.InputPointerDown:
				loc.sidebarMiddleDown = true
				loc.sidebarMiddleKey = k
			case ui.InputPointerUp:
				if loc.sidebarMiddleDown && loc.sidebarMiddleKey == k {
					loc.sidebarMiddleDown = false
					if !ev.Canceled {
						s.OpenKeyInNewTab(k)
					}
				}
			}
		case 0:
			if ev.Kind == ui.InputPointerUp && !ev.Canceled && selected {
				switch k.Kind {
				case SidebarNode:
					if n, ok := s.NodeByID(k.NodeID); ok && tab.Node == k.NodeID {
						roots := CanonicalNodeRoots(n.ReadRoots)
						if len(roots) > 0 && remoteCompareForm(tab.Path) != remoteCompareForm(roots[0]) {
							s.SelectNode(k.NodeID)
						}
					}
				case SidebarPlaceholder:
					s.ActivateSidebarKey(k)
				}
			}
		}
	})

	if k.Kind == SidebarNode || k.Kind == SidebarDir {
		content.PopoverMenu(func(m *ui.Menu) {
			if m.Item("在新标签页打开").Chosen() {
				s.OpenKeyInNewTab(k)
			}
			refreshDisabled := s.Offline
			if n, ok := s.NodeByID(k.NodeID); !ok || !n.Online || !n.AgentOK || len(n.ReadRoots) == 0 {
				refreshDisabled = true
			}
			if m.Item("刷新子目录").Disabled(refreshDisabled).Chosen() {
				s.RefreshSidebarKey(k)
			}
		})
	}
}

func (s *Session) renderNewTabPicker(c *ui.Context, pal Palette, loc *viewLocalState, mobileTouch bool) {
	ui.Scroll(c).Fill().Padding(24).Children(func() {
		ui.Column(c).FillWidth().Gap(16).MaxWidth(760).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "新建标签页").FontSize(18).FontWeight(600).TextColor(pal.Foreground)
				ui.Text(c, "选择一个设备开始浏览，或在左侧目录树中键与右键直接打开").FontSize(12).TextColor(pal.MutedFg)
			})

			if len(s.Nodes) == 0 {
				ui.Column(c).Padding(20).Radius(10).Background(pal.Muted).BorderWidth(1).BorderColor(pal.Border).Gap(8).Children(func() {
					ui.Text(c, "暂无可用设备").FontSize(14).FontWeight(600).TextColor(pal.Foreground)
					ui.Text(c, "等待 Tailnet 节点上线或前往设置检查连接状态。").FontSize(12).TextColor(pal.MutedFg)
					if ui.Button(c.Key("newtab-open-settings"), "打开设置").Clicked() {
						s.OpenSettings()
					}
				})
				return
			}

			cols := 2
			if mobileTouch {
				cols = 1
			} else if w, _ := c.Size(); w > 0 {
				sideW := ClampSidebarWidth(s.SidebarWidth)
				if s.SidebarCollapsed {
					sideW = 56
				}
				avail := w - sideW - 48
				if s.DetailsOpen {
					avail -= ClampDetailsWidth(s.DetailsWidth)
				}
				if avail >= 680 {
					cols = 3
				} else if avail < 460 {
					cols = 1
				}
			}

			ui.Grid(c).Columns(cols).Gap(12).FillWidth().Children(func() {
				for _, n := range s.Nodes {
					nodeID := n.ID
					nodeOS := ResolveNodeOS(n.OS, n.ID, n.Name)
					card := ui.Column(c.Key("newtab-node-" + nodeID)).Padding(14).Gap(8).Radius(10).Background(pal.Muted).BorderWidth(1).BorderColor(pal.Border).Label(n.Name)
					if card.Hovered() {
						card.Background(pal.RowHover).BorderColor(pal.Ring)
					}
					statusText := "离线"
					if n.Online && n.AgentOK {
						if n.ReadOnly {
							statusText = "在线 · 只读"
						} else {
							statusText = "在线 · 可写"
						}
					}
					rootText := "未配置共享目录"
					roots := CanonicalNodeRoots(n.ReadRoots)
					if len(roots) == 1 {
						rootText = BaseRemote(roots[0])
					} else if len(roots) > 1 {
						rootText = fmt.Sprintf("%d 个共享目录", len(roots))
					}

					card.Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							OSIcon(c, nodeOS, 18, pal.Foreground)
							ui.Text(c, n.Name).FontSize(14).FontWeight(600).TextColor(pal.Foreground).Grow(1).SingleLine().Ellipsis("…")
							if n.Self {
								pillBadge(c, "本机", pal.BadgeBg, pal.BadgeFg)
							}
							if n.ReadOnly {
								StrokeIcon(c, PathLock, 12, pal.MutedFg)
							}
							StatusDot(c, n.Online && n.AgentOK, n.ReadOnly)
						})
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, rootText).FontSize(12).TextColor(pal.MutedFg).Grow(1).SingleLine().Ellipsis("…")
							ui.Text(c, statusText).FontSize(11).TextColor(pal.MutedFg).SingleLine()
						})
					})

					cardKey := SidebarKey{Kind: SidebarNode, NodeID: nodeID}
					card.ObserveInput(func(ev ui.InputEvent) {
						if ev.Button != 2 {
							return
						}
						switch ev.Kind {
						case ui.InputPointerDown:
							loc.sidebarMiddleDown = true
							loc.sidebarMiddleKey = cardKey
						case ui.InputPointerUp:
							if loc.sidebarMiddleDown && loc.sidebarMiddleKey == cardKey {
								loc.sidebarMiddleDown = false
								if !ev.Canceled {
									s.OpenNodeInNewTab(nodeID, "")
								}
							}
						}
					})
					if card.Clicked() {
						s.SelectNode(nodeID)
					}
				}
			})
		})
	})
}
