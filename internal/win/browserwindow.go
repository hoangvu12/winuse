package win

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

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

// WindowTitle reads a Window's caption now, without sending it a message
// (windowTitle): the Tab match re-reads it while a page changes its title
// (browser-ux 05). "" when the Window is gone or untitled.
func WindowTitle(hwnd uint64, pid uint32) string {
	return windowTitle(windows.HWND(hwnd), pid)
}

// chromiumContentClass is the class of the child window Chromium keeps
// over a browser Window's web content (its legacy render widget window,
// "Chrome Legacy Window"): one per shown Tab view, sized to the content
// area, present while the Window is covered.
const chromiumContentClass = "Chrome_RenderWidgetHostHWND"

// contentChild is one candidate child window for ChromiumContentRect.
type contentChild struct {
	class   string
	visible bool
	rect    Rect
}

// ChromiumContentRect is the screen rectangle (physical pixels) of a
// Chromium Window's web content area: the one visible
// Chrome_RenderWidgetHostHWND child inside the Window. It is the Agent
// Cursor's page-to-screen origin (browser-ux 04), read without a UI
// Automation round trip and while the Window is covered, when Chromium's
// accessibility tree is thin. false when there is none, or several (a
// docked DevTools, a side panel): the caller then asks UI Automation.
func ChromiumContentRect(top uint64) (Rect, bool) {
	outer, ok := WindowRect(top)
	if !ok {
		return Rect{}, false
	}
	var kids []contentChild
	for _, h := range descendants(windows.HWND(top)) {
		c := contentChild{class: className(windows.HWND(h)), visible: windows.IsWindowVisible(windows.HWND(h))}
		if c.class != chromiumContentClass {
			continue
		}
		c.rect, _ = WindowRect(uint64(h))
		kids = append(kids, c)
	}
	return pickContentRect(outer, kids)
}

// pickContentRect is ChromiumContentRect's choice: exactly one visible
// content child with an area, lying inside the Window's rectangle.
func pickContentRect(outer Rect, kids []contentChild) (Rect, bool) {
	var found []Rect
	for _, k := range kids {
		r := k.rect
		if k.class != chromiumContentClass || !k.visible || r.Width() <= 0 || r.Height() <= 0 {
			continue
		}
		if r.Left < outer.Left || r.Top < outer.Top || r.Right > outer.Right || r.Bottom > outer.Bottom {
			continue
		}
		found = append(found, r)
	}
	if len(found) != 1 {
		return Rect{}, false
	}
	return found[0], true
}

// WindowDPI is GetDpiForWindow: 96 at 100% display scale, 144 at 150%.
// 96 when the API is missing or the Window is gone.
func WindowDPI(hwnd uint64) uint32 {
	return uint32(dpiForWindow(uintptr(hwnd)))
}
