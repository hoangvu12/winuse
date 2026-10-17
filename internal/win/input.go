package win

// Mouse and keyboard input injection for Actions (ticket 05 and later):
// SendInput, cursor positioning and the hit-test that guarantees a click
// lands on the target Window (not on whatever covers it). All coordinates
// are physical pixels (the process is per-monitor-v2 DPI aware). No input
// is ever injected from the CLI process — only the helper acts.

import (
	"fmt"
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
)

// inputTypeMouse is INPUT's type for MOUSEINPUT.
const inputTypeMouse = 0

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
