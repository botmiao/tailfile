//! Window state and the agent calls behind each control.
//! Network work runs on blocking threads; signals are written back on the
//! engine task that `spawn` polls.

use std::sync::OnceLock;
use std::time::Duration;

use nsh_core::prelude::*;

use crate::api::{Client, Entry, Job, NodeInfo, Transfer};
use crate::launch;
use crate::token::{
    self, base_remote, file_uri_to_path, join_remote, parent_remote, path_is_under, preview_kind,
};

#[derive(Clone, PartialEq)]
pub struct Ui {
    pub addr: String,
    pub token: String,
    pub token_file: String,
    pub state_dir: String,
    pub status: String,
    pub err: String,
    pub mode: String,
    pub nodes: Vec<NodeInfo>,
    pub open: Vec<String>,
    pub subdirs: Vec<(String, Vec<String>)>,
    pub folder_node: String,
    pub folder_path: String,
    pub entries: Vec<Entry>,
    pub selected: i32,
    pub preview_on: bool,
    pub preview_path: String,
    pub preview_kind: String,
    pub preview_text: String,
    pub preview_image: String,
    pub new_name: String,
    pub rename_to: String,
    pub import_path: String,
    pub transfers: Vec<Transfer>,
    pub imports: Vec<Job>,
    pub conflict_path: String,
    pub conflict_kind: String,
    pub conflict_id: String,
    pub conflict_apply: bool,
    pub conflict_hold: String,
    pub settings_open: bool,
    pub net_mode: String,
    pub net_requested: String,
    pub auth_configured: bool,
    pub auth_input: String,
    pub settings_note: String,
    pub drag_note: String,
    pub drag_path: String,
    pub list_gen: u64,
    pub preview_gen: u64,
    pub alive: bool,
    pub poll_busy: bool,
    pub clip_set: bool,
    pub clip_node: String,
    pub clip_path: String,
    pub clip_name: String,
}

impl Ui {
    pub fn new() -> Self {
        let launch = launch::current();
        let (token, status) = match token::load(&launch.token_file, &launch.state) {
            Ok(token) => (token, "正在连接 agent…".to_owned()),
            Err(err) => (String::new(), err),
        };
        Self {
            addr: launch.agent,
            token,
            token_file: launch.token_file,
            state_dir: launch.state,
            status,
            err: String::new(),
            mode: "push".to_owned(),
            nodes: Vec::new(),
            open: Vec::new(),
            subdirs: Vec::new(),
            folder_node: String::new(),
            folder_path: String::new(),
            entries: Vec::new(),
            selected: -1,
            preview_on: token::load_preview_on(),
            preview_path: String::new(),
            preview_kind: String::new(),
            preview_text: String::new(),
            preview_image: String::new(),
            new_name: String::new(),
            rename_to: String::new(),
            import_path: String::new(),
            transfers: Vec::new(),
            imports: Vec::new(),
            conflict_path: String::new(),
            conflict_kind: String::new(),
            conflict_id: String::new(),
            conflict_apply: false,
            conflict_hold: String::new(),
            settings_open: false,
            net_mode: String::new(),
            net_requested: String::new(),
            auth_configured: false,
            auth_input: String::new(),
            settings_note: String::new(),
            drag_note: String::new(),
            drag_path: String::new(),
            list_gen: 0,
            preview_gen: 0,
            alive: false,
            poll_busy: false,
            clip_set: false,
            clip_node: String::new(),
            clip_path: String::new(),
            clip_name: String::new(),
        }
    }
}

#[derive(Clone, PartialEq, Eq)]
pub struct TreeRow {
    pub key: String,
    pub depth: u32,
    pub label: String,
    pub node: String,
    pub path: String,
    pub is_node: bool,
}

pub fn node_key(id: &str) -> String {
    format!("n\u{1f}{id}")
}

pub fn dir_key(node: &str, path: &str) -> String {
    format!("d\u{1f}{node}\u{1f}{path}")
}

