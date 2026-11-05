//! Single-pane Finder layout: node tree on the left, files and preview on
//! the right. Labels are Chinese, including the same conflict wording as
//! the other shells (覆盖 / 跳过 / 对后续冲突都这样处理).

use nsh_base::{button, checkbox, dialog, input, theme};
use crate::icons;
use crate::persist::ThemePref;
use nsh_core::prelude::*;

use crate::api::{self, job_line, mode_label, size_label};
use crate::model::{self, TreeRow, Ui};
use crate::token::format_time;

/// The window root (`nsh.json` `"root": "ui::App"`).
#[component]
pub fn App() -> impl IntoElement {
    let state = signal(Ui::new());
    model::boot(state);
    view()
        .flex_1()
        .bg(move || theme().background)
        .text_color(move || theme().foreground)
        .child(title_row(state))
        .child(tab_bar(state))
        .child(toolbar(state))
        .child(body(state))
        .child(status_bar(state))
        .child(transfer_bar(state))
        .child(conflict_dialog(state))
        .child(settings_dialog(state))
        .child(delete_dialog(state))
        .child(daemon_dialog(state))
}


fn title_row(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(8.0))
        .p(px(10.0))
        .border_b_1()
        .border_color(move || theme().border)
        .bg(move || theme().sidebar)
        .child(text(icons::LOGO_LABEL).font_weight(FontWeight::BOLD).text_size(px(18.0)).text_color(move || theme().primary))
        .child(text(icons::LOGO_TITLE).font_weight(FontWeight::SEMIBOLD).text_size(px(16.0)))
        .child(text("Native Shell").text_size(px(12.0)).text_color(move || theme().muted_foreground))
        .child(field(260.0, "http://127.0.0.1:8645", move || state.get().addr.clone(), move |text: &str| {
            let text = text.to_owned();
            state.update(|ui| ui.addr = text);
        }))
        .child(cmd("刷新", false, move || model::refresh(state)))
        .child(cmd("设置", false, move || model::open_settings(state)))
        .child(cmd_label(
            move || format!("主题·{}", state.get().theme.label()),
            move || {
                let next = match state.get().theme {
                    ThemePref::System => ThemePref::Light,
                    ThemePref::Light => ThemePref::Dark,
                    ThemePref::Dark => ThemePref::System,
                };
                model::set_theme(state, next);
            },
        ))
        .child(
            text(move || state.get().status.clone())
                .flex_1()
                .text_size(px(12.0))
                .text_color(move || theme().muted_foreground)
                .number_of_lines(1),
        )
}

fn tab_bar(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(4.0))
        .px(px(8.0))
        .py(px(6.0))
        .border_b_1()
        .border_color(move || theme().border)
        .bg(move || theme().secondary)
        .child(scroll().flex_1().child(view().flex_row().gap(px(4.0)).child(dynamic(move || {
            let ui = state.get();
            ui.tabs
                .iter()
                .enumerate()
                .map(|(index, tab_state)| {
                    let title = tab_state.title(&ui.nodes);
                    let _selected = ui.active_tab == index;
                    cmd_label(
                        move || title.clone(),
                        move || model::switch_tab(state, index),
                    )
                })
                .collect::<Vec<_>>()
        }))))
        .child(cmd("+", false, move || model::new_tab_from_current(state)))
        .child(cmd("×", false, move || model::close_active_tab(state)))
        .child(cmd("重开", false, move || model::reopen_closed_tab(state)))
}

fn status_bar(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(12.0))
        .px(px(10.0))
        .py(px(4.0))
        .border_t_1()
        .border_color(move || theme().border)
        .bg(move || theme().card)
        .child(
            text(move || {
                let ui = state.get();
                format!("{} 项 · {}", model::visible_entries(&ui).len(), ui.folder_path)
            })
            .text_size(px(11.0))
            .text_color(move || theme().muted_foreground)
            .number_of_lines(1),
        )
        .child(when(
            move || state.get().clip_set,
            move || {
                text(move || {
                    let ui = state.get();
                    format!("剪贴板：{} 项 · 来自 {}", ui.clip_items.len().max(usize::from(ui.clip_set)), ui.clip_node)
                })
                .text_size(px(11.0))
            },
        ))
}

