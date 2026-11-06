//! Window state and the agent calls behind each control.
//! Network work runs on blocking threads; signals are written back on the
//! engine task that `spawn` polls.

use std::sync::OnceLock;
use std::time::Duration;

use nsh_core::prelude::*;

use crate::api::{Client, Entry, Job, NodeInfo, Transfer};
use crate::daemon::{self, InstallGuide};
use crate::launch;
use crate::names;
use crate::persist::{self, SavedTab, ThemePref, UiPersist};
use crate::token::{
    self, base_remote, file_uri_to_path, join_remote, parent_remote, path_is_under, preview_kind,
};
use nsh_core::platform::Appearance;

#[derive(Clone, PartialEq)]
pub struct TabState {
    pub id: String,
    pub node: String,
    pub path: String,
    pub back: Vec<String>,
    pub forward: Vec<String>,
    pub selected: i32,
    pub filter: String,
    pub sort: String,
    pub view: String,
}

impl TabState {
    pub fn new(node: impl Into<String>, path: impl Into<String>) -> Self {
        Self {
            id: format!("t{}", next_tab_id()),
            node: node.into(),
            path: path.into(),
            back: Vec::new(),
            forward: Vec::new(),
            selected: -1,
            filter: String::new(),
            sort: "name".into(),
            view: "list".into(),
        }
    }

    pub fn title(&self, nodes: &[NodeInfo]) -> String {
        let host = nodes
            .iter()
            .find(|n| n.id == self.node)
            .map(|n| {
                if n.name.is_empty() {
                    n.id.clone()
                } else {
                    n.name.clone()
                }
            })
            .unwrap_or_else(|| self.node.clone());
        let dir = base_remote(&self.path);
        let dir = if dir.is_empty() {
            self.path.clone()
        } else {
            dir
        };
        format!("{host} › {dir}")
    }
}

fn next_tab_id() -> u64 {
    use std::sync::atomic::{AtomicU64, Ordering};
    static N: AtomicU64 = AtomicU64::new(1);
    N.fetch_add(1, Ordering::Relaxed)
}

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
    pub clip_items: Vec<(String, String, String)>,
    pub tabs: Vec<TabState>,
    pub active_tab: usize,
    pub closed_tabs: Vec<TabState>,
    pub theme: ThemePref,
    pub restore_tabs: bool,
    pub filter: String,
    pub sort: String,
    pub view: String,
    pub selected_paths: Vec<String>,
    pub delete_open: bool,
    pub more_open: bool,
    pub delete_paths: Vec<String>,
    pub delete_note: String,
    pub inline_new: bool,
    pub inline_rename: bool,
    pub name_error: String,
    pub daemon_open: bool,
    pub daemon_node: String,
    pub daemon_guide: Option<InstallGuide>,
    pub daemon_os: String,
    pub history_note: String,
    /// Tailnet column folded to host icons. Remembered in `nsh-ui.json`.
    pub sidebar_collapsed: bool,
    pub ctx_open: bool,
    pub ctx_x: f32,
    pub ctx_y: f32,
    pub ctx_index: i32,
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
            clip_items: Vec::new(),
            tabs: Vec::new(),
            active_tab: 0,
            closed_tabs: Vec::new(),
            theme: ThemePref::System,
            restore_tabs: true,
            filter: String::new(),
            sort: "name".into(),
            view: "list".into(),
            selected_paths: Vec::new(),
            delete_open: false,
            more_open: false,
            delete_paths: Vec::new(),
            delete_note: String::new(),
            inline_new: false,
            inline_rename: false,
            name_error: String::new(),
            daemon_open: false,
            daemon_node: String::new(),
            daemon_guide: None,
            daemon_os: default_os_hint(),
            history_note: String::new(),
            sidebar_collapsed: false,
            ctx_open: false,
            ctx_x: 0.0,
            ctx_y: 0.0,
            ctx_index: -1,
        }
    }
}

fn default_os_hint() -> String {
    if cfg!(target_os = "windows") {
        "windows".into()
    } else if cfg!(target_os = "macos") {
        "macos".into()
    } else {
        "linux".into()
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
    if node.self_node {
        label.push_str("  ·  本机");
    }
    if !node.online {
        label.push_str("  ·  离线");
    }
    label
}

pub fn selected(ui: &Ui) -> Option<Entry> {
    ui.entries
        .get(ui.selected.max(0) as usize)
        .cloned()
        .filter(|_| ui.selected >= 0)
}

static APP_OWNER: OnceLock<Owner> = OnceLock::new();
static SHUTTING_DOWN: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);

