package win

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Integrity-level checks for Actions (ticket 05): Windows silently drops
// injected input aimed at a higher-integrity process (UIPI, no-write-up) —
// neither SendInput's return value nor GetLastError reports it — so every
// Action compares the target Window's integrity with its own before
// acting and refuses administrator Windows with an explanation (spec,
// Acting details; user story 47).

// Integrity level RIDs (win32.md §7, mandatory integrity control).
const (
	IntegrityUntrusted  uint32 = 0x0000
	IntegrityLow        uint32 = 0x1000
	IntegrityMedium     uint32 = 0x2000
	IntegrityMediumPlus uint32 = 0x2100
	IntegrityHigh       uint32 = 0x3000
	IntegritySystem     uint32 = 0x4000
)

// AdminWindowError reports that acting on a Window would have its input
// silently dropped: the Window's process runs with higher integrity than
// this process (an administrator Window), or its integrity could not be
// read — for a medium-integrity winuse that means the target is elevated
// (an elevated process's token is usually unreadable from medium).
type AdminWindowError struct {
	WindowID   uint64
	App        string // executable file name, for the message
	Unreadable bool   // the target's integrity could not be read
}

func (e *AdminWindowError) Error() string {
	likely := ""
	if e.Unreadable {
		likely = "likely "
	}
	return fmt.Sprintf("window %d (%s) is %srunning as administrator; winuse cannot send it input",
		e.WindowID, e.App, likely)
}

// RefusesInput reports whether input from a process with integrity own
// would be silently dropped in a process with integrity target: UIPI's
// no-write-up rule refuses strictly-higher targets; equal or lower targets
// (including an elevated winuse acting on medium Windows) receive input.
func RefusesInput(target, own uint32) bool { return target > own }

// CheckWindowIntegrity refuses w when input to its process would be
// silently dropped: its integrity is strictly higher than this process's,
// or its token cannot be read (treated as elevated — the safe assumption,
// since input to it is dropped either way). No input is sent either way.
func CheckWindowIntegrity(w WindowInfo) error {
	if w.PID == 0 {
		return fmt.Errorf("cannot read the process of window %d; run winuse list-windows again", w.ID)
	}
	own, err := IntegrityLevel(0)
	if err != nil {
		return fmt.Errorf("reading winuse's own integrity level failed: %v", err)
	}
	target, err := IntegrityLevel(w.PID)
	if err != nil {
		return &AdminWindowError{WindowID: w.ID, App: AppExeName(w), Unreadable: true}
	}
	if RefusesInput(target, own) {
		return &AdminWindowError{WindowID: w.ID, App: AppExeName(w)}
	}
	return nil
}

// IntegrityLevel returns the integrity level RID of the process with the
// given pid (0: the calling process).
func IntegrityLevel(pid uint32) (uint32, error) {
	var tok windows.Token
	if pid == 0 {
		tok = windows.GetCurrentProcessToken()
	} else {
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			return 0, err
		}
		defer windows.CloseHandle(h)
		if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
			return 0, err
		}
		defer tok.Close()
	}
	var n uint32
	windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, nil, 0, &n)
	if n == 0 {
		return 0, errors.New("the token reported no integrity level")
	}
	buf := make([]byte, n)
	if err := windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&buf[0])), n, &n); err != nil {
		return 0, err
	}
	sid := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buf[0])).Label.Sid
	if sid.SubAuthorityCount() == 0 {
		return 0, errors.New("the token's integrity label has no sub-authorities")
	}
	return sid.SubAuthority(uint32(sid.SubAuthorityCount() - 1)), nil
}
