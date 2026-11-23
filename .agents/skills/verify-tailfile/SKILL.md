---
name: verify-tailfile
description: Drive a real isolated Tailfile desktop GUI, attached local agent daemon, and CLI the way a user does. Use for live end-to-end UI/CLI/API verification, regression proofs, and visual screenshot/ARIA evidence capture across browsing, previews, file mutations, cross-node transfers, conflicts, and daemon attachment. (For pure-Go BFS model checking and headless ui.Tester invariants, see verify-tailfile-state-machine.)
---

# Verify Tailfile

Drive a live, isolated build of Tailfile (`MYGO_AUTOMATION` + isolated `agent --demo` + CLI subprocesses) using [`scripts/control-tailfile.go`](./scripts/control-tailfile.go) and the feature recipes in [`features/README.md`](./features/README.md). Never double-drive the user's live `127.0.0.1:8645` daemon or real tailnet state directory.

## Launch

Tailfile supports two isolated verification topologies via `--mode`:

1. **`attached` (default, recommended)**: Starts an isolated `tailfile agent --demo` daemon on an ephemeral loopback port (`127.0.0.1:<apiPort>`) with a fresh disposable `--state` dir and generated `gui-token`, waits for `/v1/health` (`ok: true`) and `/v1/info` (`mode: "demo"`), then starts the GUI with `--agent http://127.0.0.1:<apiPort> --token-file <runDir>/agent-state/gui-token --attach-only --state <runDir>/gui-state` and `MYGO_AUTOMATION=tcp:127.0.0.1:<autoPort>`. This exercises the full HTTP API + bearer token auth + desktop MyGo UI and enables independent API side-effect verification.
2. **`demo` (embedded single-process)**: Starts `tailfile --demo --state <runDir>/gui-state` with an in-process `memRoundTripper` demo cluster and `MYGO_AUTOMATION=tcp:127.0.0.1:<autoPort>` (no TCP agent port, badge reads `演示模式`).

Start the foreground supervisor as a background daemon task (e.g. `IsDaemon: true` in agent tool harnesses, or `&` in POSIX shells / separate PowerShell job), then wait for readiness with `wait-ready`:

```powershell
# 1. Start in a daemon/background runner (stays alive supervising agent + GUI):
go run .agents/skills/verify-tailfile/scripts/control-tailfile.go launch --run verify-demo --mode attached

# 2. From the main turn, wait until session.json is written and UI + API pass readiness:
go run .agents/skills/verify-tailfile/scripts/control-tailfile.go wait-ready --run verify-demo --timeout 60s
```

Why foreground supervision? Windows Job Objects and PTY runners terminate detached child processes as soon as a short-lived `launch` command exits; keeping `launch` alive as the daemon owner guarantees the GUI and agent stay running across driving commands until `cleanup --run <id>` stops them.

What `launch` does before reporting ready:

- Compiles `CGO_ENABLED=0 go build -tags "e2e mygo_nowebview" -o <TEMP>/tailfile-verify-runs/<run-id>/bin/tailfile-verify(.exe) .` (builds without `mygo_noinspector` so `plugins/automation` is compiled in via `e2e_automation.go`, and without `-H=windowsgui` so stdout/stderr logs are captured).
- Pre-seeds `<runDir>/gui-state/ui.json` (dark theme, list view, details open) so legacy user config (`ui.json` / `nsh-preview/ui.json`) is not inherited.
- Starts the demo agent (`attached` mode) and GUI window, binding ephemeral loopback ports (`127.0.0.1:0`).
- Does **not** touch the user's real tailnet, `agent.lock`, port `8645`, or real `~/Tailfile`. (Note: `mygo` persists OS window geometry under `WindowOptions.StateKey: "main"`, which does not affect shared files, transfers, or nodes.)
- Waits until `/v1/info` confirms `mode: "demo"`, the `"Tailfile"` window is visible, the sidebar footer shows `"已连接"` (or `"演示模式"`), and seeded node `alpha` + file `hello.txt` appear in the live accessibility tree.
- Persists run metadata (launcher PID, agent PID, GUI PID, ports, binary path, evidence path) atomically to `<TEMP>/tailfile-verify-runs/<run-id>/session.json` once ready (or writes failure details to `launch.error` so `wait-ready` fails fast), then prints `READY` and supervises both children until stopped.

See **Cleanup** below for tearing down the run by exact PID.

## Doctor

Run this read-only health check before driving any feature or whenever anything looks stuck:

```powershell
go run .agents/skills/verify-tailfile/scripts/control-tailfile.go doctor --run verify-demo
```

