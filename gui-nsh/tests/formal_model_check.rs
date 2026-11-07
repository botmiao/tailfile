//! Explicit-state Breadth-First Search (BFS) model checker and bounded input
//! verifier for `tailfile-nsh` (`gui-nsh/src/model.rs`, `gui-nsh/src/token.rs`,
//! `gui-nsh/src/ui.rs`).
//!
//! Unlike hand-written scenario tests that check one sequence of steps someone
//! thought to write, this suite explores *every* sequence of actions up to a
//! stated depth bound (and every generated path input in the bounded alphabet)
//! directly against the real Rust state-transition functions in `gui-nsh`.

use std::cell::RefCell;
use std::collections::{BTreeSet, VecDeque};

use nsh_core::element::Root;
use nsh_core::prelude::*;
use nsh_core::{
    ApproxTextEngine, Core, EventKind, EventPayload, MountItem, NodeId, Points, PressEvent,
    ROOT_ID, Size,
};
use tailfile_nsh::{
    api::{Entry, NodeInfo},
    model::{self, TabState, TreeRow, Ui},
    token, ui,
};

thread_local! {
    static STEP_FN: RefCell<Option<Box<dyn FnOnce(Signal<Ui>)>>> = const { RefCell::new(None) };
    static MODEL_CORE: RefCell<Option<(Core<ApproxTextEngine>, NodeId)>> = const { RefCell::new(None) };
}

#[component]
fn MinimalModelHost() -> impl IntoElement {
    let state = signal(Ui::new());
    view().on_press(move || {
        if let Some(f) = STEP_FN.with(|slot| slot.borrow_mut().take()) {
            f(state);
        }
    })
}

fn with_signal_context<R: 'static>(f: impl FnOnce(Signal<Ui>) -> R + 'static) -> R {
    model::enable_test_mode();
    let out: std::rc::Rc<RefCell<Option<R>>> = std::rc::Rc::new(RefCell::new(None));
    let out_clone = out.clone();
    STEP_FN.with(|slot| {
        *slot.borrow_mut() = Some(Box::new(move |state| {
            *out_clone.borrow_mut() = Some(f(state));
        }));
    });
    MODEL_CORE.with(|cell| {
        let mut borrow = cell.borrow_mut();
        if borrow.is_none() {
            let viewport: Size<Points> = Size::new(px(10.0), px(10.0));
            let mut core = Core::new(ApproxTextEngine, viewport);
            let root = Root::of::<MinimalModelHost>();
            core.mount_root(&root);
            let batch = core.commit().unwrap_or_default();
            let host_id = batch
                .items
                .iter()
                .find_map(|item| match item {
                    MountItem::Insert {
                        parent,
                        child,
                        ..
                    } if *parent == ROOT_ID => Some(*child),
                    _ => None,
                })
                .expect("MinimalModelHost root child node");
            *borrow = Some((core, host_id));
        }
        let (core, host_id) = borrow.as_mut().unwrap();
        core.component_event(
            *host_id,
            EventKind::Press,
            &EventPayload::Press(PressEvent::default()),
        );
    });
    let res = out.borrow_mut().take().expect("STEP_FN executed");
    res
}

fn seed_model_ui(ui: &mut Ui) {
    ui.state_dir = std::env::temp_dir()
        .join(format!("tailfile-formal-mc-{}", std::process::id()))
        .to_string_lossy()
        .into_owned();
    ui.alive = true;
    ui.nodes = vec![
        NodeInfo {
            id: "alpha".into(),
            name: "alpha-mac".into(),
            os: "darwin".into(),
            online: true,
            agent_ok: true,
            self_node: true,
            read_only: false,
            read_roots: vec!["/tmp/demo/alpha".into()],
        },
        NodeInfo {
            id: "beta".into(),
            name: "beta-win".into(),
            os: "windows".into(),
            online: true,
            agent_ok: true,
            self_node: false,
            read_only: false,
            read_roots: vec!["C:/Users/beta/Shared".into()],
        },
        NodeInfo {
            id: "gamma".into(),
            name: "gamma-linux".into(),
            os: "linux".into(),
            online: true,
            agent_ok: true,
            self_node: false,
            read_only: true,
            read_roots: vec!["/srv/gamma".into()],
        },
    ];
    ui.folder_node = "alpha".into();
    ui.folder_path = "/tmp/demo/alpha".into();
    ui.entries = vec![
        Entry {
            name: "notes".into(),
            path: "/tmp/demo/alpha/notes".into(),
            is_dir: true,
            size: 0,
            mod_time: "2026-01-02T10:00:00Z".into(),
        },
        Entry {
            name: "photos".into(),
            path: "/tmp/demo/alpha/photos".into(),
            is_dir: true,
            size: 0,
            mod_time: "2026-01-03T10:00:00Z".into(),
        },
        Entry {
            name: "hello.txt".into(),
            path: "/tmp/demo/alpha/hello.txt".into(),
            is_dir: false,
            size: 128,
            mod_time: "2026-01-01T08:00:00Z".into(),
        },
    ];
    ui.tabs = vec![
        TabState::new("alpha", "/tmp/demo/alpha"),
        TabState::new("beta", "C:/Users/beta/Shared"),
        TabState::new("gamma", "/srv/gamma"),
    ];
    ui.closed_tabs = vec![TabState::new("gamma", "/srv/gamma")];
    ui.active_tab = 0;
    ui.selected = -1;
}

