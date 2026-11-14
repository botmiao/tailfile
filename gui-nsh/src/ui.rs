//! Prototype-matching Finder layout: clean header, tabs, node tree,
//! single action toolbar, breadcrumb controls, list/grid, right details.

use crate::icons::{self, path as ip};
use crate::persist::ThemePref;
use crate::token::{self, base_remote};
use nsh_base::{Theme, button, checkbox, dialog, input, theme};
use nsh_core::Cursor;
use nsh_core::desktop::TitleBarStyle;
use nsh_core::element::Tokens;
use nsh_core::element::desktop::{set_title_bar, title_bar};
use nsh_core::prelude::*;
use nsh_core::{ContextMenuEvent, EventKind, EventPayload, PressEvent};
use nsh_core::{Edges, PointerEvents};

use crate::api::{self, job_line, size_label};
use crate::model::{self, TreeRow, Ui};
use crate::token::format_time;

/// Design tokens from `nsh.json`. `cargo run` does not load that file on its
/// own (`Root::of` keeps nsh-base's neutral theme), so the window passes
/// these in. `selection` and `primary` are what the file rows and the tab
/// underline read.
pub fn theme_tokens() -> Tokens {
    let file = include_str!("../nsh.json");
    let value: serde_json::Value =
        serde_json::from_str(file).unwrap_or_else(|_| serde_json::json!({}));
    let theme_json = value
        .get("theme")
        .map(|v| v.to_string())
        .unwrap_or_else(|| r#"{"cssVars":{}}"#.to_owned());
    Theme::from_json(&theme_json)
        .unwrap_or_else(|err| {
            eprintln!("nsh.json theme: {err}");
            Theme::default()
        })
        .into_tokens()
}

#[component]
pub fn App() -> impl IntoElement {
    let state = signal(Ui::new());
    model::boot(state);
    // One chrome row: hide the system title bar and draw logo, tabs, and
    // window controls' reserved insets ourselves. Native caption buttons stay.
    set_title_bar(TitleBarStyle::Hidden, Some(px(46.0)));
    view()
        .relative()
        .flex_1()
        .bg(move || theme().background)
        .text_color(move || theme().foreground)
        .child(chrome(state))
        .child(body(state))
        .child(status_bar(state))
        .child(transfer_bar(state))
        .child(conflict_dialog(state))
        .child(settings_dialog(state))
        .child(delete_dialog(state))
        .child(daemon_dialog(state))
        .child(context_menu_overlay(state))
}

fn chrome_height() -> nsh_core::Points {
    let h = title_bar().top;
    if h > px(0.0) { h } else { px(46.0) }
}

/// Full Tailnet column. The tab strip starts at this edge.
const SIDEBAR_W: f32 = 262.0;
/// Collapsed rail: host icons only.
const SIDEBAR_RAIL: f32 = 56.0;

fn sidebar_width(collapsed: bool) -> f32 {
    if collapsed { SIDEBAR_RAIL } else { SIDEBAR_W }
}

fn title_left_inset() -> nsh_core::Points {
    let left = title_bar().left;
    if left > px(0.0) {
        left + px(8.0)
    } else {
        px(10.0)
    }
}

/// Logo column. Its width matches the Tailnet pane so tabs begin at the divider.
/// When collapsed, the collapse/expand icon is hidden and clicking the logo
/// expands the left panel again.
fn brand_column(state: Signal<Ui>) -> impl IntoElement {
    view()
        .w(move || px(sidebar_width(state.get().sidebar_collapsed)))
        .flex_none()
        .h(move || chrome_height())
        .flex_row()
        .items_center()
        .bg(move || theme().sidebar)
        .border_r_1()
        .border_color(move || theme().sidebar_border)
        .child(
            when(
                move || state.get().sidebar_collapsed,
                move || {
                    view()
                        .flex_1()
                        .h(move || chrome_height())
                        .flex_row()
                        .items_center()
                        .justify_center()
                        .window_drag(false)
                        .cursor(Cursor::Pointer)
                        .on_press(move || model::toggle_sidebar(state))
                        .child(icons::logo_mark(22.0))
                },
            )
            .otherwise(move || {
                view()
                    .flex_1()
                    .h(move || chrome_height())
                    .flex_row()
                    .items_center()
                    .pl(title_left_inset)
                    .pr(px(6.0))
                    .gap(px(8.0))
                    .child(icons::logo_mark(22.0))
                    .child(
                        text(icons::LOGO_TITLE)
                            .font_weight(FontWeight::SEMIBOLD)
                            .text_size(px(14.0))
                            .flex_1(),
                    )
                    .child(icon_btn(ip::PANEL_LEFT_CLOSE, move || {
                        model::toggle_sidebar(state)
                    }))
            }),
        )
}

/// Logo, host tabs, and theme/settings in the window's own title bar.
/// `window_drag` moves the window; presses on tabs and buttons stay clicks.
fn chrome(state: Signal<Ui>) -> impl IntoElement {
    let hot_tab = signal(-1i32);
    let hot_close = signal(-1i32);
    view()
        .window_drag(true)
        .flex_row()
        .items_end()
        .h(move || chrome_height())
        .pr(move || {
            let right = title_bar().right;
            if right > px(0.0) {
                right + px(6.0)
            } else {
                px(8.0)
            }
        })
        .bg(move || theme().background)
        .border_b_1()
        .border_color(move || theme().border)
        .child(brand_column(state))
        .child(
            scroll()
                .flex_1()
                .h(move || chrome_height())
                .pl(px(8.0))
                .child(
                    view()
                        .flex_row()
                        .items_end()
                        .h(move || chrome_height())
                        .child(dynamic(move || {
                            let ui = state.get();
                            ui.tabs
                                .iter()
                                .enumerate()
                                .map(|(index, tab)| {
                                    let title = tab.title(&ui.nodes);
                                    let os = node_os(&ui, &tab.node);
                                    tab_chip(state, hot_tab, hot_close, index, title, os)
                                })
                                .collect::<Vec<_>>()
                        })),
                ),
        )
        .child(
            view()
                .flex_row()
                .items_center()
                .h(move || chrome_height())
                .gap(px(2.0))
                .child(icon_btn(ip::PLUS, move || {
                    model::new_tab_from_current(state)
                }))
                .child(icon_btn(ip::SUN, move || {
                    let is_dark = match state.get().theme {
                        ThemePref::Dark => true,
                        ThemePref::Light => false,
                        ThemePref::System => dark(),
                    };
                    let next = if is_dark {
                        ThemePref::Light
                    } else {
                        ThemePref::Dark
                    };
                    model::set_theme(state, next);
                }))
                .child(icon_btn(ip::SETTINGS, move || model::open_settings(state))),
        )
}

fn tab_chip(
    state: Signal<Ui>,
    hot_tab: Signal<i32>,
    hot_close: Signal<i32>,
    index: usize,
    title: String,
    os: String,
) -> impl IntoElement {
    let os_for_icon = os.clone();
    view()
        .relative()
        .window_drag(false)
        .cursor(Cursor::Pointer)
        .flex_row()
        .items_center()
        .self_stretch()
        .h_full()
        .px(px(10.0))
        .gap(px(6.0))
        .max_w(px(240.0))
        .hit_slop(Edges {
            top: px(0.0),
            right: px(0.0),
            bottom: px(8.0),
            left: px(0.0),
        })
        .bg(move || {
            let active = state.get().active_tab == index;
            if !active && hot_tab.get() == index as i32 {
                theme().secondary
            } else {
                theme().secondary.opacity(0.0)
            }
        })
        .on_hover_in(move |_: &()| hot_tab.set(index as i32))
        .on_hover_out(move |_: &()| {
            if hot_tab.get() == index as i32 {
                hot_tab.set(-1);
            }
        })
        .on_press(move || model::switch_tab(state, index))
        .child(
            view()
                .absolute()
                .top(px(0.0))
                .left(px(0.0))
                .right(px(0.0))
                .bottom(px(0.0))
                .on_press(move || model::switch_tab(state, index)),
        )
        .child(
            view()
                .flex_row()
                .items_center()
                .self_stretch()
                .h_full()
                .gap(px(6.0))
                .flex_1()
                .min_w(px(0.0))
                .pointer_events(PointerEvents::None)
                .child(icons::os_icon(&os_for_icon, 16.0).text_color(move || {
                    if state.get().active_tab == index {
                        theme().primary
                    } else {
                        theme().muted_foreground
                    }
                }))
                .child(
                    text(title)
                        .text_size(px(12.5))
                        .number_of_lines(1)
                        .text_color(move || {
                            if state.get().active_tab == index {
                                theme().foreground
                            } else {
                                theme().muted_foreground
                            }
                        }),
                ),
        )
        .child(
            view()
                .w(px(28.0))
                .self_stretch()
                .h_full()
                .items_center()
                .justify_center()
                .rounded_sm()
                .cursor(Cursor::Pointer)
                .bg(move || {
                    if hot_close.get() == index as i32 {
                        theme().muted_foreground.opacity(0.22)
                    } else {
                        theme().secondary.opacity(0.0)
                    }
                })
                .on_hover_in(move |_: &()| hot_close.set(index as i32))
                .on_hover_out(move |_: &()| {
                    if hot_close.get() == index as i32 {
                        hot_close.set(-1);
                    }
                })
                .on_press(move || model::close_tab(state, index))
                .child(icons::stroke_icon(ip::X, 12.0).text_color(move || {
                    if hot_close.get() == index as i32 || state.get().active_tab == index {
                        theme().foreground
                    } else {
                        theme().muted_foreground
                    }
                })),
        )
        .child(
            view()
                .absolute()
                .bottom(px(0.0))
                .left(px(8.0))
                .right(px(8.0))
                .h(px(2.0))
                .rounded_full()
                .pointer_events(PointerEvents::None)
                .bg(move || {
                    if state.get().active_tab == index {
                        theme().primary
                    } else {
                        theme().primary.opacity(0.0)
                    }
                }),
        )
}

fn body(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_1()
        .flex_row()
        .min_h(px(0.0))
        .child(tree_pane(state))
        .child(center_pane(state))
}

fn tree_pane(state: Signal<Ui>) -> impl IntoElement {
    let hot = signal(String::new());
    view()
        .w(move || px(sidebar_width(state.get().sidebar_collapsed)))
        .flex_none()
        .flex_col()
        .self_stretch()
        .border_r_1()
        .border_color(move || theme().sidebar_border)
        .bg(move || theme().sidebar)
        .child(when(
            move || !state.get().sidebar_collapsed,
            move || {
                view().px(px(12.0)).py(px(10.0)).gap(px(2.0)).child(
                    text(move || {
                        let n = state.get().nodes.len();
                        format!("Tailnet · {n} 个节点")
                    })
                    .text_size(px(11.0))
                    .text_color(move || theme().muted_foreground),
                )
            },
        ))
        .child(
            scroll()
                .flex_1()
                .min_h(px(0.0))
                .child(dynamic(move || -> AnyElement {
                    let ui = state.get();
                    if ui.nodes.is_empty() {
                        return text(if ui.sidebar_collapsed {
                            "—"
                        } else {
                            "还没有节点"
                        })
                        .p(px(12.0))
                        .text_size(px(13.0))
                        .text_color(move || theme().muted_foreground)
                        .into_element();
                    }
                    if ui.sidebar_collapsed {
                        view()
                            .flex_col()
                            .w(px(SIDEBAR_RAIL))
                            .pt(px(8.0))
                            .pb(px(8.0))
                            .gap(px(2.0))
                            .items_center()
                            .child(each(
                                move || collapsed_rows(&state.get()),
                                |row| row.key.clone(),
                                move |row, _| rail_node_item(state, hot, row),
                            ))
                            .into_element()
                    } else {
                        view()
                            .child(each(
                                move || model::tree_rows(&state.get()),
                                |row| row.key.clone(),
                                move |row, _| tree_item(state, hot, row),
                            ))
                            .into_element()
                    }
                })),
        )
        .child(when(
            move || !state.get().sidebar_collapsed,
            || {
                text("仅显示白名单目录 · Ctrl/右键 = 新标签页")
                    .px(px(12.0))
                    .py(px(8.0))
                    .text_size(px(10.0))
                    .text_color(move || theme().muted_foreground)
            },
        ))
}

fn collapsed_rows(ui: &Ui) -> Vec<TreeRow> {
    model::tree_rows(ui)
        .into_iter()
        .filter(|row| row.is_node)
        .collect()
}

fn rail_node_item(
    state: Signal<Ui>,
    hot: Signal<String>,
    row: ReadSignal<TreeRow>,
) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .justify_center()
        .w(px(40.0))
        .h(px(40.0))
        .rounded_md()
        .cursor(Cursor::Pointer)
        .bg(move || {
            let row = row.get();
            let ui = state.get();
            if row.node == ui.folder_node {
                theme().sidebar_accent
            } else if hot.get() == row.key {
                theme().secondary
            } else {
                theme().sidebar.opacity(0.0)
            }
        })
        .on_hover_in(move |_: &()| hot.set(row.get().key.clone()))
        .on_hover_out(move |_: &()| {
            let key = row.get().key;
            if hot.get() == key {
                hot.set(String::new());
            }
        })
        .on(EventKind::Press, move |payload: &EventPayload| {
            if let EventPayload::Press(PressEvent { modifiers, .. }) = payload {
                if modifiers.ctrl || modifiers.cmd {
                    model::click_tree_new_tab(state, row.get());
                    return;
                }
            }
            model::click_tree(state, row.get());
        })
        .on_context_menu(move |_e: &ContextMenuEvent| {
            model::click_tree_new_tab(state, row.get());
        })
        .child({
            let os = node_os(&state.get(), &row.get().node);
            view()
                .w(px(22.0))
                .h(px(22.0))
                .items_center()
                .justify_center()
                .child(icons::os_icon(&os, 20.0).text_color(move || {
                    if row.get().node == state.get().folder_node {
                        theme().sidebar_accent_foreground
                    } else {
                        theme().sidebar_foreground
                    }
                }))
        })
}

