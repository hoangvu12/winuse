package win

// Mouse and keyboard input injection for Actions (ticket 05 and later):
// SendInput, cursor positioning and the hit-test that guarantees a click
// lands on the target Window (not on whatever covers it). Ticket 10 adds
// the pointer Actions on top of the same primitives: the wheel scroll
// (WHEEL/HWHEEL), the drag (button down, interpolated absolute moves,
// button up) and the hover (an injected move that leaves the pointer at
// the point). All coordinates are physical pixels (the process is
// per-monitor-v2 DPI aware). No input is ever injected from the CLI
// process — only the helper acts.

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// MouseButton is a canonical mouse button (Codex's mouse_button values,
// canonicalized by the helper before it reaches this layer).
type MouseButton int

// The three buttons SendInput can click.
const (
	MouseLeft MouseButton = iota
	MouseRight
	MouseMiddle
)

// mouse input flags (SendInput docs, win32.md §6).
const (
	mouseEventfMove      = 0x0001
	mouseEventfLeftDown  = 0x0002
	mouseEventfLeftUp    = 0x0004
	mouseEventfRightDown = 0x0008
	mouseEventfRightUp   = 0x0010
	mouseEventfMidDown   = 0x0020
	mouseEventfMidUp     = 0x0040
	mouseEventfWheel     = 0x0800
	mouseEventfHWheel    = 0x1000
	// Absolute moves are normalized 0..65535 over the whole virtual
	// desktop, so they work on any monitor, negative coordinates included
	// (win32.md §6; the fix for Codex's pointer-never-reaches-the-target
	// dual-monitor bug).
	mouseEventfAbsolute    = 0x8000
	mouseEventfVirtualDesk = 0x4000
)

// inputTypeMouse is INPUT's type for MOUSEINPUT.
const inputTypeMouse = 0

// WheelDelta is the amount one wheel notch scrolls (the Windows
// standard, WHEEL_DELTA): the unit scroll amounts are expressed in.
const WheelDelta = 120

// maxWheelChunk is the largest delta one wheel event carries: the delta
// travels as a signed 16-bit value in the wheel messages' wParam. A
// multiple of WheelDelta, so whole notches stay whole when a huge
// amount is split across events. The helper bounds scroll amounts
// (maxScrollUnits) well within what this can chunk through.
const maxWheelChunk = 273 * WheelDelta

// injectedTag marks every event winuse injects ("WINU"): the low-level
// keyboard hook of the later Esc ticket sees the injected flag anyway, and
// the tag additionally tells our own input apart from other injectors
// (win32.md §6).
const injectedTag = 0x57494E55

var (
	procSetCursorPos    = user32.NewProc("SetCursorPos")
	procGetCursorPos    = user32.NewProc("GetCursorPos")
	procWindowFromPoint = user32.NewProc("WindowFromPoint")
	procGetAncestor     = user32.NewProc("GetAncestor")
	procSendInput       = user32.NewProc("SendInput")
)

// point is the Win32 POINT, in physical pixels.
type point struct{ X, Y int32 }

