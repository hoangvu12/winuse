package win

// The classic menu bar's Win32 layer (headless-max ticket 08,
// headless-100.md G7): invoke-menu runs a command of a Window's HMENU menu
// bar without opening the menu and without the foreground. The reads
// (GetMenu, GetMenuItemCount, GetMenuStringW, GetMenuState, GetMenuItemID,
// GetSubMenu, GetMenuItemRect) work across processes on the menu object
// itself, so the menu never opens. Before a submenu is read, InitMenuPopup
// sends it WM_INITMENUPOPUP, as Windows does before it shows the menu, so
// an App that greys or fills its items lazily (MFC's UPDATE_COMMAND_UI,
// recent-file lists) reports them current. The command itself is a posted
// WM_COMMAND(MAKEWPARAM(id, 0)), the message a chosen menu item sends.
//
// The sending calls refuse inside a go test binary (LiveDesktopGuard):
// offline tests fake them through the helper's Services.

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetMenu          = user32.NewProc("GetMenu")
	procIsMenu           = user32.NewProc("IsMenu")
	procGetMenuItemCount = user32.NewProc("GetMenuItemCount")
	procGetMenuStringW   = user32.NewProc("GetMenuStringW")
	procGetMenuState     = user32.NewProc("GetMenuState")
	procGetMenuItemID    = user32.NewProc("GetMenuItemID")
	procGetSubMenu       = user32.NewProc("GetSubMenu")
	procGetMenuItemRect  = user32.NewProc("GetMenuItemRect")
)

// Menu item flags as GetMenuState reports them (MF_*). For a submenu the
// high byte holds its item count, so only the low bits are flags.
const (
	MenuGrayed    = 0x0001 // MF_GRAYED
	MenuDisabled  = 0x0002 // MF_DISABLED
	MenuBitmap    = 0x0004 // MF_BITMAP
	MenuPopup     = 0x0010 // MF_POPUP
	MenuOwnerDraw = 0x0100 // MF_OWNERDRAW
	MenuSeparator = 0x0800 // MF_SEPARATOR

	mfByPosition    = 0x0400 // MF_BYPOSITION
	wmInitMenuPopup = 0x0117 // WM_INITMENUPOPUP

	// menuInitTimeout bounds WM_INITMENUPOPUP: a hung or slow App leaves
	// the submenu read as it is instead of stalling the helper.
	menuInitTimeout = 500 * time.Millisecond

	// maxMenuItems caps the items read from one menu: a real menu has a few
	// dozen, and the cap bounds a corrupt count.
	maxMenuItems = 512
)

// MenuItem is one item of an HMENU, read by position.
type MenuItem struct {
	Pos     int    // the item's position in its menu, from 0
	Text    string // GetMenuStringW: the caption with its & markers and its tab-separated shortcut; "" for a separator, a bitmap or an owner-drawn item
	ID      uint32 // GetMenuItemID: the command ID; meaningless for a submenu
	Flags   uint32 // GetMenuState's flag bits (MenuGrayed, MenuPopup, ...)
	Submenu uint64 // GetSubMenu: the submenu's HMENU, 0 for a command
}

// Separator reports whether the item is a separator line.
func (m MenuItem) Separator() bool { return m.Flags&MenuSeparator != 0 }

// Popup reports whether the item opens a submenu.
func (m MenuItem) Popup() bool { return m.Submenu != 0 || m.Flags&MenuPopup != 0 }

// Unavailable reports whether the item is greyed or disabled: choosing it
// does nothing in a real menu.
func (m MenuItem) Unavailable() bool { return m.Flags&(MenuGrayed|MenuDisabled) != 0 }

// MenuBar reads a Window's menu bar (GetMenu): its HMENU, or 0 when the
// Window has no classic menu bar (WinUI, WPF, WinForms MenuStrip, an
// Office ribbon and Chromium draw their own).
func MenuBar(hwnd uint64) uint64 {
	r, _, _ := procGetMenu.Call(uintptr(hwnd))
	if r == 0 {
		return 0
	}
	if ok, _, _ := procIsMenu.Call(r); ok == 0 {
		return 0
	}
	return uint64(r)
}