pub fn tree_rows(ui: &Ui) -> Vec<TreeRow> {
    let mut rows = Vec::new();
    for node in &ui.nodes {
        let key = node_key(&node.id);
        rows.push(TreeRow {
            key: key.clone(),
            depth: 0,
            label: node_label(node),
            node: node.id.clone(),
            path: String::new(),
            is_node: true,
        });
        if ui.open.iter().any(|open| open == &key) {
            for root in &node.read_roots {
                push_dir(&mut rows, ui, &node.id, root, 1);
            }
        }
    }
    rows
}

fn push_dir(rows: &mut Vec<TreeRow>, ui: &Ui, node: &str, path: &str, depth: u32) {
    let key = dir_key(node, path);
    let mut label = base_remote(path);
    if label.is_empty() {
        label = path.to_owned();
    }
    rows.push(TreeRow {
        key: key.clone(),
        depth,
        label,
        node: node.to_owned(),
        path: path.to_owned(),
        is_node: false,
    });
    if ui.open.iter().any(|open| open == &key) {
        if let Some((_, kids)) = ui.subdirs.iter().find(|(k, _)| k == &key) {
            for child in kids {
                push_dir(rows, ui, node, child, depth + 1);
            }
        }
    }
}

fn node_label(node: &NodeInfo) -> String {
    let mut label = if node.name.is_empty() {
        node.id.clone()
    } else {
        node.name.clone()
    };
    if node.read_only {
        label.push_str("  ·  只读");
    }
    if node.self_node {
        label.push_str("  ·  本机");
    }
    if !node.online {
        label.push_str("  ·  离线");
    }
    label
}

pub fn selected(ui: &Ui) -> Option<Entry> {
    ui.entries.get(ui.selected.max(0) as usize).cloned().filter(|_| ui.selected >= 0)
}

static APP_OWNER: OnceLock<Owner> = OnceLock::new();

pub fn boot(state: Signal<Ui>) {
    // Press handlers and timers are short-lived owners. Tasks spawned
    // directly from them are cancelled when the handler returns, so every
    // agent call is spawned under the component owner captured here.
    let _ = APP_OWNER.set(Owner::current());
    refresh(state);
    set_interval(Duration::from_secs(1), move || poll(state));
    on_key("mod+c", move |_| copy_selected(state));
    on_key("mod+v", move |_| paste(state));
    on_key("mod+r", move |_| refresh(state));
}

pub fn refresh(state: Signal<Ui>) {
    reload_token(state);
    let ui = state.get();
    let base = ui.addr.clone();
    let token = ui.token.clone();
    state.update(|ui| {
        ui.status = "正在连接 agent…".into();
        ui.subdirs.clear();
    });
    offload(
        state,
        move || {
            let client = Client::new(base, token);
            client.health()?;
            client.nodes()
        },
        |state, result| match result {
            Ok(nodes) => {
                let mut open_folder = false;
                state.update(|ui| {
                    ui.alive = true;
                    ui.err.clear();
                    ui.nodes = nodes;
                    ui.status = format!("{} 个节点", ui.nodes.len());
                    if ui.folder_node.is_empty() {
                        if let Some(node) = ui.nodes.first() {
                            if let Some(root) = node.read_roots.first().cloned() {
                                let id = node.id.clone();
                                ui.folder_node = id.clone();
                                ui.folder_path = root;
                                let key = node_key(&id);
                                if !ui.open.iter().any(|item| item == &key) {
                                    ui.open.push(key);
                                }
                                open_folder = true;
                            }
                        }
                    }
                });
                if open_folder || !state.get().folder_path.is_empty() {
                    load_list(state);
                }
                poll(state);
            }
            Err(err) => state.update(|ui| {
                ui.alive = false;
                ui.err = err;
                ui.status = "agent 没有在运行。请先启动 tailfile-agent（或 tailfile-agent -demo），然后点刷新。".into();
            }),
        },
    );
}

fn reload_token(state: Signal<Ui>) {
    let ui = state.get();
    match token::load(&ui.token_file, &ui.state_dir) {
        Ok(token) => state.update(|ui| {
            ui.token = token;
            if ui.status.starts_with("无法读取") {
                ui.status.clear();
            }
        }),
        Err(err) => state.update(|ui| {
            ui.token.clear();
            ui.status = err;
        }),
    }
}

