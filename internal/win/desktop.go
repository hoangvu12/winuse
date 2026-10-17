package win

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Locked-desktop detection for Actions (ticket 05): a locked (or secure)
// desktop refuses input, so Actions must be refused with an explanation
// instead of acting on the lock screen (spec, Acting details; user story
// 48). Detection per win32.md §7: the WTS session state is the primary
// check; the input desktop's name must be "Default" as the secondary one
// (which also catches the UAC secure desktop).

var (
	wtsapi32                     = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInfoW     = wtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory            = wtsapi32.NewProc("WTSFreeMemory")
	procGetUserObjectInformation = user32.NewProc("GetUserObjectInformationW")
)

// DesktopLocked reports whether the interactive desktop refuses input: the
// session is locked, or the input desktop is the secure one (the lock
// screen's LockApp, the UAC prompt). name is the input desktop's name when
// it could be read ("" when reading it was denied — which itself means a
// secure desktop is active). Reading session state sends no input and
// activates nothing.
func DesktopLocked() (locked bool, name string) {
	name, readable := InputDesktopName()
	if !readable {
		return true, "" // OpenInputDesktop denied: the secure desktop
	}
	if name != "Default" {
		return true, name
	}
	return sessionLocked(), name
}

// InputDesktopName returns the name of the current input desktop. readable
// is false when opening it was denied — the input desktop is then the
// secure desktop (Winlogon/UAC), which a normal process may not touch.
func InputDesktopName() (name string, readable bool) {
	const desktopReadObjects = 0x0001
	h, _, _ := procOpenInputDesktop.Call(0, 0, desktopReadObjects)
	if h == 0 {
		return "", false
	}
	defer procCloseDesktop.Call(h)
	buf := make([]uint16, 256)
	var n uint32
	const uoiName = 2
	if r, _, _ := procGetUserObjectInformation.Call(h, uoiName,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&n))); r == 0 {
		return "", true // opened, but the name is unreadable: not a secure desktop
	}
	return windows.UTF16ToString(buf[:n/2]), true
}

// sessionLocked reads the WTS session state of the current session:
// WTSINFOEX_LEVEL1.SessionFlags — 0 locked, 1 unlocked, -1 unknown. The
// Win7/2008R2 reversal does not apply to modern Windows.
func sessionLocked() bool {
	var buf *uint16
	var n uint32
	const (
		wtsCurrentServerHandle = 0
		wtsCurrentSession      = 0xFFFFFFFF
		wtsSessionInfoEx       = 25
	)
	r, _, _ := procWTSQuerySessionInfoW.Call(wtsCurrentServerHandle, wtsCurrentSession,
		wtsSessionInfoEx, uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&n)))
	if r == 0 || buf == nil || n < 20 {
		return false // unknown: not locked as far as we can tell
	}
	// WTSINFOEXW{Level(4) pad(4) union{WTSINFOEX_LEVEL1_W{SessionId,
	// SessionState, SessionFlags…}}}: SessionFlags sits at byte offset 16.
	flags := *(*int32)(unsafe.Add(unsafe.Pointer(buf), 16))
	procWTSFreeMemory.Call(uintptr(unsafe.Pointer(buf)))
	const wtsSessionStateLock = 0
	return flags == wtsSessionStateLock
}
