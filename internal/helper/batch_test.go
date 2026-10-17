package helper

// The Batch command's offline tests (ticket 11): the output contract
// (`ok`, or a one-line failure with a hint and non-zero exit), steps
// running in order and stopping at the first failure with the failed
// step named, malformed steps refused before any input is sent, the
// Batch's Window injected into every step (overriding any window the
// step carries), and the batchable-Action allowlist. Everything Win32
// and element-side is faked; no desktop is touched.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// batchSpy wires one Services over which every batchable Action runs:
// the click pipeline's spy (point, click, reference resolution), the
// ticket-07 actor (set-value, secondary actions) and the keyboard steps
// (type-text, press-key), all recording into one ordered step log.
type batchSpy struct {
	*actorSpy

	svc Services // the batch tests' Services: every input side faked

	mu      sync.Mutex
	typed   []string
	pressed []string

	typeErr  error // type-text's outcome
	pressErr error // press-key's outcome
}

func newBatchSpy() *batchSpy {
	base := newActorSpy()
	s := &batchSpy{actorSpy: base}
	svc := base.svc // shares the spy's Locked/Integrity/Activate
	// Every input-side service must stay faked (the defaults are the real
	// Win32 implementations; offline tests never inject input).
	svc.Actions = s // ElementPoint/SetValue/... resolve through the embeds
	svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		s.step("click")
		base.mu.Lock()
		base.clickTarget, base.clickX, base.clickY, base.clickButton, base.clickCount = target, x, y, button, count
		err := base.clickErr
		base.mu.Unlock()
		return err
	}
	svc.TypeText = func(text string, stopped func() bool) error {
		s.step("type-text")
		s.mu.Lock()
		s.typed = append(s.typed, text)
		err := s.typeErr
		if err == nil && stopped != nil && stopped() {
			// The real typing aborts between batches; the fake honors the
			// checkpoint the same way.
			err = win.ErrEscStopped
		}
		s.mu.Unlock()
		return err
	}
	svc.PressKeys = func(chord keys.Chord) error {
		s.step("press-key")
		s.mu.Lock()
		s.pressed = append(s.pressed, chord.String())
		err := s.pressErr
		s.mu.Unlock()
		return err
	}
	s.svc = svc
	return s
}

// typedTexts returns the recorded type-text calls.
func (s *batchSpy) typedTexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.typed...)
}

// pressedKeys returns the recorded press-key calls.
func (s *batchSpy) pressedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.pressed...)
}

// batchStepArgs builds a Step's wire arguments from any value.
func batchStepArgs(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// stepOf builds one protocol.Step.
func stepOf(kind string, args any) protocol.Step {
	if args == nil {
		return protocol.Step{Kind: kind}
	}
	return protocol.Step{Kind: kind, Args: batchStepArgs(args)}
}

func TestBatchRunsStepsInOrderAndPrintsOK(t *testing.T) {
	spy := newBatchSpy()
	spy.refIndex = 2 // click name:Click Me resolves to index 2
	spy.point = uia.Point{X: 100, Y: 200}
	cmds := CommandsWith(spy.svc)

	out, cmdErr := runCommand(t, cmds, "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{Ref: "name:Click Me"}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello world"}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "Return"}),
			stepOf("set-value", protocol.SetValueArgs{Ref: "id:text-field", Value: "typed"}),
			stepOf("activate-window", nil),
		},
	})
	if cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}

	// Every step ran, in order, each through the shared pipeline.
	steps := spy.recorded()
	want := []string{
		"locked", "integrity", "activate", "resolve", "point", "click", // 1: click
		"locked", "integrity", "activate", "type-text", // 2: type-text
		"locked", "integrity", "activate", "press-key", // 3: press-key
		"locked", "integrity", "activate", "resolve", "set-value", // 4: set-value
		"locked", "integrity", "activate", // 5: activate-window
	}
	if !equalStrings(steps, want) {
		t.Errorf("pipeline steps = %v, want %v", steps, want)
	}
	if typed := spy.typedTexts(); len(typed) != 1 || typed[0] != "hello world" {
		t.Errorf("typed %q, want one \"hello world\"", typed)
	}
	if pressed := spy.pressedKeys(); len(pressed) != 1 || pressed[0] != "Return" {
		t.Errorf("pressed %q, want one Return", pressed)
	}
	if spy.valueValue != "typed" || spy.valueIndex != 2 {
		t.Errorf("set value %q on index %d, want \"typed\" on the resolved 2", spy.valueValue, spy.valueIndex)
	}
	// The click landed on the Window the Batch named.
	if spy.clickTarget != 101 {
		t.Errorf("clicked window %d, want 101", spy.clickTarget)
	}
}