/// Runs a sequence of actions from the seeded initial `Ui` state inside the
/// `nsh_core` reactive signal context and returns the resulting `Ui` snapshot.
fn run_trace<A: Copy + 'static>(trace: &[A], apply: fn(Signal<Ui>, A)) -> Ui {
    let steps = trace.to_vec();
    with_signal_context(move |state| {
        let mut ui = Ui::new();
        seed_model_ui(&mut ui);
        state.set(ui);
        for action in steps {
            apply(state, action);
        }
        state.get()
    })
}

fn format_trace<A: std::fmt::Debug>(trace: &[A]) -> String {
    trace
        .iter()
        .enumerate()
        .map(|(idx, step)| format!("  Step {}: {:?}", idx + 1, step))
        .collect::<Vec<_>>()
        .join("\n")
}

// ============================================================================
// Check 1: Read-Root Sandbox Escape Freedom & Sibling Prefix Isolation
// ============================================================================

#[derive(Clone, Copy, Debug)]
enum NavStep {
    EnterDir0,
    EnterDir1,
    GoUp,
    NavBack,
    NavForward,
    SelectSubfolder,
    ClickTreeRoot,
    SwitchTab0,
    SwitchTab1,
}

fn sync_mock_entries_for_active_folder(state: Signal<Ui>) {
    state.update(|ui| {
        let base = ui.folder_path.trim_end_matches(['/', '\\']).to_string();
        if base.is_empty() {
            ui.entries.clear();
            return;
        }
        ui.entries = vec![
            Entry {
                name: "notes".into(),
                path: format!("{base}/notes"),
                is_dir: true,
                size: 0,
                mod_time: "2026-01-02T10:00:00Z".into(),
            },
            Entry {
                name: "photos".into(),
                path: format!("{base}/photos"),
                is_dir: true,
                size: 0,
                mod_time: "2026-01-03T10:00:00Z".into(),
            },
            Entry {
                name: "hello.txt".into(),
                path: format!("{base}/hello.txt"),
                is_dir: false,
                size: 128,
                mod_time: "2026-01-01T08:00:00Z".into(),
            },
        ];
    });
}

fn apply_nav_step(state: Signal<Ui>, step: NavStep) {
    match step {
        NavStep::EnterDir0 => model::enter_row_dir(state, 0),
        NavStep::EnterDir1 => model::enter_row_dir(state, 1),
        NavStep::GoUp => model::go_up(state),
        NavStep::NavBack => model::nav_back(state),
        NavStep::NavForward => model::nav_forward(state),
        NavStep::SelectSubfolder => {
            let ui = state.get();
            if ui.folder_node == "alpha" {
                model::select_folder(state, "alpha".into(), "/tmp/demo/alpha/notes/deep".into());
            } else if ui.folder_node == "beta" {
                model::select_folder(
                    state,
                    "beta".into(),
                    "C:/Users/beta/Shared/work".into(),
                );
            }
        }
        NavStep::ClickTreeRoot => {
            model::click_tree(
                state,
                TreeRow {
                    key: model::dir_key("alpha", "/tmp/demo/alpha"),
                    depth: 1,
                    label: "alpha".into(),
                    node: "alpha".into(),
                    path: "/tmp/demo/alpha".into(),
                    is_node: false,
                },
            );
        }
        NavStep::SwitchTab0 => model::switch_tab(state, 0),
        NavStep::SwitchTab1 => model::switch_tab(state, 1),
    }
    sync_mock_entries_for_active_folder(state);
}

