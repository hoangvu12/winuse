package helper

// The Window Actions (ADR 0011, .scratch/headless-parity/spec.md "Window
// Actions"): restore-window, minimize-window, move-window, resize-window
// and close-window act on the Window itself rather than on one of its
// Elements. They are Headless with no Headed form: non-activating Win32
// calls alone (ShowWindowAsync, SetWindowPos with SWP_NOACTIVATE, a
// posted WM_CLOSE), so they never activate the Window, move the pointer,
// send keys or raise the in-control banner. The Agent Cursor does not
// show them either: the Window itself moves visibly. They are not Batch
// steps, because a Batch acts inside one Window.
//
// Each one verifies its outcome by reading the Window back, polled with a
// short bound: restore and minimize the iconic state, move and resize the
// visible frame (within placementTolerance), close that the Window went away.
// Every refusal raised before the Win32 call says "nothing was sent"; a
// failed read-back says the Action was delivered.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

const (
	// placementTolerance is how far, in physical pixels, a read-back
	// edge or size may sit from the requested one: DWM animation and
	// apps that snap their frame to a grid (agent-desktop's floor).
	placementTolerance = 8
	// placementVerifyWait bounds the read-back of restore, minimize, move
	// and resize: the calls are queued on the Window's own thread.
	placementVerifyWait = time.Second
	// closeWait bounds the wait for a closed Window to go away. An app
	// that asks to save keeps it open longer, and the output says so.
	closeWait = 2 * time.Second
	// windowPollInterval paces the read-back polls.
	windowPollInterval = 20 * time.Millisecond

	// The coordinate and size limits of move-window and resize-window.
	maxWindowCoordinate = 1_000_000
	maxWindowSize       = 1_000_000
)

// restoreWindow runs `winuse restore-window <window>`: a minimized or
// maximized Window comes back at its normal size and position without
// activating it (ShowWindowAsync with SW_SHOWNOACTIVATE), verified by
// its iconic and maximized state. Windows has no non-activating
// maximize, so a Window that was maximized before it was minimized
// (WPF_RESTORETOMAXIMIZED) comes back at its normal size, and the output
// says so. A Window that is already normal is left alone.
func restoreWindow(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.WindowActionArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("restore-window: "+err.Error(), "usage: winuse restore-window <window>")
		}
		return s.runWindowAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			minimized, maximized := s.Minimized(w.ID), s.Maximized(w.ID)
			if !minimized && !maximized {
				return okOutput, nil
			}
			toMaximized := minimized && s.RestoresMaximized(w.ID)
			if err := s.RestoreWindow(w.ID); err != nil {
				return "", windowCallError(err)
			}
			restored := s.pollWindow(ctx, placementVerifyWait, func() bool {
				// A Window that came back maximized after all is restored too.
				return !s.Minimized(w.ID) && (toMaximized || !s.Maximized(w.ID))
			})
			id := strconv.FormatUint(w.ID, 10)
			if !restored {
				state := "maximized"
				if s.Minimized(w.ID) {
					state = "minimized"
				}
				return "", Fail(fmt.Sprintf("window %d is still %s after the restore", w.ID, state),
					"the restore was delivered and the Window did not change; read winuse list-windows, or bring it to the foreground with winuse activate-window "+id)
			}
			if toMaximized && !s.Maximized(w.ID) {
				return "ok (restored at normal size: the Window was maximized before it was minimized, and Windows cannot maximize a Window without activating it)\n", nil
			}
			return okOutput, nil
		})
	}
}

// minimizeWindow runs `winuse minimize-window <window>`: the Window is
// minimized and the active Window stays active (ShowWindowAsync with
// SW_SHOWMINNOACTIVE), verified by its iconic state. A Window that is
// already minimized is left alone.
func minimizeWindow(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.WindowActionArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("minimize-window: "+err.Error(), "usage: winuse minimize-window <window>")
		}
		return s.runWindowAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if s.Minimized(w.ID) {
				return okOutput, nil
			}
			if err := s.MinimizeWindow(w.ID); err != nil {
				return "", windowCallError(err)
			}
			if !s.pollWindow(ctx, placementVerifyWait, func() bool { return s.Minimized(w.ID) }) {
				return "", Fail(fmt.Sprintf("window %d is not minimized after the minimize", w.ID),
					"the minimize was delivered and the app did not minimize; read winuse list-windows before repeating it")
			}
			return okOutput, nil
		})
	}
}