`doctor` exits `0` only if all of the following pass:

- `<runDir>` is isolated under the OS temp directory and `apiUrl` does not target port `8645`.
- `<binPath> --version` matches the version built at `launch`.
- Both `agentPid` (`attached` mode) and `guiPid` are alive.
- `GET /v1/info` (`attached` mode, authenticated with `gui-token`) returns `HTTP 200` and `"mode":"demo"`.
- `MYGO_AUTOMATION` reports a visible `"Tailfile"` content window with a non-empty ARIA tree.

## Drive

Read [`features/README.md`](./features/README.md) first, then follow the recipe for each user-facing feature in [`features/`](./features/).

Key driving patterns grounded in `internal/gui/view.go`:

- **Inspect the live tree anytime** (never guess coordinates or blind-click):
  ```powershell
  go run .agents/skills/verify-tailfile/scripts/control-tailfile.go snapshot --run verify-demo
  ```
- **Toolbar buttons, modals, and text inputs have stable `Key`s**:
  - Sidebar/nodes & pane resize: `sidebar-outline`, `node-alpha`, `node-beta`, `node-gamma`, `node-pixel`, `node-phone`, `tree-dir-<nodeID>-<path>`, `collapse-sidebar`, `expand-sidebar`, `sidebar-settings`, `resize-sidebar`, `resize-details`
  - Tabs & New Tab picker: `new-tab-btn` (`"新建标签页"`), `newtab-node-<id>`, `newtab-open-settings`
  - Toolbar: `tb-back`, `tb-fwd`, `tb-up`, `tb-refresh`, `tb-copy`, `tb-paste`, `tb-mkdir`, `tb-edit`, `tb-rename`, `tb-delete`, `tb-view`, `tb-details`, `tb-theme`
  - Breadcrumb / goto / filter: `goto-btn` (`"前往路径"`), `filter-input` (`"筛选当前目录"`)
  - Inline editor: `inline-editor-input`, `inline-editor-submit` (`"创建"` / `"确定"` / `"前往"`), `inline-editor-cancel`
  - Details actions: `det-open`, `det-edit`, `det-copy`, `det-rename`, `det-delete`
  - Transfer bar: `clear-transfers` (`"清除记录"`), `toggle-transfers` (`"展开"` / `"收起"`)
  - Modals: `del-confirm`, `del-cancel`, `doc-editor-input`, `doc-editor-find-btn`, `doc-editor-findbar`, `doc-editor-save`, `doc-editor-close`, `doc-editor-retry`, `doc-conflict-overwrite`, `doc-conflict-reload`, `doc-conflict-cancel`, `doc-discard-save`, `doc-discard-confirm`, `doc-discard-cancel`, `conflict-apply-all`, `conflict-overwrite`, `conflict-skip`, `conflict-cancel`, `theme-system`, `theme-light`, `theme-dark`, `density-auto`, `density-compact`, `density-standard`, `density-comfortable`, `close-pref-ask`, `close-pref-tray`, `close-pref-quit`, `set-sync-sidebar-tree`, `set-auto-enter-single-root`, `set-cache-dir`, `set-open-cache`, `set-save`, `set-cancel`, `close-remember`, `close-tray`, `close-quit`, `close-cancel`, `daemon-close` (`set-auth-key` and `set-clear-key` only render when the agent runs in experimental `--net tsnet` mode; `--agent`/`--token-file`/`--local` are CLI-only flags and are not rendered in Settings)
- **File rows, icon tiles, and sidebar items**:
  - For expanded desktop sidebar nodes and lazy-loaded directories (`sidebar-outline`), target `--role treeItem --label <name>` (or `--key node-<id>` without `--role listItem`; `act --role treeItem --label <name> --action expand` toggles branches, `--button middle` opens in new tab). In the collapsed `56pt` icon rail, rows keep `Key("node-<id>")` with `role=listItem`.
  - For file rows in List view, `ui.List` emits two `[listItem]` wrappers per item; always pass `--key-prefix "row-" --role listItem --label <name>` (or `--key-prefix "tile-" --role listItem --label <name>` in Icon Grid view) to avoid an ambiguous 2-match error. Use `--clicks 2` to enter a folder.
- **Invoke accessible button/field actions with `act`**:
  ```powershell
  go run .agents/skills/verify-tailfile/scripts/control-tailfile.go act --run verify-demo --key tb-mkdir --action press
  go run .agents/skills/verify-tailfile/scripts/control-tailfile.go act --run verify-demo --key inline-editor-input --action setValue --text "smoke-folder"
  go run .agents/skills/verify-tailfile/scripts/control-tailfile.go act --run verify-demo --key inline-editor-submit --action press
  ```
