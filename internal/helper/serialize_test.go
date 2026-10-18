package helper

// The Action-serialization tests (finding 11): two concurrent CLI calls
// could inject input simultaneously, interleaving control of the desktop.
// Every command that activates a Window, injects input or shows the
// overlay now runs under one helper-wide action gate — one command
// controls the desktop at a time — while observation-only commands (the
// plain tree read, get-window) stay concurrent. Everything is faked; no
// desktop is touched.

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// TestActionsAreSerializedHelperWide pins the gate: the second Action
// waits for the first to finish — its input is never injected while the
// first holds the desktop — and both then succeed.
func TestActionsAreSerializedHelperWide(t *testing.T) {
	spy := &actionSpy{point: uia.Point{X: 1, Y: 2}}
	svc := spy.services()

	var clicks int32
	inClick := make(chan struct{})
	release := make(chan struct{})
	svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		if atomic.AddInt32(&clicks, 1) == 1 {
			close(inClick)
			<-release // the first Action holds the desktop
		}
		return nil
	}
	cmds := CommandsWith(svc)

	index := 1
	run := func() *Error {
		raw, err := json.Marshal(protocol.ClickArgs{Window: "101", ElementIndex: &index})
		if err != nil {
			t.Fatal(err)
		}
		_, cmdErr := cmds["click"](context.Background(), raw)
		return cmdErr
	}
	first := make(chan *Error, 1)
	go func() { first <- run() }()
	<-inClick // the first Action's input is in flight

	second := make(chan *Error, 1)
	go func() { second <- run() }()
	select {
	case err := <-second:
		t.Fatalf("the second Action ran concurrently with the first: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Still waiting for the mutex: the second Action has not injected.
	}
	if got := atomic.LoadInt32(&clicks); got != 1 {
		t.Fatalf("%d clicks were injected, want 1: input must never interleave", got)
	}

	close(release)
	for i, r := range []chan *Error{first, second} {
		select {
		case err := <-r:
			if err != nil {
				t.Fatalf("Action %d: %v", i+1, err.Error())
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Action %d never finished after the first released the desktop", i+1)
		}
	}
	if got := atomic.LoadInt32(&clicks); got != 2 {
		t.Errorf("%d clicks were injected after both Actions, want 2", got)
	}
}

// TestObservationCommandsStayConcurrent pins the other half of the
// contract: while an Action holds the mutex, an observation-only command
// (get-window) answers immediately — serialization is for the commands
// that control the desktop, not for reads.
func TestObservationCommandsStayConcurrent(t *testing.T) {
	spy := &actionSpy{point: uia.Point{X: 1, Y: 2}}
	svc := spy.services()

	inClick := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		once.Do(func() { close(inClick) })
		<-release
		return nil
	}
	cmds := CommandsWith(svc)

	index := 1
	raw, err := json.Marshal(protocol.ClickArgs{Window: "101", ElementIndex: &index})
	if err != nil {
		t.Fatal(err)
	}
	clickDone := make(chan *Error, 1)
	go func() {
		_, cmdErr := cmds["click"](context.Background(), raw)
		clickDone <- cmdErr
	}()
	<-inClick

	observed := make(chan *Error, 1)
	go func() {
		_, cmdErr := cmds["get-window"](context.Background(), json.RawMessage(`{"window":"101"}`))
		observed <- cmdErr
	}()
	select {
	case err := <-observed:
		if err != nil {
			t.Fatalf("get-window while an Action runs: %v", err.Error())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the observation command must not wait for the action mutex")
	}

	close(release)
	if err := <-clickDone; err != nil {
		t.Fatalf("click: %v", err.Error())
	}
}

// TestSerializedCommandFailsFastWhenQueuedPastItsBudget pins the wrapper's
// fail-fast: a request that waited behind another Action until its own
// budget expired refuses to act late (the server reports the timeout
// centrally).
func TestSerializedCommandFailsFastWhenQueuedPastItsBudget(t *testing.T) {
	spy := &actionSpy{point: uia.Point{X: 1, Y: 2}}
	svc := spy.services()

	inClick := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		once.Do(func() { close(inClick) })
		<-release
		return nil
	}
	cmds := CommandsWith(svc)

	index := 1
	raw, err := json.Marshal(protocol.ClickArgs{Window: "101", ElementIndex: &index})
	if err != nil {
		t.Fatal(err)
	}
	go func() { cmds["click"](context.Background(), raw) }()
	<-inClick

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // queued behind the running Action, its budget is spent
	_, cmdErr := cmds["click"](ctx, raw)
	if cmdErr == nil {
		t.Fatal("a request queued past its budget must fail instead of acting late")
	}
	if cmdErr.Msg != "another winuse Action was still running when this request's budget expired" {
		t.Fatalf("message = %q, want the queued-past-budget failure", cmdErr.Msg)
	}
	if containsStep(spy.recorded(), "click") {
		t.Error("the late request must not inject input")
	}
	close(release)
}
