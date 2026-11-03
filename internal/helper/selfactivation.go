package helper

// Self-activation after a Headless Action (headless-max ticket 03, decided
// as "detect and report"): an App can take the foreground by itself when a
// Headless Action reaches it, most often by activating a dialog the click
// opened (measured in the safe probe batch: the fixture's dialog took the
// foreground 19 ms after a UIA Invoke). winuse cannot prevent it: an
// EnableWindow guard does not cover a dialog shown after the call, and
// LockSetForegroundWindow needs foreground rights winuse lacks (round 3).
// So it reports it, as restore-window does (ADR 0011 Decision 2) and
// launch-app does: the Action's output gains the line
//
//	Note: the App took the foreground; winuse does not take it back.
//
// when the foreground moved, during the Action or its settle, from a
// Window of another process to a Window of the target's process (the
// target, or a Window it opened). winuse never takes the foreground back.
//
// A Headed Action takes the foreground on purpose, so a request that ran
// runAction (markHeaded) reports nothing; a Batch with a Headed step
// neither. The check only reads the foreground and window owners.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// watchesForeground reports whether a command is an Action whose
// self-activation is reported. restore-window has its own report
// (windowactions.go), activate-window asks for the foreground, and drag
// and hover are always Headed.
func watchesForeground(name string) bool {
	switch name {
	case "click", "set-value", "perform-secondary-action", "scroll", "select-text",
		"press-key", "type-text", "invoke-menu", "batch",
		"minimize-window", "move-window", "resize-window", "close-window",
		"tab-new", "tab-close", "tab-switch",
		"navigate", "back", "forward", "reload", "upload", "dialog", "eval":
		return true
	}
	return false
}

// fgWatch is one watched request: whether it ran a Headed Action.
type fgWatch struct {
	mu     sync.Mutex
	headed bool
}

type fgWatchKey struct{}

// markHeaded records that the request under ctx took the foreground on
// purpose (runAction); outside a watched request it does nothing.
func markHeaded(ctx context.Context) {
	if w, ok := ctx.Value(fgWatchKey{}).(*fgWatch); ok {
		w.mu.Lock()
		w.headed = true
		w.mu.Unlock()
	}
}

func (w *fgWatch) wasHeaded() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.headed
}

// reportAfter wraps an Action command with the reports that follow its
// ok: the references it re-resolved in a fresh read (reportRefs,
// liverefs.go), the Windows it opened or closed (reportOpened) and, around
// that, the foreground its App took (watchForeground).
func (s Services) reportAfter(name string, cmd Command) Command {
	return s.watchForeground(name, s.reportOpened(name, s.reportRefs(cmd)))
}

// watchForeground reads the foreground and the target's process before
// the Action, and after it (and its settle) adds the note when the
// target's App took the foreground. A Window that does not resolve, or
// whose process cannot be read, is not watched; a failed Action keeps its
// error alone, except the steps' output a failed Batch carries (ranOn).
func (s Services) watchForeground(name string, cmd Command) Command {
	if !watchesForeground(name) {
		return cmd
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		pid, ok := s.targetPID(args)
		if !ok {
			return cmd(ctx, args)
		}
		fgBefore := s.Foreground()
		w := &fgWatch{}
		out, err := cmd(context.WithValue(ctx, fgWatchKey{}, w), args)
		if (err != nil && !ranOn(err)) || w.wasHeaded() {
			return out, err
		}
		if err != nil {
			out = err.Out // a Batch that ran past a failed step (opened.go)
		}
		took := s.tookForeground(pid, fgBefore)
		if !took && strings.Contains(out, notePrefix+"a new Window opened") {
			// A dialog is activated a moment after it shows (19 ms in the
			// probe): one more look.
			s.pause(openedPoll)
			took = s.tookForeground(pid, fgBefore)
		}
		if took {
			out = withNotes(out, launchForegroundNote)
		}
		if err != nil {
			err.Out = out
			return "", err
		}
		return out, nil
	}
}

// targetPID is the process of the Action's Window, read before it runs.
func (s Services) targetPID(args json.RawMessage) (uint32, bool) {
	var a struct {
		Window string `json:"window"`
	}
	if json.Unmarshal(args, &a) != nil || strings.TrimSpace(a.Window) == "" {
		return 0, false
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return 0, false // the Action fails on it anyway
	}
	pid := s.WindowPID(w.ID)
	return pid, pid != 0
}

// tookForeground reports whether the foreground moved from a Window of
// another process to one of pid's.
func (s Services) tookForeground(pid uint32, fgBefore uint64) bool {
	fg := s.Foreground()
	if fg == 0 || fg == fgBefore || s.WindowPID(fg) != pid {
		return false
	}
	return fgBefore == 0 || s.WindowPID(fgBefore) != pid
}

// pause sleeps through Services.Sleep.
func (s Services) pause(d time.Duration) {
	if s.Sleep != nil {
		s.Sleep(d)
		return
	}
	time.Sleep(d)
}
