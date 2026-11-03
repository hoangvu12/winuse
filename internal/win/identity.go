package win

// A Window's identity (multi-agent ticket 09, ADR 0014): the Window ID is
// the HWND, and Windows reuses a closed Window's handle for a new one. The
// owning process id, that process's start time and the window class tell
// the Window an ID named when it was listed or read from a new one that
// took over its handle.

import (
	"golang.org/x/sys/windows"
)

// WindowOwner is what a Window ID named: its process and that process's
// start, and its window class.
type WindowOwner struct {
	PID     uint32
	Started int64 // the process's creation time (FILETIME, 100 ns since 1601)
	Class   string
}

// WindowIdentity reads the identity of the Window behind hwnd; false when
// the Window is gone or its process cannot be read. Read-only; refused
// (false) inside an offline test binary.
func WindowIdentity(hwnd uint64) (WindowOwner, bool) {
	if LiveDesktopGuard() != nil {
		return WindowOwner{}, false
	}
	h := windows.HWND(hwnd)
	if !windows.IsWindow(h) {
		return WindowOwner{}, false
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(h, &pid); err != nil || pid == 0 {
		return WindowOwner{}, false
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return WindowOwner{}, false
	}
	defer windows.CloseHandle(proc)
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(proc, &created, &exit, &kernel, &user); err != nil {
		return WindowOwner{}, false
	}
	return WindowOwner{PID: pid, Started: int64(created.HighDateTime)<<32 | int64(created.LowDateTime), Class: className(h)}, true
}