// moveWindow runs `winuse move-window <window> <x> <y>`: the visible
// frame's top-left moves to the screen point in physical pixels, keeping
// the size and z-order, without activating the Window (SetWindowPos with
// SWP_NOACTIVATE | SWP_NOZORDER | SWP_ASYNCWINDOWPOS). The rectangle is
// the screenshot line's `window at` one, the extended frame bounds
// without the invisible resize borders (win.MoveNoActivate), so its
// numbers can be passed here directly; it reads back in the same
// coordinates within placementTolerance.
func moveWindow(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		const usage = "usage: winuse move-window <window> <x> <y>"
		var a protocol.MoveWindowArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("move-window: "+err.Error(), usage)
		}
		if !windowArgInRange(a.X, -maxWindowCoordinate, maxWindowCoordinate) || !windowArgInRange(a.Y, -maxWindowCoordinate, maxWindowCoordinate) {
			return "", Fail(fmt.Sprintf("move-window: x and y must be between %d and %d, got %d,%d",
				-maxWindowCoordinate, maxWindowCoordinate, a.X, a.Y), usage)
		}
		x, y := int32(a.X), int32(a.Y)
		return s.runWindowAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if rerr := s.placeable(w); rerr != nil {
				return "", rerr
			}
			if err := s.MoveWindow(w.ID, x, y); err != nil {
				return "", windowCallError(err)
			}
			r, perr := s.pollPlacement(ctx, w.ID, func(r win.Rect) bool {
				return placedNear(r.Left, x) && placedNear(r.Top, y)
			})
			if perr != nil {
				return "", perr
			}
			if r != nil {
				return "", Fail(fmt.Sprintf("window %d is at %d,%d (%dx%d) after the move to %d,%d",
					w.ID, r.Left, r.Top, r.Width(), r.Height(), x, y), placementHint("move", w.ID))
			}
			return okOutput, nil
		})
	}
}

// resizeWindow runs `winuse resize-window <window> <width> <height>`: the
// visible frame's size in physical pixels (the screenshot line's `window
// at` width and height), keeping its top-left and z-order, without
// activating the Window (the same SetWindowPos flags as moveWindow), read
// back within placementTolerance.
func resizeWindow(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		const usage = "usage: winuse resize-window <window> <width> <height>"
		var a protocol.ResizeWindowArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("resize-window: "+err.Error(), usage)
		}
		if !windowArgInRange(a.Width, 1, maxWindowSize) || !windowArgInRange(a.Height, 1, maxWindowSize) {
			return "", Fail(fmt.Sprintf("resize-window: width and height must be between 1 and %d, got %dx%d",
				maxWindowSize, a.Width, a.Height), usage)
		}
		width, height := int32(a.Width), int32(a.Height)
		return s.runWindowAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if rerr := s.placeable(w); rerr != nil {
				return "", rerr
			}
			if err := s.ResizeWindow(w.ID, width, height); err != nil {
				return "", windowCallError(err)
			}
			r, perr := s.pollPlacement(ctx, w.ID, func(r win.Rect) bool {
				return placedNear(r.Width(), width) && placedNear(r.Height(), height)
			})
			if perr != nil {
				return "", perr
			}
			if r != nil {
				return "", Fail(fmt.Sprintf("window %d is %dx%d (at %d,%d) after the resize to %dx%d",
					w.ID, r.Width(), r.Height(), r.Left, r.Top, width, height), placementHint("resize", w.ID))
			}
			return okOutput, nil
		})
	}
}

// closeWindow runs `winuse close-window <window>`: WM_CLOSE is posted to
// the Window, the same request as its close button, and winuse waits up
// to closeWait for it to go away. A Window still open then is not a
// failure: the close was requested, and the app may be asking to save.
// Nothing is ever terminated.
func closeWindow(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.WindowActionArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("close-window: "+err.Error(), "usage: winuse close-window <window>")
		}
		return s.runWindowAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if err := s.CloseWindow(w.ID); err != nil {
				return "", windowCallError(err)
			}
			if s.pollWindow(ctx, closeWait, func() bool { return !s.WindowOpen(w.ID) }) {
				return okOutput, nil
			}
			return fmt.Sprintf("ok (the close was requested and window %d is still open; it may be asking to save: read winuse list-windows or the Window State next, winuse get-window-state %d)\n",
				w.ID, w.ID), nil
		})
	}
}