pub fn load_list(state: Signal<Ui>) {
    let ui = state.get();
    if ui.folder_node.is_empty() || ui.folder_path.is_empty() {
        return;
    }
    let generation = ui.list_gen.wrapping_add(1);
    let node = ui.folder_node.clone();
    let path = ui.folder_path.clone();
    let base = ui.addr.clone();
    let token = ui.token.clone();
    let node_for_call = node.clone();
    let path_for_call = path.clone();
    state.update(|ui| ui.list_gen = generation);
    offload(
        state,
        move || {
            let client = Client::new(base, token);
            let entries = client.list(&node_for_call, &path_for_call)?;
            let _ = client.set_drop_target(&node_for_call, &path_for_call);
            Ok(entries)
        },
        move |state, result| {
            let ui = state.get();
            if ui.list_gen != generation || ui.folder_node != node || ui.folder_path != path {
                return;
            }
            match result {
                Ok(entries) => {
                    state.update(|ui| {
                        ui.err.clear();
                        if ui.selected >= entries.len() as i32 {
                            ui.selected = -1;
                        }
                        ui.entries = entries;
                    });
                    preview_selected(state);
                }
                Err(err) => state.update(|ui| {
                    ui.err = err;
                    ui.entries.clear();
                    ui.selected = -1;
                }),
            }
        },
    );
}

pub fn ensure_dirs(state: Signal<Ui>, node: String, path: String) {
    let key = dir_key(&node, &path);
    if state.get().subdirs.iter().any(|(item, _)| item == &key) {
        return;
    }
    let base = state.get().addr.clone();
    let token = state.get().token.clone();
    offload(
        state,
        move || {
            let entries = Client::new(base, token).list(&node, &path)?;
            let kids = entries
                .into_iter()
                .filter(|entry| entry.is_dir)
                .map(|entry| entry.path)
                .collect::<Vec<_>>();
            Ok(kids)
        },
        move |state, result| match result {
            Ok(kids) => state.update(|ui| {
                if let Some(slot) = ui_subdir_mut(ui, &key) {
                    *slot = kids;
                } else {
                    ui.subdirs.push((key, kids));
                }
            }),
            Err(err) => state.update(|ui| ui.err = err),
        },
    );
}

fn ui_subdir_mut<'a>(ui: &'a mut Ui, key: &str) -> Option<&'a mut Vec<String>> {
    ui.subdirs
        .iter_mut()
        .find(|(item, _)| item == key)
        .map(|(_, kids)| kids)
}

pub fn click_tree(state: Signal<Ui>, row: TreeRow) {
    let key = row.key.clone();
    let opening = !state.get().open.iter().any(|item| item == &key);
    state.update(|ui| {
        if opening {
            ui.open.push(key);
        } else {
            ui.open.retain(|item| item != &row.key);
        }
    });
    if row.is_node {
        if opening {
            if let Some(node) = state.get().nodes.iter().find(|node| node.id == row.node).cloned() {
                if let Some(root) = node.read_roots.first().cloned() {
                    select_folder(state, node.id, root);
                }
            }
        }
        return;
    }
    if opening {
        ensure_dirs(state, row.node.clone(), row.path.clone());
    }
    select_folder(state, row.node, row.path);
}

pub fn select_folder(state: Signal<Ui>, node: String, path: String) {
    state.update(|ui| {
        ui.folder_node = node;
        ui.folder_path = path;
        ui.selected = -1;
        ui.preview_path.clear();
        ui.preview_text.clear();
        ui.preview_image.clear();
        ui.preview_kind.clear();
    });
    load_list(state);
}

pub fn activate_row(state: Signal<Ui>, index: i32) {
    let ui = state.get();
    let Some(entry) = ui.entries.get(index as usize).cloned() else {
        return;
    };
    if entry.is_dir && ui.selected == index {
        let node = ui.folder_node.clone();
        let path = entry.path.clone();
        let key = dir_key(&node, &path);
        state.update(|ui| {
            if !ui.open.iter().any(|item| item == &key) {
                ui.open.push(key.clone());
            }
            let parent = dir_key(&node, &ui.folder_path);
            if !ui.open.iter().any(|item| item == &parent) {
                ui.open.push(parent);
            }
        });
        ensure_dirs(state, node.clone(), path.clone());
        select_folder(state, node, path);
        return;
    }
    state.update(|ui| ui.selected = index);
    preview_selected(state);
}

