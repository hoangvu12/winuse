package win

// Posted Clicks and posted wheel scrolls (headless-max ticket 12, ADR 0013
// rule 2): mouse messages posted to the classic Win32 control under a
// point of a background Window, without the foreground, the real pointer
// or SendInput. Measured (.scratch/headless-gaps/headless-100.md G1 and
// probes-round3.md): a posted double click selects a word in a background
// Edit, a posted right click opens its context menu, and a posted
// WM_MOUSEWHEEL scrolls a background LISTBOX; the foreground stayed put.
// A RichEdit in charmap ignored the same clicks, so posting stays opt-in
// and best effort, and only the allow-listed classic classes take it.
//
// The control is found from the target Window down, never from the
// desktop (no WindowFromPoint): a covered Window is still addressed.
// Every posting call refuses inside a go test binary (LiveDesktopGuard);
// the message building is pure and tested offline.

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The mouse messages and their wParam key flags.
const (
	wmCancelMode    = 0x001F
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmLButtonDbl    = 0x0203
	wmRButtonDown   = 0x0204
	wmRButtonUp     = 0x0205
	wmRButtonDbl    = 0x0206
	wmMButtonDown   = 0x0207
	wmMButtonUp     = 0x0208
	wmMButtonDbl    = 0x0209
	wmMouseWheel    = 0x020A
	wmMouseHWheel   = 0x020E
	mkLButton       = 0x0001
	mkRButton       = 0x0002
	mkMButton       = 0x0010
	cwpSkipInvis    = 0x0001 // CWP_SKIPINVISIBLE
	cwpSkipTransp   = 0x0004 // CWP_SKIPTRANSPARENT
	maxChildDescent = 32     // a Window's child chain is never this deep
)

var (
	procChildWindowFromPointEx = user32.NewProc("ChildWindowFromPointEx")
	procScreenToClient         = user32.NewProc("ScreenToClient")
)

// postableClasses are the classic control classes that take Posted
// Clicks and posted wheel scrolls (the ticket's allow-list), lower case.
// WinForms wraps the same controls as WindowsForms10.<CLASS>.app...
// (postableClass).
var postableClasses = map[string]bool{
	"edit":          true,
	"button":        true,
	"combobox":      true,
	"comboboxex32":  true,
	"combolbox":     true,
	"listbox":       true,
	"syslistview32": true,
	"systreeview32": true,
}

// PostableClass reports whether a control class takes Posted Clicks: the
// allow-listed classic classes, RichEdit (RichEdit20W, RICHEDIT50W, ...)
// and their WinForms wrappers (WindowsForms10.EDIT.app.0.xxx).
func PostableClass(class string) bool {
	c := strings.ToLower(class)
	if inner, ok := strings.CutPrefix(c, "windowsforms10."); ok {
		c, _, _ = strings.Cut(inner, ".")
	}
	return postableClasses[c] || strings.HasPrefix(c, "richedit")
}

// mouseLParam packs client coordinates the way the mouse messages carry
// them: x in the low word, y in the high word, each a signed 16-bit value.
func mouseLParam(x, y int32) uintptr {
	return uintptr(uint32(uint16(int16(x))) | uint32(uint16(int16(y)))<<16)
}

// clickMessages builds a posted click at a client point: a move first,
// then for each click the button down and up, the second click of a
// double as the button's double-click message, as Windows delivers a
// real double click to a CS_DBLCLKS control; then WM_CANCELMODE, so a
// control that took mouse capture on the down drops it. A right click
// ends at its up: the up opens the control's context menu, whose modal
// loop would dispatch the queued WM_CANCELMODE and close the menu at once
// (measured on a covered Edit, e2e live run 2026-11-03).
func clickMessages(button MouseButton, count int, x, y int32) []postedMessage {
	down, up, dbl, mk := uint32(wmLButtonDown), uint32(wmLButtonUp), uint32(wmLButtonDbl), uintptr(mkLButton)
	switch button {
	case MouseRight:
		down, up, dbl, mk = wmRButtonDown, wmRButtonUp, wmRButtonDbl, mkRButton
	case MouseMiddle:
		down, up, dbl, mk = wmMButtonDown, wmMButtonUp, wmMButtonDbl, mkMButton
	}
	lp := mouseLParam(x, y)
	msgs := []postedMessage{{wmMouseMove, 0, lp}}
	for i := 1; i <= count; i++ {
		first := down
		if i == 2 {
			first = dbl
		}
		msgs = append(msgs, postedMessage{first, mk, lp}, postedMessage{up, 0, lp})
	}
	if button == MouseRight {
		return msgs
	}
	return append(msgs, postedMessage{wmCancelMode, 0, 0})
}

