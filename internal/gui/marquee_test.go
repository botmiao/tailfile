package gui

import (
	"reflect"
	"testing"

	"tailfile.local/internal/agent"
)

func TestNormalizeAndClampRect(t *testing.T) {
	cases := []struct {
		name           string
		x0, y0, x1, y1 float32
		want           MarqueeRect
	}{
		{"TL-BR", 10, 20, 60, 90, MarqueeRect{X: 10, Y: 20, W: 50, H: 70}},
		{"BR-TL", 60, 90, 10, 20, MarqueeRect{X: 10, Y: 20, W: 50, H: 70}},
		{"TR-BL", 60, 20, 10, 90, MarqueeRect{X: 10, Y: 20, W: 50, H: 70}},
		{"BL-TR", 10, 90, 60, 20, MarqueeRect{X: 10, Y: 20, W: 50, H: 70}},
	}
	for _, tc := range cases {
		if got := NormalizeRect(tc.x0, tc.y0, tc.x1, tc.y1); got != tc.want {
			t.Fatalf("%s: NormalizeRect = %+v, want %+v", tc.name, got, tc.want)
		}
	}

	clamped := ClampRect(MarqueeRect{X: -15, Y: 25, W: 200, H: 120}, 100, 80)
	wantClamped := MarqueeRect{X: 0, Y: 25, W: 100, H: 55}
	if clamped != wantClamped {
		t.Fatalf("ClampRect = %+v, want %+v", clamped, wantClamped)
	}
	vert := DragHitBox(50, 10, 50, 70)
	if vert != (MarqueeRect{X: 50, Y: 10, W: 1, H: 60}) {
		t.Fatalf("DragHitBox vertical = %+v", vert)
	}
}

func TestRectsIntersectAndContains(t *testing.T) {
	a := MarqueeRect{X: 10, Y: 10, W: 40, H: 30}
	if !RectsIntersect(a, MarqueeRect{X: 49, Y: 39, W: 20, H: 20}) {
		t.Fatal("expected overlapping rects to intersect")
	}
	if RectsIntersect(a, MarqueeRect{X: 50, Y: 10, W: 20, H: 30}) {
		t.Fatal("touching edges should not count as intersection")
	}
	if RectsIntersect(a, MarqueeRect{X: 20, Y: 20, W: 0, H: 10}) {
		t.Fatal("zero-width rect should not intersect")
	}
	if !RectContainsPoint(a, 10, 10) || !RectContainsPoint(a, 50, 40) || RectContainsPoint(a, 51, 20) {
		t.Fatal("unexpected RectContainsPoint result")
	}
}

func TestComputeMarqueeSelectionModes(t *testing.T) {
	items := ListMarqueeItems([]agent.Entry{
		{Name: "a", Path: "/root/a"},
		{Name: "b", Path: "/root/b"},
		{Name: "c", Path: "/root/c"},
	}, 34, 400)
	if len(items) != 3 || items[0].Rect != (MarqueeRect{0, 0, 400, 34}) || items[2].Rect != (MarqueeRect{0, 68, 400, 34}) {
		t.Fatalf("unexpected ListMarqueeItems: %+v", items)
	}

	// Replace: only hits b & c.
	boxBC := MarqueeRect{X: 10, Y: 40, W: 100, H: 40}
	sel, anchor := ComputeMarqueeSelection(map[string]bool{"/root/a": true}, MarqueeReplace, boxBC, items)
	if !reflect.DeepEqual(sel, map[string]bool{"/root/b": true, "/root/c": true}) || anchor != "/root/b" {
		t.Fatalf("Replace got (%v, %q)", sel, anchor)
	}

	// Add: keeps a and adds b & c.
	sel, anchor = ComputeMarqueeSelection(map[string]bool{"/root/a": true}, MarqueeAdd, boxBC, items)
	if !reflect.DeepEqual(sel, map[string]bool{"/root/a": true, "/root/b": true, "/root/c": true}) || anchor != "/root/b" {
		t.Fatalf("Add got (%v, %q)", sel, anchor)
	}

	// Toggle: pre-selected a & c; box over a & b flips a off and b on, keeping c on.
	base := map[string]bool{"/root/a": true, "/root/c": true}
	boxAB := MarqueeRect{X: 10, Y: 10, W: 100, H: 40}
	sel, anchor = ComputeMarqueeSelection(base, MarqueeToggle, boxAB, items)
	if !reflect.DeepEqual(sel, map[string]bool{"/root/b": true, "/root/c": true}) || anchor != "/root/a" {
		t.Fatalf("Toggle AB got (%v, %q)", sel, anchor)
	}

	// Shrinking Toggle box back to only a leaves b restored off and flips only a off, keeping c on.
	boxA := MarqueeRect{X: 10, Y: 10, W: 100, H: 10}
	sel, anchor = ComputeMarqueeSelection(base, MarqueeToggle, boxA, items)
	if !reflect.DeepEqual(sel, map[string]bool{"/root/c": true}) || anchor != "/root/a" {
		t.Fatalf("Toggle shrunk back to A got (%v, %q)", sel, anchor)
	}

	// Non-intersecting box in Add mode preserves base and sets anchor to first selected item.
	sel, anchor = ComputeMarqueeSelection(map[string]bool{"/root/c": true}, MarqueeAdd, MarqueeRect{X: 10, Y: 150, W: 50, H: 20}, items)
	if !reflect.DeepEqual(sel, map[string]bool{"/root/c": true}) || anchor != "/root/c" {
		t.Fatalf("Non-intersecting Add got (%v, %q)", sel, anchor)
	}
}
