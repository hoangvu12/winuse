package helper

// The ticket-11 Action commands' offline tests: name:/id: Element
// References through click, set-value and perform-secondary-action (one
// match becomes the Element Index the Action acts on; ambiguity lists
// the candidates; a miss points at a fresh Window State read; a Window
// without a cached state says so), and the mutual-exclusion refusals when
// two Element Reference forms are supplied. Everything Win32 and
// element-side is faked; no desktop is touched.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// ResolveElementRef implements ElementResolver on the click pipeline's
// spy: it records the call and answers with the configured outcome.
func (s *actionSpy) ResolveElementRef(hwnd uint64, ref string) (int, error) {
	s.step("resolve")
	s.mu.Lock()
	s.refHwnd, s.refRef = hwnd, ref
	index, err := s.refIndex, s.refErr
	s.mu.Unlock()
	return index, err
}

// ambiguousRefErr builds an AmbiguousRefError the way the uia layer
// returns one (wstate's structured error, surfaced through the Client).
func ambiguousRefErr(text string, matches ...int) error {
	return &wstate.AmbiguousRefError{
		Kind:    wstate.RefName,
		Text:    text,
		Matches: matches,
		Candidates: []string{
			"1 button Twin ID: twin-a",
			"2 button Twin ID: twin-b",
		},
	}
}

func TestClickByReferenceResolvesThenClicks(t *testing.T) {
	spy := &actionSpy{refIndex: 2, point: uia.Point{X: 100, Y: 200}}
	out, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", protocol.ClickArgs{
		Window: "101", Ref: "name:OK",
	})
	if cmdErr != nil {
		t.Fatalf("click by name: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	// The pipeline: checks, activation, reference resolution, then exactly
	// the Element Index form's remaining steps.
	want := []string{"locked", "integrity", "activate", "resolve", "point", "click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
	if spy.refHwnd != 101 || spy.refRef != "name:OK" {
		t.Errorf("resolved reference %q on window %d, want name:OK on 101", spy.refRef, spy.refHwnd)
	}
	if spy.pointIndex != 2 {
		t.Errorf("clicked the resolved index %d, want 2", spy.pointIndex)
	}
	if spy.clickTarget != 101 || spy.clickX != 100 || spy.clickY != 200 {
		t.Errorf("clicked window %d at (%d, %d), want 101 at (100, 200)", spy.clickTarget, spy.clickX, spy.clickY)
	}
}

func TestClickAmbiguousReferenceListsCandidates(t *testing.T) {
	spy := &actionSpy{refErr: ambiguousRefErr("Twin", 1, 2)}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", protocol.ClickArgs{
		Window: "101", Ref: "name:Twin",
	})
	if cmdErr == nil {
		t.Fatal("an ambiguous reference must fail, never guess")
	}
	wantMsg := `name "Twin" matches 2 elements in the cached Window State for window 101`
	if cmdErr.Msg != wantMsg {
		t.Errorf("message = %q, want %q", cmdErr.Msg, wantMsg)
	}
	// The hint lists the candidate lines (ticket 03's pattern).
	if !strings.HasPrefix(cmdErr.Hint, "candidates:\n") ||
		!strings.Contains(cmdErr.Hint, "1 button Twin ID: twin-a") ||
		!strings.Contains(cmdErr.Hint, "2 button Twin ID: twin-b") {
		t.Errorf("hint = %q, want the candidate lines", cmdErr.Hint)
	}
	if cmdErr.Exit != 1 {
		t.Errorf("exit = %d, want 1", cmdErr.Exit)
	}
	// No input: the resolution failed before the point/click steps.
	if containsStep(spy.recorded(), "click") || containsStep(spy.recorded(), "point") {
		t.Error("an ambiguous reference must not resolve a point or send input")
	}
}

func TestClickMissingReferencePointsAtWindowState(t *testing.T) {
	spy := &actionSpy{refErr: &wstate.NoMatchRefError{Kind: wstate.RefName, Text: "OK"}}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", protocol.ClickArgs{
		Window: "101", Ref: "name:OK",
	})
	if cmdErr == nil {
		t.Fatal("a missing reference must fail")
	}
	wantMsg := `no element named "OK" in the cached Window State for window 101`
	if cmdErr.Msg != wantMsg {
		t.Errorf("message = %q, want %q", cmdErr.Msg, wantMsg)
	}
	if !strings.Contains(cmdErr.Hint, "get-window-state 101") {
		t.Errorf("hint = %q, want the read-the-state instruction", cmdErr.Hint)
	}
	if containsStep(spy.recorded(), "click") {
		t.Error("a missing reference must not send input")
	}

	// The id: form names the ID column.
	spy = &actionSpy{refErr: &wstate.NoMatchRefError{Kind: wstate.RefID, Text: "box"}}
	_, cmdErr = runCommand(t, CommandsWith(spy.services()), "click", protocol.ClickArgs{
		Window: "101", Ref: "id:box",
	})
	if cmdErr == nil || cmdErr.Msg != `no element with ID "box" in the cached Window State for window 101` {
		t.Fatalf("message = %v, want the ID wording", cmdErr)
	}
}

