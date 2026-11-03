package win

import (
	"testing"
)

// zorder is a faked z-order, top first, with the windows' kinds.
type zorder struct {
	order   []uintptr
	cursors map[uintptr]bool
	topmost map[uintptr]bool
}

func (z zorder) prev(h uintptr) uintptr {
	for i, w := range z.order {
		if w == h && i > 0 {
			return z.order[i-1]
		}
	}
	return 0
}

func (z zorder) insertAfter(anchor uintptr) uintptr {
	return cursorInsertAfter(anchor, z.prev, func(h uintptr) bool { return z.cursors[h] }, func(h uintptr) bool { return z.topmost[h] })
}

func TestCursorInsertAfterPutsTheCursorJustAboveItsWindow(t *testing.T) {
	// Top to bottom: the taskbar (topmost), the user's Window 10, another
	// Agent's cursor window 90 above the target 20, then 30.
	z := zorder{
		order:   []uintptr{1, 10, 90, 20, 30},
		cursors: map[uintptr]bool{90: true},
		topmost: map[uintptr]bool{1: true},
	}
	// Above 20: inserted after the user's 10, passing over the other
	// cursor, so the user's Window still covers it.
	if got := z.insertAfter(20); got != 10 {
		t.Errorf("above window 20: insert after %d, want 10 (the user's Window above it)", got)
	}
	if got := z.insertAfter(30); got != 20 {
		t.Errorf("above window 30: insert after %d, want 20", got)
	}
	// The top non-topmost Window: above it is the topmost taskbar, so the
	// cursor goes to the top of the non-topmost band, never topmost.
	if got := z.insertAfter(10); got != hwndTop {
		t.Errorf("above window 10: insert after %d, want HWND_TOP", got)
	}
	// The very top, and no target at all.
	only := zorder{order: []uintptr{5}}
	if got := only.insertAfter(5); got != hwndTop {
		t.Errorf("above the top window: insert after %d, want HWND_TOP", got)
	}
	if got := z.insertAfter(0); got != hwndTop {
		t.Errorf("no anchor: insert after %d, want HWND_TOP", got)
	}
	// Only cursor windows above: HWND_TOP.
	cursorsOnly := zorder{order: []uintptr{91, 92, 20}, cursors: map[uintptr]bool{91: true, 92: true}}
	if got := cursorsOnly.insertAfter(20); got != hwndTop {
		t.Errorf("only cursors above: insert after %d, want HWND_TOP", got)
	}
}

func TestTravelAboveAndClickAbovePassTheAnchor(t *testing.T) {
	view := &fakeCursorView{}
	clock := &fakeClock{t: t0}
	c := newAgentCursor(inlineDo, view, clock.now)
	defer c.Close()
	go c.TravelAbove(4242, Rect{Right: 100, Bottom: 100}, Rect{Left: 10, Top: 10, Right: 20, Bottom: 20}, "click")
	waitFor(t, "the travel's raise", func() bool {
		var n int
		view.with(func(s *fakeViewState) { n = len(s.anchors) })
		return n >= 1
	})
	c.ClickAbove(777, Rect{Left: 10, Top: 10, Right: 20, Bottom: 20})
	waitFor(t, "the click's raise", func() bool {
		var n int
		view.with(func(s *fakeViewState) { n = len(s.anchors) })
		return n >= 2
	})
	view.with(func(s *fakeViewState) {
		if s.anchors[0] != 4242 || s.anchors[1] != 777 {
			t.Errorf("anchors = %v, want [4242 777]", s.anchors)
		}
	})
}

func TestParseCursorColor(t *testing.T) {
	cases := map[string]bool{"#FF8A3D": true, "ff8a3d": true, " #00aaFF ": true, "": false, "#fff": false, "red": false, "#GG0000": false, "#11223344": false}
	for in, ok := range cases {
		c, got := ParseCursorColor(in)
		if got != ok {
			t.Errorf("ParseCursorColor(%q) ok = %v, want %v", in, got, ok)
		}
		if ok && c.A != 0xFF {
			t.Errorf("ParseCursorColor(%q) = %v, want opaque", in, c)
		}
	}
	if c, _ := ParseCursorColor("#112233"); c.R != 0x11 || c.G != 0x22 || c.B != 0x33 {
		t.Errorf("ParseCursorColor(#112233) = %v", c)
	}
	for _, p := range CursorPalette {
		if p == DefaultCursorColor() {
			t.Errorf("the palette repeats the default Agent's colour %v", p)
		}
	}
}
