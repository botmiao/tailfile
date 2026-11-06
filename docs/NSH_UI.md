# Tailfile Native Shell（gui-nsh）批准 UI

本文描述已落地的桌面 MVP（Windows/Linux 优先）。交互以 `ux-prototype` 批准稿为准，实现为 **原生 nsh Rust**，不嵌入 HTML。

## 布局

- 单窗格：左侧节点树常驻；右侧地址/列表 + 可开关预览。
- 标题栏：`set_title_bar(Hidden)` 去掉系统标题文字，橙色点阵 **F**、应用名、主机标签、主题和设置画在同一行。系统窗口按钮留在 `title_bar()` 报告的左右留白里。每个标签绑定「主机 + 当前目录」，带该主机的 OS 字形（Apple / Windows 旗 / Tux / Android 机器人 / iPhone，单色跟随主题），当前标签为蓝色下划线。
- 工具栏：新建文件夹、复制、粘贴（剪贴板空时禁用）、重命名、删除、刷新；右侧为排序与视图。后退/前进/上一级在面包屑行。推送/拉取在「更多」。
- 状态栏：可见项数、剪贴板摘要、错误信息。
- 设置：外观（跟随系统/浅色/深色）+ 既有 auth-key 行为。

## 标签与树

- 点击节点/目录：聚焦该主机已有标签或打开新标签。
- 节点行只显示 OS 字形、状态点和「只读」（只读节点），不再写出 macOS / Windows 等系统名。
- 标签页从侧栏右缘开始，和文件列表对齐。标题栏里同一条竖线接到下方 Tailnet 分隔线上。Logo 栏右侧的折叠按钮把侧栏收成主机图标轨；折叠后不再显示折叠按钮，点击左上角 Logo 再次展开；选择记在 `nsh-ui.json` 的 `sidebarCollapsed`。
- 关闭窗口会结束 GUI 进程本身（没有托盘）。agent 是另一个进程，不受影响。
- `click_tree_new_tab` 预留强制新标签（后续接 Ctrl/中键）。
- 标签与主题持久化到 agent `state_dir/nsh-ui.json`（文件 0600，目录 0700）。

## 新建 / 重命名 / 删除

- 新建文件夹：内联输入，校验空名、`.`/`..`、非法字符、重名；只读目录禁用。
- 重命名：F2 / 工具栏，校验同上。
- 删除：确认框默认「取消」，写明主机与路径、永久删除且无回收站；调用 `POST /v1/fs/delete`。

## 复制粘贴

- 保持 A→B 直连与冲突「覆盖 / 跳过 / 对后续冲突都这样处理」；**无保留两者**。
- 可清空应用内剪贴板。

## 远程 daemon 安装（客户端引导）

当节点离线或 `agent_ok=false` 时弹出安装说明，按 Linux/Windows/macOS 生成可复制命令。

- **不会**自动 SSH，也**不会**改远程机器。
- Tailscale SSH 一键安装明确为未来项。

## 主题与图标

- `ThemePref` → `set_color_scheme`（System/Light/Dark）。
- `nsh.json` 的 `theme.cssVars` 在 `cargo run` 时经 `Root::tokens` 注入（CLI `nsh run` 自己读这份文件）。选中行用 `selection`，强调色用 `primary`（蓝）；标志 F 仍是橙色。
- 标题栏标志是应用图标：圆角橙方块（`#fb923c` → `#e2510b`）加暖白点阵 F，对齐 `gui-nsh/assets/tailfile-app-icon.svg`。工具栏用 Lucide 路径。OS 标记是五种单色剪影（Apple、Windows 旗、Tux、Android 机器人、iPhone），因为 Lucide 没有这些标志。ISC/MIT 声明见 `icons::THIRD_PARTY_NOTICES`。

## 移动端 / 响应式计划

- **当前不宣称完整移动支持。**
- 预留：窄宽时节点树改抽屉、详情改底栏、行尾操作菜单（对齐原型 ≤820px）。
- 桌面优先 Windows/Linux；macOS 在可用工具链上编译。

## Agent API 增补

- `POST /v1/fs/delete` `{ "node"?: string, "path": string }`
- 鉴权、写白名单、防穿越/符号链接逃逸；**禁止删除配置的读写根**；只读根拒绝写入类错误。

## 已知限制

- 多选主要靠后续 Ctrl 接线；当前删除可基于单选或 `selected_paths`。
- 粘贴多条目时客户端仍以首项启动传输（与既有 transfer API 对齐），完整多选粘贴可后续扩展。
- 图标视图切换已接线，网格布局仍偏列表密度。
- GUI 自动化截图依赖本机显示/Xvfb。
