# Wails v3 vs MyGo vs Native Shell

All three shells implement the same agent calls: node tree, file list, text / image / PDF preview, push and pull paste, progress, cancel, resume, mkdir, rename, import of a local path, and cache plus reveal. The agent is shared. The difference is the window. Native Shell's PDF pane is the agent's text extract; it does not rasterize the page.

Measured on Ubuntu 24.04, linux/amd64. Wails v3.0.0 and the cross-compiled sizes below are from the earlier system-tailscaled pass (`scripts/build-all.sh`, Go 1.27.3, MyGo v0.6.4) unless a row says otherwise. Native Shell numbers in the new section were taken on this machine on 2026-11-02: rustc 1.99.1 (MSRV declared later as >= 1.97), `nsh` 0.3.0 (`76fa8a7`), GTK 4.14.5, no GPU (DRI3 missing, llvmpipe). MyGo's startup and idle RSS in that section were re-measured the same day, on the same display, against the same `tailfile-agent -demo`. The older MyGo binary-size table was not regenerated.

Both GUIs preview a file as soon as it is selected. Neither had a way to turn that off. Each now has a Preview on / Preview off control, default on, persisted in the browser (`localStorage` key `tailfile.preview`) or, for MyGo, in `UserConfigDir/tailfile/mygo-preview`. The localhost API requires the per-start bearer, so image and PDF bytes are fetched with `Authorization` instead of a bare URL.

The agent can ride the machine's existing tailscaled (`-net system` or `-net auto` when LocalAPI reports `Running` and a Tailscale IP) or embed its own tsnet node (`-net tsnet`, and the auto fallback). The GUI still talks only to `127.0.0.1:8645`. System mode was not run against a live tailnet here; the tests use a fake LocalAPI.

## What was actually built and run

| Target | Built here | Run here |
| --- | --- | --- |
| Agent tests, including `-race` | yes | yes |
| Agent linux amd64 and arm64, windows amd64 and arm64, darwin amd64, darwin arm64 | yes, `CGO_ENABLED=0` | linux/amd64 binary, demo mode only |
| MyGo test (`ui.NewTester`) | yes | yes, no display |
| MyGo linux amd64 and arm64, windows amd64 and arm64, darwin amd64, darwin arm64 | yes, `CGO_ENABLED=0` | linux/amd64 process stayed up 10s under Xvfb before this pass. It logged `native UI draws without the GPU`. Widgets were not clicked. |
| Wails server linux amd64 and arm64, windows amd64 and arm64, darwin amd64, darwin arm64 | yes, `-tags server`, pure Go | linux/amd64 server, in Chrome, against `-demo` before this pass |
| Wails desktop linux (GTK 4.14.5, WebKitGTK 2.52.6) | yes, cgo | process stayed up under Xvfb until killed. Widgets were not clicked. libEGL warned that DRI3 was missing. |
| Wails desktop windows amd64 and arm64 | yes, `CGO_ENABLED=0` cross-compile | no |
| Wails desktop darwin | no | clang on Linux rejects `-arch` for the Cocoa/WebKit cgo |
| Native Shell linux/amd64 (`cargo build --release`, GTK 4) | yes | yes. Window clicked against `-demo`: tree, `hello.txt` preview, conflict dialog, settings. Release process measured |
| Native Shell windows/amd64 | not on this machine | CI job `nsh-windows` builds it. Not clicked here |
| Native Shell macOS (AppKit) | no | needs macOS 14+ and Xcode CLT. Crate is wired; this VM cannot link it |
| Real tsnet login, or system mode against a live tailscaled | no | no auth key and no tailscaled in this environment |
| Windows, macOS, Hyprland GUI | no | you have to do this |

A green cross-compile is not a working window. WebView2, Cocoa, and Hyprland were not executed.

## Code size

| Piece | Lines |
| --- | --- |
| `agent/` production Go | 5388 |
| `agent/` tests | 1436 |
| `gui-mygo/` production Go | 1067 |
| `gui-mygo/` test | 28 |
| `gui-wails/` Go (window, drop, token inject) | 179 |
| `gui-wails/frontend/src` TypeScript and CSS | 1251 |
| `gui-nsh/src` Rust, unit tests included | 2989 |
| `gui-nsh/tests/demo_agent.rs` | 205 |

Wails puts the file manager in the web UI, so the Go side stays a shell. MyGo puts it in Go, next to the widget calls. The agent dominates either way.

## Binary size (bytes)

Agent, stripped of nothing. tsnet stays linked because `-net auto` falls back to it:

