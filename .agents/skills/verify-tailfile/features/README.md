# Tailfile verification map

This directory is the maintained source for verifying the user-facing behavior of Tailfile. Read the index before driving the app, then follow the matching feature file as the recipe.

## Baseline preconditions

- Launch an isolated verification run in `attached` mode (start `launch` in a background daemon/shell, then run `wait-ready`):
  - Daemon: `go run .agents/skills/verify-tailfile/scripts/control-tailfile.go launch --run <run-id> --mode attached`
  - Ready check: `go run .agents/skills/verify-tailfile/scripts/control-tailfile.go wait-ready --run <run-id> --timeout 60s`
- Do not touch port `8645` or real user config/state directories; every run gets an ephemeral temp `<runDir>` and unique loopback ports.
- Seeded demo fixtures provide 5 nodes:
  - Writable: `alpha` (`macos`, contains `hello.txt`, `notes/week.md`, `notes/sample.pdf`, `notes/payload.bin` 2 MiB, `photos/swatch.png`) and `beta` (`windows`, contains `inbox.txt`).
  - Read-only: `gamma` (`linux`, contains `readme.txt`), `pixel` (`android`, empty), `phone` (`ios`, empty).
- Run `go run .agents/skills/verify-tailfile/scripts/control-tailfile.go doctor --run <run-id>` and confirm `DOCTOR PASSED` before driving features.
- Never drive an instance that was not started by this verification run.

## Driving conventions

- Start every recipe from the baseline state unless its preconditions say otherwise.
- Prefer stable `Key`s (`tb-mkdir`, `inline-editor-input`, `node-alpha`) and unambiguous selectors (`--role treeItem --label <name>` for expanded sidebar items, `--key-prefix "row-" --role listItem --label <name>` in List view, `--key-prefix "tile-"` in Icon Grid view) over raw coordinates.
- Treat every command as literal. Keep quoted keys, labels, and flags unchanged.
- Run GUI actions through `control-tailfile` (`act`, `click`, `key`, `type`, `wait`, `snapshot`, `screenshot`).
- Run agent API checks through `control-tailfile api --run <run-id>` and CLI checks through `control-tailfile cli --run <run-id> -- <args...>`.
- Restore or delete test files created during mutations when continuing to another feature, and never delete `.verify-proof/<run-id>/` during cleanup.

## Proof and skip reporting

- Capture both the user action and the resulting settled state, not only the final screen.
- UI proof includes an ARIA snapshot (`--out .verify-proof/<run-id>/<feature>/*.aria.txt`) and a decoded non-blank screenshot (`--out .verify-proof/<run-id>/<feature>/*.png`) showing the Tailfile window identity.
- CLI proof includes the invoked command, exit code, stdout, and stderr (`--out .verify-proof/<run-id>/<feature>/*.txt`).
- Mutation and transfer proof includes an independent second read (`control-tailfile api --path "/v1/fs/list?..."`) confirming backend state changed.
- Record the feature ID and entry point used with every artifact.
- Report an unreachable path with the attempted command and the unmet precondition; never weaken a check to make it pass.
- Do not report a skipped entry point (such as real tailnet `tsnet` or OS service registration) as verified through `--demo`.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the user-visible behavior. It then uses exactly four H2 sections in this order:

1. `Sub-features` lists short IDs with one line for each behavior.
2. `How to get to it (user POV)` lists every user entry point.
3. `Driving it with control-tailfile` starts with `Preconditions:` and uses labeled bullets that pair each user action with an exact command and observable result.
4. `Gotchas` lists traps that can waste or invalidate a verification run.

Keep implementation details out of the map. Name only user paths, stable handles, required state, commands, and observable proof.

## Features

- [Browse, navigate, and manage tabs](./browse-and-tabs.md) covers node switching, folder navigation, breadcrumbs, history navigation, Go-to-path, multi-tab lifecycle, list/icon view, and directory filtering.
- [Preview files and inspect details](./preview-and-details.md) covers selecting files, reading text/markdown previews, rendering PNG image previews, and toggling the Details pane.
- [File mutations and read-only gating](./file-mutations-and-readonly.md) covers creating folders, renaming, deleting with confirmation, API side-effect verification, and read-only enforcement on `gamma`.
- [Cross-node transfers and conflict resolution](./cross-node-transfer-and-conflict.md) covers copying files across nodes, observing transfer completion, resolving the non-dismissible overwrite conflict dialog, and clearing transfer history.
- [CLI commands and attached daemon](./cli-and-attached-daemon.md) covers `tailfile --version`/`--help`/`service --help`, bearer-token auth on `agent --demo`, attached-mode indicator, and unreachable daemon guidance.