// mouseInput is the Win32 MOUSEINPUT struct (32 bytes on amd64 and arm64:
// two int32s, three uint32s, alignment pad, one pointer).
type mouseInput struct {
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

// input is the Win32 INPUT struct: type, alignment pad, and a 32-byte
// union (40 bytes total). The array-size assertion pins the ABI.
type input struct {
	Type uint32
	_    uint32
	U    [32]byte
}

var _ [40]byte = [unsafe.Sizeof(input{})]byte{}
var _ [32]byte = [unsafe.Sizeof(mouseInput{})]byte{}

// OcclusionError reports that the point to click is covered by a window
// outside the target Window's group: injecting the click there would land
// in another app (Codex's "point is over …, not target window" class).
type OcclusionError struct {
	X, Y      int32
	OverTitle string // the covering window's title, when one can be read
}

func (e *OcclusionError) Error() string {
	if e.OverTitle != "" {
		return fmt.Sprintf("point (%d, %d) is over %q, not the target Window", e.X, e.Y, e.OverTitle)
	}
	return fmt.Sprintf("point (%d, %d) is over another window, not the target Window", e.X, e.Y)
}

// MouseClick clicks at the screen point (x, y) — physical pixels — over
// the Window target: it moves the cursor there, verifies the move, checks
// the point is over the target's window group (the Window itself, its
// children, or its owned popups such as menus and tooltips), then injects
// count press/release pairs of button in one SendInput batch (so double
// and triple clicks register as such). count must be at least 1.
func MouseClick(target uint64, x, y int32, button MouseButton, count int) error {
	if err := moveCursor(x, y); err != nil {
		return err
	}
	if err := checkOverTarget(target, x, y); err != nil {
		return err
	}
	if err := sendInput(clickEvents(button, count)...); err != nil {
		return fmt.Errorf("injecting the click failed: %v", err)
	}
	return nil
}

// moveCursor positions the cursor at (x, y) with SetCursorPos (pixel-exact
// under PMv2, unlike absolute SendInput moves whose rounding can drift a
// physical pixel) and verifies it arrived, retrying once.
func moveCursor(x, y int32) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		if r, _, _ := procSetCursorPos.Call(uintptr(int32ToU32(x)), uintptr(int32ToU32(y))); r != 0 {
			var p point
			if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p))); r != 0 && p.X == x && p.Y == y {
				return nil
			}
		}
	}
	return fmt.Errorf("the pointer did not reach (%d, %d)", x, y)
}

// checkOverTarget verifies the point (x, y) is over a window of target's
// group: the deepest window under the point, normalized to its root, then
// its owner chain — an owned popup of the target (a menu, a tooltip, a
// dropdown) is a valid place for a target Element's point to land.
func checkOverTarget(target uint64, x, y int32) error {
	p := point{X: x, Y: y}
	h, _, _ := procWindowFromPoint.Call(uintptr(unsafe.Pointer(&p)))
	if h == 0 {
		return &OcclusionError{X: x, Y: y}
	}
	root := rootWindow(h)
	for cur := root; cur != 0; cur = uintptr(ownerOf(windows.HWND(cur))) {
		if uint64(cur) == target {
			return nil
		}
	}
	return &OcclusionError{X: x, Y: y, OverTitle: windowText(windows.HWND(root))}
}

// rootWindow returns h's root window (GetAncestor GA_ROOT; h itself when
// h is top-level), falling back to h when the call fails.
func rootWindow(h uintptr) uintptr {
	const gaRoot = 3
	r, _, _ := procGetAncestor.Call(h, gaRoot)
	if r == 0 {
		return h
	}
	return r
}

// clickEvents builds count press/release pairs of button as one batch of
// INPUT events, sent in a single SendInput call so the timestamps stay
// close enough for double- and triple-click detection.
func clickEvents(button MouseButton, count int) []input {
	down, up := buttonFlags(button)
	events := make([]input, 0, 2*count)
	for i := 0; i < count; i++ {
		events = append(events, mouseEvent(down), mouseEvent(up))
	}
	return events
}

// buttonFlags returns the down and up event flags of button.
func buttonFlags(button MouseButton) (down, up uint32) {
	switch button {
	case MouseRight:
		return mouseEventfRightDown, mouseEventfRightUp
	case MouseMiddle:
		return mouseEventfMidDown, mouseEventfMidUp
	default:
		return mouseEventfLeftDown, mouseEventfLeftUp
	}
}

// mouseEvent builds one INPUT carrying a MOUSEINPUT with the given flags.
func mouseEvent(flags uint32) input {
	mi := mouseInput{Flags: flags, ExtraInfo: injectedTag}
	var in input
	in.Type = inputTypeMouse
	*(*mouseInput)(unsafe.Pointer(&in.U)) = mi
	return in
}

// sendInput injects events in one SendInput call.
func sendInput(events ...input) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	n, _, e := procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	if int(n) != len(events) {
		return fmt.Errorf("SendInput rejected %d of %d events (error %v)", len(events)-int(n), len(events), e)
	}
	return nil
}

