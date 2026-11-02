package win

// The keyboard routes' Win32 layer (ADR 0011, Decision 4): press-key and
// type-text route by the live Window, not by their arguments alone. The
// probe (ProbeKeyTarget) reads what the routing decides on: whether the
// Window is a classic dialog, its default and Cancel buttons, and the
// control that holds the keyboard focus on the Window's thread. The
// Headless routes then send the dialog manager's own message
// (PostDialogCommand) instead of a key.
//
// The probe only reads: window classes, styles, rectangles and cached
// captions, plus DM_GETDEFID through SendMessageTimeout with
// SMTO_ABORTIFHUNG, so a hung app cannot block the helper. The mutating
// calls refuse inside a go test binary (LiveDesktopGuard): offline tests
// fake them through the helper's Services.

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
	procGetDlgItem          = user32.NewProc("GetDlgItem")
	procGetDlgCtrlID        = user32.NewProc("GetDlgCtrlID")
	procIsChild             = user32.NewProc("IsChild")
)

// DialogClass is the window class of a classic Win32 dialog (message
// boxes, common dialogs, resource dialogs).
const DialogClass = "#32770"

// IDCancel is the Cancel button's control ID (IDCANCEL): the command the
// dialog manager sends for Escape.
const IDCancel = 2

// Edit control styles (ES_*) the routing reads. RichEdit controls share
// them.
const (
	ESMultiline  = 0x0004
	ESPassword   = 0x0020
	ESReadOnly   = 0x0800
	ESWantReturn = 0x1000
)

const (
	dmGetDefID      = 0x0400 // DM_GETDEFID (WM_USER+0): the dialog's default button ID
	dcHasDefID      = 0x534B // DM_GETDEFID's high word when there is a default button
	wmCommand       = 0x0111 // WM_COMMAND
	smtoAbortIfHung = 0x0002 // SMTO_ABORTIFHUNG: give up at once on a hung app

	bsTypeMask      = 0x000F // BS_TYPEMASK
	bsPushButton    = 0x0000 // BS_PUSHBUTTON
	bsDefPushButton = 0x0001 // BS_DEFPUSHBUTTON

	// probeTimeout bounds DM_GETDEFID: the probe runs before every
	// press-key and type-text, so a slow app must not stall the routing.
	probeTimeout = 500 * time.Millisecond
)

// editClasses are the classic edit controls: a window handle per control
// that answers the edit messages (EM_REPLACESEL, EM_GETSEL). Lowercase.
var editClasses = map[string]bool{
	"edit":          true,
	"richedit20a":   true,
	"richedit20w":   true,
	"richedit50w":   true,
	"richeditd2dpt": true, // Windows 11 Notepad's editor
}

// Control is one child window as the keyboard routing sees it. HWND 0
// means there is no such control.
type Control struct {
	HWND    uint64
	ID      uint16 // GetDlgCtrlID
	Class   string
	Style   uint32 // GWL_STYLE
	Enabled bool
	Rect    Rect   // GetWindowRect, physical pixels
	Text    string // the caption (buttons only; never read for the focused control, which may be a password field)
}

// IsEdit reports whether the control is a classic edit control.
func (c Control) IsEdit() bool {
	return c.HWND != 0 && editClasses[strings.ToLower(c.Class)]
}

// Writable reports whether typed text can go into the control: an enabled
// classic edit control that is not read-only.
func (c Control) Writable() bool {
	return c.IsEdit() && c.Enabled && c.Style&ESReadOnly == 0
}

// WantsReturn reports whether the control keeps Return for itself: a
// multi-line edit with ES_WANTRETURN, where Return starts a new line
// instead of pressing the dialog's default button.
func (c Control) WantsReturn() bool {
	return c.IsEdit() && c.Style&ESMultiline != 0 && c.Style&ESWantReturn != 0
}

// Password reports whether the control is a password edit (ES_PASSWORD):
// its text is never echoed.
func (c Control) Password() bool {
	return c.IsEdit() && c.Style&ESPassword != 0
}

// PushButton reports whether the control is a push button
// (BS_PUSHBUTTON or BS_DEFPUSHBUTTON).
func (c Control) PushButton() bool {
	if c.HWND == 0 || !strings.EqualFold(c.Class, "Button") {
		return false
	}
	t := c.Style & bsTypeMask
	return t == bsPushButton || t == bsDefPushButton
}

// KeyTarget is the probe's reading of one live Window: what press-key and
// type-text route on (ADR 0011, Decision 4). The zero value routes every
// key and text Headed.
type KeyTarget struct {
	Dialog     bool    // the Window's class is #32770
	HasDefault bool    // DM_GETDEFID answered with DC_HASDEFID
	DefaultID  uint16  // the default button's control ID
	Default    Control // the default button (HWND 0 when the ID names no control)
	Cancel     Control // the IDCANCEL control (HWND 0 when there is none)
	Focus      Control // the focused control on the Window's thread, when it is the Window or inside it
}

