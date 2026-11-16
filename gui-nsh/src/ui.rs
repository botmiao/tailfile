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
pub mod model {
    pub use crate::model::*;
}
use self::model::{TreeRow, Ui};
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
pub const SIDEBAR_W: f32 = 262.0;
/// Collapsed rail: host icons only.
pub const SIDEBAR_RAIL: f32 = 56.0;

pub fn sidebar_width(collapsed: bool) -> f32 {
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
                .child(
                    when(
                        move || state.get().nodes.is_empty(),
                        move || {
                            text(if state.get().sidebar_collapsed {
                                "—"
                            } else {
                                "还没有节点"
                            })
                            .p(px(12.0))
                            .text_size(px(13.0))
                            .text_color(move || theme().muted_foreground)
                        },
                    )
                    .otherwise(move || {
                        when(
                            move || state.get().sidebar_collapsed,
                            move || {
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
                            },
                        )
                        .otherwise(move || {
                            view().child(each(
                                move || model::tree_rows(&state.get()),
                                |row| row.key.clone(),
                                move |row, _| tree_item(state, hot, row),
                            ))
                        })
                    }),
                )
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

pub fn collapsed_rows(ui: &Ui) -> Vec<TreeRow> {
    model::tree_rows(ui)
        .into_iter()
        .filter(|row| row.is_node)
        .collect()
}

pub fn sidebar_rows(ui: &Ui) -> Vec<TreeRow> {
    if ui.sidebar_collapsed {
        collapsed_rows(ui)
    } else {
        model::tree_rows(ui)
    }
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
            text(move || {
                if state.get().sort == "mtime" {
                    "↓"
                } else {
                    "↑"
                }
            })
            .text_size(px(11.0))
            .text_color(move || theme().muted_foreground),
        )
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
                .cursor(Cursor::Pointer)
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
    let (orig, entry) = pair.get();
    model::is_row_selected(&state.get(), orig, &entry.path)
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
                if modifiers.shift {
                    model::select_range(state, orig as i32);
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
                if modifiers.shift {
                    model::select_range(state, orig as i32);
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
                    move || state.get().transfers.clone(),
                    |t| t.id.clone(),
                    move |t, _| {
                        view()
                            .flex_row()
                            .items_center()
                            .gap(px(8.0))
                            .child(
                                text(move || job_line(&t.get().job))
                                    .flex_1()
                                    .text_size(px(11.0))
                                    .text_color(move || theme().muted_foreground)
                                    .number_of_lines(1),
                            )
                            .child(when(
                                move || {
                                    matches!(
                                        t.get().job.status.as_str(),
                                        "running" | "queued" | "conflict"
                                    )
                                },
                                move || {
                                    tiny_text_btn("取消", move || {
                                        model::cancel_transfer(state, t.get().id);
                                    })
                                },
                            ))
                            .child(when(
                                move || matches!(t.get().job.status.as_str(), "error" | "canceled"),
                                move || {
                                    tiny_text_btn("重试", move || {
                                        model::resume_transfer(state, t.get().id);
                                    })
                                },
                            ))
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

/// Settings, grouped top to bottom: appearance, transfer (paste mode and
/// cache directory), agent connection, auth key.
/// Edits are a draft until Apply (stay open) or Save (close); Cancel,
/// Escape, the close button, and the backdrop discard them.
fn settings_card(state: Signal<Ui>) -> impl IntoElement {
    dialog()
        .close_on_escape(true)
        .close_on_backdrop_press(true)
        .on_cancel(move || model::cancel_settings(state))
        .child(
            view().flex_1().items_center().justify_center().child(
                card()
                    .w(px(SETTINGS_W))
                    .gap(px(14.0))
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
                            .child(icon_btn(ip::X, move || model::cancel_settings(state))),
                    )
                    .child(settings_section("外观"))
                    .child(form_row(
                        "主题",
                        view()
                            .flex_row()
                            .child(segmented(
                                theme_option(state, ThemePref::System),
                                theme_option(state, ThemePref::Light),
                                theme_option(state, ThemePref::Dark),
                            )),
                    ))
                    .child(section_divider())
                    .child(settings_section("传输"))
                    .child(form_row(
                        "粘贴方式",
                        view()
                            .gap(px(6.0))
                            .child(view().flex_row().child(segmented2(
                                mode_option(state, "push", "推送"),
                                mode_option(state, "pull", "拉取"),
                            )))
                            .child(
                                text("推送由源节点发起，把文件写到目标；拉取由目标节点发起，从源节点读取。")
                                    .w(px(SETTINGS_FIELD_W))
                                    .text_size(px(12.0))
                                    .text_color(move || theme().muted_foreground),
                            ),
                    ))
                    .child(form_row(
                        "缓存目录",
                        view()
                            .gap(px(6.0))
                            .child(
                                view()
                                    .flex_row()
                                    .items_center()
                                    .gap(px(8.0))
                                    .child(field(
                                        SETTINGS_ADDR_W,
                                        "留空使用默认目录",
                                        move || state.get().draft_cache_dir.clone(),
                                        move |text: &str| model::set_draft_cache_dir(state, text),
                                    ))
                                    .child(view().w(px(TEST_BTN_W)).child(dialog_btn(
                                        "打开",
                                        BtnKind::Secondary,
                                        move || !state.get().cache_dir.is_empty(),
                                        move || model::open_cache_dir(state),
                                    ))),
                            )
                            .child(
                                text(move || cache_dir_help(&state.get()))
                                    .w(px(SETTINGS_FIELD_W))
                                    .text_size(px(12.0))
                                    .text_color(move || theme().muted_foreground),
                            ),
                    ))
                    .child(section_divider())
                    .child(settings_section("Agent 连接"))
                    .child(form_row(
                        "地址",
                        view()
                            .flex_row()
                            .items_center()
                            .gap(px(8.0))
                            .child(field(
                                SETTINGS_ADDR_W,
                                "http://127.0.0.1:8645",
                                move || state.get().draft_addr.clone(),
                                move |text: &str| model::set_draft_addr(state, text),
                            ))
                            .child(view().w(px(TEST_BTN_W)).child(dialog_btn(
                                "测试连接",
                                BtnKind::Secondary,
                                || true,
                                move || model::test_draft_connection(state),
                            ))),
                    ))
                    .child(form_row(
                        "网络模式",
                        view().h(px(32.0)).justify_center().child(
                            text(move || net_value(&state.get())).text_size(px(13.0)),
                        ),
                    ))
                    .child(section_divider())
                    .child(settings_section("Tailscale 认证"))
                    .child(form_row(
                        "Auth key",
                        view()
                            .gap(px(8.0))
                            .child(
                                view().h(px(32.0)).justify_center().child(
                                    text(move || {
                                        let ui = state.get();
                                        match (ui.auth_configured, ui.draft_clear_auth) {
                                            (true, true) => "已配置，应用后删除",
                                            (true, false) => "已配置（agent 不会把 key 发回来）",
                                            (false, _) => "未配置",
                                        }
                                    })
                                    .text_size(px(13.0)),
                                ),
                            )
                            .child(
                                field(
                                    SETTINGS_FIELD_W,
                                    "输入新的 tskey-auth-…",
                                    move || state.get().auth_input.clone(),
                                    move |text: &str| model::set_draft_auth(state, text),
                                )
                                .masked(true),
                            )
                            .child(when(
                                move || state.get().auth_configured,
                                move || clear_auth_box(state),
                            ))
                            .child(
                                text("保存在 agent 状态目录的 ts-authkey，重启 agent 后生效。若设置了 TS_AUTHKEY，环境变量优先。")
                                    .w(px(SETTINGS_FIELD_W))
                                    .text_size(px(12.0))
                                    .text_color(move || theme().muted_foreground),
                            ),
                    ))
                    .child(section_divider())
                    .child(
                        view()
                            .flex_row()
                            .items_center()
                            .gap(px(8.0))
                            .child(
                                text(move || state.get().settings_note.clone())
                                    .flex_1()
                                    .text_size(px(12.0))
                                    .text_color(move || theme().muted_foreground),
                            )
                            .child(dialog_btn(
                                "应用",
                                BtnKind::Secondary,
                                move || model::settings_dirty(&state.get()),
                                move || {
                                    model::apply_settings(state);
                                },
                            ))
                            .child(dialog_btn(
                                "保存",
                                BtnKind::Primary,
                                || true,
                                move || model::save_settings(state),
                            ))
                            .child(dialog_btn(
                                "取消",
                                BtnKind::Secondary,
                                || true,
                                move || model::cancel_settings(state),
                            )),
                    ),
            ),
        )
}

const SETTINGS_W: f32 = 540.0;
const SETTINGS_LABEL_W: f32 = 88.0;
/// Card width minus border (2 × 1), padding (2 × 16), label, and the 12 px
/// row gap.
const SETTINGS_FIELD_W: f32 = SETTINGS_W - 2.0 - 32.0 - SETTINGS_LABEL_W - 12.0;
/// The address field leaves room for the 8 px gap and the test button.
const SETTINGS_ADDR_W: f32 = SETTINGS_FIELD_W - 8.0 - TEST_BTN_W;
const TEST_BTN_W: f32 = 76.0;

fn settings_section(title: &'static str) -> impl IntoElement {
    text(title)
        .font_weight(FontWeight::SEMIBOLD)
        .text_size(px(13.0))
}

fn section_divider() -> impl IntoElement {
    view().h(px(1.0)).bg(move || theme().border)
}

/// Label on the left, control column on the right.
fn form_row(label: &'static str, control: impl IntoElement) -> impl IntoElement {
    view()
        .flex_row()
        .items_start()
        .gap(px(12.0))
        .child(
            view()
                .w(px(SETTINGS_LABEL_W))
                .h(px(32.0))
                .justify_center()
                .child(
                    text(label)
                        .text_size(px(13.0))
                        .text_color(move || theme().muted_foreground),
                ),
        )
        .child(view().flex_1().child(control))
}

fn theme_option(state: Signal<Ui>, pref: ThemePref) -> impl IntoElement {
    let label = pref.label();
    let selected = pref.clone();
    seg_option(
        label,
        move || state.get().draft_theme == selected,
        move || model::set_draft_theme(state, pref.clone()),
    )
}

fn mode_option(state: Signal<Ui>, mode: &'static str, label: &'static str) -> impl IntoElement {
    seg_option(
        label,
        move || state.get().draft_mode == mode,
        move || model::set_draft_mode(state, mode),
    )
}

pub fn cache_dir_help(ui: &Ui) -> String {
    let fm = model::file_manager_name();
    if ui.cache_dir_default.is_empty() {
        format!("在{fm}中显示远程文件时，先下载到这里。")
    } else {
        format!(
            "在{fm}中显示远程文件时，先下载到这里。留空恢复默认：{}",
            ui.cache_dir_default
        )
    }
}

fn clear_auth_box(state: Signal<Ui>) -> impl IntoElement {
    checkbox()
        .checked(move || state.get().draft_clear_auth)
        .on_change(move |_: &bool| model::toggle_draft_clear_auth(state))
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
                    text(move || if state.get().draft_clear_auth { "✓" } else { "" })
                        .text_size(px(12.0)),
                ),
        )
        .child(text("应用时删除已保存的 key").text_size(px(13.0)))
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
                        .left(move || {
                            let max_x = (window().width.0 - 220.0).max(8.0);
                            px(state.get().ctx_x.clamp(8.0, max_x))
                        })
                        .top(move || {
                            let max_y = (window().height.0 - 268.0).max(8.0);
                            px(state.get().ctx_y.clamp(8.0, max_y))
                        })
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
                                        if selected_is_dir(state) { "打开" } else { reveal_label() },
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

fn selected_is_dir(state: Signal<Ui>) -> bool {
    model::selected(&state.get()).map(|e| e.is_dir).unwrap_or(false)
}

/// Menu label for opening a file: fetch it and reveal it locally.
fn reveal_label() -> &'static str {
    if cfg!(target_os = "macos") {
        "在访达中显示"
    } else if cfg!(target_os = "windows") {
        "在资源管理器中显示"
    } else {
        "在文件管理器中显示"
    }
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
                            .child(view().flex_row().child(segmented(
                                daemon_os_option(state, "linux", "Linux"),
                                daemon_os_option(state, "windows", "Windows"),
                                daemon_os_option(state, "macos", "macOS"),
                            )))
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Crumb {
    pub key: String,
    pub label: String,
    pub node: String,
    pub path: String,
    pub sep: bool,
    pub last: bool,
}

pub fn node_os(ui: &Ui, id: &str) -> String {
    ui.nodes
        .iter()
        .find(|n| n.id == id)
        .map(|n| icons::resolve_node_os(&n.os, &n.id, &n.name))
        .unwrap_or_else(|| icons::resolve_node_os("", id, id))
}

pub fn crumbs(ui: &Ui) -> Vec<Crumb> {
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

pub fn net_line(ui: &Ui) -> String {
    format!("网络模式：{}", net_value(ui))
}

/// The resolved network mode, plus the requested `--net` when it differs.
pub fn net_value(ui: &Ui) -> String {
    let mode = if ui.net_mode.is_empty() {
        "…".to_owned()
    } else {
        ui.net_mode.clone()
    };
    if !ui.net_requested.is_empty() && ui.net_requested != ui.net_mode {
        format!("{mode}（请求的 --net：{}）", ui.net_requested)
    } else {
        mode
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
    let kind = if primary { BtnKind::Primary } else { BtnKind::Secondary };
    dialog_btn(label, kind, || true, on_press)
}

fn cmd_label(
    label: impl Fn() -> String + 'static,
    on_press: impl Fn() + 'static,
) -> impl IntoElement {
    btn_shell(BtnKind::Secondary, || true, on_press).child(
        text(label)
            .text_size(px(13.0))
            .text_color(move || theme().secondary_foreground),
    )
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum BtnKind {
    Primary,
    Secondary,
}

/// The hovered shade of a filled button: darker in light mode, lighter in
/// dark mode, so the change reads on either background.
fn hover_shade(c: AdaptiveColor, amount: f32) -> AdaptiveColor {
    AdaptiveColor {
        light: c.light.darken(amount * 0.6),
        dark: c.dark.lighten(amount),
    }
}

fn btn_bg(kind: BtnKind, hot: bool, enabled: bool) -> AdaptiveColor {
    let t = theme();
    match (kind, enabled, hot) {
        (_, false, _) => t.secondary.opacity(0.6),
        (BtnKind::Primary, true, false) => t.primary,
        (BtnKind::Primary, true, true) => hover_shade(t.primary, 0.14),
        (BtnKind::Secondary, true, false) => t.secondary,
        (BtnKind::Secondary, true, true) => hover_shade(t.secondary, 0.22),
    }
}

/// A filled dialog button without its label: pointer cursor, hover shade,
/// and a dimmed look while `enabled` is false.
fn btn_shell(
    kind: BtnKind,
    enabled: impl Fn() -> bool + Clone + 'static,
    on_press: impl Fn() + 'static,
) -> nsh_base::ButtonElement {
    let enabled_bg = enabled.clone();
    button()
        .flex_row()
        .items_center()
        .justify_center()
        .min_w(px(64.0))
        .h(px(30.0))
        .px(px(12.0))
        .rounded_md()
        .cursor(Cursor::Pointer)
        .bg(move || btn_bg(kind, false, enabled_bg()))
        .hover(move |s| s.bg(btn_bg(kind, true, true)))
        .disabled_style(move |s| s.bg(btn_bg(kind, false, false)))
        .disabled(move || !enabled())
        .on_press(on_press)
}

fn dialog_btn(
    label: &'static str,
    kind: BtnKind,
    enabled: impl Fn() -> bool + Clone + 'static,
    on_press: impl Fn() + 'static,
) -> impl IntoElement {
    let enabled_fg = enabled.clone();
    btn_shell(kind, enabled, on_press).child(text(label).text_size(px(13.0)).text_color(move || {
        match (kind, enabled_fg()) {
            (_, false) => theme().muted_foreground,
            (BtnKind::Primary, true) => theme().primary_foreground,
            (BtnKind::Secondary, true) => theme().secondary_foreground,
        }
    }))
}

/// Three mutually exclusive options in one track; the chosen one is
/// filled with the primary color.
fn segmented(a: impl IntoElement, b: impl IntoElement, c: impl IntoElement) -> impl IntoElement {
    view()
        .flex_row()
        .gap(px(2.0))
        .p(px(2.0))
        .rounded_md()
        .bg(move || theme().secondary)
        .child(a)
        .child(b)
        .child(c)
}

fn segmented2(a: impl IntoElement, b: impl IntoElement) -> impl IntoElement {
    view()
        .flex_row()
        .gap(px(2.0))
        .p(px(2.0))
        .rounded_md()
        .bg(move || theme().secondary)
        .child(a)
        .child(b)
}

fn seg_option(
    label: &'static str,
    selected: impl Fn() -> bool + Clone + 'static,
    on_press: impl Fn() + 'static,
) -> impl IntoElement {
    let hot = signal(false);
    let selected_bg = selected.clone();
    let selected_fg = selected;
    view()
        .flex_row()
        .items_center()
        .justify_center()
        .min_w(px(72.0))
        .h(px(28.0))
        .px(px(12.0))
        .rounded_md()
        .cursor(Cursor::Pointer)
        .bg(move || {
            if selected_bg() {
                theme().primary
            } else if hot.get() {
                hover_shade(theme().secondary, 0.22)
            } else {
                theme().secondary.opacity(0.0)
            }
        })
        .on_hover_in(move |_: &()| hot.set(true))
        .on_hover_out(move |_: &()| hot.set(false))
        .on_press(on_press)
        .child(text(label).text_size(px(13.0)).text_color(move || {
            if selected_fg() {
                theme().primary_foreground
            } else {
                theme().secondary_foreground
            }
        }))
}

fn daemon_os_option(state: Signal<Ui>, os: &'static str, label: &'static str) -> impl IntoElement {
    seg_option(
        label,
        move || state.get().daemon_os == os,
        move || state.update(|ui| ui.daemon_os = os.into()),
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
