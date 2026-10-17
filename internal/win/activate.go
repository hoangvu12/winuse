package win

// Foreground activation for Actions (ticket 05): every Action brings its
// target Window to the foreground first and verifies it arrived before
// any input is injected (spec, Acting details — Codex behaviour, minus
// its known activation bugs).

import (
	"fmt"
	"runtime"
	"time"

	"golang.org/x/sys/windows"
)

var (
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop    = user32.NewProc("BringWindowToTop")
	procShowWindow          = user32.NewProc("ShowWindow")
	procAttachThreadInput   = user32.NewProc("AttachThreadInput")
	procIsWindowEnabled     = user32.NewProc("IsWindowEnabled")
	procGetLastActivePopup  = user32.NewProc("GetLastActivePopup")
)

const (
	swRestore = 9 // SW_RESTORE

	// Activation is asynchronous: poll for the foreground change for a
	// short, bounded period (win32.md §5).
	activateVerifyTimeout = 500 * time.Millisecond
	activatePollInterval  = 20 * time.Millisecond
)

// NotForegroundError reports that a Window could not be brought to the
// foreground within the verification period.
type NotForegroundError struct{ WindowID uint64 }

func (e *NotForegroundError) Error() string {
	return fmt.Sprintf("failed to activate window %d", e.WindowID)
}

// ModalDialogError reports that a Window cannot be activated because a
// modal dialog of its own App is open: the Window is disabled and focus
// belongs to the dialog. The error names the dialog's Window ID so the
// agent can act on the dialog instead (Codex's owned-dialog bug class,
// codex-surface.md §6).
type ModalDialogError struct {
	WindowID uint64
	DialogID uint64
}

func (e *ModalDialogError) Error() string {
	return fmt.Sprintf("window %d has a modal dialog open (window %d); act on the dialog instead", e.WindowID, e.DialogID)
}

// Activate brings hwnd to the foreground and verifies it is in front,
// using the standard foreground-lock workarounds (win32.md §5): a no-op
// injected move to satisfy "the caller received the last input event",
// plus AttachThreadInput to the current foreground thread for the
// SetForegroundWindow call.
//
// A Window disabled by its open modal dialog fails with a
// ModalDialogError naming the dialog. A minimized Window is restored —
// and only a minimized one (un-maximizing on activation is a documented
// Codex bug). No input is injected while the Window is disabled.
func Activate(hwnd uint64) error {
	h := windows.HWND(hwnd)

	// AttachThreadInput binds OS threads, and Go may migrate this goroutine
	// between threads across syscalls: pin the thread for the duration so
	// attach and detach hit the same thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// A disabled window cannot take the foreground: point at the modal
	// dialog instead of failing generically.
	if !isEnabled(h) {
		if popup := lastActivePopup(h); popup != 0 && popup != uintptr(h) {
			return &ModalDialogError{WindowID: hwnd, DialogID: uint64(popup)}
		}
		return &NotForegroundError{WindowID: hwnd}
	}
	if isIconic(h) {
		procShowWindow.Call(uintptr(h), swRestore)
	}
	if windows.GetForegroundWindow() == h {
		return nil
	}

	// Workaround 1: a process that received the last input event may set
	// the foreground window. Inject a zero-distance relative move from
	// this process — no visible effect, no menu side effects (unlike the
	// classic Alt trick).
	sendInput(mouseEvent(mouseEventfMove))

	// Workaround 2: attach to the current foreground thread's input queue
	// for the call, sharing its activation permission. The queues stay
	// attached only for the call (key state is shared while attached).
	if fg := windows.GetForegroundWindow(); fg != 0 {
		me := windows.GetCurrentThreadId()
		if fgTID, err := windows.GetWindowThreadProcessId(fg, nil); err == nil && fgTID != 0 && fgTID != me {
			procAttachThreadInput.Call(uintptr(me), uintptr(fgTID), 1)
			defer procAttachThreadInput.Call(uintptr(me), uintptr(fgTID), 0)
		}
	}
	procSetForegroundWindow.Call(uintptr(h))
	procBringWindowToTop.Call(uintptr(h))

	// Verify: poll briefly for the (asynchronous) foreground change.
	deadline := time.Now().Add(activateVerifyTimeout)
	for time.Now().Before(deadline) {
		if windows.GetForegroundWindow() == h {
			return nil
		}
		time.Sleep(activatePollInterval)
	}
	return &NotForegroundError{WindowID: hwnd}
}

func isEnabled(h windows.HWND) bool {
	r, _, _ := procIsWindowEnabled.Call(uintptr(h))
	return r != 0
}

// lastActivePopup returns h's most recently active owned popup (h itself
// when it has none) — the open modal dialog of a disabled Window.
func lastActivePopup(h windows.HWND) uintptr {
	r, _, _ := procGetLastActivePopup.Call(uintptr(h))
	return r
}