pub fn go_up(state: Signal<Ui>) {
    let ui = state.get();
    let Some(parent) = parent_remote(&ui.folder_path) else {
        return;
    };
    let inside = ui.nodes.iter().any(|node| {
        node.id == ui.folder_node && node.read_roots.iter().any(|root| path_is_under(&parent, root))
    });
    if !inside {
        state.update(|ui| ui.status = "已在共享根目录".into());
        return;
    }
    let node = ui.folder_node.clone();
    select_folder(state, node, parent);
}

pub fn copy_selected(state: Signal<Ui>) {
    let ui = state.get();
    let Some(entry) = selected(&ui) else {
        state.update(|ui| ui.status = "请先选择要复制的文件或文件夹".into());
        return;
    };
    if ui.folder_node.is_empty() {
        return;
    }
    let name = entry.name.clone();
    state.update(|ui| {
        ui.clip_set = true;
        ui.clip_node = ui.folder_node.clone();
        ui.clip_path = entry.path.clone();
        ui.clip_name = entry.name.clone();
        ui.status = format!("已复制 {name}");
    });
}

pub fn paste(state: Signal<Ui>) {
    let ui = state.get();
    if !ui.clip_set {
        state.update(|ui| ui.status = "剪贴板是空的".into());
        return;
    }
    if ui.folder_node.is_empty() || ui.folder_path.is_empty() {
        state.update(|ui| ui.status = "请先打开目标文件夹".into());
        return;
    }
    let mode = ui.mode.clone();
    let from_node = ui.clip_node.clone();
    let from_path = ui.clip_path.clone();
    let to_node = ui.folder_node.clone();
    let to_path = join_remote(&ui.folder_path, &ui.clip_name);
    let base = ui.addr.clone();
    let token = ui.token.clone();
    offload(
        state,
        move || {
            Client::new(base, token).start_transfer(&mode, &from_node, &from_path, &to_node, &to_path)?;
            Ok(mode)
        },
        |state, result| match result {
            Ok(mode) => {
                state.update(|ui| {
                    ui.err.clear();
                    ui.status = format!("已开始{} {} → {}", crate::api::mode_label(&mode), ui.clip_node, ui.folder_node);
                });
                poll(state);
            }
            Err(err) => state.update(|ui| {
                ui.err = err;
                ui.status = "粘贴失败".into();
            }),
        },
    );
}

pub fn mkdir(state: Signal<Ui>) {
    let ui = state.get();
    let name = ui.new_name.trim().to_owned();
    if name.is_empty() || ui.folder_node.is_empty() {
        state.update(|ui| ui.status = "请先输入文件夹名称".into());
        return;
    }
    let path = join_remote(&ui.folder_path, &name);
    let node = ui.folder_node.clone();
    let parent = ui.folder_path.clone();
    let base = ui.addr.clone();
    let token = ui.token.clone();
    let call_node = node.clone();
    let call_path = path.clone();
    offload(
        state,
        move || Client::new(base, token).mkdir(&call_node, &call_path),
        move |state, result| match result {
            Ok(()) => {
                state.update(|ui| {
                    ui.new_name.clear();
                    ui.err.clear();
                    ui.status = format!("已创建 {name}");
                    ui.subdirs.retain(|(key, _)| key != &dir_key(&node, &parent));
                });
                load_list(state);
                ensure_dirs(state, node, parent);
            }
            Err(err) => state.update(|ui| ui.err = err),
        },
    );
}