// int32ToU32 widens a signed coordinate for by-value uintptr passing; the
// low 32 bits are what the ABI reads, so sign-extension is harmless.
func int32ToU32(v int32) uint32 {
	return uint32(v)
}

// ---- The pointer Actions (ticket 10) ----

// MouseScroll scrolls over the Window target at the screen point (x, y)
// — physical pixels — by the amounts dx, dy: positive dx scrolls right,
// positive dy scrolls down (Codex's scroll semantics; the sign flip to
// the wheel's "positive is up" is scrollEvents', below). The point is
// verified to be over the target's window group first (a point over
// another window would scroll that app), the cursor is moved there (the
// wheel goes to the window under the cursor, or the focused window when
// "scroll inactive windows" is off — the target is the foreground
// window either way, the Action pipeline activated it), then the wheel
// events are injected. dx = dy = 0 scrolls nothing.
func MouseScroll(target uint64, x, y, dx, dy int32) error {
	if err := checkOverTarget(target, x, y); err != nil {
		return err
	}
	if err := moveCursor(x, y); err != nil {
		return err
	}
	if err := sendInput(scrollEvents(dx, dy)...); err != nil {
		return fmt.Errorf("injecting the scroll failed: %v", err)
	}
	return nil
}

// scrollEvents converts scroll amounts — Codex's semantics, positive
// dy scrolls down — into wheel events with SendInput's semantics: a
// positive WHEEL delta scrolls up (away from the user) and a positive
// HWHEEL delta scrolls right (win32.md §6), so the vertical axis flips
// its sign and the horizontal does not. Both axes are injected even
// together (apps accumulate wheel deltas across events).
func scrollEvents(dx, dy int32) []input {
	events := wheelEvents(mouseEventfHWheel, dx)
	events = append(events, wheelEvents(mouseEventfWheel, -dy)...)
	return events
}

// wheelEvents splits one axis's wheel delta into events, each within
// the signed 16-bit field the wheel messages carry. Deltas of normal
// size are one event; a huge amount becomes several events whose
// deltas apps accumulate.
func wheelEvents(flags uint32, delta int32) []input {
	if delta == 0 {
		return nil
	}
	var events []input
	for d := delta; d != 0; {
		chunk := d
		if chunk > maxWheelChunk {
			chunk = maxWheelChunk
		} else if chunk < -maxWheelChunk {
			chunk = -maxWheelChunk
		}
		events = append(events, wheelEvent(flags, chunk))
		d -= chunk
	}
	return events
}

// wheelEvent builds one wheel INPUT: MouseData carries the delta as the
// unsigned field's two's complement (negative deltas included), the
// injected tag marks it as winuse's own input.
func wheelEvent(flags uint32, delta int32) input {
	mi := mouseInput{MouseData: uint32(delta), Flags: flags, ExtraInfo: injectedTag}
	var in input
	in.Type = inputTypeMouse
	*(*mouseInput)(unsafe.Pointer(&in.U)) = mi
	return in
}

// MouseDrag drags over the Window target from screen point (fx, fy) to
// (tx, ty) — physical pixels — with the left button (Codex: non-left
// drag is not supported): the pointer is placed pixel-exactly at the
// start, the button goes down, interpolated injected absolute moves
// carry the pointer to the end in steps (so apps register a drag —
// menus track, canvases draw, lists reorder — instead of a teleport),
// the exact end point is pinned, and the button comes up. The button
// is released even when a move fails, so a failed drag never leaves it
// held.
func MouseDrag(target uint64, fx, fy, tx, ty int32) error {
	// Both endpoints are verified before anything moves: a drag whose
	// start or end is over another window would drag that app's content.
	if err := checkOverTarget(target, fx, fy); err != nil {
		return err
	}
	if err := checkOverTarget(target, tx, ty); err != nil {
		return err
	}
	if err := moveCursor(fx, fy); err != nil {
		return err
	}
	if err := sendInput(mouseEvent(mouseEventfLeftDown)); err != nil {
		return fmt.Errorf("injecting the drag press failed: %v", err)
	}
	err := dragSteps(fx, fy, tx, ty)
	// Always release: a drag that failed part-way must not leave the
	// button held (the pointer would drag whatever it passes on the
	// user's next physical click).
	if upErr := sendInput(mouseEvent(mouseEventfLeftUp)); upErr != nil && err == nil {
		err = fmt.Errorf("injecting the drag release failed: %v", upErr)
	}
	return err
}

