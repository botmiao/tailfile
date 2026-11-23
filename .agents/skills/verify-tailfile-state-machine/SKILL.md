---
name: verify-tailfile-state-machine
description: Run explicit-state BFS formal model checking, bounded path-sandbox checks, and headless MyGo UI invariant verification for unified tailfile (internal/gui/session.go, tree.go, pathbox.go, names.go, persist.go, view.go, mobile.go). Use when modifying tab lifecycle, sidebar directory tree, folder navigation, read/write root sandboxing, selection/write gating, conflict/settings modals, or mobile Drawer/Sheet state transitions.
---

# Verify Tailfile UI State Machine & Formal Checks

Run this verification workflow whenever changing navigation, tabs, read/write sandboxing, selection resets, settings drafts, conflict handling, or responsive desktop/mobile layout in `internal/gui`.

## Commands

```powershell
# 1. Explicit-state BFS & bounded path-sandbox checks (fast, pure Go)
go test -count=1 -v ./internal/gui -run "TestFormal|TestPath|TestFileURI|TestRemoteAncestor|TestSidebarChildren|TestSyncSidebar|TestPersist|TestNewTabBlank"

# 2. Full headless MyGo UI verification (INV-1..INV-7 against embedded Demo cluster)
go test -count=1 -v ./internal/gui -run TestVerify

# 3. Full suite (agent, cliflag, guitoken, gui, CLI)
go test -count=1 ./...
```

## Covered Invariants

1. `TestFormalTabAndNavigationInvariants`: Exhaustive BFS across `NewTab` (blank Scheme 1 tab), `OpenNodeInNewTab`, `CloseTab`, `CloseTabByID`, `SwitchTab`, `ReopenClosedTab`, `SelectNode`, `SelectNodePath`, `Navigate`, `GoBack`, `GoForward`, `GoUp` verifying `len(Tabs) >= 1`, `0 <= ActiveIdx < len(Tabs)`, blank-tab isolation, `History[HistIdx] == Path`, and `PathIsUnderAny(Path, ReadRoots)`.
2. `TestFormalFolderChangeClearsTransientState`: Exhaustive transition × editor matrix proving filter text, inline editors (`Mkdir`, `Rename`, `Goto`), `ModalDelete`, and foreign selections clear across every folder/tab transition.
3. `TestFormalModalAndWriteGating`: Proves read-only nodes disable all write operations and active `ConflictPrompt` preempts overlays and resists Esc/outside dismissal.
4. `TestFormalPathSandboxBoundedInputs`: Verifies lexical resolution (`CanonicalizeRemote`, `PathIsUnder`, `ParentRemote`, `RemoteAncestorPaths`) blocks `..`, backslash, UNC, and sibling-prefix escapes (`/srv/alpha2` vs `/srv/alpha`).
5. `TestFormalSettingsDraftIsolation`: Verifies `SettingsDraft` edits do not mutate live session state on `CancelSettings` and commit cleanly on `SaveSettings`.
6. `TestVerifyINV1..INV8`: Drives rendered desktop (`1220x780`) and mobile (`390x844`) views with `ui.NewTester` against `agent.StartEmbedded(..., Demo: true)`, covering live previews, built-in UTF-8 plain-text editor (`ModalDocEdit`, conflict detection, dirty close guard, read-only viewer), native `ui.Outline` lazy directory tree (folder-only filtering, empty-folder chevron removal, `AutoEnterSingleRoot`, `SyncSidebarTree`, middle-click / right-click open in new tab, keyboard tree nav), middle-click tab close, aligned `+` button, blank New Tab device picker, file transfers, non-dismissible conflicts, mobile drawer/sheets, dark/light themes, and polling suspension when `WindowHidden()`.
7. `TestFormalDocEditorInvariants`: Verifies read-only gating on `ModalDocEdit`, dirty close guard interception in `SyncOverlays`, and preservation of unsaved buffer edits across `ConflictPrompt` preemption.
