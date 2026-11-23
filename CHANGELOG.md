# Changelog

All notable changes to this project are documented in this file.

## [0.2.0] - 2026-11-24

### Added
- Unified single-binary pure-Go desktop application and background node agent (`tailfile`, `tailfile agent`, `tailfile service <install|uninstall|start|stop|status>`).
- Zero-config embedded desktop mode over in-memory transport (`memRoundTripper`) with close-to-tray background sharing.
- Native MyGo GUI with multi-tab browsing, device picker, lazy-loaded sidebar directory tree (`ui.Outline`), List and Icon grid views, desktop marquee selection, image/text/PDF previews, built-in UTF-8 text editor, and OS drag-in/drag-out.
- Configurable layout density preference (`auto`, `compact`, `standard`, `comfortable`) alongside `system`/`light`/`dark` themes, persisted in `<StateDir>/ui.json`.
- Point-to-point Tailnet transfers with chunked resumable uploads, SHA-256 verification, and non-dismissible conflict resolution.
- Scoped read/write root sandboxing, symlink escape checks, Tailscale ACL capability enforcement, and Tailnet/CORS/Host header guards.
- Per-user background service installer (`systemd --user`, `LaunchAgent`, Windows Task Scheduler) with legacy `tailfile-agent` cleanup.
- Cross-platform one-click installation scripts (`scripts/install.sh`, `scripts/install.ps1`), release packaging script (`scripts/build-all.sh`), and GitHub Actions CI & Release workflows.
