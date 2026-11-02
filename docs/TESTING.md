# 手动测试清单

自动化覆盖由 `agent` 和 `gui-mygo` 中的 `go test` 完成。本清单是由人在真实机器上走一遍的测试。请以普通用户身份进行。这里的任何步骤都不应要求 sudo、管理员密码或 UAC 提示。如果可以在 Settings 中填写 auth key，就不要把它粘贴到会记录历史的 shell 里。日志必须给出 `gui-token` 和 `ts-authkey` 的路径，且不得打印二者的值。

Windows 上请用 PowerShell，并把下文的 `./tailfile-agent` 换成 `.\tailfile-agent.exe`（构建时用 `go build -o tailfile-agent.exe ./cmd/tailfile-agent`，Go 不会自动加 `.exe`）。`~/Tailfile` 在 Windows 上是 `%USERPROFILE%\Tailfile`（PowerShell：`$HOME\Tailfile`）。

Native Shell 与另外两个 GUI 读同一个 `gui-token`。agent 在第一个终端跑起来之后，在仓库根目录开第二个终端：

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

如果 agent 用了 `-state` 或 `-hostname`，这里加上同一个 `-state`。界面是中文。冲突对话框是「目标已有同名文件」「覆盖」「跳过」「对后续冲突都这样处理」。设置里的 auth key 输入框不回显已保存的 key。

下文中的 `<hostname>` 是 `tailfile-` 加上本机主机名，除非你传了 `-hostname`。

## 状态文件