fn nav_state_fingerprint(ui: &Ui) -> String {
    let tab = ui.tabs.get(ui.active_tab);
    format!(
        "{}|{}|{}|{:?}|{:?}",
        ui.active_tab,
        ui.folder_node,
        ui.folder_path,
        tab.map(|t| &t.back),
        tab.map(|t| &t.forward),
    )
}

#[test]
fn check_read_root_escape_freedom() {
    // Part A: Exhaustive BFS over all navigation sequences up to depth 4.
    let alphabet = [
        NavStep::EnterDir0,
        NavStep::EnterDir1,
        NavStep::GoUp,
        NavStep::NavBack,
        NavStep::NavForward,
        NavStep::SelectSubfolder,
        NavStep::ClickTreeRoot,
        NavStep::SwitchTab0,
        NavStep::SwitchTab1,
    ];
    let max_depth = 4usize;
    let mut visited = BTreeSet::new();
    let mut queue: VecDeque<Vec<NavStep>> = VecDeque::new();
    queue.push_back(Vec::new());

    let mut checked_states = 0usize;
    while let Some(trace) = queue.pop_front() {
        let ui = run_trace(&trace, apply_nav_step);
        checked_states += 1;

        // Invariant: active folder_path must always remain under the active node's read_root.
        let node = ui
            .nodes
            .iter()
            .find(|n| n.id == ui.folder_node)
            .expect("active node must exist");
        let root = &node.read_roots[0];
        assert!(
            token::path_is_under(&ui.folder_path, root),
            "Property violated (ReadRootEscapeFreedom):\n\
             Active path {:?} escaped read_root {:?} of node {:?}\n\
             Counterexample trace:\n{}",
            ui.folder_path,
            root,
            ui.folder_node,
            format_trace(&trace)
        );

        // Invariant: every breadcrumb generated by `ui::crumbs(&ui)` must also stay under `root`.
        for crumb in ui::crumbs(&ui) {
            if !crumb.sep {
                assert!(
                    token::path_is_under(&crumb.path, root),
                    "Property violated (CrumbEscapeFreedom):\n\
                     Breadcrumb path {:?} escaped read_root {:?}\n\
                     Counterexample trace:\n{}",
                    crumb.path,
                    root,
                    format_trace(&trace)
                );
            }
        }

        let fp = nav_state_fingerprint(&ui);
        if visited.insert(fp) && trace.len() < max_depth {
            for &step in &alphabet {
                let mut next = trace.clone();
                next.push(step);
                queue.push_back(next);
            }
        }
    }
    assert!(checked_states > 50, "explored {checked_states} states");

    // Part B: Exhaustive bounded input verification over normalized Unix & Windows path prefixes.
    let roots = [
        "/tmp/alpha",
        "/tmp/alpha/",
        "/Users/alice",
        "C:/Users/beta/Shared",
        "C:\\Users\\beta\\Shared\\",
    ];
    let suffixes = [
        "",
        "/",
        "\\",
        "/notes",
        "/notes/deep.txt",
        "-evil",
        "2",
        "_backup/file.txt",
    ];
    for root in roots {
        let norm_root = token::norm_remote(root);
        for suffix in suffixes {
            let candidate = format!("{norm_root}{suffix}");
            let is_under = token::path_is_under(&candidate, root);
            let expected = suffix.is_empty()
                || suffix == "/"
                || suffix == "\\"
                || suffix == "/notes"
                || suffix == "/notes/deep.txt";
            assert_eq!(
                is_under, expected,
                "Property violated (PrefixCollisionSafety): path_is_under({candidate:?}, {root:?}) == {is_under}, expected {expected}"
            );
        }
    }
}

// ============================================================================
// Check 2: Overlay Mutual Exclusion & Escape Dismissal Determinism
// ============================================================================

#[derive(Clone, Copy, Debug)]
enum OverlayStep {
    BeginNewFolder,
    BeginRename,
    AskDelete,
    OpenDaemonHelp,
    OpenSettings,
    OpenContextMenuRow0,
    OpenMorePopover,
    SelectRow0,
    ToggleSelectHello,
    PressEscape,
}