fn tree_item(state: Signal<Ui>, hot: Signal<String>, row: ReadSignal<TreeRow>) -> impl IntoElement {
    let depth = move || px(10.0 + row.get().depth as f32 * 14.0);
    view()
        .flex_row()
        .items_center()
        .h(px(28.0))
        .pl(depth)
        .pr(px(8.0))
        .gap(px(6.0))
        .rounded_md()
        .mx(px(6.0))
        .cursor(Cursor::Pointer)
        .bg(move || {
            let row = row.get();
            let ui = state.get();
            let current = !row.is_node && row.node == ui.folder_node && row.path == ui.folder_path;
            if current {
                theme().sidebar_accent
            } else if hot.get() == row.key {
                theme().secondary
            } else {
                theme().sidebar.opacity(0.0)
            }
        })
        .on_hover_in(move |_: &()| hot.set(row.get().key.clone()))
        .on_hover_out(move |_: &()| {
            let key = row.get().key;
            if hot.get() == key {
                hot.set(String::new());
            }
        })
        .on(EventKind::Press, move |payload: &EventPayload| {
            if let EventPayload::Press(PressEvent { modifiers, .. }) = payload {
                if modifiers.ctrl || modifiers.cmd {
                    model::click_tree_new_tab(state, row.get());
                    return;
                }
            }
            model::click_tree(state, row.get());
        })
        .on_context_menu(move |_e: &ContextMenuEvent| {
            model::click_tree_new_tab(state, row.get());
        })
        .child(dynamic(move || -> AnyElement {
            let row = row.get();
            if row.is_node {
                let ui = state.get();
                let node = ui.nodes.iter().find(|n| n.id == row.node);
                let online = node.map(|n| n.online && n.agent_ok).unwrap_or(false);
                let ro = node.map(|n| n.read_only).unwrap_or(false);
                let os = node_os(&ui, &row.node);
                view()
                    .flex_row()
                    .items_center()
                    .gap(px(6.0))
                    .child(icons::os_icon(&os, 16.0).text_color(move || theme().muted_foreground))
                    .child(icons::status_dot(online, ro))
                    .into_element()
            } else {
                icons::typed_file_icon(icons::FileKind::Folder, 16.0).into_element()
            }
        }))
        .child(
            text(move || row.get().label.clone())
                .text_size(px(12.5))
                .number_of_lines(1)
                .flex_1()
                .text_color(move || {
                    let row = row.get();
                    let ui = state.get();
                    if !row.is_node && row.node == ui.folder_node && row.path == ui.folder_path {
                        theme().sidebar_accent_foreground
                    } else {
                        theme().sidebar_foreground
                    }
                }),
        )
        .child(when(
            move || {
                state
                    .get()
                    .nodes
                    .iter()
                    .find(|n| n.id == row.get().node)
                    .map(|n| n.read_only && row.get().is_node)
                    .unwrap_or(false)
            },
            || {
                text("只读")
                    .text_size(px(10.0))
                    .text_color(move || theme().muted_foreground)
            },
        ))
}