/// Terminate the process immediately without risking a Windows `ExitProcess`
/// (`LdrShutdownProcess` / `DllMain(DLL_PROCESS_DETACH)`) deadlock when
/// `nsh-ui` (Direct3D 11 / DXGI / Direct2D / DirectWrite / OLE / DWM) or
/// `nsh-tasks` / `tokio` blocking pool threads (Winsock `ureq`) hold internal
/// loader or COM locks during teardown.
pub fn force_exit(code: i32) -> ! {
    use std::io::Write;
    let _ = std::io::stdout().flush();
    let _ = std::io::stderr().flush();
    #[cfg(target_os = "windows")]
    unsafe {
        #[link(name = "kernel32")]
        unsafe extern "system" {
            fn GetCurrentProcess() -> *mut std::ffi::c_void;
            fn TerminateProcess(hProcess: *mut std::ffi::c_void, uExitCode: u32) -> i32;
        }
        TerminateProcess(GetCurrentProcess(), code as u32);
    }
    std::process::exit(code);
}

struct ExitOnWindowClose;

impl Drop for ExitOnWindowClose {
    fn drop(&mut self) {
        SHUTTING_DOWN.store(true, std::sync::atomic::Ordering::SeqCst);
        if !cfg!(test) {
            // `Runtime::drop` frees cleanups without calling their closures,
            // so `Drop` is what actually runs when the window closes. Give
            // `WM_DESTROY` / `run_app` a brief window to return to `main()`
            // cleanly, then force-exit so lingering runtime / D3D / COM threads
            // can never hang the command line.
            std::thread::spawn(|| {
                std::thread::sleep(Duration::from_millis(120));
                force_exit(0);
            });
        }
    }
}

pub fn boot(state: Signal<Ui>) {
    // Press handlers and timers are short-lived owners. Tasks spawned
    // directly from them are cancelled when the handler returns, so every
    // agent call is spawned under the component owner captured here.
    let _ = APP_OWNER.set(Owner::current());
    // This process is only the GUI client. When the window's owner is
    // dropped (the window closed), exit so tailfile-nsh cannot linger
    // with no window and no tray. The agent is a different process.
    let exit_guard = ExitOnWindowClose;
    cleanup(move || drop(exit_guard));
    restore_persist(state);
    apply_theme_pref(state.get().theme.clone());
    refresh(state);
    set_interval(Duration::from_secs(1), move || poll(state));
    on_key("mod+c", move |_| copy_selected(state));
    on_key("mod+v", move |_| paste(state));
    on_key("mod+r", move |_| refresh(state));
    on_key("mod+shift+n", move |_| begin_new_folder(state));
    on_key("f2", move |_| begin_rename(state));
    on_key("delete", move |_| ask_delete(state));
    on_key("backspace", move |_| {
        if cfg!(target_os = "macos") {
            ask_delete(state);
        }
    });
    on_key("mod+w", move |_| close_active_tab(state));
    on_key("mod+shift+t", move |_| reopen_closed_tab(state));
    on_key("alt+left", move |_| nav_back(state));
    on_key("alt+right", move |_| nav_forward(state));
    on_key("escape", move |_| {
        state.update(|ui| {
            ui.inline_new = false;
            ui.inline_rename = false;
            ui.name_error.clear();
            ui.selected_paths.clear();
            ui.selected = -1;
            ui.delete_open = false;
            ui.daemon_open = false;
            ui.settings_open = false;
            ui.ctx_open = false;
            ui.more_open = false;
        });
    });
    on_key("enter", move |_| {
        let ui = state.get();
        if ui.inline_rename {
            rename(state);
        } else if ui.inline_new {
            mkdir(state);
        } else {
            open_selected(state);
        }
    });
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
        |state, result| {
            match result {
            Ok(nodes) => {
                let mut open_folder = false;
                let mut expand: Option<(String, String)> = None;
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
                                ui.folder_path = root.clone();
                                let key = node_key(&id);
                                if !ui.open.iter().any(|item| item == &key) {
                                    ui.open.push(key);
                                }
                                if ui.tabs.is_empty() {
                                    ensure_tab_for(ui, &id, &root, true);
                                }
                                open_folder = true;
                            }
                        }
                    } else if ui.tabs.is_empty() && !ui.folder_path.is_empty() {
                        let id = ui.folder_node.clone();
                        let root = ui.folder_path.clone();
                        ensure_tab_for(ui, &id, &root, true);
                    }
                    if !ui.folder_node.is_empty() {
                        let id = ui.folder_node.clone();
                        let key = node_key(&id);
                        if !ui.open.iter().any(|item| item == &key) {
                            ui.open.push(key);
                        }
                        if let Some(node) = ui.nodes.iter().find(|n| n.id == id).cloned() {
                            let root = node
                                .read_roots
                                .iter()
                                .find(|r| path_is_under(&ui.folder_path, r))
                                .cloned()
                                .or_else(|| node.read_roots.first().cloned());
                            if let Some(root) = root {
                                let opened = dir_key(&id, &root);
                                if !ui.open.iter().any(|item| item == &opened) {
                                    ui.open.push(opened);
                                }
                                expand = Some((id, root));
                            }
                        }
                    }
                });
                if let Some((node, root)) = expand {
                    ensure_dirs(state, node, root);
                }
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
        }
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
    click_tree_ex(state, row, false);
}

