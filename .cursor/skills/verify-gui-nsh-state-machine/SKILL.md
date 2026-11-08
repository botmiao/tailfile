---
name: verify-gui-nsh-state-machine
description: Run explicit-state BFS formal model checking and bounded path-input verification for tailfile-nsh (gui-nsh/src/model.rs, gui-nsh/src/token.rs, gui-nsh/src/ui.rs). Use when modifying tab lifecycle, folder navigation, read_roots path sandboxing, selection/write-action gating, or modal/overlay state transitions in gui-nsh.
---

# Formal Verification: `tailfile-nsh` State Machine & Path Sandbox

This skill runs an explicit-state Breadth-First Search (BFS) model checker and bounded path-input verifier ([`gui-nsh/tests/formal_model_check.rs`](../../../../gui-nsh/tests/formal_model_check.rs)) directly against the real Rust state-transition functions in `tailfile-nsh`.

Unlike linear scenario tests that check one sequence of steps someone thought to write, this suite explores **every** sequence of actions up to a stated depth bound and **every** path combination in a bounded Unix/Windows path alphabet, returning the shortest counterexample trace whenever a property fails.

## Sync State

- **Sync commit**: `36418330efacc5960f2c7c78199beaa0ebe85d8e`
- **Verifier harness**: `gui-nsh/tests/formal_model_check.rs`
- **Maintenance skill**: Run `/maintain-formal-verification-skill` after changes to covered files to inspect `git diff <sync_commit>..HEAD` and keep this verifier in sync.

## Coverage Map

| Check Function | Covered Source Files & Symbols | Property Verified | Harm if Broken |
| :--- | :--- | :--- | :--- |
| `check_read_root_escape_freedom` | `gui-nsh/src/token.rs`: `path_is_under`, `norm_remote`, `join_remote`<br>`gui-nsh/src/model.rs`: `go_up`, `enter_row_dir`, `select_folder`, `click_tree`, `nav_back`, `nav_forward`, `switch_tab`<br>`gui-nsh/src/ui.rs`: `crumbs` | **ReadRootEscapeFreedom**, **CrumbEscapeFreedom**, **PrefixCollisionSafety**: Across all navigation action sequences up to depth 5 and 40 normalized Unix/Windows sibling-prefix combinations, `ui.folder_path` and every breadcrumb segment remain inside the active node's `read_roots`, and sibling prefixes (`/tmp/alpha-evil` vs `/tmp/alpha`) never collide. | Users or remote navigation state can escape configured `read_roots` sandboxes or misclassify a sibling directory (`/tmp/alpha-evil`) as lying inside `/tmp/alpha`. |
| `check_overlay_and_escape_determinism` | `gui-nsh/src/model.rs`: `begin_new_folder`, `begin_rename`, `ask_delete`, `open_daemon_help`, `open_settings`, `open_context_menu`, `select_row`, `toggle_select_path`, `boot` (`escape` handler) | **InlineEditorExclusion**, **PopupMenuExclusion**, **EscapeUniversalReset**: Across all reachable combinations of the 7 modal/editor/popup flags up to depth 4, `inline_new` and `inline_rename` are never simultaneously active, `ctx_open` and `more_open` are never simultaneously active, and pressing `escape` deterministically resets all 7 flags (`inline_new`, `inline_rename`, `delete_open`, `daemon_open`, `settings_open`, `ctx_open`, `more_open`) plus selection (`selected == -1`, `selected_paths.is_empty()`, `name_error.is_empty()`) in 1 step. | Overlapping inline editors or popups corrupt file actions (`Enter` triggers the wrong editor), or `Escape` leaves ghost modals/selections stuck on screen. |
| `check_folder_action_target_isolation` | `gui-nsh/src/model.rs`: `begin_new_folder`, `begin_rename`, `ask_delete`, `confirm_delete`, `mkdir`, `rename`, `toggle_select_path`, `switch_tab`, `select_folder`, `focus_or_open_tab`, `nav_back`, `reopen_closed_tab`<br>`gui-nsh/src/token.rs`: `path_is_under` | **ReadOnlyWriteGate**, **SelectionFolderScope**, **DotDotTraversalInPathIsUnder**: Across all 2-step sequences of write-intent/selection actions followed by folder/tab transitions: (1) if the active node has `read_only == true`, no write editor/modal (`inline_new`, `inline_rename`, `delete_open`) may remain open; (2) every path in `ui.selected_paths` must belong to the active `ui.folder_path`; (3) `path_is_under` must reject `..` parent-traversal escapes (`/tmp/alpha/../alpha-evil`). | Starting a rename/mkdir or multi-selecting files on Tab A and switching to Tab B executes `DELETE`/`MKDIR`/`RENAME` against the wrong folder/host or on a read-only node. |
| `check_active_tab_state_sync` | `gui-nsh/src/model.rs`: `select_row`, `set_sort`, ` set_filter`, `switch_tab`, `close_tab`, `reopen_closed_tab`, `new_tab_from_current`, `focus_or_open_tab`, `sync_active_tab_fields` | **ActiveTabPathSync**, **ActiveTabFieldsSync**: Whenever `ui.tabs` is non-empty, `ui.active_tab < ui.tabs.len()` and top-level `Ui` fields (`folder_node`, `folder_path`, `selected`, `sort`, `filter`, `view`) match `ui.tabs[ui.active_tab]` after every sequence of tab/selection/sort actions up to depth 3. | Tab switching, closing, or reopening silently loses or leaks row selection, sort order, or active folder path between tabs. |