fn toolbar(state: Signal<Ui>) -> impl IntoElement {
    view()
        .gap(px(8.0))
        .p(px(10.0))
        .border_b_1()
        .border_color(move || theme().border)
        .bg(move || theme().card)
                .child(toolbar_actions(state))
        .child(toolbar_import(state))
        .child(when(
            move || !state.get().err.is_empty(),
            move || {
                text(move || state.get().err.clone())
                    .text_size(px(12.0))
                    .text_color(move || theme().destructive)
            },
        ))
        .child(when(
            move || !state.get().drag_note.is_empty(),
            move || {
                text(move || state.get().drag_note.clone())
                    .text_size(px(12.0))
                    .text_color(move || theme().muted_foreground)
            },
        ))
}


fn toolbar_actions(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(6.0))
        .child(cmd("←", false, move || model::nav_back(state)))
        .child(cmd("→", false, move || model::nav_forward(state)))
        .child(cmd("↑", false, move || model::go_up(state)))
        .child(mode_button(state, "push", "推送"))
        .child(mode_button(state, "pull", "拉取"))
        .child(
            text("A→B")
                .text_size(px(12.0))
                .text_color(move || theme().muted_foreground),
        )
        .child(cmd("复制", false, move || model::copy_selected(state)))
        .child(cmd("粘贴", false, move || model::paste(state)))
        .child(cmd("清空剪贴板", false, move || model::clear_clipboard(state)))
        .child(cmd("新建文件夹", false, move || model::begin_new_folder(state)))
        .child(cmd("重命名", false, move || model::begin_rename(state)))
        .child(cmd("删除", false, move || model::ask_delete(state)))
        .child(cmd_label(
            move || if state.get().view == "icons" { "图标".into() } else { "列表".into() },
            move || {
                let next = if state.get().view == "icons" { "list" } else { "icons" };
                model::set_view(state, next);
            },
        ))
        .child(field(120.0, "筛选", move || state.get().filter.clone(), move |text: &str| {
            model::set_filter(state, text.to_owned());
        }))
        .child(cmd("名", false, move || model::set_sort(state, "name")))
        .child(cmd("大小", false, move || model::set_sort(state, "size")))
        .child(cmd("时间", false, move || model::set_sort(state, "mtime")))
        .child(cmd("缓存", false, move || model::cache_selected(state)))
        .child(cmd("显示", false, move || model::reveal_cached(state)))
        .child(cmd_label(
            move || {
                if state.get().preview_on {
                    "预览开".to_owned()
                } else {
                    "预览关".to_owned()
                }
            },
            move || model::toggle_preview(state),
        ))
}

fn toolbar_import(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(6.0))
        .child(field(280.0, "要导入的本地路径", move || state.get().import_path.clone(), move |text: &str| {
            let text = text.to_owned();
            state.update(|ui| ui.import_path = text);
        }))
        .child(cmd("导入", false, move || model::import_typed(state)))
        .child(
            text("或把文件拖到右侧列表")
                .text_size(px(12.0))
                .text_color(move || theme().muted_foreground),
        )
}

fn body(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_1()
        .flex_row()
        .child(tree_pane(state))
        .child(right_pane(state))
}