pub fn click_tree_new_tab(state: Signal<Ui>, row: TreeRow) {
    click_tree_ex(state, row, true);
}

fn click_tree_ex(state: Signal<Ui>, row: TreeRow, force_new: bool) {
    let key = row.key.clone();
    let opening = !state.get().open.iter().any(|item| item == &key);
    state.update(|ui| {
        if opening {
            ui.open.push(key);
        } else if !force_new {
            // keep expanded when focusing; only collapse on plain toggle of empty expand intent
        }
    });
    if row.is_node {
        if let Some(node) = state
            .get()
            .nodes
            .iter()
            .find(|node| node.id == row.node)
            .cloned()
        {
            if !node.agent_ok || !node.online {
                open_daemon_help(state, node_label(&node));
            }
            if let Some(root) = node.read_roots.first().cloned() {
                focus_or_open_tab(state, node.id, root, force_new);
            }
        }
        return;
    }
    if opening {
        ensure_dirs(state, row.node.clone(), row.path.clone());
    }
    // unreachable daemon hint
    if let Some(node) = state.get().nodes.iter().find(|n| n.id == row.node) {
        if !node.agent_ok || !node.online {
            open_daemon_help(state, node_label(node));
        }
    }
    focus_or_open_tab(state, row.node, row.path, force_new);
}

pub fn select_folder(state: Signal<Ui>, node: String, path: String) {
    state.update(|ui| {
        let prev_node = ui.folder_node.clone();
        let prev_path = ui.folder_path.clone();
        if !prev_node.is_empty() && (prev_node != node || prev_path != path) {
            if let Some(tab) = ui.tabs.get_mut(ui.active_tab) {
                tab.back.push(format!("{prev_node}\0{prev_path}"));
                tab.forward.clear();
            }
        }
        ui.folder_node = node.clone();
        ui.folder_path = path.clone();
        ui.selected = -1;
        ui.selected_paths.clear();
        ui.preview_path.clear();
        ui.preview_text.clear();
        ui.preview_image.clear();
        ui.preview_kind.clear();
        ensure_tab_for(ui, &node, &path, false);
        sync_active_tab_fields(ui);
    });
    persist_ui(state);
    load_list(state);
}

pub fn select_row(state: Signal<Ui>, index: i32) {
    state.update(|ui| {
        ui.selected = index;
        ui.selected_paths.clear();
        ui.ctx_open = false;
        sync_active_tab_fields(ui);
    });
    preview_selected(state);
}