## How to Run

### Standard Desktop Environment (Windows / macOS / Linux with system libs)

```bash
cargo test --manifest-path gui-nsh/Cargo.toml --test formal_model_check -- --nocapture
```

To run a single check:

```bash
cargo test --manifest-path gui-nsh/Cargo.toml --test formal_model_check check_read_root_escape_freedom -- --nocapture
```

### Headless Linux Container (without GTK4 `.pc` files installed)

```bash
SYSTEM_DEPS_GTK4_NO_PKG_CONFIG=1 \
SYSTEM_DEPS_GTK4_LIB=c \
SYSTEM_DEPS_GRAPHENE_GOBJECT_1_0_NO_PKG_CONFIG=1 \
SYSTEM_DEPS_GRAPHENE_GOBJECT_1_0_LIB=c \
RUSTFLAGS="-C link-arg=-Wl,--unresolved-symbols=ignore-all" \
cargo +stable test --manifest-path gui-nsh/Cargo.toml --test formal_model_check -- --nocapture
```

## Bounds, Scope & Assumptions

1. **Tool choice**: Explicit-state BFS model checker in Rust (`gui-nsh/tests/formal_model_check.rs`) driving the real `Signal<Ui>` reactive state inside a 1-node `nsh_core::Core<ApproxTextEngine>` host (`MinimalModelHost`). No external solver binary (Z3/Kani/TLA+) is required; it runs via `cargo test` in < 1 second once compiled.
2. **Search bounds**:
   - `check_read_root_escape_freedom`: BFS up to depth `5` over `9` navigation actions (`EnterDir0`, `EnterDir1`, `GoUp`, `NavBack`, `NavForward`, `SelectSubfolder`, `ClickTreeRoot`, `SwitchTab0`, `SwitchTab1`) with state-fingerprint deduplication, plus `5 × 8 = 40` normalized Unix/Windows path boundary pairs.
   - `check_overlay_and_escape_determinism`: BFS up to depth `4` over `10` modal/editor/selection actions with 9-tuple boolean state deduplication, verifying universal 1-step `PressEscape` reset from every reachable state.
   - `check_folder_action_target_isolation`: Exhaustive `10 × 10` 2-step cross-product of write-intent/selection actions and tab/folder transition actions, plus dot-dot traversal boundary check.
   - `check_active_tab_state_sync`: Exhaustive BFS up to depth `3` (`1 + 9 + 81 + 729 = 820` traces) over `9` tab/selection/sort actions.
