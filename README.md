# Tailfile

一个简短的技术验证（spike）：在同一个 Tailscale 文件 agent 之上做了两个 Finder 风格的外壳，方便你在自己的机器上试用，并从中挑选 GUI 技术栈。这不是产品。没有删除功能，没有提权，也没有第二个窗格。

左侧窗格是一棵树。根节点是运行 agent 的 tailnet 节点，下面是文件夹。右侧窗格是文件列表（名称、大小、修改时间），以及文本、图片和 PDF 的预览。在节点 A 上复制，粘贴到节点 B 的某个文件夹。GUI 只负责让其中一个 agent 推送（push）或拉取（pull），文件字节直接从 A 传到 B。

```
gui-wails / gui-mygo  --localhost-->  local agent  --tailnet-->  peer agent
                                              |                         |
                                              +---- job: push or pull --+
```

## 目录结构

| 路径 | 说明 |
| --- | --- |
| `agent/` | 共享的 Go 模块。文件 API、可续传分块、复制任务、系统 tailscaled 或 tsnet。 |
| `gui-wails/` | [Wails v3](https://v3.wails.io) 外壳。React UI，也可以在浏览器中运行（`-tags server`）。 |
| `gui-mygo/` | [MyGo](https://mygo.egoist.dev) 外壳。原生树、表格和拖出。 |
| `COMPARISON.md` | 两个框架在本次 spike 中的实际表现。 |

两个 GUI 都通过 `http://127.0.0.1:8645` 与 agent 通信。它们自身不内嵌 tailnet 节点。每台机器运行一个 agent。无论 agent 使用的是系统 tailscaled 还是内嵌的 tsnet 节点，GUI API 都相同。

需要 **Go 1.27.3**（MyGo v0.6.4 在更旧的 Go 上无法构建）。Wails v3.0.0 要求 Go 1.26 或更新版本。如果要修改 Wails UI，前端需要 Node 22。`go.work` 把三个模块关联在一起。

## 不用 tailnet 先试一下

```sh
cd agent
go build -o tailfile-agent ./cmd/tailfile-agent
./tailfile-agent -demo
```

`-demo` 会在进程内启动三个节点：`alpha` 和 `beta`（可写）以及 `gamma`（只读），所有者为 `ada@example.com`。不会加入任何 tailnet。API 位于 `http://127.0.0.1:8645`。`-demo` 会忽略 `-net`。

agent 在前台运行。下面的 GUI 命令请在另一个终端中、从仓库根目录执行。

在浏览器中运行 Wails（不需要 GTK，没有窗口）：

```sh
cd gui-wails
go build -tags server -o tailfile-wails .
./tailfile-wails -agent http://127.0.0.1:8645
```

打开 [http://127.0.0.1:17431](http://127.0.0.1:17431)。在 alpha 上复制 `hello.txt`，打开 beta，粘贴。传输行显示的是命令和进度。页面本身不会下载文件字节。

MyGo：

```sh
cd gui-mygo
go build -o tailfile-mygo .
./tailfile-mygo -agent http://127.0.0.1:8645
```

如果修改了 React UI，需要先重新构建它，再构建 Go 二进制，因为二进制会内嵌 `frontend/dist`：

```sh
cd gui-wails/frontend
npm ci
npm run build
```

`scripts/build-all.sh` 会运行 agent 测试、MyGo 测试以及下文的交叉编译，包括 agent、MyGo 和 Wails server 模式的 linux/arm64 与 windows/arm64。Linux 桌面版保持宿主机架构（cgo）。Windows 桌面 arm64 与 Windows 桌面 amd64 一样，只是尽力支持。

## 真实 tailnet

在每台机器上，选择 agent 可以读取的文件夹和可以写入的文件夹。没有 `-write` 列表时节点为只读。没有 `-read` 列表时，agent 会创建 `~/Tailfile`（权限 `0700`）并且只共享该目录。Demo 模式不使用这个默认值，而是共享临时测试数据目录。

`-net` 决定 agent 如何加入 tailnet：

| 取值 | 行为 |
| --- | --- |
| `auto`（默认） | 如果本地 tailscaled 响应其 LocalAPI、状态为 `Running` 且有 Tailscale IP，则使用 system 模式，否则内嵌 tsnet。日志会说明选择了哪种以及原因。 |
| `system` | 使用本机已在运行的 tailscaled。不启动第二个节点。 |
| `tsnet` | 内嵌一个 tailscale.com/tsnet 节点。它是独立于系统 tailscaled 的另一个节点。 |

```sh
# Prefer the machine's own tailscaled. No auth key.
./tailfile-agent -net system -read "$HOME/Tailfile" -write "$HOME/Tailfile"

# Force an embedded node.
export TS_AUTHKEY='tskey-auth-...'   # never commit this, never pass it on the command line if you can avoid it
./tailfile-agent -net tsnet \
  -hostname my-laptop \
  -read "$HOME/Tailfile" \
  -write "$HOME/Tailfile"
```

如果给 agent 传了 `-hostname` 或 `-state`，状态目录（以及其中的 `gui-token`）就不在默认位置，启动 GUI 时需要用 `-state`（或 `-token-file`）指向同一个目录。

`-authkey` 参数仍然可用，但会打印警告。建议使用 `TS_AUTHKEY` 或 Settings 界面，这样 key 不会出现在 shell 历史和服务命令中。进程启动时的读取顺序是 `TS_AUTHKEY`，然后是 `-authkey`，最后是文件 `<state>/ts-authkey`。key 为空时，tsnet 会打印一个交互式登录 URL，用浏览器打开即可。保存或清除 key 不会重启已经运行的节点，需要重启 agent。本地 API 只返回是否配置了 key，从不返回 key 本身，key 也从不写入日志。

状态保存在用户配置目录下的 `tailfile/<hostname>`（权限 `0700`），或 `-state` 指定的位置。不要提交该目录。

| 操作系统 | 状态目录 |
| --- | --- |
| Linux | `~/.config/tailfile/<hostname>/` |
| macOS | `~/Library/Application Support/tailfile/<hostname>/` |
| Windows | `%AppData%\tailfile\<hostname>\` |

`<hostname>` 默认是 `tailfile-` 加上本机主机名。`gui-token` 在每次启动时重写（权限 `0600`；Windows 上 DACL 仅授权给当前用户）。`ts-authkey` 是保存的 auth key，权限相同，重启后保留。`jobs.json`、`transfers.json` 和 `service.json` 的权限也是 `0600`。System 模式不会创建 tailnet 节点密钥；`-hostname` 只用于命名状态目录。其他节点看到的名称是 tailscaled 原有的名称。

System 模式只在节点的 Tailscale IPv4 和 IPv6 地址（`100.64.0.0/10` 和 `fd7a:115c:a1e0::/48`）上监听 `-port`（8644），从不绑定 `0.0.0.0`。如果这些地址变化或 tailscaled 重启，agent 会丢弃旧 socket 并绑定新地址。对其他 agent 的出站调用是普通的系统拨号，目标为 `100.x` / `fd7a::`，不走用户态网络栈。主机防火墙必须允许 Tailscale 接口（Linux 上为 `tailscale0`）的入站 TCP 8644。tsnet 模式在进程内部监听，因此主机防火墙看不到该端口。

在 Linux 上，LocalAPI socket 通常只有 root 可访问。如果 system 模式（或 auto 模式的回退日志）提示无法读取 tailscaled，执行 `sudo tailscale set --operator=$USER`，然后以该用户身份重新启动 agent。在 macOS 上，auto 和 system 会同时尝试开源版 `tailscaled` 的 unix socket 以及 App Store / 独立版应用。这些 GUI 版本通过 `sameuserproof` 文件（App Store 版为 `IPNExtension`，独立版为 `macsys`）发布 LocalAPI 端口，而不仅仅是 socket。官方的 `tailscale.com/client/local` 拨号器能找到该端口；本 agent 没有设置 `UseSocketOnly`，因此两者都会尝试，存在 GUI 版时优先使用它。

无论哪种模式，agent 还会在 localhost 的 `127.0.0.1:8645` 上监听。本地监听器拒绝 `127.0.0.1`、`localhost` 或 `::1` 以外的任何地址（`0.0.0.0:8645` 和 `:8645` 都无法启动）。不要对外公开 8645 端口。GUI bearer token、origin 检查和 host 检查都保持不变。

每个 tailnet 请求都会通过 LocalAPI `WhoIs` 检查，无论该 LocalAPI 属于系统 tailscaled 还是内嵌的 tsnet 节点。在 agent 知道自己的 tailnet 用户之前，它会拒绝 tailnet 调用。之后只允许该用户访问，除非你通过 `-owner` 或 `TAILFILE_OWNER` 指定其他登录名。System 模式下默认所有者是本地 tailscaled 已登录的用户。tailnet 用户为 `tagged-devices` 的节点，或自身 peer 带有 tag 的节点，没有人类所有者；此时除非设置了 `-owner` 或 `TAILFILE_OWNER`，否则进程会报错退出。

Tailscale grants 可以收窄（以及共享）访问权限。如果 WhoIs 的 `CapMap` 包含 `iori.dev/cap/tailfile`，则强制执行该 grant，不再要求同一用户检查。每个值都是 JSON：

```json
{"read": ["/home/ada/Tailfile"], "write": ["/home/ada/Tailfile/inbox"]}
```

grant 中的路径会与本节点的 `-read` 和 `-write` 列表取交集。列表之外的路径会被丢弃，因此 grant 无法扩大节点的共享范围。比已配置根目录更宽的 grant 会保留已配置的根目录。没有该 capability 时回退到同一用户规则。capability 存在但为空，或不是合法 JSON 时，一律拒绝（fail closed）。

localhost API 是唯一可以导入、缓存或显示路径的地方。它不使用 WhoIs，而是要求每次 agent 启动时生成的 bearer token（32 个随机字节，十六进制），以权限 `0600` 写入 `<state dir>/gui-token`。日志行会给出该路径，但不打印 token。两个 GUI 读取同一个文件（或通过 `-token-file` / `-state` 指定）。Wails 会把它作为 `window.TAILFILE_TOKEN` 注入 `index.html`，并把页面标记为 `no-store`，因为浏览器自己无法读取该文件。MyGo 发送 `Origin: mygo://tailfile`。请求必须带 `Authorization: Bearer …`。`Host` 必须是回环地址，这样可以拦截指向 127.0.0.1 的 DNS rebinding 域名。不设置 `Access-Control-Allow-Origin: *`。只有当浏览器 `Origin` 是 Wails server（`http://127.0.0.1:17431`、`localhost` 或 `[::1]`，端口 17431）、`wails://` 桌面页面、`http://wails.localhost` 或 `mygo://tailfile` 时才会回显。空 `Origin` 配合 bearer 是允许的（MyGo、curl、测试）。预检 `OPTIONS` 不需要 bearer。把 token 放进回环地址上的 Wails 页面，无法阻止已经能连接 `127.0.0.1:17431` 的其他系统用户；`0600` 文件权限可以阻止其他用户从磁盘读取 token。

Settings 仅限本地。`GET /v1/local/settings` 返回解析后的网络模式（`system`、`tsnet` 或 `demo`）、请求的 `-net` 值以及 `authKeyConfigured`。`PUT /v1/local/settings/authkey` 携带 `{"authKey":"..."}` 会写入 `<state>/ts-authkey`。`DELETE /v1/local/settings/authkey` 会删除它。这两个写操作的响应都会设置 `restartRequired`，且不回显 key。tailnet 监听器不提供这些路由。

`/v1/local/import` 只接受位于读取根目录内的系统路径（包括解析符号链接之后）。根目录以外的路径会被拒绝。Demo 模式下读取根目录是测试数据目录，因此导入 `/tmp/...` 会失败。在真实 agent 上，需要为每个想导入的目录传入 `-read`。

复制是两个节点之一上的一个任务（job）。**Push** 表示源端读取自己的磁盘并写入目标端。**Pull** 表示目标端读取源端并写入自己的磁盘。分块大小为 1 MiB，每块有各自的 SHA-256。partial 文件（`*.tailfile-partial`）及其 meta（`*.tailfile-meta`，权限 `0600`）记录源文件大小、源文件 mtime，以及完成时的整文件 SHA-256。完成时如果该哈希不匹配，就删除 partial。之后续传时如果发现源文件大小或 mtime 变了，该文件会从头开始。

当目标路径已存在且是文件时，任务暂停，状态为 `conflict`。两个 GUI 都会询问 覆盖（overwrite）或 跳过（skip）。勾选 对后续冲突都这样处理 会把该选择保存为同一任务中后续文件的策略。没有明确选择时绝不替换文件。覆盖会把新字节写入 partial，只有在整文件 SHA-256 匹配后才把 partial 重命名到目标位置。哈希不对会删除 partial 并保留旧文件。meta 仍与当前源文件大小和 mtime 一致的 partial 会直接续传而不提示，包括旧目标文件还在时被中断的覆盖。目标是目录时报错，而不是提示。待处理的冲突、逐文件选择和"应用到后续"策略都保存在 `jobs.json` 中，因此重启后仍然有效。通过 `POST /v1/transfers/{id}/conflict`（GUI 发起的 push 或 pull）或 `POST /v1/jobs/{id}/conflict`（本地导入任务）作答：

```json
{"action": "overwrite", "applyToRest": false}
```

`action` 为 `overwrite` 或 `skip`。任务和传输记录保存在状态目录的 `jobs.json` 和 `transfers.json`（权限 `0600`）中，因此 agent 进程重启后 Resume 仍然可用。关闭时正在运行的任务会以 `interrupted` 状态恢复。正在等待冲突处理的任务保持 `conflict`；在对话框作答之前，对该任务的 Resume 会被拒绝。删除功能未实现。

节点不会提权。它不能 sudo，也不会弹出 UAC 提示。它只能看到启动它的用户能看到的文件。

## 按用户安装的服务

```sh
tailfile-agent service install|uninstall|start|stop|status [flags]
```

这只为当前用户安装服务，不需要 root 或管理员。`--system` 会被拒绝。全机范围的 unit 留给以后单独的安装程序（见 `agent/service.go` 中 `ErrUserServiceOnly` 上的 TODO），那条路径不得复用按用户安装的 unit、plist 或登录任务。要更换服务所属的账户，先卸载，再以另一个用户登录后重新安装。已有服务时再次安装也会失败，因此修改 `-net`、`-read`、`-write` 或 `-hostname` 同样需要这两步。

你传入的参数会写入服务命令和 `<state>/service.json`（`scope` 为 `"user"`，权限 `0600`）。该文件不含任何密钥。如果省略了这些参数，安装时还会记录解析后的 `-hostname`、`-state`、`-net` 以及默认的 `-read`（`~/Tailfile`，权限 `0700`），这样以后机器主机名变化时服务也不会指向新的状态目录。`-authkey` 会保存到 `ts-authkey`，不会写入 unit、plist、任务或 `service.json`。服务进程不会继承执行 `install` 的 shell 中的 `TS_AUTHKEY`。如果希望它优先于文件，需要自己在服务环境中设置该变量。

安装只注册服务，不会在当前会话中启动进程，`service start` 才会启动。在 Linux 和 Windows 上，注册后该用户下次登录时也会启动 agent。在 macOS 上，只有在 `service start` 加载 plist 之后，登录时才会启动。

### Linux

用户 unit 为 `~/.config/systemd/user/tailfile-agent.service`。安装会执行 `systemctl --user enable` 和 daemon-reload。该 unit 由 `default.target` 拉起（wanted by），失败时自动重启。它不是系统 unit，也不读取 `/etc/sysconfig`。它在该用户下次登录时启动。如果希望在没有人登录时也保持用户管理器（以及 agent）运行：

```sh
loginctl enable-linger "$USER"
```

### macOS

LaunchAgent 为 `~/Library/LaunchAgents/tailfile-agent.plist`，带 `RunAtLoad`。没有 LaunchDaemon，也不需要 `sudo`。安装只写入 plist。`service start` 执行 `launchctl load`，会立即启动 agent，并在以后登录时再次启动。`service stop` 会卸载 plist，在下次 `service start` 之前保持卸载状态。

### Windows

Windows 上的 `kardianos/service` 通过服务控制管理器（Service Control Manager）安装，需要管理员权限。它的 `UserService` 选项文档中仅支持 POSIX，因此本构建在 Windows 上不调用该库，而是为执行安装的用户注册一个名为 `TailfileAgent` 的 Task Scheduler 登录任务（`schtasks /Create /XML`）：`RunLevel` 为 LeastPrivilege，交互式登录触发器，无执行时间限制。`HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 值同样无需提权，但无法启动、停止或查询状态；任务则可以通过 `schtasks /Run`、`/End` 和 `/Query` 做到。该任务在该用户登录时运行。它不是 Windows 服务，也不会弹出 UAC 提示。

```sh
./tailfile-agent service install -net system -read "$HOME/Tailfile" -write "$HOME/Tailfile"
./tailfile-agent service status
./tailfile-agent service start
```

## 各操作系统的构建

安装 Go 1.27.3 后，同样的 `go build` 命令在各操作系统上都可用。生成本仓库的 Linux 虚拟机上的交叉编译结果见 `COMPARISON.md`。能编译通过不代表窗口能正常运行。

### Windows

- 从 [go.dev](https://go.dev/dl/) 安装 Go 1.27.3；只有重新构建 Wails 前端时才需要 Node 22。
- Wails 桌面版使用 WebView2，当前的 Windows 11 已自带。Windows 桌面二进制可以在 Linux 上用 `CGO_ENABLED=0` 交叉编译，但未在此运行过。
- MyGo 和 agent 都是 `CGO_ENABLED=0` 构建。
- 先运行 `tailfile-agent`，再运行 `tailfile-wails` 或 `tailfile-mygo`。

### macOS

- Go 1.27.3，以及构建 Wails 桌面二进制所需的 Xcode 命令行工具（它使用 cgo：Cocoa 和 WebKit）。
- Linux 机器无法生成 Wails 桌面二进制。`GOOS=darwin` 时 `clang` 会拒绝 `-arch`。请在 Mac 上构建：

  ```sh
  cd gui-wails && go build -o tailfile-wails .
  ```

- agent、MyGo 和 Wails **server** 模式（`-tags server`）可以交叉编译到 darwin amd64 和 arm64。server 模式是浏览器 UI，不是 Mac 窗口。
- MyGo 的拖出使用原生文件拖拽，拖的是磁盘上已存在的路径。

### Ubuntu

```sh
sudo apt install gcc pkg-config libgtk-4-dev libwebkitgtk-6.0-dev
# MyGo's window loads GTK 3 at runtime:
sudo apt install libgtk-3-0
cd gui-wails && go build -o tailfile-wails .
```

Wails 默认使用 GTK 4 和 WebKitGTK 6.0。如果只有 WebKit2GTK 4.1，请用 `-tags gtk3` 构建。

### Arch / Omarchy（Hyprland，Wayland）

```sh
sudo pacman -S --needed base-devel gtk4 webkitgtk-6.0 pkgconf
# MyGo window:
sudo pacman -S --needed gtk3
```

在 Hyprland 上，WebKitGTK 常常显示空白视图，直到降低合成（compositing）设置。请为 Wails 桌面进程设置以下变量；MyGo 在其 web view 中显示 PDF 时也要设置：

```sh
export WEBKIT_DISABLE_DMABUF_RENDERER=1
./tailfile-wails
```

如果窗口仍然空白，还可以在 XWayland 下尝试 `GDK_BACKEND=x11`。这一点没有在此虚拟机上针对 Hyprland 测试过。

## 把文件拖到系统文件管理器

两个外壳都可以把文件下载到本地缓存（`POST /v1/local/cache`），然后显示它（`POST /v1/local/reveal`：macOS 上用 Finder，Windows 上用资源管理器，Linux 上对父目录执行 `xdg-open`）。缓存位于 `os.UserCacheDir()/tailfile`（权限 `0700`），而不是状态目录。每个文件权限为 `0600`，文件名由节点 id 加远程路径的哈希生成，因此两个 `note.txt` 不会互相覆盖。

之后 MyGo 可以用 `transfer.FileData` 把该缓存路径拖出去。该路径必须已经存在，不支持 promised-file 拖拽。

Wails v3 可以接受拖放**到**窗口上（`EnableFileDrop`，带 `data-file-drop-target` 的元素）。没有找到能把本地路径拖出到资源管理器或 Finder 的 Wails API。Wails UI 会显示缓存路径供你复制，Reveal 会打开它。浏览器版（`-tags server`）在拖放时看不到系统路径，所以它还有一个"要导入的本地路径"输入框。MyGo 也有同样的输入框，另外支持原生拖放到文件列表。

## sing-box（TUN）与 Tailscale

**System 模式**使用已有的内核（或 tailscaled 用户态）路由。发往 `100.64.0.0/10` 和 `fd7a:115c:a1e0::/48` 的数据包对 sing-box 这类 TUN 栈是可见的。这些网段必须设为 **direct**（直连），否则 agent 无法访问 peer，peer 也无法访问 8644 端口。除非 TUN 也截获了 agent 的 LocalAPI socket 或 GUI 的 localhost 调用，否则不需要为 agent 本身设置进程直连规则。

**tsnet 模式**不会安装内核路由。这些网段的数据面留在进程内部（gVisor）。sing-box 永远看不到来自 agent 的这些数据包。主机能看到的是控制连接（到 Tailscale 的 HTTPS）和 DERP（UDP）。如果 agent 进程被 TUN 截获，断掉的就是这些流量，节点会显示离线。请把 `tailfile-agent` 设为 sing-box **direct**，同时仍让 Tailscale 网段直连，以保证系统 `tailscaled` 和 `tailscale ping` 正常工作：

- `100.64.0.0/10`
- `fd7a:115c:a1e0::/48`
- `-net tsnet` 时为 `tailfile-agent` 设置进程直连（如果 localhost 被截获，GUI 也要设置）

从系统 tailscale 检查：

```sh
tailscale ping my-laptop
```

在 tsnet 模式下，该主机名就是你给 agent 的 `-hostname`，它和系统 tailscaled 是不同的节点。在 system 模式下，ping 本机已有的 MagicDNS 名称；agent 不是独立节点。收到 pong 说明系统 tailnet 能访问该名称。如果每次 ping 都显示 `via DERP`，说明直连路径没有建立；复制仍可工作，只是会经由 DERP 中转。`tailscale ping` 本身不能证明 tsnet agent 的用户态栈绕过了 TUN。如果 ping 正常而 tsnet agent 仍然无法保持登录，缺的就是进程规则。

## 测试

下面两条命令都从仓库根目录执行（分别执行，第二条不要在 `agent/` 目录中运行）：

```sh
cd agent && go test -count=1 -race .
cd gui-mygo && go test -count=1 .
```

agent 测试覆盖：列目录、符号链接逃逸、范围读取、错误的分块哈希、续传、同一用户鉴权、push 与 pull（通过计数传输层证明字节流经执行方而不是编排方）、取消后续传、目录复制以及只读目标。加固相关测试为 `TestLocalRequiresTokenAndLoopbackHost`、`TestImportRespectsReadRoots`、`TestDefaultReadRootIsTailfile`、`TestListenRejectsNonLoopback`、`TestResolveOwnerTaggedDevice`、`TestGrantNarrowsRoots`、`TestFinishRejectsBadSHA256`、`TestResumeRestartsWhenSourceChanges`、`TestJobListSurvivesRestart` 和 `TestCacheKeySeparatesSameNames`。系统 tailscaled 模式由 `TestNetAutoSelectsSystem`、`TestNetAutoFallsBackToTsnet`、`TestNetSystemRequiresRunningTailscaled`、`TestNetSystemPermissionError`、`TestSystemWhoIsAuth`、`TestTailListenAddrsRejectsNonTailscale`、`TestSystemRebindOnIPChange`、`TestSystemNodeListFromStatus` 和 `TestProbeSystemTailscaledReturns` 覆盖。同名目标由 `TestExistingFilePromptsAndOverwriteIsAtomic`、`TestExistingFileSkipLeavesIt`、`TestConflictApplyToRestSkipsTheNextFile`、`TestPartialSameSourceResumesWithoutPrompt`、`TestOverwriteRenamesOnlyAfterHash`、`TestConflictSurvivesRestart` 和 `TestAnswerWithoutChoiceDoesNotOverwrite` 覆盖。auth key 文件由 `TestAuthKeyAPIStoresAndDoesNotEcho` 和 `TestAuthKeyRejectsEmptyAndMultiline` 覆盖。按用户服务的方案由 `TestPrepareServiceArgsOmitsAuthKeyAndRejectsSystem`、`TestStableServiceArgsAndConfig`、`TestWindowsLogonTaskDoesNotElevate`、`TestServicePlanStoresAuthKeyOutsideTheUnit` 和 `TestUserSystemdUnitIsPerUser` 覆盖。这些测试检查 unit 文本和任务 XML，不调用 `systemctl`、`launchctl` 或 `schtasks`。以上所有测试都使用进程内回环测试环境或伪造的 LocalAPI，而不是真实 tailnet，也不是 tsnet 的集成控制平面。默认测试不会联网回报。

针对 system 模式、磁盘上的 token 和 auth key、`~/Tailfile`、服务安装、冲突对话框以及 Settings 界面的手动测试步骤见 [docs/TESTING.md](docs/TESTING.md)。

选中文件时，两个 GUI 都会显示预览。各自有一个会持久保存的 Preview on / Preview off 开关（默认开启）。Wails 将其保存在 `localStorage` 的 `tailfile.preview` 下。MyGo 在 `UserConfigDir/tailfile/mygo-preview`（权限 `0600`）中保存 `0` 或 `1`。图片和 PDF 字节通过 bearer token 获取，因为 `<img src>` 或 PDF.js URL 不会发送 `Authorization`。

MyGo 的测试用 `ui.NewTester` 构建窗口，并点击 Copy 和 Paste。`TestConflictAndSettingsDialogs` 检查冲突对话框（覆盖、跳过、对后续冲突都这样处理）以及显示网络模式的设置视图。它不会打开显示器。

## 此虚拟机没有做的事

没有在 Windows、macOS 或 Hyprland 上逐一点击测试 GUI。Wails server 模式在 Chrome 中针对 `tailfile-agent -demo` 测试过（树、文本、图片、PDF canvas、push、pull、只读粘贴、mkdir、重命名、导入、缓存）。Linux 的 Wails 桌面进程和 MyGo 进程在 Xvfb 下都保持运行，但没有点击其中的控件。这里没有 `TS_AUTHKEY`，也没有本地 tailscaled，因此既没有运行真实的 tsnet 加入，也没有针对真实 tailnet 运行 system 模式。详细情况和二进制大小见 `COMPARISON.md`。