func TestBatchStopsAtFirstFailureNamingTheStep(t *testing.T) {
	spy := newBatchSpy()
	spy.point = uia.Point{X: 100, Y: 200}
	spy.typeErr = &win.OcclusionError{X: 660, Y: 430, OverTitle: "Something Else"}
	cmds := CommandsWith(spy.svc)

	_, cmdErr := runCommand(t, cmds, "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(1)}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello"}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "Return"}), // must not run
		},
	})
	if cmdErr == nil {
		t.Fatal("a failed step must fail the Batch")
	}
	wantMsg := `batch stopped at step 2 (type-text): point (660, 430) is over "Something Else", not the target Window`
	if cmdErr.Msg != wantMsg {
		t.Errorf("message = %q, want %q", cmdErr.Msg, wantMsg)
	}
	if !strings.Contains(cmdErr.Hint, "covers the target Element") {
		t.Errorf("hint = %q, want the step's own recovery hint forwarded", cmdErr.Hint)
	}
	if cmdErr.Exit != 1 {
		t.Errorf("exit = %d, want 1", cmdErr.Exit)
	}
	// Steps after the failure did not run: no press-key at all.
	if pressed := spy.pressedKeys(); len(pressed) != 0 {
		t.Errorf("pressed %q after the failure; remaining steps must not run", pressed)
	}
	if count := countSteps(spy.recorded(), "type-text"); count != 1 {
		t.Errorf("type-text ran %d times, want 1", count)
	}
}

func TestBatchFirstStepFailureNamesStepOne(t *testing.T) {
	// An unresolvable Window fails the first step, and no step runs.
	spy := newBatchSpy()
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "Chrome", // two Windows: ambiguous
		Steps:  []protocol.Step{stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(1)})},
	})
	if cmdErr == nil {
		t.Fatal("an ambiguous Window must fail the Batch")
	}
	wantMsg := `batch stopped at step 1 (click): app "Chrome" has 2 open Windows; name the Window by its Window ID`
	if cmdErr.Msg != wantMsg {
		t.Errorf("message = %q, want %q", cmdErr.Msg, wantMsg)
	}
	if !strings.HasPrefix(cmdErr.Hint, "candidates:") {
		t.Errorf("hint = %q, want the candidate Windows", cmdErr.Hint)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("pipeline steps = %v, want none", got)
	}
}

func TestBatchRefusesMalformedStepsBeforeAnyInput(t *testing.T) {
	// A step whose Kind is not a batchable Action, or whose arguments are
	// malformed JSON, fails the whole Batch before step 1 sends anything.
	cases := []struct {
		name  string
		steps []protocol.Step
		want  string
	}{
		{
			name:  "unknown action in a later step",
			steps: []protocol.Step{stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(1)}), {Kind: "get-window-state"}},
			want:  `step 2 (get-window-state) is not a batchable action`,
		},
		{
			name:  "observation command",
			steps: []protocol.Step{{Kind: "zoom"}},
			want:  `step 1 (zoom) is not a batchable action`,
		},
		{
			name:  "a nested batch",
			steps: []protocol.Step{{Kind: "batch"}},
			want:  `step 1 (batch) is not a batchable action`,
		},
		{
			name:  "arguments that are not an object",
			steps: []protocol.Step{{Kind: "click", Args: json.RawMessage(`5`)}},
			want:  `step 1 (click) has invalid arguments`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newBatchSpy()
			spy.point = uia.Point{X: 100, Y: 200}
			raw, merr := json.Marshal(protocol.BatchArgs{Window: "101", Steps: tc.steps})
			if merr != nil {
				t.Fatal(merr)
			}
			cmds := CommandsWith(spy.svc)
			_, cmdErr := cmds["batch"](context.Background(), raw)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.want) {
				t.Fatalf("message = %v, want it to contain %q", cmdErr, tc.want)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("pipeline steps = %v, want none: a malformed Batch never acts", got)
			}
		})
	}
}

func TestBatchUsageFailures(t *testing.T) {
	cases := []struct {
		name string
		args string // raw JSON; one case is malformed on purpose
		want string
	}{
		{"no window", `{"steps":[{"kind":"activate-window"}]}`, "a window reference is required"},
		{"no steps", `{"window":"101"}`, "at least one action step is required"},
		{"not JSON", `{"window":`, "invalid arguments"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmds := CommandsWith(newBatchSpy().svc)
			_, cmdErr := cmds["batch"](context.Background(), json.RawMessage(tc.args))
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.want) {
				t.Fatalf("message = %v, want it to contain %q", cmdErr, tc.want)
			}
			if !strings.Contains(cmdErr.Hint, "usage: winuse batch") {
				t.Errorf("hint = %q, want the usage line", cmdErr.Hint)
			}
		})
	}
}

