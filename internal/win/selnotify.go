package win

// The select route's Win32 layer (desktop-parity 12, follow-up). A
// classic Win32 ComboBox or ListBox tells its parent about a user's
// choice with WM_COMMAND(CBN_SELCHANGE or LBN_SELCHANGE), but a
// programmatic selection (CB_SETCURSEL, LB_SETCURSEL) notifies nobody. An
// app that reacts to the notification (records the choice, fills a
// dependent field) never saw set-value's pick. After the select route has
// read its selection back, NotifySelectionChange sends the parent the
// notification the user's choice would have sent.
//
// Only the SELCHANGE code is sent. A user's pick from a drop-down combo
// also sends CBN_SELENDOK (and CBN_CLOSEUP when the list was open), but
// CBN_SELCHANGE is the one apps act on, and a second message is a second
// chance for an app to act twice. WM_COMMAND carries no pointers, so it
// crosses processes as is.
//
// A classic combo is also listed, selected and read back here (ComboItems,
// SelectComboItem, ComboSelection) rather than through UIA, all measured
// on the fixture's Classic Dialog: its proxy exposes no items while the
// combo is closed; an opened combo lists every top-level window on the
// desktop among its children; the Collapse after the pick cancels it (the
// combo restores its old selection); and its Value reads the window's
// caption, not the selection. The messages read and write the combo
// without opening it.

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetParent       = user32.NewProc("GetParent")
	procGetComboBoxInfo = user32.NewProc("GetComboBoxInfo")
)

const (
	cbnSelChange        = 1      // CBN_SELCHANGE
	lbnSelChange        = 1      // LBN_SELCHANGE
	lbsNotify           = 0x0001 // LBS_NOTIFY: a ListBox that notifies its parent at all
	cbsTypeMask         = 0x0003 // CBS_SIMPLE, CBS_DROPDOWN or CBS_DROPDOWNLIST
	cbsDropDownList     = 0x0003 // CBS_DROPDOWNLIST: a drop-down with no edit field
	cbemGetComboControl = 0x0406 // CBEM_GETCOMBOCONTROL (WM_USER+6): a ComboBoxEx's inner ComboBox
	cbGetCount          = 0x0146 // CB_GETCOUNT
	cbGetCurSel         = 0x0147 // CB_GETCURSEL
	cbGetLBText         = 0x0148 // CB_GETLBTEXT
	cbGetLBTextLen      = 0x0149 // CB_GETLBTEXTLEN
	cbSetCurSel         = 0x014E // CB_SETCURSEL
	cbErr               = -1     // CB_ERR

	// comboTimeout bounds each combo message and the notification: the
	// parent runs its handler inside the call, and a hung or busy app must
	// not block the helper.
	comboTimeout = 2 * time.Second
)

// Combo reports whether the control is a classic Win32 ComboBox.
func (c Control) Combo() bool {
	return c.HWND != 0 && strings.EqualFold(c.Class, "ComboBox")
}

// DropDownList reports whether the control is a classic drop-down list
// combo (ComboBox with CBS_DROPDOWNLIST): it has no edit field, so its
// value is always one of its items. UIA's proxy still reports its Value
// writable, but that Value is the window's caption, not the selection;
// set-value picks the item by name instead.
func (c Control) DropDownList() bool {
	return c.Combo() && c.Style&cbsTypeMask == cbsDropDownList
}

// WithinControl reports whether the window h belongs to the control
// window control: control itself, one of its child windows, or a combo's
// drop-down list (a top-level ComboLBox popup, GetComboBoxInfo). An opened
// Win32 combo's proxy lists every top-level window on the desktop among
// its children; the select route's UIA search keeps to the windows this
// admits.
func WithinControl(control, h uint64) bool {
	if h == control {
		return true
	}
	if r, _, _ := procIsChild.Call(uintptr(control), uintptr(h)); r != 0 {
		return true
	}
	return h != 0 && h == comboList(control)
}

// comboBoxInfo is COMBOBOXINFO.
type comboBoxInfo struct {
	size        uint32
	item        Rect
	button      Rect
	buttonState uint32
	combo       windows.HWND
	edit        windows.HWND
	list        windows.HWND
}

// comboList is a combo's drop-down list window (GetComboBoxInfo, which
// reads across processes), or 0 for any other window.
func comboList(combo uint64) uint64 {
	info := comboBoxInfo{size: uint32(unsafe.Sizeof(comboBoxInfo{}))}
	if r, _, _ := procGetComboBoxInfo.Call(uintptr(combo), uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0
	}
	return uint64(info.list)
}