pub fn rename(state: Signal<Ui>) {
    let ui = state.get();
    let Some(entry) = selected(&ui) else {
        state.update(|ui| ui.status = "请选择一项并输入新名称".into());
        return;
    };
    let name = ui.rename_to.trim().to_owned();
    if name.is_empty() {
        state.update(|ui| ui.status = "请选择一项并输入新名称".into());
        return;
    }
    let dest = join_remote(&ui.folder_path, &name);
    let node = ui.folder_node.clone();
    let from = entry.path.clone();
    let base = ui.addr.clone();
    let token = ui.token.clone();
    offload(
        state,
        move || Client::new(base, token).rename(&node, &from, &dest),
        |state, result| match result {
            Ok(()) => {
                state.update(|ui| {
                    ui.err.clear();
                    ui.status = "已重命名".into();
                    ui.preview_path.clear();
                });
                load_list(state);
            }
            Err(err) => state.update(|ui| ui.err = err),
        },
    );
}

pub fn import_typed(state: Signal<Ui>) {
    let path = state.get().import_path.trim().to_owned();
    if path.is_empty() {
        state.update(|ui| ui.status = "请输入要导入的本地路径".into());
        return;
    }
    import_paths(state, vec![path]);
    state.update(|ui| ui.import_path.clear());
}

pub fn import_dropped(state: Signal<Ui>, uris: Vec<String>) {
    let paths: Vec<String> = uris.into_iter().map(|uri| file_uri_to_path(&uri)).filter(|path| !path.is_empty()).collect();
    import_paths(state, paths);
}

fn import_paths(state: Signal<Ui>, paths: Vec<String>) {
    let ui = state.get();
    if ui.folder_node.is_empty() || paths.is_empty() {
        return;
    }
    let node = ui.folder_node.clone();
    let dest = ui.folder_path.clone();
    let count = paths.len();
    let base = ui.addr.clone();
    let token = ui.token.clone();
    offload(
        state,
        move || {
            Client::new(base, token).import(&node, &dest, &paths)?;
            Ok(())
        },
        move |state, result| match result {
            Ok(()) => {
                state.update(|ui| {
                    ui.err.clear();
                    ui.status = format!("正在导入 {count} 项");
                });
                poll(state);
            }
            Err(err) => state.update(|ui| ui.err = err),
        },
    );
}

pub fn cache_selected(state: Signal<Ui>) {
    let ui = state.get();
    let Some(entry) = selected(&ui) else {
        state.update(|ui| ui.status = "请选择要缓存的文件".into());
        return;
    };
    if entry.is_dir {
        state.update(|ui| ui.status = "请选择要缓存的文件".into());
        return;
    }
    let node = ui.folder_node.clone();
    let path = entry.path.clone();
    let name = entry.name.clone();
    let base = ui.addr.clone();
    let token = ui.token.clone();
    offload(
        state,
        move || Client::new(base, token).cache(&node, &path),
        move |state, result| match result {
            Ok(local) => state.update(|ui| {
                ui.drag_path = local.clone();
                ui.drag_note = format!("已缓存 {name}。本机路径：{local}。Native Shell 没有把本地文件拖出到系统文件管理器的接口，请点「显示」。");
                ui.status = ui.drag_note.clone();
                ui.err.clear();
            }),
            Err(err) => state.update(|ui| ui.err = err),
        },
    );
}

pub fn reveal_cached(state: Signal<Ui>) {
    let path = state.get().drag_path.clone();
    if path.is_empty() {
        state.update(|ui| ui.status = "请先缓存文件".into());
        return;
    }
    let base = state.get().addr.clone();
    let token = state.get().token.clone();
    offload(
        state,
        move || Client::new(base, token).reveal(&path),
        |state, result| {
            if let Err(err) = result {
                state.update(|ui| ui.err = err);
            }
        },
    );
}

pub fn toggle_preview(state: Signal<Ui>) {
    state.update(|ui| {
        ui.preview_on = !ui.preview_on;
        token::save_preview_on(ui.preview_on);
        if !ui.preview_on {
            ui.preview_path.clear();
            ui.preview_text.clear();
            ui.preview_image.clear();
            ui.preview_kind.clear();
        }
    });
    if state.get().preview_on {
        preview_selected(state);
    }
}

