package helper

// Esc stops every agent (multi-agent 05, ADR 0014 Decision 5): one press
// stops the running Action and every request waiting for its turn, each
// waiting one with "nothing was sent" and exit 130, and a request that
// arrives after the press runs. The Esc service is a fake with the
// waiting side; no hook is installed.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// genEsc is an Esc service with the waiting side: a press latches for the
// running scope and advances the stop generation.
type genEsc struct {
	armed   atomic.Int32
	pressed atomic.Bool
	gen     atomic.Uint64
	waiting atomic.Int32
}

func (e *genEsc) Arm() { e.armed.Add(1) }
func (e *genEsc) Disarm() {
	if e.armed.Add(-1) == 0 {
		e.pressed.Store(false)
	}
}
func (e *genEsc) Stopped() bool              { return e.pressed.Load() }
func (e *genEsc) BeginWait() uint64          { e.waiting.Add(1); return e.gen.Load() }
func (e *genEsc) EndWait()                   { e.waiting.Add(-1) }
func (e *genEsc) StoppedSince(g uint64) bool { return e.gen.Load() != g }
func (e *genEsc) press()                     { e.pressed.Store(e.armed.Load() > 0); e.gen.Add(1) }
func (e *genEsc) waitingFor(n int32, t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for e.waiting.Load() != n {
		if time.Now().After(deadline) {
			t.Fatalf("%d requests waiting, want %d", e.waiting.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEscStopsRunningAndWaitingRequests(t *testing.T) {
	esc := &genEsc{}
	s := Services{actionGate: make(chan struct{}, 1), Esc: esc}

	started, release := make(chan struct{}), make(chan struct{})
	running := s.serialized(func(ctx context.Context, args json.RawMessage) (string, *Error) {
		esc.Arm()
		defer esc.Disarm()
		close(started)
		<-release
		if esc.Stopped() {
			return "", escStopError()
		}
		return okOutput, nil
	})
	var ran atomic.Int32
	waiting := s.serialized(func(ctx context.Context, args json.RawMessage) (string, *Error) {
		ran.Add(1)
		return okOutput, nil
	})

	type result struct {
		out string
		err *Error
	}
	results := make(chan result, 3)
	go func() { out, err := running(context.Background(), nil); results <- result{out, err} }()
	<-started
	esc.waitingFor(0, t) // the running request is no longer waiting
	var wg sync.WaitGroup
	waited := make(chan result, 2)
	for range 2 { // two other agents' requests, waiting their turn
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := waiting(context.Background(), nil)
			waited <- result{out, err}
		}()
	}
	esc.waitingFor(2, t)

	esc.press()
	wg.Wait()
	close(waited)
	for r := range waited {
		if r.err == nil || r.err.Exit != protocol.ExitEsc || !strings.Contains(r.err.Hint, "nothing was sent") {
			t.Errorf("a waiting request: %q, %+v; want the stop with nothing sent", r.out, r.err)
		}
	}
	if ran.Load() != 0 {
		t.Errorf("%d stopped requests ran", ran.Load())
	}
	close(release)
	if r := <-results; r.err == nil || r.err.Exit != protocol.ExitEsc {
		t.Errorf("the running request: %q, %+v; want the stop", r.out, r.err)
	}

	// After the stop, a new request runs: Esc is not a lock.
	if out, err := waiting(context.Background(), nil); err != nil || out != okOutput || ran.Load() != 1 {
		t.Errorf("a request after the stop: %q, %+v", out, err)
	}
	if esc.waiting.Load() != 0 {
		t.Errorf("%d waiting scopes left open", esc.waiting.Load())
	}
}

// Without the waiting side (the fakes, the no-op), a waiting request keeps
// today's behaviour: it waits, and its budget can run out.
func TestWaitingWithoutTheWaitingSide(t *testing.T) {
	s := Services{actionGate: make(chan struct{}, 1), Esc: &scopeEsc{}}
	s.actionGate <- struct{}{} // another Action holds the turn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := s.serialized(func(context.Context, json.RawMessage) (string, *Error) { return okOutput, nil })(ctx, nil)
	if err == nil || !strings.Contains(err.Msg, "another winuse Action was still running") {
		t.Errorf("queued out: %+v", err)
	}
}
