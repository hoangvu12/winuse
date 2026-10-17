package helper

// The Esc-stop tests (ticket 12): the physical Escape key aborts the
// in-progress Action or Batch, remaining Batch steps do not run, and the
// stopped command reports exit 130 with the message that says the user
// stopped winuse. The watcher is faked (a latch the tests press at chosen
// moments); the real hook and its swallowing live in internal/win and are
// exercised only by the user-run e2e suite.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/apps"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// escFake is a fake EscWatch: press latches a stop for the armed scope,
// exactly like the real latch, so tests can press the key at a chosen
// moment (inside the activation, during typing, between steps).
type escFake struct {
	mu      sync.Mutex
	armed   int
	pressed bool
}

func (e *escFake) Arm() {
	e.mu.Lock()
	e.armed++
	e.mu.Unlock()
}

func (e *escFake) Disarm() {
	e.mu.Lock()
	if e.armed > 0 {
		e.armed--
		if e.armed == 0 {
			e.pressed = false // the outermost scope clears the latch
		}
	}
	e.mu.Unlock()
}

func (e *escFake) Stopped() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pressed
}

// press latches the user's Esc press.
func (e *escFake) press() {
	e.mu.Lock()
	e.pressed = true
	e.mu.Unlock()
}

// escSpy builds an actionSpy whose Services carry esc, with hooks to press
// the key at chosen moments: during the activation, or right after the
// click injection (the act step's tail).
type escSpy struct {
	*actionSpy
	esc     *escFake
	svc     Services
	pressOn string // "activate" or "click": the step that presses Esc
}

func newEscSpy(pressOn string) *escSpy {
	e := &escFake{}
	spy := &actionSpy{point: uia.Point{X: 100, Y: 200}}
	s := &escSpy{actionSpy: spy, esc: e}
	s.pressOn = pressOn
	svc := spy.services()
	svc.Esc = e
	svc.Activate = func(hwnd uint64) error {
		spy.step("activate")
		if s.pressOn == "activate" {
			e.press()
		}
		return spy.activateErr
	}
	svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		spy.step("click")
		spy.mu.Lock()
		spy.clickTarget, spy.clickX, spy.clickY, spy.clickButton, spy.clickCount = target, x, y, button, count
		err := spy.clickErr
		spy.mu.Unlock()
		if s.pressOn == "click" {
			e.press() // the press lands mid-injection, like a real one would
		}
		return err
	}
	s.svc = svc
	return s
}

func TestEscDuringActivationStopsBeforeAnyInput(t *testing.T) {
	spy := newEscSpy("activate")
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil {
		t.Fatal("a press during the activation must stop the click")
	}
	if cmdErr.Msg != escStopMessage {
		t.Errorf("message = %q, want %q", cmdErr.Msg, escStopMessage)
	}
	if cmdErr.Exit != protocol.ExitEsc {
		t.Errorf("exit = %d, want %d (Codex's interrupt code)", cmdErr.Exit, protocol.ExitEsc)
	}
	if !strings.Contains(cmdErr.Hint, "ask the user") {
		t.Errorf("hint = %q, want it to tell the agent to stop and ask", cmdErr.Hint)
	}
	// The activation ran but no input was injected.
	if got := spy.recorded(); !equalStrings(got, []string{"locked", "integrity", "activate"}) {
		t.Errorf("pipeline steps = %v, want no click after the stop", got)
	}
}

func TestEscDuringTheActStepStopsDespiteInputLanding(t *testing.T) {
	// A press that lands while the input is being injected (the click
	// succeeded) still stops the command: the user's signal outranks the
	// step's own outcome, and the agent must re-observe before acting.
	spy := newEscSpy("click")
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc {
		t.Fatalf("a press during the click must stop it: %v", cmdErr)
	}
	if cmdErr.Msg != escStopMessage {
		t.Errorf("message = %q, want %q", cmdErr.Msg, escStopMessage)
	}
	if got := spy.recorded(); !equalStrings(got, []string{"locked", "integrity", "activate", "point", "click"}) {
		t.Errorf("pipeline steps = %v, want the click to have run (its outcome is unknown)", got)
	}
}

