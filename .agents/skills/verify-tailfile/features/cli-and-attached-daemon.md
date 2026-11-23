# CLI commands and attached daemon

CLI commands and attached daemon lets users and automation scripts inspect Tailfile version/usage, run an isolated headless daemon (`tailfile agent --demo`), authenticate against `gui-token`, attach a GUI (`--attach-only`) without port collisions, and receive actionable guidance when a daemon is unreachable.

## Sub-features

- `cli-version-help` prints `tailfile <version>` and subcommand usage without launching a GUI window.
- `daemon-token-auth` writes `<state>/gui-token`, requires `Authorization: Bearer <token>` on localhost API routes, and reports `mode: "demo"` in `--demo`.
- `gui-attached-badge` shows `已连接 · <online>/<total> 在线` in the sidebar footer when attached to an isolated daemon, and `<node> (可写|只读)` in the bottom-right status bar.
- `gui-offline-guide` opens `后台 Agent 未运行或无法连接` (`查看启动指南`) when `--attach-only` points to a closed local port.

## How to get to it (user POV)

- Run `tailfile --version`, `tailfile --help`, and `tailfile service --help` in a terminal.
- Run `tailfile agent --demo --local 127.0.0.1:<port> --state <dir>` to serve the demo API on loopback with bearer auth.
- Launch `tailfile --agent http://127.0.0.1:<port> --token-file <dir>/gui-token --attach-only --state <gui-state>` and inspect the footer badge.

## Driving it with control-tailfile

Preconditions:

- Isolated verification run `<run-id>` was launched via `--mode attached` and `doctor --run <run-id>` passes.

- **Version and help CLI subcommands.** Capture CLI transcripts using the run's compiled binary.
  1. `control-tailfile cli --run <run-id> --out .verify-proof/<run-id>/cli-daemon/version.txt -- --version` (exit code `0`, stdout `tailfile <ver>`).
  2. `control-tailfile cli --run <run-id> --out .verify-proof/<run-id>/cli-daemon/help.txt -- --help` (exit code `0`, lists `agent`, `service`, `version`, `help`, `--demo`, `--attach-only`).
  3. `control-tailfile cli --run <run-id> --out .verify-proof/<run-id>/cli-daemon/service-help.txt -- service --help` (exit code `0`, lists `install`, `uninstall`, `start`, `stop`, `status`).
- **Verify bearer token enforcement.** Hit `/v1/info` without and with auth header on the isolated agent.
  1. Unauthenticated: `control-tailfile api --run <run-id> --no-auth --allow-error --path "/v1/info" --out .verify-proof/<run-id>/cli-daemon/noauth-info.txt` (returns `HTTP 401` / `403`, never leaks node info).
  2. Authenticated: `control-tailfile api --run <run-id> --path "/v1/info" --out .verify-proof/<run-id>/cli-daemon/auth-info.json` (returns `HTTP 200` and `"mode":"demo"`).
- **Verify attached mode badge in GUI.** Inspect the live ARIA snapshot and screenshot.
  1. `control-tailfile snapshot --run <run-id> --out .verify-proof/<run-id>/cli-daemon/attached-ui.aria.txt`
  2. `control-tailfile screenshot --run <run-id> --out .verify-proof/<run-id>/cli-daemon/attached-ui.png`
  3. Confirm `"已连接"` appears in the sidebar footer and `"alpha (可写)"` appears in the bottom-right status bar (with no `"内置模式"` or `"本地后台"`).

## Gotchas

- Never pass `--local 127.0.0.1:8645` during verification; that collides with the user's real daemon if running.
- Unauthenticated `GET /v1/health` may behave differently from protected routes depending on local auth middleware — verify auth enforcement on `/v1/info`, `/v1/nodes`, or `/v1/fs/list`.
- Real service registration (`tailfile service install|start|stop|status`) modifies per-user OS scheduler/LaunchAgent/systemd user units and real `tailnet` requires live credentials — keep `service` tests scoped to `--help` / unit tests unless testing on a disposable VM.