| OS | bytes |
| --- | --- |
| linux/amd64 | 33201833 |
| linux/arm64 | 30972474 |
| windows/amd64 | 33198592 |
| windows/arm64 | 30628352 |
| darwin/amd64 | 32793024 |
| darwin/arm64 | 30872818 |

MyGo (pure Go, tsnet not linked; it is only an HTTP client of the local agent):

| OS | bytes |
| --- | --- |
| linux/amd64 | 35704304 |
| linux/arm64 | 33175036 |
| windows/amd64 | 36028928 |
| windows/arm64 | 33138176 |
| darwin/amd64 | 35697536 |
| darwin/arm64 | 33499074 |

Wails server mode (pure Go, assets embedded, tsnet not linked; it only imports the token-path helper):

| OS | bytes |
| --- | --- |
| linux/amd64 | 19507668 |
| linux/arm64 | 18326559 |
| windows/amd64 | 20473344 |
| windows/arm64 | 19120128 |
| darwin/amd64 | 19774256 |
| darwin/arm64 | 18806354 |

Wails desktop: linux/amd64 `19284568` bytes; windows/amd64 `20067840` bytes; windows/arm64 `18754560` bytes. None of the desktop binaries were executed in this pass. Darwin desktop did not link.

Native Shell release, this machine, `cargo build --release`, linux/amd64, **not stripped**: `33901208` bytes. `strip --strip-unneeded` on a copy brought it to `24600936`. Windows and macOS sizes were not produced here. A MyGo linux/amd64 binary rebuilt the same day, `CGO_ENABLED=0`, was `35849570` bytes (the table above is the older `build-out` figure, `35704304`).

MyGo is larger than the Wails shell because the UI toolkit is in the binary. Native Shell's release binary is in the same range as MyGo before stripping. The agent is still the large piece on a machine that runs both.

## Wails v3.0.0

What worked:

- `go build -tags server` is pure Go. The same React UI served on `127.0.0.1:17431` and was driven in headless Chrome against the demo agent.
- Checked there: tree of alpha / beta / gamma, text preview of `hello.txt` and `week.md`, PNG preview (`naturalWidth > 0`), PDF.js canvas (588×224, dark pixels, no error) plus the agent's text extract, push of `hello.txt` onto beta, pull of `inbox.txt` onto alpha, paste onto read-only gamma (error shown), mkdir, rename, import of a typed local path, cache path shown.
- `@tanstack/react-virtual` windows the tree and the file list. PDF.js renders the preview. That is ordinary web code.
- File drop **in** is a documented window option (`EnableFileDrop` and `data-file-drop-target`). The Go side posts the paths to `/v1/local/import`. This path was compiled. It was not exercised with a real OS drag, because the browser build cannot see those paths.
- Linux desktop linked against system WebKitGTK 6.0 and GTK 4 and stayed running under Xvfb.
- Windows desktop cross-compiled from Linux with cgo off.

What did not:

- No API in v3.0.0 starts a drag of a local path out to Explorer, Finder, or Nautilus. Cache and Reveal are the stand-in. The page says so.
- Server mode cannot receive an OS file drop. Typed paths cover import while you are in the browser.
- Darwin desktop does not cross-compile. You need a Mac with the platform SDK.
- v3.0.0 was released the day this spike was written. The option names used here (`ServerOptions`, `BundledAssetFileServer`, `EnableFileDrop`) matched the module. They can still move.
- Under Xvfb, WebKit logged a DRI3 / libEGL failure and a missing system dbus socket. The process did not exit. That is not a Hyprland test.

Linux WebKit / Wayland: the desktop shell is WebKitGTK. On Hyprland set `WEBKIT_DISABLE_DMABUF_RENDERER=1` before launching. `GDK_BACKEND=x11` is the fallback. Neither was verified on Hyprland.

Build friction: two toolchains (Go and Node) and, for a real window, a WebKit dev package. Server mode avoids the WebKit package, which is why it was the one that got clicked. `frontend/dist` is committed so a Go-only checkout can still `go build -tags server`.

## MyGo v0.6.4

What worked:

- The module fetched, compiled, and the headless tester clicked Copy and Paste (`TestChrome`) on the first fix (an unused import).
- `Outline` and `Table` are windowed. `buildList` only builds rows in the visible range, so a second virtualizer was not added.
- `transfer.FileData` builds a drag payload for a path that already exists. The UI attaches it to the "drag this row out" handle after cache. This was compiled and not dragged onto a real file manager.
- `DroppedFiles` on the list posts an import. Same caveat: not dragged from a real desktop.
- PDF uses `Window.NewWebView` when a window exists, and the agent's text extract either way. The headless test does not open a web view.
- Cross-compile to linux, windows, and both darwin architectures with `CGO_ENABLED=0` succeeded. The linux binary ran under Xvfb without a GPU and did not exit.

