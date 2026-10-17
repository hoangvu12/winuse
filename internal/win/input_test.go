package win

// Offline tests for the input layer's pure half (event building, flags,
// ABI sizes). The injection itself (SetCursorPos, SendInput,
// WindowFromPoint) drives the live desktop and is covered only by the
// opt-in e2e suite.

import (
	"testing"
	"unsafe"
)

func TestInputABI(t *testing.T) {
	if unsafe.Sizeof(input{}) != 40 {
		t.Errorf("sizeof(INPUT) = %d, want 40", unsafe.Sizeof(input{}))
	}
	if unsafe.Sizeof(mouseInput{}) != 32 {
		t.Errorf("sizeof(MOUSEINPUT) = %d, want 32", unsafe.Sizeof(mouseInput{}))
	}
	if unsafe.Offsetof(mouseInput{}.ExtraInfo) != 24 {
		t.Errorf("ExtraInfo offset = %d, want 24 (the union must hold a pointer)", unsafe.Offsetof(mouseInput{}.ExtraInfo))
	}
}

func TestClickEvents(t *testing.T) {
	cases := []struct {
		button    MouseButton
		count     int
		wantFlags []uint32
	}{
		{MouseLeft, 1, []uint32{mouseEventfLeftDown, mouseEventfLeftUp}},
		{MouseLeft, 2, []uint32{mouseEventfLeftDown, mouseEventfLeftUp, mouseEventfLeftDown, mouseEventfLeftUp}},
		{MouseLeft, 3, []uint32{
			mouseEventfLeftDown, mouseEventfLeftUp,
			mouseEventfLeftDown, mouseEventfLeftUp,
			mouseEventfLeftDown, mouseEventfLeftUp,
		}},
		{MouseRight, 1, []uint32{mouseEventfRightDown, mouseEventfRightUp}},
		{MouseMiddle, 1, []uint32{mouseEventfMidDown, mouseEventfMidUp}},
	}
	for _, tc := range cases {
		events := clickEvents(tc.button, tc.count)
		if len(events) != len(tc.wantFlags) {
			t.Fatalf("%v x%d: %d events, want %d", tc.button, tc.count, len(events), len(tc.wantFlags))
		}
		for i, ev := range events {
			mi := *(*mouseInput)(unsafe.Pointer(&ev.U))
			if ev.Type != inputTypeMouse {
				t.Errorf("%v x%d event %d: type = %d, want INPUT_MOUSE", tc.button, tc.count, i, ev.Type)
			}
			if mi.Flags != tc.wantFlags[i] {
				t.Errorf("%v x%d event %d: flags = 0x%x, want 0x%x", tc.button, tc.count, i, mi.Flags, tc.wantFlags[i])
			}
			if mi.ExtraInfo != injectedTag {
				t.Errorf("%v x%d event %d: ExtraInfo = 0x%x, want the winuse tag", tc.button, tc.count, i, mi.ExtraInfo)
			}
			if mi.Dx != 0 || mi.Dy != 0 || mi.MouseData != 0 || mi.Time != 0 {
				t.Errorf("%v x%d event %d: stray MOUSEINPUT field: %+v", tc.button, tc.count, i, mi)
			}
		}
	}
}

func TestButtonFlagsCoverAllButtons(t *testing.T) {
	// Every button maps to distinct down/up flags (a swapped pair would
	// click the wrong button).
	seen := map[uint32]bool{}
	for _, b := range []MouseButton{MouseLeft, MouseRight, MouseMiddle} {
		down, up := buttonFlags(b)
		if down == up || seen[down] || seen[up] {
			t.Errorf("button %d reuses flags: down 0x%x up 0x%x", b, down, up)
		}
		seen[down], seen[up] = true, true
	}
}

func TestMouseEventIsAMoveEvent(t *testing.T) {
	// The activation workaround injects a zero-distance move: it must be a
	// MOVE event with no button flags and no displacement.
	ev := mouseEvent(mouseEventfMove)
	mi := *(*mouseInput)(unsafe.Pointer(&ev.U))
	if mi.Flags != mouseEventfMove || mi.Dx != 0 || mi.Dy != 0 {
		t.Errorf("zero move event = %+v, want a MOVE with no displacement", mi)
	}
}

func TestOcclusionErrorMessages(t *testing.T) {
	e := &OcclusionError{X: 660, Y: 430, OverTitle: "Extensions"}
	if want := `point (660, 430) is over "Extensions", not the target Window`; e.Error() != want {
		t.Errorf("error = %q, want %q", e.Error(), want)
	}
	e = &OcclusionError{X: 1, Y: 2}
	if want := "point (1, 2) is over another window, not the target Window"; e.Error() != want {
		t.Errorf("error = %q, want %q", e.Error(), want)
	}
}
