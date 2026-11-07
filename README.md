# Tailfile

基于 Tailscale 文件 agent 的跨平台桌面文件管理器，桌面客户端采用 [Native Shell (`nsh`)](https://nsh.dev)（`gui-nsh/`，Rust 编写，Linux 上使用 GTK 4，Windows 上使用 Win32/Direct2D，macOS 上使用 AppKit）。已支持标签页、深浅主题、双击进入目录、右键上下文菜单、新建文件夹、重命名、删除（确认后永久删除）、跨节点复制粘贴与冲突处理、按用户服务安装及 daemon 引导。进程不提权。

左侧窗格是一棵树。根节点是运行 agent 的 tailnet 节点，下面是文件夹。右侧窗格是文件列表（名称、大小、修改时间），以及文本、图片和 PDF 文本的预览。在节点 A 上复制，粘贴到节点 B 的某个文件夹。GUI 只负责让其中一个 agent 推送（push）或拉取（pull），文件字节直接从 A 传到 B。

```
gui-nsh  --localhost-->  local agent  --tailnet-->  peer agent
                              |                         |
                              +---- job: push or pull --+
```

## 目录结构

| 路径 | 说明 |
| --- | --- |
| `agent/` | Go 编写的后台与节点通信模块。文件 API、可续传分块、复制任务、系统 tailscaled 或 tsnet。 |
| `gui-nsh/` | [Native Shell](https://nsh.dev) 桌面客户端（Rust）。Linux 上是 GTK 4，Windows 上是 Win32/Direct2D，macOS 上是 AppKit。 |

GUI 通过 `http://127.0.0.1:8645` 与本地 agent 通信，自身不内嵌 tailnet 节点。每台机器运行一个 agent。无论 agent 使用的是系统 tailscaled 还是内嵌的 tsnet 节点，GUI API 都相同。

Agent 需要 **Go 1.27.3**。

Native Shell 客户端是独立的 Rust crate（`gui-nsh/`）。它要求 **rustc >= 1.97**（edition 2024；`gui-nsh/Cargo.toml` 显式声明了 `rust-version = "1.97"`，`rust-toolchain.toml` 锁定为 1.97.0，已在 rustc 1.99.1 下验证测试）和 crates.io 上的 `nsh-core` / `nsh-base` / `nsh-platform-*` **0.3.0**。用户可通过 `rustc --version` 检查版本，必要时执行 `rustup update stable` 升级。日常构建直接使用 `cargo build` 即可，不强制安装 `nsh` CLI。Linux 编译需要 GTK 4 开发包；Windows 上使用 Win32/Direct2D，不需要 GTK。

## 不用 tailnet 先试一下

下面的命令都假定从仓库根目录开始。Linux / macOS：

```sh
cd agent
go build -o tailfile-agent ./cmd/tailfile-agent
./tailfile-agent -demo
```

Windows（PowerShell）：

```powershell
cd agent
go build -o tailfile-agent.exe ./cmd/tailfile-agent
.\tailfile-agent.exe -demo
```

**Windows 注意：** 用 `-o` 指定输出文件名时，Go 不会自动加 `.exe`。请在 `-o` 中写上 `.exe`。

`-demo` 会在进程内启动三个节点：`alpha` 和 `beta`（可写）以及 `gamma`（只读），所有者为 `ada@example.com`。不会加入任何 tailnet。API 位于 `http://127.0.0.1:8645`。`-demo` 会忽略 `-net`。

agent 在前台运行。接下来在第二个终端（从仓库根目录）启动 `gui-nsh`。

Linux / macOS：

```sh
cd gui-nsh
cargo build --release
./target/release/tailfile-nsh -agent http://127.0.0.1:8645
```

Windows（PowerShell）：

```powershell
cd gui-nsh
cargo build --release
.\target\release\tailfile-nsh.exe -agent http://127.0.0.1:8645
```

`cargo run --release -- -agent http://127.0.0.1:8645` 等价。如果 agent 使用了非默认状态目录，GUI 也要通过 `-state` 或 `-token-file` 指向同一目录；也可设置环境变量 `TAILFILE_AGENT`、`TAILFILE_STATE`、`TAILFILE_TOKEN_FILE`。

Linux 上先装编译依赖（Ubuntu 24.04 的 GTK 4.14 即可）：

```sh
sudo apt install pkg-config libgtk-4-dev clang
```

`scripts/build-all.sh` 是构建与测试脚本，会运行 agent 测试、`gui-nsh` 测试以及多平台交叉编译。

## 真实 tailnet

在每台机器上，选择 agent 可以读取的文件夹和可以写入的文件夹。没有 `-write` 列表时节点为只读。没有 `-read` 列表时，agent 会创建 `~/Tailfile`（权限 `0700`；Windows 上是 `%USERPROFILE%\Tailfile`，即 PowerShell 中的 `$HOME\Tailfile`）并且只共享该目录。Demo 模式不使用这个默认值，而是共享临时测试数据目录。

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

Windows（PowerShell）：

```powershell
# Prefer the machine's own tailscaled. No auth key.
.\tailfile-agent.exe -net system -read "$HOME\Tailfile" -write "$HOME\Tailfile"

# Force an embedded node.
$env:TS_AUTHKEY = 'tskey-auth-...'   # never commit this
.\tailfile-agent.exe -net tsnet -hostname my-laptop -read "$HOME\Tailfile" -write "$HOME\Tailfile"
```

如果给 agent 传了 `-hostname` 或 `-state`，状态目录（以及其中的 `gui-token`）就不在默认位置，启动 GUI 时需要用 `-state`（或 `-token-file`）指向同一个目录。

`-authkey` 参数仍然可用，但会打印警告。建议使用 `TS_AUTHKEY` 或 Settings 界面，这样 key 不会出现在 shell 历史和服务命令中。进程启动时的读取顺序是 `TS_AUTHKEY`，然后是 `-authkey`，最后是文件 `<state>/ts-authkey`。key 为空时，tsnet 会打印一个交互式登录 URL，用浏览器打开即可。保存或清除 key 不会重启已经运行的节点，需要重启 agent。本地 API 只返回是否配置了 key，从不返回 key 本身，key 也从不写入日志。

状态保存在用户配置目录下的 `tailfile/<hostname>`（权限 `0700`），或 `-state` 指定的位置。不要提交该目录。

| 操作系统 | 状态目录 |
| --- | --- |
| Linux | `~/.config/tailfile/<hostname>/` |
| macOS | `~/Library/Application Support/tailfile/<hostname>/` |
| Windows | `%AppData%\tailfile\<hostname>\` |

Windows 上 `%AppData%` 即 `os.UserConfigDir()` 返回的 Roaming 目录，通常是 `C:\Users\<用户名>\AppData\Roaming`，PowerShell 中为 `$env:APPDATA`。例如：`Get-ChildItem "$env:APPDATA\tailfile"`。

`<hostname>` 默认是 `tailfile-` 加上本机主机名。`gui-token` 在每次启动时重写（权限 `0600`；Windows 上 DACL 仅授权给当前用户）。`ts-authkey` 是保存的 auth key，权限相同，重启后保留。`jobs.json`、`transfers.json` 和 `service.json` 的权限也是 `0600`。System 模式不会创建 tailnet 节点密钥；`-hostname` 只用于命名状态目录。其他节点看到的名称是 tailscaled 原有的名称。

System 模式只在节点的 Tailscale IPv4 和 IPv6 地址（`100.64.0.0/10` 和 `fd7a:115c:a1e0::/48`）上监听 `-port`（8644），从不绑定 `0.0.0.0`。如果这些地址变化或 tailscaled 重启，agent 会丢弃旧 socket 并绑定新地址。对其他 agent 的出站调用是普通的系统拨号，目标为 `100.x` / `fd7a::`，不走用户态网络栈。主机防火墙必须允许 Tailscale 接口（Linux 上为 `tailscale0`）的入站 TCP 8644。tsnet 模式在进程内部监听，因此主机防火墙看不到该端口。

在 Linux 上，LocalAPI socket 通常只有 root 可访问。如果 system 模式（或 auto 模式的回退日志）提示无法读取 tailscaled，执行 `sudo tailscale set --operator=$USER`，然后以该用户身份重新启动 agent。在 macOS 上，auto 和 system 会同时尝试开源版 `tailscaled` 的 unix socket 以及 App Store / 独立版应用。

无论哪种模式，agent 还会在 localhost 的 `127.0.0.1:8645` 上监听。本地监听器拒绝 `127.0.0.1`、`localhost` 或 `::1` 以外的任何地址（`0.0.0.0:8645` 和 `:8645` 都无法启动）。不要对外公开 8645 端口。

每个 tailnet 请求都会通过 LocalAPI `WhoIs` 检查，无论该 LocalAPI 属于系统 tailscaled 还是内嵌的 tsnet 节点。在 agent 知道自己的 tailnet 用户之前，它会拒绝 tailnet 调用。之后只允许该用户访问，除非你通过 `-owner` 或 `TAILFILE_OWNER` 指定其他登录名。

Tailscale grants 可以收窄（以及共享）访问权限。如果 WhoIs 的 `CapMap` 包含 `iori.dev/cap/tailfile`，则强制执行该 grant，不再要求同一用户检查。每个值都是 JSON：

```json
{"read": ["/home/ada/Tailfile"], "write": ["/home/ada/Tailfile/inbox"]}
```

grant 中的路径会与本节点的 `-read` 和 `-write` 列表取交集。列表之外的路径会被丢弃，因此 grant 无法扩大节点的共享范围。比已配置根目录更宽的 grant 会保留已配置的根目录。没有该 capability 时回退到同一用户规则。capability 存在但为空，或不是合法 JSON 时，一律拒绝（fail closed）。

localhost API 是唯一可以导入、缓存或显示路径的地方。它不使用 WhoIs，而是要求每次 agent 启动时生成的 bearer token（32 个随机字节，十六进制），以权限 `0600` 写入 `<state dir>/gui-token`。日志行会给出该路径，但不打印 token。`gui-nsh` 读取该文件（或通过 `-token-file` / `-state` 指定），不设置 `Origin`（空 Origin 加上 bearer 是 agent 允许的原生客户端路径），请求走本机 HTTP。请求必须带 `Authorization: Bearer …`，`Host` 必须是回环地址。

Settings 仅限本地。`GET /v1/local/settings` 返回解析后的网络模式（`system`、`tsnet` 或 `demo`）、请求的 `-net` 值以及 `authKeyConfigured`。`PUT /v1/local/settings/authkey` 携带 `{"authKey":"..."}` 会写入 `<state>/ts-authkey`。`DELETE /v1/local/settings/authkey` 会删除它。这两个写操作的响应都会设置 `restartRequired`，且不回显 key。tailnet 监听器不提供这些路由。

`/v1/local/import` 只接受位于读取根目录内的系统路径（包括解析符号链接之后）。根目录以外的路径会被拒绝。

复制是两个节点之一上的一个任务（job）。**Push** 表示源端读取自己的磁盘并写入目标端。**Pull** 表示目标端读取源端并写入自己的磁盘。分块大小为 1 MiB，每块有各自的 SHA-256。partial 文件（`*.tailfile-partial`）及其 meta（`*.tailfile-meta`，权限 `0600`）记录源文件大小、源文件 mtime，以及完成时的整文件 SHA-256。

当目标路径已存在且是文件时，任务暂停，状态为 `conflict`。GUI 会询问 覆盖（overwrite）或 跳过（skip）。勾选 对后续冲突都这样处理 会把该选择保存为同一任务中后续文件的策略。没有明确选择时绝不替换文件。待处理的冲突、逐文件选择和"应用到后续"策略都保存在 `jobs.json` 中，因此重启后仍然有效。通过 `POST /v1/transfers/{id}/conflict`（GUI 发起的 push 或 pull）或 `POST /v1/jobs/{id}/conflict`（本地导入任务）作答：

```json
{"action": "overwrite", "applyToRest": false}
```

`action` 为 `overwrite` 或 `skip`。任务和传输记录保存在状态目录的 `jobs.json` 和 `transfers.json`（权限 `0600`）中，因此 agent 进程重启后 Resume 仍然可用。

节点不会提权。它不能 sudo，也不会弹出 UAC 提示。它只能看到启动它的用户能看到的文件。

## 按用户安装的服务

```sh
tailfile-agent service install|uninstall|start|stop|status [flags]
```

这只为当前用户安装服务，不需要 root 或管理员。`--system` 会被拒绝。

### Linux

用户 unit 为 `~/.config/systemd/user/tailfile-agent.service`。安装会执行 `systemctl --user enable` 和 daemon-reload。如果希望在没有人登录时也保持用户管理器（以及 agent）运行：

```sh
loginctl enable-linger "$USER"
```

### macOS

LaunchAgent 为 `~/Library/LaunchAgents/tailfile-agent.plist`，带 `RunAtLoad`。没有 LaunchDaemon，也不需要 `sudo`。

### Windows

注册一个名为 `TailfileAgent` 的 Task Scheduler 登录任务（`schtasks /Create /XML`）：`RunLevel` 为 LeastPrivilege，交互式登录触发器，无执行时间限制。

```sh
./tailfile-agent service install -net system -read "$HOME/Tailfile" -write "$HOME/Tailfile"
./tailfile-agent service status
./tailfile-agent service start
```

Windows（PowerShell，不需要以管理员身份运行）：

```powershell
.\tailfile-agent.exe service install -net system -read "$HOME\Tailfile" -write "$HOME\Tailfile"
.\tailfile-agent.exe service status
.\tailfile-agent.exe service start
```

## 各操作系统的构建

### Windows

- 从 [go.dev](https://go.dev/dl/) 安装 Go 1.27.3，构建 `tailfile-agent.exe`。
- 安装 [rustup](https://rustup.rs)，确保 **rustc >= 1.97**（可执行 `rustc --version` 检查，必要时 `rustup update stable`）。
- 在 `gui-nsh` 中执行 `cargo build --release`，产物为 `gui-nsh\target\release\tailfile-nsh.exe`（已通过 `build.rs` 内嵌 Tailfile 图标资源）。窗口使用 Win32 + Direct2D 原生渲染，无需 GTK 或 WebView2。

### macOS

- 安装 Go 1.27.3、Xcode 命令行工具与 Rust（>= 1.97）：

  ```sh
  cd gui-nsh && cargo build --release
  ./target/release/tailfile-nsh -agent http://127.0.0.1:8645
  ```

### Ubuntu / Debian

```sh
sudo apt install pkg-config libgtk-4-dev clang
cd gui-nsh && cargo build --release
```

### Arch / Omarchy（Hyprland，Wayland）

```sh
sudo pacman -S --needed base-devel gtk4 pkgconf clang
cd gui-nsh && cargo build --release
```

## 本地文件缓存与拖入导入

`gui-nsh` 支持把远程文件下载到本地缓存（`POST /v1/local/cache`），然后点击「在文件夹中显示」（`POST /v1/local/reveal`：macOS 上用 Finder，Windows 上用资源管理器，Linux 上对父目录执行 `xdg-open`）。缓存位于 `os.UserCacheDir()/tailfile`（权限 `0700`）。同时支持将系统本地文件直接拖入文件列表（`on_file_drop`）或通过「更多 -> 导入本地路径…」导入到当前远程目录。

## sing-box（TUN）与 Tailscale

- `100.64.0.0/10` 与 `fd7a:115c:a1e0::/48` 网段需设为 **direct**（直连）。
- `-net tsnet` 时请为 `tailfile-agent` 设置进程直连。

## 测试与形式化验证

在仓库根目录执行。Linux / macOS：

```sh
(cd agent && go test -count=1 -race .)
(cd gui-nsh && cargo test --lib --test demo_agent)
```

Windows（PowerShell）：

```powershell
Push-Location agent; go test -count=1 .; Pop-Location
Push-Location gui-nsh; cargo test --lib --test demo_agent; Pop-Location
```

此外，`gui-nsh/tests/formal_model_check.rs` 提供了显式状态广度优先搜索（BFS）模型检验器与有界路径输入验证（详见 `.cursor/skills/verify-gui-nsh-state-machine/SKILL.md`）：

```sh
cargo test --manifest-path gui-nsh/Cargo.toml --test formal_model_check -- --nocapture
```

## Native Shell UI 设计说明

详见 [docs/NSH_UI.md](docs/NSH_UI.md) 与 [docs/TESTING.md](docs/TESTING.md)。
