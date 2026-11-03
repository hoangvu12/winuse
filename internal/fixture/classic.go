package fixture

// The classic Win32 windows (see ClassicDialogTitle and MessageBoxTitle):
// a modeless #32770 dialog built from an in-memory template, and a
// MessageBox. They exist so the e2e tests can drive winuse's keyboard
// routes for classic dialogs and edit controls (DM_GETDEFID, EM_REPLACESEL,
// posted keys) against the system's own controls, which the fixture's
// drawn Elements cannot stand in for.

import (
	"runtime"
	"unicode/utf16"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/win"
	"golang.org/x/sys/windows"
)

// Dialog template and control constants (WinUser.h).
const (
	dsSetFont    = 0x0040
	dsModalFrame = 0x0080

	wsChild   = 0x40000000
	wsVisible = 0x10000000
	wsBorder  = 0x00800000
	wsTabStop = 0x00010000

	esPassword    = 0x0020
	esAutoHScroll = 0x0080

	wsVScroll       = 0x00200000
	cbsDropDownList = 0x0003

	bsPushButton    = 0x0000
	bsDefPushButton = 0x0001

	// Predefined window class ordinals of a dialog item.
	classButton uint16 = 0x0080
	classEdit   uint16 = 0x0081
	classStatic uint16 = 0x0082
	classCombo  uint16 = 0x0085

	// The Fruits list view (comctl32): LVS_LIST without LVS_SINGLESEL, so
	// several items can be selected.
	classListView      = "SysListView32"
	lvsList            = 0x0003
	lvsShowSelAlways   = 0x0008
	lvmInsertItemW     = 0x104D
	lvifText           = 0x0001
	iccListviewClasses = 0x0001

	idOK     = 1
	idCancel = 2
	idStatic = 0xFFFF

	enSetFocus = 0x0100
	enChange   = 0x0300

	cbnSelChange = 1
	cbnDropDown  = 7
	cbAddString  = 0x0143
	cbGetCurSel  = 0x0147

	mbOKCancel = 0x0001

	mfGrayed        = 0x0001
	mfPopup         = 0x0010
	wmInitMenuPopup = 0x0117
	emSetSel        = 0x00B1

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
)

// dlgItem is one control of an in-memory dialog template, in dialog units.
type dlgItem struct {
	class        uint16 // a predefined class ordinal (classButton, classEdit, classStatic)
	id           uint16
	style        uint32 // WS_CHILD and WS_VISIBLE are added
	x, y, cx, cy int16
	text         string
	className    string // a registered class by name instead of class (classListView)
}

// dialogTemplate lays out a DLGTEMPLATE followed by its DLGITEMTEMPLATEs,
// as UTF-16 words: the header (style, extended style, item count, position
// and size, no menu, the predefined dialog class, the title, then the font
// when style has DS_SETFONT) and each item DWORD-aligned (style, extended
// style, position and size, ID, class ordinal or name, text, no creation
// data).
// A Go []uint16 allocation is at least DWORD-aligned, so offsets that are
// even in words are DWORD-aligned in memory.
func dialogTemplate(style uint32, cx, cy int16, title, font string, pointSize uint16, items []dlgItem) []uint16 {
	var b []uint16
	dword := func(v uint32) { b = append(b, uint16(v), uint16(v>>16)) }
	str := func(s string) {
		b = append(b, utf16.Encode([]rune(s))...)
		b = append(b, 0)
	}
	dword(style)
	dword(0)
	b = append(b, uint16(len(items)), 0, 0, uint16(cx), uint16(cy))
	b = append(b, 0, 0) // no menu, the predefined dialog class
	str(title)
	if style&dsSetFont != 0 {
		b = append(b, pointSize)
		str(font)
	}
	for _, it := range items {
		if len(b)%2 != 0 {
			b = append(b, 0)
		}
		dword(it.style | wsChild | wsVisible)
		dword(0)
		b = append(b, uint16(it.x), uint16(it.y), uint16(it.cx), uint16(it.cy), it.id)
		if it.className != "" {
			str(it.className)
		} else {
			b = append(b, 0xFFFF, it.class)
		}
		str(it.text)
		b = append(b, 0) // no creation data
	}
	return b
}