fn preview_selected(state: Signal<Ui>) {
    let ui = state.get();
    if !ui.preview_on {
        return;
    }
    let Some(entry) = selected(&ui) else {
        return;
    };
    if entry.is_dir || entry.path == ui.preview_path {
        return;
    }
    let path = entry.path.clone();
    let name = entry.name.clone();
    let kind = preview_kind(&name).to_owned();
    let node = ui.folder_node.clone();
    let size = entry.size;
    let generation = ui.preview_gen.wrapping_add(1);
    let base = ui.addr.clone();
    let token = ui.token.clone();
    let node_for_call = node.clone();
    let path_for_call = path.clone();
    state.update(|ui| {
        ui.preview_gen = generation;
        ui.preview_path = path.clone();
        ui.preview_kind = kind.clone();
        ui.preview_text.clear();
        ui.preview_image.clear();
    });
    offload(
        state,
        move || {
            let client = Client::new(base, token);
            let preview = client.preview(&node_for_call, &path_for_call)?;
            let mut image = String::new();
            if kind == "image" && size <= 20 * 1024 * 1024 {
                if let Ok(bytes) = client.read(&node_for_call, &path_for_call) {
                    let ext = name.rsplit_once('.').map(|(_, ext)| ext).unwrap_or("img");
                    if let Ok(file) = token::write_preview_file(ext, &bytes) {
                        image = file.display().to_string();
                    }
                }
            }
            Ok((preview.text, preview.kind, image))
        },
        move |state, result| {
            let ui = state.get();
            if ui.preview_gen != generation || ui.preview_path != path {
                return;
            }
            match result {
                Ok((text, kind, image)) => state.update(|ui| {
                    ui.preview_kind = if kind.is_empty() { ui.preview_kind.clone() } else { kind };
                    ui.preview_text = text;
                    ui.preview_image = image;
                }),
                Err(err) => state.update(|ui| ui.preview_text = err),
            }
        },
    );
}

pub fn poll(state: Signal<Ui>) {
    if !state.get().alive || state.get().poll_busy {
        return;
    }
    let base = state.get().addr.clone();
    let token = state.get().token.clone();
    state.update(|ui| ui.poll_busy = true);
    offload(
        state,
        move || {
            let client = Client::new(base, token);
            Ok((client.transfers()?, client.jobs()?))
        },
        |state, result| {
            state.update(|ui| ui.poll_busy = false);
            if let Ok((transfers, jobs)) = result {
                state.update(|ui| {
                    ui.transfers = transfers;
                    ui.imports = jobs.into_iter().filter(|job| job.mode == "import").collect();
                    note_conflicts(ui);
                });
            }
        },
    );
}

fn note_conflicts(ui: &mut Ui) {
    let mut path = String::new();
    let mut kind = String::new();
    let mut id = String::new();
    for transfer in &ui.transfers {
        if transfer.job.status == "conflict" && !transfer.job.conflict_path.is_empty() {
            path = transfer.job.conflict_path.clone();
            kind = "transfer".into();
            id = transfer.id.clone();
            break;
        }
    }
    if path.is_empty() {
        for job in &ui.imports {
            if job.status == "conflict" && !job.conflict_path.is_empty() {
                path = job.conflict_path.clone();
                kind = "job".into();
                id = job.id.clone();
                break;
            }
        }
    }
    let key = if id.is_empty() || path.is_empty() {
        String::new()
    } else {
        format!("{kind}\0{id}\0{path}")
    };
    if !key.is_empty() && key == ui.conflict_hold {
        ui.conflict_path.clear();
        return;
    }
    if !ui.conflict_hold.is_empty() && key != ui.conflict_hold {
        ui.conflict_hold.clear();
    }
    ui.conflict_path = path;
    ui.conflict_kind = kind;
    ui.conflict_id = id;
}

