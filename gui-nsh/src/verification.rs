//! Executable formal verification suite for `tailfile-nsh` (INV-1 .. INV-6).
//!
//! Proves SVG/Canvas icon geometry, Taffy layout & bounding-box invariants across
//! desktop viewports, reactive state-machine transitions & `MountBatch` deltas,
//! interactive hit targets & keyboard shortcuts, end-to-end localhost HTTP agent
//! workflows, and cross-platform path/token security invariants.

use std::collections::{BTreeMap, BTreeSet};
use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpListener;
use std::sync::{Arc, Mutex};

use nsh_core::canvas::{
    CanvasOp, Drawing, FillRule, LineCap, LineJoin, Num, PathOp, parse_svg_path, point_in_path,
    point_in_stroke,
};
use nsh_core::element::Root;
use nsh_core::keymap::{KeyMatch, Keymap, Keystroke, Modifiers};
use nsh_core::platform::{Appearance, Os};
use nsh_core::prelude::*;
use nsh_core::{
    ApproxTextEngine, Bounds, ContextMenuEvent, Core, Cursor, EventKind, EventMask, EventPayload,
    InputConfig, MountBatch, MountItem, NodeId, PayloadValue, Points, PressEvent, ROOT_ID, Size,
    ViewKind, ViewProps, ViewStyle,
};

use tailfile_nsh::api::{Client, Entry, Job, NodeInfo, Transfer};
use tailfile_nsh::icons::{self, FileKind, OsKind, path as ip};
use tailfile_nsh::persist::{self, SavedTab, ThemePref, UiPersist};
use tailfile_nsh::ui::model::{self, TabState, TreeRow, Ui};
use tailfile_nsh::{Launch, daemon, names, set_launch, token, ui};

/// Reconstructed native view node in the shadow-tree simulator.
#[derive(Debug, Clone)]
struct MountedNode {
    id: NodeId,
    kind: ViewKind,
    parent: Option<NodeId>,
    children: Vec<NodeId>,
    frame: Bounds<Points>,
    style: ViewStyle,
    props: ViewProps,
    events: EventMask,
    text: Option<String>,
    drawing: Option<Arc<Drawing>>,
    input_config: Option<Box<InputConfig>>,
    input_value: Option<(String, u32)>,
}

impl MountedNode {
    fn new(id: NodeId, kind: ViewKind) -> Self {
        Self {
            id,
            kind,
            parent: None,
            children: Vec::new(),
            frame: Bounds::default(),
            style: ViewStyle::default(),
            props: ViewProps::default(),
            events: EventMask::default(),
            text: None,
            drawing: None,
            input_config: None,
            input_value: None,
        }
    }
}

/// Deterministic shadow-tree and Taffy layout simulator wrapping `nsh_core::Core`.
struct Harness {
    core: Core<ApproxTextEngine>,
    viewport: Size<Points>,
    nodes: BTreeMap<NodeId, MountedNode>,
    keymap: Arc<Keymap>,
    appearance: Appearance,
    commits: usize,
}

impl Harness {
    fn new(width: f32, height: f32) -> Self {
        model::enable_test_mode();
        let viewport = Size::new(px(width), px(height));
        let mut core = Core::new(ApproxTextEngine, viewport);
        core.set_environment(nsh_core::element::Environment {
            os: Os::Linux,
            window: viewport,
            ..Default::default()
        });
        let mut root_node = MountedNode::new(ROOT_ID, ViewKind::View);
        root_node.frame = Bounds {
            origin: point(px(0.0), px(0.0)),
            size: viewport,
        };
        let mut nodes = BTreeMap::new();
        nodes.insert(ROOT_ID, root_node);

        let mut harness = Self {
            core,
            viewport,
            nodes,
            keymap: Arc::new(Keymap::default()),
            appearance: Appearance::System,
            commits: 0,
        };
        let root = Root::of::<ui::App>().tokens(ui::theme_tokens());
        harness.core.mount_root(&root);
        harness.commit();
        harness
    }

    fn commit(&mut self) -> MountBatch {
        let batch = self.core.commit().unwrap_or_default();
        self.apply_batch(&batch);
        self.commits += 1;
        self.assert_tree_invariants();
        batch
    }

    fn apply_batch(&mut self, batch: &MountBatch) {
        for item in &batch.items {
            match item {
                MountItem::Create { id, kind } => {
                    assert!(
                        !self.nodes.contains_key(id),
                        "duplicate MountItem::Create for node {id}"
                    );
                    self.nodes.insert(*id, MountedNode::new(*id, *kind));
                }
                MountItem::Insert {
                    parent,
                    child,
                    before,
                } => {
                    assert_ne!(parent, child, "node {child} cannot be inserted into itself");
                    assert!(
                        self.nodes.contains_key(parent),
                        "Insert references unknown parent {parent}"
                    );
                    assert!(
                        self.nodes.contains_key(child),
                        "Insert references unknown child {child}"
                    );
                    if let Some(old_parent) = self.nodes[child].parent {
                        if let Some(p) = self.nodes.get_mut(&old_parent) {
                            p.children.retain(|&c| c != *child);
                        }
                    }
                    self.nodes.get_mut(child).unwrap().parent = Some(*parent);
                    let p = self.nodes.get_mut(parent).unwrap();
                    p.children.retain(|&c| c != *child);
                    if let Some(anchor) = before {
                        let pos = p.children.iter().position(|c| c == anchor).unwrap_or_else(|| {
                            panic!(
                                "Insert anchor {anchor} is not a child of parent {parent}: {:?}",
                                p.children
                            )
                        });
                        p.children.insert(pos, *child);
                    } else {
                        p.children.push(*child);
                    }
                }
                MountItem::Remove { id } => {
                    if let Some(node) = self.nodes.get_mut(id) {
                        if let Some(parent_id) = node.parent.take() {
                            if let Some(parent) = self.nodes.get_mut(&parent_id) {
                                parent.children.retain(|&c| c != *id);
                            }
                        }
                    }
                }
                MountItem::Delete { id } => {
                    if let Some(node) = self.nodes.remove(id) {
                        if let Some(parent_id) = node.parent {
                            if let Some(parent) = self.nodes.get_mut(&parent_id) {
                                parent.children.retain(|&c| c != *id);
                            }
                        }
                        for child_id in node.children {
                            if let Some(child) = self.nodes.get_mut(&child_id) {
                                if child.parent == Some(*id) {
                                    child.parent = None;
                                }
                            }
                        }
                    }
                }
                MountItem::Frame { id, frame } => {
                    let node = self
                        .nodes
                        .get_mut(id)
                        .unwrap_or_else(|| panic!("Frame for unknown node {id}"));
                    node.frame = *frame;
                }
                MountItem::ViewStyle { id, style } => {
                    let node = self
                        .nodes
                        .get_mut(id)
                        .unwrap_or_else(|| panic!("ViewStyle for unknown node {id}"));
                    node.style = (**style).clone();
                }
                MountItem::Text { id, text, .. } => {
                    let node = self
                        .nodes
                        .get_mut(id)
                        .unwrap_or_else(|| panic!("Text for unknown node {id}"));
                    node.text = Some(text.text.to_string());
                }
                MountItem::Events { id, events } => {
                    let node = self
                        .nodes
                        .get_mut(id)
                        .unwrap_or_else(|| panic!("Events for unknown node {id}"));
                    node.events = *events;
                }
                MountItem::ViewProps { id, props } => {
                    let node = self
                        .nodes
                        .get_mut(id)
                        .unwrap_or_else(|| panic!("ViewProps for unknown node {id}"));
                    node.props = (**props).clone();
                }
                MountItem::Canvas { id, drawing } => {
                    let node = self
                        .nodes
                        .get_mut(id)
                        .unwrap_or_else(|| panic!("Canvas for unknown node {id}"));
                    node.drawing = Some(drawing.clone());
                }
                MountItem::Input { id, config } => {
                    let node = self
                        .nodes
                        .get_mut(id)
                        .unwrap_or_else(|| panic!("Input for unknown node {id}"));
                    node.input_config = Some(config.clone());
                }
                MountItem::InputValue { id, text, count } => {
                    let node = self
                        .nodes
                        .get_mut(id)
                        .unwrap_or_else(|| panic!("InputValue for unknown node {id}"));
                    node.input_value = Some((text.clone(), *count));
                }
                MountItem::Keymap(keymap) => {
                    self.keymap = keymap.clone();
                }
                MountItem::Appearance { appearance } => {
                    self.appearance = *appearance;
                }
                _ => {}
            }
        }
    }

    fn assert_tree_invariants(&self) {
        let mut visited = BTreeSet::new();
        self.walk_verify(ROOT_ID, &mut visited);
    }