fn tree_pane(state: Signal<Ui>) -> impl IntoElement {
    view()
        .w(px(280.0))
        .flex_none()
        .border_r_1()
        .border_color(move || theme().border)
        .bg(move || theme().card)
        .child(
            text("节点")
                .p(px(8.0))
                .text_size(px(11.0))
                .text_color(move || theme().muted_foreground),
        )
        .child(
            scroll().flex_1().child(when(
                move || model::tree_rows(&state.get()).is_empty(),
                move || {
                    text("还没有节点")
                        .p(px(8.0))
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
}

fn tree_item(state: Signal<Ui>, row: ReadSignal<TreeRow>) -> impl IntoElement {
    let depth = move || px(8.0 + row.get().depth as f32 * 14.0);
    view()
        .flex_row()
        .items_center()
        .h(px(28.0))
        .pl(depth)
        .pr(px(8.0))
        .bg(move || {
            let row = row.get();
            let ui = state.get();
            if !row.is_node && row.node == ui.folder_node && row.path == ui.folder_path {
                theme().accent
            } else {
                theme().card
            }
        })
        .on_press(move || model::click_tree(state, row.get()))
        .child(
            text(move || {
                let row = row.get();
                let open = state.get().open.iter().any(|key| key == &row.key);
                let mark = if open { "▾ " } else { "▸ " };
                format!("{mark}{}", row.label)
            })
            .text_size(px(13.0))
            .number_of_lines(1),
        )
}

fn inline_editors(state: Signal<Ui>) -> impl IntoElement {
    view()
        .child(when(
            move || state.get().inline_new,
            move || {
                view()
                    .flex_row()
                    .items_center()
                    .gap(px(6.0))
                    .p(px(8.0))
                    .child(field(200.0, "新文件夹名", move || state.get().new_name.clone(), move |text: &str| {
                        let text = text.to_owned();
                        state.update(|ui| ui.new_name = text);
                    }))
                    .child(cmd("创建", true, move || model::mkdir(state)))
                    .child(cmd("取消", false, move || state.update(|ui| { ui.inline_new = false; ui.name_error.clear(); })))
                    .child(when(move || !state.get().name_error.is_empty(), move || text(move || state.get().name_error.clone()).text_size(px(12.0)).text_color(move || theme().destructive)))
            },
        ))
        .child(when(
            move || state.get().inline_rename,
            move || {
                view()
                    .flex_row()
                    .items_center()
                    .gap(px(6.0))
                    .p(px(8.0))
                    .child(field(200.0, "新名称", move || state.get().rename_to.clone(), move |text: &str| {
                        let text = text.to_owned();
                        state.update(|ui| ui.rename_to = text);
                    }))
                    .child(cmd("确定", true, move || model::rename(state)))
                    .child(cmd("取消", false, move || state.update(|ui| { ui.inline_rename = false; ui.name_error.clear(); })))
            },
        ))
}

fn right_pane(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_1()
        .child(crumb(state))
        .child(inline_editors(state))
        .child(file_list(state))
        .child(preview(state))
}

fn crumb(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .gap(px(8.0))
        .px(px(12.0))
        .h(px(36.0))
        .border_b_1()
        .border_color(move || theme().border)
        .child(cmd("上级", false, move || model::go_up(state)))
        .child(
            text(move || {
                let path = state.get().folder_path.clone();
                if path.is_empty() {
                    "请在左侧选择文件夹".to_owned()
                } else {
                    path
                }
            })
            .flex_1()
            .text_size(px(12.0))
            .text_color(move || theme().muted_foreground)
            .number_of_lines(1),
        )
}

fn file_list(state: Signal<Ui>) -> impl IntoElement {
    view()
        .flex_1()
        .on_file_drop(move |event: &FileDropEvent| {
            model::import_dropped(state, event.uris.clone());
        })
        .child(file_header())
        .child(scroll().flex_1().child(dynamic(move || -> AnyElement {
            let ui = state.get();
            if ui.folder_path.is_empty() {
                return empty_hint("在左侧树里选择一个节点文件夹。").into_element();
            }
            if ui.entries.is_empty() {
                return empty_hint("这个文件夹是空的。可以把桌面上的文件拖进来导入。").into_element();
            }
            view()
                .child(each(
                    move || state.get().entries.clone(),
                    |entry| entry.path.clone(),
                    move |entry, index| file_row(state, entry, index),
                ))
                .into_element()
        })))
}

fn empty_hint(message: &'static str) -> impl IntoElement {
    text(message)
        .p(px(16.0))
        .text_size(px(13.0))
        .text_color(move || theme().muted_foreground)
}

fn file_header() -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .h(px(28.0))
        .px(px(12.0))
        .bg(move || theme().muted)
        .child(column_label("名称").flex_1())
        .child(column_label("大小").w(px(90.0)))
        .child(column_label("修改时间").w(px(150.0)))
}

fn column_label(label: &'static str) -> nsh_core::element::Text {
    text(label)
        .text_size(px(11.0))
        .text_color(move || theme().muted_foreground)
}

fn file_row(state: Signal<Ui>, entry: ReadSignal<api::Entry>, index: ReadSignal<usize>) -> impl IntoElement {
    view()
        .flex_row()
        .items_center()
        .h(px(30.0))
        .px(px(12.0))
        .bg(move || {
            if state.get().selected == index.get() as i32 {
                theme().accent
            } else {
                theme().background
            }
        })
        .on_press(move || model::activate_row(state, index.get() as i32))
        .child(
            text(move || {
                let entry = entry.get();
                if entry.is_dir {
                    format!("{}/", entry.name)
                } else {
                    entry.name.clone()
                }
            })
            .flex_1()
            .text_size(px(13.0))
            .number_of_lines(1),
        )
        .child(
            text(move || size_label(entry.get().size, entry.get().is_dir))
                .w(px(90.0))
                .text_size(px(12.0))
                .text_color(move || theme().muted_foreground),
        )
        .child(
            text(move || format_time(&entry.get().mod_time))
                .w(px(150.0))
                .text_size(px(12.0))
                .text_color(move || theme().muted_foreground),
        )
}

fn preview(state: Signal<Ui>) -> impl IntoElement {
    view()
        .h(px(220.0))
        .border_t_1()
        .border_color(move || theme().border)
        .bg(move || theme().card)
        .p(px(12.0))
        .gap(px(6.0))
        .child(
            text(move || {
                let kind = state.get().preview_kind.clone();
                if kind.is_empty() {
                    "预览".to_owned()
                } else {
                    format!("预览 · {kind}")
                }
            })
            .text_size(px(11.0))
            .text_color(move || theme().muted_foreground),
        )
        .child(scroll().flex_1().child(dynamic(move || preview_body(state))))
}

fn preview_body(state: Signal<Ui>) -> impl IntoElement {
    let ui = state.get();
    if !ui.preview_on {
        return text("预览已关闭。")
            .text_size(px(13.0))
            .text_color(move || theme().muted_foreground)
            .into_element();
    }
    if ui.preview_kind == "image" && !ui.preview_image.is_empty() {
        return image(move || state.get().preview_image.clone())
            .resize_mode(ResizeMode::Contain)
            .h(px(160.0))
            .w(px(320.0))
            .into_element();
    }
    if ui.preview_kind == "image" {
        return text("无法解码这张图片。")
            .text_size(px(13.0))
            .text_color(move || theme().muted_foreground)
            .into_element();
    }
    if ui.preview_text.is_empty() {
        let message = if ui.preview_kind == "pdf" {
            "正在读取 PDF 文本…"
        } else if ui.preview_kind == "other" {
            "这个文件没有文本预览。"
        } else {
            "选择文件以预览文本、图片或 PDF。"
        };
        return text(message)
            .text_size(px(13.0))
            .text_color(move || theme().muted_foreground)
            .into_element();
    }
    let heading = if ui.preview_kind == "pdf" {
        "PDF 文本（agent 提取，不是整页渲染）"
    } else {
        ""
    };
    view()
        .gap(px(4.0))
        .child(when(
            move || state.get().preview_kind == "pdf",
            move || {
                text(heading)
                    .text_size(px(12.0))
                    .text_color(move || theme().muted_foreground)
            },
        ))
        .child(each(
            move || preview_lines(&state.get().preview_text),
            |line| line.0,
            |line, _| text(move || line.get().1.clone()).text_size(px(12.0)),
        ))
        .into_element()
}

fn preview_lines(text: &str) -> Vec<(usize, String)> {
    if text.is_empty() {
        Vec::new()
    } else {
        text.split('\n')
            .enumerate()
            .map(|(index, line)| (index, line.to_owned()))
            .collect()
    }
}

fn transfer_bar(state: Signal<Ui>) -> impl IntoElement {
    view()
        .max_h(px(140.0))
        .border_t_1()
        .border_color(move || theme().border)
        .bg(move || theme().card)
        .p(px(8.0))
        .gap(px(4.0))
        .child(
            text("传输")
                .text_size(px(11.0))
                .text_color(move || theme().muted_foreground),
        )
        .child(scroll().max_h(px(100.0)).child(dynamic(move || -> AnyElement {
            let ui = state.get();
            if ui.transfers.is_empty() && ui.imports.is_empty() {
                return text("在一个节点上复制，打开另一个节点的文件夹，再粘贴。文件字节在两个 agent 之间直接传输。")
                    .text_size(px(12.0))
                    .text_color(move || theme().muted_foreground)
                    .into_element();
            }
            view()
                .gap(px(4.0))
                .child(each(
                    move || state.get().transfers.clone(),
                    |item| item.id.clone(),
                    move |item, _| transfer_row(state, item),
                ))
                .child(each(
                    move || state.get().imports.clone(),
                    |item| item.id.clone(),
                    move |item, _| {
                        text(move || format!("导入  {}", job_line(&item.get())))
                            .text_size(px(12.0))
                            .number_of_lines(1)
                    },
                ))
                .into_element()
        })))
}

fn transfer_row(state: Signal<Ui>, item: ReadSignal<api::Transfer>) -> impl IntoElement {
    view()
        .gap(px(2.0))
        .child(
            view()
                .flex_row()
                .items_center()
                .gap(px(8.0))
                .child(
                    text(move || {
                        let item = item.get();
                        format!(
                            "{}  {} → {}  {}",
                            mode_label(&item.mode),
                            item.from_node,
                            item.to_node,
                            job_line(&item.job)
                        )
                    })
                    .flex_1()
                    .text_size(px(12.0))
                    .number_of_lines(1),
                )
                .child(when(
                    move || item.get().job.status == "running",
                    move || {
                        let id = item.get().id.clone();
                        cmd("取消", false, move || model::cancel_transfer(state, id.clone()))
                    },
                ))
                .child(when(
                    move || matches!(item.get().job.status.as_str(), "error" | "canceled" | "interrupted"),
                    move || {
                        let id = item.get().id.clone();
                        cmd("继续", false, move || model::resume_transfer(state, id.clone()))
                    },
                )),
        )
        .child(when(
            move || item.get().job.bytes_total > 0,
            move || {
                let job = item.get().job.clone();
                let frac = if job.bytes_total <= 0 {
                    0.0
                } else {
                    (job.bytes_done as f32 / job.bytes_total as f32).clamp(0.0, 1.0)
                };
                view()
                    .h(px(4.0))
                    .w_full()
                    .rounded_full()
                    .bg(move || theme().muted)
                    .child(
                        view()
                            .h(px(4.0))
                            .w(percentage(frac))
                            .rounded_full()
                            .bg(move || theme().primary),
                    )
            },
        ))
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

fn mode_button(state: Signal<Ui>, mode: &'static str, label: &'static str) -> impl IntoElement {
    button()
        .px(px(10.0))
        .py(px(6.0))
        .rounded_md()
        .bg(move || {
            if state.get().mode == mode {
                theme().primary
            } else {
                theme().secondary
            }
        })
        .on_press(move || state.update(|ui| ui.mode = mode.to_owned()))
        .child(
            text(label).text_size(px(13.0)).text_color(move || {
                if state.get().mode == mode {
                    theme().primary_foreground
                } else {
                    theme().secondary_foreground
                }
            }),
        )
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