fn center_pane(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_1()
        .flex_col()
        .min_w(px(0.0))
        .bg(move || theme().card)
        .child(action_toolbar(state))
        .child(nav_controls(state))
        .child(name_editor(state))
        .child(
            view()
                .flex_1()
                .flex_row()
                .min_h(px(0.0))
                .child(file_list(state))
                .child(when(
                    move || state.get().preview_on,
                    move || details_pane(state),
                )),
        )
}

fn action_toolbar(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(2.0))
        .px(px(8.0))
        .h(px(40.0))
        .border_b_1()
        .border_color(move || theme().border)
        .child(tool_labeled(
            ip::FOLDER_PLUS,
            "新建文件夹",
            || true,
            move || model::begin_new_folder(state),
        ))
        .child(tool_labeled(
            ip::COPY,
            "复制",
            || true,
            move || model::copy_selected(state),
        ))
        .child(tool_labeled(
            ip::CLIPBOARD,
            "粘贴",
            move || state.get().clip_set,
            move || model::paste(state),
        ))
        .child(tool_labeled(
            ip::PENCIL,
            "重命名",
            || true,
            move || model::begin_rename(state),
        ))
        .child(tool_labeled(
            ip::TRASH,
            "删除",
            || true,
            move || model::ask_delete(state),
        ))
        .child(tool_labeled(
            ip::REFRESH,
            "刷新",
            || true,
            move || model::refresh(state),
        ))
        .child(view().flex_1())
        .child(sort_btn(state))
        .child(seg_icon(
            ip::LIST,
            move || state.get().view != "icons",
            move || model::set_view(state, "list"),
        ))
        .child(seg_icon(
            ip::LAYOUT_GRID,
            move || state.get().view == "icons",
            move || model::set_view(state, "icons"),
        ))
        .child(seg_icon(
            ip::PANEL_RIGHT,
            move || state.get().preview_on,
            move || model::toggle_preview(state),
        ))
        .child(tiny_text_btn("更多", move || {
            state.update(|ui| ui.more_open = !ui.more_open);
        }))
        .child(when(
            move || state.get().more_open,
            move || more_menu(state),
        ))
}

fn sort_btn(state: Signal<Ui>) -> impl IntoElement {
    button()
        .flex_row()
        .items_center()
        .justify_center()
        .gap(px(4.0))
        .px(px(8.0))
        .h(px(30.0))
        .rounded_md()
        .cursor(Cursor::Pointer)
        .hover(|s| s.bg(theme().secondary))
        .on_press(move || model::cycle_sort(state))
        .child(
            text(move || match state.get().sort.as_str() {
                "mtime" => "时间",
                "size" => "大小",
                _ => "名称",
            })
            .text_size(px(12.0))
            .text_color(move || theme().foreground),
        )
        .child(
            text("↑")
                .text_size(px(11.0))
                .text_color(move || theme().muted_foreground),
        )
}

fn more_menu(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(4.0))
        .child(seg_mode(state, "push", "推送"))
        .child(seg_mode(state, "pull", "拉取"))
        .child(tiny_text_btn("导入", move || {
            state.update(|ui| {
                ui.status = "拖放文件到列表区域导入，或在设置中使用高级连接".into();
                ui.more_open = false;
            });
        }))
        .child(tiny_text_btn("清空剪贴板", move || {
            model::clear_clipboard(state)
        }))
        .child(tiny_text_btn("缓存", move || {
            model::cache_selected(state)
        }))
        .child(tiny_text_btn("显示缓存", move || {
            model::reveal_cached(state)
        }))
}

fn nav_controls(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(6.0))
        .px(px(8.0))
        .h(px(44.0))
        .border_b_1()
        .border_color(move || theme().border)
        .child(icon_btn(ip::CHEVRON_LEFT, move || model::nav_back(state)))
        .child(icon_btn(ip::CHEVRON_RIGHT, move || {
            model::nav_forward(state)
        }))
        .child(icon_btn(ip::ARROW_UP, move || model::go_up(state)))
        .child(breadcrumb(state))
        .child(
            view()
                .flex_row()
                .items_center()
                .w(px(180.0))
                .h(px(32.0))
                .px(px(8.0))
                .gap(px(4.0))
                .rounded_md()
                .border_1()
                .border_color(move || theme().input)
                .bg(move || theme().muted)
                .child(
                    icons::stroke_icon(ip::SEARCH, 14.0)
                        .text_color(move || theme().muted_foreground),
                )
                .child(
                    input()
                        .flex_1()
                        .h(px(28.0))
                        .text_size(px(12.5))
                        .placeholder("筛选")
                        .value(move || state.get().filter.clone())
                        .on_change(move |text: &str| model::set_filter(state, text.to_owned())),
                ),
        )
}

fn breadcrumb(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_1()
        .flex_row()
        .items_center()
        .h(px(32.0))
        .px(px(8.0))
        .gap(px(4.0))
        .min_w(px(0.0))
        .rounded_md()
        .border_1()
        .border_color(move || theme().input)
        .bg(move || theme().background)
        .child(dynamic(move || {
            let ui = state.get();
            let os = node_os(&ui, &ui.folder_node);
            icons::os_icon(&os, 15.0).text_color(move || theme().muted_foreground)
        }))
        .child(
            scroll()
                .flex_1()
                .child(view().flex_row().items_center().child(dynamic(move || {
                    let parts = crumbs(&state.get());
                    if parts.is_empty() {
                        return text("选择左侧节点")
                            .text_size(px(12.5))
                            .text_color(move || theme().muted_foreground)
                            .into_element();
                    }
                    view()
                        .flex_row()
                        .items_center()
                        .child(each(
                            move || crumbs(&state.get()),
                            |c| c.key.clone(),
                            move |c, _| crumb_view(state, c),
                        ))
                        .into_element()
                }))),
        )
}