pub fn answer_conflict(state: Signal<Ui>, action: &str) {
    let ui = state.get();
    if !ui.conflict_hold.is_empty() || ui.conflict_id.is_empty() || ui.conflict_path.is_empty() {
        return;
    }
    let kind = ui.conflict_kind.clone();
    let id = ui.conflict_id.clone();
    let path = ui.conflict_path.clone();
    let apply = ui.conflict_apply;
    let hold = format!("{kind}\0{id}\0{path}");
    let action = action.to_owned();
    let base = ui.addr.clone();
    let token = ui.token.clone();
    state.update(|ui| {
        ui.conflict_hold = hold.clone();
        ui.conflict_apply = false;
        ui.conflict_path.clear();
    });
    offload(
        state,
        move || {
            let client = Client::new(base, token);
            if kind == "job" {
                client.answer_job_conflict(&id, &action, apply)
            } else {
                client.answer_transfer_conflict(&id, &action, apply)
            }
        },
        move |state, result| {
            state.update(|ui| {
                if ui.conflict_hold == hold {
                    ui.conflict_hold.clear();
                }
                if let Err(err) = result {
                    ui.err = err;
                }
            });
            poll(state);
            load_list(state);
        },
    );
}

pub fn cancel_transfer(state: Signal<Ui>, id: String) {
    let base = state.get().addr.clone();
    let token = state.get().token.clone();
    offload(
        state,
        move || Client::new(base, token).cancel_transfer(&id),
        |state, result| {
            if let Err(err) = result {
                state.update(|ui| ui.err = err);
            }
            poll(state);
        },
    );
}

pub fn resume_transfer(state: Signal<Ui>, id: String) {
    let base = state.get().addr.clone();
    let token = state.get().token.clone();
    offload(
        state,
        move || Client::new(base, token).resume_transfer(&id),
        |state, result| {
            if let Err(err) = result {
                state.update(|ui| ui.err = err);
            }
            poll(state);
        },
    );
}

pub fn open_settings(state: Signal<Ui>) {
    state.update(|ui| {
        ui.settings_open = true;
        ui.auth_input.clear();
        ui.settings_note.clear();
    });
    let base = state.get().addr.clone();
    let token = state.get().token.clone();
    offload(
        state,
        move || Client::new(base, token).settings(),
        |state, result| match result {
            Ok(settings) => state.update(|ui| {
                ui.net_mode = settings.net;
                ui.net_requested = settings.net_requested;
                ui.auth_configured = settings.auth_key_configured;
            }),
            Err(err) => state.update(|ui| ui.settings_note = err),
        },
    );
}

pub fn save_auth_key(state: Signal<Ui>) {
    let key = state.get().auth_input.trim().to_owned();
    if key.is_empty() || key.contains('\n') || key.contains('\r') {
        state.update(|ui| ui.settings_note = "key 不能为空，也不能包含换行".into());
        return;
    }
    state.update(|ui| ui.auth_input.clear());
    let base = state.get().addr.clone();
    let token = state.get().token.clone();
    offload(
        state,
        move || Client::new(base, token).set_auth_key(&key),
        |state, result| match result {
            Ok(status) => state.update(|ui| {
                ui.auth_configured = status.configured;
                ui.settings_note = "已保存。需要重启 agent 后才会使用。界面不会回显 key。若设置了 TS_AUTHKEY，环境变量优先。".into();
            }),
            Err(err) => state.update(|ui| ui.settings_note = err),
        },
    );
}

pub fn clear_auth_key(state: Signal<Ui>) {
    state.update(|ui| ui.auth_input.clear());
    let base = state.get().addr.clone();
    let token = state.get().token.clone();
    offload(
        state,
        move || Client::new(base, token).clear_auth_key(),
        |state, result| match result {
            Ok(status) => state.update(|ui| {
                ui.auth_configured = status.configured;
                ui.settings_note = "已清除。需要重启 agent。".into();
            }),
            Err(err) => state.update(|ui| ui.settings_note = err),
        },
    );
}

fn offload<T, F, A>(state: Signal<Ui>, work: F, apply: A)
where
    T: Send + 'static,
    F: FnOnce() -> Result<T, String> + Send + 'static,
    A: FnOnce(Signal<Ui>, Result<T, String>) + 'static,
{
    let run = move || {
        spawn(async move {
            let result = match blocking(work).await {
                Ok(value) => value,
                Err(err) => Err(err.to_string()),
            };
            apply(state, result);
        });
    };
    match APP_OWNER.get() {
        Some(owner) => {
            owner.run(run);
        }
        None => run(),
    }
}
