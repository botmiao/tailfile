# Architect：nsh 批准 UI（feat/nsh-approved-ui）

## Problem

把 `/workspace/tmp/tailfile-ux-ref` 批准的 Explorer/Finder 交互落到现有 `gui-nsh`（Native Shell 0.3）与 Go `agent`，保持 A→B 直连复制与白名单安全模型。agent 已有 mkdir/rename，缺 delete；gui-nsh 是单目录原型，缺标签页、主题持久化、内联新建/重命名、删除确认、daemon 安装引导。不能嵌入 HTML。

## Usage（调用方视角）

- GUI：`cargo run -p tailfile-nsh -- --demo`（或已有 agent）打开单窗格；左侧节点树；顶栏标签（主机 › 目录）；工具栏/面包屑/筛选；右键/快捷键新建文件夹、F2 重命名、Delete 确认后永久删除；跨主机复制粘贴沿用 覆盖/跳过；设置保留 auth-key 并加外观；daemon 不可达时展示可复制的安装命令。
- Agent：`POST /v1/fs/delete` JSON `{node?, path}`，鉴权同 mkdir；写白名单内、禁止删配置根、防穿越/符号链接逃逸。
- 客户端：`Client::delete(node, path)`；演示 `-demo` 可测全流程。

## Shape

**选定：Tab 嵌在 `Ui`（候选 A）**  
`Ui.tabs: Vec<TabState>` + `active_tab`；`TabState` 持有 host/path、历史、选择、排序/筛选/视图、滚动占位。树点击按原型规则聚焦或开标签。持久化 `state_dir/nsh-ui.json`（0600）。

删除落在 `fs.delete` → HTTP `POST /v1/fs/delete` → `NodeAPI::Delete`（direct+http），与 mkdir/rename 对称。

主题：应用内 `ThemePref = System|Light|Dark`，写入同一 JSON；启动与变更时 `ambient::set_color_scheme`。图标用内联 Unicode/简易 canvas 点阵 F + 文本动作按钮（Lucide 许可声明放入 `THIRD_PARTY_NOTICES`）；资源 SVG 放入 `gui-nsh/assets/`。

Daemon 安装：仅 UI 文案与剪贴板复制，不 SSH、不改远端。

## Synthesis decision

候选 A（Ui 内嵌 tabs）相对候选 B（独立 TabStore Signal）：更少跨 signal 同步，贴合现有 `Signal<Ui>` 模型。候选 B 在并行刷新时更清晰，但会迫使几乎每个 handler 双写，短期成本更高。主题不 fork nsh Theme 结构，只切换 Appearance。delete 用 POST+JSON 而非 REST DELETE+query，与现有 mkdir/rename 一致，避免双路径编码。

## Tradeoffs accepted

- 接受图标以文本/简易矢量为主，完整 Lucide 打包留后续。
- 接受移动端仅文档与布局预留，不宣称可用。
- 接受内联编辑在 nsh 输入能力范围内用对话框/行内 input 近似原型。
- 删除为永久删除，无回收站。

## Alternatives considered

- 嵌入 WebView 跑原型 HTML：违背“native nsh”要求。
- 独立 TabStore：接口更深、调用方需记住双状态。
- REST `DELETE /v1/fs?path=`：与现有 POST 主体风格不一致。

## Open questions and risks

- nsh 对键盘快捷键/上下文菜单的平台差异是否覆盖 Cmd vs Ctrl。
- 图标视图在列表组件上的密度是否够用。

## Next implementation step

先落地 agent `Delete` + 测试，再扩 `api.rs`/`persist`/`model`/`ui`。

## Attribution

agy 因 *User location is not supported* 立即失败（0 token）；本设计与实现由 executor 完成。