// The drag interpolation's shape (win32.md §6): about 10–20 injected
// moves with ~10 ms gaps between button-down and button-up.
const (
	dragStepCount = 16
	dragStepDelay = 10 * time.Millisecond
)

// dragSteps interpolates the injected absolute moves from (fx, fy) to
// (tx, ty) — one SendInput per step, ~10 ms apart so apps pump the
// moves as separate positions — and pins the cursor on the exact end
// point (the absolute normalization can round a step off by a pixel).
func dragSteps(fx, fy, tx, ty int32) error {
	for i := 1; i <= dragStepCount; i++ {
		x, y := dragStepPoint(fx, fy, tx, ty, i, dragStepCount)
		if err := sendInput(absoluteMoveInput(x, y)); err != nil {
			return fmt.Errorf("injecting a drag move failed: %v", err)
		}
		if i < dragStepCount {
			time.Sleep(dragStepDelay)
		}
	}
	return moveCursor(tx, ty)
}

// dragStepPoint is the drag's i-th of n interpolation steps from
// (fx, fy) toward (tx, ty); i = n is the end point. int64 math, so a
// drag across the whole virtual desktop cannot overflow.
func dragStepPoint(fx, fy, tx, ty int32, i, n int) (int32, int32) {
	x := fx + int32(int64(tx-fx)*int64(i)/int64(n))
	y := fy + int32(int64(ty-fy)*int64(i)/int64(n))
	return x, y
}

// MouseHover moves the pointer over the Window target to the screen
// point (x, y) — physical pixels — and leaves it there, so hover-only
// UI (toolbars, tooltips) appears and stays: the point is verified to
// be over the target's window group, the pointer is placed pixel-
// exactly, and an injected absolute move follows — some apps (Chromium)
// update hover state only on injected moves (win32.md §6) — before the
// exact position is re-pinned (the absolute move can round off a
// pixel).
func MouseHover(target uint64, x, y int32) error {
	if err := checkOverTarget(target, x, y); err != nil {
		return err
	}
	if err := moveCursor(x, y); err != nil {
		return err
	}
	if err := sendInput(absoluteMoveInput(x, y)); err != nil {
		return fmt.Errorf("injecting the hover move failed: %v", err)
	}
	return moveCursor(x, y)
}

// absoluteMoveInput builds one injected MOVE|ABSOLUTE|VIRTUALDESK event
// whose normalized coordinates resolve to (x, y) on the current virtual
// desktop.
func absoluteMoveInput(x, y int32) input {
	return absoluteMoveEvent(x, y, VirtualScreenRect())
}

// absoluteMoveEvent builds the injected absolute move for (x, y)
// against a virtual desktop rectangle — split out so the normalization
// is offline-testable.
func absoluteMoveEvent(x, y int32, vs Rect) input {
	mi := mouseInput{
		Dx:        normalizeAbsolute(x, vs.Left, vs.Width()),
		Dy:        normalizeAbsolute(y, vs.Top, vs.Height()),
		Flags:     mouseEventfMove | mouseEventfAbsolute | mouseEventfVirtualDesk,
		ExtraInfo: injectedTag,
	}
	var in input
	in.Type = inputTypeMouse
	*(*mouseInput)(unsafe.Pointer(&in.U)) = mi
	return in
}

// normalizeAbsolute maps a physical pixel coordinate to SendInput's
// 0..65535 normalized axis over the virtual desktop, rounding so the
// OS's denormalization resolves back to the same pixel whenever the
// geometry allows (win32.md §6). Degenerate extents map to 0.
func normalizeAbsolute(v, origin, extent int32) int32 {
	if extent <= 1 {
		return 0
	}
	return int32((int64(v-origin)*65535 + int64(extent-1)/2) / int64(extent-1))
}
