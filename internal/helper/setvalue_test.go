package helper

// The ticket-07 Action commands' offline tests: the output contract (`ok`
// or a one-line failure with a hint, exit 1), the argument validation
// (unknown action names, missing indexes), the set-value failures (not
// settable pointing at type-text, disabled, stale, no cached state), the
// perform-secondary-action failures (unsupported action, an action the
// Element does not offer listing the ones it does), and the pipeline
// ordering: the checks refuse before any activation, and no input is
// sent once anything refuses. Everything Win32 and element-side is
// faked; no desktop is touched.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// actorSpy fakes the element-side half of ticket 07's Actions on top of
// the click pipeline's spy: it records SetValue / PerformSecondaryAction
// calls and returns configurable errors.
type actorSpy struct {
	svc Services
	*actionSpy

	setValueErr     error
	secondaryErr    error
	valueHwnd       uint64
	valueIndex      int
	valueValue      string
	actionHwnd      uint64
	actionIndex     int
	actionPerformed string
}

func newActorSpy() *actorSpy {
	base := &actionSpy{}
	s := &actorSpy{actionSpy: base}
	svc := testServices(&fakeTrees{})
	svc.Locked = func() (bool, string) { s.step("locked"); return base.locked, base.lockedDesktop }
	svc.Integrity = func(w win.WindowInfo) error { s.step("integrity"); return base.refuse }
	svc.Activate = func(hwnd uint64) error { s.step("activate"); return base.activateErr }
	svc.Actions = s
	s.svc = svc
	return s
}

// SetValue implements ElementActor.
func (s *actorSpy) SetValue(ctx context.Context, hwnd uint64, index int, value string) error {
	s.step("set-value")
	s.valueHwnd, s.valueIndex, s.valueValue = hwnd, index, value
	return s.setValueErr
}

// PerformSecondaryAction implements ElementActor.
func (s *actorSpy) PerformSecondaryAction(ctx context.Context, hwnd uint64, index int, action string) error {
	s.step("secondary")
	s.actionHwnd, s.actionIndex, s.actionPerformed = hwnd, index, action
	return s.secondaryErr
}

// setValueArgs builds wire arguments for set-value.
func setValueArgs(window string, index int, value string) json.RawMessage {
	raw, err := json.Marshal(protocol.SetValueArgs{Window: window, ElementIndex: &index, Value: value})
	if err != nil {
		panic(err)
	}
	return raw
}

// secondaryArgs builds wire arguments for perform-secondary-action.
func secondaryArgs(window string, index int, action string) json.RawMessage {
	raw, err := json.Marshal(protocol.SecondaryActionArgs{Window: window, ElementIndex: &index, Action: action})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestSetValuePrintsOK(t *testing.T) {
	spy := newActorSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("101", 2, "hello world"))
	if cmdErr != nil {
		t.Fatalf("set-value: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	// The pipeline order: checks, activation, then the element-side step.
	want := []string{"locked", "integrity", "activate", "set-value"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
	if spy.valueHwnd != 101 || spy.valueIndex != 2 || spy.valueValue != "hello world" {
		t.Errorf("set window %d index %d value %q, want window 101 index 2 value %q",
			spy.valueHwnd, spy.valueIndex, spy.valueValue, "hello world")
	}
}

func TestSetValueRefusesLikeClick(t *testing.T) {
	locked := newActorSpy()
	locked.locked = true
	_, cmdErr := runCommand(t, CommandsWith(locked.svc), "set-value", setValueArgs("101", 1, "x"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "desktop is locked") {
		t.Fatalf("locked: %v", cmdErr)
	}
	admin := newActorSpy()
	admin.refuse = &win.AdminWindowError{WindowID: 101, App: "Notepad.exe"}
	_, cmdErr = runCommand(t, CommandsWith(admin.svc), "set-value", setValueArgs("101", 1, "x"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "administrator") {
		t.Fatalf("admin: %v", cmdErr)
	}
	if containsStep(admin.recorded(), "set-value") {
		t.Error("a refused Window must not reach the element step")
	}
}

func TestSetValueValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    json.RawMessage
		wantMsg string
	}{
		{"no arguments", nil, "set-value: arguments are required"},
		{"missing element index", json.RawMessage(`{"window":"101","value":"x"}`), "set-value: an element index is required"},
		{"negative element index", setValueArgs("101", -1, "x"), "set-value: the element index must be a non-negative integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newActorSpy()
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "set-value", tc.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.wantMsg) {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantMsg)
			}
			if cmdErr.Exit != 1 {
				t.Errorf("exit = %d, want 1", cmdErr.Exit)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("argument failures must not touch the desktop: steps %v", got)
			}
		})
	}
}

