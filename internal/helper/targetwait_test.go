package helper

// Waiting briefly before refusing (learn-from 02, targetwait.go): a
// disabled native Element and a name:/id: reference that matches nothing
// yet are tried again for up to 2 s, a disabled Page Element is checked
// and aimed at again, and everything else refuses at once. The clock is
// faked: Sleep records the pauses and never sleeps.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// waitSpy is the headless spy whose click is refused as disabled for its
// first disabledFor tries, and whose live resolver finds name:Save only
// after missFor fresh reads; its Sleep records the pauses.
type waitSpy struct {
	*headlessSpy
	disabledFor int
	clicks      int
	missFor     int
	reads       int
	sleeps      []time.Duration
	onSleep     func(n int) // after the nth pause
}

func newWaitSpy() *waitSpy {
	w := &waitSpy{headlessSpy: newHeadlessSpy()}
	w.svc.Actions = w
	w.svc.Sleep = func(d time.Duration) {
		w.sleeps = append(w.sleeps, d)
		if w.onSleep != nil {
			w.onSleep(len(w.sleeps))
		}
	}
	return w
}

func (w *waitSpy) HeadlessClick(ctx context.Context, hwnd uint64, index int) (string, error) {
	w.step("headless-click")
	w.clicks++
	w.hIndex = index
	if w.clicks <= w.disabledFor {
		return "", uia.ErrDisabled
	}
	return "Invoke", w.hClickErr
}

func (w *waitSpy) ResolveElementRefLive(ctx context.Context, hwnd uint64, ref string, budget time.Duration) (uia.LiveResolution, error) {
	w.reads++
	if w.reads <= w.missFor {
		return uia.LiveResolution{}, &uia.LiveRefError{Err: &wstate.NoMatchRefError{Kind: wstate.RefName, Text: "Save"}}
	}
	return uia.LiveResolution{Index: 245, Fresh: true}, nil
}

func (w *waitSpy) slept() time.Duration {
	var total time.Duration
	for _, d := range w.sleeps {
		total += d
	}
	return total
}

func TestDisabledElementIsTriedAgainUntilEnabled(t *testing.T) {
	w := newWaitSpy()
	w.disabledFor = 3
	out, e := runCommand(t, CommandsWith(w.svc), "click", clickArgs("101", 2, 1, "left"))
	if e != nil || out != okOutput {
		t.Fatalf("click: %q, %+v", out, e)
	}
	if w.clicks != 4 || w.hIndex != 2 || len(w.sleeps) != 3 || w.sleeps[0] != targetPoll {
		t.Errorf("tries %d on %d, pauses %v", w.clicks, w.hIndex, w.sleeps)
	}
	// The cursor glides once and ripples once, on the delivered click.
	if len(w.cursor.targets) != 1 || len(w.cursor.clicks) != 1 {
		t.Errorf("cursor travels %d, ripples %d", len(w.cursor.targets), len(w.cursor.clicks))
	}
	assertNoV1(t, w.recorded())
}

func TestDisabledElementRefusesAfterTheWait(t *testing.T) {
	w := newWaitSpy()
	w.disabledFor = 1000
	_, e := runCommand(t, CommandsWith(w.svc), "click", clickArgs("101", 2, 1, "left"))
	if e == nil || e.Msg != "the Element is disabled, still after waiting 2s; nothing was sent" || !strings.Contains(e.Hint, "disabled") {
		t.Fatalf("refusal: %+v", e)
	}
	if w.slept() != targetWait || w.clicks != 1+int(targetWait/targetPoll) {
		t.Errorf("waited %s over %d tries", w.slept(), w.clicks)
	}
	if containsStep(w.recorded(), "cursor-click") {
		t.Error("the cursor rippled on a refusal")
	}
	// The Delivery the refusal reports is still not delivered.
	if d, _ := protocol.DeliveryOf(e.Msg, e.Hint); d != protocol.DeliveryNotDelivered {
		t.Errorf("delivery %q", d)
	}

	// TargetWait below zero: the refusal at once, as before.
	w = newWaitSpy()
	w.disabledFor = 1000
	w.svc.TargetWait = -1
	_, e = runCommand(t, CommandsWith(w.svc), "click", clickArgs("101", 2, 1, "left"))
	if e == nil || e.Msg != "the Element is disabled; nothing was sent" || w.clicks != 1 || len(w.sleeps) != 0 {
		t.Errorf("no wait: %+v after %d tries, %v", e, w.clicks, w.sleeps)
	}

	// A shorter TargetWait bounds the tries.
	w = newWaitSpy()
	w.disabledFor = 1000
	w.svc.TargetWait = 500 * time.Millisecond
	_, e = runCommand(t, CommandsWith(w.svc), "click", clickArgs("101", 2, 1, "left"))
	if e == nil || !strings.Contains(e.Msg, "still after waiting 500ms") || w.slept() != 500*time.Millisecond {
		t.Errorf("500 ms: %+v, waited %s", e, w.slept())
	}
}

