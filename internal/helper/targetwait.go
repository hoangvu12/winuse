package helper

// Waiting briefly before refusing (learn-from 02, ADR 0022): an Action
// whose target is not actionable yet, for a reason that may pass on its
// own, tries again for up to targetWait before it refuses, instead of
// costing the agent a `wait` and a retry turn.
//
//   - A name:/id: reference that matches no Element, in the cached Window
//     State or a fresh read (refs.go, liverefs.go): the fresh read runs
//     again every targetPoll. An Element Index never re-resolves: it names
//     the Element of the Window State that printed it, or it is stale.
//   - A native Element whose pattern call UI Automation refused as disabled
//     (uia.ErrDisabled, raised before anything reached the App): the same
//     call on the same Element is tried again.
//   - A Page Element that is disabled (page.WhyDisabled, from its check or
//     the aim): the check and the aim run again. Neither sends input.
//
// Every other refusal is immediate, as before: an ambiguous reference, a
// stale index, a hidden Element, a Secondary Action the Element does not
// offer. The wait runs inside the Action's lanes and its budget, stops at
// the user's Esc (nothing was sent), and leaves the request's last
// targetWaitMargin to the Action. A refusal after a wait says how long it
// waited, and still says nothing was sent.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

const (
	// targetWait bounds the wait. agent-desktop waits up to 5 s; winuse's
	// Action budget is 10 s (protocol.RequestTimeout) and a fresh read of a
	// reference may take 3 s of it (liveRefBudget), so 2 s leaves the
	// Action, its Agent Cursor glide and a last fresh read inside the
	// budget, and adds at most 2 s to another Agent's ~12 s lane wait
	// (laneWaitLimit). A control that enables after a click, or a dialog's
	// Element that appears, usually takes well under a second.
	targetWait = 2 * time.Second
	// targetPoll is the pause between two tries.
	targetPoll = 200 * time.Millisecond
	// targetWaitMargin is kept between the wait and the request's
	// deadline: the Action, and a fresh read, still have to run.
	targetWaitMargin = 3 * time.Second
)

// targetWaitLimit is how long an Action waits for its target (Services.
// TargetWait: 0 is targetWait, below 0 refuses at once).
func (s Services) targetWaitLimit() time.Duration {
	switch {
	case s.TargetWait < 0:
		return 0
	case s.TargetWait == 0:
		return targetWait
	}
	return s.TargetWait
}

// escStopped reports the Esc latch, false without an Esc service.
func (s Services) escStopped() bool {
	return s.Esc != nil && s.Esc.Stopped()
}

// waitActionable runs after a first try that was refused for a transient
// reason: every targetPoll it runs try again, until try reports that the
// refusal is gone or is no longer transient (false), the wait passes, the
// request's deadline comes within targetWaitMargin, or the user presses Esc
// (stop). The wait is counted in pauses slept, as pollUntil counts it, so a
// faked Sleep runs it at once; Now bounds it in real time too. A try runs
// on the caller's goroutine: it may act, so it is never abandoned.
func (s Services) waitActionable(ctx context.Context, stopped func() bool, try func() bool) (waited time.Duration, stop bool) {
	limit := s.targetWaitLimit()
	sleep, now := s.Sleep, s.Now
	if sleep == nil {
		sleep = time.Sleep
	}
	if now == nil {
		now = time.Now
	}
	start := now()
	for waited < limit {
		if ctx.Err() != nil {
			break
		}
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) < targetWaitMargin+targetPoll {
			break
		}
		step := min(targetPoll, limit-waited)
		sleep(step)
		waited += step
		if stopped() {
			return waited, true
		}
		if !try() || now().Sub(start) >= limit {
			break
		}
	}
	return waited, false
}

// withWaited adds how long the Action waited to a refusal raised after a
// wait, before its "nothing was sent": `the Element is disabled, still
// after waiting 2s; nothing was sent`. No wait, no change.
func withWaited(e *Error, waited time.Duration) *Error {
	if e == nil || waited <= 0 {
		return e
	}
	out := *e
	note := fmt.Sprintf(", still after waiting %s", waited.Round(100*time.Millisecond))
	if base, ok := strings.CutSuffix(out.Msg, nothingSentNote); ok {
		out.Msg = base + note + nothingSentNote
	} else {
		out.Msg += note
	}
	return &out
}

// refPending reports a name:/id: reference that matched no Element, where
// trying again can find it: a fresh read is possible (a LiveResolver, and
// not a cache-only resolution). Several matches, a bad reference and
// another Agent's Window State are not pending.
func refPending(ctx context.Context, s Services, err error) bool {
	if err == nil || isCacheOnly(ctx) {
		return false
	}
	if _, ok := s.Actions.(LiveResolver); !ok {
		return false // the cache alone answers the same again
	}
	var none *wstate.NoMatchRefError
	return errors.As(err, &none)
}

// disabledBeforeCall reports a native Element whose pattern call was
// refused as disabled before anything reached the App: the same call can
// be tried again.
func disabledBeforeCall(err error) bool {
	return errors.Is(err, uia.ErrDisabled) && refusedBeforeCall(err)
}

// pageDisabled reports a Page Element refused as disabled (its check or the
// aim): nothing was sent, and the check and the aim can run again.
func pageDisabled(err error) bool {
	var na *page.NotActionableError
	return errors.As(err, &na) && na.Why == page.WhyDisabled
}

// pageAimWaiting runs a Page Action's check (when set) and its aim, and
// while they refuse the Element as disabled, runs them again for up to the
// target wait. Neither sends input.
func (s Services) pageAimWaiting(ctx context.Context, o *page.Origin, opt page.AimOptions, check func(context.Context) error, stopped func() bool) (aim page.Aim, waited time.Duration, stop bool, err error) {
	try := func() (page.Aim, error) {
		if check != nil {
			if err := check(ctx); err != nil {
				return page.Aim{}, err
			}
		}
		return o.Aim(ctx, opt)
	}
	aim, err = try()
	if pageDisabled(err) {
		waited, stop = s.waitActionable(ctx, stopped, func() bool {
			aim, err = try()
			return pageDisabled(err)
		})
	}
	return aim, waited, stop, err
}