// classicItems are the Classic Dialog's controls, in tab order: the
// name Edit takes the focus when the dialog opens. The combo's height
// includes its drop-down list.
var classicItems = []dlgItem{
	{classStatic, idStatic, 0, 7, 9, 44, 8, "Name:", ""},
	{classEdit, ClassicEditID, wsBorder | wsTabStop | esAutoHScroll, 55, 7, 158, 14, "", ""},
	{classStatic, idStatic, 0, 7, 29, 44, 8, "Password:", ""},
	{classEdit, ClassicPasswordID, wsBorder | wsTabStop | esAutoHScroll | esPassword, 55, 27, 158, 14, "", ""},
	{classStatic, idStatic, 0, 7, 50, 44, 8, "Color:", ""},
	{classCombo, ClassicComboID, wsTabStop | wsVScroll | cbsDropDownList, 55, 48, 158, 70, "", ""},
	{classButton, idOK, wsTabStop | bsDefPushButton, 109, 79, 50, 14, "OK", ""},
	{classButton, idCancel, wsTabStop | bsPushButton, 163, 79, 50, 14, "Cancel", ""},
	// Last, so the controls above keep their Element Indexes (ClassicListID).
	{classStatic, idStatic, 0, 7, 100, 44, 8, "Fruits:", ""},
	{0, ClassicListID, wsBorder | wsTabStop | lvsList | lvsShowSelAlways, 55, 100, 158, 52, "", classListView},
}

// lvItem is LVITEMW (CommCtrl.h): the Fruits list view's items carry only
// their text.
type lvItem struct {
	mask      uint32
	item      int32
	subItem   int32
	state     uint32
	stateMask uint32
	text      *uint16
	textMax   int32
	image     int32
	param     uintptr
	indent    int32
	groupID   int32
	columns   uint32
	puColumns uintptr
	piColFmt  uintptr
	group     int32
}

// classicTemplate is the Classic Dialog's template.
func classicTemplate() []uint16 {
	return dialogTemplate(wsPopup|wsCaption|wsSysMenu|dsModalFrame|dsSetFont, 220, 158,
		ClassicDialogTitle, "Segoe UI", 9, classicItems)
}

// commandResult names the button that closed a dialog or MessageBox, as
// the state file records it.
func commandResult(id uintptr) string {
	switch id {
	case idOK:
		return "ok"
	case idCancel:
		return "cancel"
	case 0:
		return "failed"
	}
	return "other"
}

// classicFocusName names a Classic Dialog Edit for the state file's
// classicFocus ("" for any other control).
func classicFocusName(id uint16) string {
	switch id {
	case ClassicEditID:
		return "name"
	case ClassicPasswordID:
		return "password"
	}
	return ""
}