What did not, or is unverified:

- The native window was not clicked. Tester coverage is the toolbar status strings for an empty clipboard.
- Web view PDF preview was not shown on a display. Text extract is the path that does not need WebKit.
- `RunOnMain` from a goroutine can block if the event loop is not running. The GUI test must not call `refresh`, which posts to the main loop. That is easy to get wrong.
- Go 1.27 is a hard requirement. A machine on Go 1.22 or 1.26 cannot build this shell.
- The widget API is young. This spike used `Outline`, `Table`, `Split`, `WebView`, `DragData`, and `DroppedFiles` as they exist in v0.6.4. A 0.7 can rename them.

Linux WebKit / Wayland: the shell itself is GTK 3 and OpenGL, with a software fallback that ran under Xvfb. The PDF web view is still WebKit, so the same `WEBKIT_DISABLE_DMABUF_RENDERER=1` hint applies when a PDF is open. The rest of the window may be fine without it. Not verified on Hyprland.

Build friction: one `go build`, no cgo, no Node. The highest friction is the Go version and the fact that almost nobody has shipped a MyGo app yet. Runtime needs GTK 3 on Linux even though the compile does not.

## Native Shell 0.3.0

MSRV: **rustc >= 1.97** (`rust-version = "1.97"` in `gui-nsh/Cargo.toml`; `gui-nsh/rust-toolchain.toml` pins `1.97.0`). Verified on this pass with rustc 1.99.1. Upstream `nsh-core` 0.3.0 uses features stabilized in 1.95 (`if let` guards) and 1.97 (`isolate_lowest_one`) but does not declare `rust-version`; older toolchains fail with `error[E0658]` (seen on Windows). Check with `rustc --version`; update with `rustup update stable`.

The window is Rust (`gui-nsh/`), not TSX. Rust is the path the docs lead with (`nsh init --template rust`, no QuickJS in the binary). The TSX template exists and hot-reloads, but nothing in 0.3.0 showed the Rust widgets this screen needs (`view`, `text`, `input`, `scroll`, `image`, `button`, `checkbox`, `dialog`) as missing or stubbed. The Linux template compiled here on the first try. TSX was not used.

License: Apache-2.0. The installed CLI is `nsh 0.3.0 (76fa8a7)`, fetched from `https://nsh.dev/install.sh`, which downloads `nsh-linux-x86_64.tar.gz` from the GitHub release. crates.io has `nsh-core`, `nsh-base`, `nsh-platform-linux`, `nsh-platform-windows`, and `nsh-platform-macos` at 0.3.0. The GitHub repo `longbridge/nsh` was created 2026-10-14. The 0.3.0 release notes say the compatibility baseline is not frozen and that 0.2.x to 0.3.0 is a breaking import move. That is the maturity risk.

What was clicked, on this machine, against `tailfile-agent -demo`:

- Left tree of alpha / beta / gamma, gamma marked 只读. Opening alpha listed `notes/`, `photos/`, `hello.txt`.
- Selecting `hello.txt` showed the agent's text: `Hello from alpha.`
- Pasting that file onto the same folder raised 目标已有同名文件, with 覆盖, 跳过, and 对后续冲突都这样处理. 跳过 closed the dialog. The transfer line then read 完成 100%.
- 设置 showed `网络模式：demo（请求的 -net：auto）` and `Auth key：未配置`. The field does not echo a saved key. The `cargo test` demo drives PUT/DELETE and checks the response body and `GET /v1/local/settings` do not contain the key.

Image preview is implemented (bytes are written under the user cache and passed to the image element). It was not the click exercised in the screenshot pass. PDF preview is the text extract only.

Drag-out: no API was found that starts a file drag of an existing path. Cache plus 显示 (`POST /v1/local/reveal`) is the stand-in, same shape as Wails. Drop-in uses `on_file_drop`. Typed import is there. Neither drop was performed with a real file manager in this VM.

Measured on this machine, linux/amd64, display `:1`, no GPU:

| | Native Shell 0.3.0 release | MyGo v0.6.4, rebuilt the same day |
| --- | --- | --- |
| Binary | `33901208` bytes, not stripped (`24600936` after `strip --strip-unneeded`) | `35849570` bytes, `CGO_ENABLED=0` |
| Time until a window titled Tailfile existed | 143 ms | 67 ms |
| RSS about 4 s after that | 211688 KB | 67892 KB |

