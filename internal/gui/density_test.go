package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestResolveDensityMatrix(t *testing.T) {
	cases := []struct {
		name string
		pref DensityPref
		env  DensityEnv
		want ResolvedDensity
	}{
		{
			name: "explicit compact overrides large desktop",
			pref: DensityPrefCompact,
			env:  DensityEnv{Width: 1920, Height: 1080, AnyPointer: ui.PointerFine, GOOS: "windows"},
			want: DensityCompact,
		},
		{
			name: "explicit standard overrides touch mobile",
			pref: DensityPrefStandard,
			env:  DensityEnv{Width: 390, Height: 844, AnyPointer: ui.PointerCoarse, GOOS: "ios"},
			want: DensityStandard,
		},
		{
			name: "explicit comfortable overrides cramped desktop",
			pref: DensityPrefComfortable,
			env:  DensityEnv{Width: 800, Height: 600, AnyPointer: ui.PointerFine, GOOS: "linux"},
			want: DensityComfortable,
		},
		{
			name: "auto ios resolves comfortable",
			pref: DensityPrefAuto,
			env:  DensityEnv{Width: 390, Height: 844, AnyPointer: ui.PointerFine, GOOS: "ios"},
			want: DensityComfortable,
		},
		{
			name: "auto android resolves comfortable",
			pref: DensityPrefAuto,
			env:  DensityEnv{Width: 412, Height: 915, AnyPointer: ui.PointerFine, GOOS: "android"},
			want: DensityComfortable,
		},
		{
			name: "auto touch-only coarse AnyPointer resolves comfortable",
			pref: DensityPrefAuto,
			env:  DensityEnv{Width: 1280, Height: 800, AnyPointer: ui.PointerCoarse, GOOS: "windows"},
			want: DensityComfortable,
		},
		{
			name: "auto windows desktop with mouse fine AnyPointer resolves standard",
			pref: DensityPrefAuto,
			env:  DensityEnv{Width: 1220, Height: 780, AnyPointer: ui.PointerFine, GOOS: "windows"},
			want: DensityStandard,
		},
		{
			name: "auto narrow desktop width resolves compact",
			pref: DensityPrefAuto,
			env:  DensityEnv{Width: 1039, Height: 780, AnyPointer: ui.PointerFine, GOOS: "windows"},
			want: DensityCompact,
		},
		{
			name: "auto short desktop height resolves compact",
			pref: DensityPrefAuto,
			env:  DensityEnv{Width: 1280, Height: 679, AnyPointer: ui.PointerFine, GOOS: "darwin"},
			want: DensityCompact,
		},
		{
			name: "auto boundary 1040x680 resolves standard",
			pref: DensityPrefAuto,
			env:  DensityEnv{Width: 1040, Height: 680, AnyPointer: ui.PointerFine, GOOS: "windows"},
			want: DensityStandard,
		},
		{
			name: "auto 4k desktop stays standard",
			pref: DensityPrefAuto,
			env:  DensityEnv{Width: 2560, Height: 1440, AnyPointer: ui.PointerFine, GOOS: "darwin"},
			want: DensityStandard,
		},
		{
			name: "auto zero size falls back to standard",
			pref: DensityPrefAuto,
			env:  DensityEnv{Width: 0, Height: 0, AnyPointer: ui.PointerFine, GOOS: "windows"},
			want: DensityStandard,
		},
		{
			name: "unknown pref defaults to auto",
			pref: DensityPref("bogus"),
			env:  DensityEnv{Width: 900, Height: 700, AnyPointer: ui.PointerFine, GOOS: "linux"},
			want: DensityCompact,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveDensity(tc.pref, tc.env); got != tc.want {
				t.Fatalf("ResolveDensity(%q, %+v) = %q, want %q", tc.pref, tc.env, got, tc.want)
			}
		})
	}
}

