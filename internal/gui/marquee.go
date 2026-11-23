package gui

import "tailfile.local/internal/agent"

// MarqueeRect represents an axis-aligned rectangle in viewport or content DIPs.
type MarqueeRect struct {
	X float32
	Y float32
	W float32
	H float32
}

// MarqueeMode controls how a marquee box combines with the pre-drag selection.
type MarqueeMode uint8

const (
	MarqueeReplace MarqueeMode = iota
	MarqueeToggle
	MarqueeAdd
)

// MarqueeItem pairs an entry path with its hit-test rectangle in scroll-content coordinates.
type MarqueeItem struct {
	Path string
	Rect MarqueeRect
}

// NormalizeRect builds a non-negative-size rectangle from any two opposite corners.
func NormalizeRect(x0, y0, x1, y1 float32) MarqueeRect {
	x := min(x0, x1)
	y := min(y0, y1)
	return MarqueeRect{
		X: x,
		Y: y,
		W: max(x0, x1) - x,
		H: max(y0, y1) - y,
	}
}

// DragHitBox converts a drag segment into a hit-test box of at least 1x1 DIPs so purely vertical or horizontal drags intersect entries.
func DragHitBox(x0, y0, x1, y1 float32) MarqueeRect {
	r := NormalizeRect(x0, y0, x1, y1)
	if r.W < 1 {
		r.W = 1
	}
	if r.H < 1 {
		r.H = 1
	}
	return r
}

// ClampRect restricts r to [0, maxW] x [0, maxH].
func ClampRect(r MarqueeRect, maxW, maxH float32) MarqueeRect {
	x0 := max(0, min(r.X, maxW))
	y0 := max(0, min(r.Y, maxH))
	x1 := max(0, min(r.X+r.W, maxW))
	y1 := max(0, min(r.Y+r.H, maxH))
	return MarqueeRect{
		X: x0,
		Y: y0,
		W: max(0, x1-x0),
		H: max(0, y1-y0),
	}
}

// RectsIntersect reports whether a and b share a positive-area overlap.
func RectsIntersect(a, b MarqueeRect) bool {
	return a.W > 0 && a.H > 0 && b.W > 0 && b.H > 0 &&
		a.X < b.X+b.W && a.X+a.W > b.X &&
		a.Y < b.Y+b.H && a.Y+a.H > b.Y
}

// RectContainsPoint reports whether (x, y) lies inside non-empty rectangle r.
func RectContainsPoint(r MarqueeRect, x, y float32) bool {
	return r.W > 0 && r.H > 0 &&
		x >= r.X && x <= r.X+r.W &&
		y >= r.Y && y <= r.Y+r.H
}

// ListMarqueeItems returns scroll-content row rectangles across all entries so virtualized off-screen rows can be marquee-selected.
func ListMarqueeItems(entries []agent.Entry, rowH, viewportW float32) []MarqueeItem {
	if rowH <= 0 || viewportW <= 0 || len(entries) == 0 {
		return nil
	}
	items := make([]MarqueeItem, len(entries))
	for i, e := range entries {
		items[i] = MarqueeItem{
			Path: e.Path,
			Rect: MarqueeRect{
				X: 0,
				Y: float32(i) * rowH,
				W: viewportW,
				H: rowH,
			},
		}
	}
	return items
}

// ComputeMarqueeSelection reduces the pre-drag base selection with box and items into the next selection map and Shift anchor.
func ComputeMarqueeSelection(base map[string]bool, mode MarqueeMode, box MarqueeRect, items []MarqueeItem) (map[string]bool, string) {
	out := make(map[string]bool, len(base))
	if mode == MarqueeAdd || mode == MarqueeToggle {
		for k, v := range base {
			if v {
				out[k] = true
			}
		}
	}
	firstHit := ""
	for _, it := range items {
		if !RectsIntersect(box, it.Rect) {
			continue
		}
		if firstHit == "" {
			firstHit = it.Path
		}
		switch mode {
		case MarqueeToggle:
			if out[it.Path] {
				delete(out, it.Path)
			} else {
				out[it.Path] = true
			}
		default:
			out[it.Path] = true
		}
	}
	anchor := firstHit
	if anchor == "" {
		for _, it := range items {
			if out[it.Path] {
				anchor = it.Path
				break
			}
		}
	}
	return out, anchor
}