3. **What is modeled vs. stubbed**:
   - `model::enable_test_mode()` is enabled so background HTTP thread spawns (`offload`) do not make network calls.
   - In `check_read_root_escape_freedom`, `sync_mock_entries_for_active_folder` populates `ui.entries` with child entries under the active `ui.folder_path` after each navigation step to model steady-state `load_list` completion.
4. **What is NOT covered by this skill**:
   - SVG/Canvas icon geometry, Taffy pixel layout across viewports, and live TCP localhost HTTP round-trips (covered by `gui-nsh/src/verification.rs` `INV-1..INV-6`).
   - OS-native drag-and-drop file promises (`start_native_drag`) and platform window manager APIs.

## Distinguishing a Real Bug from Modeling Drift

When a check in `formal_model_check` fails, determine whether it is a real product bug or harness drift using these rules:

1. **Real bug in product code (`gui-nsh/src/model.rs`, `token.rs`, `ui.rs`)**:
   - The counterexample trace calls only public state-transition functions (`switch_tab`, `select_folder`, `reopen_closed_tab`, `focus_or_open_tab`, `begin_new_folder`, `toggle_select_path`, `go_up`, `path_is_under`, etc.) and reaches a state where:
     - `ui.folder_path` or a breadcrumb is outside `node.read_roots`,
     - `inline_new` / `inline_rename` / `delete_open` remains `true` on a `read_only == true` node,
     - `ui.selected_paths` retains paths from an old folder after `ui.folder_path` changed,
     - `ui.selected` / `ui.sort` / `ui.filter` / `ui.view` disagrees with `ui.tabs[ui.active_tab]`, or
     - `PressEscape` fails to clear an open modal/editor/popup flag.
   - **Action**: Keep the finding; do not weaken the assertion in `formal_model_check.rs`. Fix the state transition in `gui-nsh/src/model.rs` or `gui-nsh/src/token.rs` (when requested by the user).

2. **Harness / modeling drift (`gui-nsh/tests/formal_model_check.rs`)**:
   - A transition function in `model.rs` was intentionally renamed, or a new field was added to `TabState` / `Ui` without updating `seed_model_ui` or `PressEscape` in `formal_model_check.rs`.
   - A synchronous test step relies on an async `offload` callback updating `ui.entries` after `load_list`, and the test step forgot to call `sync_mock_entries_for_active_folder(state)`.
   - **Action**: Update `formal_model_check.rs` to match the new signature/assumption and update the sync commit in this `SKILL.md`.

## Verification Status (All 4 Checks Passing)

All 4 formal model checks (`check_read_root_escape_freedom`, `check_overlay_and_escape_determinism`, `check_folder_action_target_isolation`, `check_active_tab_state_sync`) pass with `0` counterexamples:

1. **`ReadOnlyWriteGate` (Resolved)**:
   - `reset_folder_transient_state` in `gui-nsh/src/model.rs` clears `inline_new`, `inline_rename`, `delete_open`, `ctx_open`, and `more_open` across all folder/tab transitions (`select_folder`, `focus_or_open_tab`, `switch_tab`, `close_tab`, `reopen_closed_tab`, `nav_back`, `nav_forward`), and `mkdir`, `rename`, and `confirm_delete` re-verify `current_folder_readonly(&ui)`.
2. **`SelectionFolderScope` (Resolved)**:
   - `reset_folder_transient_state` clears `ui.selected_paths` across every folder/tab transition, and `toggle_select_path` verifies `path_is_under(&path, &ui.folder_path)` before adding a path to `ui.selected_paths`.
3. **`DotDotTraversalInPathIsUnder` (Resolved)**:
   - `token::path_is_under` canonicalizes `.` and `..` segments via `canonicalize_remote` in `gui-nsh/src/token.rs`, rejecting parent-directory traversal outside `root` (e.g., `/tmp/alpha/../alpha-evil`).
4. **`ActiveTabFieldsSync` (Resolved)**:
   - `focus_or_open_tab`, `select_folder`, `switch_tab`, `close_tab`, `reopen_closed_tab`, `nav_back`, and `nav_forward` synchronize outgoing and incoming `TabState` fields (`selected`, `filter`, `sort`, `view`, `node`, `path`) with top-level `Ui`.