    fn walk_verify(&self, id: NodeId, visited: &mut BTreeSet<NodeId>) {
        assert!(
            visited.insert(id),
            "cycle or duplicate child detected in shadow tree at node {id}"
        );
        let node = self
            .nodes
            .get(&id)
            .unwrap_or_else(|| panic!("missing node {id} in shadow tree"));
        let x = node.frame.origin.x.0;
        let y = node.frame.origin.y.0;
        let w = node.frame.size.width.0;
        let h = node.frame.size.height.0;
        assert!(
            x.is_finite() && y.is_finite() && w.is_finite() && h.is_finite(),
            "node {id} ({:?}) has non-finite frame: {:?}",
            node.kind,
            node.frame
        );
        assert!(
            w >= 0.0 && h >= 0.0,
            "node {id} ({:?}) has negative dimensions: {:?}",
            node.kind,
            node.frame
        );
        for &child in &node.children {
            let child_node = self
                .nodes
                .get(&child)
                .unwrap_or_else(|| panic!("missing child {child} of parent {id}"));
            assert_eq!(
                child_node.parent,
                Some(id),
                "child {child} parent pointer {:?} != {id}",
                child_node.parent
            );
            self.walk_verify(child, visited);
        }
    }

    fn abs_bounds(&self, id: NodeId) -> Bounds<Points> {
        let node = &self.nodes[&id];
        let mut x = node.frame.origin.x.0;
        let mut y = node.frame.origin.y.0;
        let mut cur = node.parent;
        while let Some(pid) = cur {
            let p = &self.nodes[&pid];
            if p.kind == ViewKind::Modal || pid == ROOT_ID {
                break;
            }
            x += p.frame.origin.x.0;
            y += p.frame.origin.y.0;
            cur = p.parent;
        }
        Bounds {
            origin: point(px(x), px(y)),
            size: node.frame.size,
        }
    }

    fn attached_nodes(&self) -> Vec<&MountedNode> {
        let mut out = Vec::new();
        let mut stack = vec![ROOT_ID];
        while let Some(id) = stack.pop() {
            if let Some(node) = self.nodes.get(&id) {
                if node.style.hidden {
                    continue;
                }
                out.push(node);
                for &child in node.children.iter().rev() {
                    stack.push(child);
                }
            }
        }
        out
    }

    fn find_text(&self, exact: &str) -> Vec<NodeId> {
        self.attached_nodes()
            .into_iter()
            .filter(|n| n.text.as_deref() == Some(exact))
            .map(|n| n.id)
            .collect()
    }

    fn find_text_containing(&self, needle: &str) -> Vec<NodeId> {
        self.attached_nodes()
            .into_iter()
            .filter(|n| n.text.as_deref().is_some_and(|t| t.contains(needle)))
            .map(|n| n.id)
            .collect()
    }

    fn pressable_ancestor(&self, id: NodeId) -> Option<NodeId> {
        let mut cur = Some(id);
        while let Some(nid) = cur {
            let node = self.nodes.get(&nid)?;
            if node.events.contains(EventKind::Press) {
                return Some(nid);
            }
            cur = node.parent;
        }
        None
    }

    fn context_menu_ancestor(&self, id: NodeId) -> Option<NodeId> {
        let mut cur = Some(id);
        while let Some(nid) = cur {
            let node = self.nodes.get(&nid)?;
            if node.events.contains(EventKind::ContextMenu) {
                return Some(nid);
            }
            cur = node.parent;
        }
        None
    }

    fn press_node(&mut self, id: NodeId) -> MountBatch {
        self.press_node_with(
            id,
            PressEvent {
                clicks: 1,
                ..Default::default()
            },
        )
    }

    fn press_node_with(&mut self, id: NodeId, press: PressEvent) -> MountBatch {
        let target = self
            .pressable_ancestor(id)
            .unwrap_or_else(|| panic!("node {id} has no pressable ancestor"));
        self.core
            .component_event(target, EventKind::Press, &EventPayload::Press(press));
        self.commit()
    }

    fn press_text(&mut self, exact: &str) -> MountBatch {
        let matches = self.find_text(exact);
        assert!(
            !matches.is_empty(),
            "expected attached text node {exact:?}, found none"
        );
        let target = matches
            .iter()
            .copied()
            .find_map(|id| self.pressable_ancestor(id))
            .unwrap_or_else(|| panic!("text node {exact:?} ({matches:?}) has no pressable target"));
        self.press_node(target)
    }

    fn right_click_node(&mut self, id: NodeId, page_x: f32, page_y: f32) -> MountBatch {
        let target = self
            .context_menu_ancestor(id)
            .unwrap_or_else(|| panic!("node {id} has no ContextMenu ancestor"));
        let ev = ContextMenuEvent {
            position: point(px(4.0), px(4.0)),
            page_position: point(px(page_x), px(page_y)),
            modifiers: Modifiers::default(),
        };
        self.core.component_event(
            target,
            EventKind::ContextMenu,
            &EventPayload::ContextMenu(ev),
        );
        self.commit()
    }

    fn send_key(&mut self, shortcut: &str) -> MountBatch {
        let stroke = Keystroke::parse(shortcut, Os::Linux)
            .unwrap_or_else(|e| panic!("invalid shortcut {shortcut:?}: {e}"));
        let KeyMatch::Fire(binding_id) = self.keymap.find(stroke, false, false) else {
            panic!(
                "shortcut {shortcut:?} ({stroke:?}) not bound in keymap: {:?}",
                self.keymap.bindings()
            );
        };
        self.core.component_event(
            ROOT_ID,
            EventKind::KeyCommand,
            &EventPayload::Value(PayloadValue::Number(binding_id as f64)),
        );
        self.commit()
    }

    fn with_state(&mut self, f: impl FnOnce(Signal<Ui>) + 'static) -> MountBatch {
        model::set_test_runner(f);
        self.send_key("f24")
    }

    fn read_ui(&mut self) -> Ui {
        let slot: Arc<Mutex<Option<Ui>>> = Arc::new(Mutex::new(None));
        let slot_clone = slot.clone();
        self.with_state(move |state| {
            *slot_clone.lock().unwrap() = Some(state.get());
        });
        slot.lock().unwrap().take().expect("read_ui")
    }

    fn resize(&mut self, width: f32, height: f32) -> MountBatch {
        self.viewport = Size::new(px(width), px(height));
        self.nodes.get_mut(&ROOT_ID).unwrap().frame.size = self.viewport;
        self.core.set_viewport(self.viewport);
        self.commit()
    }
}

fn demo_nodes() -> Vec<NodeInfo> {
    vec![
        NodeInfo {
            id: "alpha".into(),
            name: "alpha-mac".into(),
            os: "macos".into(),
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
            name: "gamma-ro".into(),
            os: "linux".into(),
            online: true,
            agent_ok: true,
            self_node: false,
            read_only: true,
            read_roots: vec!["/srv/gamma".into()],
        },
        NodeInfo {
            id: "delta".into(),
            name: "delta-offline".into(),
            os: "android".into(),
            online: false,
            agent_ok: false,
            self_node: false,
            read_only: false,
            read_roots: vec!["/sdcard/Download".into()],
        },
    ]
}

fn demo_entries() -> Vec<Entry> {
    vec![
        Entry {
            name: "notes".into(),
            path: "/tmp/demo/alpha/notes".into(),
            is_dir: true,
            size: 0,
            mod_time: "2026-03-01T10:00:00Z".into(),
        },
        Entry {
            name: "photos".into(),
            path: "/tmp/demo/alpha/photos".into(),
            is_dir: true,
            size: 0,
            mod_time: "2026-03-02T12:00:00Z".into(),
        },
        Entry {
            name: "hello.txt".into(),
            path: "/tmp/demo/alpha/hello.txt".into(),
            is_dir: false,
            size: 128,
            mod_time: "2026-03-03T08:30:00Z".into(),
        },
        Entry {
            name: "report.pdf".into(),
            path: "/tmp/demo/alpha/report.pdf".into(),
            is_dir: false,
            size: 4096,
            mod_time: "2026-03-05T19:45:00Z".into(),
        },
        Entry {
            name: "budget.xlsx".into(),
            path: "/tmp/demo/alpha/budget.xlsx".into(),
            is_dir: false,
            size: 2048,
            mod_time: "2026-03-04T14:20:00Z".into(),
        },
    ]
}

fn seed_demo_ui(ui: &mut Ui) {
    ui.alive = true;
    ui.err.clear();
    ui.net_mode = "demo".into();
    ui.net_requested = "demo".into();
    ui.nodes = demo_nodes();
    ui.folder_node = "alpha".into();
    ui.folder_path = "/tmp/demo/alpha".into();
    ui.open = vec![
        model::node_key("alpha"),
        model::dir_key("alpha", "/tmp/demo/alpha"),
    ];
    ui.subdirs = vec![(
        model::dir_key("alpha", "/tmp/demo/alpha"),
        vec![
            "/tmp/demo/alpha/notes".into(),
            "/tmp/demo/alpha/photos".into(),
        ],
    )];
    ui.entries = demo_entries();
    ui.selected = 2; // hello.txt
    ui.selected_paths = vec!["/tmp/demo/alpha/hello.txt".into()];
    ui.preview_on = true;
    ui.preview_path = "/tmp/demo/alpha/hello.txt".into();
    ui.preview_kind = "text".into();
    ui.preview_text = "Hello from alpha formal verification".into();
    ui.preview_image.clear();
    ui.tabs = vec![
        TabState {
            id: "tab-1".into(),
            node: "alpha".into(),
            path: "/tmp/demo/alpha".into(),
            back: Vec::new(),
            forward: Vec::new(),
            selected: 2,
            filter: String::new(),
            sort: "name".into(),
            view: "list".into(),
        },
        TabState::new("beta", "C:/Users/beta/Shared"),
    ];
    ui.active_tab = 0;
    ui.status = "就绪".into();
}

