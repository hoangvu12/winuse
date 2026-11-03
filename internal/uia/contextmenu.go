package uia

// The element side of the ContextMenu Secondary Action (headless-max
// ticket 09, headless-100.md G6): the facts the helper picks a route from
// (the Element's UI framework and its own window), and UIA's
// IUIAutomationElement3::ShowContextMenu, which opens the menu of a
// Chromium or WinUI 3 Element without the foreground (probes rounds 4 and
// 5). On a classic control or a WPF Element it returns S_OK and opens
// nothing, so the helper reads a new menu window back after every call.

import (
	"context"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ContextMenuAction is the ContextMenu Secondary Action's canonical name.
const ContextMenuAction = "ContextMenu"

const (
	propFrameworkId = 30024 // UIA_FrameworkIdPropertyId

	el3ShowContextMenu = 91 // IUIAutomationElement3::ShowContextMenu() (SDK 10.0.26100.0)
)

// iidUIAutomationElement3 is IUIAutomationElement3, the interface that
// carries ShowContextMenu (Windows 8.1 and later).
var iidUIAutomationElement3 = windows.GUID{
	Data1: 0x8471df34, Data2: 0xaee0, Data3: 0x4a01,
	Data4: [8]byte{0xa7, 0xde, 0x7d, 0xb9, 0xaf, 0x12, 0xc2, 0x96},
}

// ErrNoShowContextMenu is an Element whose UIA client object has no
// IUIAutomationElement3. Nothing was sent.
var ErrNoShowContextMenu = errors.New("this Element's context menu has no background route: UI Automation cannot open it (no IUIAutomationElement3)")

// MenuTarget is what the context menu route is chosen from: the Element's
// UI framework (UIA FrameworkId: "Win32", "WinForm", "WPF", "XAML",
// "Chrome", "DirectUI", ...; "" when it reports none) and its own window
// (NativeWindowHandle: a classic control's HWND, 0 for an Element drawn
// inside another window).
type MenuTarget struct {
	Framework string
	Native    uint64
}

// ContextMenuTarget reads the route facts of the Element behind index of
// hwnd's latest Window State. Read-only.
func (c *Client) ContextMenuTarget(ctx context.Context, hwnd uint64, index int) (MenuTarget, error) {
	var t MenuTarget
	err := c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		t = MenuTarget{Framework: currentString(el, propFrameworkId), Native: nativeWindow(el)}
		return nil
	})
	return t, err
}

// ShowContextMenu calls IUIAutomationElement3::ShowContextMenu on the
// Element behind index. S_OK says only that the provider took the call:
// the caller reads the menu back.
func (c *Client) ShowContextMenu(ctx context.Context, hwnd uint64, index int) error {
	return c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		var el3 comPtr
		if hr := call(el, 0, uintptr(unsafe.Pointer(&iidUIAutomationElement3)), uintptr(unsafe.Pointer(&el3))); hr.Failed() || el3 == nil {
			if hrStale(hr) {
				return staleErr(hr)
			}
			return ErrNoShowContextMenu
		}
		defer release(el3)
		if hr := call(el3, el3ShowContextMenu); hr.Failed() {
			return callErr(hr, "opening the Element's context menu")
		}
		return nil
	})
}