func TestEscStopDoesNotLeakIntoTheNextCommand(t *testing.T) {
	// The latch clears when the outermost scope ends: the command after a
	// stopped one starts clean (there is no turn concept — Codex's
	// per-turn latch is winuse's per-command latch).
	spy := newEscSpy("activate")
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc {
		t.Fatalf("the first click must stop: %v", cmdErr)
	}
	spy.pressOn = "" // no press this time
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr != nil {
		t.Fatalf("the next command must run clean: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
}

func TestTypeTextAbortBecomesTheEscStop(t *testing.T) {
	// TypeText aborts between its batches (win.ErrEscStopped); the helper
	// maps that to the exit-130 stop with the user-stopped-winuse message.
	e := &escFake{}
	spy := &actionSpy{}
	svc := spy.services()
	svc.Esc = e
	svc.TypeText = func(text string, stopped func() bool) error {
		spy.step("type")
		if stopped == nil {
			t.Error("type-text must receive the Esc-stop checkpoint")
		}
		return win.ErrEscStopped
	}
	_, cmdErr := runCommand(t, CommandsWith(svc), "type-text", protocol.TypeTextArgs{Window: "101", Text: "hello"})
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc {
		t.Fatalf("an aborted typing must report the stop: %v", cmdErr)
	}
	if cmdErr.Msg != escStopMessage {
		t.Errorf("message = %q, want %q", cmdErr.Msg, escStopMessage)
	}
}

func TestNoEscWatchStillRunsCommands(t *testing.T) {
	// Services without an Esc watch (offline tests, a helper whose hook
	// could not start) install the no-op: commands run, nothing stops.
	spy := &actionSpy{point: uia.Point{X: 100, Y: 200}}
	svc := spy.services()
	if svc.Esc == nil {
		svc.Esc = noopEscWatch{} // CommandsWith would; tests that build Services directly mirror it
	}
	out, cmdErr := runCommand(t, CommandsWith(svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr != nil {
		t.Fatalf("click: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	// A default-built registry (CommandsWith(Services{})) carries the noop.
	cmds := CommandsWith(testServices(&fakeTrees{}))
	if cmds["batch"] == nil || cmds["launch-app"] == nil {
		t.Fatal("the registry must wire batch and launch-app")
	}
}

func TestEscStopsTheBatchInTheFailedStep(t *testing.T) {
	// A press during a step aborts it; the Batch names the stopped step,
	// keeps exit 130 and the stop's message and hint, and the remaining
	// steps do not run.
	e := &escFake{}
	spy := newBatchSpy()
	spy.svc.Esc = e
	spy.point = uia.Point{X: 100, Y: 200}
	spy.svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		spy.step("click")
		spy.mu.Lock()
		spy.clickTarget, spy.clickX, spy.clickY = target, x, y
		spy.mu.Unlock()
		e.press() // the press lands mid-click
		return nil
	}
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(1)}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "must not appear"}),
		},
	})
	if cmdErr == nil {
		t.Fatal("a stopped step must stop the Batch")
	}
	if cmdErr.Exit != protocol.ExitEsc {
		t.Errorf("exit = %d, want %d", cmdErr.Exit, protocol.ExitEsc)
	}
	want := "batch stopped at step 1 (click): " + escStopMessage
	if cmdErr.Msg != want {
		t.Errorf("message = %q, want %q", cmdErr.Msg, want)
	}
	if !strings.Contains(cmdErr.Hint, "ask the user") {
		t.Errorf("hint = %q, want the stop's recovery hint", cmdErr.Hint)
	}
	if typed := spy.typedTexts(); len(typed) != 0 {
		t.Errorf("remaining steps must not run; typed %q", typed)
	}
}

func TestEscBetweenStepsStopsTheBatch(t *testing.T) {
	// A press in the gap after a step's checks (before the next step's
	// own checks would see it) is caught at the top of the run loop: the
	// next step never runs. The hand-built registry presses on behalf of
	// a step that already finished all its own checks.
	e := &escFake{}
	spy := newBatchSpy()
	spy.svc.Esc = e
	spy.point = uia.Point{X: 100, Y: 200}
	cmds := map[string]Command{
		"click": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			return okOutput, nil
		},
		"type-text": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			return okOutput, nil
		},
	}
	batchCmd := batch(cmds, e)
	raw, err := json.Marshal(protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(1)}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "second"}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Press before the batch runs its loop: the latch is exactly what a
	// press in the inter-step gap leaves behind (step 1 finished all its
	// checks; the loop top is the next checkpoint).
	e.press()
	_, cmdErr := batchCmd(context.Background(), raw)
	if cmdErr == nil {
		t.Fatal("a press between steps must stop the Batch")
	}
	if cmdErr.Exit != protocol.ExitEsc || cmdErr.Msg != escStopMessage {
		t.Errorf("stop = %q (exit %d), want the plain stop with exit %d", cmdErr.Msg, cmdErr.Exit, protocol.ExitEsc)
	}
}