func TestBatchInjectsTheBatchesWindowIntoEveryStep(t *testing.T) {
	// A step whose arguments carry a window of their own still acts on
	// the Batch's Window: one Batch, one Window.
	spy := newBatchSpy()
	spy.point = uia.Point{X: 100, Y: 200}
	stepWithWindow := protocol.Step{
		Kind: "click",
		Args: batchStepArgs(protocol.ClickArgs{Window: "202", ElementIndex: intPtrBatch(1)}),
	}
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps:  []protocol.Step{stepWithWindow},
	})
	if cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	if spy.clickTarget != 101 || spy.pointHwnd != 101 {
		t.Errorf("clicked window %d (resolved %d), want 101", spy.clickTarget, spy.pointHwnd)
	}
}

func TestBatchExitCodeForwarding(t *testing.T) {
	// Drive the batch handler over a hand-built registry so the step's
	// exit code (130, the Esc stop) is observable end to end.
	batchCmd := batch(map[string]Command{
		"press-key": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			return "", &Error{
				Msg:  "the user stopped winuse with the physical Escape key",
				Hint: "stop and ask the user",
				Exit: protocol.ExitEsc,
			}
		},
	}, nil)
	raw, err := json.Marshal(protocol.BatchArgs{
		Window: "101",
		Steps:  []protocol.Step{stepOf("press-key", protocol.PressKeyArgs{Key: "Return"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, cmdErr := batchCmd(context.Background(), raw)
	if cmdErr == nil {
		t.Fatal("the stopped step must fail the Batch")
	}
	if cmdErr.Exit != protocol.ExitEsc {
		t.Errorf("exit = %d, want %d forwarded from the step", cmdErr.Exit, protocol.ExitEsc)
	}
	if !strings.HasPrefix(cmdErr.Msg, "batch stopped at step 1 (press-key): the user stopped winuse") {
		t.Errorf("message = %q, want the step named with its message", cmdErr.Msg)
	}
	if cmdErr.Hint != "stop and ask the user" {
		t.Errorf("hint = %q, want the step's hint verbatim", cmdErr.Hint)
	}
}

func TestWithWindowPreservesStepValues(t *testing.T) {
	// The injection overlays the window without touching the step's own
	// values (numbers and strings survive verbatim).
	injected, err := withWindow("101", json.RawMessage(`{"click_count":2,"mouse_button":"right","ref":"name:OK"}`))
	if err != nil {
		t.Fatalf("withWindow: %v", err)
	}
	var a protocol.ClickArgs
	if err := json.Unmarshal(injected, &a); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if a.Window != "101" || a.Ref != "name:OK" || a.ClickCount != 2 || a.MouseButton != "right" {
		t.Errorf("injected args = %+v, want the step's values with window 101", a)
	}

	// No arguments at all: just the window.
	injected, err = withWindow("202", nil)
	if err != nil {
		t.Fatalf("withWindow(nil): %v", err)
	}
	var w map[string]json.RawMessage
	if err := json.Unmarshal(injected, &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(w["window"]) != `"202"` || len(w) != 1 {
		t.Errorf("injected = %s, want only window 202", injected)
	}
}

func TestBatchRegisteredInCommands(t *testing.T) {
	cmds := CommandsWith(newBatchSpy().svc)
	if _, ok := cmds["batch"]; !ok {
		t.Fatal("batch must be registered in the command set")
	}
	// And every batchable Action is a registered command the batch can
	// dispatch to.
	for _, kind := range protocol.BatchStepKinds {
		if _, ok := cmds[kind]; !ok {
			t.Errorf("batch step kind %q has no registered command", kind)
		}
	}
}

// countSteps counts the recorded steps named step.
func countSteps(steps []string, step string) int {
	n := 0
	for _, s := range steps {
		if s == step {
			n++
		}
	}
	return n
}

// intPtrBatch is the tests' *int builder (named for this file to keep the
// helper package's test helpers distinct).
func intPtrBatch(v int) *int { return &v }

func TestBatchStepWithoutAnAction(t *testing.T) {
	raw, err := json.Marshal(protocol.BatchArgs{
		Window: "101",
		Steps:  []protocol.Step{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, cmdErr := batch(map[string]Command{}, nil)(context.Background(), raw)
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "step 1 has no action") {
		t.Fatalf("message = %v, want the no-action refusal", cmdErr)
	}
}