func TestDensityMetricsOrderingAndHelpers(t *testing.T) {
	c := MetricsFor(DensityCompact)
	s := MetricsFor(DensityStandard)
	f := MetricsFor(DensityComfortable)

	if !(c.ThemeSpacing < s.ThemeSpacing && s.ThemeSpacing < f.ThemeSpacing) {
		t.Fatalf("ThemeSpacing not strictly ordered: %v, %v, %v", c.ThemeSpacing, s.ThemeSpacing, f.ThemeSpacing)
	}
	if !(c.TreeRowTotalH() < s.TreeRowTotalH() && s.TreeRowTotalH() < f.TreeRowTotalH()) {
		t.Fatalf("TreeRowTotalH not strictly ordered: %v, %v, %v", c.TreeRowTotalH(), s.TreeRowTotalH(), f.TreeRowTotalH())
	}
	if !(c.ListRowH < s.ListRowH && s.ListRowH < f.ListRowH) {
		t.Fatalf("ListRowH not strictly ordered: %v, %v, %v", c.ListRowH, s.ListRowH, f.ListRowH)
	}
	if !(c.GridTileW < s.GridTileW && s.GridTileW < f.GridTileW) {
		t.Fatalf("GridTileW not strictly ordered: %v, %v, %v", c.GridTileW, s.GridTileW, f.GridTileW)
	}
	if !(c.DetailsPad < s.DetailsPad && s.DetailsPad < f.DetailsPad) {
		t.Fatalf("DetailsPad not strictly ordered: %v, %v, %v", c.DetailsPad, s.DetailsPad, f.DetailsPad)
	}
	if c.MobileListRowH < 44 || s.MobileListRowH < 44 || f.MobileListRowH < 44 {
		t.Fatalf("mobile row height below 44pt touch minimum: %v, %v, %v", c.MobileListRowH, s.MobileListRowH, f.MobileListRowH)
	}

	if got := s.OutlineArrowCenterX(0); got != 16 {
		t.Fatalf("Standard depth-0 arrow center X = %v, want 16", got)
	}
	if got := s.OutlineArrowCenterX(1); got != 36 {
		t.Fatalf("Standard depth-1 arrow center X = %v, want 36", got)
	}
	if got := s.ListMetaReservedW(); got != 14+132+8+84 {
		t.Fatalf("Standard ListMetaReservedW = %v, want 238", got)
	}
	if got := s.GridColumns(0); got != 5 {
		t.Fatalf("GridColumns(0) = %d, want 5", got)
	}
	if got := s.GridColumns(100); got != 2 {
		t.Fatalf("GridColumns(100) = %d, want minimum 2", got)
	}
	// Standard: pad=12, tile=96, gap=10 -> 4 tiles need 2*12 + 4*96 + 3*10 = 438.
	if got := s.GridColumns(437); got != 3 {
		t.Fatalf("GridColumns(437) = %d, want 3", got)
	}
	if got := s.GridColumns(438); got != 4 {
		t.Fatalf("GridColumns(438) = %d, want 4", got)
	}
}

func TestPersistDensityDefaultsAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	uiPath := filepath.Join(dir, "ui.json")

	if err := os.WriteFile(uiPath, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := LoadPersist(dir)
	if legacy.Density != DensityPrefAuto {
		t.Fatalf("legacy ui.json Density = %q, want %q", legacy.Density, DensityPrefAuto)
	}

	if err := os.WriteFile(uiPath, []byte(`{"density":"invalid-value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := LoadPersist(dir)
	if bad.Density != DensityPrefAuto {
		t.Fatalf("invalid ui.json Density = %q, want %q", bad.Density, DensityPrefAuto)
	}

	for _, pref := range []DensityPref{DensityPrefCompact, DensityPrefStandard, DensityPrefComfortable, DensityPrefAuto} {
		p := defaultPersist()
		p.Density = pref
		if err := SavePersist(dir, p); err != nil {
			t.Fatal(err)
		}
		reloaded := LoadPersist(dir)
		if reloaded.Density != pref {
			t.Fatalf("round-trip Density = %q, want %q", reloaded.Density, pref)
		}
	}
}
