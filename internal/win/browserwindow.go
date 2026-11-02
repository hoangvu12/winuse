package win

import "unsafe"

// The read-only Window geometry a browser Window is matched to its Tab
// with (browser merge ticket 04): the ticket 03 probe matched
// Browser.getWindowForTarget's DIP bounds, scaled by the Window's DPI,
// against GetWindowRect (not the DWM frame bounds).

// WindowRect is GetWindowRect in physical pixels (the helper is
// per-monitor-v2 DPI aware). ok is false when the Window is gone.
func WindowRect(hwnd uint64) (r Rect, ok bool) {
	r1, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r, r1 != 0
}

// WindowDPI is GetDpiForWindow: 96 at 100% display scale, 144 at 150%.
// 96 when the API is missing or the Window is gone.
func WindowDPI(hwnd uint64) uint32 {
	return uint32(dpiForWindow(uintptr(hwnd)))
}
