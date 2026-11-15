package fixture

// The huge-list Window (learn-from 08), opened only with -huge-list N: a
// top-level Window holding a classic list box of N items and, after it, a
// button. A tree read must cut the list with a note and still read the
// button; the latency A/B (`winusetasks latency`) times reads of it. It is
// the system's own LISTBOX, read through UI Automation's list box proxy,
// so its items are the real thing rather than the fixture's own provider.

import (
	"strconv"
	"unsafe"
)

// The huge-list Window's contract.
const (
	// HugeListTitle is the huge-list Window's title.
	HugeListTitle = "winuse fixture huge list"
	// HugeListClass is its window class.
	HugeListClass = "WinuseFixtureHugeList"
	// HugeListButton is the name of the button after the list: a read that
	// shows it went on past the list.
	HugeListButton = "After the list"
	// HugeListItemPrefix starts every item's text: "Item 1" to "Item N".
	HugeListItemPrefix = "Item "
)

// List box messages and styles (WinUser.h).
const (
	lbAddString         = 0x0180
	lbInitStorage       = 0x01A8
	lbsNoIntegralHeight = 0x0100
	wmSetRedraw         = 0x000B
	swShowNoActivate    = 4
)

// openHugeList creates the huge-list Window with n items, shown without
// activation at x, y. Its class answers with DefWindowProc alone: closing
// it destroys it and leaves the fixture running.
func (a *app) openHugeList(n int, x, y uintptr) error {
	cn := utf16Ptr(HugeListClass)
	wc := wndclassex{
		Size:       uint32(unsafe.Sizeof(wndclassex{})),
		WndProc:    procDefWindowProcW.Addr(),
		Instance:   a.instance,
		ClassName:  cn,
		Background: 16, // COLOR_BTNFACE + 1
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	const wsFixed = wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox
	w, h := windowSizeFor(wsFixed, 320, 400)
	title := utf16Ptr(HugeListTitle)
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cn)), uintptr(unsafe.Pointer(title)),
		uintptr(wsFixed), x, y, w, h, 0, 0, a.instance, 0)
	if hwnd == 0 {
		return err
	}
	list, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("LISTBOX"))), 0,
		uintptr(wsChild|wsVisible|wsBorder|wsVScroll|lbsNoIntegralHeight), 10, 10, 300, 340, hwnd, 1, a.instance, 0)
	if list == 0 {
		return err
	}
	procSendMessageW.Call(list, wmSetRedraw, 0, 0)
	procSendMessageW.Call(list, lbInitStorage, uintptr(n), uintptr(n*16))
	for i := 1; i <= n; i++ {
		procSendMessageW.Call(list, lbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr(HugeListItemPrefix+strconv.Itoa(i)))))
	}
	procSendMessageW.Call(list, wmSetRedraw, 1, 0)
	button, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("BUTTON"))), uintptr(unsafe.Pointer(utf16Ptr(HugeListButton))),
		uintptr(wsChild|wsVisible|wsTabStop|bsPushButton), 10, 356, 300, 32, hwnd, 2, a.instance, 0)
	if button == 0 {
		return err
	}
	procShowWindow.Call(hwnd, swShowNoActivate)
	a.state.HugeListWindowID = uint64(hwnd)
	return nil
}