fn apply_overlay_step(state: Signal<Ui>, step: OverlayStep) {
    match step {
        OverlayStep::BeginNewFolder => model::begin_new_folder(state),
        OverlayStep::BeginRename => {
            model::select_row(state, 0);
            model::begin_rename(state);
        }
        OverlayStep::AskDelete => {
            model::select_row(state, 0);
            model::ask_delete(state);
        }
        OverlayStep::OpenDaemonHelp => model::open_daemon_help(state, "delta".into()),
        OverlayStep::OpenSettings => model::open_settings(state),
        OverlayStep::OpenContextMenuRow0 => model::open_context_menu(state, 0, 200.0, 200.0),
        OverlayStep::OpenMorePopover => state.update(|ui| ui.more_open = true),
        OverlayStep::SelectRow0 => model::select_row(state, 0),
        OverlayStep::ToggleSelectHello => {
            model::toggle_select_path(state, "/tmp/demo/alpha/hello.txt".into())
        }
        OverlayStep::PressEscape => {
            // Exact body of the `"escape"` key handler registered in `model::boot` (`gui-nsh/src/model.rs:519-532`).
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
        }
    }
}

fn overlay_fingerprint(ui: &Ui) -> u16 {
    (ui.inline_new as u16)
        | ((ui.inline_rename as u16) << 1)
        | ((ui.delete_open as u16) << 2)
        | ((ui.daemon_open as u16) << 3)
        | ((ui.settings_open as u16) << 4)
        | ((ui.ctx_open as u16) << 5)
        | ((ui.more_open as u16) << 6)
        | (((ui.selected >= 0) as u16) << 7)
        | (((!ui.selected_paths.is_empty()) as u16) << 8)
}

#[test]
fn check_overlay_and_escape_determinism() {
    let openers = [
        OverlayStep::BeginNewFolder,
        OverlayStep::BeginRename,
        OverlayStep::AskDelete,
        OverlayStep::OpenDaemonHelp,
        OverlayStep::OpenSettings,
        OverlayStep::OpenContextMenuRow0,
        OverlayStep::OpenMorePopover,
        OverlayStep::SelectRow0,
        OverlayStep::ToggleSelectHello,
    ];
    let mut visited = BTreeSet::new();
    let mut queue: VecDeque<Vec<OverlayStep>> = VecDeque::new();
    queue.push_back(Vec::new());

    while let Some(trace) = queue.pop_front() {
        let ui = run_trace(&trace, apply_overlay_step);

        // Invariant 1: inline_new and inline_rename are mutually exclusive.
        assert!(
            !(ui.inline_new && ui.inline_rename),
            "Property violated (InlineEditorMutualExclusion):\n\
             Both inline_new and inline_rename are true simultaneously.\n\
             Counterexample trace:\n{}",
            format_trace(&trace)
        );

        // Invariant 2: if the last step was OpenContextMenuRow0, more_open must be false.
        if matches!(trace.last(), Some(OverlayStep::OpenContextMenuRow0)) {
            assert!(
                !ui.more_open,
                "Property violated (ContextMenuClosesMorePopover):\n\
                 more_open remained true after open_context_menu.\n\
                 Counterexample trace:\n{}",
                format_trace(&trace)
            );
        }

        // Invariant 3: from EVERY reachable overlay state, pressing Escape clears all overlays & selections.
        let mut esc_trace = trace.clone();
        esc_trace.push(OverlayStep::PressEscape);
        let after_esc = run_trace(&esc_trace, apply_overlay_step);
        assert!(
            !after_esc.inline_new
                && !after_esc.inline_rename
                && !after_esc.delete_open
                && !after_esc.daemon_open
                && !after_esc.settings_open
                && !after_esc.ctx_open
                && !after_esc.more_open
                && after_esc.selected == -1
                && after_esc.selected_paths.is_empty(),
            "Property violated (EscapeDismissesAllOverlays):\n\
             Overlays/selections remained active after Escape.\n\
             Counterexample trace:\n{}",
            format_trace(&esc_trace)
        );

        let fp = overlay_fingerprint(&ui);
        if visited.insert(fp) && trace.len() < 4 {
            for &step in &openers {
                let mut next = trace.clone();
                next.push(step);
                queue.push_back(next);
            }
        }
    }
}

// ============================================================================
// Check 3: Folder-Scoped Action & Selection Isolation Across Tab/Folder Moves
// ============================================================================