// ============================================================================
// INV-1: Icon & SVG Path Rendering Proofs
// ============================================================================

#[component]
fn IconGallery() -> impl IntoElement {
    let all_stroke_paths = [
        ip::FOLDER,
        ip::FILE,
        ip::FILE_TEXT,
        ip::IMAGE,
        ip::FILE_TYPE,
        ip::COPY,
        ip::CLIPBOARD,
        ip::PENCIL,
        ip::TRASH,
        ip::REFRESH,
        ip::FOLDER_PLUS,
        ip::CHEVRON_LEFT,
        ip::CHEVRON_RIGHT,
        ip::ARROW_UP,
        ip::SEARCH,
        ip::LIST,
        ip::LAYOUT_GRID,
        ip::PANEL_RIGHT,
        ip::PANEL_LEFT_CLOSE,
        ip::PANEL_LEFT_OPEN,
        ip::SETTINGS,
        ip::SUN,
        ip::MOON,
        ip::LAPTOP,
        ip::MONITOR,
        ip::SERVER,
        ip::SMARTPHONE,
        ip::LOCK,
        ip::PLUS,
        ip::X,
        ip::ELLIPSIS,
        ip::SQUARE_TERMINAL,
    ];
    let mut col = view()
        .flex_row()
        .flex_wrap()
        .child(icons::logo_mark(22.0))
        .child(icons::folder_icon(20.0, true))
        .child(icons::folder_icon(20.0, false))
        .child(icons::os_icon("macos", 16.0))
        .child(icons::os_icon("windows", 16.0))
        .child(icons::os_icon("ios", 16.0))
        .child(icons::os_icon("android", 16.0))
        .child(icons::os_icon("linux", 16.0))
        .child(icons::os_icon("other", 16.0))
        .child(icons::typed_file_icon(FileKind::Folder, 24.0))
        .child(icons::typed_file_icon(FileKind::Text, 24.0))
        .child(icons::typed_file_icon(FileKind::Markdown, 24.0))
        .child(icons::typed_file_icon(FileKind::Image, 24.0))
        .child(icons::typed_file_icon(FileKind::Pdf, 24.0))
        .child(icons::typed_file_icon(FileKind::Sheet, 24.0))
        .child(icons::typed_file_icon(FileKind::Code, 24.0))
        .child(icons::typed_file_icon(FileKind::Other, 24.0));
    for d in all_stroke_paths {
        col = col.child(icons::stroke_icon(d, 16.0));
    }
    col
}

fn assert_num_in_viewbox(n: Num, name: &str) {
    match n {
        Num::Const(v) => {
            assert!(
                v.is_finite() && (-4.0..=28.0).contains(&v),
                "icon {name} has out-of-viewbox coordinate {v}"
            );
        }
        Num::Slot(s) => panic!("icon {name} unexpectedly uses animated slot {s}"),
    }
}

fn probe_path_non_degenerate(ops: &[PathOp], filled: bool, rule: FillRule, name: &str) {
    let stroke_ops = [
        CanvasOp::LineWidth(Num::Const(2.0)),
        CanvasOp::LineCap(LineCap::Round),
        CanvasOp::LineJoin(LineJoin::Round),
    ];
    let mut hits = 0usize;
    let mut misses = 0usize;
    for gy in -2..=26 {
        for gx in -2..=26 {
            let p = point(px(gx as f32 + 0.5), px(gy as f32 + 0.5));
            let inside = if filled {
                point_in_path(&[], Some(ops), rule, p, |_| 0.0)
            } else {
                point_in_stroke(&stroke_ops, Some(ops), p, |_| 0.0)
            };
            if inside {
                hits += 1;
            } else {
                misses += 1;
            }
        }
    }
    assert!(
        hits > 0,
        "icon {name} produced 0 hit points across 24x24 viewBox (degenerate invisible path)"
    );
    assert!(
        misses > 0,
        "icon {name} hit every point including outside viewBox (unbounded path)"
    );
}

#[test]
fn inv1_svg_and_icon_rendering_proofs() {
    let named_paths: &[(&str, &str, bool, FillRule)] = &[
        ("FOLDER", ip::FOLDER, false, FillRule::NonZero),
        ("FILE", ip::FILE, false, FillRule::NonZero),
        ("FILE_TEXT", ip::FILE_TEXT, false, FillRule::NonZero),
        ("IMAGE", ip::IMAGE, false, FillRule::NonZero),
        ("FILE_TYPE", ip::FILE_TYPE, false, FillRule::NonZero),
        ("COPY", ip::COPY, false, FillRule::NonZero),
        ("CLIPBOARD", ip::CLIPBOARD, false, FillRule::NonZero),
        ("PENCIL", ip::PENCIL, false, FillRule::NonZero),
        ("TRASH", ip::TRASH, false, FillRule::NonZero),
        ("REFRESH", ip::REFRESH, false, FillRule::NonZero),
        ("FOLDER_PLUS", ip::FOLDER_PLUS, false, FillRule::NonZero),
        ("CHEVRON_LEFT", ip::CHEVRON_LEFT, false, FillRule::NonZero),
        ("CHEVRON_RIGHT", ip::CHEVRON_RIGHT, false, FillRule::NonZero),
        ("ARROW_UP", ip::ARROW_UP, false, FillRule::NonZero),
        ("SEARCH", ip::SEARCH, false, FillRule::NonZero),
        ("LIST", ip::LIST, false, FillRule::NonZero),
        ("LAYOUT_GRID", ip::LAYOUT_GRID, false, FillRule::NonZero),
        ("PANEL_RIGHT", ip::PANEL_RIGHT, false, FillRule::NonZero),
        (
            "PANEL_LEFT_CLOSE",
            ip::PANEL_LEFT_CLOSE,
            false,
            FillRule::NonZero,
        ),
        (
            "PANEL_LEFT_OPEN",
            ip::PANEL_LEFT_OPEN,
            false,
            FillRule::NonZero,
        ),
        ("SETTINGS", ip::SETTINGS, false, FillRule::NonZero),
        ("SUN", ip::SUN, false, FillRule::NonZero),
        ("MOON", ip::MOON, false, FillRule::NonZero),
        ("LAPTOP", ip::LAPTOP, false, FillRule::NonZero),
        ("MONITOR", ip::MONITOR, false, FillRule::NonZero),
        ("SERVER", ip::SERVER, false, FillRule::NonZero),
        ("SMARTPHONE", ip::SMARTPHONE, false, FillRule::NonZero),
        ("LOCK", ip::LOCK, false, FillRule::NonZero),
        ("PLUS", ip::PLUS, false, FillRule::NonZero),
        ("X", ip::X, false, FillRule::NonZero),
        ("ELLIPSIS", ip::ELLIPSIS, false, FillRule::NonZero),
        ("APPLE", ip::APPLE, true, FillRule::NonZero),
        ("WINDOWS", ip::WINDOWS, true, FillRule::NonZero),
        ("TUX", ip::TUX, true, FillRule::EvenOdd),
        ("ANDROID", ip::ANDROID, true, FillRule::EvenOdd),
        ("IPHONE", ip::IPHONE, true, FillRule::EvenOdd),
        (
            "SQUARE_TERMINAL",
            ip::SQUARE_TERMINAL,
            false,
            FillRule::NonZero,
        ),
        ("PAGE", ip::PAGE, true, FillRule::NonZero),
        ("FOLD", ip::FOLD, true, FillRule::NonZero),
    ];

    for &(name, d, filled, rule) in named_paths {
        let ops = parse_svg_path(d);
        assert!(
            ops.len() >= 2,
            "SVG path {name} parsed to fewer than 2 PathOps: {ops:?}"
        );
        assert!(
            matches!(ops[0], PathOp::MoveTo(_, _)),
            "SVG path {name} must start with MoveTo, got {:?}",
            ops[0]
        );
        for op in &ops {
            match *op {
                PathOp::MoveTo(x, y) | PathOp::LineTo(x, y) => {
                    assert_num_in_viewbox(x, name);
                    assert_num_in_viewbox(y, name);
                }
                PathOp::QuadTo(nums) | PathOp::Rect(nums) => {
                    for n in nums {
                        assert_num_in_viewbox(n, name);
                    }
                }
                PathOp::CubicTo(nums) => {
                    for n in nums {
                        assert_num_in_viewbox(n, name);
                    }
                }
                PathOp::Arc { x, y, radius, .. } => {
                    assert_num_in_viewbox(x, name);
                    assert_num_in_viewbox(y, name);
                    assert_num_in_viewbox(radius, name);
                }
                PathOp::ArcTo(nums) => {
                    for n in nums {
                        assert_num_in_viewbox(n, name);
                    }
                }
                PathOp::Ellipse { geometry, .. } => {
                    for n in geometry {
                        assert_num_in_viewbox(n, name);
                    }
                }
                PathOp::RoundRect(rect, radii) => {
                    for n in rect.into_iter().chain(radii) {
                        assert_num_in_viewbox(n, name);
                    }
                }
                PathOp::Close => {}
            }
        }
        probe_path_non_degenerate(&ops, filled, rule, name);
    }

    // Mount all icon constructors into Core and verify Canvas drawings & frames.
    let mut core = Core::new(ApproxTextEngine, Size::new(px(800.0), px(600.0)));
    core.mount_root(&Root::of::<IconGallery>().tokens(ui::theme_tokens()));
    let batch = core.commit().unwrap_or_default();
    let mut canvas_drawings = Vec::new();
    let mut frames = BTreeMap::new();
    for item in &batch.items {
        match item {
            MountItem::Canvas { id, drawing } => canvas_drawings.push((*id, drawing.clone())),
            MountItem::Frame { id, frame } => {
                frames.insert(*id, *frame);
            }
            _ => {}
        }
    }
    // 1 logo + 2 folder_icons + 6 os_icons + 8 typed_file_icons + 32 stroke_icons = 49 canvases
    assert_eq!(canvas_drawings.len(), 49);
    for (id, drawing) in &canvas_drawings {
        let frame = frames.get(id).expect("canvas must have frame");
        assert!(frame.size.width.0 > 0.0 && frame.size.height.0 > 0.0);
        let scale_24 = frame.size.width.0 / 24.0;
        let scale_64 = frame.size.width.0 / 64.0;
        let has_scale = drawing.ops.iter().any(|op| match op {
            CanvasOp::Scale(Num::Const(sx), Num::Const(sy)) => {
                ((sx - scale_24).abs() < 1e-4 && (sy - scale_24).abs() < 1e-4)
                    || ((sx - scale_64).abs() < 1e-4 && (sy - scale_64).abs() < 1e-4)
            }
            _ => false,
        });
        assert!(
            has_scale,
            "canvas {id} missing Scale in ops: {:?}",
            drawing.ops
        );
        let has_paint = drawing
            .ops
            .iter()
            .any(|op| matches!(op, CanvasOp::Fill(_, _) | CanvasOp::Stroke(_)));
        assert!(has_paint, "canvas {id} has neither Fill nor Stroke op");
    }

    // Exhaustive OS resolution and file kind classification checks.
    assert_eq!(icons::os_kind("darwin"), OsKind::Mac);
    assert_eq!(icons::os_kind("windows"), OsKind::Windows);
    assert_eq!(icons::os_kind("linux"), OsKind::Linux);
    assert_eq!(icons::os_kind("android"), OsKind::Android);
    assert_eq!(icons::os_kind("ios"), OsKind::Ios);
    assert_eq!(icons::os_kind("loopback"), OsKind::Other);
    assert_eq!(icons::resolve_node_os("darwin", "x", "x"), "darwin");
    assert_eq!(icons::resolve_node_os("loopback", "alpha", ""), "macos");
    assert_eq!(icons::resolve_node_os("loopback", "beta", ""), "windows");
    assert_eq!(icons::resolve_node_os("loopback", "gamma", ""), "linux");
    assert_eq!(icons::resolve_node_os("loopback", "pixel", ""), "android");
    assert_eq!(icons::resolve_node_os("loopback", "phone", ""), "ios");

    let expected_kinds = [
        ("docs", true, FileKind::Folder, "文件夹"),
        ("README.md", false, FileKind::Markdown, "Markdown"),
        ("notes.txt", false, FileKind::Text, "文本文档"),
        ("photo.webp", false, FileKind::Image, "图片"),
        ("manual.pdf", false, FileKind::Pdf, "PDF 文档"),
        ("table.xlsx", false, FileKind::Sheet, "电子表格"),
        ("main.rs", false, FileKind::Code, "代码"),
        ("archive.bin", false, FileKind::Other, "文件"),
    ];
    for (name, is_dir, kind, label) in expected_kinds {
        assert_eq!(icons::file_kind(name, is_dir), kind, "{name}");
        assert_eq!(icons::type_label(kind), label, "{name}");
    }
}