// ProbeKeyTarget reads hwnd's KeyTarget. Every read is best effort: a
// Window that is gone, a hung dialog or a thread with no focus leaves the
// matching fields zero, and the routing then stays Headed.
func ProbeKeyTarget(hwnd uint64) KeyTarget {
	h := windows.HWND(hwnd)
	var t KeyTarget
	if r, _, _ := procIsWindow.Call(uintptr(h)); r == 0 {
		return t
	}
	t.Focus = FocusedControl(hwnd)
	if className(h) != DialogClass {
		return t
	}
	t.Dialog = true
	if reply, err := sendMessageTimeout(h, dmGetDefID, 0, 0, probeTimeout); err == nil {
		if id, ok := defaultIDOf(reply); ok {
			t.HasDefault, t.DefaultID = true, id
			t.Default = controlOf(dlgItem(h, id), true)
		}
	}
	t.Cancel = controlOf(dlgItem(h, IDCancel), true)
	return t
}

// FocusedControl reads the control that holds the keyboard focus on
// hwnd's thread (GetGUIThreadInfo hwndFocus). A thread keeps its focus
// window while it is in the background, so this works without the
// foreground. A focus outside hwnd (another top-level Window of the same
// thread) or none reads as no control.
func FocusedControl(hwnd uint64) Control {
	h := windows.HWND(hwnd)
	tid, err := windows.GetWindowThreadProcessId(h, nil)
	if err != nil || tid == 0 {
		return Control{}
	}
	info := windows.GUIThreadInfo{Size: uint32(unsafe.Sizeof(windows.GUIThreadInfo{}))}
	if err := windows.GetGUIThreadInfo(tid, &info); err != nil || info.Focus == 0 {
		return Control{}
	}
	f := info.Focus
	if f != h {
		if r, _, _ := procIsChild.Call(uintptr(h), uintptr(f)); r == 0 {
			return Control{}
		}
	}
	return controlOf(f, false)
}

// PostDialogCommand posts WM_COMMAND(MAKEWPARAM(id, BN_CLICKED), control)
// to a dialog: the message the dialog manager sends itself when the user
// presses Enter (the default button) or Escape (IDCANCEL). control is the
// button's window, or 0 when the dialog has none for that ID. Posting
// never waits on the app.
func PostDialogCommand(dialog uint64, id uint16, control uint64) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if r, _, err := procPostMessageW.Call(uintptr(dialog), wmCommand, dialogCommandWParam(id), uintptr(control)); r == 0 {
		return fmt.Errorf("PostMessage(WM_COMMAND) failed for window %d: %v", dialog, err)
	}
	return nil
}

// dialogCommandWParam is MAKEWPARAM(id, BN_CLICKED); BN_CLICKED is 0.
func dialogCommandWParam(id uint16) uintptr {
	return uintptr(id)
}

// defaultIDOf decodes DM_GETDEFID's reply: the default button's ID in the
// low word when the high word is DC_HASDEFID.
func defaultIDOf(reply uintptr) (uint16, bool) {
	if uint16(reply>>16) != dcHasDefID {
		return 0, false
	}
	return uint16(reply), true
}

// controlOf reads one child window; withText reads its cached caption
// too (GetWindowText reads it without a message for other processes'
// windows).
func controlOf(h windows.HWND, withText bool) Control {
	if h == 0 {
		return Control{}
	}
	c := Control{
		HWND:    uint64(h),
		Class:   className(h),
		Style:   windowLong(h, gwlStyle),
		Enabled: isEnabled(h),
	}
	if r, _, _ := procGetDlgCtrlID.Call(uintptr(h)); r != 0 {
		c.ID = uint16(r)
	}
	var r Rect
	if ok, _, _ := procGetWindowRect.Call(uintptr(h), uintptr(unsafe.Pointer(&r))); ok != 0 {
		c.Rect = r
	}
	if withText {
		c.Text = windowText(h)
	}
	return c
}

// dlgItem is GetDlgItem: the dialog's child with that control ID, or 0.
func dlgItem(dialog windows.HWND, id uint16) windows.HWND {
	r, _, _ := procGetDlgItem.Call(uintptr(dialog), uintptr(id))
	return windows.HWND(r)
}

// sendMessageTimeout is SendMessageTimeoutW with SMTO_ABORTIFHUNG: the
// message's result, or an error when the call failed or timed out (a
// timed-out message may still be processed later).
func sendMessageTimeout(h windows.HWND, msg uint32, wparam, lparam uintptr, timeout time.Duration) (uintptr, error) {
	var result uintptr
	r, _, err := procSendMessageTimeoutW.Call(uintptr(h), uintptr(msg), wparam, lparam,
		smtoAbortIfHung, uintptr(timeout.Milliseconds()), uintptr(unsafe.Pointer(&result)))
	if r == 0 {
		return 0, fmt.Errorf("SendMessageTimeout(%#x) failed for window %d: %v", msg, h, err)
	}
	return result, nil
}

