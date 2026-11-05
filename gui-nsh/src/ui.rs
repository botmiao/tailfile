//! Prototype-matching Finder layout: clean header, tabs, node tree,
//! single action toolbar, breadcrumb controls, list/grid, right details.

use nsh_base::{button, checkbox, dialog, input, theme};
use crate::icons::{self, path as ip};
use crate::persist::ThemePref;
use nsh_core::prelude::*;
use nsh_core::{ContextMenuEvent, EventKind, EventPayload, PressEvent};

use crate::api::{self, job_line, size_label};
use crate::model::{self, TreeRow, Ui};
use crate::token::format_time;

#[component]
pub fn App() -> impl IntoElement {
    let state = signal(Ui::new());
    model::boot(state);
    view()
        .flex_1()
        .bg(move || theme().background)
        .text_color(move || theme().foreground)
        .child(header(state))
        .child(tab_bar(state))
        .child(body(state))
        .child(status_bar(state))
        .child(transfer_bar(state))
        .child(conflict_dialog(state))
        .child(settings_dialog(state))
        .child(delete_dialog(state))
        .child(daemon_dialog(state))
}

fn header(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .h(px(44.0))
        .px(px(12.0))
        .gap(px(8.0))
        .bg(move || theme().background)
        .child(icons::logo_mark(22.0))
        .child(
            text(icons::LOGO_TITLE)
                .font_weight(FontWeight::SEMIBOLD)
                .text_size(px(15.0)),
        )
        .child(view().flex_1())
        .child(icon_btn(ip::SUN, move || {
            let next = match state.get().theme {
                ThemePref::System => ThemePref::Light,
                ThemePref::Light => ThemePref::Dark,
                ThemePref::Dark => ThemePref::System,
            };
            model::set_theme(state, next);
        }))
        .child(icon_btn(ip::SETTINGS, move || model::open_settings(state)))
}

fn tab_bar(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_end()
        .h(px(36.0))
        .px(px(10.0))
        .gap(px(4.0))
        .bg(move || theme().background)
        .border_b_1()
        .border_color(move || theme().border)
        .child(scroll().flex_1().child(view().flex_row().gap(px(2.0)).items_end().child(dynamic(move || {
            let ui = state.get();
            ui.tabs
                .iter()
                .enumerate()
                .map(|(index, tab)| {
                    let title = tab.title(&ui.nodes);
                    let selected = ui.active_tab == index;
                    tab_chip(state, index, title, selected)
                })
                .collect::<Vec<_>>()
        }))))
        .child(icon_btn(ip::PLUS, move || model::new_tab_from_current(state)))
        .child(icon_btn(ip::X, move || model::close_active_tab(state)))
        .child(when(
            move || !state.get().closed_tabs.is_empty(),
            move || tiny_text_btn("重开", move || model::reopen_closed_tab(state)),
        ))
}

