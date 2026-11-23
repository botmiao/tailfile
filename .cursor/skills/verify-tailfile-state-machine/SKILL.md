---
name: verify-tailfile-state-machine
description: Run explicit-state BFS formal model checking, bounded path-sandbox checks, and headless MyGo UI invariant verification for unified tailfile (internal/gui/session.go, pathbox.go, names.go, persist.go, view.go, mobile.go). Use when modifying tab lifecycle, folder navigation, read/write root sandboxing, selection/write gating, conflict/settings modals, or mobile Drawer/Sheet state transitions.
---

# Verify Tailfile UI State Machine & Formal Checks

Run this verification workflow whenever changing navigation, tabs, read/write sandboxing, selection resets, settings drafts, conflict handling, or responsive desktop/mobile layout in `internal/gui`.

## Commands

```powershell
# 1. Explicit-state BFS & bounded path-sandbox checks (fast, pure Go)
go test -count=1 -v ./internal/gui -run "TestFormal|TestPath|TestFileURI"

# 2. Full headless MyGo UI verification (INV-1..INV-7 against embedded Demo cluster)
go test -count=1 -v ./internal/gui -run TestVerify

# 3. Full suite (agent, cliflag, guitoken, gui, CLI)
go test -count=1 ./...
```