func TestEscInTheLastStepsTailStopsTheBatch(t *testing.T) {
	// A press in the last step's tail (after every step-level check) still
	// surfaces: the post-loop check replaces the Batch's success with the
	// stop — the user's Esc was swallowed, so `ok` would be a lie.
	e := &escFake{}
	cmds := map[string]Command{
		"activate-window": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			e.press() // the press lands after the step's own checks
			return okOutput, nil
		},
	}
	batchCmd := batch(cmds, e)
	raw, err := json.Marshal(protocol.BatchArgs{
		Window: "101",
		Steps:  []protocol.Step{stepOf("activate-window", nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, cmdErr := batchCmd(context.Background(), raw)
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc || cmdErr.Msg != escStopMessage {
		t.Fatalf("a press in the last step's tail must stop the Batch: %v", cmdErr)
	}
}

func TestBatchExitCodeForwardingStillPasses(t *testing.T) {
	// The pre-existing contract (ticket 11): a step's exit code passes
	// through the Batch wrapper. Driving it through the real registry with
	// a stopped step keeps 130 end to end.
	e := &escFake{}
	spy := newBatchSpy()
	spy.svc.Esc = e
	spy.svc.TypeText = func(text string, stopped func() bool) error {
		e.press()
		return win.ErrEscStopped
	}
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello"}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "Return"}), // must not run
		},
	})
	if cmdErr == nil {
		t.Fatal("the stopped step must fail the Batch")
	}
	if cmdErr.Exit != protocol.ExitEsc {
		t.Errorf("exit = %d, want %d", cmdErr.Exit, protocol.ExitEsc)
	}
	if pressed := spy.pressedKeys(); len(pressed) != 0 {
		t.Errorf("remaining steps must not run; pressed %q", pressed)
	}
}

func TestLaunchErrorMapsTheStoppedWait(t *testing.T) {
	// launch-app's wait failures: the Esc stop keeps exit 130 and its
	// message; the never-appeared Window keeps Codex's message and hint.
	cmdErr := launchError(errors.Join(apps.ErrStopped))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc || cmdErr.Msg != escStopMessage {
		t.Fatalf("stopped wait: %v (exit %d), want the exit-130 stop", cmdErr, cmdErr.Exit)
	}
	cmdErr = launchError(&apps.NoWindowError{App: "Some App"})
	if cmdErr == nil || cmdErr.Exit != 1 {
		t.Fatalf("no window: %v, want exit 1", cmdErr)
	}
	if cmdErr.Msg != "launched app did not expose a targetable window: Some App" {
		t.Errorf("message = %q", cmdErr.Msg)
	}
	if !strings.Contains(cmdErr.Hint, "list-windows") {
		t.Errorf("hint = %q, want the list-windows hint", cmdErr.Hint)
	}
}

func TestServerEscStopOutranksTheTimeout(t *testing.T) {
	// A stop that races the request budget keeps its exit 130 and message
	// instead of the generic timeout the user did not cause.
	s := New("0.1.0", time.Hour, time.Millisecond, map[string]Command{
		"esc-late": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			time.Sleep(10 * time.Millisecond) // past the budget
			return "", escStopError()
		},
	}, discardLogger())
	resp := s.handle(&protocol.Request{V: protocol.Version, Ver: "0.1.0", Cmd: "esc-late"})
	if resp.OK {
		t.Fatal("the stopped command must fail")
	}
	if resp.Exit != protocol.ExitEsc {
		t.Errorf("exit = %d, want %d", resp.Exit, protocol.ExitEsc)
	}
	if resp.Err != escStopMessage {
		t.Errorf("error = %q, want the Esc stop message, not the timeout", resp.Err)
	}
}