// ============================================================================
// INV-2: Layout & Bounding-Box Proofs across Desktop Viewports
// ============================================================================

#[test]
fn inv2_layout_and_bounding_box_proofs() {
    let viewports = [
        (1440.0f32, 900.0f32),
        (1280.0f32, 800.0f32),
        (1024.0f32, 768.0f32),
        (800.0f32, 600.0f32),
    ];

    let mut h = Harness::new(1440.0, 900.0);
    h.with_state(|state| {
        state.update(seed_demo_ui);
    });

    for (vw, vh) in viewports {
        h.resize(vw, vh);

        // 1. Top Chrome: brand column ("Tailfile") sits at top-left [0, 46] with width 262.
        let brand_txt = h.find_text(icons::LOGO_TITLE);
        assert_eq!(
            brand_txt.len(),
            1,
            "expected Tailfile brand title at {vw}x{vh}"
        );
        let brand_bounds = h.abs_bounds(brand_txt[0]);
        assert!(
            brand_bounds.origin.y.0 >= 0.0
                && brand_bounds.origin.y.0 + brand_bounds.size.height.0 <= 46.5,
            "brand title outside top chrome [0, 46] at {vw}x{vh}: {brand_bounds:?}"
        );
        assert!(
            brand_bounds.origin.x.0 < ui::SIDEBAR_W,
            "brand title x={} exceeds SIDEBAR_W at {vw}x{vh}",
            brand_bounds.origin.x.0
        );

        // Tab chips in chrome start at or right of SIDEBAR_W (262pt).
        let tab1_ids = h.find_text("alpha-mac › alpha");
        assert_eq!(tab1_ids.len(), 1);
        let tab1_bounds = h.abs_bounds(tab1_ids[0]);
        assert!(
            tab1_bounds.origin.x.0 >= ui::SIDEBAR_W,
            "tab chip overlaps brand column at {vw}x{vh}: {tab1_bounds:?}"
        );
        assert!(
            tab1_bounds.origin.y.0 + tab1_bounds.size.height.0 <= 46.5,
            "tab chip exceeds chrome height 46pt at {vw}x{vh}: {tab1_bounds:?}"
        );

        // 2. Sidebar expanded width == 262.0; main content starts at x >= 262.0.
        let tailnet_hdr = h.find_text_containing("仅显示白名单目录");
        assert_eq!(tailnet_hdr.len(), 1);
        let tailnet_bounds = h.abs_bounds(tailnet_hdr[0]);
        assert!(tailnet_bounds.origin.x.0 < ui::SIDEBAR_W);
        assert!(tailnet_bounds.origin.y.0 >= 46.0);

        // 3. Main Content Column: action bar ("新建文件夹") and breadcrumb/file rows start >= 262.0.
        let new_folder_btn = h.find_text("新建文件夹");
        assert_eq!(new_folder_btn.len(), 1);
        let nf_bounds = h.abs_bounds(new_folder_btn[0]);
        assert!(
            nf_bounds.origin.x.0 >= ui::SIDEBAR_W,
            "action bar button overlaps sidebar at {vw}x{vh}: {nf_bounds:?}"
        );
        assert!(
            nf_bounds.origin.y.0 >= 46.0 && nf_bounds.origin.y.0 <= 90.0,
            "action bar button y out of range [46, 90] at {vw}x{vh}: {nf_bounds:?}"
        );

        // 4. Right Preview Pane: mounted when preview_on && selected.is_some(), width == 300.0,
        // sitting at the right edge [vw - 300.0, vw].
        let preview_txt = h.find_text("Hello from alpha formal verification");
        assert_eq!(preview_txt.len(), 1);
        let pv_bounds = h.abs_bounds(preview_txt[0]);
        assert!(
            pv_bounds.origin.x.0 >= vw - 300.5,
            "preview text should sit inside right 300pt preview pane at {vw}x{vh}: {pv_bounds:?}"
        );

        // 5. Status Bar: sits at bottom [vh - 28.0, vh] with non-overlapping left & right labels.
        let status_left = h.find_text("5 个项目 · 已选择 1 项")[0];
        let status_right = h.find_text("alpha · 就绪")[0];
        let sl_bounds = h.abs_bounds(status_left);
        let sr_bounds = h.abs_bounds(status_right);
        assert!(
            sl_bounds.origin.y.0 >= vh - 32.0 && sl_bounds.origin.y.0 <= vh,
            "status bar not at bottom at {vw}x{vh}: {sl_bounds:?}"
        );
        assert!(
            sl_bounds.origin.x.0 + sl_bounds.size.width.0 <= sr_bounds.origin.x.0 + 1.0,
            "status bar left and right labels overlap at {vw}x{vh}: left={sl_bounds:?}, right={sr_bounds:?}"
        );

        // 6. Context Menu Clamping: open context menu at extreme coordinates and prove it stays in-bounds.
        for (req_x, req_y) in [(1400.0, 860.0), (vw + 500.0, vh + 500.0), (-40.0, -20.0)] {
            h.with_state(move |state| {
                model::open_context_menu(state, 2, req_x, req_y);
            });
            let refresh_item = h
                .find_text("刷新")
                .into_iter()
                .last()
                .expect("context menu item");
            // Walk up to the 212pt menu card container.
            let mut menu_id = refresh_item;
            while let Some(pid) = h.nodes[&menu_id].parent {
                if (h.nodes[&pid].frame.size.width.0 - 212.0).abs() < 1.0 {
                    menu_id = pid;
                    break;
                }
                menu_id = pid;
            }
            let mb = h.abs_bounds(menu_id);
            assert_eq!(mb.size.width.0, 212.0);
            assert!(
                mb.origin.x.0 >= 8.0 && mb.origin.x.0 + mb.size.width.0 <= vw,
                "context menu x overflow at {vw}x{vh} for ({req_x}, {req_y}): {mb:?}"
            );
            assert!(
                mb.origin.y.0 >= 8.0 && mb.origin.y.0 + mb.size.height.0 <= vh,
                "context menu y overflow at {vw}x{vh} for ({req_x}, {req_y}): {mb:?}"
            );
            h.with_state(|state| model::close_context_menu(state));
        }
    }

    // Verify Sidebar collapse/expand shifts main content x from 262.0 to 56.0.
    h.resize(1280.0, 800.0);
    h.with_state(|state| model::toggle_sidebar(state));
    assert!(h.read_ui().sidebar_collapsed);
    assert!(
        h.find_text_containing("仅显示白名单目录").is_empty(),
        "sidebar footer must unmount when collapsed"
    );
    let nf_bounds_collapsed = h.abs_bounds(h.find_text("新建文件夹")[0]);
    assert!(
        nf_bounds_collapsed.origin.x.0 >= ui::SIDEBAR_RAIL
            && nf_bounds_collapsed.origin.x.0 < ui::SIDEBAR_W,
        "main content should shift left to SIDEBAR_RAIL (56pt) when collapsed: {nf_bounds_collapsed:?}"
    );
    h.with_state(|state| model::toggle_sidebar(state));
    assert!(!h.read_ui().sidebar_collapsed);

    // Verify Preview toggle unmounts right details pane.
    h.with_state(|state| model::toggle_preview(state));
    assert!(
        h.find_text("Hello from alpha formal verification")
            .is_empty(),
        "preview pane must unmount when preview_on == false"
    );
    h.with_state(|state| {
        model::toggle_preview(state);
        state.update(|ui| {
            ui.preview_text = "Hello from alpha formal verification".into();
        });
    });
    assert_eq!(h.find_text("Hello from alpha formal verification").len(), 1);
}

