package helper

// The Window identity check (multi-agent ticket 09, ADR 0014): a Window ID
// is the HWND, and Windows hands a closed Window's handle to a new one. So
// the helper records what each Window ID named, the owning process, that
// process's start time and the window class (win.WindowIdentity), when the
// Window is listed or read, and checks it before every serialized command
// that names a Window by its ID. A mismatch means the Window the agent
// knew closed and another took its ID: the command is refused with nothing
// sent, and the agent lists the Windows again.
//
// The records are shared by all agents (a list or read records what it
// sees, replacing a record whose handle now names another Window) and are
// dropped when their Window closes: list-windows prunes the records of
// Windows that are gone, and a refused check drops its record so the next
// list or read records the new Window. A Window with no record (the helper
// restarted) is recorded at its first Action and passes. A Window whose
// identity cannot be read is not checked: its command fails on it anyway.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/hoangvu12/winuse/internal/win"
)

// windowRecords is what each Window ID named when it was listed or read.
type windowRecords struct {
	mu   sync.Mutex
	seen map[uint64]win.WindowOwner
}

func newWindowRecords() *windowRecords {
	return &windowRecords{seen: map[uint64]win.WindowOwner{}}
}

// record stores what hwnd names now.
func (r *windowRecords) record(hwnd uint64, o win.WindowOwner) {
	r.mu.Lock()
	r.seen[hwnd] = o
	r.mu.Unlock()
}

// forget drops hwnd's record.
func (r *windowRecords) forget(hwnd uint64) {
	r.mu.Lock()
	delete(r.seen, hwnd)
	r.mu.Unlock()
}

// lookup returns hwnd's record.
func (r *windowRecords) lookup(hwnd uint64) (win.WindowOwner, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.seen[hwnd]
	return o, ok
}

// ids lists the recorded Window IDs.
func (r *windowRecords) ids() []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uint64, 0, len(r.seen))
	for id := range r.seen {
		out = append(out, id)
	}
	return out
}

// sawWindow records what a listed or read Window ID names now.
func (s Services) sawWindow(hwnd uint64) {
	if s.windowIDs == nil || s.WindowIdentity == nil {
		return
	}
	if o, ok := s.WindowIdentity(hwnd); ok {
		s.windowIDs.record(hwnd, o)
	}
}

// sawWindows records the listed Windows and drops the records of Windows
// that are gone.
func (s Services) sawWindows(ws []win.WindowInfo) {
	if s.windowIDs == nil || s.WindowIdentity == nil {
		return
	}
	for _, w := range ws {
		s.sawWindow(w.ID)
	}
	for _, id := range s.windowIDs.ids() {
		if _, ok := s.WindowIdentity(id); !ok {
			s.windowIDs.forget(id)
		}
	}
}

// numericWindow is the Window ID a command's arguments name, when they
// name one by its ID (an App name always resolves afresh).
func numericWindow(args json.RawMessage) (uint64, bool) {
	var a struct {
		Window string `json:"window"`
	}
	if json.Unmarshal(args, &a) != nil {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(a.Window), 10, 64)
	return id, err == nil
}

// checkWindow refuses a command whose Window ID now names another Window
// than the one it named when it was listed or read.
func (s Services) checkWindow(cmd Command) Command {
	if s.windowIDs == nil || s.WindowIdentity == nil {
		return cmd
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		id, ok := numericWindow(args)
		if !ok {
			return cmd(ctx, args)
		}
		now, ok := s.WindowIdentity(id)
		if !ok {
			s.windowIDs.forget(id) // gone: the command fails on it anyway
			return cmd(ctx, args)
		}
		was, ok := s.windowIDs.lookup(id)
		switch {
		case !ok:
			s.windowIDs.record(id, now) // first seen here (a helper restart)
		case was != now:
			s.windowIDs.forget(id)
			return "", reusedWindowError(id)
		}
		return cmd(ctx, args)
	}
}

// reusedWindowError is the refusal of a Window ID another Window took.
func reusedWindowError(id uint64) *Error {
	return Fail(fmt.Sprintf("window %d closed; a new window reused its ID; nothing was sent", id),
		"run winuse list-windows and use the Window ID it prints now; read the new Window's State before acting on it")
}

// recordReads records the Window a successful read names, without
// enumerating the Windows again: get-window-state, find and the other
// reads that take a Window ID, and the Window line get-window and
// launch-app print (a read by App name prints no ID otherwise, and an App
// name is never checked).
func (s Services) recordReads(name string, cmd Command) Command {
	if s.windowIDs == nil || s.WindowIdentity == nil || name == "list-windows" {
		return cmd // list-windows records every listed Window itself
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		out, err := cmd(ctx, args)
		if err != nil {
			return out, err
		}
		if name == "launch-app" || name == "get-window" {
			if id, perr := strconv.ParseUint(strings.SplitN(out, "\t", 2)[0], 10, 64); perr == nil {
				s.sawWindow(id)
			}
			return out, nil
		}
		if id, ok := numericWindow(args); ok {
			s.sawWindow(id)
		}
		return out, nil
	}
}
