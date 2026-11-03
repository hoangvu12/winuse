package helper

// The hung pre-check (multi-agent ticket 07, ADR 0009 as amended by ADR
// 0014): before an Action that reaches a Window's UI thread, the helper
// asks whether that thread responds (win.WindowHung: IsHungAppWindow, then
// a WM_NULL with a short timeout). A hung App is refused with nothing
// sent: a UIA call would only time out on its worker, and a posted
// message or real input would land late, whenever the App wakes. The
// check runs once the command holds its lanes, after the Window identity
// check. Page Actions, which reach the Page through the browser rather
// than the Window's UI thread, are not checked, nor are commands that name
// no Window.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type laneSetKey struct{}

// withLaneSet hands the lanes a command took to the wrappers inside them.
func withLaneSet(ctx context.Context, set laneSet) context.Context {
	return context.WithValue(ctx, laneSetKey{}, set)
}

func laneSetOf(ctx context.Context) (laneSet, bool) {
	set, ok := ctx.Value(laneSetKey{}).(laneSet)
	return set, ok
}

// reachesThread reports whether the set holds its Window's UI-thread lane:
// the command acts outside the Page.
func (l laneSet) reachesThread() bool {
	for _, k := range l.keys {
		if strings.HasPrefix(k, "1 ") {
			return true
		}
	}
	return false
}

// refuseHung refuses a command whose Window's UI thread is not responding.
func (s Services) refuseHung(cmd Command) Command {
	if s.WindowHung == nil {
		return cmd
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		if set, ok := laneSetOf(ctx); ok && set.window != 0 && set.reachesThread() && s.WindowHung(set.window) {
			return "", hungWindowError(set.window)
		}
		return cmd(ctx, args)
	}
}

// hungWindowError is the refusal of an Action on a hung App.
func hungWindowError(id uint64) *Error {
	return Fail(fmt.Sprintf("window %d's App is not responding; nothing was sent", id),
		"wait until the App responds, then read its Window State and retry; work in another App's Window meanwhile")
}