// openClassic opens the Classic Dialog (posted by the Classic Dialog...
// button). It sits inside the main window's frame, so a Window that covers
// the main window covers it too, and the name Edit takes the focus.
func (a *app) openClassic() {
	if a.classicHwnd != 0 {
		return
	}
	a.state.ClassicResult = ""
	a.state.ClassicText = ""
	a.state.ClassicPassword = ""
	a.state.ClassicFocus = ""
	a.state.ClassicColor = ""
	a.state.ClassicComboDrops = 0
	a.state.ClassicMenu = ""
	a.state.ClassicMenuInits = 0
	// The Fruits list view's class is comctl32's, registered here.
	icc := struct{ size, icc uint32 }{8, iccListviewClasses}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	tmpl := classicTemplate()
	h, _, _ := procCreateDialogIndirectParamW.Call(a.instance, uintptr(unsafe.Pointer(&tmpl[0])), a.mainHwnd, a.classicProc, 0)
	runtime.KeepAlive(tmpl)
	if h == 0 {
		a.setStatus("classic dialog failed")
		a.changed()
		return
	}
	a.classicHwnd = h
	a.state.ClassicOpen = true
	a.state.ClassicWindowID = uint64(h)

	var r win.Rect
	procGetWindowRect.Call(a.mainHwnd, uintptr(unsafe.Pointer(&r)))
	procSetWindowPos.Call(h, 0, uintptr(uint32(r.Left+48)), uintptr(uint32(r.Top+96)), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	procShowWindow.Call(h, swShow)
	if edit, _, _ := procGetDlgItem.Call(h, ClassicEditID); edit != 0 {
		procSetFocus.Call(edit)
	}
	a.setStatus("classic dialog opened")
	a.changed()
}

// dlgClassic is the Classic Dialog's dialog procedure. OK and Cancel
// (including Return, Escape and the close box, which the dialog manager
// turns into IDOK and IDCANCEL) destroy it; the Edits report their text
// and focus to the state file.
func (a *app) dlgClassic(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case wmInitDialog:
		if combo, _, _ := procGetDlgItem.Call(hwnd, ClassicComboID); combo != 0 {
			for _, c := range ClassicColors {
				p := utf16Ptr(c)
				procSendMessageW.Call(combo, cbAddString, 0, uintptr(unsafe.Pointer(p)))
				runtime.KeepAlive(p)
			}
		}
		if list, _, _ := procGetDlgItem.Call(hwnd, ClassicListID); list != 0 {
			for i, f := range ClassicFruits {
				p := utf16Ptr(f)
				item := lvItem{mask: lvifText, item: int32(i), text: p}
				procSendMessageW.Call(list, lvmInsertItemW, 0, uintptr(unsafe.Pointer(&item)))
				runtime.KeepAlive(p)
			}
		}
		a.addClassicMenu(hwnd)
		return 1 // the dialog manager focuses the first tab stop, the name Edit
	case wmInitMenuPopup:
		a.state.ClassicMenuInits++
		if wp == a.classicEdit {
			ctl, _, _ := procGetDlgItem.Call(hwnd, ClassicEditID)
			state := uintptr(mfGrayed)
			if windowText(ctl) != "" {
				state = 0 // MF_ENABLED
			}
			procEnableMenuItem.Call(a.classicEdit, ClassicMenuClearID, state)
		}
		a.changed()
		return 0
	case wmCommand:
		id, code := uint16(wp), uint16(wp>>16)
		switch {
		case id == idOK || id == idCancel:
			a.closeClassic(hwnd, commandResult(uintptr(id)))
			return 1
		case code == 0 && classicMenuName(id) != "":
			a.classicMenuCommand(hwnd, id)
			return 1
		case id == ClassicComboID && code == cbnSelChange:
			ctl, _, _ := procGetDlgItem.Call(hwnd, ClassicComboID)
			a.state.ClassicColor = classicColor(int32(sendMessage(ctl, cbGetCurSel, 0, 0)))
			a.changed()
			return 1
		case id == ClassicComboID && code == cbnDropDown:
			a.state.ClassicComboDrops++
			a.changed()
			return 1
		case code == enChange && classicFocusName(id) != "":
			ctl, _, _ := procGetDlgItem.Call(hwnd, uintptr(id))
			if id == ClassicEditID {
				a.state.ClassicText = windowText(ctl)
			} else {
				a.state.ClassicPassword = windowText(ctl)
			}
			a.changed()
			return 1
		case code == enSetFocus && classicFocusName(id) != "":
			a.state.ClassicFocus = classicFocusName(id)
			a.changed()
			return 1
		}
	}
	return 0
}