// ReadMenu reads the items of one HMENU by position. Reading sends nothing
// to the App and opens nothing.
func ReadMenu(menu uint64) ([]MenuItem, error) {
	h := uintptr(menu)
	n, _, _ := procGetMenuItemCount.Call(h)
	count := int(int32(n))
	if count < 0 {
		return nil, fmt.Errorf("menu %#x cannot be read: it may have closed", menu)
	}
	count = min(count, maxMenuItems)
	items := make([]MenuItem, 0, count)
	for pos := 0; pos < count; pos++ {
		state, _, _ := procGetMenuState.Call(h, uintptr(pos), mfByPosition)
		if int32(state) == -1 {
			continue // the item went away while the menu was read
		}
		it := MenuItem{Pos: pos, Flags: menuFlags(uint32(state))}
		if sub, _, _ := procGetSubMenu.Call(h, uintptr(pos)); sub != 0 {
			it.Submenu = uint64(sub)
		} else {
			id, _, _ := procGetMenuItemID.Call(h, uintptr(pos))
			it.ID = uint32(id)
		}
		if !it.Separator() {
			it.Text = menuString(h, pos)
		}
		items = append(items, it)
	}
	return items, nil
}

// menuFlags decodes GetMenuState's reply: for a submenu (MF_POPUP) the
// high byte of the low word is its item count, not flags.
func menuFlags(state uint32) uint32 {
	if state&MenuPopup != 0 {
		return state & 0xFF
	}
	return state & 0xFFFF
}

// menuString reads one item's caption by position (GetMenuStringW); "" for
// an item with no text.
func menuString(menu uintptr, pos int) string {
	n, _, _ := procGetMenuStringW.Call(menu, uintptr(pos), 0, 0, mfByPosition)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetMenuStringW.Call(menu, uintptr(pos), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), mfByPosition)
	return strings.TrimRight(windows.UTF16ToString(buf), "\x00")
}

// InitMenuPopup sends WM_INITMENUPOPUP(submenu, MAKELPARAM(pos, FALSE)) to
// the Window that owns the menu bar: what Windows sends just before it
// shows the submenu at that position, so the App can grey, check or fill
// its items. It runs no command and is safe to repeat. SendMessageTimeout
// with SMTO_ABORTIFHUNG bounds a hung App; the caller reads the submenu
// as it is when the send fails.
func InitMenuPopup(hwnd, submenu uint64, pos int) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if _, err := sendMessageTimeout(windows.HWND(hwnd), wmInitMenuPopup, uintptr(submenu), uintptr(uint16(pos)), menuInitTimeout); err != nil {
		return fmt.Errorf("WM_INITMENUPOPUP to window %d failed: %v", hwnd, err)
	}
	return nil
}

// MenuBarItemRect reads the screen rectangle of the menu bar item at pos
// (GetMenuItemRect), in physical pixels: where the Agent Cursor goes. ok
// is false when Windows does not report one.
func MenuBarItemRect(hwnd uint64, pos int) (Rect, bool) {
	menu, _, _ := procGetMenu.Call(uintptr(hwnd))
	if menu == 0 {
		return Rect{}, false
	}
	var r Rect
	if ok, _, _ := procGetMenuItemRect.Call(uintptr(hwnd), menu, uintptr(pos), uintptr(unsafe.Pointer(&r))); ok == 0 {
		return Rect{}, false
	}
	return r, r.Width() > 0 && r.Height() > 0
}

// PostMenuCommand posts WM_COMMAND(MAKEWPARAM(id, 0), 0) to the Window:
// the message a chosen menu item sends its owner (a high word of 0 marks a
// menu). Posting never waits on the App.
func PostMenuCommand(hwnd uint64, id uint16) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if r, _, err := procPostMessageW.Call(uintptr(hwnd), wmCommand, uintptr(id), 0); r == 0 {
		return fmt.Errorf("PostMessage(WM_COMMAND) failed for window %d: %v", hwnd, err)
	}
	return nil
}