func TestSetValueWithoutUIAClient(t *testing.T) {
	// Services with a Trees fake that is not an ElementActor: the command
	// refuses rather than panicking.
	svc := testServices(&fakeTrees{})
	_, cmdErr := runCommand(t, CommandsWith(svc), "set-value", setValueArgs("101", 1, "x"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "set-value is unavailable: no UIA client") {
		t.Fatalf("message = %v", cmdErr)
	}
}

func TestSetValueFailuresAndHints(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantMsg  string
		wantHint string
	}{
		{
			name:     "not settable points at type-text",
			err:      uia.ErrNotSettable,
			wantMsg:  "Cannot set a value for an element that is not settable",
			wantHint: "type-text",
		},
		{
			name:     "range value must be a number",
			err:      fmt.Errorf("%w: abc", uia.ErrRangeValue),
			wantMsg:  "range value must be a number: abc",
			wantHint: "numeric",
		},
		{
			name:     "focus failure",
			err:      uia.ErrFocusBeforeValue,
			wantMsg:  "focus element before setting value",
			wantHint: "retry",
		},
		{
			name:     "disabled element",
			err:      uia.ErrDisabled,
			wantMsg:  "the Element is disabled",
			wantHint: "disabled",
		},
		{
			name:     "stale element index",
			err:      fmt.Errorf("%w: element 2 is not in the cached Window State of window 101", uia.ErrStale),
			wantMsg:  "element 2 is not available in the cached Window State for window 101",
			wantHint: "get-window-state 101",
		},
		{
			name:     "no cached state",
			err:      fmt.Errorf("%w: window 101", uia.ErrNoState),
			wantMsg:  "no cached Window State is available for window 101",
			wantHint: "get-window-state 101",
		},
		{
			name:     "a COM failure surfaces verbatim",
			err:      errors.New("setting the Element's value failed (HRESULT 0x8000ffff)"),
			wantMsg:  "setting the Element's value failed (HRESULT 0x8000ffff)",
			wantHint: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newActorSpy()
			spy.setValueErr = tc.err
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("101", 2, "x"))
			if cmdErr == nil || cmdErr.Msg != tc.wantMsg {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantMsg)
			}
			if tc.wantHint != "" && !strings.Contains(cmdErr.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to mention %q", cmdErr.Hint, tc.wantHint)
			}
			if cmdErr.Exit != 1 {
				t.Errorf("exit = %d, want 1", cmdErr.Exit)
			}
		})
	}
}

func TestSetValueWindowResolutionFailure(t *testing.T) {
	spy := newActorSpy()
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("Paint", 1, "x"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `app "Paint" has no open Windows`) {
		t.Fatalf("message = %v", cmdErr)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("a window that does not resolve must not activate or act: steps %v", got)
	}
}

