package gui

import "github.com/egoist/mygo/ui"

// DensityPref selects the user's preferred layout spacing mode.
type DensityPref string

const (
	DensityPrefAuto        DensityPref = "auto"
	DensityPrefCompact     DensityPref = "compact"
	DensityPrefStandard    DensityPref = "standard"
	DensityPrefComfortable DensityPref = "comfortable"
)

// ResolvedDensity is a concrete layout density active for the current frame.
type ResolvedDensity string

const (
	DensityCompact     ResolvedDensity = "compact"
	DensityStandard    ResolvedDensity = "standard"
	DensityComfortable ResolvedDensity = "comfortable"
)

// DensityEnv captures viewport and pointer inputs used to resolve DensityPrefAuto.
// Uses AnyPointer rather than primary Pointer because non-convertible Windows touchscreen PCs
// with an attached mouse report SM_CONVERTIBLESLATEMODE=0 (Pointer=Coarse) while AnyPointer remains Fine.
type DensityEnv struct {
	Width      float32
	Height     float32
	AnyPointer ui.PointerAccuracy
	GOOS       string
}

func sanitizeDensityPref(pref DensityPref) DensityPref {
	switch pref {
	case DensityPrefAuto, DensityPrefCompact, DensityPrefStandard, DensityPrefComfortable:
		return pref
	default:
		return DensityPrefAuto
	}
}

// Label returns the display text for a density preference.
func (p DensityPref) Label() string {
	switch p {
	case DensityPrefCompact:
		return "紧凑"
	case DensityPrefStandard:
		return "标准"
	case DensityPrefComfortable:
		return "宽松"
	default:
		return "自动"
	}
}

// Label returns the display text for a resolved layout density.
func (d ResolvedDensity) Label() string {
	switch d {
	case DensityCompact:
		return "紧凑"
	case DensityComfortable:
		return "宽松"
	default:
		return "标准"
	}
}

// ResolveDensity turns a preference and environment snapshot into a concrete density.
func ResolveDensity(pref DensityPref, env DensityEnv) ResolvedDensity {
	switch sanitizeDensityPref(pref) {
	case DensityPrefCompact:
		return DensityCompact
	case DensityPrefStandard:
		return DensityStandard
	case DensityPrefComfortable:
		return DensityComfortable
	default:
		if env.GOOS == "ios" || env.GOOS == "android" || env.AnyPointer == ui.PointerCoarse {
			return DensityComfortable
		}
		if (env.Width > 0 && env.Width < 1040) || (env.Height > 0 && env.Height < 680) {
			return DensityCompact
		}
		return DensityStandard
	}
}

// LayoutMetrics centralizes spacing, sizing, and hit-test geometry for one density level.
type LayoutMetrics struct {
	Resolved           ResolvedDensity
	ThemeSpacing       float32
	TreeOutlineSpacing float32
	TreeRowContentH    float32
	TreeRowGap         float32
	TreeIndent         float32
	TreeIconSize       float32
	TreeNodeFontSize   float32
	TreeDirFontSize    float32
	TreeOuterPadV      float32
	TreeOuterPadH      float32
	RailRowH           float32
	RailGap            float32
	ListHeaderH        float32
	ListRowH           float32
	MobileListRowH     float32
	ListPadX           float32
	ListGap            float32
	ListIconSize       float32
	ListSizeColW       float32
	ListTimeColW       float32
	ListRenameH        float32
	GridPad            float32
	GridGap            float32
	GridTileW          float32
	GridTileH          float32
	GridTilePadV       float32
	GridTilePadH       float32
	GridIconSize       float32
	DetailsPad         float32
	DetailsGap         float32
	DetailsMetaGap     float32
	TransferPadV       float32
	TransferPadH       float32
	TransferGap        float32
	MobileDrawerPad    float32
	MobileDrawerGap    float32
	MobileDrawerTabH   float32
	MobileDrawerNodeH  float32
}