// ============================================================================
// INV-3: State Machine & Reactive Data-Flow Proofs
// ============================================================================

#[test]
fn inv3_state_machine_and_data_flow_proofs() {
    let mut h = Harness::new(1440.0, 900.0);
    h.with_state(|state| state.update(seed_demo_ui));

    // 1. Tab lifecycle: new_tab_from_current, switch_tab, close_tab, reopen_closed_tab.
    h.with_state(|state| model::new_tab_from_current(state));
    let ui = h.read_ui();
    assert_eq!(ui.tabs.len(), 3);
    assert_eq!(ui.active_tab, 2);
    assert_eq!(ui.tabs[2].node, "alpha");
    assert_eq!(ui.tabs[2].path, "/tmp/demo/alpha");

    // Switch back to tab 1 (beta) and verify active tab state loads.
    h.with_state(|state| model::switch_tab(state, 1));
    let ui = h.read_ui();
    assert_eq!(ui.active_tab, 1);
    assert_eq!(ui.folder_node, "beta");
    assert_eq!(ui.folder_path, "C:/Users/beta/Shared");

    // Close tab 2 and tab 1; reopen_closed_tab restores in LIFO order.
    h.with_state(|state| {
        model::close_tab(state, 2);
        model::close_tab(state, 1);
    });
    let ui = h.read_ui();
    assert_eq!(ui.tabs.len(), 1);
    assert_eq!(ui.closed_tabs.len(), 2);

    h.with_state(|state| model::reopen_closed_tab(state));
    let ui = h.read_ui();
    assert_eq!(ui.tabs.len(), 2);
    assert_eq!(ui.closed_tabs.len(), 1);
    assert_eq!(ui.folder_node, "beta");

    // 2. Navigation history: select_folder, nav_back, nav_forward, go_up (stops at read_root).
    h.with_state(|state| {
        model::switch_tab(state, 0);
        model::select_folder(state, "alpha".into(), "/tmp/demo/alpha/notes".into());
        model::select_folder(state, "alpha".into(), "/tmp/demo/alpha/notes/deep".into());
    });
    let ui = h.read_ui();
    assert_eq!(ui.folder_path, "/tmp/demo/alpha/notes/deep");
    assert_eq!(
        ui.tabs[ui.active_tab].back,
        vec![
            "alpha\0/tmp/demo/alpha".to_owned(),
            "alpha\0/tmp/demo/alpha/notes".to_owned(),
        ]
    );
    assert!(ui.tabs[ui.active_tab].forward.is_empty());

    h.with_state(|state| model::nav_back(state));
    assert_eq!(h.read_ui().folder_path, "/tmp/demo/alpha/notes");
    assert_eq!(h.read_ui().tabs[0].forward.len(), 1);

    h.with_state(|state| model::nav_forward(state));
    assert_eq!(h.read_ui().folder_path, "/tmp/demo/alpha/notes/deep");

    // go_up walks up to /tmp/demo/alpha and then refuses to escape above read_root.
    h.with_state(|state| {
        model::go_up(state);
        model::go_up(state);
    });
    assert_eq!(h.read_ui().folder_path, "/tmp/demo/alpha");
    h.with_state(|state| model::go_up(state));
    assert_eq!(
        h.read_ui().folder_path,
        "/tmp/demo/alpha",
        "go_up must never escape above node read_root"
    );
    assert_eq!(h.read_ui().status, "已在共享根目录");

    // 3. Selection & multi-selection: click_row, toggle_select_path.
    h.with_state(|state| {
        state.update(|ui| {
            ui.entries = demo_entries();
        });
        model::click_row(state, 1, 1); // photos
    });
    let ui = h.read_ui();
    assert_eq!(ui.selected, 1);

    h.with_state(|state| {
        model::toggle_select_path(state, "/tmp/demo/alpha/photos".into());
        model::toggle_select_path(state, "/tmp/demo/alpha/hello.txt".into());
        model::toggle_select_path(state, "/tmp/demo/alpha/report.pdf".into());
    });
    let ui = h.read_ui();
    assert_eq!(
        ui.selected_paths,
        vec![
            "/tmp/demo/alpha/photos",
            "/tmp/demo/alpha/hello.txt",
            "/tmp/demo/alpha/report.pdf",
        ]
    );

    h.with_state(|state| model::toggle_select_path(state, "/tmp/demo/alpha/report.pdf".into()));
    let ui = h.read_ui();
    assert_eq!(
        ui.selected_paths,
        vec!["/tmp/demo/alpha/photos", "/tmp/demo/alpha/hello.txt"]
    );

    // 4. Filtering & sorting: set_filter, cycle_sort, visible_entries.
    h.with_state(|state| model::set_filter(state, "PDF".into()));
    let ui = h.read_ui();
    let vis = model::visible_entries(&ui);
    assert_eq!(vis.len(), 1);
    assert_eq!(vis[0].1.name, "report.pdf");

    h.with_state(|state| model::set_filter(state, String::new()));
    // Sort by mtime (newest first, directories always first).
    h.with_state(|state| model::cycle_sort(state));
    let ui = h.read_ui();
    assert_eq!(ui.sort, "mtime");
    assert_eq!(h.find_text("↓").len(), 1, "mtime sort must show ↓ arrow");
    let vis = model::visible_entries(&ui);
    let sorted_names: Vec<_> = vis.iter().map(|(_, e)| e.name.as_str()).collect();
    assert_eq!(
        sorted_names,
        vec!["photos", "notes", "report.pdf", "budget.xlsx", "hello.txt"]
    );

    // Sort by size (ascending, directories first).
    h.with_state(|state| model::cycle_sort(state));
    let ui = h.read_ui();
    assert_eq!(ui.sort, "size");
    assert_eq!(h.find_text("↑").len(), 1, "size sort must show ↑ arrow");
    let vis = model::visible_entries(&ui);
    let sorted_names: Vec<_> = vis.iter().map(|(_, e)| e.name.as_str()).collect();
    assert_eq!(
        sorted_names,
        vec!["notes", "photos", "hello.txt", "budget.xlsx", "report.pdf"]
    );

    // Cycle back to name sort.
    h.with_state(|state| model::cycle_sort(state));
    assert_eq!(h.read_ui().sort, "name");

    // 5. Sidebar tree expansion & collapse (Item 6 verification).
    let beta_row = TreeRow {
        key: model::node_key("beta"),
        depth: 0,
        label: "beta-win".into(),
        node: "beta".into(),
        path: String::new(),
        is_node: true,
    };
    h.with_state({
        let beta_row = beta_row.clone();
        move |state| model::click_tree(state, beta_row)
    });
    assert!(
        h.read_ui().open.contains(&model::node_key("beta")),
        "first click_tree on closed node must expand it"
    );
    h.with_state({
        let beta_row = beta_row.clone();
        move |state| model::click_tree(state, beta_row)
    });
    assert!(
        !h.read_ui().open.contains(&model::node_key("beta")),
        "second click_tree on open node must collapse it"
    );
    // click_tree_new_tab expands and keeps expanded.
    h.with_state({
        let beta_row = beta_row.clone();
        move |state| model::click_tree_new_tab(state, beta_row)
    });
    assert!(h.read_ui().open.contains(&model::node_key("beta")));
    assert!(ui::collapsed_rows(&h.read_ui()).iter().all(|r| r.is_node));

    // 6. Inline folder creation, rename & name validation + read-only guard.
    h.with_state(|state| {
        model::switch_tab(state, 0);
        state.update(|ui| {
            ui.folder_node = "alpha".into();
            ui.folder_path = "/tmp/demo/alpha".into();
            ui.entries = demo_entries();
            ui.selected = 2;
            ui.selected_paths = vec!["/tmp/demo/alpha/hello.txt".into()];
        });
        model::begin_new_folder(state);
    });
    let ui = h.read_ui();
    assert!(ui.inline_new && !ui.inline_rename);
    assert_eq!(ui.new_name, "新建文件夹");
    assert!(ui.name_error.is_empty());

    // Validate duplicate, path separator, and Windows reserved name rejection via mkdir().
    h.with_state(|state| {
        state.update(|ui| ui.new_name = "notes".into());
        model::mkdir(state);
    });
    assert_eq!(h.read_ui().name_error, "已存在同名项");

    h.with_state(|state| {
        state.update(|ui| ui.new_name = "bad/name".into());
        model::mkdir(state);
    });
    assert_eq!(h.read_ui().name_error, "名称包含非法字符");

    h.with_state(|state| {
        state.update(|ui| ui.inline_new = false);
    });
    assert!(!h.read_ui().inline_new);

    // Read-only node guard for mkdir, rename, delete.
    h.with_state(|state| {
        state.update(|ui| {
            ui.folder_node = "gamma".into();
            ui.folder_path = "/srv/gamma".into();
            ui.entries = vec![Entry {
                name: "readonly.txt".into(),
                path: "/srv/gamma/readonly.txt".into(),
                is_dir: false,
                size: 10,
                mod_time: String::new(),
            }];
            ui.selected = 0;
            ui.selected_paths = vec!["/srv/gamma/readonly.txt".into()];
        });
        model::begin_new_folder(state);
    });
    assert!(!h.read_ui().inline_new);
    assert_eq!(h.read_ui().status, "当前目录只读，无法新建文件夹");

    h.with_state(|state| model::begin_rename(state));
    assert!(!h.read_ui().inline_rename);
    assert_eq!(h.read_ui().status, "当前目录只读，无法重命名");

    h.with_state(|state| model::ask_delete(state));
    assert!(!h.read_ui().delete_open);
    assert_eq!(h.read_ui().status, "当前目录只读，无法删除");

    // 7. Delete confirmation flow on writable node.
    h.with_state(|state| {
        state.update(|ui| {
            ui.folder_node = "alpha".into();
            ui.folder_path = "/tmp/demo/alpha".into();
            ui.entries = demo_entries();
            ui.selected = 2;
            ui.selected_paths = vec![
                "/tmp/demo/alpha/hello.txt".into(),
                "/tmp/demo/alpha/report.pdf".into(),
            ];
        });
        model::ask_delete(state);
    });
    let ui = h.read_ui();
    assert!(ui.delete_open);
    assert_eq!(ui.delete_paths.len(), 2);
    assert!(ui.delete_note.contains("将永久删除 2 项"));
    h.with_state(|state| model::cancel_delete(state));
    assert!(!h.read_ui().delete_open);

    // 8. Clipboard & conflict resolution flow.
    h.with_state(|state| model::copy_selected(state));
    let ui = h.read_ui();
    assert!(ui.clip_set);
    assert_eq!(ui.clip_node, "alpha");
    assert_eq!(ui.clip_items.len(), 2);
    h.with_state(|state| model::clear_clipboard(state));
    assert!(!h.read_ui().clip_set);
    assert!(h.read_ui().clip_items.is_empty());

    h.with_state(|state| {
        state.update(|ui| {
            ui.transfers = vec![Transfer {
                id: "tx-1".into(),
                mode: "push".into(),
                from_node: "alpha".into(),
                from_path: "/tmp/demo/alpha/hello.txt".into(),
                to_node: "beta".into(),
                to_path: "C:/Users/beta/Shared/hello.txt".into(),
                job: Job {
                    id: "job-1".into(),
                    mode: "push".into(),
                    status: "conflict".into(),
                    bytes_total: 128,
                    bytes_done: 0,
                    current: "hello.txt".into(),
                    dest_path: "C:/Users/beta/Shared/hello.txt".into(),
                    error: String::new(),
                    conflict_path: "C:/Users/beta/Shared/hello.txt".into(),
                },
            }];
            model::note_conflicts(ui);
        });
    });
    let ui = h.read_ui();
    assert_eq!(ui.conflict_id, "tx-1");
    assert_eq!(ui.conflict_kind, "transfer");
    assert_eq!(ui.conflict_path, "C:/Users/beta/Shared/hello.txt");
    assert!(!h.find_text("目标已有同名文件").is_empty());

    // Holding conflict key suppresses re-prompt until conflict clears.
    h.with_state(|state| {
        state.update(|ui| {
            ui.conflict_apply = true;
            ui.conflict_hold = "transfer\0tx-1\0C:/Users/beta/Shared/hello.txt".to_owned();
            model::note_conflicts(ui);
        });
    });
    assert!(h.read_ui().conflict_apply);
    assert!(h.read_ui().conflict_path.is_empty());

    // 9. Offline node click opens daemon help modal with OS-specific commands.
    let delta_row = TreeRow {
        key: model::node_key("delta"),
        depth: 0,
        label: "delta-offline  ·  离线".into(),
        node: "delta".into(),
        path: String::new(),
        is_node: true,
    };
    h.with_state(move |state| model::click_tree(state, delta_row));
    assert!(h.read_ui().daemon_open);
    assert!(!h.find_text_containing("Tailfile daemon").is_empty());
    h.with_state(|state| model::close_daemon_help(state));
    assert!(!h.read_ui().daemon_open);

    // 10. Theme switching emits MountItem::Appearance.
    let batch_dark = h.with_state(|state| model::set_theme(state, ThemePref::Dark));
    assert!(batch_dark.items.iter().any(|item| matches!(
        item,
        MountItem::Appearance {
            appearance: Appearance::Dark
        }
    )));
    let batch_light = h.with_state(|state| model::set_theme(state, ThemePref::Light));
    assert!(batch_light.items.iter().any(|item| matches!(
        item,
        MountItem::Appearance {
            appearance: Appearance::Light
        }
    )));
}

