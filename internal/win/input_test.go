package win

// Offline tests for the input layer's pure half (event building, flags,
// ABI sizes). The injection itself (SetCursorPos, SendInput,
// WindowFromPoint) drives the live desktop and is covered only by the
// opt-in e2e suite.

import (
	"errors"
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

func TestScrollEventsFollowCodexSigns(t *testing.T) {
	// Codex: positive scrollY scrolls DOWN; SendInput's WHEEL: positive
	// scrolls UP. The vertical axis flips, the horizontal does not.
	events := scrollEvents(240, 600)
	if len(events) != 2 {
		t.Fatalf("scroll(240, 600) built %d events, want 2 (one per axis)", len(events))
	}
	hwheel := decodeMouse(t, events[0])
	if hwheel.Flags != mouseEventfHWheel || int32(hwheel.MouseData) != 240 {
		t.Errorf("horizontal event = %+v, want HWHEEL with delta 240", hwheel)
	}
	wheel := decodeMouse(t, events[1])
	if wheel.Flags != mouseEventfWheel || int32(wheel.MouseData) != -600 {
		t.Errorf("vertical event = %+v, want WHEEL with delta -600 (600 down)", wheel)
	}

	// Scrolling up is a positive wheel delta; a zero axis sends nothing.
	events = scrollEvents(0, -240)
	if len(events) != 1 {
		t.Fatalf("scroll(0, -240) built %d events, want 1", len(events))
	}
	if wheel := decodeMouse(t, events[0]); wheel.Flags != mouseEventfWheel || int32(wheel.MouseData) != 240 {
		t.Errorf("vertical event = %+v, want WHEEL with delta 240 (240 up)", wheel)
	}
	if events := scrollEvents(0, 0); len(events) != 0 {
		t.Errorf("scroll(0, 0) built %d events, want none", len(events))
	}
}

func TestScrollEventsChunkHugeAmounts(t *testing.T) {
	// The wheel delta travels in a signed 16-bit field: a huge amount is
	// split into events apps accumulate, each within the field.
	events := scrollEvents(0, 2*maxWheelChunk+120)
	if len(events) != 3 {
		t.Fatalf("built %d events, want 3 (two chunks plus the remainder)", len(events))
	}
	total := 0
	for _, ev := range events {
		mi := decodeMouse(t, ev)
		if mi.Flags != mouseEventfWheel {
			t.Fatalf("event flags = 0x%x, want WHEEL", mi.Flags)
		}
		if d := int32(mi.MouseData); d < -maxWheelChunk || d > maxWheelChunk {
			t.Errorf("chunk delta %d escapes the 16-bit field", d)
		} else {
			total += -int(d) // deltas are negative for downward amounts
		}
	}
	if total != 2*maxWheelChunk+120 {
		t.Errorf("chunks sum to %d, want %d", total, 2*maxWheelChunk+120)
	}

	// A negative huge amount chunks too.
	events = scrollEvents(-maxWheelChunk-240, 0)
	if len(events) != 2 {
		t.Fatalf("built %d events, want 2", len(events))
	}
	if mi := decodeMouse(t, events[0]); int32(mi.MouseData) != -maxWheelChunk {
		t.Errorf("first chunk = %d, want %d", int32(mi.MouseData), -maxWheelChunk)
	}
}

func TestWheelEventsTagInput(t *testing.T) {
	for _, flags := range []uint32{mouseEventfWheel, mouseEventfHWheel} {
		for _, delta := range []int32{120, -120, 600} {
			mi := decodeMouse(t, wheelEvent(flags, delta))
			if mi.MouseData != uint32(delta) || mi.ExtraInfo != injectedTag {
				t.Errorf("wheel event = %+v, want delta %d and the winuse tag", mi, delta)
			}
		}
	}
}

func TestAbsoluteMoveEvent(t *testing.T) {
	// A single-monitor 1920×1080 desktop at the origin: the normalized
	// coordinates of a point denormalize back to it.
	vs := Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	mi := decodeMouse(t, absoluteMoveEvent(960, 540, vs))
	if mi.Flags != mouseEventfMove|mouseEventfAbsolute|mouseEventfVirtualDesk {
		t.Errorf("flags = 0x%x, want MOVE|ABSOLUTE|VIRTUALDESK", mi.Flags)
	}
	if mi.ExtraInfo != injectedTag {
		t.Errorf("ExtraInfo = 0x%x, want the winuse tag", mi.ExtraInfo)
	}
	if x := denorm(mi.Dx, 1920); x != 960 {
		t.Errorf("x 960 normalizes to %d, denormalizes to %d", mi.Dx, x)
	}
	if y := denorm(mi.Dy, 1080); y != 540 {
		t.Errorf("y 540 normalizes to %d, denormalizes to %d", mi.Dy, y)
	}
}

func TestAbsoluteMoveEventAnyMonitor(t *testing.T) {
	// A dual-monitor desktop with a negative-coordinate monitor on the
	// left: the virtual screen is (-1920, 0)..(1920, 1080). Points on
	// both monitors — negative coordinates included — denormalize back
	// to within a pixel (the documented absolute-move rounding).
	vs := Rect{Left: -1920, Top: 0, Right: 1920, Bottom: 1080}
	for _, p := range [][2]int32{{-1920, 0}, {-1000, 540}, {-1, 1079}, {0, 0}, {960, 540}, {1919, 1079}} {
		mi := decodeMouse(t, absoluteMoveEvent(p[0], p[1], vs))
		if got := denorm(mi.Dx, 3840) + vs.Left; abs32(got-p[0]) > 1 {
			t.Errorf("x = %d normalizes to %d, denormalizes to %d, want %d within 1", p[0], mi.Dx, got, p[0])
		}
		if got := denorm(mi.Dy, 1080); abs32(got-p[1]) > 1 {
			t.Errorf("y = %d denormalizes to %d, want %d within 1", p[1], got, p[1])
		}
	}
}

func TestNormalizeAbsolute(t *testing.T) {
	// The ends of the axis map to the ends of the normalized range, and
	// every pixel of a desktop round-trips to within a pixel (the
	// documented rounding of absolute moves).
	if got := normalizeAbsolute(0, 0, 1920); got != 0 {
		t.Errorf("axis start = %d, want 0", got)
	}
	if got := normalizeAbsolute(1919, 0, 1920); got != 65535 {
		t.Errorf("axis end = %d, want 65535", got)
	}
	if got := normalizeAbsolute(-1920, -1920, 3840); got != 0 {
		t.Errorf("negative-coordinate start = %d, want 0", got)
	}
	if got := normalizeAbsolute(1919, -1920, 3840); got != 65535 {
		t.Errorf("negative-coordinate end = %d, want 65535", got)
	}
	// Degenerate extents map to 0 rather than dividing by zero.
	if got := normalizeAbsolute(5, 0, 1); got != 0 {
		t.Errorf("degenerate extent = %d, want 0", got)
	}
	for v := int32(0); v < 1080; v += 7 {
		if got := denorm(normalizeAbsolute(v, 0, 1080), 1080); abs32(got-v) > 1 {
			t.Errorf("%d round-trips to %d", v, got)
		}
	}
}

func TestDragStepPoint(t *testing.T) {
	// The steps run monotonically from the start to the exact end, in
	// int64 math (a virtual-desktop-wide drag cannot overflow).
	fx, fy, tx, ty := int32(-1920), int32(100), int32(1919), int32(-700)
	prevX, prevY := fx, fy
	for i := 1; i <= 16; i++ {
		x, y := dragStepPoint(fx, fy, tx, ty, i, 16)
		if (x-prevX)*(tx-fx) < 0 || (y-prevY)*(ty-fy) < 0 {
			t.Errorf("step %d moved backwards: (%d, %d) after (%d, %d)", i, x, y, prevX, prevY)
		}
		prevX, prevY = x, y
	}
	if prevX != tx || prevY != ty {
		t.Errorf("last step = (%d, %d), want the exact end (%d, %d)", prevX, prevY, tx, ty)
	}

	// A zero-distance drag stays put.
	for i := 1; i <= 4; i++ {
		if x, y := dragStepPoint(5, 6, 5, 6, i, 4); x != 5 || y != 6 {
			t.Errorf("zero-distance step %d = (%d, %d)", i, x, y)
		}
	}
}

// decodeMouse unpacks an INPUT's MOUSEINPUT for assertions.
func decodeMouse(t *testing.T, ev input) mouseInput {
	t.Helper()
	if ev.Type != inputTypeMouse {
		t.Fatalf("event type = %d, want INPUT_MOUSE", ev.Type)
	}
	return *(*mouseInput)(unsafe.Pointer(&ev.U))
}

// denorm inverts the SendInput absolute normalization for one axis, the
// way the OS denormalizes a move event's coordinates (integer math,
// rounding down).
func denorm(n, extent int32) int32 {
	if extent <= 1 {
		return 0
	}
	return int32(int64(n) * int64(extent-1) / 65535)
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestDragStepsStopBeforeAnyInput pins finding 10's drag checkpoint: a
// stopped drag aborts before injecting anything (the interpolation loop
// polls the checkpoint before every step, so the events already injected
// are the only ones that landed).
func TestDragStepsStopBeforeAnyInput(t *testing.T) {
	err := dragSteps(0, 0, 100, 100, func() bool { return true })
	if !errors.Is(err, ErrEscStopped) {
		t.Fatalf("dragSteps = %v, want ErrEscStopped", err)
	}
}
