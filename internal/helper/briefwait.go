package helper

// The brief wait on a refusal that may pass on its own (reliability-council
// 06): an Action refused because its Element is disabled, because a modal
// dialog blocks its Window, or because its name:/id: reference matches
// nothing yet, waits up to briefWaitLimit for that to change and then runs
// once more. A "Next" button that enables 300 ms later no longer costs the
// agent a turn and a re-read. The output says so:
//
//	ok
//	Note: waited 0.4 s for element 14 to enable
//
// The Action runs on its lanes as always (lanes.go). Only a refusal typed
// at its source (Error.wait) and proven to have sent nothing (SentNothing)
// waits: by then the Action has returned and released its lanes, so the
// poll holds none (ADR 0014), and the next try retakes them and makes its
// own checks again, the one more check under the lane. A failure that may
// have taken effect is never tried again. Nothing visible or unoccluded is
// asked of a Headless Action: acting on a covered Window is the point.
//
// The poll only reads, as the desktop waits do (waits.go): the Element's
// live IsEnabled, the Window's modal dialog, or the Window's tree (without
// replacing the Agent's Window State). It ends at briefWaitLimit, half the
// request's remaining budget, or the user's Esc (nothing was sent, exit
// 130). Past it the Action fails with its own refusal, which now says how
// long winuse waited. --no-wait (Request.NoWait) fails at once, as before.
// A Batch's steps never wait: the Batch holds its lanes from its first
// step to its last.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

const (
	// briefWaitLimit bounds the wait: long enough for a button that
	// enables after a validation or a dialog that is closing, short beside
	// a turn. agent-desktop's 5 s would be too long for a refusal that is
	// real.
	briefWaitLimit = 1800 * time.Millisecond
	// briefWaitPoll paces the cheap polls: one property read, or one
	// window style read.
	briefWaitPoll = 100 * time.Millisecond
)

// refusalKind is what a refusal that may pass on its own waits for.
type refusalKind int

const (
	waitEnable refusalKind = iota // a disabled Element: to enable
	waitDialog                    // a modal dialog over the Window: to close
	waitAppear                    // a name:/id: reference that matches nothing: to appear
)

// refusalWait is a refusal's wait, set where the refusal is raised.
type refusalWait struct {
	kind   refusalKind
	window win.WindowInfo
	index  int    // waitEnable: the Element Index refused
	ref    string // waitAppear: the reference
}

// waitsFor marks e as a refusal that may pass on its own.
func waitsFor(e *Error, r refusalWait) *Error {
	e.wait = &r
	return e
}

// what is the refusal's wait in a note's words.
func (r refusalWait) what() string {
	switch r.kind {
	case waitEnable:
		return fmt.Sprintf("element %d to enable", r.index)
	case waitDialog:
		return fmt.Sprintf("the modal dialog over window %d to close", r.window.ID)
	}
	return fmt.Sprintf("%s to appear in window %d", r.ref, r.window.ID)
}

// interval is the pause between two polls: a tree walk is paced as a text
// wait's.
func (r refusalWait) interval() time.Duration {
	if r.kind == waitAppear {
		return treeWaitPoll
	}
	return briefWaitPoll
}

// briefRefusal is a failure's wait when it may pass on its own and
// provably sent nothing; nil for any other.
func briefRefusal(e *Error) *refusalWait {
	if e == nil || e.wait == nil || !SentNothing(e) {
		return nil
	}
	return e.wait
}

type noWaitKey struct{}

// withNoWait marks a request sent with --no-wait.
func withNoWait(ctx context.Context) context.Context {
	return context.WithValue(ctx, noWaitKey{}, true)
}

func noWaitOf(ctx context.Context) bool {
	on, _ := ctx.Value(noWaitKey{}).(bool)
	return on
}

