package win

// The Window Actions' Win32 layer (ADR 0011): restore, minimize, move,
// resize and close a Window without activating it. Every call here is
// non-activating and asynchronous towards the target: ShowWindowAsync and
// SetWindowPos(SWP_ASYNCWINDOWPOS) queue the change on the Window's own
// thread, and WM_CLOSE is posted, so a hung app can never block the
// helper. The read-backs (IsIconic, IsZoomed, WindowFrame,
// GetWindowPlacement, IsWindow) are the helper's verification; it polls
// them with a short bound, as Activate polls the foreground.
//
// Move and resize work in the visible frame: the DWM extended frame
// bounds, the rectangle the screenshot line prints as `window at X,Y WxH`.
// SetWindowPos takes GetWindowRect's rectangle instead, which includes the
// invisible resize borders Windows 10 and 11 draw outside the visible
// frame (about 7 px at 100% scaling on the left, right and bottom), so
// each call reads both rectangles and passes the outer one that gives the
// requested visible frame (outerForFrame).
//
// Each mutating call refuses inside a go test binary (LiveDesktopGuard):
// offline tests fake them through the helper's Services.

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procShowWindowAsync    = user32.NewProc("ShowWindowAsync")
	procIsZoomed           = user32.NewProc("IsZoomed")
	procIsWindow           = user32.NewProc("IsWindow")
	procPostMessageW       = user32.NewProc("PostMessageW")
	procGetWindowPlacement = user32.NewProc("GetWindowPlacement")
)

const (
	swShowMinNoActive     = 7      // SW_SHOWMINNOACTIVE: minimize, the active Window stays active
	swpNoZOrder           = 0x0004 // SWP_NOZORDER
	swpAsyncWindowPos     = 0x4000 // SWP_ASYNCWINDOWPOS: queue it on the Window's thread
	wmClose               = 0x0010 // WM_CLOSE
	wpfRestoreToMaximized = 0x0002 // WINDOWPLACEMENT.flags: a minimized Window comes back maximized

	// placementFlags never activate the Window or change its z-order.
	placementFlags = swpNoActivate | swpNoZOrder | swpAsyncWindowPos
)

// windowPlacement is the Win32 WINDOWPLACEMENT.
type windowPlacement struct {
	length        uint32
	flags         uint32
	showCmd       uint32
	ptMinPosition struct{ x, y int32 }
	ptMaxPosition struct{ x, y int32 }
	normal        Rect
}

// RestoreNoActivate restores a minimized or maximized Window to its normal
// size and position without activating it (ShowWindowAsync with
// SW_SHOWNOACTIVATE). Windows has no non-activating maximize, so a Window
// that was maximized before it was minimized comes back at its normal
// size.
func RestoreNoActivate(hwnd uint64) error {
	return showAsync(hwnd, swShowNoActivate, "SW_SHOWNOACTIVATE")
}

// MinimizeNoActivate minimizes a Window and leaves the active Window
// active (ShowWindowAsync with SW_SHOWMINNOACTIVE).
func MinimizeNoActivate(hwnd uint64) error {
	return showAsync(hwnd, swShowMinNoActive, "SW_SHOWMINNOACTIVE")
}

func showAsync(hwnd uint64, cmd uintptr, name string) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if r, _, err := procShowWindowAsync.Call(uintptr(hwnd), cmd); r == 0 {
		return fmt.Errorf("ShowWindowAsync(%s) failed for window %d: %v", name, hwnd, err)
	}
	return nil
}

// MoveNoActivate moves the Window's visible frame top-left to the screen
// point (x, y) in physical pixels, keeping its size and z-order, without
// activating it. The point is in the screenshot line's coordinates (the
// extended frame bounds), so a `window at X,Y` read there moves back to
// the same place.
func MoveNoActivate(hwnd uint64, x, y int32) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	outer, frame, err := placementRects(hwnd)
	if err != nil {
		return err
	}
	want := Rect{Left: x, Top: y, Right: x + frame.Width(), Bottom: y + frame.Height()}
	o := outerForFrame(outer, frame, want)
	return setWindowPos(hwnd, o.Left, o.Top, 0, 0, placementFlags|swpNoSize)
}