#[derive(Clone, Copy, Debug)]
enum IsolationStep {
    ToggleSelectAlphaHello,
    BeginNewFolder,
    BeginRenameRow0,
    SwitchTab1Beta,
    SwitchTab2GammaReadOnly,
    SelectFolderGammaReadOnly,
    NavBack,
    ReopenClosedTab,
}

fn apply_isolation_step(state: Signal<Ui>, step: IsolationStep) {
    match step {
        IsolationStep::ToggleSelectAlphaHello => {
            model::toggle_select_path(state, "/tmp/demo/alpha/hello.txt".into())
        }
        IsolationStep::BeginNewFolder => model::begin_new_folder(state),
        IsolationStep::BeginRenameRow0 => {
            model::select_row(state, 0);
            model::begin_rename(state);
        }
        IsolationStep::SwitchTab1Beta => model::switch_tab(state, 1),
        IsolationStep::SwitchTab2GammaReadOnly => model::switch_tab(state, 2),
        IsolationStep::SelectFolderGammaReadOnly => {
            model::select_folder(state, "gamma".into(), "/srv/gamma".into())
        }
        IsolationStep::NavBack => model::nav_back(state),
        IsolationStep::ReopenClosedTab => model::reopen_closed_tab(state),
    }
}

#[test]
fn check_folder_action_target_isolation() {
    let alphabet = [
        IsolationStep::ToggleSelectAlphaHello,
        IsolationStep::BeginNewFolder,
        IsolationStep::BeginRenameRow0,
        IsolationStep::SwitchTab1Beta,
        IsolationStep::SwitchTab2GammaReadOnly,
        IsolationStep::SelectFolderGammaReadOnly,
        IsolationStep::NavBack,
        IsolationStep::ReopenClosedTab,
    ];
    let mut queue: VecDeque<Vec<IsolationStep>> = VecDeque::new();
    queue.push_back(Vec::new());

    let mut violations = Vec::new();
    let mut seen_violation_kinds = BTreeSet::new();

    while let Some(trace) = queue.pop_front() {
        let ui = run_trace(&trace, apply_isolation_step);

        // Invariant A (ReadOnlyWriteGate): When the active folder_node is read-only ("gamma"),
        // neither inline_new nor inline_rename nor delete_open may be active.
        let is_ro = ui
            .nodes
            .iter()
            .find(|n| n.id == ui.folder_node)
            .map(|n| n.read_only)
            .unwrap_or(false);
        if is_ro && (ui.inline_new || ui.inline_rename || ui.delete_open) {
            if seen_violation_kinds.insert("ReadOnlyWriteGate") {
                violations.push(format!(
                    "[Violation 1 — ReadOnlyWriteGate]\n\
                     Active node {:?} has read_only=true, but write editor is open \
                     (inline_new={}, inline_rename={}, delete_open={}).\n\
                     Shortest counterexample ({} steps):\n{}",
                    ui.folder_node,
                    ui.inline_new,
                    ui.inline_rename,
                    ui.delete_open,
                    trace.len(),
                    format_trace(&trace)
                ));
            }
        }

        // Invariant B (SelectionFolderScope): Every path in `ui.selected_paths` must lie
        // under the currently active `ui.folder_path`.
        for sel_path in &ui.selected_paths {
            if !token::path_is_under(sel_path, &ui.folder_path) {
                if seen_violation_kinds.insert("SelectionFolderScope") {
                    violations.push(format!(
                        "[Violation 2 — SelectionFolderScope]\n\
                         Active folder is {}:{} but ui.selected_paths still contains {:?} from another folder.\n\
                         Calling ask_delete/confirm_delete in this state targets the wrong host/folder.\n\
                         Shortest counterexample ({} steps):\n{}",
                        ui.folder_node,
                        ui.folder_path,
                        sel_path,
                        trace.len(),
                        format_trace(&trace)
                    ));
                }
            }
        }

        if trace.len() < 2 {
            for &step in &alphabet {
                let mut next = trace.clone();
                next.push(step);
                queue.push_back(next);
            }
        }
    }

    if token::path_is_under("/tmp/alpha/../alpha-evil", "/tmp/alpha") {
        violations.push(
            "[Violation 3 — DotDotTraversalInPathIsUnder]\n\
             token::path_is_under(\"/tmp/alpha/../alpha-evil\", \"/tmp/alpha\") returned true \
             because token::norm_remote does not resolve or reject `..` segments."
                .to_string(),
        );
    }

    assert!(
        violations.is_empty(),
        "Formal model check `check_folder_action_target_isolation` found {} invariant violation(s):\n\n{}",
        violations.len(),
        violations.join("\n\n")
    );
}