pub fn enter_row_dir(state: Signal<Ui>, index: i32) {
    let ui = state.get();
    let Some(entry) = ui.entries.get(index as usize).cloned() else {
        return;
    };
    if !entry.is_dir {
        return;
    }
    let node = ui.folder_node.clone();
    let path = entry.path.clone();
    let key = dir_key(&node, &path);
    state.update(|ui| {
        ui.ctx_open = false;
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
}

pub fn click_row(state: Signal<Ui>, index: i32, clicks: u32) {
    use std::sync::Mutex;
    use std::time::Instant;
    static LAST_CLICK: Mutex<Option<(String, String, Instant)>> = Mutex::new(None);

    let ui = state.get();
    let Some(entry) = ui.entries.get(index as usize).cloned() else {
        return;
    };
    let now = Instant::now();
    let mut is_double = clicks >= 2;
    if let Ok(mut guard) = LAST_CLICK.lock() {
        if !is_double {
            if let Some((prev_node, prev_path, prev_time)) = guard.as_ref() {
                if prev_node == &ui.folder_node
                    && prev_path == &entry.path
                    && now.duration_since(*prev_time) <= Duration::from_millis(450)
                {
                    is_double = true;
                }
            }
        }
        if is_double {
            *guard = None;
        } else {
            *guard = Some((ui.folder_node.clone(), entry.path.clone(), now));
        }
    }

    if is_double && entry.is_dir {
        enter_row_dir(state, index);
        return;
    }
    select_row(state, index);
}

pub fn open_selected(state: Signal<Ui>) {
    let ui = state.get();
    if ui.selected < 0 {
        return;
    }
    let Some(entry) = ui.entries.get(ui.selected as usize).cloned() else {
        return;
    };
    state.update(|ui| ui.ctx_open = false);
    if entry.is_dir {
        enter_row_dir(state, ui.selected);
    } else {
        cache_selected(state);
    }
}

pub fn open_selected_new_tab(state: Signal<Ui>) {
    let ui = state.get();
    let Some(entry) = selected(&ui) else {
        return;
    };
    if !entry.is_dir {
        return;
    }
    state.update(|ui| ui.ctx_open = false);
    focus_or_open_tab(state, ui.folder_node, entry.path, true);
}

pub fn open_context_menu(state: Signal<Ui>, index: i32, x: f32, y: f32) {
    state.update(|ui| {
        ui.ctx_open = true;
        ui.ctx_x = x;
        ui.ctx_y = y;
        ui.ctx_index = index;
        ui.more_open = false;
        if index >= 0 {
            if let Some(entry) = ui.entries.get(index as usize) {
                if !ui.selected_paths.iter().any(|p| p == &entry.path) {
                    ui.selected_paths.clear();
                }
            }
            ui.selected = index;
            sync_active_tab_fields(ui);
        }
    });
    if index >= 0 {
        preview_selected(state);
    }
}

pub fn close_context_menu(state: Signal<Ui>) {
    if state.get().ctx_open {
        state.update(|ui| ui.ctx_open = false);
    }
}

pub fn go_up(state: Signal<Ui>) {
    let ui = state.get();
    let Some(parent) = parent_remote(&ui.folder_path) else {
        return;
    };
    let inside = ui.nodes.iter().any(|node| {
        node.id == ui.folder_node
            && node
                .read_roots
                .iter()
                .any(|root| path_is_under(&parent, root))
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
    if ui.folder_node.is_empty() {
        return;
    }
    let mut items: Vec<(String, String, String)> = Vec::new();
    if !ui.selected_paths.is_empty() {
        for path in &ui.selected_paths {
            let name = base_remote(path);
            items.push((ui.folder_node.clone(), path.clone(), name));
        }
    } else if let Some(entry) = selected(&ui) {
        items.push((
            ui.folder_node.clone(),
            entry.path.clone(),
            entry.name.clone(),
        ));
    } else {
        state.update(|ui| ui.status = "请先选择要复制的文件或文件夹".into());
        return;
    }
    let n = items.len();
    let first = items[0].clone();
    state.update(|ui| {
        ui.clip_set = true;
        ui.clip_node = first.0.clone();
        ui.clip_path = first.1.clone();
        ui.clip_name = first.2.clone();
        ui.clip_items = items;
        ui.status = format!("剪贴板：{n} 项 · 来自 {}", first.0);
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
    let mut items = ui.clip_items.clone();
    if items.is_empty() && !ui.clip_path.is_empty() {
        items.push((
            ui.clip_node.clone(),
            ui.clip_path.clone(),
            ui.clip_name.clone(),
        ));
    }
    if items.is_empty() {
        state.update(|ui| ui.status = "剪贴板是空的".into());
        return;
    }
    let mode = ui.mode.clone();
    let to_node = ui.folder_node.clone();
    let to_dir = ui.folder_path.clone();
    let base = ui.addr.clone();
    let token = ui.token.clone();
    let n = items.len();
    offload(
        state,
        move || {
            let client = Client::new(base, token);
            let mut started = 0usize;
            let mut errors: Vec<String> = Vec::new();
            for (from_node, from_path, name) in items {
                let to_path = join_remote(&to_dir, &name);
                match client.start_transfer(&mode, &from_node, &from_path, &to_node, &to_path) {
                    Ok(_) => started += 1,
                    Err(err) => errors.push(format!("{name}: {err}")),
                }
            }
            if started == 0 {
                return Err(if errors.is_empty() {
                    "粘贴失败".into()
                } else {
                    errors.join("; ")
                });
            }
            Ok((mode, started, n, errors))
        },
        |state, result| match result {
            Ok((mode, started, total, errors)) => {
                state.update(|ui| {
                    ui.err = if errors.is_empty() {
                        String::new()
                    } else {
                        format!("部分失败：{}", errors.join("; "))
                    };
                    ui.status = format!(
                        "已开始{} {started}/{total} 项 → {}",
                        crate::api::mode_label(&mode),
                        ui.folder_node
                    );
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
    if ui.folder_node.is_empty() {
        state.update(|ui| ui.status = "请先打开可写目录".into());
        return;
    }
    let existing: Vec<String> = ui.entries.iter().map(|e| e.name.clone()).collect();
    let windows = cfg!(target_os = "windows");
    if let Err(err) = names::validate_name(&name, &existing, windows) {
        state.update(|ui| {
            ui.name_error = err.clone();
            ui.status = err;
        });
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
                    ui.inline_new = false;
                    ui.name_error.clear();
                    ui.subdirs
                        .retain(|(key, _)| key != &dir_key(&node, &parent));
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
    let existing: Vec<String> = ui
        .entries
        .iter()
        .filter(|e| e.path != entry.path)
        .map(|e| e.name.clone())
        .collect();
    if let Err(err) = names::validate_name(&name, &existing, cfg!(target_os = "windows")) {
        state.update(|ui| {
            ui.name_error = err.clone();
            ui.status = err;
        });
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
                    ui.inline_rename = false;
                    ui.name_error.clear();
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
    let paths: Vec<String> = uris
        .into_iter()
        .map(|uri| file_uri_to_path(&uri))
        .filter(|path| !path.is_empty())
        .collect();
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
        move |state, result| {
            match result {
            Ok(local) => state.update(|ui| {
                ui.drag_path = local.clone();
                ui.drag_note = format!("已缓存 {name}。本机路径：{local}。Native Shell 没有把本地文件拖出到系统文件管理器的接口，请点「显示」。");
                ui.status = ui.drag_note.clone();
                ui.err.clear();
            }),
            Err(err) => state.update(|ui| ui.err = err),
        }
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
                    ui.preview_kind = if kind.is_empty() {
                        ui.preview_kind.clone()
                    } else {
                        kind
                    };
                    ui.preview_text = text;
                    ui.preview_image = image;
                }),
                Err(err) => state.update(|ui| ui.preview_text = err),
            }
        },
    );
}

pub fn poll(state: Signal<Ui>) {
    use std::sync::atomic::{AtomicBool, Ordering};
    static POLL_BUSY: AtomicBool = AtomicBool::new(false);

    let ui = state.get();
    if !ui.alive {
        return;
    }
    if POLL_BUSY
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    let base = ui.addr.clone();
    let token = ui.token.clone();
    offload(
        state,
        move || {
            let client = Client::new(base, token);
            Ok((client.transfers()?, client.jobs()?))
        },
        |state, result| {
            POLL_BUSY.store(false, Ordering::SeqCst);
            if let Ok((transfers, jobs)) = result {
                let imports: Vec<Job> = jobs
                    .into_iter()
                    .filter(|job| job.mode == "import")
                    .collect();
                let cur = state.get();
                if cur.transfers != transfers || cur.imports != imports {
                    state.update(|ui| {
                        ui.transfers = transfers;
                        ui.imports = imports;
                        note_conflicts(ui);
                    });
                }
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
        |state, result| {
            match result {
            Ok(status) => state.update(|ui| {
                ui.auth_configured = status.configured;
                ui.settings_note = "已保存。需要重启 agent 后才会使用。界面不会回显 key。若设置了 TS_AUTHKEY，环境变量优先。".into();
            }),
            Err(err) => state.update(|ui| ui.settings_note = err),
        }
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

pub fn visible_entries(ui: &Ui) -> Vec<(usize, Entry)> {
    let mut rows: Vec<(usize, Entry)> = ui
        .entries
        .iter()
        .cloned()
        .enumerate()
        .filter(|(_, e)| {
            ui.filter.trim().is_empty()
                || e.name
                    .to_ascii_lowercase()
                    .contains(&ui.filter.trim().to_ascii_lowercase())
        })
        .collect();
    match ui.sort.as_str() {
        "size" => rows.sort_by(|a, b| {
            b.1.is_dir
                .cmp(&a.1.is_dir)
                .then(a.1.size.cmp(&b.1.size))
                .then(
                    a.1.name
                        .to_ascii_lowercase()
                        .cmp(&b.1.name.to_ascii_lowercase()),
                )
        }),
        "mtime" => rows.sort_by(|a, b| {
            b.1.is_dir
                .cmp(&a.1.is_dir)
                .then(b.1.mod_time.cmp(&a.1.mod_time))
                .then(
                    a.1.name
                        .to_ascii_lowercase()
                        .cmp(&b.1.name.to_ascii_lowercase()),
                )
        }),
        _ => rows.sort_by(|a, b| {
            b.1.is_dir.cmp(&a.1.is_dir).then(
                a.1.name
                    .to_ascii_lowercase()
                    .cmp(&b.1.name.to_ascii_lowercase()),
            )
        }),
    }
    rows
}

fn restore_persist(state: Signal<Ui>) {
    let ui = state.get();
    let data = persist::load(&ui.state_dir);
    state.update(|ui| {
        ui.theme = data.theme.clone();
        ui.restore_tabs = data.restore_tabs;
        ui.preview_on = data.preview_on;
        ui.sidebar_collapsed = data.sidebar_collapsed;
        if data.restore_tabs {
            ui.tabs = data
                .tabs
                .into_iter()
                .map(|t| TabState {
                    id: t.id,
                    node: t.node,
                    path: t.path,
                    back: Vec::new(),
                    forward: Vec::new(),
                    selected: -1,
                    filter: t.filter,
                    sort: t.sort,
                    view: t.view,
                })
                .collect();
            ui.active_tab = data.active_tab.min(ui.tabs.len().saturating_sub(1));
            if let Some(tab) = ui.tabs.get(ui.active_tab).cloned() {
                ui.folder_node = tab.node;
                ui.folder_path = tab.path;
                ui.filter = tab.filter;
                ui.sort = tab.sort;
                ui.view = tab.view;
            }
        }
    });
}

fn persist_ui(state: Signal<Ui>) {
    let ui = state.get();
    let data = UiPersist {
        theme: ui.theme.clone(),
        restore_tabs: ui.restore_tabs,
        tabs: ui
            .tabs
            .iter()
            .map(|t| SavedTab {
                id: t.id.clone(),
                node: t.node.clone(),
                path: t.path.clone(),
                filter: t.filter.clone(),
                sort: t.sort.clone(),
                view: t.view.clone(),
            })
            .collect(),
        active_tab: ui.active_tab,
        preview_on: ui.preview_on,
        sidebar_collapsed: ui.sidebar_collapsed,
    };
    persist::save(&ui.state_dir, &data);
    token::save_preview_on(ui.preview_on);
}

fn apply_theme_pref(pref: ThemePref) {
    let appearance = match pref {
        ThemePref::System => Appearance::System,
        ThemePref::Light => Appearance::Light,
        ThemePref::Dark => Appearance::Dark,
    };
    set_color_scheme(appearance);
}

pub fn toggle_sidebar(state: Signal<Ui>) {
    state.update(|ui| ui.sidebar_collapsed = !ui.sidebar_collapsed);
    persist_ui(state);
}

pub fn set_theme(state: Signal<Ui>, pref: ThemePref) {
    apply_theme_pref(pref.clone());
    state.update(|ui| {
        ui.theme = pref;
        ui.settings_note = format!("外观：{}", ui.theme.label());
    });
    persist_ui(state);
}

fn sync_active_tab_fields(ui: &mut Ui) {
    if let Some(tab) = ui.tabs.get_mut(ui.active_tab) {
        tab.node = ui.folder_node.clone();
        tab.path = ui.folder_path.clone();
        tab.selected = ui.selected;
        tab.filter = ui.filter.clone();
        tab.sort = ui.sort.clone();
        tab.view = ui.view.clone();
    }
}

fn ensure_tab_for(ui: &mut Ui, node: &str, path: &str, force_new: bool) {
    if !force_new {
        if let Some(idx) = ui.tabs.iter().position(|t| t.node == node) {
            // same host: reuse most recently matching path or that tab
            if let Some(exact) = ui
                .tabs
                .iter()
                .position(|t| t.node == node && t.path == path)
            {
                ui.active_tab = exact;
                return;
            }
            ui.active_tab = idx;
            if let Some(tab) = ui.tabs.get_mut(idx) {
                tab.path = path.to_owned();
            }
            return;
        }
    }
    let tab = TabState::new(node, path);
    ui.tabs.push(tab);
    ui.active_tab = ui.tabs.len() - 1;
}

pub fn focus_or_open_tab(state: Signal<Ui>, node: String, path: String, force_new: bool) {
    state.update(|ui| {
        ensure_tab_for(ui, &node, &path, force_new);
        ui.folder_node = node;
        ui.folder_path = path;
        ui.selected = -1;
        ui.selected_paths.clear();
        if let Some(tab) = ui.tabs.get(ui.active_tab) {
            ui.filter = tab.filter.clone();
            ui.sort = tab.sort.clone();
            ui.view = tab.view.clone();
        }
    });
    persist_ui(state);
    load_list(state);
}

pub fn switch_tab(state: Signal<Ui>, index: usize) {
    state.update(|ui| {
        if index >= ui.tabs.len() {
            return;
        }
        sync_active_tab_fields(ui);
        ui.active_tab = index;
        if let Some(tab) = ui.tabs.get(index).cloned() {
            ui.folder_node = tab.node;
            ui.folder_path = tab.path;
            ui.selected = tab.selected;
            ui.filter = tab.filter;
            ui.sort = tab.sort;
            ui.view = tab.view;
        }
        ui.preview_path.clear();
    });
    persist_ui(state);
    load_list(state);
}

pub fn close_active_tab(state: Signal<Ui>) {
    close_tab(state, state.get().active_tab);
}

pub fn close_tab(state: Signal<Ui>, index: usize) {
    let empty = state.get().tabs.is_empty();
    if empty || index >= state.get().tabs.len() {
        return;
    }
    state.update(|ui| {
        if index >= ui.tabs.len() {
            return;
        }
        let closed = ui.tabs.remove(index);
        ui.closed_tabs.push(closed);
        if ui.tabs.is_empty() {
            ui.folder_node.clear();
            ui.folder_path.clear();
            ui.entries.clear();
            ui.selected = -1;
            ui.selected_paths.clear();
            ui.active_tab = 0;
        } else if ui.active_tab == index || ui.active_tab > index {
            if ui.active_tab > index {
                ui.active_tab -= 1;
            }
            if ui.active_tab == index {
                ui.active_tab = index.min(ui.tabs.len() - 1);
            }
            if let Some(tab) = ui.tabs.get(ui.active_tab).cloned() {
                ui.folder_node = tab.node;
                ui.folder_path = tab.path;
                ui.filter = tab.filter;
                ui.sort = tab.sort;
                ui.view = tab.view;
                ui.selected = tab.selected;
            }
        }
    });
    persist_ui(state);
    if !state.get().folder_path.is_empty() {
        load_list(state);
    }
}

pub fn cycle_sort(state: Signal<Ui>) {
    let next = match state.get().sort.as_str() {
        "name" => "mtime",
        "mtime" => "size",
        _ => "name",
    };
    set_sort(state, next);
}

pub fn reopen_closed_tab(state: Signal<Ui>) {
    state.update(|ui| {
        if let Some(tab) = ui.closed_tabs.pop() {
            ui.folder_node = tab.node.clone();
            ui.folder_path = tab.path.clone();
            ui.filter = tab.filter.clone();
            ui.sort = tab.sort.clone();
            ui.view = tab.view.clone();
            ui.tabs.push(tab);
            ui.active_tab = ui.tabs.len() - 1;
        }
    });
    persist_ui(state);
    if !state.get().folder_path.is_empty() {
        load_list(state);
    }
}

pub fn new_tab_from_current(state: Signal<Ui>) {
    let ui = state.get();
    if ui.folder_node.is_empty() {
        return;
    }
    focus_or_open_tab(state, ui.folder_node.clone(), ui.folder_path.clone(), true);
}

pub fn nav_back(state: Signal<Ui>) {
    let ui = state.get();
    let Some(tab) = ui.tabs.get(ui.active_tab) else {
        return;
    };
    let Some(prev) = tab.back.last().cloned() else {
        state.update(|ui| ui.status = "没有上一页".into());
        return;
    };
    let parts: Vec<&str> = prev.split('\0').collect();
    if parts.len() != 2 {
        return;
    }
    state.update(|ui| {
        if let Some(tab) = ui.tabs.get_mut(ui.active_tab) {
            let _ = tab.back.pop();
            tab.forward
                .push(format!("{}\0{}", ui.folder_node, ui.folder_path));
        }
        ui.folder_node = parts[0].to_owned();
        ui.folder_path = parts[1].to_owned();
        sync_active_tab_fields(ui);
    });
    load_list(state);
}

pub fn nav_forward(state: Signal<Ui>) {
    let ui = state.get();
    let Some(tab) = ui.tabs.get(ui.active_tab) else {
        return;
    };
    let Some(next) = tab.forward.last().cloned() else {
        state.update(|ui| ui.status = "没有下一页".into());
        return;
    };
    let parts: Vec<&str> = next.split('\0').collect();
    if parts.len() != 2 {
        return;
    }
    state.update(|ui| {
        if let Some(tab) = ui.tabs.get_mut(ui.active_tab) {
            let _ = tab.forward.pop();
            tab.back
                .push(format!("{}\0{}", ui.folder_node, ui.folder_path));
        }
        ui.folder_node = parts[0].to_owned();
        ui.folder_path = parts[1].to_owned();
        sync_active_tab_fields(ui);
    });
    load_list(state);
}

pub fn set_filter(state: Signal<Ui>, filter: String) {
    state.update(|ui| {
        ui.filter = filter;
        sync_active_tab_fields(ui);
    });
    persist_ui(state);
}

pub fn set_sort(state: Signal<Ui>, sort: &'static str) {
    state.update(|ui| {
        ui.sort = sort.into();
        sync_active_tab_fields(ui);
    });
    persist_ui(state);
}

pub fn set_view(state: Signal<Ui>, view: &'static str) {
    state.update(|ui| {
        ui.view = view.into();
        sync_active_tab_fields(ui);
    });
    persist_ui(state);
}

pub fn begin_new_folder(state: Signal<Ui>) {
    let ui = state.get();
    if ui.folder_node.is_empty() {
        return;
    }
    if current_folder_readonly(&ui) {
        state.update(|ui| ui.status = "当前目录只读，无法新建文件夹".into());
        return;
    }
    let existing: Vec<String> = ui.entries.iter().map(|e| e.name.clone()).collect();
    let name = names::unique_folder_name(&existing);
    state.update(|ui| {
        ui.inline_new = true;
        ui.inline_rename = false;
        ui.new_name = name;
        ui.name_error.clear();
    });
}

pub fn begin_rename(state: Signal<Ui>) {
    let ui = state.get();
    let Some(entry) = selected(&ui) else {
        state.update(|ui| ui.status = "请先选择要重命名的项目".into());
        return;
    };
    if current_folder_readonly(&ui) {
        state.update(|ui| ui.status = "当前目录只读，无法重命名".into());
        return;
    }
    state.update(|ui| {
        ui.inline_rename = true;
        ui.inline_new = false;
        ui.rename_to = entry.name;
        ui.name_error.clear();
    });
}

fn current_folder_readonly(ui: &Ui) -> bool {
    ui.nodes
        .iter()
        .find(|n| n.id == ui.folder_node)
        .map(|n| n.read_only)
        .unwrap_or(false)
}

pub fn ask_delete(state: Signal<Ui>) {
    let ui = state.get();
    let mut paths = ui.selected_paths.clone();
    if paths.is_empty() {
        if let Some(entry) = selected(&ui) {
            paths.push(entry.path.clone());
        }
    }
    if paths.is_empty() {
        state.update(|ui| ui.status = "请先选择要删除的项目".into());
        return;
    }
    if current_folder_readonly(&ui) {
        state.update(|ui| ui.status = "当前目录只读，无法删除".into());
        return;
    }
    let host = ui
        .nodes
        .iter()
        .find(|n| n.id == ui.folder_node)
        .map(node_label)
        .unwrap_or_else(|| ui.folder_node.clone());
    let listing: Vec<String> = paths.iter().take(6).cloned().collect();
    let more = if paths.len() > 6 {
        format!("\n…另有 {} 项", paths.len() - 6)
    } else {
        String::new()
    };
    let note = format!(
        "将永久删除 {} 项（主机 {}）\n{}\n{}\n\n无法撤销，不进入回收站。",
        paths.len(),
        host,
        listing.join("\n"),
        more
    );
    state.update(|ui| {
        ui.delete_open = true;
        ui.delete_paths = paths;
        ui.delete_note = note;
    });
}

pub fn cancel_delete(state: Signal<Ui>) {
    state.update(|ui| {
        ui.delete_open = false;
        ui.delete_paths.clear();
        ui.delete_note.clear();
    });
}

pub fn confirm_delete(state: Signal<Ui>) {
    let ui = state.get();
    if ui.delete_paths.is_empty() {
        cancel_delete(state);
        return;
    }
    let node = ui.folder_node.clone();
    let paths = ui.delete_paths.clone();
    let base = ui.addr.clone();
    let token = ui.token.clone();
    state.update(|ui| {
        ui.delete_open = false;
        ui.status = "正在永久删除…".into();
    });
    offload(
        state,
        move || {
            let client = Client::new(base, token);
            for path in &paths {
                client.delete(&node, path)?;
            }
            Ok(paths.len())
        },
        move |state, result| match result {
            Ok(n) => {
                state.update(|ui| {
                    ui.err.clear();
                    ui.status =
                        format!("已永久删除 {n} 项 · {}:{}", ui.folder_node, ui.folder_path);
                    ui.delete_paths.clear();
                    ui.selected = -1;
                    ui.selected_paths.clear();
                });
                load_list(state);
            }
            Err(err) => state.update(|ui| {
                ui.err = err;
                ui.status = "删除失败".into();
            }),
        },
    );
}

pub fn clear_clipboard(state: Signal<Ui>) {
    state.update(|ui| {
        ui.clip_set = false;
        ui.clip_node.clear();
        ui.clip_path.clear();
        ui.clip_name.clear();
        ui.clip_items.clear();
        ui.status = "已清空剪贴板".into();
    });
}

pub fn open_daemon_help(state: Signal<Ui>, label: String) {
    let guide = daemon::for_unreachable(&label);
    state.update(|ui| {
        ui.daemon_open = true;
        ui.daemon_node = label;
        ui.daemon_guide = Some(guide);
    });
}

pub fn copy_daemon_command(state: Signal<Ui>) {
    let ui = state.get();
    let Some(guide) = ui.daemon_guide.as_ref() else {
        return;
    };
    let text = daemon::commands_clipboard(guide, &ui.daemon_os);
    set_clipboard(text);
    state.update(|ui| ui.status = "已复制安装命令到系统剪贴板".into());
}

pub fn close_daemon_help(state: Signal<Ui>) {
    state.update(|ui| {
        ui.daemon_open = false;
        ui.daemon_guide = None;
    });
}

pub fn toggle_select_path(state: Signal<Ui>, path: String) {
    state.update(|ui| {
        if let Some(pos) = ui.selected_paths.iter().position(|p| p == &path) {
            ui.selected_paths.remove(pos);
        } else {
            ui.selected_paths.push(path);
        }
    });
}

fn offload<T, F, A>(state: Signal<Ui>, work: F, apply: A)
where
    T: Send + 'static,
    F: FnOnce() -> Result<T, String> + Send + 'static,
    A: FnOnce(Signal<Ui>, Result<T, String>) + 'static,
{
    if SHUTTING_DOWN.load(std::sync::atomic::Ordering::SeqCst) {
        return;
    }
    let run = move || {
        spawn(async move {
            if SHUTTING_DOWN.load(std::sync::atomic::Ordering::SeqCst) {
                return;
            }
            let result = match blocking(work).await {
                Ok(value) => value,
                Err(err) => Err(err.to_string()),
            };
            if SHUTTING_DOWN.load(std::sync::atomic::Ordering::SeqCst) {
                return;
            }
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
