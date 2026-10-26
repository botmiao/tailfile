# Wails v3 vs MyGo

Both shells implement the same agent calls: node tree, file list, text / image / PDF preview, push and pull paste, progress, cancel, resume, mkdir, rename, import of a local path, and cache plus reveal. The agent is shared. The difference is the window.

Measured on Ubuntu 24.04, linux/amd64, Go 1.27.3, Wails v3.0.0, MyGo v0.6.4. Sizes are the binaries `scripts/build-all.sh` writes under `build-out/` (that directory is gitignored). Re-run the script before trusting a number you did not just produce. The figures below are from the hardening pass.

Both GUIs preview a file as soon as it is selected. Neither had a way to turn that off. Each now has a Preview on / Preview off control, default on, persisted in the browser (`localStorage` key `tailfile.preview`) or, for MyGo, in `UserConfigDir/tailfile/mygo-preview`. The localhost API requires the per-start bearer, so image and PDF bytes are fetched with `Authorization` instead of a bare URL.

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
| Real tsnet login, two physical machines | no | no auth key in this environment |
| Windows, macOS, Hyprland GUI | no | you have to do this |

A green cross-compile is not a working window. WebView2, Cocoa, and Hyprland were not executed.

## Code size

| Piece | Lines |
| --- | --- |
| `agent/` production Go | 4972 |
| `agent/` tests | 1046 |
| `gui-mygo/` production Go | 1067 |
| `gui-mygo/` test | 28 |
| `gui-wails/` Go (window, drop, token inject) | 179 |
| `gui-wails/frontend/src` TypeScript and CSS | 1251 |

Wails puts the file manager in the web UI, so the Go side stays a shell. MyGo puts it in Go, next to the widget calls. The agent dominates either way.

## Binary size (bytes)

Agent, stripped of nothing, tsnet linked in:

| OS | bytes |
| --- | --- |
| linux/amd64 | 33159793 |
| linux/arm64 | 30898170 |
| windows/amd64 | 33159168 |
| windows/arm64 | 30593024 |
| darwin/amd64 | 32745520 |
| darwin/arm64 | 30820818 |

MyGo (pure Go, tsnet not linked; it is only an HTTP client of the local agent):

| OS | bytes |
| --- | --- |
| linux/amd64 | 35704864 |
| linux/arm64 | 33175516 |
| windows/amd64 | 36030464 |
| windows/arm64 | 33138176 |
| darwin/amd64 | 35697600 |
| darwin/arm64 | 33499138 |

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

MyGo is larger than the Wails shell because the UI toolkit is in the binary. The agent is the large piece on a machine that runs both.

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

## Drag-out

| | Wails v3.0.0 | MyGo v0.6.4 |
| --- | --- | --- |
| Drop files in from the OS | Yes, desktop window only. Paths arrive in `WindowFilesDropped`. | Yes, `DroppedFiles` on the list. |
| Drag a file out to the OS file manager | No API found. Cache the file, copy the path, or Reveal. | Yes, `transfer.FileData` of an existing cached path. No promised-file / virtual file drag. |
| Typed local path import | Yes | Yes |

The drag-out cache is the OS user cache directory plus `tailfile` (mode `0700`), with `0600` files named from a hash of the node and the remote path. It is not the agent state directory.

Neither drag was performed against Explorer, Finder, or a Wayland portal in this VM. Treat the MyGo drag-out as "the API is there and typechecks," and confirm it on each OS, especially Wayland, where portal drag-and-drop is the usual failure.

## Maturity

Wails v2 has years of use. v3.0.0 does not; it shipped on 2026-10-26. The server build tag and the asset server behaved as the module comments describe. Budget time for API drift if you keep this past the spike.

MyGo v0.6.4 is explicitly early. It compiled and its own list virtualization is real, which is more than a README claim. The risk is the toolkit, not the file protocol. A breaking widget change costs a rewrite of `view.go`. A Wails widget change costs the thin Go shell; the React code would mostly stay.

## Recommendation

Keep the agent. It is the part with tests, and both shells are already clients of it.

Use the Wails server build to decide whether the file manager behaves the way you want. That is the UI that was actually clicked, including a direct A→B paste. Use the MyGo binary on your own machines to decide whether the native window, and drag-out in particular, is worth a toolkit that requires Go 1.27 and is still at 0.6.

If the weekend ends and only one shell should keep getting changes: **Wails**, unless drag-out to the OS file manager is a requirement rather than a nice-to-have. MyGo is the one that can start that drag. Wails is the one whose UI you can develop in a browser and whose risky dependency (WebKit) is the same one you already have to live with for a web view. On Hyprland, judge both with `WEBKIT_DISABLE_DMABUF_RENDERER=1` before you blame the layout. This VM did not run Hyprland, Windows, or macOS.