// ComboItems reads a classic combo's item texts in index order, at most
// max of them: CB_GETCOUNT, then CB_GETLBTEXTLEN and CB_GETLBTEXT per
// item. All three are below WM_USER, so Windows copies CB_GETLBTEXT's text
// back across processes. An owner-drawn combo without CBS_HASSTRINGS has
// no texts to read and fails.
func ComboItems(combo uint64, max int) ([]string, error) {
	h := windows.HWND(combo)
	n, err := sendMessageTimeout(h, cbGetCount, 0, 0, comboTimeout)
	if err != nil {
		return nil, fmt.Errorf("reading the items of combo %d failed: %v", combo, err)
	}
	if int32(n) == cbErr {
		return nil, fmt.Errorf("combo %d cannot count its items", combo)
	}
	names := make([]string, min(int(int32(n)), max))
	for i := range names {
		size, err := sendMessageTimeout(h, cbGetLBTextLen, uintptr(i), 0, comboTimeout)
		if err != nil || int32(size) == cbErr {
			return nil, fmt.Errorf("reading item %d of combo %d failed: %v", i, combo, err)
		}
		buf := make([]uint16, int(int32(size))+1)
		got, err := sendMessageTimeout(h, cbGetLBText, uintptr(i), uintptr(unsafe.Pointer(&buf[0])), comboTimeout)
		if err != nil || int32(got) == cbErr {
			return nil, fmt.Errorf("reading item %d of combo %d failed: %v", i, combo, err)
		}
		names[i] = windows.UTF16ToString(buf)
	}
	return names, nil
}

// ComboSelection reads a classic combo's selected item index
// (CB_GETCURSEL), -1 when nothing is selected.
func ComboSelection(combo uint64) (int, error) {
	n, err := sendMessageTimeout(windows.HWND(combo), cbGetCurSel, 0, 0, comboTimeout)
	if err != nil {
		return -1, err
	}
	return int(int32(n)), nil
}

// SelectComboItem selects the item at index in a classic combo
// (CB_SETCURSEL) without opening it. It notifies nobody; the select route
// then calls NotifySelectionChange. A timed-out message may still be
// processed afterwards, so a failure here may have taken effect.
func SelectComboItem(combo uint64, index int) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	r, err := sendMessageTimeout(windows.HWND(combo), cbSetCurSel, uintptr(index), 0, comboTimeout)
	if err != nil {
		return err
	}
	if int32(r) != int32(index) {
		return fmt.Errorf("combo %d selects item %d, not %d", combo, int32(r), index)
	}
	return nil
}

// selectionNotice is the WM_COMMAND wParam a user's choice in c sends its
// parent, MAKEWPARAM(control ID, CBN_SELCHANGE or LBN_SELCHANGE): a
// classic ComboBox, or a ListBox with LBS_NOTIFY. Any other window (a
// combo's drop-down ComboLBox, a list view, an Element drawn inside
// another window: HWND 0) gets none.
func selectionNotice(c Control) (uintptr, bool) {
	var code uintptr
	switch {
	case c.Combo():
		code = cbnSelChange
	case c.HWND != 0 && strings.EqualFold(c.Class, "ListBox") && c.Style&lbsNotify != 0:
		code = lbnSelChange
	default:
		return 0, false
	}
	return uintptr(c.ID) | code<<16, true
}

// NotifySelectionChange tells the parent of the classic combo or list
// whose window is hwnd (an Element's NativeWindowHandle) that its
// selection changed: SendMessageTimeout(GetParent, WM_COMMAND,
// MAKEWPARAM(id, CBN_SELCHANGE or LBN_SELCHANGE), hwnd). A ComboBoxEx32
// notifies through its inner ComboBox, whose parent (the ComboBoxEx)
// forwards it. Any other window is left alone and nil returned. A
// timed-out message may still be processed afterwards.
func NotifySelectionChange(hwnd uint64) error {
	c := ReadControl(hwnd)
	if strings.EqualFold(c.Class, "ComboBoxEx32") {
		inner, err := sendMessageTimeout(windows.HWND(hwnd), cbemGetComboControl, 0, 0, comboTimeout)
		if err != nil {
			return fmt.Errorf("finding the combo inside ComboBoxEx %d failed: %v", hwnd, err)
		}
		c = ReadControl(uint64(inner))
	}
	wparam, ok := selectionNotice(c)
	if !ok {
		return nil
	}
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	parent, _, _ := procGetParent.Call(uintptr(c.HWND))
	if parent == 0 {
		return fmt.Errorf("%s %d has no parent window to notify", c.Class, c.HWND)
	}
	if _, err := sendMessageTimeout(windows.HWND(parent), wmCommand, wparam, uintptr(c.HWND), comboTimeout); err != nil {
		return fmt.Errorf("notifying the parent of %s %d failed: %v", c.Class, c.HWND, err)
	}
	return nil
}