func TestClickReferenceWithoutCachedState(t *testing.T) {
	spy := &actionSpy{refErr: fmt.Errorf("%w: window 101", uia.ErrNoState)}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", protocol.ClickArgs{
		Window: "101", Ref: "name:OK",
	})
	if cmdErr == nil || cmdErr.Msg != "no cached Window State is available for window 101" {
		t.Fatalf("message = %v, want the shared no-state failure", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "get-window-state 101") {
		t.Errorf("hint = %q, want the read-the-state instruction", cmdErr.Hint)
	}
}

func TestClickMalformedReference(t *testing.T) {
	spy := &actionSpy{refErr: &wstate.BadRefError{Ref: "what:OK"}}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", protocol.ClickArgs{
		Window: "101", Ref: "what:OK",
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "invalid element reference") {
		t.Fatalf("message = %v, want the invalid-reference refusal", cmdErr)
	}
	if containsStep(spy.recorded(), "click") {
		t.Error("a malformed reference must not send input")
	}
}

func TestClickRefusesTwoElementReferenceForms(t *testing.T) {
	spy := &actionSpy{refIndex: 2}
	index := 3
	cases := []struct {
		name string
		args protocol.ClickArgs
	}{
		{"index and reference", protocol.ClickArgs{Window: "101", ElementIndex: &index, Ref: "name:OK"}},
		{"point and reference", protocol.ClickArgs{Window: "101", Point: &protocol.Point{X: 1, Y: 1}, Ref: "name:OK"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", tc.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, "not several") {
				t.Fatalf("message = %v, want the several-forms refusal", cmdErr)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("pipeline steps = %v, want none: the refusal comes first", got)
			}
		})
	}
}

func TestSetValueByReferenceResolvesThenSets(t *testing.T) {
	spy := newActorSpy()
	spy.refIndex = 2
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "set-value", protocol.SetValueArgs{
		Window: "101", Ref: "id:text-field", Value: "hello",
	})
	if cmdErr != nil {
		t.Fatalf("set-value by id: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	if spy.refRef != "id:text-field" || spy.valueIndex != 2 {
		t.Errorf("resolved %q to index %d, want id:text-field to 2", spy.refRef, spy.valueIndex)
	}
	if spy.valueHwnd != 101 || spy.valueValue != "hello" {
		t.Errorf("set window %d value %q, want 101 \"hello\"", spy.valueHwnd, spy.valueValue)
	}
}

func TestSetValueReferenceFailuresShareTheClickMapping(t *testing.T) {
	// The reference-failure mapping is shared with click (one place, the
	// same messages); set-value proves the wiring, click pins the texts.
	spy := newActorSpy()
	spy.refErr = ambiguousRefErr("Twin", 1, 2)
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "set-value", protocol.SetValueArgs{
		Window: "101", Ref: "name:Twin", Value: "x",
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `name "Twin" matches 2 elements`) {
		t.Fatalf("message = %v, want the ambiguous failure", cmdErr)
	}
	if !strings.HasPrefix(cmdErr.Hint, "candidates:") {
		t.Errorf("hint = %q, want the candidate list", cmdErr.Hint)
	}
	if spy.valueIndex != 0 || containsStep(spy.recorded(), "set-value") {
		t.Error("an ambiguous reference must not set a value")
	}

	// Two Element Reference forms at once is refused before the pipeline.
	index := 2
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "set-value", protocol.SetValueArgs{
		Window: "101", ElementIndex: &index, Ref: "name:Twin", Value: "x",
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "not several") {
		t.Fatalf("message = %v, want the several-forms refusal", cmdErr)
	}
}

func TestPerformSecondaryActionByReference(t *testing.T) {
	spy := newActorSpy()
	spy.refIndex = 6
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", protocol.SecondaryActionArgs{
		Window: "101", Ref: "name:Details", Action: "expand",
	})
	if cmdErr != nil {
		t.Fatalf("perform-secondary-action by name: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	if spy.actionIndex != 6 || spy.actionPerformed != "Expand" {
		t.Errorf("performed %q on index %d, want Expand on 6", spy.actionPerformed, spy.actionIndex)
	}

	// A reference resolution failure is the shared mapping.
	spy = newActorSpy()
	spy.refErr = &wstate.NoMatchRefError{Kind: wstate.RefID, Text: "nope"}
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", protocol.SecondaryActionArgs{
		Window: "101", Ref: "id:nope", Action: "Expand",
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `no element with ID "nope"`) {
		t.Fatalf("message = %v, want the missing-reference failure", cmdErr)
	}
}

func TestReferencesWithoutAResolverFailClosed(t *testing.T) {
	// A Services whose Actions carries no ElementResolver (a fake, or a
	// UIA-less helper) refuses references instead of guessing.
	svc := testServices(&fakeTrees{})
	svc.Locked = func() (bool, string) { return false, "" }
	svc.Integrity = func(win.WindowInfo) error { return nil }
	svc.Activate = func(uint64) error { return nil }
	svc.Actions = nil
	_, cmdErr := runCommand(t, CommandsWith(svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:OK"})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "unavailable") {
		t.Fatalf("message = %v, want the no-UIA-client refusal", cmdErr)
	}
}
