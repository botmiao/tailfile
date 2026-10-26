# Tailfile

A short spike: two Finder-style shells over one Tailscale file agent, so you can try them on your own machines and pick a GUI stack. This is not a product. There is no delete, no privilege escalation, and no second pane.

The left pane is a tree. Its roots are the tailnet nodes running the agent, then folders. The right pane is a file list (name, size, modified) and a preview for text, images, and PDF. Copy on node A and paste into a folder on node B. The GUI only tells one agent to push or to pull. The bytes go directly from A to B.

```
gui-wails / gui-mygo  --localhost-->  local agent  --tailnet-->  peer agent
                                              |                         |
                                              +---- job: push or pull --+
```

## Layout

| Path | What it is |
| --- | --- |
| `agent/` | Shared Go module. File API, resumable chunks, copy jobs, tsnet. |
| `gui-wails/` | [Wails v3](https://v3.wails.io) shell. React UI, also runnable in a browser (`-tags server`). |
| `gui-mygo/` | [MyGo](https://mygo.egoist.dev) shell. Native tree, table, and drag-out. |
| `COMPARISON.md` | What each framework actually did in this spike. |

Both GUIs talk to the agent at `http://127.0.0.1:8645`. They do not embed a second tsnet node. Run one agent per machine.

Requires **Go 1.27.3** (MyGo v0.6.4 will not build on older Go). Wails v3.0.0 wants Go 1.26 or newer. The frontend wants Node 22 if you change the Wails UI. `go.work` ties the three modules together.

## Try it with no tailnet

```sh
cd agent
go build -o tailfile-agent ./cmd/tailfile-agent
./tailfile-agent -demo
```

`-demo` starts three in-process nodes, `alpha` and `beta` (writable) and `gamma` (read-only), owned by `ada@example.com`. Nothing joins a tailnet. The API is on `http://127.0.0.1:8645`.

Wails in the browser (no GTK, no window):

```sh
cd gui-wails
go build -tags server -o tailfile-wails .
./tailfile-wails -agent http://127.0.0.1:8645
```

Open [http://127.0.0.1:17431](http://127.0.0.1:17431). Copy `hello.txt` on alpha, open beta, Paste. The transfer line is the command and the progress. The file bytes are not downloaded by the page.

MyGo:

```sh
cd gui-mygo
go build -o tailfile-mygo .
./tailfile-mygo -agent http://127.0.0.1:8645
```

If you change the React UI, rebuild it before the Go binary, because the binary embeds `frontend/dist`:

```sh
cd gui-wails/frontend
npm ci
npm run build
```

`scripts/build-all.sh` runs the agent tests, the MyGo test, and the cross-compiles below.

## Real tailnet

On each machine, pick the folders the agent may read and the folders it may write. With no `-write` list the node is read-only. With no `-read` list the default read root is your home directory. Demo mode does not use that default.

```sh
export TS_AUTHKEY='tskey-auth-...'   # never commit this, never pass it on the command line if you can avoid it
./tailfile-agent \
  -hostname my-laptop \
  -read "$HOME" \
  -write "$HOME/Tailfile"
```

`-authkey` exists and prints a warning. Prefer `TS_AUTHKEY` so the key stays out of shell history. An empty key makes tsnet print an interactive login URL; open it in a browser. State (the node key) lives under the user config directory, `tailfile/<hostname>`, mode `0700`, or `-state`. Do not commit that directory.

The agent listens on the tailnet at `:8644` and on localhost at `127.0.0.1:8645`. Every tailnet request is checked with tsnet `LocalClient.WhoIs`. Until the agent knows its own tailnet user it refuses tailnet calls. After that, only that user is allowed, unless you set `-owner` or `TAILFILE_OWNER` to a different login. Localhost is trusted because it is loopback, and it is the only place that can import, cache, or reveal a path. Do not publish port 8645.

Copy is a job on one of the two nodes. **Push** means the source reads its disk and writes the destination. **Pull** means the destination reads the source and writes its disk. Chunks are 1 MiB, SHA-256 checked, and a partial file (`*.tailfile-partial` plus a meta file) survives a dropped connection. Cancel and Resume work for the life of the agent process. Job records are in memory; the partial file is what makes resume possible after a cancel. Restarting the agent drops the job list. Existing files are not overwritten. Delete is not implemented.

The node does not escalate. It cannot sudo, and it does not show a UAC prompt. It only sees the files the user who launched it can see.

## Build per OS

The same `go build` lines work on each OS once Go 1.27.3 is installed. Cross-compile results from the Linux VM that produced this repo are in `COMPARISON.md`. A compile is not a substitute for running the window.

### Windows

- Go 1.27.3 from [go.dev](https://go.dev/dl/), Node 22 only if you rebuild the Wails frontend.
- Wails desktop uses WebView2, which current Windows 11 already has. The Windows desktop binary can be cross-compiled from Linux with `CGO_ENABLED=0`; it was not run here.
- MyGo and the agent are `CGO_ENABLED=0` builds.
- Run `tailfile-agent` first, then `tailfile-wails` or `tailfile-mygo`.

### macOS

- Go 1.27.3, and Xcode command-line tools for the Wails desktop binary (it is cgo: Cocoa and WebKit).
- A Linux machine cannot produce the Wails desktop binary. `clang` rejects `-arch` when `GOOS=darwin`. Build it on a Mac:

  ```sh
  cd gui-wails && go build -o tailfile-wails .
  ```

- The agent, MyGo, and Wails **server** mode (`-tags server`) do cross-compile to darwin amd64 and arm64. Server mode is a browser UI, not a Mac window.
- MyGo's drag-out uses a native file drag of a path that already exists on disk.

### Ubuntu

```sh
sudo apt install gcc pkg-config libgtk-4-dev libwebkitgtk-6.0-dev
# MyGo's window loads GTK 3 at runtime:
sudo apt install libgtk-3-0
cd gui-wails && go build -o tailfile-wails .
```

Wails defaults to GTK 4 and WebKitGTK 6.0. If you only have WebKit2GTK 4.1, build with `-tags gtk3`.

### Arch / Omarchy (Hyprland, Wayland)

```sh
sudo pacman -S --needed base-devel gtk4 webkitgtk-6.0 pkgconf
# MyGo window:
sudo pacman -S --needed gtk3
```

WebKitGTK on Hyprland often paints a blank view until compositing is turned down. Set this for the Wails desktop process and for MyGo when a PDF is shown in its web view:

```sh
export WEBKIT_DISABLE_DMABUF_RENDERER=1
./tailfile-wails
```

If the window is still blank, also try `GDK_BACKEND=x11` under XWayland. That was not tested on Hyprland from this VM.

## Dragging files to the OS file manager

Both shells can download a file into a local cache (`POST /v1/local/cache`) and then reveal it (`POST /v1/local/reveal`: Finder on macOS, Explorer on Windows, `xdg-open` of the parent directory on Linux).

MyGo can then drag that cached path out with `transfer.FileData`. The path has to exist already. There is no promised-file drag.

Wails v3 can accept a drop **onto** the window (`EnableFileDrop`, elements with `data-file-drop-target`). No Wails API was found that starts a drag of a local path out to Explorer or Finder. The Wails UI shows the cached path so you can copy it, and Reveal opens it. The browser build (`-tags server`) cannot see OS paths on a drop, so it also has a "local path to import" field. MyGo has the same field plus a native drop onto the file list.

## sing-box (TUN) and Tailscale

tsnet does not install a kernel route. Its data plane for `100.64.0.0/10` and `fd7a:115c:a1e0::/48` stays inside the process (gVisor). A TUN stack such as sing-box never sees those packets from the agent. What the host does see is the control connection (HTTPS to Tailscale) and DERP (UDP). If the agent process is captured by the TUN, those are the flows that break, and the node looks offline.

Put the agent, and the GUI if it is a separate process, in sing-box **direct**. Also send the Tailscale prefixes direct, because the system `tailscaled` and `tailscale ping` use them on the host:

- `100.64.0.0/10`
- `fd7a:115c:a1e0::/48`
- process direct for `tailfile-agent`, `tailfile-wails`, and `tailfile-mygo`

Check from the system tailscale, which is a different node from the tsnet agent:

```sh
tailscale ping my-laptop
```

Use the `-hostname` you gave the agent. A pong means the system tailnet can reach that tsnet node. `via DERP` on every ping means the direct path is not up; the copy can still work, it will just hairpin through DERP. `tailscale ping` does not by itself prove the agent's own userspace stack is bypassing the TUN. If ping works and the agent still cannot stay logged in, the process rule is the one that is missing.

## Tests

```sh
cd agent && go test -count=1 -race .
cd gui-mygo && go test -count=1 .
```

The agent tests cover listing, symlink escape, range reads, a bad chunk hash, resume, same-user auth, push and pull where a counting transport shows the bytes on the runner rather than on the orchestrator, cancel plus resume, a directory copy, and a read-only destination. They use an in-process loopback harness, not a live tailnet and not tsnet's integration control plane. Default tests do not phone home.

MyGo's test builds the window with `ui.NewTester` and clicks Copy and Paste. It does not open a display.

## What this VM did not do

The GUI was not clicked through on Windows, macOS, or Hyprland. Wails server mode was exercised in Chrome against `tailfile-agent -demo` (tree, text, image, PDF canvas, push, pull, read-only paste, mkdir, rename, import, cache). The Linux Wails desktop process and the MyGo process both stayed up under Xvfb; their widgets were not clicked. There is no `TS_AUTHKEY` here, so a real tailnet join was not run. Details and binary sizes are in `COMPARISON.md`.