fn crumb_view(state: Signal<Ui>, crumb: ReadSignal<Crumb>) -> impl IntoElement {
    let node = crumb.get().node.clone();
    let path = crumb.get().path.clone();
    view()
        .flex_row()
        .items_center()
        .gap(px(2.0))
        .child(when(
            move || crumb.get().sep,
            || {
                icons::stroke_icon(ip::CHEVRON_RIGHT, 12.0)
                    .text_color(move || theme().muted_foreground)
            },
        ))
        .child(
            text(move || crumb.get().label.clone())
                .text_size(px(12.5))
                .number_of_lines(1)
                .text_color(move || {
                    if crumb.get().last {
                        theme().foreground
                    } else {
                        theme().muted_foreground
                    }
                })
                .on_press(move || {
                    model::select_folder(state, node.clone(), path.clone());
                }),
        )
}

fn name_editor(state: Signal<Ui>) -> impl IntoElement {
    when(
        move || state.get().inline_new || state.get().inline_rename,
        move || {
            view()
                .flex_row()
                .items_center()
                .gap(px(8.0))
                .px(px(12.0))
                .py(px(8.0))
                .border_b_1()
                .border_color(move || theme().border)
                .child(
                    text(move || {
                        if state.get().inline_rename {
                            "重命名"
                        } else {
                            "新建文件夹"
                        }
                    })
                    .text_size(px(12.0))
                    .text_color(move || theme().muted_foreground),
                )
                .child(field(
                    280.0,
                    "名称",
                    move || {
                        let ui = state.get();
                        if ui.inline_rename {
                            ui.rename_to.clone()
                        } else {
                            ui.new_name.clone()
                        }
                    },
                    move |t: &str| {
                        let t = t.to_owned();
                        state.update(|ui| {
                            if ui.inline_rename {
                                ui.rename_to = t;
                            } else {
                                ui.new_name = t;
                            }
                            ui.name_error.clear();
                        });
                    },
                ))
                .child(cmd("确定", true, move || {
                    if state.get().inline_rename {
                        model::rename(state);
                    } else {
                        model::mkdir(state);
                    }
                }))
                .child(cmd("取消", false, move || {
                    state.update(|ui| {
                        ui.inline_rename = false;
                        ui.inline_new = false;
                        ui.new_name.clear();
                        ui.rename_to.clear();
                        ui.name_error.clear();
                    });
                }))
                .child(when(
                    move || !state.get().name_error.is_empty(),
                    move || {
                        text(move || state.get().name_error.clone())
                            .text_size(px(12.0))
                            .text_color(move || theme().destructive)
                    },
                ))
        },
    )
}

fn file_list(state: Signal<Ui>) -> impl IntoElement {
    let hot = signal(String::new());
    view()
        .flex_1()
        .min_w(px(0.0))
        .on_file_drop(move |event: &FileDropEvent| {
            model::import_dropped(state, event.uris.clone());
        })
        .child(when(move || state.get().view != "icons", || file_header()))
        .child(
            scroll().flex_1().child(
                when(
                    move || state.get().folder_path.is_empty(),
                    || empty_hint("在左侧树里选择一个节点文件夹。"),
                )
                .otherwise(move || {
                    when(
                        move || model::visible_entries(&state.get()).is_empty(),
                        move || {
                            view()
                                .flex_1()
                                .on_context_menu(move |e: &ContextMenuEvent| {
                                    model::open_context_menu(
                                        state,
                                        -1,
                                        e.page_position.x.0,
                                        e.page_position.y.0,
                                    );
                                })
                                .child(empty_hint("这个文件夹是空的，或筛选无结果。"))
                        },
                    )
                    .otherwise(move || {
                        when(
                            move || state.get().view == "icons",
                            move || {
                                view()
                                    .flex_row()
                                    .flex_wrap()
                                    .gap(px(10.0))
                                    .p(px(12.0))
                                    .child(each(
                                        move || model::visible_entries(&state.get()),
                                        |pair| pair.1.path.clone(),
                                        move |pair, _| file_icon_tile(state, hot, pair),
                                    ))
                            },
                        )
                        .otherwise(move || {
                            view()
                                .child(each(
                                    move || model::visible_entries(&state.get()),
                                    |pair| pair.1.path.clone(),
                                    move |pair, _| file_row(state, hot, pair),
                                ))
                                .child(
                                    view()
                                        .h(px(360.0))
                                        .on_press(move || {
                                            state.update(|ui| {
                                                ui.selected = -1;
                                                ui.selected_paths.clear();
                                                ui.ctx_open = false;
                                            });
                                        })
                                        .on_context_menu(move |e: &ContextMenuEvent| {
                                            model::open_context_menu(
                                                state,
                                                -1,
                                                e.page_position.x.0,
                                                e.page_position.y.0,
                                            );
                                        }),
                                )
                        })
                    })
                }),
            ),
        )
        .child(when(
            move || !state.get().err.is_empty(),
            move || {
                text(move || state.get().err.clone())
                    .p(px(8.0))
                    .text_size(px(12.0))
                    .text_color(move || theme().destructive)
            },
        ))
}

fn file_header() -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .h(px(28.0))
        .bg(move || theme().muted)
        .border_b_1()
        .border_color(move || theme().border)
        .child(view().w(px(3.0)))
        .child(column_label("名称").flex_1().pl(px(8.0)))
        .child(column_label("修改时间").w(px(156.0)))
        .child(column_label("类型").w(px(96.0)))
        .child(column_label("大小").w(px(80.0)).pr(px(8.0)))
}

fn row_selected(state: Signal<Ui>, pair: ReadSignal<(usize, api::Entry)>) -> bool {
    let ui = state.get();
    let (orig, entry) = pair.get();
    ui.selected == orig as i32 || ui.selected_paths.iter().any(|p| p == &entry.path)
}

fn file_row(
    state: Signal<Ui>,
    hot: Signal<String>,
    pair: ReadSignal<(usize, api::Entry)>,
) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .h(px(34.0))
        .cursor(Cursor::Pointer)
        .bg(move || {
            if row_selected(state, pair) {
                theme().selection()
            } else if hot.get() == pair.get().1.path {
                theme().secondary
            } else {
                theme().card
            }
        })
        .on_hover_in(move |_: &()| hot.set(pair.get().1.path))
        .on_hover_out(move |_: &()| {
            let path = pair.get().1.path;
            if hot.get() == path {
                hot.set(String::new());
            }
        })
        .on(EventKind::Press, move |payload: &EventPayload| {
            let (orig, entry) = pair.get();
            let mut clicks = 1u32;
            if let EventPayload::Press(PressEvent {
                modifiers,
                clicks: c,
                ..
            }) = payload
            {
                if modifiers.ctrl || modifiers.cmd {
                    model::toggle_select_path(state, entry.path);
                    return;
                }
                clicks = (*c).max(1);
            }
            model::click_row(state, orig as i32, clicks);
        })
        .on_context_menu(move |e: &ContextMenuEvent| {
            let (orig, _) = pair.get();
            model::open_context_menu(state, orig as i32, e.page_position.x.0, e.page_position.y.0);
        })
        .child(view().w(px(3.0)).h(px(34.0)).bg(move || {
            if row_selected(state, pair) {
                theme().primary
            } else {
                theme().primary.opacity(0.0)
            }
        }))
        .child(view().ml(px(8.0)).child(dynamic(move || {
            let e = pair.get().1;
            let kind = icons::file_kind(&e.name, e.is_dir);
            icons::typed_file_icon(kind, 18.0)
        })))
        .child(
            text(move || pair.get().1.name.clone())
                .flex_1()
                .ml(px(8.0))
                .text_size(px(13.0))
                .number_of_lines(1),
        )
        .child(
            text(move || format_time(&pair.get().1.mod_time))
                .w(px(156.0))
                .text_size(px(12.0))
                .text_color(move || theme().muted_foreground),
        )
        .child(
            text(move || {
                let e = pair.get().1;
                icons::type_label(icons::file_kind(&e.name, e.is_dir)).to_owned()
            })
            .w(px(96.0))
            .text_size(px(12.0))
            .text_color(move || theme().muted_foreground),
        )
        .child(
            text(move || {
                let e = pair.get().1;
                size_label(e.size, e.is_dir)
            })
            .w(px(80.0))
            .text_size(px(12.0))
            .text_color(move || theme().muted_foreground),
        )
}