// addClassicMenu gives the Classic Dialog its menu bar (see
// ClassicMenuFillID) and grows the window by the bar's height, so the
// controls keep their client area.
func (a *app) addClassicMenu(hwnd uintptr) {
	bar, _, _ := procCreateMenu.Call()
	file, _, _ := procCreatePopupMenu.Call()
	edit, _, _ := procCreatePopupMenu.Call()
	if bar == 0 || file == 0 || edit == 0 {
		return
	}
	appendMenu(file, 0, ClassicMenuFillID, "&Fill Name")
	appendMenu(edit, 0, ClassicMenuSelectAllID, "Select &All\tCtrl+A")
	appendMenu(edit, mfGrayed, ClassicMenuClearID, "&Clear Name")
	appendMenu(edit, mfGrayed, ClassicMenuLockedID, "&Locked")
	appendMenu(bar, mfPopup, file, "&File")
	appendMenu(bar, mfPopup, edit, "&Edit")

	var before, after win.Rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&before)))
	procSetMenu.Call(hwnd, bar)
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&after)))
	a.classicEdit = edit
	if grow := before.Height() - after.Height(); grow > 0 {
		var r win.Rect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(r.Width()), uintptr(r.Height()+grow), swpNoMove|swpNoZOrder|swpNoActivate)
	}
}

// appendMenu appends one item to a menu: a command (id) or, with
// MF_POPUP, a submenu (id is its HMENU).
func appendMenu(menu uintptr, flags uint32, id uintptr, text string) {
	p := utf16Ptr(text)
	procAppendMenuW.Call(menu, uintptr(flags), id, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}

// classicMenuName names a Classic Dialog menu command for the state
// file's classicMenu ("" for any other ID).
func classicMenuName(id uint16) string {
	switch id {
	case ClassicMenuFillID:
		return "fill-name"
	case ClassicMenuSelectAllID:
		return "select-all"
	case ClassicMenuClearID:
		return "clear-name"
	case ClassicMenuLockedID:
		return "locked"
	}
	return ""
}

// classicMenuCommand runs a Classic Dialog menu command on the name Edit
// and records it.
func (a *app) classicMenuCommand(hwnd uintptr, id uint16) {
	ctl, _, _ := procGetDlgItem.Call(hwnd, ClassicEditID)
	switch id {
	case ClassicMenuFillID:
		p := utf16Ptr(ClassicFillText)
		procSetWindowTextW.Call(ctl, uintptr(unsafe.Pointer(p)))
		runtime.KeepAlive(p)
	case ClassicMenuSelectAllID:
		sendMessage(ctl, emSetSel, 0, ^uintptr(0))
	case ClassicMenuClearID:
		p := utf16Ptr("")
		procSetWindowTextW.Call(ctl, uintptr(unsafe.Pointer(p)))
		runtime.KeepAlive(p)
	}
	a.state.ClassicMenu = classicMenuName(id)
	a.setStatus("classic menu: " + a.state.ClassicMenu)
	a.changed()
}

// classicColor names the combo item at index sel for the state file's
// classicColor ("" for CB_ERR or an index out of range).
func classicColor(sel int32) string {
	if sel < 0 || int(sel) >= len(ClassicColors) {
		return ""
	}
	return ClassicColors[sel]
}

// sendMessage sends a message to a window of the fixture's own thread.
func sendMessage(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

// closeClassic destroys the Classic Dialog and records how it closed.
func (a *app) closeClassic(hwnd uintptr, result string) {
	procDestroyWindow.Call(hwnd)
	a.classicHwnd = 0
	a.classicEdit = 0
	a.state.ClassicOpen = false
	a.state.ClassicResult = result
	a.setStatus("classic dialog closed: " + result)
	a.changed()
}

// openMessageBox shows the MessageBox (posted by the Message Box...
// button) and records the button that closed it. MessageBoxW runs its own
// modal loop, which keeps serving the fixture's windows and its UI
// Automation provider meanwhile.
func (a *app) openMessageBox() {
	if a.state.MessageBoxOpen {
		return
	}
	a.state.MessageBoxOpen = true
	a.state.MessageBoxResult = ""
	a.setStatus("message box opened")
	a.changed()
	text, title := utf16Ptr(MessageBoxText), utf16Ptr(MessageBoxTitle)
	r, _, _ := procMessageBoxW.Call(a.mainHwnd, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbOKCancel)
	a.state.MessageBoxOpen = false
	a.state.MessageBoxResult = commandResult(r)
	a.setStatus("message box closed: " + a.state.MessageBoxResult)
	a.changed()
}

// windowText reads a window's text from inside its own process (so a
// password Edit answers too).
func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}
