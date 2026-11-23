# Browse, navigate, and manage tabs

Browse, navigate, and manage tabs lets a user switch between tailnet devices, move through folders via double-click, breadcrumbs, toolbar history, or explicit path entry, filter items, switch list/icon layouts, and organize work across tabs.

## Sub-features

- `browse-switch-node` switches the active device in the left sidebar and updates the file list and status bar.
- `browse-enter-and-crumbs` enters subfolders and returns via breadcrumb segments, Back/Forward, or Up.
- `browse-goto-path` jumps directly to a valid shared path and keeps invalid escapes inside sandbox bounds.
- `browse-filter-and-view` filters the current directory and toggles between List and Icon Grid views.
- `browse-marquee-select` rubber-band (marquee) selects files and folders by dragging from empty viewport space (or right-column row whitespace in List view) across items in both List and Icon Grid views, supports `Ctrl`/`Cmd` toggle and `Shift` add, and clears selection on empty-space click.
- `browse-tabs` opens a new tab, switches tabs, closes tabs, and reopens the last closed tab.

## How to get to it (user POV)

- Click a device row in the left sidebar (`alpha`, `beta`, `gamma`, `pixel`, `phone`).
- Double-click a folder row in List view (or tile in Icon Grid view), or click a parent segment in the breadcrumb bar.
- Drag from empty space (or whitespace past filenames in List view) to draw a rubber-band selection box across multiple files and folders in List or Icon Grid view; hold `Ctrl`/`Cmd` to toggle, `Shift` to add, press `Escape` mid-drag to cancel, or click empty space to deselect.
- Click toolbar `后退`, `前进`, `上一级` buttons (or press `Alt+Left`, `Alt+Right`, `Alt+Up`).
- Click `前往路径` (or press `Ctrl+L` / `Cmd+L`) and submit a target path.
- Type into the `筛选当前目录` search box (or focus it with `Ctrl+F` / `Cmd+F`).
- Click the toolbar view toggle (`切换图标视图` / `切换列表视图`), or open/close tabs via `+` (`新建标签页` / `Ctrl+T`), close (`Ctrl+W`), and reopen (`Ctrl+Shift+T`).

## Driving it with control-tailfile

Preconditions:

- Isolated verification run `<run-id>` is healthy (`doctor --run <run-id>` passes) with `alpha` initially selected (`hello.txt`, `notes`, `photos` visible).

- **Enter a subfolder.** Double-click `notes` on `alpha`. Run `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "notes" --clicks 2` and `control-tailfile wait --run <run-id> --contains "week.md"`. The breadcrumb bar gains a `notes` segment and `week.md`, `sample.pdf`, and `payload.bin` appear.
- **Return via toolbar Back and Up.** Go back to root, then forward again. Run `control-tailfile act --run <run-id> --key tb-back --action press` and `control-tailfile wait --run <run-id> --contains "hello.txt"`. Then run `control-tailfile act --run <run-id> --key tb-fwd --action press`, `control-tailfile wait --run <run-id> --contains "week.md"`, followed by `control-tailfile act --run <run-id> --key tb-up --action press` and `control-tailfile wait --run <run-id> --contains "hello.txt"`.
- **Switch nodes.** Choose `beta` in the sidebar. Run `control-tailfile click --run <run-id> --role treeItem --label beta` and `control-tailfile wait --run <run-id> --contains "inbox.txt"`. Status bar shows `beta (可写)` and file list shows `inbox.txt`.
- **Expand lazy sidebar tree (`browse-tree`).** Expand `alpha` in `sidebar-outline`. Run `control-tailfile act --run <run-id> --role treeItem --label alpha --action expand` and `control-tailfile wait --run <run-id> --contains "notes"`, then click `notes` (`control-tailfile click --run <run-id> --role treeItem --label notes`) or middle-click (`--button middle`) to open directly in a new tab.
- **Filter directory.** Focus `alpha` and filter for `hello`. Run `control-tailfile click --run <run-id> --role treeItem --label alpha`, `control-tailfile wait --run <run-id> --contains "hello.txt"`, then `control-tailfile act --run <run-id> --key filter-input --action setValue --text "hello"`. Visible entry count drops to `1 项`, leaving only `hello.txt`. Clear the filter with `control-tailfile act --run <run-id> --key filter-input --action setValue --text ""`.
- **Toggle Icon Grid view and Details.** Toggle view mode via toolbar button. Run `control-tailfile act --run <run-id> --key tb-view --action press` and `control-tailfile wait --run <run-id> --key "icon-grid-scroll"`. File rows become grid tiles (`tile-...`). Click `tb-view` again to return to List view.
- **Marquee box selection (`browse-marquee-select`).** In List or Icon Grid view, drag across items from empty space or right-side row whitespace using `control-tailfile drag --run <run-id> --from-x <x0> --from-y <y0> --to-x <x1> --to-y <y1> [--hold]`; status bar updates to `已选 N 项` and mid-drag captures show `marquee-box`.
- **Open, close, and reopen a tab.** Open a blank New Tab (`新建标签页` device picker), bind a device card, close it, and reopen it. Run `control-tailfile act --run <run-id> --key new-tab-btn --action press`, `control-tailfile wait --run <run-id> --contains "选择一个设备开始浏览"`, `control-tailfile act --run <run-id> --key newtab-node-beta --action press`, `control-tailfile key --run <run-id> --key "CmdOrCtrl+W"`, then `control-tailfile key --run <run-id> --key "CmdOrCtrl+Shift+T"`. Tab strip displays both tabs again. (Closing a bound tab with middle-click `--button middle` works directly on the tab pill too.)
- **Jump via Goto.** Open Goto bar, inspect current root path (first query `/v1/info?node=alpha` via `api` if needed), navigate to `<alphaRoot>/photos`. Run `control-tailfile act --run <run-id> --key goto-btn --action press`, `control-tailfile wait --run <run-id> --key inline-editor-input`, fill `inline-editor-input` to `<alphaRoot>/photos`, press `inline-editor-submit`, and run `control-tailfile wait --run <run-id> --contains "swatch.png"`.
- **Proof.** Capture the resulting state. Run `control-tailfile snapshot --run <run-id> --out .verify-proof/<run-id>/browse-and-tabs/photos.aria.txt` and `control-tailfile screenshot --run <run-id> --out .verify-proof/<run-id>/browse-and-tabs/photos.png`.

## Gotchas

- Folder changes automatically clear `filter-input` and close any open inline editor (`Mkdir`/`Rename`/`Goto`). Re-open Goto after switching nodes.
- `ui.List` emits two `[listItem]` ARIA nodes per entry (the virtual wrapper and `row-<path>`), so always pass `--key-prefix "row-"` when clicking files/folders in List view (or `--key-prefix "tile-"` in Icon Grid view).
- Single-clicking a folder row on desktop selects it without entering it; use `--clicks 2` (or `mobile` mode on `<820pt` width) to navigate into folders.
- `alpha`'s filesystem root is inside the ephemeral temp demo dir (`tailfile-demo-*`), so obtain the exact path from `/v1/info?node=alpha` before calling `Goto` with an absolute path.