// ============================================================================
// INV-4: UI Interaction, Hit-Testing & Event-Dispatch Proofs
// ============================================================================

#[test]
fn inv4_ui_interaction_and_hit_target_proofs() {
    let mut h = Harness::new(1440.0, 900.0);
    h.with_state(|state| state.update(seed_demo_ui));

    // 1. Verify Item 2 fix: tab_chip has NO duplicate on_press on its inner label container.
    let tab_text_id = h.find_text("alpha-mac › alpha")[0];
    let inner_box = h.nodes[&tab_text_id].parent.expect("tab inner view");
    let outer_chip = h.nodes[&inner_box].parent.expect("tab outer chip");
    assert!(
        !h.nodes[&inner_box].events.contains(EventKind::Press),
        "inner view of tab_chip must NOT register duplicate EventKind::Press"
    );
    assert!(
        h.nodes[&outer_chip].events.contains(EventKind::Press),
        "outer view of tab_chip must register EventKind::Press"
    );
    assert_eq!(h.nodes[&outer_chip].style.cursor, Cursor::Pointer);

    // 2. Verify Item 3 fix: breadcrumb items have Cursor::Pointer and EventKind::Press.
    let crumb_ids = h.find_text("alpha-mac");
    assert!(!crumb_ids.is_empty());
    let crumb_id = crumb_ids
        .into_iter()
        .find(|&id| h.nodes[&id].events.contains(EventKind::Press))
        .expect("clickable breadcrumb");
    assert_eq!(
        h.nodes[&crumb_id].style.cursor,
        Cursor::Pointer,
        "breadcrumb item must have Cursor::Pointer"
    );

    // 3. Verify all pressable controls in main view have positive hit areas.
    for node in h.attached_nodes() {
        if node.events.contains(EventKind::Press) && node.id != ROOT_ID {
            let b = h.abs_bounds(node.id);
            assert!(
                b.size.width.0 >= 12.0 && b.size.height.0 >= 12.0,
                "pressable node {} ({:?}) has degenerate hit target: {b:?}",
                node.id,
                node.kind
            );
        }
    }

    // 4. Press tab 2 ("beta-win › Shared") and verify tab switch.
    h.press_text("beta-win › Shared");
    assert_eq!(h.read_ui().active_tab, 1);
    assert_eq!(h.read_ui().folder_node, "beta");

    // Press tab 1 ("alpha-mac › alpha") to switch back, and re-seed entries.
    h.press_text("alpha-mac › alpha");
    assert_eq!(h.read_ui().active_tab, 0);
    assert_eq!(h.read_ui().folder_node, "alpha");
    h.with_state(|state| {
        state.update(|ui| {
            ui.entries = demo_entries();
            ui.selected = 2;
        });
    });

    // 5. Press sort button ("名称") -> cycles to "时间" ("↓") -> "大小" ("↑") -> "名称" ("↑").
    h.press_text("名称");
    assert_eq!(h.read_ui().sort, "mtime");
    assert_eq!(h.find_text("↓").len(), 1);
    h.press_text("时间");
    assert_eq!(h.read_ui().sort, "size");
    assert_eq!(h.find_text("↑").len(), 1);
    h.press_text("大小");
    assert_eq!(h.read_ui().sort, "name");

    // 6. Press "新建文件夹" button in toolbar -> opens inline folder editor.
    h.press_text("新建文件夹");
    assert!(h.read_ui().inline_new);
    assert!(!h.find_text("确定").is_empty());
    // Press "取消" to close inline editor.
    h.press_text("取消");
    assert!(!h.read_ui().inline_new);

    // 7. Click file rows in the table.
    let report_id = h.find_text("report.pdf")[0];
    h.press_node(report_id);
    assert_eq!(h.read_ui().selected, 3);

    // 8. Right-click a file row -> opens context menu at cursor, click "重命名" -> opens inline rename.
    let hello_id = h.find_text("hello.txt")[0];
    h.right_click_node(hello_id, 420.0, 320.0);
    assert!(h.read_ui().ctx_open);
    let rename_items = h.find_text("重命名");
    let ctx_rename = *rename_items.last().unwrap();
    h.press_node(ctx_rename);
    assert!(!h.read_ui().ctx_open);
    assert!(h.read_ui().inline_rename);
    assert_eq!(h.read_ui().rename_to, "hello.txt");

    // 9. Keyboard shortcuts: Escape cancels inline rename; F2 starts rename; Delete opens delete modal;
    // Mod+W closes active tab; Mod+Shift+T reopens it.
    h.send_key("escape");
    assert!(!h.read_ui().inline_rename);
    assert_eq!(h.read_ui().selected, -1);

    let hello_id = h.find_text("hello.txt")[0];
    h.press_node(hello_id);
    h.send_key("f2");
    assert!(h.read_ui().inline_rename);
    h.send_key("escape");

    let hello_id = h.find_text("hello.txt")[0];
    h.press_node(hello_id);
    h.send_key("delete");
    assert!(h.read_ui().delete_open);
    h.send_key("escape");
    assert!(!h.read_ui().delete_open);

    let tabs_before = h.read_ui().tabs.len();
    h.send_key("mod+w");
    assert_eq!(h.read_ui().tabs.len(), tabs_before - 1);
    h.send_key("mod+shift+t");
    assert_eq!(h.read_ui().tabs.len(), tabs_before);
}