fn file_icon_tile(
    state: Signal<Ui>,
    hot: Signal<String>,
    pair: ReadSignal<(usize, api::Entry)>,
) -> impl IntoElement {
    view()
        .w(px(96.0))
        .h(px(104.0))
        .gap(px(6.0))
        .p(px(8.0))
        .items_center()
        .rounded(theme().radius)
        .cursor(Cursor::Pointer)
        .bg(move || {
            if row_selected(state, pair) {
                theme().selection()
            } else if hot.get() == pair.get().1.path {
                theme().secondary
            } else {
                theme().card
            }
        })
        .on_hover_in(move |_: &()| hot.set(pair.get().1.path))
        .on_hover_out(move |_: &()| {
            let path = pair.get().1.path;
            if hot.get() == path {
                hot.set(String::new());
            }
        })
        .border_1()
        .border_color(move || {
            if row_selected(state, pair) {
                theme().primary
            } else {
                theme().border
            }
        })
        .on(EventKind::Press, move |payload: &EventPayload| {
            let (orig, entry) = pair.get();
            let mut clicks = 1u32;
            if let EventPayload::Press(PressEvent {
                modifiers,
                clicks: c,
                ..
            }) = payload
            {
                if modifiers.ctrl || modifiers.cmd {
                    model::toggle_select_path(state, entry.path);
                    return;
                }
                clicks = (*c).max(1);
            }
            model::click_row(state, orig as i32, clicks);
        })
        .on_context_menu(move |e: &ContextMenuEvent| {
            let (orig, _) = pair.get();
            model::open_context_menu(state, orig as i32, e.page_position.x.0, e.page_position.y.0);
        })
        .child(dynamic(move || {
            let e = pair.get().1;
            icons::typed_file_icon(icons::file_kind(&e.name, e.is_dir), 40.0)
        }))
        .child(
            text(move || pair.get().1.name.clone())
                .text_size(px(11.0))
                .number_of_lines(2),
        )
}

fn details_pane(state: Signal<Ui>) -> impl IntoElement {
    view()
        .w(px(300.0))
        .flex_none()
        .border_l_1()
        .border_color(move || theme().border)
        .bg(move || theme().muted)
        .p(px(12.0))
        .gap(px(8.0))
        .child(
            text("详情")
                .text_size(px(11.0))
                .text_color(move || theme().muted_foreground),
        )
        .child(
            scroll()
                .flex_1()
                .child(dynamic(move || preview_body(state))),
        )
}

fn preview_body(state: Signal<Ui>) -> impl IntoElement {
    let ui = state.get();
    if !ui.preview_on {
        return text("详情已关闭。")
            .text_size(px(13.0))
            .text_color(move || theme().muted_foreground)
            .into_element();
    }
    if ui.preview_kind == "image" && !ui.preview_image.is_empty() {
        return image(move || state.get().preview_image.clone())
            .resize_mode(ResizeMode::Contain)
            .h(px(160.0))
            .into_element();
    }
    if !ui.preview_text.is_empty() {
        return view()
            .gap(px(8.0))
            .child(
                text(move || base_remote(&state.get().preview_path))
                    .text_size(px(13.0))
                    .font_weight(FontWeight::SEMIBOLD)
                    .number_of_lines(2),
            )
            .child(
                text(move || state.get().preview_text.clone())
                    .text_size(px(12.0))
                    .text_color(move || theme().foreground),
            )
            .child(meta_block(state))
            .into_element();
    }
    view()
        .gap(px(8.0))
        .child(
            text("选择文件以预览文本、图片或 PDF。")
                .text_size(px(13.0))
                .text_color(move || theme().muted_foreground),
        )
        .child(meta_block(state))
        .into_element()
}

fn meta_block(state: Signal<Ui>) -> impl IntoElement {
    when(
        move || model::selected(&state.get()).is_some(),
        move || {
            view()
                .gap(px(6.0))
                .pt(px(8.0))
                .child(meta_line("类型", move || {
                    let ui = state.get();
                    model::selected(&ui)
                        .map(|e| icons::type_label(icons::file_kind(&e.name, e.is_dir)).to_owned())
                        .unwrap_or_default()
                }))
                .child(meta_line("大小", move || {
                    let ui = state.get();
                    model::selected(&ui)
                        .map(|e| size_label(e.size, e.is_dir))
                        .unwrap_or_default()
                }))
                .child(meta_line("修改时间", move || {
                    let ui = state.get();
                    model::selected(&ui)
                        .map(|e| format_time(&e.mod_time))
                        .unwrap_or_default()
                }))
                .child(meta_line("位置", move || {
                    let ui = state.get();
                    model::selected(&ui).map(|e| e.path).unwrap_or_default()
                }))
        },
    )
}

fn meta_line(label: &'static str, value: impl Fn() -> String + 'static) -> impl IntoElement {
    view()
        .gap(px(2.0))
        .child(
            text(label)
                .text_size(px(11.0))
                .text_color(move || theme().muted_foreground),
        )
        .child(text(value).text_size(px(12.0)).number_of_lines(3))
}

fn empty_hint(message: &'static str) -> impl IntoElement {
    text(message)
        .p(px(16.0))
        .text_size(px(13.0))
        .text_color(move || theme().muted_foreground)
}

fn column_label(label: &'static str) -> nsh_core::element::Text {
    text(label)
        .text_size(px(11.0))
        .text_color(move || theme().muted_foreground)
}

fn status_bar(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(12.0))
        .px(px(12.0))
        .h(px(28.0))
        .border_t_1()
        .border_color(move || theme().border)
        .bg(move || theme().background)
        .child(
            text(move || {
                let ui = state.get();
                let n = model::visible_entries(&ui).len();
                let sel = if ui.selected_paths.is_empty() {
                    if ui.selected >= 0 { 1 } else { 0 }
                } else {
                    ui.selected_paths.len()
                };
                if sel > 0 {
                    format!("{n} 个项目 · 已选择 {sel} 项")
                } else {
                    format!("{n} 个项目")
                }
            })
            .text_size(px(11.0))
            .text_color(move || theme().muted_foreground),
        )
        .child(view().flex_1())
        .child(
            text(move || {
                let ui = state.get();
                if ui.folder_node.is_empty() {
                    ui.status.clone()
                } else {
                    format!("{} · {}", ui.folder_node, ui.status)
                }
            })
            .text_size(px(11.0))
            .text_color(move || theme().muted_foreground)
            .number_of_lines(1),
        )
}

