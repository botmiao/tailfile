# Implementation notes — feat/nsh-approved-ui

## Attribution

- **agy**：启动即失败 `FAILED_PRECONDITION (code 400): User location is not supported for the API use.`（0 token，无代码产出）。日志：`/workspace/tmp/logs/agy-nsh-approved-ui.jsonl`。
- **未使用 CloudAgent**（任务明确禁止在 location 失败时切换）。
- **全部实现代码由 executor（Grok Bot）编写**，含 architect 文档与后续提交。

## Scope kept

- 未改写 gui-mygo / gui-wails。
- 保留既有复制冲突语义与安全模型；仅新增 delete 与 nsh UI 能力。

## PR #3 gap fix (resume)

- Agent: explicit `POST /v1/fs/{mkdir,rename,delete}`; GUI `Client.mkdir/rename/delete`. `deletePath` re-resolves + refuses configured roots; symlink remove-link-not-target. Tests in `fs_mutate_test.go` + `delete_test.go`.
- Multi-item paste loops `clip_items` with aggregated start counts / partial errors; conflicts still surface via existing transfer poll.
- Host Ctrl/Cmd+click and context-menu open new tab (`click_tree_new_tab`); tabs persist via `nsh-ui.json` (nav/selection/view per tab).
- Icon view: `flex_wrap` grid tiles (emoji folder/file) using `visible_entries` original indices.
- Native Xvfb screenshots under `docs/screenshots/nsh-native/` (ffmpeg x11grab). Settings dialog click under headless Xvfb was unreliable; delete confirm captured.
- agy retry: `FAILED_PRECONDITION` location-not-supported — executor continued (no CloudAgent).

## Title bar polish (after 0c844d7)

- Tabs, logo, and theme/settings share one row. `set_title_bar(TitleBarStyle::Hidden)` drops the system title text; GTK/Windows/macOS window buttons stay in the insets from `title_bar()`.
- `cargo run` injects `nsh.json` `theme.cssVars` via `Root::tokens`. Row selection uses `selection`; the left bar and the active-tab underline use `primary`. The dotted F stays orange.
- Each tab and tree node shows one of five mono silhouettes from the node's `os`: Apple, Windows flag, Tux, Android robot, iPhone. Lucide has none of those marks. Demo nodes are macos, windows, linux, android, and ios.
- Paste is disabled while the clipboard is empty. Breadcrumbs are the host plus the path under that node's read root.
- Screenshots: `docs/screenshots/nsh-native/light-main.png`, `dark-main.png`, `delete-confirm.png`.
- Title-bar mark is the app icon from `gui-nsh/assets/tailfile-app-icon.svg` (squircle `#fb923c`→`#e2510b`, warm-white dotted F), not the bare wordmark F.

