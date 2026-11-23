# Tailfile UI Design & Architecture (`internal/gui`)

Tailfile renders its native interface directly from Go using `github.com/egoist/mygo` (`ui` + `ui/mobile`) with zero WebView, zero CGo, and zero IPC binary boundaries in default desktop/mobile mode.

## Connection Architecture

- **Zero-config Desktop (`tailfile`)**: Runs the agent in-process over `memRoundTripper` (`ConnEmbedded`), opening zero loopback TCP sockets (`:8645` disabled unless `--local` is set) and writing no `gui-token` to disk. Attaches over loopback TCP (`ConnAttached`) only when `--agent`, `--local`, or `--attach-only` is passed.
- **Demo Mode (`tailfile --demo`)**: Launches an isolated 5-node in-memory tailnet (`alpha`, `beta`, `gamma`, `pixel`, `phone`) over `memRoundTripper` (`ConnDemo`).
- **Mobile (`ios` / `android`)**: Always starts `ConnEmbedded` over `memRoundTripper` (UI layout complete; mobile `tsnet` login and store/release packaging are Roadmap).
- **Responsive Breakpoint & Layout Density**: Switches automatically between Desktop (`>=820` DIPs, 46pt custom `TitleBarHidden` chrome, 262/56pt collapsible sidebar, 300pt details pane, transfer queue, status bar) and Mobile (`<820` DIPs or iOS/Android, `mobile.TopBar`, `mobile.Drawer`, touch-sized rows, `mobile.Sheet` details/transfer sheets). Content spacing, MyGo `Theme.Spacing`, and marquee hit geometry are driven by `LayoutMetrics` (`internal/gui/density.go`, supporting `auto`, `compact`, `standard`, and `comfortable` in `<StateDir>/ui.json`).

## Theme Tokens & Icons

- Light and dark palettes (`internal/gui/theme.go`) carry all 27 color tokens matching the approved palette (`#e8ebf0`/`#121419` chrome, `#ffffff`/`#1b1e25` surface, `#2563eb`/`#2f6fe4` primary).
- `internal/gui/icons.go` draws vector Lucide paths, 5 OS silhouettes (`Apple`, `Windows`, `Tux`, `Android`, `iPhone`, plus `Server` fallback), 8 `FileKind` tiles, and the orange squircle + 9-dot logo mark (`resources/icon.png` embedded as `AppIconPNG`).
- All 1s background health/node/transfer polling pauses automatically while `c.WindowHidden()` is true.