// briefWaited runs an Action (on its lanes) and, when it is refused for a
// reason that may pass on its own and sent nothing, waits outside the
// lanes for that to change and runs it again, until it is not refused so
// or the wait ends.
func (s Services) briefWaited(name string, cmd Command) Command {
	if name == "batch" {
		return cmd // its steps run inside the Batch's lanes
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		out, err := cmd(ctx, args)
		r := briefRefusal(err)
		if r == nil || noWaitOf(ctx) {
			return out, err
		}
		want := briefWaitLimit
		if dl, ok := ctx.Deadline(); ok {
			want = min(want, time.Until(dl)/2) // leave the Action its time
		}
		if want < briefWaitPoll {
			return out, err
		}
		start := s.Now()
		// spent is the wait so far: the pauses, or the clock when the polls
		// and tries took longer. A pause never runs past the wait.
		var slept time.Duration
		spent := func() time.Duration { return max(slept, s.Now().Sub(start)) }
		sleep := func(d time.Duration) {
			if d = min(d, want-spent()); d > 0 {
				slept += d
				s.Sleep(d)
			}
		}
		for {
			end, ok := s.waitOut(ctx, *r, want-spent(), sleep)
			switch {
			case !ok:
				return out, err // the poll cannot tell: the refusal as it was
			case end == waitStopped:
				return "", escStopBeforeActError()
			case end == waitTimedOut:
				return out, waitedRefusal(err, *r, s.Now().Sub(start))
			}
			out, err = cmd(ctx, args)
			next := briefRefusal(err)
			if next == nil {
				if err == nil {
					out = withNotes(out, fmt.Sprintf("%swaited %s for %s\n", notePrefix, seconds(s.Now().Sub(start)), r.what()))
				}
				return out, err
			}
			r = next
			if spent() >= want || ctx.Err() != nil {
				return out, waitedRefusal(err, *r, s.Now().Sub(start))
			}
			sleep(r.interval()) // refused again: never two tries back to back
		}
	}
}

// waitOut polls, outside every lane, until the refusal's reason is gone
// (waitHeld), the wait runs out, or the user presses Esc. ok is false
// when the poll cannot tell: no probe for it, or a read that failed.
func (s Services) waitOut(ctx context.Context, r refusalWait, left time.Duration, sleep func(time.Duration)) (end waitEnd, ok bool) {
	if left <= 0 {
		return waitTimedOut, true
	}
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()
	wctx, cancel := context.WithTimeout(ctx, left)
	defer cancel()
	// The polls' UIA calls end with the wait without abandoning the UIA
	// worker, as a desktop wait's do.
	wctx = uia.WithWaitPoll(wctx)
	var e *Error
	switch r.kind {
	case waitEnable:
		holder, has := s.Actions.(ElementHolder)
		if !has {
			return 0, false
		}
		held, err := holder.HoldElement(wctx, r.window.ID, r.index)
		if err != nil {
			return 0, false
		}
		defer held.Release()
		_, end, e = pollUntil(wctx, sleep, left, r.interval(), esc.Stopped, func() (struct{}, bool, *Error) {
			st, err := held.State(wctx)
			switch {
			case err == nil:
				return struct{}{}, st.Enabled, nil
			case wctx.Err() != nil, errors.Is(err, uia.ErrWorkerGone):
				return struct{}{}, false, nil // the wait's end, or a replaced worker: the next poll says
			}
			return struct{}{}, false, Fail(err.Error(), "") // gone or unreadable: the try says
		})
	case waitDialog:
		check := s.Ready
		if check == nil {
			check = win.HeadlessReady
		}
		_, end, e = pollUntil(wctx, sleep, left, r.interval(), esc.Stopped, func() (struct{}, bool, *Error) {
			var modal *win.ModalDialogError
			return struct{}{}, !errors.As(check(r.window.ID), &modal), nil
		})
	case waitAppear:
		peeker, has := s.Trees.(TreePeeker)
		if !has {
			return 0, false
		}
		w := r.window
		_, end, e = pollUntil(wctx, sleep, left, r.interval(), esc.Stopped, func() (struct{}, bool, *Error) {
			tree, err := peeker.PeekTree(wctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
			switch {
			case err == nil:
				_, rerr := wstate.ResolveElementRef(tree.Elements, r.ref)
				var ambiguous *wstate.AmbiguousRefError
				return struct{}{}, rerr == nil || errors.As(rerr, &ambiguous), nil // several: the try lists them
			case wctx.Err() != nil, errors.Is(err, uia.ErrWorkerGone):
				return struct{}{}, false, nil
			}
			return struct{}{}, false, Fail(err.Error(), "") // the Window closed or cannot be read: the try says
		})
	}
	if e != nil {
		return 0, false
	}
	return end, true
}

// waitedRefusal is the refusal after a wait that ran out: the same
// failure, whose hint now says how long winuse waited.
func waitedRefusal(e *Error, r refusalWait, waited time.Duration) *Error {
	out := *e
	more := ""
	if r.kind == waitEnable {
		more = "; to wait longer: winuse wait " + strconv.FormatUint(r.window.ID, 10) + " " + strconv.Itoa(r.index) + " enabled --timeout <seconds>"
	}
	note := fmt.Sprintf("winuse waited %s for %s%s", seconds(waited), r.what(), more)
	if out.Hint == "" {
		out.Hint = note
	} else {
		out.Hint += " (" + note + ")"
	}
	return &out
}

// seconds renders a wait as the note prints it: 0.4 s.
func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + " s"
}