| 操作系统 | 状态目录 |
| --- | --- |
| Linux | `~/.config/tailfile/<hostname>/` |
| macOS | `~/Library/Application Support/tailfile/<hostname>/` |
| Windows | `%AppData%\tailfile\<hostname>\`（PowerShell：`"$env:APPDATA\tailfile\<hostname>"`） |

- [ ] 不带 `-state` 启动后，`gui-token` 位于该目录，权限为 `0600`（Windows：文件的 DACL 授权给当前用户，不授权给其他用户）。
- [ ] agent 日志打印 `gui-token` 的路径，而不是 token 本身。
- [ ] 重启 agent 会更换 token。之前已打开的 GUI 会失败，直到重启并读取新文件。
- [ ] 在保存之前 `ts-authkey` 不存在。保存后其权限为 `0600`（Windows：仅当前用户的 DACL），并且仍不会出现在日志和 `GET /v1/local/settings` 中。

## 默认共享

- [ ] 不带 `-read` 和 `-write` 启动。agent 创建权限为 `0700` 的 `~/Tailfile`，并且只共享该目录。
- [ ] 粘贴到该文件夹可以成功。粘贴到该文件夹以外的路径会被拒绝。
- [ ] `-demo` 不会创建 `~/Tailfile`。Demo 共享其临时测试数据目录。

## 系统 tailscale 模式

需要一个已经以你的身份登录的 tailscaled。不需要 auth key。

```sh
./tailfile-agent -net system -read "$HOME/Tailfile" -write "$HOME/Tailfile"
```

Windows（PowerShell）：

```powershell
.\tailfile-agent.exe -net system -read "$HOME\Tailfile" -write "$HOME\Tailfile"
```

- [ ] 日志显示 system 模式，而不是 tsnet。`GET /v1/local/settings`（bearer 取自 `gui-token`）显示 `"net": "system"`。
- [ ] 监听地址是该节点的 Tailscale 地址、端口 8644，而不是 `0.0.0.0`。
- [ ] 同一 tailnet、同一 tailnet 用户下的第二台机器能列出此节点，并能读取 `~/Tailfile`。
- [ ] 其他 tailnet 用户会被拒绝。
- [ ] 在 Linux 上，如果日志提示 LocalAPI socket 不可读，执行 `sudo tailscale set --operator=$USER`，然后以该用户身份重新启动 agent。agent 本身仍不得用 sudo 运行。
- [ ] 当 tailscaled 处于 Running 状态且有 Tailscale IP 时，`-net auto` 选择 system，否则选择 tsnet。日志会说明选了哪个以及原因。
- [ ] 退出 tailscaled，或拿掉它的 IP，确认 agent 会重新绑定或退出，而不是在非 Tailscale 地址上监听。

## 按用户安装的服务

使用你希望后台进程保留的同一组参数。示例：

```sh
./tailfile-agent service install -net system -read "$HOME/Tailfile" -write "$HOME/Tailfile"
./tailfile-agent service start
./tailfile-agent service status
```

Windows（PowerShell）：

```powershell
.\tailfile-agent.exe service install -net system -read "$HOME\Tailfile" -write "$HOME\Tailfile"
.\tailfile-agent.exe service start
.\tailfile-agent.exe service status
```

- [ ] 安装时输出一行按用户安装成功的信息，并且不提示输入密码。
- [ ] 状态目录中的 `service.json` 含有 `"scope": "user"`、可执行文件路径和参数，不含 auth key。
- [ ] 当你省略 `-hostname`、`-state`、`-net` 时，unit 或任务中也包含解析后的这些值；省略 `-read` 时，包含指向 `~/Tailfile` 的 `-read`。
- [ ] 第二次 `service install` 会失败，并提示先卸载。
- [ ] `./tailfile-agent service install --system` 会失败，且不会写入系统 unit、LaunchDaemon 或 Windows 服务。
- [ ] 先 `service stop` 再 `service status`，报告已停止。`service uninstall` 删除服务定义，`service.json` 也被删除。卸载不会删除 `ts-authkey`。

### Linux

- [ ] unit 为 `~/.config/systemd/user/tailfile-agent.service`，由 `default.target` 拉起（wanted by）。不存在 `/etc/systemd/system/tailfile-agent.service`。
- [ ] 注销后重新登录。agent 无需手动启动即在运行。
- [ ] 执行 `loginctl enable-linger "$USER"`，然后重启且不登录。用户管理器仍会运行 agent。如果不需要这种行为，事后关闭 linger。

### macOS

- [ ] plist 为 `~/Library/LaunchAgents/tailfile-agent.plist`。`/Library/LaunchDaemons` 下没有 plist。
- [ ] 仅安装不会启动进程。`service start` 会加载它（`launchctl list` 显示 `tailfile-agent`）。
- [ ] `service stop` 会卸载它。下次登录时不会自动恢复，直到执行 `service start`。

### Windows

- [ ] 二进制是 `tailfile-agent.exe`（没有扩展名的文件运行时会弹出"选择应用以打开"对话框）。
- [ ] Task Scheduler 中有一个属于当前用户、名为 `TailfileAgent` 的任务。它不会出现在 `services.msc` 中。
- [ ] 该任务的运行级别为 Least Privilege。创建时没有出现 UAC。
- [ ] 注销后重新登录。agent 以当前用户身份启动。
- [ ] `service start`、`service stop` 和 `service status` 分别与 `schtasks /Run`、`/End` 和 `/Query` 一致（例如 `schtasks /Query /TN TailfileAgent`）。

## 冲突提示

在两个节点上（或一个节点加一次本地导入），把文件复制到一个已存在同名、内容不同且已完成的文件的路径上。

- [ ] 传输停在 `conflict`。目标原有字节不变。Wails、MyGo 和 Native Shell 都显示 目标已有同名文件、路径、覆盖、跳过 以及 对后续冲突都这样处理。
- [ ] 选择 跳过 后目标不变，任务完成。
- [ ] 重复一次，选择 覆盖。目标变为源文件的字节。复制过程中旧文件仍是旧字节（新字节在 `*.tailfile-partial` 中，直到哈希校验通过）。
- [ ] 复制一个包含两个冲突文件的目录。在第一个冲突时勾选 对后续冲突都这样处理 并选择 跳过。第二个会被跳过，且不会再弹出对话框。
- [ ] 在对话框打开时停止 agent，然后再次启动。任务仍是 `conflict`（而不是 `interrupted`），对话框会重新出现。作答后复制完成。Native Shell 在点「刷新」时重新读 `gui-token`；agent 重启后先点刷新，不必为了新 token 单独重启窗口。
- [ ] 在文件复制到一半时中断，留下一个 meta 与源文件大小和 mtime 一致的 partial，同时在目标处留一个已完成的文件。恢复传输。不会出现对话框，且只有在哈希匹配后才替换已完成的文件。
- [ ] 从对话框中取消（使用传输的 Cancel 控件，而不是 覆盖 或 跳过）。目标不变。

## Auth key 设置

使用一个可丢弃的 tsnet auth key。不要使用你无法吊销的 key。

- [ ] 三个 GUI 的 Settings 都显示当前网络模式（`system`、`tsnet` 或 `demo`；当请求的 `-net` 不同时也会显示它）。Native Shell 的文案是「网络模式」。
- [ ] 保存之前，界面显示未配置 key。`GET /v1/local/settings` 返回 `"authKeyConfigured": false`，且没有 key 字段。
- [ ] 粘贴 key 并保存。GUI 提示需要重启。响应包含 `configured: true` 和 `restartRequired: true`，且不含 key。磁盘上的 `ts-authkey` 与你粘贴的 key 一致。agent 日志中没有该 key。
- [ ] 使用 `-net tsnet` 且不设置 `TS_AUTHKEY` 重启。日志说明 key 来自文件，且不打印它。节点加入 tailnet。
- [ ] export 一个不同的 `TS_AUTHKEY` 并重启（Windows PowerShell：`$env:TS_AUTHKEY = '...'`）。日志显示来源为 `TS_AUTHKEY`。文件不变。节点使用环境变量中的值。
- [ ] 在 Settings 中清除，然后在不设置 `TS_AUTHKEY`、不带 `-authkey` 的情况下重启。文件已删除。tsnet 打印登录 URL，而不是使用旧 key。
- [ ] 在 tailnet 端口（8644）上 `GET` 设置 URL。返回的不是设置文档。
