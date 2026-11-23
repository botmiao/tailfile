# Tailfile — Agent & Developer Guide

## Architecture & Module Map

Unified single-binary pure-Go application (`module tailfile.local`, Go 1.27+, `CGO_ENABLED=0`, UI via `github.com/egoist/mygo@v0.10.0`).

- `main.go`, `console_*.go`, `e2e_automation.go`: CLI & GUI entrypoint (`tailfile`, `tailfile agent`, `tailfile service <install|uninstall|start|stop|status>`, `tailfile version`, `tailfile help`).
- `internal/agent/`: System `tailscaled` integration (default `--net system`, no silent fallback; `--net tsnet` is CLI opt-in experimental), local/tailnet HTTP servers, single-file `PUT /v1/fs/write` (`docfile.go`), resumable chunked uploads/transfers, `memRoundTripper` in-process transport, `Connect` / `StartEmbedded`, per-user service installer (`tailfile` / `Tailfile`), and non-blocking state lock (`agent.lock`).
- `internal/gui/`: Pure Go UI state machine (`session.go`, `tree.go`, `docedit.go`, `pathbox.go`, `names.go`, `persist.go`, `density.go`, `daemon.go`), custom palette & vector icons (`theme.go`, `icons.go`, `assets.go`), responsive desktop (`view.go`) and `<820` mobile (`mobile.go`) MyGo views, plus BFS formal tests (`formal_test.go`), tree unit tests (`tree_test.go`), and `ui.NewTester` integration tests (`verify_test.go`).
- `internal/cliflag/`, `internal/guitoken/`: CLI usage formatting and bearer token management.
- `resources/icon.png`, `mygo.json`, `scripts/gen-winres/`, `rsrc_windows_*.syso`, `LICENSE`: App packaging icon, MyGo bundle metadata, MIT license, and pure-Go generated Windows PE resources (icon, PerMonitorV2 manifest, version info; regenerate via `go generate .`).
- `scripts/` & `.github/workflows/`: Cross-platform installers (`install.sh`, `install.ps1`, `install-agent.sh`), release packager (`build-all.sh`), and CI/Release workflows (`ci.yml`, `release.yml`).

## Build & Test Commands

```powershell
# Run all unit, formal BFS, and headless UI tests
go test -count=1 ./...

# Formal state machine + path sandbox checks
go test -count=1 -v ./internal/gui -run "TestFormal|TestPath"

# Live isolated GUI + attached daemon + CLI verification (see .agents/skills/verify-tailfile/SKILL.md)
# Run `launch` as a background daemon (`IsDaemon: true` / `&`), then wait & drive:
go run .agents/skills/verify-tailfile/scripts/control-tailfile.go launch --run smoke --mode attached
go run .agents/skills/verify-tailfile/scripts/control-tailfile.go wait-ready --run smoke --timeout 60s
go run .agents/skills/verify-tailfile/scripts/control-tailfile.go doctor --run smoke
go run .agents/skills/verify-tailfile/scripts/control-tailfile.go cleanup --run smoke

# Build Windows production GUI+CLI binary (no WebView runtime required)
$env:CGO_ENABLED = "0"
go build -tags mygo_nowebview -ldflags "-s -w -H=windowsgui" -o tailfile.exe .
```

## Key Invariants

1. **Zero-Config Single Process & Opt-In `:8645` (`internal/agent/embed.go`, `tsnet.go`, `internal/gui/view.go`)**: Desktop `tailfile` defaults to single-process `ConnEmbedded` over `memRoundTripper` (zero TCP `:8645`, zero disk `gui-token`) with close-to-tray via `mygo.NewTray` (`singleInstance` desktop only). Headless `tailfile agent` / `service` disables the `:8645` loopback listener unless explicit `--local` is passed, and GUI attaches over TCP only when `--agent`, `--local`, or `--attach-only` is given.
2. **Default Shared Folder (`internal/agent/run.go`)**: Without `--read`, `--write`, or `--read-only`, `~/Tailfile` defaults to read-write; `--read-only` disables writes (`WriteRoots = nil`). Passing `--read` without `--write` stays read-only.
3. **Path Sandboxing & Tailnet Edge Guards (`internal/gui/pathbox.go`, `internal/agent/paths.go`, `auth.go`)**: GUI canonicalizes remote paths lexically and enforces `ReadRoots`/`WriteRoots` membership; agent resolves symlinks on every file request, and `TailnetHandler` (`:8644`) rejects browser CORS/Fetch headers (`Origin`, `Sec-Fetch-*`) and non-Tailnet `Host` headers.
4. **Transient State Isolation, Layout Density & Sidebar Tree Sync**: Every folder or tab change (`SelectNode`, `SelectNodePath`, `Navigate`, `GoBack`, `GoForward`, `GoUp`, `SwitchTab`, `CloseTab`, `ReopenClosedTab`, `OpenNodeInNewTab`) clears filter, inline editors, and delete modal state; `DocEditor` / `ModalDocEdit` is isolated from `Session.Editor` (preserving unsaved edits across folder/tab transitions and `ConflictPrompt` preemption) and enforces `CanWritePath`; `ConflictPrompt` takes highest overlay priority and is non-dismissible. `NewTab` opens a blank device-picker tab (`Node == ""`, guarded against `RefreshAll` hijack via `initialNodeBound`); expanded desktop sidebar uses native `ui.Outline` lazy-loading subdirectories only (`AutoEnterSingleRoot` default `true`, `SyncSidebarTree` default `false`, persisted in `ui.json`). `Density` (`auto|compact|standard|comfortable`, default `auto`, persisted in `ui.json`) drives `ui.Theme.Spacing` and centralized `LayoutMetrics` across desktop/mobile views and marquee hit-testing, with `auto` using `AnyPointer` + viewport DIP bounds so touchscreen desktop PCs with a mouse stay on desktop density.
5. **Demo & Transfer Queue Isolation (`internal/agent/run.go`, `demo.go`, `server.go`)**: `--demo` always isolates state & cache in temporary directories without writing to user `StateDir`/`CacheDir`; non-demo startup prunes legacy `tailfile-demo-` entries from `transfers.json`, and transfer/job listings sort deterministically by `Seq`/`Created`.
6. **Experimental `tsnet` Gating (`internal/agent/netmode.go`, `internal/gui/view.go`)**: Default `--net` is `system` (`tailscaled`); legacy `--net auto` resolves to `system` without silent `tsnet` fallback. Explicit `--net tsnet` stays CLI-only experimental, and Settings hides `AuthKey` controls unless `Settings.Net == "tsnet"`.
7. **Docs & Skills**: Keep `AGENTS.md`, `README.md`, `.agents/skills/verify-tailfile/SKILL.md`, and `.agents/skills/verify-tailfile-state-machine/SKILL.md` aligned whenever architecture, flags, UI handles, or invariants change.
