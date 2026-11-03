package win

// The UIA worker pool's probes (multi-agent ticket 07, ADR 0009 as amended
// by ADR 0014): the App a Window belongs to, which picks the UIA worker
// that serves it, and whether that App's UI thread is hung, which refuses
// an Action before anything is sent.

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// hungProbe bounds the WM_NULL that asks a Window's UI thread whether it
// answers. SendMessageTimeout waits the whole timeout even when Windows
// already marks the App hung (probes round 3), so it is short: a thread
// that cannot answer a WM_NULL this fast would also stall a UIA call.
const hungProbe = 300 * time.Millisecond

// WindowHung reports whether the UI thread behind hwnd is not responding:
// Windows marks it hung (IsHungAppWindow: no messages read for 5 s), or it
// does not answer a WM_NULL within hungProbe. A Window whose WM_NULL UIPI
// refuses (an administrator App) is judged by IsHungAppWindow alone, and a
// Window that is gone is not hung (its command fails on it anyway).
// Read-only; false inside an offline test binary.
func WindowHung(hwnd uint64) bool {
	if LiveDesktopGuard() != nil {
		return false
	}
	h := windows.HWND(hwnd)
	if !windows.IsWindow(h) {
		return false
	}
	if hung, _, _ := procIsHungAppWindow.Call(uintptr(h)); hung != 0 {
		return true
	}
	var result uintptr
	r, _, err := procSendMessageTimeoutW.Call(uintptr(h), wmNull, 0, 0,
		smtoAbortIfHung, uintptr(hungProbe.Milliseconds()), uintptr(unsafe.Pointer(&result)))
	if r != 0 {
		return false
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || !windows.IsWindow(h) {
		return false
	}
	return true
}

// AppPID is the process of the App behind hwnd, the UIA worker pool's
// affinity key: a UWP App's frame is owned by ApplicationFrameHost, which
// frames every UWP App, so its content's process names the App instead.
// 0 when it cannot be read.
func AppPID(hwnd uint64) uint32 {
	if pid := uwpContentPID(windows.HWND(hwnd)); pid != 0 {
		return pid
	}
	return WindowPID(hwnd)
}

// RootWindowAt is the top-level Window under the screen point (x, y), 0
// when there is none: the App an Element-at-a-point Action reaches.
func RootWindowAt(x, y int32) uint64 {
	// WindowFromPoint takes the POINT by value, x in the low half.
	h, _, _ := procWindowFromPoint.Call(uintptr(uint32(x)) | uintptr(uint32(y))<<32)
	if h == 0 {
		return 0
	}
	return uint64(rootWindow(h))
}
