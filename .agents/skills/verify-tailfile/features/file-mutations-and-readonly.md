# File mutations and read-only gating

File mutations and read-only gating lets a user create folders, rename items, and permanently delete items (with confirmation) on writable devices, while disabling all write actions on read-only devices.

## Sub-features

- `mut-mkdir` creates a new subfolder inside the active writable directory via the top inline editor bar.
- `mut-rename` renames a single selected entry in-place directly inside its file row or icon tile via the toolbar rename button (`tb-rename`), `F2`, right-click context menu, or Details pane `det-rename` (no top banner bar).
- `mut-delete` confirms deletion in `ModalDelete` and removes the entry from UI and agent disk.
- `mut-readonly-gate` disables `tb-mkdir`, `tb-rename`, `tb-delete`, and `det-rename`/`det-delete` on read-only nodes such as `gamma`.

## How to get to it (user POV)

- Select a writable node (`alpha` or `beta`), click `新建文件夹` in the toolbar (`tb-mkdir`), enter a folder name, and press `创建` (or `Enter`).
- Select a single item in the file list or icon grid, click the rename cursor icon (`tb-rename`), press `F2`, or click `重命名` in the Details pane (`det-rename`), edit the name in-place directly inside the row/tile, and press `Enter` (or click away to commit, `Escape` to cancel).
- Select one or more items, click `删除` (`tb-delete`, `Delete`, or `det-delete`), and confirm deletion in the `确认删除` modal (`del-confirm`).
- Select read-only device `gamma` in the left sidebar and inspect toolbar button states and status bar permission badge (`gamma (只读)`).

## Driving it with control-tailfile

Preconditions:

- Isolated verification run `<run-id>` is healthy (`doctor --run <run-id>` passes) in `attached` mode (`alpha` selected, no folder named `verify-dir` or `verify-renamed`).
- Query `alpha`'s root path once via `control-tailfile api --run <run-id> --path "/v1/info?node=alpha" --out .verify-proof/<run-id>/file-mutations/alpha-info.json` so you know `<alphaRoot>` for read-only API checks.

- **Create folder (`mut-mkdir`).** Open the inline editor, type `verify-dir`, capture action evidence, and submit.
  1. `control-tailfile act --run <run-id> --key tb-mkdir --action press`
  2. `control-tailfile wait --run <run-id> --key inline-editor-input`
  3. `control-tailfile act --run <run-id> --key inline-editor-input --action setValue --text "verify-dir"`
  4. Capture action dialog: `control-tailfile screenshot --run <run-id> --out .verify-proof/<run-id>/file-mutations/01-mkdir-editor.png`
  5. `control-tailfile act --run <run-id> --key inline-editor-submit --action press`
  6. `control-tailfile wait --run <run-id> --contains "verify-dir"`
  7. Verify disk/API side effect: `control-tailfile api --run <run-id> --path "/v1/fs/list?node=alpha&path=<alphaRoot>" --out .verify-proof/<run-id>/file-mutations/01-mkdir-api.json` shows `"name": "verify-dir"` and `"isDir": true`.
- **Rename folder (`mut-rename`).** Select `verify-dir`, rename it in-place to `verify-renamed`, and verify via UI + API.
  1. `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "verify-dir"`
  2. `control-tailfile act --run <run-id> --key tb-rename --action press`
  3. `control-tailfile wait --run <run-id> --key inline-editor-input`
  4. `control-tailfile act --run <run-id> --key inline-editor-input --action setValue --text "verify-renamed"`
  5. `control-tailfile key --run <run-id> --key Enter`
  6. `control-tailfile wait --run <run-id> --contains "verify-renamed"` and `control-tailfile wait --run <run-id> --absent "verify-dir"`
  7. Capture UI + API proof: `control-tailfile screenshot --run <run-id> --out .verify-proof/<run-id>/file-mutations/02-renamed.png`, `control-tailfile snapshot --run <run-id> --out .verify-proof/<run-id>/file-mutations/02-renamed.aria.txt`, and `control-tailfile api --run <run-id> --path "/v1/fs/list?node=alpha&path=<alphaRoot>" --out .verify-proof/<run-id>/file-mutations/02-renamed-api.json`.
- **Delete folder with confirmation (`mut-delete`).** Open delete modal, capture modal, confirm, and verify removal.
  1. `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "verify-renamed"`
  2. `control-tailfile act --run <run-id> --key tb-delete --action press`
  3. `control-tailfile wait --run <run-id> --key del-confirm`
  4. Capture modal: `control-tailfile screenshot --run <run-id> --out .verify-proof/<run-id>/file-mutations/03-delete-modal.png`
  5. `control-tailfile act --run <run-id> --key del-confirm --action press`
  6. `control-tailfile wait --run <run-id> --absent "verify-renamed"`
  7. Verify disk/API side effect: `control-tailfile api --run <run-id> --path "/v1/fs/list?node=alpha&path=<alphaRoot>" --out .verify-proof/<run-id>/file-mutations/03-deleted-api.json` no longer contains `"verify-renamed"`.
- **Read-only gating (`mut-readonly-gate`).** Switch to `gamma` (`Linux RO`) and select `readme.txt`.
  1. `control-tailfile click --run <run-id> --role treeItem --label gamma`
  2. `control-tailfile wait --run <run-id> --contains "readme.txt"`
  3. `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "readme.txt"`
  4. `control-tailfile wait --run <run-id> --key det-delete`
  4. Capture ARIA + screenshot: `control-tailfile snapshot --run <run-id> --out .verify-proof/<run-id>/file-mutations/04-readonly-gamma.aria.txt` and `control-tailfile screenshot --run <run-id> --out .verify-proof/<run-id>/file-mutations/04-readonly-gamma.png`.
  5. Assert in `04-readonly-gamma.aria.txt` that `tb-mkdir`, `tb-rename`, `tb-delete`, `det-rename`, and `det-delete` all include `disabled` in `states=[...]` and the status bar contains `gamma (只读)`.

## Gotchas

- `setValue` on `inline-editor-input` replaces the full draft string cleanly; using `type` instead would append onto the pre-filled old name during Rename!
- Always select the item with `--key-prefix "row-" --role listItem --label <name>` before pressing `tb-rename` or `tb-delete`; without an active selection those toolbar buttons stay disabled, and without `--key-prefix "row-"` two `listItem` nodes match.
- On Windows, pass `<alphaRoot>` with forward slashes (`C:/Users/.../alpha`) in `api --path "/v1/fs/list?..."` query strings to avoid shell/URL backslash escaping issues.
- Sending raw names containing `/` or `..` returns an inline validation error inside the editor bar instead of escaping the root sandbox.