func TestEscStopsTheWait(t *testing.T) {
	w := newWaitSpy()
	w.disabledFor = 1000
	w.esc.pressOn = "sleep"
	w.onSleep = func(n int) {
		if n == 2 {
			w.esc.pressIf("sleep")
		}
	}
	_, e := runCommand(t, CommandsWith(w.svc), "click", clickArgs("101", 2, 1, "left"))
	if e == nil || e.Exit != protocol.ExitEsc || e.Msg != escStopMessage || !strings.Contains(e.Hint, "nothing was sent") {
		t.Fatalf("stop: %+v", e)
	}
	if w.clicks != 2 {
		t.Errorf("tried %d times, want none after the press", w.clicks)
	}

	// The same during a reference's wait.
	w = newWaitSpy()
	w.missFor = 1000
	w.esc.pressOn = "sleep"
	w.onSleep = func(n int) { w.esc.pressIf("sleep") }
	_, e = runCommand(t, CommandsWith(w.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Save"})
	if e == nil || e.Exit != protocol.ExitEsc || e.Msg != escStopMessage || w.reads != 1 || w.clicks != 0 {
		t.Errorf("stop during a reference: %+v after %d reads, %d clicks", e, w.reads, w.clicks)
	}
}

func TestPendingReferenceIsReadAgainUntilFound(t *testing.T) {
	w := newWaitSpy()
	w.missFor = 2
	out, e := runCommand(t, CommandsWith(w.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Save"})
	want := "ok\nNote: found name:Save in a fresh read of window 101, as Element Index 245; the Window State you have is unchanged\n"
	if e != nil || out != want || w.hIndex != 245 {
		t.Fatalf("click: %q, %+v on %d", out, e, w.hIndex)
	}
	if w.reads != 3 || len(w.sleeps) != 2 {
		t.Errorf("%d fresh reads, pauses %v", w.reads, w.sleeps)
	}

	// Never found: the fresh read's refusal, with the wait.
	w = newWaitSpy()
	w.missFor = 1000
	_, e = runCommand(t, CommandsWith(w.svc), "set-value", protocol.SetValueArgs{Window: "101", Ref: "name:Save", Value: "x"})
	if e == nil || e.Msg != `no element named "Save" in window 101, in its cached Window State or a fresh read, still after waiting 2s; nothing was sent` {
		t.Fatalf("never found: %+v", e)
	}
	if containsStep(w.recorded(), "headless-set-value") {
		t.Error("a reference that never resolved acted")
	}
}

func TestImmediateRefusalsDoNotWait(t *testing.T) {
	cases := []struct {
		name  string
		setup func(w *waitSpy)
		args  any
	}{
		{"stale index", func(w *waitSpy) { w.hClickErr = fmt.Errorf("%w: element 2 is gone", uia.ErrStale) }, clickArgs("101", 2, 1, "left")},
		{"no click route", func(w *waitSpy) { w.hClickErr = uia.ErrNoClickRoute }, clickArgs("101", 2, 1, "left")},
		{"a write that may have taken effect", func(w *waitSpy) { w.hClickErr = &uia.WriteError{What: "invoking the Element", HR: -0x7fffbffb} }, clickArgs("101", 2, 1, "left")},
	}
	for _, c := range cases {
		w := newWaitSpy()
		c.setup(w)
		if _, e := runCommand(t, CommandsWith(w.svc), "click", c.args); e == nil || strings.Contains(e.Msg, "after waiting") {
			t.Errorf("%s: %+v", c.name, e)
		}
		if len(w.sleeps) != 0 || w.clicks != 1 {
			t.Errorf("%s: %d tries, pauses %v", c.name, w.clicks, w.sleeps)
		}
	}

	// An ambiguous reference is never a guess, and never waited on.
	l := newLiveSpy()
	var sleeps int
	l.svc.Sleep = func(time.Duration) { sleeps++ }
	l.err = &uia.LiveRefError{Err: &wstate.AmbiguousRefError{Kind: wstate.RefName, Text: "Twin", Matches: []int{12, 13}, Candidates: []string{"12 button Twin", "13 button Twin"}}}
	if _, e := runCommand(t, CommandsWith(l.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Twin"}); e == nil || sleeps != 0 || len(l.refs) != 1 {
		t.Errorf("ambiguous: %+v, %d pauses, %d reads", e, sleeps, len(l.refs))
	}

	// A helper with the cache alone (no fresh read) has nothing to wait for.
	spy := newHeadlessSpy()
	spy.svc.Sleep = func(time.Duration) { sleeps++ }
	spy.refErr = &wstate.NoMatchRefError{Kind: wstate.RefName, Text: "Save"}
	if _, e := runCommand(t, CommandsWith(spy.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Save"}); e == nil || sleeps != 0 {
		t.Errorf("cache alone: %+v, %d pauses", e, sleeps)
	}
}

func TestWaitLeavesTheRequestItsMargin(t *testing.T) {
	s := Services{Sleep: func(time.Duration) { t.Fatal("slept with no time left") }}
	ctx, cancel := context.WithTimeout(context.Background(), targetWaitMargin)
	defer cancel()
	tries := 0
	waited, stop := s.waitActionable(ctx, func() bool { return false }, func() bool { tries++; return true })
	if waited != 0 || stop || tries != 0 {
		t.Errorf("waited %s, stop %v, %d tries", waited, stop, tries)
	}

	// A wall clock past the limit ends it too, whatever was slept.
	now := time.Now()
	s = Services{Sleep: func(time.Duration) {}, Now: func() time.Time { now = now.Add(time.Second); return now }}
	waited, _ = s.waitActionable(context.Background(), func() bool { return false }, func() bool { tries++; return true })
	if tries != 2 || waited != 2*targetPoll {
		t.Errorf("by the clock: %d tries, counted %s", tries, waited)
	}
}

func TestWithWaited(t *testing.T) {
	e := withWaited(Fail("the Element is disabled; nothing was sent", "h"), 1800*time.Millisecond)
	if e.Msg != "the Element is disabled, still after waiting 1.8s; nothing was sent" || e.Hint != "h" {
		t.Errorf("%+v", e)
	}
	if e := withWaited(Fail("no element named \"x\"", ""), 2*time.Second); e.Msg != `no element named "x", still after waiting 2s` {
		t.Errorf("%+v", e)
	}
	orig := Fail("x; nothing was sent", "")
	if withWaited(orig, 0) != orig || withWaited(nil, time.Second) != nil {
		t.Error("no wait must leave the refusal alone")
	}
}

func TestDisabledPageElementIsAimedAgainUntilEnabled(t *testing.T) {
	r := newPageRig(t)
	var pauses int
	r.svc.Sleep = func(d time.Duration) {
		if d != targetPoll {
			return
		}
		pauses++
		if pauses == 2 {
			r.set(func() { r.aim = map[string]any{} })
		}
	}
	r.set(func() { r.aim = map[string]any{"state": "disabled"} })
	out, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e != nil || out != okOutput {
		t.Fatalf("click: %q, %+v", out, e)
	}
	if pauses != 2 || len(r.sent()) == 0 {
		t.Errorf("%d pauses, sent %v", pauses, r.sent())
	}

	// A disabled field for set-value: its check reads it again.
	r = newPageRig(t)
	pauses = 0
	r.svc.Sleep = func(d time.Duration) {
		if d != targetPoll {
			return
		}
		pauses++
		if pauses == 3 {
			r.set(func() { r.field = map[string]any{"kind": "text"} })
		}
	}
	r.set(func() { r.field = map[string]any{"kind": "text", "disabled": true} })
	if out, e := r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "a@b.c"}); e != nil || out != okOutput || pauses != 3 {
		t.Errorf("set-value: %q, %+v after %d pauses", out, e, pauses)
	}

	// Covered is not waited on: the click point's re-centre (page.Aim) is
	// its one retry.
	r = newPageRig(t)
	pauses = 0
	r.svc.Sleep = func(d time.Duration) { pauses++ }
	r.set(func() { r.aim = map[string]any{"state": "covered", "by": "<div>"} })
	if _, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}); e == nil || strings.Contains(e.Msg, "after waiting") || pauses != 0 {
		t.Errorf("covered: %+v after %d pauses", e, pauses)
	}
}