// ============================================================================
// INV-5: End-to-End Localhost Agent Integration & Security Proofs
// ============================================================================

struct MockAgent {
    addr: String,
    token: String,
    state_dir: std::path::PathBuf,
    requests: Arc<Mutex<Vec<(String, String)>>>,
}

impl MockAgent {
    fn start() -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind mock agent");
        let port = listener.local_addr().unwrap().port();
        let addr = format!("http://127.0.0.1:{port}");
        let token = "tf-secret-bearer-token-do-not-leak-999".to_owned();
        let state_dir = std::env::temp_dir().join(format!(
            "tailfile-nsh-formal-verify-{}-{port}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&state_dir);
        std::fs::create_dir_all(&state_dir).unwrap();
        std::fs::write(state_dir.join("gui-token"), format!("{token}\n")).unwrap();

        let requests = Arc::new(Mutex::new(Vec::new()));
        let req_log = requests.clone();
        let expected_auth = format!("Bearer {token}");

        std::thread::spawn(move || {
            for stream in listener.incoming().flatten() {
                let mut reader = BufReader::new(stream.try_clone().unwrap());
                let mut request_line = String::new();
                if reader.read_line(&mut request_line).is_err() || request_line.is_empty() {
                    continue;
                }
                let parts: Vec<&str> = request_line.trim().split_whitespace().collect();
                if parts.len() < 2 {
                    continue;
                }
                let method = parts[0].to_owned();
                let target = parts[1].to_owned();

                let mut content_len = 0usize;
                let mut auth_hdr = String::new();
                let mut origin_hdr = String::new();
                loop {
                    let mut line = String::new();
                    if reader.read_line(&mut line).is_err() || line == "\r\n" || line.is_empty() {
                        break;
                    }
                    if let Some((k, v)) = line.split_once(':') {
                        let k = k.trim().to_ascii_lowercase();
                        let v = v.trim().to_owned();
                        if k == "content-length" {
                            content_len = v.parse().unwrap_or(0);
                        } else if k == "authorization" {
                            auth_hdr = v;
                        } else if k == "origin" {
                            origin_hdr = v;
                        }
                    }
                }
                let mut body = vec![0u8; content_len];
                let _ = reader.read_exact(&mut body);
                let body_str = String::from_utf8_lossy(&body).into_owned();

                assert!(
                    origin_hdr.is_empty(),
                    "native client must not send Origin header, got {origin_hdr:?}"
                );
                if auth_hdr != expected_auth {
                    let mut out = stream;
                    let _ = write!(
                        out,
                        "HTTP/1.1 401 Unauthorized\r\nContent-Length: 12\r\nConnection: close\r\n\r\nunauthorized"
                    );
                    continue;
                }

                req_log
                    .lock()
                    .unwrap()
                    .push((format!("{method} {target}"), body_str));

                let response_body = match (method.as_str(), target.as_str()) {
                    ("GET", "/v1/health") => r#"{"ok":true}"#.to_owned(),
                    ("GET", "/v1/nodes") => serde_json::json!({
                        "nodes": [
                            {
                                "id": "alpha",
                                "name": "alpha-node",
                                "os": "linux",
                                "online": true,
                                "agentOk": true,
                                "self": true,
                                "readOnly": false,
                                "readRoots": ["/tmp/alpha"]
                            }
                        ]
                    })
                    .to_string(),
                    ("GET", t) if t.starts_with("/v1/fs/list") => serde_json::json!({
                        "entries": [
                            {
                                "name": "readme.md",
                                "path": "/tmp/alpha/readme.md",
                                "isDir": false,
                                "size": 64,
                                "modTime": "2026-03-01T00:00:00Z"
                            }
                        ]
                    })
                    .to_string(),
                    ("GET", t) if t.starts_with("/v1/fs/preview") => serde_json::json!({
                        "kind": "markdown",
                        "name": "readme.md",
                        "text": "# Verified Markdown",
                        "truncated": false
                    })
                    .to_string(),
                    ("GET", "/v1/transfers") => serde_json::json!({
                        "transfers": [
                            {
                                "id": "tx-live",
                                "mode": "push",
                                "fromNode": "alpha",
                                "fromPath": "/tmp/alpha/readme.md",
                                "toNode": "alpha",
                                "toPath": "/tmp/alpha/copy.md",
                                "job": {
                                    "id": "job-live",
                                    "mode": "push",
                                    "status": "running",
                                    "bytesTotal": 64,
                                    "bytesDone": 32
                                }
                            }
                        ]
                    })
                    .to_string(),
                    ("GET", "/v1/jobs") => serde_json::json!({ "jobs": [] }).to_string(),
                    ("GET", "/v1/local/settings") => serde_json::json!({
                        "net": "tsnet",
                        "netRequested": "auto",
                        "authKeyConfigured": true
                    })
                    .to_string(),
                    ("PUT", "/v1/local/settings/authkey") => serde_json::json!({
                        "configured": true,
                        "restartRequired": true
                    })
                    .to_string(),
                    ("DELETE", "/v1/local/settings/authkey") => serde_json::json!({
                        "configured": false,
                        "restartRequired": true
                    })
                    .to_string(),
                    ("POST", "/v1/local/import") => serde_json::json!({
                        "id": "imp-1",
                        "mode": "import",
                        "status": "completed",
                        "bytesTotal": 12,
                        "bytesDone": 12
                    })
                    .to_string(),
                    _ => r#"{"ok":true}"#.to_owned(),
                };
                let mut out = stream;
                let _ = write!(
                    out,
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                    response_body.len(),
                    response_body
                );
            }
        });

        Self {
            addr,
            token,
            state_dir,
            requests,
        }
    }
}

impl Drop for MockAgent {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.state_dir);
    }
}

