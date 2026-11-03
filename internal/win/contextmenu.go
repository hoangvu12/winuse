package win

// The context menu's Win32 layer (headless-max ticket 09, headless-100.md
// G6; probes rounds 4 and 5, section 2). A classic control's menu opens
// from a posted WM_CONTEXTMENU(control, -1), the keyboard form, without
// the foreground; it is never sent, because TrackPopupMenu is modal and a
// send would block the helper. The open menu is a `#32768` popup: its
// HMENU is read through MN_GETHMENU, and an item is chosen by posting its
// mnemonic key to the popup, or WM_COMMAND to the menu's owner (a
// TPM_RETURNCMD menu, like the Edit's, ignores the WM_COMMAND, so the
// mnemonic comes first). WM_CANCELMODE to the owner closes it. A WinUI 3
// menu (PopupHost) closes on Escape posted to its island's input window,
// not to the PopupHost itself.
//
// The sending calls refuse inside a go test binary (LiveDesktopGuard):
// offline tests fake them through the helper's Services.

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The menu windows the context menu route opens, picks from and closes.
const (
	MenuClass      = "#32768"                                     // a classic popup menu
	PopupHostClass = "Microsoft.UI.Content.PopupWindowSiteBridge" // a WinUI 3 flyout's top-level ("PopupHost")
)

const (
	wmContextMenu = 0x007B // WM_CONTEXTMENU
	mnGetHMenu    = 0x01E1 // MN_GETHMENU

	// menuSendTimeout bounds MN_GETHMENU and WM_CANCELMODE: a hung App
	// fails the read instead of stalling the helper.
	menuSendTimeout = 500 * time.Millisecond
)

// popupInputClasses are the input windows inside a WinUI 3 PopupHost that
// take a posted Escape (round 5: the PopupHost itself ignores it).
var popupInputClasses = map[string]bool{
	"InputSiteWindowClass":                   true,
	"Windows.UI.Input.InputSite.WindowClass": true,
}

// PostContextMenu posts WM_CONTEXTMENU(control, -1) to a classic control:
// the message the Menu key sends, which opens the control's context menu
// at its caret without the foreground (round 4). lParam -1 marks the
// keyboard form, so no screen point is read.
func PostContextMenu(control uint64) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if r, _, err := procPostMessageW.Call(uintptr(control), wmContextMenu, uintptr(control), ^uintptr(0)); r == 0 {
		return fmt.Errorf("PostMessage(WM_CONTEXTMENU) failed for window %d: %v", control, err)
	}
	return nil
}

// PopupMenu reads the HMENU an open `#32768` popup shows (MN_GETHMENU),
// readable across processes with ReadMenu; 0 when it cannot be read.
func PopupMenu(menu uint64) uint64 {
	if err := LiveDesktopGuard(); err != nil {
		return 0
	}
	r, err := sendMessageTimeout(windows.HWND(menu), mnGetHMenu, 0, 0, menuSendTimeout)
	if err != nil || r == 0 {
		return 0
	}
	if ok, _, _ := procIsMenu.Call(r); ok == 0 {
		return 0
	}
	return uint64(r)
}

// MenuOwner reads the Window that owns the menu its thread is tracking
// (GetGUIThreadInfo hwndMenuOwner): the window a chosen item's WM_COMMAND
// goes to, and the one WM_CANCELMODE closes the menu through. 0 when the
// thread is in no menu mode.
func MenuOwner(menu uint64) uint64 {
	tid, err := windows.GetWindowThreadProcessId(windows.HWND(menu), nil)
	if err != nil || tid == 0 {
		return 0
	}
	info := windows.GUIThreadInfo{Size: uint32(unsafe.Sizeof(windows.GUIThreadInfo{}))}
	if err := windows.GetGUIThreadInfo(tid, &info); err != nil {
		return 0
	}
	return uint64(info.MenuOwner)
}

// PostMenuKey posts one key (WM_KEYDOWN, then WM_KEYUP) to an open
// `#32768` popup: a mnemonic letter or digit chooses its item there, as
// round 4 measured. It returns how many messages were posted.
func PostMenuKey(menu uint64, key rune) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	vk, ok := mnemonicVK(key)
	if !ok {
		return 0, fmt.Errorf("%q has no virtual key to post", key)
	}
	scan := mapVirtualKeyScan(vk)
	return postAll(menu, []postedMessage{
		{wmKeyDown, uintptr(vk), keyLParam(scan, false, false)},
		{wmKeyUp, uintptr(vk), keyLParam(scan, false, true)},
	})
}

// mnemonicVK is the virtual key of a mnemonic: a letter's upper case or a
// digit (their VK codes are their ASCII codes).
func mnemonicVK(key rune) (uint16, bool) {
	switch {
	case key >= 'a' && key <= 'z':
		return uint16(key - 'a' + 'A'), true
	case key >= 'A' && key <= 'Z', key >= '0' && key <= '9':
		return uint16(key), true
	}
	return 0, false
}

// CancelMenu sends WM_CANCELMODE to a menu's owner, which ends its menu
// mode and closes the menu (round 4, on a classic Edit and a Chrome
// top-level). SendMessageTimeout bounds a hung App.
func CancelMenu(owner uint64) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if _, err := sendMessageTimeout(windows.HWND(owner), wmCancelMode, 0, 0, menuSendTimeout); err != nil {
		return fmt.Errorf("WM_CANCELMODE to window %d failed: %v", owner, err)
	}
	return nil
}

// PostMenuEscape posts Escape (WM_KEYDOWN, then WM_KEYUP) where an open
// menu takes it: a WinUI 3 PopupHost's island input windows (round 5),
// any other menu window itself (a `#32768`, a Chromium menu popup). It
// returns how many messages were posted: 0 means nothing was sent. The
// caller reads the close back.
func PostMenuEscape(menu uint64) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	h := windows.HWND(menu)
	if className(h) != PopupHostClass {
		return postAll(menu, overflowEscapeMessages())
	}
	sent := 0
	for _, child := range descendants(h) {
		if !popupInputClasses[className(windows.HWND(child))] {
			continue
		}
		n, err := postAll(uint64(child), overflowEscapeMessages())
		sent += n
		if err != nil {
			return sent, err
		}
	}
	if sent == 0 {
		return 0, fmt.Errorf("window %d has no island input window to post Escape to", menu)
	}
	return sent, nil
}
