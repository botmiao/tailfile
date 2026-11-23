package gui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
)

func (s *Session) viewMobile(c *ui.Context, pal Palette, metrics LayoutMetrics, loc *viewLocalState) {
	tab := s.Active()
	title := s.TabLabel(tab)
	canWrite := s.CanWriteActive()

	ui.Column(c).Fill().Background(pal.Card).Children(func() {
		mobile.TopBar(c, mobile.TopBarOptions{
			Title: title,
			Leading: func() {
				if iconBtn(c, "mob-menu", "菜单", PathMenu, pal.Foreground, pal.RowHover, false).Clicked() {
					s.mobileDrawerOpen = true
				}
			},
			Trailing: func() {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					if iconBtn(c, "mob-up", "上一级", PathArrowUp, pal.Foreground, pal.RowHover, !s.CanGoUp()).Clicked() {
						s.GoUp()
					}
					if iconBtn(c, "mob-mkdir", "新建文件夹", PathFolderPlus, pal.Foreground, pal.RowHover, !canWrite).Clicked() {
						s.BeginMkdir()
					}
					if iconBtn(c, "mob-settings", "设置", PathSettings, pal.Foreground, pal.RowHover, false).Clicked() {
						s.OpenSettings()
					}
				})
			},
		})

		ui.Divider(c).Background(pal.Border)

		// Breadcrumb + Filter strip
		ui.Column(c).FillWidth().Padding(8, 12).Gap(8).Background(pal.Muted).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Children(func() {
			var roots []string
			if n, ok := s.NodeByID(tab.Node); ok {
				roots = n.ReadRoots
			}
			crumbs := BuildCrumbs(tab.Path, roots)
			ui.ScrollHorizontal(c).FillWidth().Children(func() {
				ui.Row(c).Gap(0).AlignItems(ui.Center).Children(func() {
					renderBreadcrumbCrumbs(c, pal, crumbs, "mcrumb", func(p string) {
						_ = s.Navigate(p)
					})
				})
			})
			ui.Row(c).FillWidth().Height(34).Padding(0, 10).Gap(6).AlignItems(ui.Center).Background(pal.Card).BorderWidth(1).BorderColor(pal.Input).Radius(8).Children(func() {
				StrokeIcon(c, PathSearch, 14, pal.MutedFg)
				ui.TextInput(c.Key("mob-filter"), &s.Filter).Placeholder("筛选当前目录…").Label("筛选当前目录").Grow(1).BorderWidth(0).Background(ui.Transparent).Height(28).FontSize(13)
			})
		})

		if s.Editor.Kind == EditorMkdir || s.Editor.Kind == EditorGoto {
			s.renderInlineEditor(c, pal, loc)
		}

		// File list (touch-friendly rows)
		ui.Box(c).Grow(1).FillWidth().Children(func() {
			s.renderFileBrowser(c, pal, metrics, loc, tab, canWrite, true)
		})

		if len(s.Transfers) > 0 {
			activeCnt := 0
			for _, tr := range s.Transfers {
				if tr.Job.Status == "running" || tr.Job.Status == "queued" {
					activeCnt++
				}
			}
			bar := ui.Row(c.Key("mob-transfer-pill")).FillWidth().Height(40).Padding(0, 14).AlignItems(ui.Center).Background(pal.Sidebar).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border)
			bar.Children(func() {
				ui.Text(c, fmt.Sprintf("传输任务: %d（%d 进行中）", len(s.Transfers), activeCnt)).FontSize(12).FontWeight(600).TextColor(pal.Foreground).Grow(1)
				ui.Text(c, "查看").FontSize(12).TextColor(pal.Primary)
			})
			if bar.Clicked() {
				s.mobileTransferOpen = true
			}
		}
	})

	// Left navigation drawer (devices + tabs + theme/settings)
	mobile.Drawer(c, &s.mobileDrawerOpen, mobile.DrawerOptions{Width: 280}, func() {
		ui.Column(c).Fill().Padding(metrics.MobileDrawerPad).Gap(metrics.MobileDrawerGap).Background(pal.Sidebar).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				LogoMark(c, 22)
				ui.Text(c, "Tailfile").FontSize(15).FontWeight(600).TextColor(pal.SidebarFg).Grow(1)
				ui.Text(c, s.sidebarStatusText()).FontSize(11).TextColor(pal.MutedFg)
			})

			ui.Divider(c).Background(pal.Border)
			ui.Text(c, "标签页").FontSize(11).FontWeight(600).TextColor(pal.MutedFg)
			ui.Column(c).Gap(4).Children(func() {
				for i, t := range s.Tabs {
					active := i == s.ActiveIdx
					row := ui.Row(c.Key(fmt.Sprintf("mtab-%d", t.ID))).Height(metrics.MobileDrawerTabH).FillWidth().Padding(0, 10).Radius(6).AlignItems(ui.Center).Gap(6)
					if active {
						row.Background(pal.Accent)
					}
					row.Children(func() {
						ui.Text(c, s.TabLabel(t)).FontSize(12).Grow(1).SingleLine()
					})
					if row.Clicked() {
						s.SwitchTab(i)
						s.mobileDrawerOpen = false
					}
				}
				if ui.Button(c.Key("mob-new-tab"), "+ 新标签页").Height(32).FontSize(12).Clicked() {
					s.NewTab()
					s.mobileDrawerOpen = false
				}
			})

			ui.Divider(c).Background(pal.Border)
			ui.Text(c, "设备").FontSize(11).FontWeight(600).TextColor(pal.MutedFg)
			ui.Scroll(c).Grow(1).Gap(2).Children(func() {
				for _, n := range s.Nodes {
					active := tab.Node == n.ID
					nodeOS := ResolveNodeOS(n.OS, n.ID, n.Name)
					fg := pal.SidebarFg
					bg := ui.Transparent
					if active {
						fg = pal.AccentFg
						bg = pal.Accent
					}
					row := ui.Row(c.Key("mnode-"+n.ID)).Height(metrics.MobileDrawerNodeH).FillWidth().Padding(0, 10).Radius(8).Gap(8).AlignItems(ui.Center).Background(bg)
					row.Children(func() {
						OSIcon(c, nodeOS, 16, fg)
						ui.Text(c, n.Name).FontSize(13).FontWeight(500).TextColor(fg).Grow(1).SingleLine()
						if n.ReadOnly {
							StrokeIcon(c, PathLock, 12, pal.MutedFg)
						}
						StatusDot(c, n.Online && n.AgentOK, n.ReadOnly)
					})
					if row.Clicked() {
						s.SelectNode(n.ID)
						s.mobileDrawerOpen = false
					}
				}
			})

			ui.Divider(c).Background(pal.Border)
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c.Key("mob-paste"), "粘贴").Disabled(!canWrite || s.Clipboard == nil).Height(34).Grow(1).Clicked() {
					s.Paste()
					s.mobileDrawerOpen = false
				}
				if ui.Button(c.Key("mob-drawer-settings"), "设置").Height(34).Grow(1).Clicked() {
					s.mobileDrawerOpen = false
					s.OpenSettings()
				}
			})
		})
	})

	// File preview & action bottom sheet
	mobile.Sheet(c, &s.mobileDetailsOpen, mobile.SheetOptions{
		Detents: []float32{0.55, 0.92},
		Label:   "文件详情",
	}, func() {
		ui.Scroll(c).Fill().Children(func() {
			s.renderDetailsPane(c, pal, metrics, loc, canWrite)
		})
	})

	// Transfer list bottom sheet
	mobile.Sheet(c, &s.mobileTransferOpen, mobile.SheetOptions{
		Detents: []float32{0.5, 0.85},
		Label:   "传输队列",
	}, func() {
		s.renderTransferBar(c, pal, metrics)
	})
}

func trimOrEmpty(s string) string { return strings.TrimSpace(s) }