func TestPerformSecondaryActionPrintsOKAndCanonicalizes(t *testing.T) {
	// Case-insensitive action names reach the element side canonicalized.
	spy := newActorSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", secondaryArgs("101", 3, "scroll  DOWN"))
	if cmdErr != nil {
		t.Fatalf("perform-secondary-action: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	if spy.actionHwnd != 101 || spy.actionIndex != 3 || spy.actionPerformed != "Scroll Down" {
		t.Errorf("performed %q on window %d index %d, want \"Scroll Down\" on window 101 index 3",
			spy.actionPerformed, spy.actionHwnd, spy.actionIndex)
	}
	want := []string{"locked", "integrity", "activate", "secondary"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
}

func TestPerformSecondaryActionValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    json.RawMessage
		wantMsg string
	}{
		{"no arguments", nil, "perform-secondary-action: arguments are required"},
		{"missing element index", json.RawMessage(`{"window":"101","action":"Expand"}`), "perform-secondary-action: an element index is required"},
		{"negative element index", secondaryArgs("101", -1, "Expand"), "perform-secondary-action: the element index must be a non-negative integer"},
		{"missing action", secondaryArgs("101", 1, ""), "perform-secondary-action: an action is required"},
		{"unknown action lists the expected set", secondaryArgs("101", 1, "Fly"),
			"unsupported secondary action: Fly; expected Raise, Scroll Up, Scroll Down, Scroll Left, Scroll Right, Expand, Collapse, Toggle, or Select"},
		{"case does not make an unknown action known", secondaryArgs("101", 1, "invoke"),
			"unsupported secondary action: invoke; expected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newActorSpy()
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", tc.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.wantMsg) {
				t.Fatalf("message = %v, want it to contain %q", cmdErr, tc.wantMsg)
			}
			if tc.name == "unknown action lists the expected set" {
				if !strings.Contains(cmdErr.Hint, "Window State") {
					t.Errorf("hint = %q, want it to point at the Window State", cmdErr.Hint)
				}
			}
			if cmdErr.Exit != 1 {
				t.Errorf("exit = %d, want 1", cmdErr.Exit)
			}
			// Validation refuses before the pipeline: no activation.
			if containsStep(spy.recorded(), "activate") {
				t.Errorf("%s: validation must refuse before activation", tc.name)
			}
		})
	}
}

func TestPerformSecondaryActionFailuresAndHints(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantMsg  string
		wantHint string
	}{
		{
			name: "an action the Element does not offer lists what it does",
			err: &uia.UnavailableActionError{Action: "Expand", Index: 6,
				Offered: []string{"Toggle"}},
			wantMsg:  "secondary action 'Expand' is not available for element 6; available actions: Toggle",
			wantHint: "get-window-state 101",
		},
		{
			name: "an Element with no Secondary Actions says so",
			err: &uia.UnavailableActionError{Action: "Toggle", Index: 2,
				Offered: nil},
			wantMsg:  "secondary action 'Toggle' is not available for element 2; available actions: none",
			wantHint: "get-window-state 101",
		},
		{
			name:     "the pattern disappeared",
			err:      errors.New("element no longer exposes ScrollPattern"),
			wantMsg:  "element no longer exposes ScrollPattern",
			wantHint: "",
		},
		{
			name:     "stale element index",
			err:      fmt.Errorf("%w: element 6 is not in the cached Window State of window 101", uia.ErrStale),
			wantMsg:  "element 6 is not available in the cached Window State for window 101",
			wantHint: "get-window-state 101",
		},
		{
			name:     "no cached state",
			err:      fmt.Errorf("%w: window 101", uia.ErrNoState),
			wantMsg:  "no cached Window State is available for window 101",
			wantHint: "get-window-state 101",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newActorSpy()
			spy.secondaryErr = tc.err
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", secondaryArgs("101", 6, "Expand"))
			if cmdErr == nil || cmdErr.Msg != tc.wantMsg {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantMsg)
			}
			if tc.wantHint != "" && !strings.Contains(cmdErr.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to mention %q", cmdErr.Hint, tc.wantHint)
			}
			if cmdErr.Exit != 1 {
				t.Errorf("exit = %d, want 1", cmdErr.Exit)
			}
		})
	}
}

func TestSetValueAndSecondaryCommandsRegistered(t *testing.T) {
	cmds := Commands()
	for _, name := range []string{"set-value", "perform-secondary-action"} {
		if _, ok := cmds[name]; !ok {
			t.Errorf("%s missing from the command registry: %v", name, cmds)
		}
	}
}
