# Preview files and inspect details

Preview files and inspect details lets a user select any file or folder to inspect its metadata (`大小`, `修改时间`, `路径`) and view inline previews for text/markdown files and PNG/JPEG/GIF images inside the Details pane.

## Sub-features

- `preview-text` renders inline text/markdown excerpts (for `hello.txt` and `notes/week.md`).
- `preview-image` decodes and renders inline image bitmaps (for `photos/swatch.png`).
- `preview-metadata-and-toggle` displays file size/time/path and toggles the Details pane open and closed.
- `doc-edit` opens the built-in UTF-8 plain-text editor (`ModalDocEdit`) from `det-edit` / `tb-edit` (editable with `doc-editor-save` on writable nodes, read-only `"查看全文"` on `gamma`).

## How to get to it (user POV)

- Click a file row in the List or Icon Grid view while the Details pane is open.
- Toggle the Details pane open/closed from the toolbar `详情面板` button (`tb-details`).
- Open the folder `notes` or `photos` by double-clicking it and click a previewable file inside.

## Driving it with control-tailfile

Preconditions:

- Isolated verification run `<run-id>` is healthy (`doctor --run <run-id>` passes), `alpha` is selected, and Details pane is open (`详情与预览` visible in tree).

- **Select and preview text file.** Click `hello.txt` in `alpha`. Run `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "hello.txt"`, then `control-tailfile wait --run <run-id> --contains "Hello from alpha."`. The Details pane shows `"text"`, size, path, action buttons (`det-open`, `det-copy`, `det-rename`, `det-delete`), and body `"Hello from alpha."`.
- **Preview markdown file in subfolder.** Open `notes` and select `week.md`. Run `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "notes" --clicks 2`, `control-tailfile wait --run <run-id> --contains "week.md"`, `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "week.md"`, and `control-tailfile wait --run <run-id> --contains "# Week notes"`. Markdown content appears in the preview box.
- **Preview PNG image.** Return up one level, open `photos`, and select `swatch.png`. Run `control-tailfile act --run <run-id> --key tb-up --action press`, `control-tailfile wait --run <run-id> --contains "photos"`, `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "photos" --clicks 2`, `control-tailfile wait --run <run-id> --contains "swatch.png"`, `control-tailfile click --run <run-id> --key-prefix "row-" --role listItem --label "swatch.png"`, and wait until `"正在加载预览…"` disappears (`control-tailfile wait --run <run-id> --absent "正在加载预览…"`).
- **Proof (image preview).** Capture both ARIA tree and real rendered PNG showing the gradient `swatch.png` bitmap inside the Details pane. Run `control-tailfile snapshot --run <run-id> --out .verify-proof/<run-id>/preview-and-details/swatch.aria.txt` and `control-tailfile screenshot --run <run-id> --out .verify-proof/<run-id>/preview-and-details/swatch.png`. Inspect the PNG visually to verify the gradient swatch image is rendered (not blank/error text).
- **Toggle Details pane.** Hide and restore the Details pane. Run `control-tailfile act --run <run-id> --key tb-details --action press` and `control-tailfile wait --run <run-id> --absent "详情与预览"`. Press `tb-details` again and wait for `"详情与预览"` to return.
- **Edit remote text directly (`doc-edit`).** Select `hello.txt` on `alpha`, press `det-edit`, wait for `doc-editor-input`, update via `act --key doc-editor-input --action setValue --text "..."`, press `doc-editor-save`, wait for `"已保存"`, press `doc-editor-close`, and confirm the Details preview and `/v1/fs/read` match the updated text. On read-only `gamma`, pressing `det-edit` (`"查看全文"`) opens `doc-editor-input` in `readOnly` state without `doc-editor-save`.

## Gotchas

- Once `hello.txt` is selected, its filename appears in the Details pane (`role=text`) AND `ui.List` renders two `listItem` nodes per row. Always filter List-view file clicks with `--key-prefix "row-" --role listItem`.
- `notes/sample.pdf` and `notes/payload.bin` do not render inline pixels/text (PDF/binary metadata only); inspect `week.md` and `swatch.png` for content preview verification.
- On narrow mobile widths (`<820pt`), tapping a file opens a bottom/overlay details sheet (`mobileDetailsOpen`) instead of the persistent desktop right column.