// ResizeNoActivate sets the Window's visible frame size (the screenshot
// line's `window at` width and height, see MoveNoActivate) in physical
// pixels, keeping its top-left and z-order, without activating it.
func ResizeNoActivate(hwnd uint64, width, height int32) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	outer, frame, err := placementRects(hwnd)
	if err != nil {
		return err
	}
	want := Rect{Left: frame.Left, Top: frame.Top, Right: frame.Left + width, Bottom: frame.Top + height}
	o := outerForFrame(outer, frame, want)
	return setWindowPos(hwnd, 0, 0, o.Width(), o.Height(), placementFlags|swpNoMove)
}

// outerForFrame returns the GetWindowRect rectangle that shows the visible
// frame want, given the Window's current GetWindowRect rectangle (outer)
// and visible frame (frame): each side keeps its invisible border inset,
// outer minus frame. With no borders (frame == outer) it returns want.
func outerForFrame(outer, frame, want Rect) Rect {
	return Rect{
		Left:   want.Left - (frame.Left - outer.Left),
		Top:    want.Top - (frame.Top - outer.Top),
		Right:  want.Right + (outer.Right - frame.Right),
		Bottom: want.Bottom + (outer.Bottom - frame.Bottom),
	}
}

// placementFrame is the visible frame move and resize work in: the DWM
// extended frame bounds when the call answered (ok) with a non-empty
// rectangle, else GetWindowRect itself (DWM off, or the call failed), so
// the placement falls back to the outer rectangle on both sides.
func placementFrame(outer, dwm Rect, ok bool) Rect {
	if !ok || dwm.Width() <= 0 || dwm.Height() <= 0 {
		return outer
	}
	return dwm
}

// placementRects reads the Window's GetWindowRect rectangle and its
// visible frame (placementFrame).
func placementRects(hwnd uint64) (outer, frame Rect, err error) {
	if ok, _, cerr := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&outer))); ok == 0 {
		return Rect{}, Rect{}, fmt.Errorf("reading the rectangle of window %d failed: %v", hwnd, cerr)
	}
	var dwm Rect
	derr := windows.DwmGetWindowAttribute(windows.HWND(hwnd), windows.DWMWA_EXTENDED_FRAME_BOUNDS,
		unsafe.Pointer(&dwm), uint32(unsafe.Sizeof(dwm)))
	return outer, placementFrame(outer, dwm, derr == nil), nil
}

func setWindowPos(hwnd uint64, x, y, cx, cy int32, flags uintptr) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	r, _, err := procSetWindowPos.Call(uintptr(hwnd), 0,
		uintptr(x), uintptr(y), uintptr(cx), uintptr(cy), flags)
	if r == 0 {
		return fmt.Errorf("SetWindowPos failed for window %d: %v", hwnd, err)
	}
	return nil
}

// PostClose posts WM_CLOSE to the Window: the same request as its close
// button. The app decides what happens (it may ask to save); nothing is
// ever terminated.
func PostClose(hwnd uint64) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if r, _, err := procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0); r == 0 {
		return fmt.Errorf("PostMessage(WM_CLOSE) failed for window %d: %v", hwnd, err)
	}
	return nil
}

// WindowFrame reads the Window's visible frame in physical pixels: the
// extended frame bounds, the rectangle the screenshot line prints and
// MoveNoActivate and ResizeNoActivate set, so a placement reads back in
// the coordinates it was given. It falls back to GetWindowRect when DWM
// does not answer, as the placement does.
func WindowFrame(hwnd uint64) (Rect, error) {
	_, frame, err := placementRects(hwnd)
	return frame, err
}

// IsMaximized reports whether hwnd is maximized (IsZoomed).
func IsMaximized(hwnd uint64) bool {
	r, _, _ := procIsZoomed.Call(uintptr(hwnd))
	return r != 0
}

// RestoresToMaximized reports whether a minimized Window was maximized
// before it was minimized (WINDOWPLACEMENT's WPF_RESTORETOMAXIMIZED): a
// non-activating restore brings it back at its normal size instead.
func RestoresToMaximized(hwnd uint64) bool {
	p := windowPlacement{length: uint32(unsafe.Sizeof(windowPlacement{}))}
	if r, _, _ := procGetWindowPlacement.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&p))); r == 0 {
		return false
	}
	return p.flags&wpfRestoreToMaximized != 0
}

// WindowOpen reports whether hwnd is still an open Window: it exists and
// is visible. A Window that only hid itself on WM_CLOSE (a tray app) has
// gone from list-windows too.
func WindowOpen(hwnd uint64) bool {
	if r, _, _ := procIsWindow.Call(uintptr(hwnd)); r == 0 {
		return false
	}
	return windows.IsWindowVisible(windows.HWND(hwnd))
}