Both drew without a real GPU. MyGo logged `native UI draws without the GPU`. Native Shell pulled in llvmpipe threads after a DRI3 / libEGL warning. Those RSS numbers are this VM, not a Windows desktop with a GPU. They are measured, not estimated. Windows binary size, Windows RSS, and macOS anything were not measured.

An Xvfb pass of the same MyGo binary sat at 122252 KB after 3 s. That run is not the row above; the row is the paired display measurement.

Dev experience: one Rust crate, `cargo build`, GTK 4 headers on Linux, no Node, no cgo. `nsh check` typechecks the Rust root. `cargo test` does not need a display; the demo test does need `go` and a built agent, and CI runs it. The sharp edge hit while writing this spike: `spawn` is owned by whoever is on the stack, and a button handler's owner dies when the handler returns, which cancelled preview and paste until tasks were re-parented onto the component owner. That is a real footgun, not a README hypothetical.

Windows: the crate depends on `nsh-platform-windows` 0.3.0 (Win32, Direct2D). `cargo build --release` is the command, and the binary is `target\release\tailfile-nsh.exe`. This VM cannot run it. The CI job `nsh-windows` is what checks that the Win32 backend links.

macOS: `nsh-platform-macos` is the AppKit backend, documented as macOS 14+. Not built here.

## Drag-out

| | Wails v3.0.0 | MyGo v0.6.4 | Native Shell 0.3.0 |
| --- | --- | --- | --- |
| Drop files in from the OS | Yes, desktop window only. Paths arrive in `WindowFilesDropped`. | Yes, `DroppedFiles` on the list. | Yes, `on_file_drop` on the list. Not dragged from a real desktop in this VM. |
| Drag a file out to the OS file manager | No API found. Cache the file, copy the path, or Reveal. | Yes, `transfer.FileData` of an existing cached path. No promised-file / virtual file drag. | No API found. Cache, the path is shown, 显示 calls the agent's reveal. |
| Typed local path import | Yes | Yes | Yes |

The drag-out cache is the OS user cache directory plus `tailfile` (mode `0700`), with `0600` files named from a hash of the node and the remote path. It is not the agent state directory.

Neither drag was performed against Explorer, Finder, or a Wayland portal in this VM. Treat the MyGo drag-out as "the API is there and typechecks," and confirm it on each OS, especially Wayland, where portal drag-and-drop is the usual failure.

## Maturity

Wails v2 has years of use. v3.0.0 does not; it shipped on 2026-10-26. The server build tag and the asset server behaved as the module comments describe. Budget time for API drift if you keep this past the spike.

MyGo v0.6.4 is explicitly early. It compiled and its own list virtualization is real, which is more than a README claim. The risk is the toolkit, not the file protocol. A breaking widget change costs a rewrite of `view.go`. A Wails widget change costs the thin Go shell; the React code would mostly stay.

Native Shell 0.3.0 is younger than both. The repo dates from 2026-10-14, the release says the compatibility baseline is not frozen, and a button-handler task was cancelled by the toolkit until the call moved under the component owner. The Rust API that this window uses did compile and the GTK window did the file-manager clicks above. A breaking 0.4 would be a rewrite of `gui-nsh/src/ui.rs` and `model.rs`, which is most of the shell.

## Recommendation

Keep the agent. It is the part with tests, and both shells are already clients of it.

Use the Wails server build to decide whether the file manager behaves the way you want. That is the UI that was actually clicked, including a direct A→B paste. Use the MyGo binary on your own machines to decide whether the native window, and drag-out in particular, is worth a toolkit that requires Go 1.27 and is still at 0.6.

If the weekend ends and only one shell should keep getting changes: **Wails**, unless drag-out to the OS file manager is a requirement rather than a nice-to-have. MyGo is the one that can start that drag. Wails is the one whose UI you can develop in a browser and whose risky dependency (WebKit) is the same one you already have to live with for a web view. On Hyprland, judge both with `WEBKIT_DISABLE_DMABUF_RENDERER=1` before you blame the layout. This VM did not run Hyprland, Windows, or macOS.

Native Shell does not change that drag-out split. It is the third window, and the one whose Linux build was actually clicked in this pass, but on this machine it opened slower than MyGo (143 ms vs 67 ms) and sat at about three times the RSS (211688 KB vs 67892 KB) with no drag-out and no PDF page render. Use it to judge the Win32/Direct2D and GTK 4 look on your own machines. The toolkit risk is higher than MyGo's, which is already early.
