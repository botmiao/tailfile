# Implementation notes — feat/nsh-approved-ui

## Attribution

- **agy**：启动即失败 `FAILED_PRECONDITION (code 400): User location is not supported for the API use.`（0 token，无代码产出）。日志：`/workspace/tmp/logs/agy-nsh-approved-ui.jsonl`。
- **未使用 CloudAgent**（任务明确禁止在 location 失败时切换）。
- **全部实现代码由 executor（Grok Bot）编写**，含 architect 文档与后续提交。

## Scope kept

- 未改写 gui-mygo / gui-wails。
- 保留既有复制冲突语义与安全模型；仅新增 delete 与 nsh UI 能力。
