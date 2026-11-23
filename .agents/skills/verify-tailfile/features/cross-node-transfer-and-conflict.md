# Cross-node transfers and conflict resolution

Cross-node transfers and conflict resolution lets a user copy files on one node, paste them onto another writable node, monitor live transfer status, resolve overwrite conflicts via a non-dismissible dialog, and clear completed transfer records.

## Sub-features

- `xfer-copy-paste` copies selected items (`alpha/hello.txt`) and pastes them onto a writable peer (`beta`), completing asynchronously via chunked transfer.
- `xfer-conflict-dialog` prompts with `目标文件已存在` when pasting onto an existing file and refuses Escape dismissal until the user picks `覆盖`, `跳过`, or `取消任务`.
- `xfer-clear-history` removes finished transfer rows via `清除记录` (`clear-transfers`) or single-row `移除记录` (`tr-dismiss-...`), leaving an empty transfer drawer.

## How to get to it (user POV)

- Select one or more files on a source node (`alpha`), click `复制` (`tb-copy`, `Ctrl+C`, or Details `det-copy`).
- Switch to a writable destination node or folder (`beta`), click `粘贴` (`tb-paste` or `Ctrl+V`).
- Watch the bottom transfer queue (`传输队列：...`) update from `进行中` to `已完成`.
- When pasting a file whose destination path already exists, respond to the modal (`跳过`, `覆盖`, or `取消任务`).
- Click `清除记录` in the transfer bar header to clear finished entries.

## Driving it with control-tailfile

Preconditions:

- Isolated verification run `<run-id>` is healthy (`doctor --run <run-id>` passes) in `attached` mode.
- `alpha` has `hello.txt` and `beta` does not yet have `hello.txt` (only `inbox.txt`).
- Query `/v1/info?node=beta` via `api` to obtain `<betaRoot>`.

- **Copy source file (`xfer-copy-paste`).** Select `hello.txt` on `alpha` and click Copy.
  1. `control-tailfile click --run <run-id> --role treeItem --label alpha`
  2. `control-tailfile wait --run <run-id> --contains "hello.txt"`
  3. `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "hello.txt"`
  4. `control-tailfile act --run <run-id> --key tb-copy --action press`
- **Paste onto `beta` and observe completion.** Switch to `beta`, paste, and wait for completion + API confirmation.
  1. `control-tailfile click --run <run-id> --role treeItem --label beta`
  2. `control-tailfile wait --run <run-id> --contains "inbox.txt"`
  3. `control-tailfile act --run <run-id> --key tb-paste --action press`
  4. `control-tailfile wait --run <run-id> --contains "已完成"`
  5. Capture completion UI + API proof: `control-tailfile screenshot --run <run-id> --out .verify-proof/<run-id>/transfers/01-completed.png` and `control-tailfile api --run <run-id> --path "/v1/fs/list?node=beta&path=<betaRoot>" --out .verify-proof/<run-id>/transfers/01-beta-list.json` (contains `hello.txt`).
- **Trigger and verify non-dismissible conflict (`xfer-conflict-dialog`).** Paste `hello.txt` into `beta` a second time, verify `Escape` cannot dismiss the modal, then choose `覆盖`.
  1. `control-tailfile act --run <run-id> --key tb-paste --action press`
  2. `control-tailfile wait --run <run-id> --key conflict-overwrite`
  3. Send Escape and assert dialog stays open: `control-tailfile key --run <run-id> --key Escape` and `control-tailfile wait --run <run-id> --key conflict-overwrite`
  4. Capture conflict modal: `control-tailfile screenshot --run <run-id> --out .verify-proof/<run-id>/transfers/02-conflict-modal.png`
  5. Resolve conflict: `control-tailfile act --run <run-id> --key conflict-overwrite --action press`
  6. `control-tailfile wait --run <run-id> --absent-key conflict-overwrite` and `control-tailfile wait --run <run-id> --contains "已完成"`
- **Clear finished records (`xfer-clear-history`).** Click `清除记录` and verify transfer bar disappears and `hello.txt` remains on `beta`.
  1. `control-tailfile act --run <run-id> --key clear-transfers --action press`
  2. `control-tailfile wait --run <run-id> --absent "传输队列"`
  3. Verify backend queue empty via `control-tailfile api --run <run-id> --path "/v1/transfers" --out .verify-proof/<run-id>/transfers/03-transfers-cleared.json` (`"transfers": []`).

## Gotchas

- Pasting onto read-only nodes (`gamma`, `pixel`, `phone`) keeps `tb-paste` disabled; test cross-node paste into `beta`.
- On Windows, pass `<betaRoot>` with forward slashes (`C:/Users/.../beta`) in `api --path "/v1/fs/list?..."` query strings to avoid backslash URL escaping issues.
- `hello.txt` is 71 bytes (`Hello from alpha...`), so `"传输中"` may flash to `"已完成"` within one frame. To verify in-flight `"传输中"` or `"取消"` / `"恢复"`, copy `alpha/notes/payload.bin` (2 MiB) instead.
- If a run cleans up without deleting transferred files, the temp `<runDir>` fixtures vanish at cleanup anyway; within the same run, delete `beta/hello.txt` if subsequent features expect clean `beta`.
