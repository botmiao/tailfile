package gui

import "github.com/egoist/mygo/ui"

// Palette carries all 27 color tokens matching the Tailfile palette for light & dark modes.
type Palette struct {
	Dark        bool
	Background  ui.Color
	Foreground  ui.Color
	Card        ui.Color
	CardFg      ui.Color
	Popover     ui.Color
	PopoverFg   ui.Color
	Primary     ui.Color
	PrimaryFg   ui.Color
	Secondary   ui.Color
	SecondaryFg ui.Color
	Muted       ui.Color
	MutedFg     ui.Color
	Accent      ui.Color
	AccentFg    ui.Color
	Destructive ui.Color
	Border      ui.Color
	Input       ui.Color
	Ring        ui.Color
	Sidebar     ui.Color
	SidebarFg   ui.Color
	Selection   ui.Color
	RowHover    ui.Color
	BadgeBg     ui.Color
	BadgeFg     ui.Color
}

var lightPalette = Palette{
	Dark:        false,
	Background:  ui.Hex("#e8ebf0"),
	Foreground:  ui.Hex("#1d2129"),
	Card:        ui.Hex("#ffffff"),
	CardFg:      ui.Hex("#1d2129"),
	Popover:     ui.Hex("#ffffff"),
	PopoverFg:   ui.Hex("#1d2129"),
	Primary:     ui.Hex("#2563eb"),
	PrimaryFg:   ui.Hex("#ffffff"),
	Secondary:   ui.Hex("#f1f3f6"),
	SecondaryFg: ui.Hex("#374151"),
	Muted:       ui.Hex("#f6f7f9"),
	MutedFg:     ui.Hex("#6b7280"),
	Accent:      ui.Hex("#eaf1fe"),
	AccentFg:    ui.Hex("#1d4ed8"),
	Destructive: ui.Hex("#d92d20"),
	Border:      ui.Hex("#e2e5ea"),
	Input:       ui.Hex("#d3d8df"),
	Ring:        ui.Hex("#2563eb"),
	Sidebar:     ui.Hex("#f6f7f9"),
	SidebarFg:   ui.Hex("#1d2129"),
	Selection:   ui.Hex("#dce8fd"),
	RowHover:    ui.RGBA(29, 33, 41, 0.04),
	BadgeBg:     ui.Hex("#f1f3f6"),
	BadgeFg:     ui.Hex("#4b5563"),
}

var darkPalette = Palette{
	Dark:        true,
	Background:  ui.Hex("#121419"),
	Foreground:  ui.Hex("#e7e9ed"),
	Card:        ui.Hex("#1b1e25"),
	CardFg:      ui.Hex("#e7e9ed"),
	Popover:     ui.Hex("#1b1e25"),
	PopoverFg:   ui.Hex("#e7e9ed"),
	Primary:     ui.Hex("#2f6fe4"),
	PrimaryFg:   ui.Hex("#ffffff"),
	Secondary:   ui.Hex("#2b3039"),
	SecondaryFg: ui.Hex("#cfd3da"),
	Muted:       ui.Hex("#171a20"),
	MutedFg:     ui.Hex("#9aa2ae"),
	Accent:      ui.RGBA(79, 141, 247, 0.16),
	AccentFg:    ui.Hex("#82adff"),
	Destructive: ui.Hex("#d13a3f"),
	Border:      ui.Hex("#2c313a"),
	Input:       ui.Hex("#3a404b"),
	Ring:        ui.Hex("#2f6fe4"),
	Sidebar:     ui.Hex("#171a20"),
	SidebarFg:   ui.Hex("#e7e9ed"),
	Selection:   ui.RGBA(79, 141, 247, 0.26),
	RowHover:    ui.RGBA(255, 255, 255, 0.05),
	BadgeBg:     ui.Hex("#252a33"),
	BadgeFg:     ui.Hex("#aeb6c4"),
}

// ResolvePalette returns the Palette for pref using systemDark as fallback when pref is system.
func ResolvePalette(pref ThemePref, systemDark bool) Palette {
	switch pref {
	case ThemePrefDark:
		return darkPalette
	case ThemePrefLight:
		return lightPalette
	default:
		if systemDark {
			return darkPalette
		}
		return lightPalette
	}
}

// ApplyTheme configures the ui.Context theme to match Tailfile's palette and density spacing.
func ApplyTheme(c *ui.Context, pref ThemePref, metrics LayoutMetrics) Palette {
	sysDark := false
	if cur := c.Theme(); cur != nil {
		sysDark = cur.Dark
	}
	pal := ResolvePalette(pref, sysDark)
	var base *ui.Theme
	if pal.Dark {
		base = ui.DarkTheme()
	} else {
		base = ui.LightTheme()
	}
	th := *base
	th.Dark = pal.Dark
	th.Background = pal.Card
	th.Surface = pal.Secondary
	th.SurfaceHover = pal.RowHover
	th.SurfacePressed = pal.Selection
	th.Border = pal.Border
	th.Text = pal.Foreground
	th.TextMuted = pal.MutedFg
	th.Accent = pal.Primary
	th.AccentText = pal.PrimaryFg
	th.InactiveAccent = pal.Accent
	th.Danger = pal.Destructive
	th.Selection = pal.Selection
	th.Focus = pal.Ring
	th.Radius = 8
	th.FontSize = 13
	if metrics.ThemeSpacing > 0 {
		th.Spacing = metrics.ThemeSpacing
	} else {
		th.Spacing = 4
	}
	c.SetTheme(&th)
	return pal
}