fn tab_chip(state: Signal<Ui>, index: usize, title: String, selected: bool) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(6.0))
        .h(px(30.0))
        .px(px(12.0))
        .rounded(theme().radius)
        .bg(move || {
            if state.get().active_tab == index {
                theme().card
            } else {
                theme().muted
            }
        })
        .border_1()
        .border_color(move || {
            if state.get().active_tab == index {
                theme().border
            } else {
                theme().border.opacity(0.0)
            }
        })
        .on_press(move || model::switch_tab(state, index))
        .child(icons::stroke_icon(ip::LAPTOP, 14.0).text_color(move || {
            if state.get().active_tab == index {
                theme().accent_foreground
            } else {
                theme().muted_foreground
            }
        }))
        .child(
            text(title)
                .text_size(px(12.0))
                .number_of_lines(1)
                .text_color(move || {
                    if state.get().active_tab == index {
                        theme().foreground
                    } else {
                        theme().muted_foreground
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
    view()
        .w(px(262.0))
        .flex_none()
        .border_r_1()
        .border_color(move || theme().sidebar_border)
        .bg(move || theme().sidebar)
        .child(
            view()
                .px(px(12.0))
                .py(px(10.0))
                .gap(px(2.0))
                .child(
                    text(move || {
                        let n = state.get().nodes.len();
                        format!("Tailnet · {n} 个节点")
                    })
                    .text_size(px(11.0))
                    .text_color(move || theme().muted_foreground),
                ),
        )
        .child(
            scroll().flex_1().child(when(
                move || model::tree_rows(&state.get()).is_empty(),
                move || {
                    text("还没有节点")
                        .p(px(12.0))
                        .text_size(px(13.0))
                        .text_color(move || theme().muted_foreground)
                },
            ).otherwise(move || {
                view().child(each(
                    move || model::tree_rows(&state.get()),
                    |row| row.key.clone(),
                    move |row, _| tree_item(state, row),
                ))
            })),
        )
        .child(
            text("仅显示白名单目录 · Ctrl/右键 = 新标签页")
                .px(px(12.0))
                .py(px(8.0))
                .text_size(px(10.0))
                .text_color(move || theme().muted_foreground),
        )
}

fn tree_item(state: Signal<Ui>, row: ReadSignal<TreeRow>) -> impl IntoElement {
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
        .bg(move || {
            let row = row.get();
            let ui = state.get();
            if !row.is_node && row.node == ui.folder_node && row.path == ui.folder_path {
                theme().sidebar_accent
            } else {
                theme().sidebar.opacity(0.0)
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
                let online = state
                    .get()
                    .nodes
                    .iter()
                    .find(|n| n.id == row.node)
                    .map(|n| n.online && n.agent_ok)
                    .unwrap_or(false);
                let ro = state
                    .get()
                    .nodes
                    .iter()
                    .find(|n| n.id == row.node)
                    .map(|n| n.read_only)
                    .unwrap_or(false);
                view()
                    .flex_row()
                    .items_center()
                    .gap(px(6.0))
                    .child(icons::stroke_icon(ip::LAPTOP, 14.0).text_color(move || theme().muted_foreground))
                    .child(icons::status_dot(online, ro))
                    .into_element()
            } else {
                icons::folder_icon(14.0, false)
                    .text_color(move || theme().muted_foreground)
                    .into_element()
            }
        }))
        .child(
            text(move || row.get().label.clone())
            .text_size(px(12.5))
            .number_of_lines(1)
            .flex_1(),
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
        .gap(px(4.0))
        .px(px(10.0))
        .py(px(8.0))
        .border_b_1()
        .border_color(move || theme().border)
        .child(icon_btn(ip::CHEVRON_LEFT, move || model::nav_back(state)))
        .child(icon_btn(ip::CHEVRON_RIGHT, move || model::nav_forward(state)))
        .child(icon_btn(ip::ARROW_UP, move || model::go_up(state)))
        .child(view().w(px(6.0)))
        .child(tool_labeled(ip::FOLDER_PLUS, "新建文件夹", move || model::begin_new_folder(state)))
        .child(tool_labeled(ip::COPY, "复制", move || model::copy_selected(state)))
        .child(tool_labeled(ip::CLIPBOARD, "粘贴", move || model::paste(state)))
        .child(tool_labeled(ip::PENCIL, "重命名", move || model::begin_rename(state)))
        .child(tool_labeled(ip::TRASH, "删除", move || model::ask_delete(state)))
        .child(tool_labeled(ip::REFRESH, "刷新", move || model::refresh(state)))
        .child(view().flex_1())
        .child(when(
            move || state.get().clip_set,
            move || {
                text(move || {
                    let ui = state.get();
                    format!("剪贴板 {} 项", ui.clip_items.len().max(1))
                })
                .text_size(px(11.0))
                .text_color(move || theme().muted_foreground)
            },
        ))
        .child(tiny_text_btn("更多", move || {
            state.update(|ui| ui.more_open = !ui.more_open);
        }))
        .child(when(
            move || state.get().more_open,
            move || more_menu(state),
        ))
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
        .child(tiny_text_btn("清空剪贴板", move || model::clear_clipboard(state)))
        .child(tiny_text_btn("缓存", move || model::cache_selected(state)))
        .child(tiny_text_btn("显示缓存", move || model::reveal_cached(state)))
}

fn nav_controls(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(6.0))
        .px(px(10.0))
        .py(px(8.0))
        .border_b_1()
        .border_color(move || theme().border)
        .child(
            view()
                .flex_1()
                .flex_row()
                .items_center()
                .h(px(32.0))
                .px(px(10.0))
                .rounded_md()
                .border_1()
                .border_color(move || theme().primary.opacity(0.55))
                .bg(move || theme().background)
                .child(
                    text(move || {
                        let ui = state.get();
                        if ui.folder_node.is_empty() {
                            "选择左侧节点".into()
                        } else {
                            format!("{} › {}", ui.folder_node, ui.folder_path)
                        }
                    })
                    .text_size(px(12.5))
                    .number_of_lines(1)
                    .flex_1(),
                ),
        )
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
                .child(icons::stroke_icon(ip::SEARCH, 14.0).text_color(move || theme().muted_foreground))
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
        .child(tiny_text_btn("名称", move || model::set_sort(state, "name")))
        .child(seg_icon(ip::LIST, move || state.get().view != "icons", move || model::set_view(state, "list")))
        .child(seg_icon(ip::LAYOUT_GRID, move || state.get().view == "icons", move || model::set_view(state, "icons")))
        .child(seg_icon(ip::PANEL_RIGHT, move || state.get().preview_on, move || model::toggle_preview(state)))
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
                .child(field(280.0, "名称", move || {
                    let ui = state.get();
                    if ui.inline_rename { ui.rename_to.clone() } else { ui.new_name.clone() }
                }, move |t: &str| {
                    let t = t.to_owned();
                    state.update(|ui| {
                        if ui.inline_rename {
                            ui.rename_to = t;
                        } else {
                            ui.new_name = t;
                        }
                        ui.name_error.clear();
                    });
                }))
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
    view()
        .flex_1()
        .min_w(px(0.0))
        .on_file_drop(move |event: &FileDropEvent| {
            model::import_dropped(state, event.uris.clone());
        })
        .child(when(
            move || state.get().view != "icons",
            || file_header(),
        ))
        .child(scroll().flex_1().child(dynamic(move || -> AnyElement {
            let ui = state.get();
            if ui.folder_path.is_empty() {
                return empty_hint("在左侧树里选择一个节点文件夹。").into_element();
            }
            let rows = model::visible_entries(&ui);
            if rows.is_empty() {
                return empty_hint("这个文件夹是空的，或筛选无结果。").into_element();
            }
            if ui.view == "icons" {
                return view()
                    .flex_row()
                    .flex_wrap()
                    .gap(px(10.0))
                    .p(px(12.0))
                    .child(each(
                        move || model::visible_entries(&state.get()),
                        |pair| pair.1.path.clone(),
                        move |pair, _| file_icon_tile(state, pair),
                    ))
                    .into_element();
            }
            view()
                .child(each(
                    move || model::visible_entries(&state.get()),
                    |pair| pair.1.path.clone(),
                    move |pair, _| file_row(state, pair),
                ))
                .into_element()
        })))
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
        .px(px(12.0))
        .bg(move || theme().muted)
        .child(column_label("名称").flex_1())
        .child(column_label("修改时间").w(px(150.0)))
        .child(column_label("类型").w(px(90.0)))
        .child(column_label("大小").w(px(80.0)))
}

fn file_row(state: Signal<Ui>, pair: ReadSignal<(usize, api::Entry)>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .h(px(32.0))
        .px(px(12.0))
        .gap(px(8.0))
        .bg(move || {
            let ui = state.get();
            let (orig, entry) = pair.get();
            let selected =
                ui.selected == orig as i32 || ui.selected_paths.iter().any(|p| p == &entry.path);
            if selected {
                theme().selection()
            } else {
                theme().card
            }
        })
        .on(EventKind::Press, move |payload: &EventPayload| {
            let (orig, entry) = pair.get();
            if let EventPayload::Press(PressEvent { modifiers, .. }) = payload {
                if modifiers.ctrl || modifiers.cmd {
                    model::toggle_select_path(state, entry.path);
                    return;
                }
            }
            model::activate_row(state, orig as i32);
        })
        .child(dynamic(move || {
            let e = pair.get().1;
            let kind = icons::file_kind(&e.name, e.is_dir);
            icons::typed_file_icon(kind, 16.0)
        }))
        .child(
            text(move || pair.get().1.name.clone())
                .flex_1()
                .text_size(px(13.0))
                .number_of_lines(1),
        )
        .child(
            text(move || format_time(&pair.get().1.mod_time))
                .w(px(150.0))
                .text_size(px(12.0))
                .text_color(move || theme().muted_foreground),
        )
        .child(
            text(move || {
                let e = pair.get().1;
                icons::type_label(icons::file_kind(&e.name, e.is_dir)).to_owned()
            })
            .w(px(90.0))
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

fn file_icon_tile(state: Signal<Ui>, pair: ReadSignal<(usize, api::Entry)>) -> impl IntoElement {
    view()
        .w(px(96.0))
        .h(px(104.0))
        .gap(px(6.0))
        .p(px(8.0))
        .items_center()
        .rounded(theme().radius)
        .bg(move || {
            let ui = state.get();
            let (orig, entry) = pair.get();
            let selected =
                ui.selected == orig as i32 || ui.selected_paths.iter().any(|p| p == &entry.path);
            if selected {
                theme().selection()
            } else {
                theme().muted
            }
        })
        .border_1()
        .border_color(move || theme().border)
        .on(EventKind::Press, move |payload: &EventPayload| {
            let (orig, entry) = pair.get();
            if let EventPayload::Press(PressEvent { modifiers, .. }) = payload {
                if modifiers.ctrl || modifiers.cmd {
                    model::toggle_select_path(state, entry.path);
                    return;
                }
            }
            model::activate_row(state, orig as i32);
        })
        .child(dynamic(move || {
            let e = pair.get().1;
            icons::typed_file_icon(icons::file_kind(&e.name, e.is_dir), 36.0)
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
        .child(scroll().flex_1().child(dynamic(move || preview_body(state))))
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
                text(move || state.get().preview_path.clone())
                    .text_size(px(12.0))
                    .font_weight(FontWeight::SEMIBOLD)
                    .number_of_lines(2),
            )
            .child(
                text(move || state.get().preview_text.clone())
                    .text_size(px(12.0))
                    .text_color(move || theme().foreground),
            )
            .into_element();
    }
    text("选择文件以预览文本、图片或 PDF。")
        .text_size(px(13.0))
        .text_color(move || theme().muted_foreground)
        .into_element()
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
        .bg(move || theme().secondary.opacity(0.0))
        .on_press(on_press)
        .child(icons::stroke_icon(d, 16.0).text_color(move || theme().foreground))
}

fn tool_labeled(d: &'static str, label: &'static str, on_press: impl Fn() + 'static) -> impl IntoElement {
    button()
        .flex_row()
        .items_center()
        .gap(px(5.0))
        .px(px(10.0))
        .h(px(30.0))
        .rounded_md()
        .border_1()
        .border_color(move || theme().border)
        .bg(move || theme().card)
        .on_press(on_press)
        .child(icons::stroke_icon(d, 14.0).text_color(move || theme().foreground))
        .child(text(label).text_size(px(12.5)))
}

fn tiny_text_btn(label: &'static str, on_press: impl Fn() + 'static) -> impl IntoElement {
    button()
        .px(px(8.0))
        .h(px(28.0))
        .rounded_md()
        .bg(move || theme().secondary)
        .on_press(on_press)
        .child(text(label).text_size(px(12.0)).text_color(move || theme().secondary_foreground))
}

fn seg_mode(state: Signal<Ui>, mode: &'static str, label: &'static str) -> impl IntoElement {
    button()
        .px(px(8.0))
        .h(px(28.0))
        .rounded_md()
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
        .bg(move || {
            if active_bg() {
                theme().accent
            } else {
                theme().secondary.opacity(0.0)
            }
        })
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
                    view()
                        .flex_1()
                        .items_center()
                        .justify_center()
                        .child(card().w(px(440.0)).child(text("目标已有同名文件").font_weight(FontWeight::SEMIBOLD).text_size(px(16.0))).child(
                            text(move || state.get().conflict_path.clone())
                                .text_size(px(12.0))
                                .text_color(move || theme().muted_foreground),
                        ).child(apply_box(state)).child(
                            view()
                                .flex_row()
                                .gap(px(8.0))
                                .child(cmd("覆盖", true, move || model::answer_conflict(state, "overwrite")))
                                .child(cmd("跳过", false, move || model::answer_conflict(state, "skip"))),
                        )),
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
                .child(text(move || if state.get().conflict_apply { "✓" } else { "" }).text_size(px(12.0))),
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
        .on_cancel(move || state.update(|ui| ui.settings_open = false))
        .child(
            view().flex_1().items_center().justify_center().child(
                card()
                    .w(px(460.0))
                    .child(text("设置").font_weight(FontWeight::SEMIBOLD).text_size(px(16.0)))
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
                            .child(cmd("清除", false, move || model::clear_auth_key(state)))
                            .child(cmd("关闭", false, move || {
                                state.update(|ui| ui.settings_open = false);
                            })),
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
                            .child(text("确认永久删除").font_weight(FontWeight::SEMIBOLD).text_color(move || theme().destructive))
                            .child(text(move || state.get().delete_note.clone()).text_size(px(12.0)))
                            .child(
                                view()
                                    .flex_row()
                                    .gap(px(8.0))
                                    .child(cmd("取消", true, move || model::cancel_delete(state)))
                                    .child(cmd_label(
                                        move || format!("永久删除 {} 项", state.get().delete_paths.len()),
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
                            .child(text(move || {
                                state.get().daemon_guide.as_ref().map(|g| g.title.clone()).unwrap_or_else(|| "Daemon".into())
                            }).font_weight(FontWeight::SEMIBOLD))
                            .child(text(move || {
                                state.get().daemon_guide.as_ref().map(|g| g.summary.clone()).unwrap_or_default()
                            }).text_size(px(12.0)))
                            .child(
                                view()
                                    .flex_row()
                                    .gap(px(6.0))
                                    .child(cmd("Linux", false, move || state.update(|ui| ui.daemon_os = "linux".into())))
                                    .child(cmd("Windows", false, move || state.update(|ui| ui.daemon_os = "windows".into())))
                                    .child(cmd("macOS", false, move || state.update(|ui| ui.daemon_os = "macos".into()))),
                            )
                            .child(text(move || {
                                let ui = state.get();
                                ui.daemon_guide.as_ref().map(|g| crate::daemon::commands_clipboard(g, &ui.daemon_os)).unwrap_or_default()
                            }).text_size(px(11.0)))
                            .child(text(move || {
                                state.get().daemon_guide.as_ref().map(|g| g.note.clone()).unwrap_or_default()
                            }).text_size(px(11.0)).text_color(move || theme().muted_foreground))
                            .child(
                                view()
                                    .flex_row()
                                    .gap(px(8.0))
                                    .child(cmd("复制命令", true, move || model::copy_daemon_command(state)))
                                    .child(cmd("关闭", false, move || model::close_daemon_help(state))),
                            ),
                    ),
                )
        },
    )
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
        .bg(move || if primary { theme().primary } else { theme().secondary })
        .on_press(on_press)
        .child(
            text(label).text_size(px(13.0)).text_color(move || {
                if primary {
                    theme().primary_foreground
                } else {
                    theme().secondary_foreground
                }
            }),
        )
}

fn cmd_label(label: impl Fn() -> String + 'static, on_press: impl Fn() + 'static) -> impl IntoElement {
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