// runWindowAction is the Window Actions' pipeline: resolve the Window;
// refuse a locked desktop and an administrator Window (UIPI blocks
// SetWindowPos, ShowWindowAsync and posted messages across the integrity
// boundary) with "nothing was sent"; then run the Action's own step
// inside a passive Esc scope, like every Headless Action. No activation,
// no banner, no Agent Cursor.
func (s Services) runWindowAction(ctx context.Context, ref string, act func(ctx context.Context, w win.WindowInfo) (string, *Error)) (string, *Error) {
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return "", rerr
	}
	if locked, desktop := s.Locked(); locked {
		return "", nothingSent(lockedDesktopFailure(desktop))
	}
	if err := s.Integrity(w); err != nil {
		var admin *win.AdminWindowError
		if errors.As(err, &admin) {
			return "", nothingSent(Fail(err.Error(),
				"Windows blocks placing and closing a higher-integrity Window; start winuse from an elevated terminal to control administrator Windows, or ask the user to operate this Window"))
		}
		return "", nothingSent(Fail(err.Error(), ""))
	}
	defer armPassive(s.Esc)()
	out, e := act(ctx, w)
	if s.Esc.Stopped() {
		return "", escStopError()
	}
	return out, e
}

// placeable refuses to move or resize a minimized or maximized Window:
// its placement is not its frame on screen, and restoring it is the
// agent's call.
func (s Services) placeable(w win.WindowInfo) *Error {
	id := strconv.FormatUint(w.ID, 10)
	if s.Minimized(w.ID) {
		return nothingSent(Fail(fmt.Sprintf("window %d is minimized", w.ID),
			"restore it first: winuse restore-window "+id))
	}
	if s.Maximized(w.ID) {
		return nothingSent(Fail(fmt.Sprintf("window %d is maximized", w.ID),
			"restore it to its normal size first: winuse restore-window "+id))
	}
	return nil
}

// pollPlacement polls the Window's visible frame until ok holds or
// placementVerifyWait passes. It returns nil when the placement holds,
// the last rectangle when it never did, and an error when the rectangle
// cannot be read (the Window closed meanwhile).
func (s Services) pollPlacement(ctx context.Context, hwnd uint64, ok func(win.Rect) bool) (*win.Rect, *Error) {
	var (
		r   win.Rect
		err error
	)
	held := s.pollWindow(ctx, placementVerifyWait, func() bool {
		r, err = s.WindowFrame(hwnd)
		return err != nil || ok(r)
	})
	if err != nil {
		return nil, Fail(err.Error(), "the Action was delivered; the Window may have closed, run winuse list-windows")
	}
	if held {
		return nil, nil
	}
	return &r, nil
}

// pollWindow polls ok every windowPollInterval until it holds, wait
// passes or the request ends. The wait is counted in intervals slept, so
// a faked Sleep runs the poll instantly; on the real desktop each probe
// adds a little to it.
func (s Services) pollWindow(ctx context.Context, wait time.Duration, ok func() bool) bool {
	for waited := time.Duration(0); ; waited += windowPollInterval {
		if ok() {
			return true
		}
		if waited >= wait || ctx.Err() != nil {
			return false
		}
		s.Sleep(windowPollInterval)
	}
}

// windowCallError maps a failed Win32 call: the call itself was refused,
// so nothing reached the Window.
func windowCallError(err error) *Error {
	return nothingSent(Fail(err.Error(), "the Window may have closed; run winuse list-windows"))
}

// placementHint is the hint of a placement that did not read back: it was
// delivered, so the agent reads the visible frame instead of repeating it.
func placementHint(verb string, hwnd uint64) string {
	return "the " + verb + " was delivered; the app may limit where its Window goes or how large it gets; do not repeat it, read where it is from the screenshot line's window at X,Y WxH (the coordinates move-window and resize-window take): winuse get-window-state " +
		strconv.FormatUint(hwnd, 10) + " --screenshot"
}

// placedNear reports whether a read-back value is within placementTolerance of
// the requested one.
func placedNear(got, want int32) bool {
	d := int64(got) - int64(want)
	return d >= -placementTolerance && d <= placementTolerance
}

// windowArgInRange reports whether lo <= v <= hi.
func windowArgInRange(v, lo, hi int) bool {
	return v >= lo && v <= hi
}