fn transfer_bar(state: Signal<Ui>) -> impl IntoElement {
    when(
        move || !state.get().transfers.is_empty(),
        move || {
            view()
                .px(px(12.0))
                .py(px(6.0))
                .border_t_1()
                .border_color(move || theme().border)
                .bg(move || theme().card)
                .child(each(
                    move || {
                        state
                            .get()
                            .transfers
                            .iter()
                            .map(|t| job_line(&t.job))
                            .collect::<Vec<_>>()
                    },
                    |line| line.clone(),
                    move |line, _| {
                        text(move || line.get())
                            .text_size(px(11.0))
                            .text_color(move || theme().muted_foreground)
                            .number_of_lines(1)
                    },
                ))
        },
    )
}

fn icon_btn(d: &'static str, on_press: impl Fn() + 'static) -> impl IntoElement {
    button()
        .w(px(30.0))
        .h(px(30.0))
        .items_center()
        .justify_center()
        .rounded_md()
        .cursor(Cursor::Pointer)
        .bg(move || theme().secondary.opacity(0.0))
        .hover(|s| s.bg(theme().secondary))
        .on_press(on_press)
        .child(icons::stroke_icon(d, 16.0).text_color(move || theme().foreground))
}

fn tool_labeled(
    d: &'static str,
    label: &'static str,
    enabled: impl Fn() -> bool + Clone + 'static,
    on_press: impl Fn() + 'static,
) -> impl IntoElement {
    let enabled_dis = enabled.clone();
    let enabled_icon = enabled.clone();
    let enabled_label = enabled;
    button()
        .flex_row()
        .items_center()
        .gap(px(5.0))
        .px(px(8.0))
        .h(px(30.0))
        .rounded_md()
        .cursor(Cursor::Pointer)
        .hover(|s| s.bg(theme().secondary))
        .disabled(move || !enabled_dis())
        .on_press(on_press)
        .child(icons::stroke_icon(d, 15.0).text_color(move || {
            if enabled_icon() {
                theme().foreground
            } else {
                theme().muted_foreground
            }
        }))
        .child(text(label).text_size(px(12.5)).text_color(move || {
            if enabled_label() {
                theme().foreground
            } else {
                theme().muted_foreground
            }
        }))
}

fn tiny_text_btn(label: &'static str, on_press: impl Fn() + 'static) -> impl IntoElement {
    button()
        .flex_row()
        .items_center()
        .justify_center()
        .px(px(8.0))
        .h(px(30.0))
        .rounded_md()
        .cursor(Cursor::Pointer)
        .bg(move || theme().secondary.opacity(0.0))
        .hover(|s| s.bg(theme().secondary))
        .on_press(on_press)
        .child(
            text(label)
                .text_size(px(12.5))
                .text_color(move || theme().foreground),
        )
}

fn seg_mode(state: Signal<Ui>, mode: &'static str, label: &'static str) -> impl IntoElement {
    button()
        .flex_row()
        .items_center()
        .justify_center()
        .px(px(8.0))
        .h(px(30.0))
        .rounded_md()
        .cursor(Cursor::Pointer)
        .bg(move || {
            if state.get().mode == mode {
                theme().primary
            } else {
                theme().secondary
            }
        })
        .on_press(move || state.update(|ui| ui.mode = mode.to_owned()))
        .child(text(label).text_size(px(12.0)).text_color(move || {
            if state.get().mode == mode {
                theme().primary_foreground
            } else {
                theme().secondary_foreground
            }
        }))
}

fn seg_icon(
    d: &'static str,
    active: impl Fn() -> bool + Clone + 'static,
    on_press: impl Fn() + 'static,
) -> impl IntoElement {
    let active_bg = active.clone();
    let active_fg = active;
    button()
        .w(px(30.0))
        .h(px(30.0))
        .items_center()
        .justify_center()
        .rounded_md()
        .cursor(Cursor::Pointer)
        .bg(move || {
            if active_bg() {
                theme().accent
            } else {
                theme().secondary.opacity(0.0)
            }
        })
        .hover(|s| s.bg(theme().secondary))
        .on_press(on_press)
        .child(icons::stroke_icon(d, 15.0).text_color(move || {
            if active_fg() {
                theme().accent_foreground
            } else {
                theme().muted_foreground
            }
        }))
}

fn conflict_dialog(state: Signal<Ui>) -> impl IntoElement {
    when(
        move || !state.get().conflict_path.is_empty(),
        move || {
            dialog()
                .close_on_escape(false)
                .close_on_backdrop_press(false)
                .child(
                    view().flex_1().items_center().justify_center().child(
                        card()
                            .w(px(440.0))
                            .child(
                                text("目标已有同名文件")
                                    .font_weight(FontWeight::SEMIBOLD)
                                    .text_size(px(16.0)),
                            )
                            .child(
                                text(move || state.get().conflict_path.clone())
                                    .text_size(px(12.0))
                                    .text_color(move || theme().muted_foreground),
                            )
                            .child(apply_box(state))
                            .child(
                                view()
                                    .flex_row()
                                    .gap(px(8.0))
                                    .child(cmd("覆盖", true, move || {
                                        model::answer_conflict(state, "overwrite")
                                    }))
                                    .child(cmd("跳过", false, move || {
                                        model::answer_conflict(state, "skip")
                                    })),
                            ),
                    ),
                )
        },
    )
}

fn apply_box(state: Signal<Ui>) -> impl IntoElement {
    checkbox()
        .checked(move || state.get().conflict_apply)
        .on_change(move |checked: &bool| {
            let checked = *checked;
            state.update(|ui| ui.conflict_apply = checked);
        })
        .indicator(
            view()
                .w(px(16.0))
                .h(px(16.0))
                .rounded_sm()
                .border_1()
                .border_color(move || theme().border)
                .items_center()
                .justify_center()
                .bg(move || theme().background)
                .child(
                    text(move || {
                        if state.get().conflict_apply {
                            "✓"
                        } else {
                            ""
                        }
                    })
                    .text_size(px(12.0)),
                ),
        )
        .child(text("对后续冲突都这样处理").text_size(px(13.0)))
}

fn settings_dialog(state: Signal<Ui>) -> impl IntoElement {
    when(
        move || state.get().settings_open,
        move || settings_card(state),
    )
}

