# Testing Guide

## Automated Verification Layers

1. **Agent, CLI Flag, & Token Security (`internal/agent`, `internal/cliflag`, `internal/guitoken`)**
   - Covers path root sandboxing, symlink escape refusal, Tailnet grant scoping, loopback bearer/Origin checks, `memRoundTripper` embedded runtime, state directory flock/LockFileEx preventing token/port clobbering, resumable uploads, and per-user service config generation.
2. **Formal State Machine & Path Sandbox BFS (`internal/gui/formal_test.go`, `pathbox_test.go`)**
   - Exhaustive BFS over tab creation/closing/switching/reopening, node switching, directory navigation, filter/editor/delete transient resets, read-only write gating, conflict priority, lexical path sandboxing, and `SettingsDraft` isolation.
3. **Headless Native UI Integration (`internal/gui/verify_test.go`, `density_test.go`)**
   - Uses `mygo/ui.NewTester` against a live embedded 5-node `--demo` cluster (`INV-1..INV-8` + Layout Density):
     - `INV-1`: Desktop navigation, history Back/Forward/Up, tabs (`Mod+T`/`Mod+W`/`Mod+Shift+T`), List/Icon view toggles.
     - `INV-2`: Text and PNG bitmap preview rendering.
     - `INV-3`: Folder creation, renaming, cross-node copy/paste transfer, and read-only write gating (`gamma`).
     - `INV-4`: Non-dismissible `ConflictDialog` (`Escape` blocked; `覆盖`/`跳过`/`取消任务` resolves).
     - `INV-5`: Compact `<820` mobile layout (`390x844`, `mobile.Drawer`, `mobile.Sheet`, Dark/Light themes).
     - `INV-6`: Pausing 1s polling while `WindowHidden()`.
     - `INV-7`: Built-in plain-text editor (`ModalDocEdit`), remote save & preview refresh, external modification conflict overwrite, dirty close guard, read-only viewer (`gamma`), and `<820` mobile render.
     - `INV-8` & Layout Density: Lazy `ui.Outline` sidebar tree, middle/right-click open in new tab, blank New Tab picker, and immediate `auto`/`compact`/`standard`/`comfortable` density switching with marquee hit-testing.
4. **Live Window E2E (`-tags e2e`)**
   - `e2e_automation.go` enables `github.com/egoist/mygo/plugins/automation` when built with `-tags e2e` and launched with `MYGO_AUTOMATION`.

## Running Tests

```powershell
# Run the full suite
go test -count=1 ./...

# With race detector (Linux/macOS, or Windows with CGO_ENABLED=1 + gcc)
go test -race -count=1 ./...
```