// Edit messages the Headless type-text route sends (ticket 06). All three
// are below WM_USER, so Windows marshals their arguments across processes:
// EM_REPLACESEL's string pointer is copied into the target process, and
// EM_GETSEL and WM_GETTEXTLENGTH answer through the return value alone.
const (
	wmGetTextLength = 0x000E // WM_GETTEXTLENGTH
	emGetSel        = 0x00B0 // EM_GETSEL
	emReplaceSel    = 0x00C2 // EM_REPLACESEL

	// editTimeout bounds each edit message: a hung or busy app fails the
	// route instead of blocking the helper.
	editTimeout = 2 * time.Second
)

// EditState is an edit control's text length and selection, read before
// and after a Headless insert to verify it.
type EditState struct {
	Length   int  // WM_GETTEXTLENGTH, in UTF-16 units
	SelStart int  // EM_GETSEL start (the caret when nothing is selected)
	SelEnd   int  // EM_GETSEL end: the first character after the selection
	SelKnown bool // EM_GETSEL answers -1 when either end is past 65,535
}

// ReadEditState reads an edit control's length and selection. It sends
// only reading messages, through SendMessageTimeout with
// SMTO_ABORTIFHUNG. The text itself is never read: the control may be a
// password field.
//
// A RichEdit control counts a paragraph break as two units in
// WM_GETTEXTLENGTH but one in EM_GETSEL, and an ANSI control may count
// bytes; a selection across a paragraph break, or double-byte text in an
// ANSI control, can then fail the verification although the insert
// worked.
func ReadEditState(edit uint64) (EditState, error) {
	h := windows.HWND(edit)
	n, err := sendMessageTimeout(h, wmGetTextLength, 0, 0, editTimeout)
	if err != nil {
		return EditState{}, fmt.Errorf("reading the length of edit control %d failed: %v", edit, err)
	}
	reply, err := sendMessageTimeout(h, emGetSel, 0, 0, editTimeout)
	if err != nil {
		return EditState{}, fmt.Errorf("reading the selection of edit control %d failed: %v", edit, err)
	}
	s := EditState{Length: int(int32(n))}
	s.SelStart, s.SelEnd, s.SelKnown = editSelectionOf(reply)
	return s, nil
}

// editSelectionOf decodes EM_GETSEL's reply (sent with no pointers): the
// start in the low word, the end in the high word; -1 when either is past
// 65,535, and the selection is then unknown.
func editSelectionOf(reply uintptr) (start, end int, ok bool) {
	if int32(reply) == -1 {
		return 0, 0, false
	}
	return int(uint16(reply)), int(uint16(reply >> 16)), true
}

// emSetSel is EM_SETSEL: select-text's classic route (headless-max ticket
// 10). Below WM_USER with no pointers, so it crosses processes as is.
const emSetSel = 0x00B1

// RichEdit reports whether the control is a RichEdit control (RichEdit20A,
// RichEdit20W, RichEdit50W or RichEditD2DPT), which counts a paragraph
// break as two units in WM_GETTEXTLENGTH but one in EM_GETSEL.
func (c Control) RichEdit() bool {
	return c.IsEdit() && strings.HasPrefix(strings.ToLower(c.Class), "richedit")
}

// ReadControl reads one window as the keyboard routing sees a control
// (class, style, enabled state, rectangle; never its text). select-text
// reads the window behind an Element's NativeWindowHandle with it. A
// window that is gone reads as a control of no class.
func ReadControl(hwnd uint64) Control {
	return controlOf(windows.HWND(hwnd), false)
}

// SetEditSelection selects the UTF-16 units start to end of an edit
// control: SendMessageTimeout(EM_SETSEL, start, end), which moves no
// focus and needs no foreground (measured cross-thread, headless-100.md
// G8). end -1 selects to the end of the text. A timed-out message may
// still be processed afterwards, so a failure here may have taken effect.
func SetEditSelection(edit uint64, start, end int) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if _, err := sendMessageTimeout(windows.HWND(edit), emSetSel, uintptr(start), uintptr(end), editTimeout); err != nil {
		return fmt.Errorf("EM_SETSEL on edit control %d failed: %v", edit, err)
	}
	return nil
}

// ReplaceSelection inserts text into an edit control the way typing does:
// SendMessageTimeout(EM_REPLACESEL, TRUE, text) replaces the selection, or
// inserts at the caret, and the change can be undone (wParam TRUE). The
// message is below WM_USER, so Windows copies the string into the
// target process. A timed-out message may still be processed afterwards,
// so a failure here may have taken effect.
func ReplaceSelection(edit uint64, text string) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return fmt.Errorf("the text cannot be sent: %v", err)
	}
	if _, err := sendMessageTimeout(windows.HWND(edit), emReplaceSel, 1, uintptr(unsafe.Pointer(p)), editTimeout); err != nil {
		return fmt.Errorf("EM_REPLACESEL into edit control %d failed: %v", edit, err)
	}
	return nil
}
