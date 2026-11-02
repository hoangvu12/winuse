package win

import "testing"

// The Window Actions' placement arithmetic (ADR 0011), offline: move-window
// and resize-window take the visible frame (the extended frame bounds, the
// screenshot line's `window at` rectangle), and SetWindowPos takes the
// GetWindowRect rectangle, invisible resize borders included. The Win32
// calls themselves never run here.

func TestOuterForFrameKeepsTheInvisibleBorders(t *testing.T) {
	// A Windows 11 Window at 100% scaling: 7 px invisible borders on the
	// left, right and bottom, none on top.
	outer := Rect{Left: 93, Top: 100, Right: 907, Bottom: 707}
	frame := Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}
	cases := []struct {
		name string
		want Rect
		out  Rect
	}{
		{"unchanged", frame, outer},
		{"moved", Rect{Left: 0, Top: 0, Right: 800, Bottom: 600},
			Rect{Left: -7, Top: 0, Right: 807, Bottom: 607}},
		{"moved to a left monitor", Rect{Left: -1920, Top: 40, Right: -1120, Bottom: 640},
			Rect{Left: -1927, Top: 40, Right: -1113, Bottom: 647}},
		{"resized", Rect{Left: 100, Top: 100, Right: 1380, Bottom: 820},
			Rect{Left: 93, Top: 100, Right: 1387, Bottom: 827}},
	}
	for _, c := range cases {
		if got := outerForFrame(outer, frame, c.want); got != c.out {
			t.Errorf("%s: outerForFrame(%+v) = %+v, want %+v", c.name, c.want, got, c.out)
		}
	}
}

func TestOuterForFrameWithoutBorders(t *testing.T) {
	// DWM off or unreadable: the frame is GetWindowRect itself, so the
	// requested rectangle goes to SetWindowPos unchanged.
	outer := Rect{Left: 10, Top: 20, Right: 410, Bottom: 320}
	want := Rect{Left: 50, Top: 60, Right: 690, Bottom: 540}
	if got := outerForFrame(outer, outer, want); got != want {
		t.Errorf("outerForFrame with no borders = %+v, want %+v", got, want)
	}
}

func TestPlacementFrameFallsBackToTheWindowRect(t *testing.T) {
	outer := Rect{Left: 93, Top: 100, Right: 907, Bottom: 707}
	frame := Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}
	if got := placementFrame(outer, frame, true); got != frame {
		t.Errorf("with DWM bounds: %+v, want the extended frame bounds %+v", got, frame)
	}
	if got := placementFrame(outer, frame, false); got != outer {
		t.Errorf("DWM call failed: %+v, want GetWindowRect %+v", got, outer)
	}
	if got := placementFrame(outer, Rect{}, true); got != outer {
		t.Errorf("empty DWM bounds: %+v, want GetWindowRect %+v", got, outer)
	}
}