var densityMetricsTable = map[ResolvedDensity]LayoutMetrics{
	DensityCompact: {
		Resolved:           DensityCompact,
		ThemeSpacing:       3,
		TreeOutlineSpacing: 3,
		TreeRowContentH:    22,
		TreeRowGap:         5,
		TreeIndent:         14,
		TreeIconSize:       14,
		TreeNodeFontSize:   12.5,
		TreeDirFontSize:    12,
		TreeOuterPadV:      4,
		TreeOuterPadH:      6,
		RailRowH:           30,
		RailGap:            2,
		ListHeaderH:        26,
		ListRowH:           30,
		MobileListRowH:     44,
		ListPadX:           12,
		ListGap:            6,
		ListIconSize:       16,
		ListSizeColW:       84,
		ListTimeColW:       132,
		ListRenameH:        24,
		GridPad:            10,
		GridGap:            8,
		GridTileW:          88,
		GridTileH:          96,
		GridTilePadV:       8,
		GridTilePadH:       5,
		GridIconSize:       36,
		DetailsPad:         10,
		DetailsGap:         8,
		DetailsMetaGap:     4,
		TransferPadV:       5,
		TransferPadH:       10,
		TransferGap:        4,
		MobileDrawerPad:    12,
		MobileDrawerGap:    8,
		MobileDrawerTabH:   32,
		MobileDrawerNodeH:  38,
	},
	DensityStandard: {
		Resolved:           DensityStandard,
		ThemeSpacing:       4,
		TreeOutlineSpacing: 4,
		TreeRowContentH:    24,
		TreeRowGap:         6,
		TreeIndent:         16,
		TreeIconSize:       15,
		TreeNodeFontSize:   13,
		TreeDirFontSize:    12.5,
		TreeOuterPadV:      4,
		TreeOuterPadH:      6,
		RailRowH:           32,
		RailGap:            2,
		ListHeaderH:        28,
		ListRowH:           34,
		MobileListRowH:     48,
		ListPadX:           14,
		ListGap:            8,
		ListIconSize:       18,
		ListSizeColW:       84,
		ListTimeColW:       132,
		ListRenameH:        26,
		GridPad:            12,
		GridGap:            10,
		GridTileW:          96,
		GridTileH:          104,
		GridTilePadV:       10,
		GridTilePadH:       6,
		GridIconSize:       40,
		DetailsPad:         14,
		DetailsGap:         12,
		DetailsMetaGap:     6,
		TransferPadV:       6,
		TransferPadH:       12,
		TransferGap:        6,
		MobileDrawerPad:    14,
		MobileDrawerGap:    10,
		MobileDrawerTabH:   34,
		MobileDrawerNodeH:  40,
	},
	DensityComfortable: {
		Resolved:     DensityComfortable,
		ThemeSpacing: 5,
		// Cap Outline spacing at 4 so MyGo's Space(4) disclosure arrow stays 16x16 instead of 20x20.
		TreeOutlineSpacing: 4,
		TreeRowContentH:    28,
		TreeRowGap:         8,
		TreeIndent:         18,
		TreeIconSize:       16,
		TreeNodeFontSize:   13.5,
		TreeDirFontSize:    13,
		TreeOuterPadV:      6,
		TreeOuterPadH:      8,
		RailRowH:           36,
		RailGap:            4,
		ListHeaderH:        32,
		ListRowH:           40,
		MobileListRowH:     54,
		ListPadX:           16,
		ListGap:            10,
		ListIconSize:       20,
		ListSizeColW:       88,
		ListTimeColW:       136,
		ListRenameH:        30,
		GridPad:            16,
		GridGap:            14,
		GridTileW:          108,
		GridTileH:          116,
		GridTilePadV:       12,
		GridTilePadH:       8,
		GridIconSize:       44,
		DetailsPad:         18,
		DetailsGap:         14,
		DetailsMetaGap:     8,
		TransferPadV:       8,
		TransferPadH:       16,
		TransferGap:        8,
		MobileDrawerPad:    16,
		MobileDrawerGap:    12,
		MobileDrawerTabH:   38,
		MobileDrawerNodeH:  46,
	},
}

// MetricsFor returns the LayoutMetrics for d, falling back to Standard.
func MetricsFor(d ResolvedDensity) LayoutMetrics {
	if m, ok := densityMetricsTable[d]; ok {
		return m
	}
	return densityMetricsTable[DensityStandard]
}

// TreeRowTotalH returns the full DIP height of a sidebar ui.Outline row including outline padding.
func (m LayoutMetrics) TreeRowTotalH() float32 {
	return m.TreeRowContentH + 2*m.TreeOutlineSpacing
}

// OutlineArrowCenterX returns the disclosure chevron center X offset from a ui.Outline row's left edge.
func (m LayoutMetrics) OutlineArrowCenterX(depth int) float32 {
	padL := m.TreeOutlineSpacing * 2
	gap := m.TreeOutlineSpacing
	arrow := m.TreeOutlineSpacing * 4
	if depth <= 0 {
		return padL + arrow/2
	}
	return padL + float32(depth)*m.TreeIndent + gap + arrow/2
}

// ListMetaReservedW returns the width reserved on the right side of a desktop list row for size and time columns.
func (m LayoutMetrics) ListMetaReservedW() float32 {
	return m.ListPadX + m.ListTimeColW + m.ListGap + m.ListSizeColW
}

// GridColumns computes how many icon grid tiles fit inside availW after subtracting outer scroll padding.
func (m LayoutMetrics) GridColumns(availW float32) int {
	if availW <= 0 {
		return 5
	}
	usable := availW - 2*m.GridPad
	step := m.GridTileW + m.GridGap
	if step <= 0 {
		return 2
	}
	n := int((usable + m.GridGap) / step)
	if n < 2 {
		return 2
	}
	return n
}