- **Keyboard shortcuts & waiting**: Use `key` (`Enter`, `Escape`, `F2`, `Delete`, `CmdOrCtrl+T`, `CmdOrCtrl+W`, `CmdOrCtrl+Shift+T`, `CmdOrCtrl+C`, `CmdOrCtrl+V`) and `wait` (`--contains` / `--absent` / `--key`) rather than fixed sleeps.
- **Side-effect & CLI verification**: Use `api` (authenticated requests to `/v1/fs/list`, `/v1/transfers`, `/v1/info`) and `cli` (`--version`, `--help`, `service --help`) against the same run's built binary.

## Evidence

Default proof root: `.verify-proof/<run-id>/<feature>/` (ignored by `.gitignore`; survives `cleanup`). Override via `launch --evidence <dir>`.

Proof standards:

1. **Real user path only**: Drive real buttons, list items, shortcuts, and CLI flags. Never mutate backend files behind the UI's back to fake a UI state.
2. **Action + resulting state**: Capture both the user action (e.g., open inline editor / confirmation dialog / CLI invocation) and the resulting settled state (`snapshot --out ...aria.txt` + `screenshot --out ...png`).
3. **Verify side effects independently**: After a file mutation or cross-node paste, query `api --path "/v1/fs/list?node=..."` (in `attached` mode) to prove disk/agent state changed, not just UI text.
4. **Visually inspect PNGs**: Open captured screenshots (e.g., via `read_file`) to verify rendered pixels match the ARIA snapshot. `screenshot` also automatically rejects blank/uniform images.
5. **Do not overclaim `--demo`**: Demo mode uses loopback mock peers and temp fixtures (`mode: "demo"`). Real tailnet `tsnet` login, peer `whois` grants, and OS service installation (`tailfile service install`) remain unverified unless explicitly run against a real tailnet host.

## Cleanup

Always terminate the run's processes and remove disposable state — **including after failed attempts**:

```powershell
go run .agents/skills/verify-tailfile/scripts/control-tailfile.go cleanup --run verify-demo
```

`cleanup` guarantees:

- Stops **only** `guiPid`, `agentPid`, and (if still running) `launcherPid` recorded in `<TEMP>/tailfile-verify-runs/<run-id>/session.json` (never kills by process name such as `tailfile.exe`), allowing the background `launch` daemon to exit cleanly.
- Deletes `<TEMP>/tailfile-verify-runs/<run-id>` (compiled temp binary, `agent-state/`, `gui-state/`, logs, `session.json`).
- **Never deletes** `.verify-proof/<run-id>/` (or custom `--evidence` dir) and reports the surviving artifact count.

## Helpers

[`scripts/control-tailfile.go`](./scripts/control-tailfile.go) (pure Go stdlib, zero external deps):

- `launch [--run <id>] [--mode attached|demo] [--evidence <dir>] [--timeout 35s]` (run as daemon/background task)
- `wait-ready --run <id> [--timeout 60s]`
- `doctor --run <id>`
- `snapshot --run <id> [--out <file.aria.txt>] [--json-out <file.json>]`
- `act --run <id> (--key <key> | --key-suffix <suffix> | --role <role> --label <label> | --id <id>) --action <press|focus|setValue|insertText|scrollIntoView> [--text <val>]`
- `click --run <id> [--key <key> | --key-prefix <prefix> | --key-suffix <suffix> | --role <role>] [--label <label>] [--contains <subtext>] [--index <n>] [--clicks 1|2] [--button left|right] [--mods cmdOrCtrl,shift]`
- `drag --run <id> --from-x <x0> --from-y <y0> --to-x <x1> --to-y <y1> [--steps 6] [--mods cmdOrCtrl,shift] [--hold] [--release-only]`
- `type --run <id> --text <str>`
- `key --run <id> --key <accel>`
- `wait --run <id> [--contains <subtext>] [--absent <subtext>] [--key <key>] [--absent-key <key>] [--timeout 10s]`
- `screenshot --run <id> --out <file.png>`
- `api --run <id> [--method GET] --path </v1/...> [--body <json>] [--no-auth] [--allow-error] [--raw] [--out <file>]`
- `cli [--run <id> | --bin <exe>] [--out <file>] [--allow-nonzero] [--timeout 20s] -- <args...>`
- `cleanup --run <id>`