// wheelMessages builds posted wheel scrolls at a screen point (the wheel
// messages carry screen coordinates): vertical first, then horizontal, in
// scroll's signs (positive dy scrolls down, so the wheel delta is
// negative; positive dx scrolls right). A large amount is several
// messages, each within the 16-bit delta field (wheelEvents' chunks).
func wheelMessages(sx, sy, dx, dy int32) []postedMessage {
	lp := mouseLParam(sx, sy)
	var msgs []postedMessage
	add := func(msg uint32, delta int32) {
		for d := delta; d != 0; {
			chunk := max(min(d, maxWheelChunk), -maxWheelChunk)
			msgs = append(msgs, postedMessage{msg, uintptr(uint32(uint16(int16(chunk))) << 16), lp})
			d -= chunk
		}
	}
	add(wmMouseWheel, -dy)
	add(wmMouseHWheel, dx)
	return msgs
}

// ChildAtPoint finds the deepest visible, non-transparent child window of
// top under a screen point (ChildWindowFromPointEx, from top down: a
// covered Window is still addressed), and returns it with the point in its
// client coordinates and its class. top itself is returned when no child
// is there.
func ChildAtPoint(top uint64, sx, sy int32) (hwnd uint64, cx, cy int32, class string) {
	h := windows.HWND(top)
	for range maxChildDescent {
		p := struct{ X, Y int32 }{sx, sy}
		procScreenToClient.Call(uintptr(h), uintptr(unsafe.Pointer(&p)))
		child, _, _ := procChildWindowFromPointEx.Call(uintptr(h), uintptr(uint32(p.X))|uintptr(uint32(p.Y))<<32, cwpSkipInvis|cwpSkipTransp)
		if child == 0 || windows.HWND(child) == h {
			break
		}
		h = windows.HWND(child)
	}
	p := struct{ X, Y int32 }{sx, sy}
	procScreenToClient.Call(uintptr(h), uintptr(unsafe.Pointer(&p)))
	return uint64(h), p.X, p.Y, className(h)
}

// withKeyState sets the modifiers' MK_* flags in every mouse message's key
// state (desktop-parity ticket 06): a mouse message's wParam carries the
// keys held while it happened; the wheel messages carry them in the low
// word, beside the delta. WM_CANCELMODE carries none.
func withKeyState(msgs []postedMessage, mk uintptr) []postedMessage {
	if mk == 0 {
		return msgs
	}
	out := make([]postedMessage, len(msgs))
	for i, m := range msgs {
		if m.msg != wmCancelMode {
			m.wparam |= mk
		}
		out[i] = m
	}
	return out
}

// PostClick posts a click (clickMessages) to target at its client point,
// with the modifiers' key state (Alt cannot be posted; the helper refuses
// it). It returns how many messages were posted, so the caller can tell
// "nothing was sent" (0) from a partial post.
func PostClick(target uint64, cx, cy int32, button MouseButton, count int, mods Modifiers) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	if count < 1 || count > 3 {
		return 0, fmt.Errorf("click count %d is not 1, 2 or 3", count)
	}
	return postAll(target, withKeyState(clickMessages(button, count, cx, cy), mods.MK()))
}

// PostWheel posts wheel scrolls (wheelMessages) to target at a screen
// point, with the modifiers' key state. It returns how many messages were
// posted.
func PostWheel(target uint64, sx, sy, dx, dy int32, mods Modifiers) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	return postAll(target, withKeyState(wheelMessages(sx, sy, dx, dy), mods.MK()))
}