fn settings_card(state: Signal<Ui>) -> impl IntoElement {
    let auth = field(
        420.0,
        "tskey-auth-…",
        move || state.get().auth_input.clone(),
        move |text: &str| {
            let text = text.to_owned();
            state.update(|ui| ui.auth_input = text);
        },
    )
    .masked(true);
    dialog()
        .close_on_escape(true)
        .close_on_backdrop_press(true)
        .on_cancel(move || state.update(|ui| ui.settings_open = false))
        .child(
            view().flex_1().items_center().justify_center().child(
                card()
                    .w(px(460.0))
                    .child(
                        view()
                            .flex_row()
                            .items_center()
                            .child(
                                text("设置")
                                    .font_weight(FontWeight::SEMIBOLD)
                                    .text_size(px(16.0))
                                    .flex_1(),
                            )
                            .child(icon_btn(ip::X, move || {
                                state.update(|ui| ui.settings_open = false);
                            })),
                    )
                    .child(text("外观").text_size(px(12.0)).text_color(move || theme().muted_foreground))
                    .child(
                        view()
                            .flex_row()
                            .gap(px(6.0))
                            .child(cmd("跟随系统", false, move || model::set_theme(state, ThemePref::System)))
                            .child(cmd("浅色", false, move || model::set_theme(state, ThemePref::Light)))
                            .child(cmd("深色", false, move || model::set_theme(state, ThemePref::Dark))),
                    )
                    .child(text(move || net_line(&state.get())).text_size(px(13.0)))
                    .child(text("高级 · Agent 连接").text_size(px(12.0)).text_color(move || theme().muted_foreground))
                    .child(field(420.0, "http://127.0.0.1:8645", move || state.get().addr.clone(), move |text: &str| {
                        let text = text.to_owned();
                        state.update(|ui| ui.addr = text);
                    }))
                    .child(cmd("连接 / 刷新", false, move || model::refresh(state)))
                    .child(
                        text(move || {
                            if state.get().auth_configured {
                                "Auth key：已配置。agent 不会把 key 发回来。"
                            } else {
                                "Auth key：未配置。agent 不会把 key 发回来。"
                            }
                        })
                        .text_size(px(12.0))
                        .text_color(move || theme().muted_foreground),
                    )
                    .child(auth)
                    .child(
                        view()
                            .flex_row()
                            .gap(px(8.0))
                            .child(cmd("保存", true, move || model::save_auth_key(state)))
                            .child(cmd("清除", false, move || model::clear_auth_key(state))),
                    )
                    .child(when(
                        move || !state.get().settings_note.is_empty(),
                        move || text(move || state.get().settings_note.clone()).text_size(px(12.0)),
                    ))
                    .child(
                        text("保存在 agent 状态目录的 ts-authkey。重启 agent 后生效。若设置了 TS_AUTHKEY，环境变量优先。")
                            .text_size(px(12.0))
                            .text_color(move || theme().muted_foreground),
                    ),
            ),
        )
}

fn context_menu_overlay(state: Signal<Ui>) -> impl IntoElement {
    when(
        move || state.get().ctx_open,
        move || {
            view()
                .absolute()
                .top(px(0.0))
                .left(px(0.0))
                .right(px(0.0))
                .bottom(px(0.0))
                .z_index(100)
                .on_press(move || model::close_context_menu(state))
                .on_context_menu(move |_e: &ContextMenuEvent| model::close_context_menu(state))
                .child(
                    view()
                        .absolute()
                        .left(move || px(state.get().ctx_x.clamp(8.0, 960.0)))
                        .top(move || px(state.get().ctx_y.clamp(8.0, 480.0)))
                        .w(px(212.0))
                        .p(px(4.0))
                        .gap(px(2.0))
                        .rounded_md()
                        .border_1()
                        .border_color(move || theme().border)
                        .bg(move || theme().popover)
                        .child(when(
                            move || model::selected(&state.get()).is_some(),
                            move || {
                                view()
                                    .gap(px(2.0))
                                    .child(ctx_item(
                                        ip::FOLDER,
                                        "打开",
                                        "Enter",
                                        false,
                                        move || model::open_selected(state),
                                    ))
                                    .child(when(
                                        move || {
                                            model::selected(&state.get())
                                                .map(|e| e.is_dir)
                                                .unwrap_or(false)
                                        },
                                        move || {
                                            ctx_item(
                                                ip::PLUS,
                                                "在新标签页打开",
                                                "",
                                                false,
                                                move || model::open_selected_new_tab(state),
                                            )
                                        },
                                    ))
                                    .child(ctx_divider())
                                    .child(ctx_item(
                                        ip::COPY,
                                        "复制",
                                        "Ctrl+C",
                                        false,
                                        move || {
                                            model::close_context_menu(state);
                                            model::copy_selected(state);
                                        },
                                    ))
                                    .child(ctx_item(
                                        ip::PENCIL,
                                        "重命名",
                                        "F2",
                                        false,
                                        move || {
                                            model::close_context_menu(state);
                                            model::begin_rename(state);
                                        },
                                    ))
                                    .child(ctx_item(ip::TRASH, "删除", "Del", true, move || {
                                        model::close_context_menu(state);
                                        model::ask_delete(state);
                                    }))
                                    .child(ctx_divider())
                            },
                        ))
                        .child(ctx_item(
                            ip::CLIPBOARD,
                            "粘贴",
                            "Ctrl+V",
                            false,
                            move || {
                                model::close_context_menu(state);
                                model::paste(state);
                            },
                        ))
                        .child(ctx_item(
                            ip::FOLDER_PLUS,
                            "新建文件夹",
                            "Ctrl+Shift+N",
                            false,
                            move || {
                                model::close_context_menu(state);
                                model::begin_new_folder(state);
                            },
                        ))
                        .child(ctx_item(
                            ip::REFRESH,
                            "刷新",
                            "Ctrl+R",
                            false,
                            move || {
                                model::close_context_menu(state);
                                model::refresh(state);
                            },
                        )),
                )
        },
    )
}

fn ctx_divider() -> impl IntoElement {
    view()
        .h(px(1.0))
        .my(px(2.0))
        .mx(px(4.0))
        .bg(move || theme().border)
}

fn ctx_item(
    icon: &'static str,
    label: &'static str,
    shortcut: &'static str,
    destructive: bool,
    on_press: impl Fn() + 'static,
) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .h(px(28.0))
        .px(px(8.0))
        .gap(px(8.0))
        .rounded_sm()
        .cursor(Cursor::Pointer)
        .hover(|s| s.bg(theme().secondary))
        .on_press(on_press)
        .child(icons::stroke_icon(icon, 14.0).text_color(move || {
            if destructive {
                theme().destructive
            } else {
                theme().muted_foreground
            }
        }))
        .child(
            text(label)
                .flex_1()
                .text_size(px(12.5))
                .text_color(move || {
                    if destructive {
                        theme().destructive
                    } else {
                        theme().foreground
                    }
                }),
        )
        .child(
            text(shortcut)
                .text_size(px(11.0))
                .text_color(move || theme().muted_foreground),
        )
}

fn delete_dialog(state: Signal<Ui>) -> impl IntoElement {
    when(
        move || state.get().delete_open,
        move || {
            dialog()
                .close_on_escape(true)
                .on_cancel(move || model::cancel_delete(state))
                .child(
                    view().flex_1().items_center().justify_center().child(
                        card()
                            .w(px(480.0))
                            .child(
                                text("确认永久删除")
                                    .font_weight(FontWeight::SEMIBOLD)
                                    .text_color(move || theme().destructive),
                            )
                            .child(
                                text(move || state.get().delete_note.clone()).text_size(px(12.0)),
                            )
                            .child(
                                view()
                                    .flex_row()
                                    .gap(px(8.0))
                                    .child(cmd("取消", true, move || model::cancel_delete(state)))
                                    .child(cmd_label(
                                        move || {
                                            format!(
                                                "永久删除 {} 项",
                                                state.get().delete_paths.len()
                                            )
                                        },
                                        move || model::confirm_delete(state),
                                    )),
                            ),
                    ),
                )
        },
    )
}