// ============================================================================
// Check 4: Active Tab & Top-Level Ui Field Synchronization Across Tab Ops
// ============================================================================

#[derive(Clone, Copy, Debug)]
enum TabSyncStep {
    SelectRow2,
    SetSortMtime,
    SwitchTab0,
    SwitchTab1,
    CloseTab0,
    CloseTab1,
    ReopenClosedTab,
    NewTabFromCurrent,
    FocusExistingHostSubfolder,
}

fn apply_tab_sync_step(state: Signal<Ui>, step: TabSyncStep) {
    match step {
        TabSyncStep::SelectRow2 => model::select_row(state, 2),
        TabSyncStep::SetSortMtime => model::set_sort(state, "mtime"),
        TabSyncStep::SwitchTab0 => model::switch_tab(state, 0),
        TabSyncStep::SwitchTab1 => model::switch_tab(state, 1),
        TabSyncStep::CloseTab0 => model::close_tab(state, 0),
        TabSyncStep::CloseTab1 => model::close_tab(state, 1),
        TabSyncStep::ReopenClosedTab => model::reopen_closed_tab(state),
        TabSyncStep::NewTabFromCurrent => model::new_tab_from_current(state),
        TabSyncStep::FocusExistingHostSubfolder => {
            model::focus_or_open_tab(
                state,
                "alpha".into(),
                "/tmp/demo/alpha/notes".into(),
                false,
            );
        }
    }
}

#[test]
fn check_active_tab_state_sync() {
    let alphabet = [
        TabSyncStep::SelectRow2,
        TabSyncStep::SetSortMtime,
        TabSyncStep::SwitchTab0,
        TabSyncStep::SwitchTab1,
        TabSyncStep::CloseTab0,
        TabSyncStep::CloseTab1,
        TabSyncStep::ReopenClosedTab,
        TabSyncStep::NewTabFromCurrent,
        TabSyncStep::FocusExistingHostSubfolder,
    ];
    let mut queue: VecDeque<Vec<TabSyncStep>> = VecDeque::new();
    queue.push_back(Vec::new());

    let mut violations = Vec::new();
    let mut seen_violation_kinds = BTreeSet::new();

    while let Some(trace) = queue.pop_front() {
        let ui = run_trace(&trace, apply_tab_sync_step);

        if !ui.tabs.is_empty() {
            let active = &ui.tabs[ui.active_tab];
            if ui.folder_node != active.node || ui.folder_path != active.path {
                if seen_violation_kinds.insert("ActiveTabPathSync") {
                    violations.push(format!(
                        "[Violation — ActiveTabPathSync]\n\
                         ui=({}, {}) != ui.tabs[{}]=({}, {})\n\
                         Shortest counterexample ({} steps):\n{}",
                        ui.folder_node,
                        ui.folder_path,
                        ui.active_tab,
                        active.node,
                        active.path,
                        trace.len(),
                        format_trace(&trace)
                    ));
                }
            }
            if ui.selected != active.selected
                || ui.sort != active.sort
                || ui.filter != active.filter
                || ui.view != active.view
            {
                let last_step_name = format!("{:?}", trace.last());
                if seen_violation_kinds.insert(Box::leak(last_step_name.into_boxed_str())) {
                    violations.push(format!(
                        "[Violation — ActiveTabFieldsSync after {:?}]\n\
                         Top-level Ui (selected={}, sort={:?}) desynchronized from \
                         ui.tabs[{}] (selected={}, sort={:?}).\n\
                         Shortest counterexample ({} steps):\n{}",
                        trace.last().unwrap(),
                        ui.selected,
                        ui.sort,
                        ui.active_tab,
                        active.selected,
                        active.sort,
                        trace.len(),
                        format_trace(&trace)
                    ));
                }
            }
        }

        if trace.len() < 3 {
            for &step in &alphabet {
                let mut next = trace.clone();
                next.push(step);
                queue.push_back(next);
            }
        }
    }

    assert!(
        violations.is_empty(),
        "Formal model check `check_active_tab_state_sync` found {} invariant violation(s):\n\n{}",
        violations.len(),
        violations.join("\n\n")
    );
}
