// Package win is winuse's Win32 layer: process identity, DPI awareness,
// window enumeration and the detached-helper spawn. All raw syscalls live
// here; other packages consume Go values so the interesting logic (filtering,
// formatting, protocol) stays testable offline.
//
// The package is Windows-only (the whole module is; see README).
package win

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// Identity is the security identity of a winuse process. Pipe names, the
// single-instance mutex and the pipe DACL are bound to it so that only the
// current user can talk to the helper, and elevated and non-elevated winuse
// processes stay separate.
type Identity struct {
	SID      string // e.g. "S-1-5-21-..."
	Session  uint32 // terminal session id
	Elevated bool   // token is elevated (admin)
}

// String renders the identity for pipe/mutex naming: sid, session, and an
// "-admin" marker when elevated.
func (id Identity) String() string {
	s := fmt.Sprintf("%s-%d", id.SID, id.Session)
	if id.Elevated {
		s += "-admin"
	}
	return s
}

// CurrentIdentity reads the calling process's identity.
func CurrentIdentity() (Identity, error) {
	token := windows.GetCurrentProcessToken()
	tu, err := token.GetTokenUser()
	if err != nil {
		return Identity{}, fmt.Errorf("reading process user: %w", err)
	}
	sid := tu.User.Sid.String()
	var session uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &session); err != nil {
		return Identity{}, fmt.Errorf("reading session id: %w", err)
	}
	return Identity{SID: sid, Session: session, Elevated: token.IsElevated()}, nil
}

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procOpenInputDesktop = user32.NewProc("OpenInputDesktop")
	procCloseDesktop     = user32.NewProc("CloseDesktop")
)

// HasInputDesktop reports whether the calling process can see the interactive
// input desktop. winuse drives a real desktop: a process in session 0 (a
// service, an SSH-spawned process) has no input desktop and must fail loudly
// instead of silently seeing no windows. A locked desktop still answers true
// (detecting the lock is DesktopLocked's job, at Action time).
func HasInputDesktop() bool {
	_, readable := InputDesktopName()
	return readable
}

// ErrNoDesktop is returned when the process has no input desktop.
var ErrNoDesktop = errors.New("winuse needs an interactive desktop session; this process has none (session 0 / service?)")
