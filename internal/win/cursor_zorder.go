package win

// The Agent Cursor's place in the z-order (multi-agent ticket 04, ADR 0014
// Decision 4, like cua's agent cursor): not topmost, but just above the
// Window its Action targets, so the user's Windows above that Window also
// cover the cursor, and each Agent's cursor sits with its own Window. The
// decision is pure (cursorInsertAfter, unit-tested against a faked
// z-order); placeAbove applies it with SetWindowPos.

import "golang.org/x/sys/windows"

const (
	hwndTop       = 0           // HWND_TOP: the top of the non-topmost band
	hwndNoTopmost = ^uintptr(1) // HWND_NOTOPMOST (-2)
	gwHwndPrev    = 3           // GW_HWNDPREV: the window above in the z-order
)

// cursorInsertAfter is the window the cursor's windows go just below
// (SetWindowPos's hWndInsertAfter), so that they sit just above anchor:
// the first window above anchor that is not an Agent Cursor window (every
// Agent's cursor windows are passed over, so they never anchor each
// other). When there is none, or it is topmost, the cursor goes to the
// top of the non-topmost band (hwndTop), which is still just above an
// anchor at the top of that band. An anchor of 0 (no target Window) also
// gives hwndTop. prev is GW_HWNDPREV, cursor reports an Agent Cursor
// window and topmost a topmost one.
func cursorInsertAfter(anchor uintptr, prev func(uintptr) uintptr, cursor, topmost func(uintptr) bool) uintptr {
	if anchor == 0 {
		return hwndTop
	}
	p := prev(anchor)
	for steps := 0; p != 0 && cursor(p) && steps < 64; steps++ {
		p = prev(p)
	}
	if p == 0 || cursor(p) || topmost(p) {
		return hwndTop
	}
	return p
}

// placeAbove puts the cursor's windows just above anchor, bottom to top in
// the order given (each inserted at the same place lands above the ones
// before it), and never topmost.
func placeAbove(anchor uintptr, wins ...*layeredWin) {
	after := cursorInsertAfter(anchor,
		func(h uintptr) uintptr { r, _, _ := procGetWindow.Call(h, gwHwndPrev); return r },
		func(h uintptr) bool { return className(windows.HWND(h)) == AgentCursorClassName },
		func(h uintptr) bool { return windowLong(windows.HWND(h), gwlExStyle)&wsExTopmost != 0 },
	)
	for _, l := range wins {
		// Leave the topmost band first: a window inserted after a
		// non-topmost one keeps its topmost bit otherwise.
		procSetWindowPos.Call(l.hwnd, hwndNoTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
		procSetWindowPos.Call(l.hwnd, after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
}