fn daemon_dialog(state: Signal<Ui>) -> impl IntoElement {
    when(
        move || state.get().daemon_open,
        move || {
            dialog()
                .on_cancel(move || model::close_daemon_help(state))
                .child(
                    view().flex_1().items_center().justify_center().child(
                        card()
                            .w(px(520.0))
                            .child(
                                text(move || {
                                    state
                                        .get()
                                        .daemon_guide
                                        .as_ref()
                                        .map(|g| g.title.clone())
                                        .unwrap_or_else(|| "Daemon".into())
                                })
                                .font_weight(FontWeight::SEMIBOLD),
                            )
                            .child(
                                text(move || {
                                    state
                                        .get()
                                        .daemon_guide
                                        .as_ref()
                                        .map(|g| g.summary.clone())
                                        .unwrap_or_default()
                                })
                                .text_size(px(12.0)),
                            )
                            .child(
                                view()
                                    .flex_row()
                                    .gap(px(6.0))
                                    .child(cmd("Linux", false, move || {
                                        state.update(|ui| ui.daemon_os = "linux".into())
                                    }))
                                    .child(cmd("Windows", false, move || {
                                        state.update(|ui| ui.daemon_os = "windows".into())
                                    }))
                                    .child(cmd("macOS", false, move || {
                                        state.update(|ui| ui.daemon_os = "macos".into())
                                    })),
                            )
                            .child(
                                text(move || {
                                    let ui = state.get();
                                    ui.daemon_guide
                                        .as_ref()
                                        .map(|g| {
                                            crate::daemon::commands_clipboard(g, &ui.daemon_os)
                                        })
                                        .unwrap_or_default()
                                })
                                .text_size(px(11.0)),
                            )
                            .child(
                                text(move || {
                                    state
                                        .get()
                                        .daemon_guide
                                        .as_ref()
                                        .map(|g| g.note.clone())
                                        .unwrap_or_default()
                                })
                                .text_size(px(11.0))
                                .text_color(move || theme().muted_foreground),
                            )
                            .child(
                                view()
                                    .flex_row()
                                    .gap(px(8.0))
                                    .child(cmd("复制命令", true, move || {
                                        model::copy_daemon_command(state)
                                    }))
                                    .child(cmd("关闭", false, move || {
                                        model::close_daemon_help(state)
                                    })),
                            ),
                    ),
                )
        },
    )
}

#[derive(Clone, PartialEq)]
struct Crumb {
    key: String,
    label: String,
    node: String,
    path: String,
    sep: bool,
    last: bool,
}

fn node_os(ui: &Ui, id: &str) -> String {
    ui.nodes
        .iter()
        .find(|n| n.id == id)
        .map(|n| icons::resolve_node_os(&n.os, &n.id, &n.name))
        .unwrap_or_else(|| icons::resolve_node_os("", id, id))
}

fn crumbs(ui: &Ui) -> Vec<Crumb> {
    if ui.folder_node.is_empty() || ui.folder_path.is_empty() {
        return Vec::new();
    }
    let host = ui
        .nodes
        .iter()
        .find(|n| n.id == ui.folder_node)
        .map(|n| {
            if n.name.is_empty() {
                n.id.clone()
            } else {
                n.name.clone()
            }
        })
        .unwrap_or_else(|| ui.folder_node.clone());
    let path = token::norm_remote(&ui.folder_path);
    let root = ui
        .nodes
        .iter()
        .find(|n| n.id == ui.folder_node)
        .and_then(|n| {
            n.read_roots
                .iter()
                .filter(|r| token::path_is_under(&path, r))
                .max_by_key(|r| token::norm_remote(r).len())
                .cloned()
        });
    let root_path = root
        .as_deref()
        .map(token::norm_remote)
        .unwrap_or_else(|| path.clone());
    let mut out = vec![Crumb {
        key: format!("h:{}", ui.folder_node),
        label: host,
        node: ui.folder_node.clone(),
        path: root_path.clone(),
        sep: false,
        last: false,
    }];
    if let Some(root) = root {
        let root_norm = token::norm_remote(&root);
        let rel = path
            .strip_prefix(&root_norm)
            .unwrap_or("")
            .trim_start_matches('/');
        let mut acc = root_norm;
        for part in rel.split('/').filter(|s| !s.is_empty()) {
            acc = token::join_remote(&acc, part);
            out.push(Crumb {
                key: format!("p:{acc}"),
                label: part.to_owned(),
                node: ui.folder_node.clone(),
                path: acc.clone(),
                sep: true,
                last: false,
            });
        }
    }
    if let Some(last) = out.last_mut() {
        last.last = true;
    }
    out
}

fn net_line(ui: &Ui) -> String {
    let mode = if ui.net_mode.is_empty() {
        "…".to_owned()
    } else {
        ui.net_mode.clone()
    };
    if !ui.net_requested.is_empty() && ui.net_requested != ui.net_mode {
        format!("网络模式：{mode}（请求的 -net：{}）", ui.net_requested)
    } else {
        format!("网络模式：{mode}")
    }
}

fn card() -> nsh_core::element::View {
    view()
        .gap(px(10.0))
        .p(px(16.0))
        .rounded_lg()
        .border_1()
        .border_color(move || theme().border)
        .bg(move || theme().popover)
}

fn cmd(label: &'static str, primary: bool, on_press: impl Fn() + 'static) -> impl IntoElement {
    button()
        .px(px(10.0))
        .py(px(6.0))
        .rounded_md()
        .bg(move || {
            if primary {
                theme().primary
            } else {
                theme().secondary
            }
        })
        .on_press(on_press)
        .child(text(label).text_size(px(13.0)).text_color(move || {
            if primary {
                theme().primary_foreground
            } else {
                theme().secondary_foreground
            }
        }))
}

fn cmd_label(
    label: impl Fn() -> String + 'static,
    on_press: impl Fn() + 'static,
) -> impl IntoElement {
    button()
        .px(px(10.0))
        .py(px(6.0))
        .rounded_md()
        .bg(move || theme().secondary)
        .on_press(on_press)
        .child(
            text(label)
                .text_size(px(13.0))
                .text_color(move || theme().secondary_foreground),
        )
}

fn field(
    width: f32,
    placeholder: &'static str,
    value: impl Fn() -> String + 'static,
    on_change: impl Fn(&str) + 'static,
) -> nsh_core::element::Input {
    input()
        .w(px(width))
        .h(px(32.0))
        .px(px(8.0))
        .rounded_md()
        .border_1()
        .border_color(move || theme().input)
        .bg(move || theme().background)
        .text_size(px(13.0))
        .placeholder(placeholder)
        .value(value)
        .on_change(on_change)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn theme_file_drives_blue_selection() {
        let tokens = theme_tokens();
        let primary = tokens.color("primary").expect("primary");
        let selection = tokens.color("selection").expect("selection");
        assert_eq!(primary.light.to_hex(), "#2563EB");
        assert_eq!(selection.light.to_hex(), "#DCE8FD");
        assert!(primary.dark.to_hex().eq_ignore_ascii_case("#2F6FE4"));
    }

    #[test]
    fn crumbs_drop_the_read_root_prefix() {
        let mut ui = Ui::new();
        ui.nodes.push(api::NodeInfo {
            id: "alpha".into(),
            name: "alpha".into(),
            os: "macos".into(),
            online: true,
            agent_ok: true,
            self_node: true,
            read_only: false,
            read_roots: vec!["/tmp/demo/alpha".into()],
        });
        ui.folder_node = "alpha".into();
        ui.folder_path = "/tmp/demo/alpha/notes".into();
        let parts = crumbs(&ui);
        let labels: Vec<_> = parts.iter().map(|c| c.label.as_str()).collect();
        assert_eq!(labels, ["alpha", "notes"]);
        assert_eq!(parts[0].path, "/tmp/demo/alpha");
        assert_eq!(parts[1].path, "/tmp/demo/alpha/notes");
        assert!(parts[1].sep && parts[1].last);
    }
}