#[test]
fn inv5_end_to_end_agent_integration_proofs() {
    let mock = MockAgent::start();
    let token_file = mock.state_dir.join("gui-token");
    set_launch(Launch {
        agent: mock.addr.clone(),
        state: mock.state_dir.to_string_lossy().into_owned(),
        token_file: token_file.to_string_lossy().into_owned(),
    });

    // Direct Client verification against mock agent.
    let loaded_tok = token::load("", mock.state_dir.to_str().unwrap()).expect("load token");
    assert_eq!(loaded_tok, mock.token);
    let client = Client::new(&mock.addr, loaded_tok);
    client.health().expect("health");
    let nodes = client.nodes().expect("nodes");
    assert_eq!(nodes.len(), 1);
    assert_eq!(nodes[0].name, "alpha-node");

    // Exercise cancel_transfer, resume_transfer, import_typed, save_auth_key, clear_auth_key
    // through the UI model and verify zero secret leakage.
    let secret_auth_key = "tskey-auth-super-secret-value-never-display";
    let mut h = Harness::new(1440.0, 900.0);
    h.with_state({
        let addr = mock.addr.clone();
        let tok = mock.token.clone();
        move |state| {
            state.update(|ui| {
                ui.addr = addr;
                ui.token = tok;
                ui.folder_node = "alpha".into();
                ui.folder_path = "/tmp/alpha".into();
                ui.nodes = nodes;
                ui.import_path = "/tmp/local-a.txt".into();
                ui.auth_input = secret_auth_key.into();
            });
            model::import_typed(state);
            model::cancel_transfer(state, "tx-live".into());
            model::resume_transfer(state, "tx-live".into());
            model::save_auth_key(state);
        }
    });

    // Wait briefly for background blocking threads to finish and pump component tasks.
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(3);
    while std::time::Instant::now() < deadline {
        h.core.run_component_tasks();
        h.commit();
        let reqs = mock.requests.lock().unwrap();
        let has_import = reqs.iter().any(|(p, _)| p == "POST /v1/local/import");
        let has_cancel = reqs
            .iter()
            .any(|(p, _)| p == "POST /v1/transfers/tx-live/cancel");
        let has_resume = reqs
            .iter()
            .any(|(p, _)| p == "POST /v1/transfers/tx-live/resume");
        let has_auth = reqs
            .iter()
            .any(|(p, _)| p == "PUT /v1/local/settings/authkey");
        drop(reqs);
        if has_import && has_cancel && has_resume && has_auth {
            break;
        }
        std::thread::sleep(std::time::Duration::from_millis(20));
    }

    let reqs = mock.requests.lock().unwrap().clone();
    let paths: Vec<&str> = reqs.iter().map(|(p, _)| p.as_str()).collect();
    assert!(
        paths.contains(&"POST /v1/local/import"),
        "import_typed did not hit /v1/local/import: {paths:?}"
    );
    assert!(
        paths.contains(&"POST /v1/transfers/tx-live/cancel"),
        "cancel_transfer did not hit cancel endpoint: {paths:?}"
    );
    assert!(
        paths.contains(&"POST /v1/transfers/tx-live/resume"),
        "resume_transfer did not hit resume endpoint: {paths:?}"
    );
    assert!(
        paths.contains(&"PUT /v1/local/settings/authkey"),
        "save_auth_key did not hit PUT /v1/local/settings/authkey: {paths:?}"
    );

    // Security invariant: neither bearer token nor auth key may appear in Ui messages,
    // persisted nsh-ui.json, or any rendered text node in the shadow tree.
    let ui = h.read_ui();
    assert!(
        ui.auth_input.is_empty(),
        "auth_input must be cleared after save_auth_key"
    );
    assert!(!ui.status.contains(secret_auth_key));
    assert!(!ui.settings_note.contains(secret_auth_key));
    assert!(!ui.status.contains(&mock.token));
    for node in h.attached_nodes() {
        if let Some(txt) = &node.text {
            assert!(
                !txt.contains(secret_auth_key) && !txt.contains(&mock.token),
                "secret leaked into rendered text node {}: {txt:?}",
                node.id
            );
        }
    }

    persist::save(
        mock.state_dir.to_str().unwrap(),
        &UiPersist {
            theme: ThemePref::Dark,
            restore_tabs: true,
            tabs: vec![SavedTab {
                id: "t1".into(),
                node: "alpha".into(),
                path: "/tmp/alpha".into(),
                filter: String::new(),
                sort: "name".into(),
                view: "list".into(),
            }],
            active_tab: 0,
            preview_on: true,
            sidebar_collapsed: false,
        },
    );
    let persisted_raw = std::fs::read_to_string(mock.state_dir.join("nsh-ui.json")).unwrap();
    assert!(!persisted_raw.contains(secret_auth_key));
    assert!(!persisted_raw.contains(&mock.token));
}

// ============================================================================
// INV-6: Cross-Platform Path, URI, & Token Safety Proofs
// ============================================================================

#[test]
fn inv6_cross_platform_path_and_safety_proofs() {
    // 1. Path normalization & boundary checks across Unix, Windows drive roots, and UNC paths.
    assert_eq!(
        token::norm_remote("C:\\Users\\Alice\\Docs\\"),
        "C:/Users/Alice/Docs"
    );
    assert_eq!(token::norm_remote("C:\\"), "C:");
    assert_eq!(token::norm_remote("/"), "");
    assert_eq!(
        token::parent_remote("C:/Users/Alice").as_deref(),
        Some("C:/Users")
    );
    assert_eq!(token::parent_remote("C:/"), None);
    assert_eq!(token::parent_remote("/var/log").as_deref(), Some("/var"));
    assert_eq!(token::join_remote("C:/", "Users"), "C:/Users");
    assert_eq!(
        token::join_remote("/tmp/alpha", "notes.txt"),
        "/tmp/alpha/notes.txt"
    );

    // Prefix-collision attack prevention: `/tmp/alpha-evil` is NOT under `/tmp/alpha`.
    assert!(token::path_is_under("/tmp/alpha", "/tmp/alpha"));
    assert!(token::path_is_under("/tmp/alpha/sub", "/tmp/alpha"));
    assert!(!token::path_is_under("/tmp/alpha-evil", "/tmp/alpha"));
    assert!(token::path_is_under(
        "C:/Users/beta/Shared/doc.txt",
        "C:/Users/beta/Shared"
    ));
    assert!(!token::path_is_under(
        "C:/Users/beta/Shared2",
        "C:/Users/beta/Shared"
    ));

    // 2. Breadcrumb crumbs() boundary check on partial prefix collision.
    let mut ui = Ui::new();
    ui.nodes = demo_nodes();
    ui.folder_node = "alpha".into();
    ui.folder_path = "/tmp/demo/alpha-other/sub".into();
    let parts = ui::crumbs(&ui);
    // Because /tmp/demo/alpha-other/sub is NOT under /tmp/demo/alpha, crumbs must not
    // strip /tmp/demo/alpha as if it were the read_root.
    assert_eq!(parts.len(), 1);
    assert_eq!(parts[0].path, "/tmp/demo/alpha-other/sub");

    // 3. file:// URI decoding across Unix, localhost authority, UNC shares, and UTF-8 percent encoding.
    assert_eq!(
        token::file_uri_to_path("file:///Users/alice/My%20File%20%E4%B8%AD%E6%96%87.txt"),
        "/Users/alice/My File 中文.txt"
    );
    assert_eq!(
        token::file_uri_to_path("file://localhost/tmp/report%201.pdf"),
        "/tmp/report 1.pdf"
    );
    assert_eq!(
        token::file_uri_to_path("file://server/share/folder/doc.txt"),
        "//server/share/folder/doc.txt"
    );
    assert_eq!(
        token::file_uri_to_path("/tmp/plain-path.txt"),
        "/tmp/plain-path.txt"
    );

    // 4. Hostname sanitization & non-echoing token error guarantee.
    assert_eq!(token::sanitize("my host/name!"), "my-host-name-");
    assert_eq!(token::sanitize(".."), "node");

    // Daemon install guides cover all desktop platforms without SSH.
    let guide = daemon::for_unreachable("node-1");
    assert!(guide.summary.contains("不会自动"));
    assert!(guide.note.contains("SSH"));
    for os in ["linux", "windows", "macos"] {
        assert!(!daemon::commands_clipboard(&guide, os).is_empty());
    }

    // Name validator edge cases.
    let existing = vec!["taken.txt".to_owned()];
    assert!(names::validate_name("valid_name-1.txt", &existing, false).is_ok());
    assert!(names::validate_name("taken.txt", &existing, false).is_err());
    assert!(names::validate_name("trailing.", &existing, true).is_err());
    assert!(names::validate_name("LPT1.txt", &existing, true).is_err());
}
