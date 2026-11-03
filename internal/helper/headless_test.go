package helper

// The Headless Actions' offline tests (ADR 0010, tickets 01 and 02 of the
// headless spec): the routing per Action and flag (Headless click,
// set-value and Secondary Actions never activate, inject or show the
// banner; @x,y, double/right/middle clicks, Raise and `headed` stay v1),
// the "nothing was sent" refusals and the --headed hint, the write and
// read-back failure wording, the Agent Cursor's order around the act, the
// passive Esc scope, and the Batch's up-front scope choice. Everything
// Win32 and element-side is faked; no desktop is touched.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// headlessSpy is a batchSpy (every v1 seam faked and recorded) whose
// element side is also a HeadlessActor, so Element Actions route
// Headless; the v1 recordings ("activate", "click", "type-text") prove
// the Headless path never reaches them.
type headlessSpy struct {
	*batchSpy

	route      string // HeadlessClick's route
	hClickErr  error
	hValueErr  error
	hSecErr    error
	readyErr   error
	boundsErr  error
	bounds     win.Rect
	boundsName string

	hIndex  int
	hValue  string
	hAction string

	scroll scrollRecord // Element scroll's fake (scroll_test.go)
	text   textRecord   // select-text's fake (selecttext_test.go)

	esc    *scopeEsc
	cursor *cursorSpy
}

func newHeadlessSpy() *headlessSpy {
	s := &headlessSpy{batchSpy: newBatchSpy(), route: "Invoke"}
	s.point = uia.Point{X: 100, Y: 200}
	s.bounds = win.Rect{Left: 300, Top: 400, Right: 380, Bottom: 430}
	s.boundsName = "Save"
	s.esc = &scopeEsc{}
	s.cursor = &cursorSpy{share: s.step}
	s.svc.Actions = s
	s.svc.Esc = s.esc
	s.svc.Cursor = s.cursor
	s.svc.Ready = func(hwnd uint64) error { s.step("ready"); return s.readyErr }
	return s
}

// HeadlessClick implements HeadlessActor.
func (s *headlessSpy) HeadlessClick(ctx context.Context, hwnd uint64, index int) (string, error) {
	s.step("headless-click")
	s.hIndex = index
	s.esc.pressIf("act")
	return s.route, s.hClickErr
}

// SetValueHeadless implements HeadlessActor.
func (s *headlessSpy) SetValueHeadless(ctx context.Context, hwnd uint64, index int, value string) error {
	s.step("headless-set-value")
	s.hIndex, s.hValue = index, value
	s.esc.pressIf("act")
	return s.hValueErr
}

// PerformSecondaryActionHeadless implements HeadlessActor.
func (s *headlessSpy) PerformSecondaryActionHeadless(ctx context.Context, hwnd uint64, index int, action string) error {
	s.step("headless-secondary")
	s.hIndex, s.hAction = index, action
	s.esc.pressIf("act")
	return s.hSecErr
}

// ElementBounds implements HeadlessActor.
func (s *headlessSpy) ElementBounds(ctx context.Context, hwnd uint64, index int) (int32, int32, int32, int32, string, error) {
	s.step("bounds")
	b := s.bounds
	return b.Left, b.Top, b.Right, b.Bottom, s.boundsName, s.boundsErr
}

// scopeEsc is an Esc service with both scope kinds (PassiveEscWatch): it
// logs every bracket so tests can assert which kind a command armed, and
// latches like the real one (the last scope of either kind clears).
type scopeEsc struct {
	mu      sync.Mutex
	armed   int
	passive int
	pressed bool
	log     []string
	pressOn string // "travel" or "act": the moment the fakes press Esc
}

func (e *scopeEsc) Arm()           { e.bracket("arm", &e.armed, 1) }
func (e *scopeEsc) Disarm()        { e.bracket("disarm", &e.armed, -1) }
func (e *scopeEsc) ArmPassive()    { e.bracket("arm-passive", &e.passive, 1) }
func (e *scopeEsc) DisarmPassive() { e.bracket("disarm-passive", &e.passive, -1) }

func (e *scopeEsc) bracket(name string, count *int, d int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.log = append(e.log, name)
	if *count+d < 0 {
		return
	}
	*count += d
	if e.armed == 0 && e.passive == 0 {
		e.pressed = false
	}
}

func (e *scopeEsc) Stopped() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pressed
}

func (e *scopeEsc) pressIf(moment string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pressOn == moment && (e.armed > 0 || e.passive > 0) {
		e.pressed = true
	}
}

func (e *scopeEsc) brackets() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.log...)
}

// cursorSpy fakes the Agent Cursor, recording into the shared step log.
type cursorSpy struct {
	share   func(string)
	esc     *scopeEsc
	mu      sync.Mutex
	origins []win.Rect
	targets []win.Rect
	labels  []string
	clicks  []win.Rect
	hidden  bool
	panics  bool
}

func (c *cursorSpy) Travel(origin, target win.Rect, label string) {
	c.share("cursor-travel")
	c.mu.Lock()
	c.origins = append(c.origins, origin)
	c.targets = append(c.targets, target)
	c.labels = append(c.labels, label)
	panics := c.panics
	c.mu.Unlock()
	if c.esc != nil {
		c.esc.pressIf("travel")
	}
	if panics {
		panic("cursor wedged")
	}
}

func (c *cursorSpy) Click(target win.Rect) {
	c.share("cursor-click")
	c.mu.Lock()
	c.clicks = append(c.clicks, target)
	c.mu.Unlock()
}

func (c *cursorSpy) HideWhile(f func()) {
	c.share("cursor-capture-hide")
	c.mu.Lock()
	c.hidden = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.hidden = false
		c.mu.Unlock()
		c.share("cursor-capture-restore")
	}()
	f()
}

func (c *cursorSpy) isHidden() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hidden
}

// v1Steps are the recordings only a Headed Action makes.
var v1Steps = []string{"activate", "point", "click", "type-text", "press-key", "set-value", "secondary"}

func assertNoV1(t *testing.T, steps []string) {
	t.Helper()
	for _, v1 := range v1Steps {
		if containsStep(steps, v1) {
			t.Errorf("a Headless Action reached the v1 step %q: %v", v1, steps)
		}
	}
	if hasStepPrefix(steps, "overlay-") {
		t.Errorf("a Headless Action touched the in-control banner: %v", steps)
	}
}

func TestHeadlessClickNeverActivatesInjectsOrShowsTheBanner(t *testing.T) {
	spy := newHeadlessSpy()
	ov := &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr != nil {
		t.Fatalf("click: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	want := []string{"locked", "integrity", "ready", "bounds", "cursor-travel", "headless-click", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if spy.hIndex != 2 {
		t.Errorf("clicked element %d, want 2", spy.hIndex)
	}
	if got := spy.esc.brackets(); !equalStrings(got, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want one passive scope", got)
	}
	// The cursor starts at the Window and glides to the Element; the
	// ripple outlines the Element.
	c := spy.cursor
	if len(c.origins) != 1 || c.origins[0] != (win.Rect{Left: 10, Top: 20, Right: 810, Bottom: 490}) {
		t.Errorf("cursor origins = %v, want the Window's bounds", c.origins)
	}
	if len(c.targets) != 1 || c.targets[0] != spy.bounds || len(c.clicks) != 1 || c.clicks[0] != spy.bounds {
		t.Errorf("cursor targets %v clicks %v, want the Element's bounds", c.targets, c.clicks)
	}
	if len(c.labels) != 1 || c.labels[0] != `click "Save"` {
		t.Errorf("cursor labels = %q, want click \"Save\"", c.labels)
	}
}

func TestHeadlessClickByReference(t *testing.T) {
	spy := newHeadlessSpy()
	spy.refIndex = 7
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Save"})
	if cmdErr != nil {
		t.Fatalf("click: %v", cmdErr.Error())
	}
	want := []string{"locked", "integrity", "ready", "resolve", "bounds", "cursor-travel", "headless-click", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
	if spy.hIndex != 7 {
		t.Errorf("clicked element %d, want the resolved 7", spy.hIndex)
	}
}

func TestHeadlessSetValueAndSecondaryActions(t *testing.T) {
	spy := newHeadlessSpy()
	spy.boundsName = "Password"
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("101", 3, "hunter2"))
	if cmdErr != nil {
		t.Fatalf("set-value: %v", cmdErr.Error())
	}
	want := []string{"locked", "integrity", "ready", "bounds", "cursor-travel", "headless-set-value", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("set-value steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if spy.hIndex != 3 || spy.hValue != "hunter2" {
		t.Errorf("set element %d to %q, want 3 and the value", spy.hIndex, spy.hValue)
	}
	if label := spy.cursor.labels[0]; label != `set value "Password"` || strings.Contains(label, "hunter2") {
		t.Errorf("label = %q, want the verb and name, never the value", label)
	}

	for _, tc := range []struct {
		action, name, wantAction, wantLabel string
	}{
		{"toggle", "Bold", "Toggle", `toggle "Bold"`},
		{"EXPAND", "File", "Expand", `expand "File"`},
		{"scroll down", "List", "Scroll Down", `scroll down "List"`},
		{"Select", "", "Select", "select"},
	} {
		spy := newHeadlessSpy()
		spy.boundsName = tc.name
		_, cmdErr := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", secondaryArgs("101", 4, tc.action))
		if cmdErr != nil {
			t.Fatalf("%s: %v", tc.action, cmdErr.Error())
		}
		assertNoV1(t, spy.recorded())
		if spy.hAction != tc.wantAction || spy.hIndex != 4 {
			t.Errorf("%s: performed %q on %d, want %q on 4", tc.action, spy.hAction, spy.hIndex, tc.wantAction)
		}
		if spy.cursor.labels[0] != tc.wantLabel {
			t.Errorf("%s: label = %q, want %q", tc.action, spy.cursor.labels[0], tc.wantLabel)
		}
	}
}

func TestHeadedFormsKeepTheV1Pipeline(t *testing.T) {
	// The physical forms stay Headed with no flag; `headed` forces v1 for
	// the Headless ones. Each reaches the activation and never the
	// Headless element side.
	headed := true
	cases := []struct {
		name string
		cmd  string
		args any
		want []string // nil: only assert the activation ran
	}{
		{"double click", "click", clickArgs("101", 2, 2, "left"), []string{"locked", "integrity", "activate", "point", "click"}},
		{"right click", "click", clickArgs("101", 2, 1, "right"), []string{"locked", "integrity", "activate", "point", "click"}},
		{"middle click", "click", clickArgs("101", 2, 1, "m"), []string{"locked", "integrity", "activate", "point", "click"}},
		{"@x,y click", "click", protocol.ClickArgs{Window: "101", Point: &protocol.Point{X: 5, Y: 5}}, nil},
		{"headed click", "click", protocol.ClickArgs{Window: "101", ElementIndex: intPtrBatch(2), Headed: headed},
			[]string{"locked", "integrity", "activate", "point", "click"}},
		{"Raise", "perform-secondary-action", secondaryArgs("101", 4, "raise"), []string{"locked", "integrity", "activate", "secondary"}},
		{"headed secondary", "perform-secondary-action", protocol.SecondaryActionArgs{Window: "101", ElementIndex: intPtrBatch(4), Action: "Toggle", Headed: true},
			[]string{"locked", "integrity", "activate", "secondary"}},
		{"headed set-value", "set-value", protocol.SetValueArgs{Window: "101", ElementIndex: intPtrBatch(3), Value: "x", Headed: true},
			[]string{"locked", "integrity", "activate", "set-value"}},
		{"@x,y set-value", "set-value", protocol.SetValueArgs{Window: "101", Point: &protocol.Point{X: 5, Y: 5}, Value: "x"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newHeadlessSpy()
			_, _ = runCommand(t, CommandsWith(spy.svc), tc.cmd, tc.args)
			got := spy.recorded()
			if tc.want != nil && !equalStrings(got, tc.want) {
				t.Errorf("pipeline steps = %v, want %v", got, tc.want)
			}
			if !containsStep(got, "activate") {
				t.Errorf("a Headed Action must activate: %v", got)
			}
			for _, h := range []string{"ready", "bounds", "headless-click", "headless-set-value", "headless-secondary"} {
				if containsStep(got, h) {
					t.Errorf("a Headed Action reached the Headless step %q: %v", h, got)
				}
			}
			if b := spy.esc.brackets(); len(b) == 0 || b[0] != "arm" {
				t.Errorf("Esc brackets = %v, want v1's swallowing scope", b)
			}
		})
	}
}

func TestHeadlessStepPredicate(t *testing.T) {
	raw := func(v any) json.RawMessage { return batchStepArgs(v) }
	i := intPtrBatch(1)
	cases := []struct {
		kind string
		args json.RawMessage
		want bool
	}{
		{"click", raw(protocol.ClickArgs{ElementIndex: i}), true},
		{"click", raw(protocol.ClickArgs{Ref: "id:ok", ClickCount: 1, MouseButton: "L"}), true},
		{"click", raw(protocol.ClickArgs{ElementIndex: i, ClickCount: 2}), false},
		{"click", raw(protocol.ClickArgs{ElementIndex: i, MouseButton: "right"}), false},
		{"click", raw(protocol.ClickArgs{ElementIndex: i, MouseButton: "middle"}), false},
		{"click", raw(protocol.ClickArgs{Point: &protocol.Point{X: 1, Y: 1}}), false},
		{"click", raw(protocol.ClickArgs{ElementIndex: i, Headed: true}), false},
		{"click", raw(protocol.ClickArgs{ElementIndex: i, Ref: "id:x"}), false}, // several forms: the command refuses
		{"set-value", raw(protocol.SetValueArgs{Ref: "name:Search", Value: "x"}), true},
		{"set-value", raw(protocol.SetValueArgs{Point: &protocol.Point{}, Value: "x"}), false},
		{"set-value", raw(protocol.SetValueArgs{ElementIndex: i, Headed: true}), false},
		{"perform-secondary-action", raw(protocol.SecondaryActionArgs{ElementIndex: i, Action: "toggle"}), true},
		{"perform-secondary-action", raw(protocol.SecondaryActionArgs{ElementIndex: i, Action: "Raise"}), false},
		{"perform-secondary-action", raw(protocol.SecondaryActionArgs{ElementIndex: i, Action: "Fly"}), false},
		{"perform-secondary-action", raw(protocol.SecondaryActionArgs{Point: &protocol.Point{}, Action: "Toggle"}), false},
		{"type-text", raw(protocol.TypeTextArgs{Text: "x"}), false},
		{"press-key", raw(protocol.PressKeyArgs{Key: "Return"}), false},
		{"scroll", raw(protocol.ScrollArgs{}), false},
		{"activate-window", nil, false},
		{"click", json.RawMessage(`not json`), false},
	}
	for _, tc := range cases {
		if got := headlessStep(tc.kind, tc.args, nil); got != tc.want {
			t.Errorf("headlessStep(%s, %s) = %v, want %v", tc.kind, tc.args, got, tc.want)
		}
	}
	// Without a HeadlessActor (v1's fakes) nothing routes Headless.
	if _, ok := (&actionSpy{}).services().headless("click", raw(protocol.ClickArgs{ElementIndex: i})); ok {
		t.Error("a helper without a HeadlessActor must route every Action Headed")
	}
}

func TestHeadlessClickWithNoRouteSaysNothingSentAndHintsHeaded(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     any
		wantHint string
	}{
		{"index", clickArgs("101", 2, 1, "left"), "winuse --headed click 101 2"},
		{"reference with spaces", protocol.ClickArgs{Window: "101", Ref: "name:Save As"}, `winuse --headed click 101 "name:Save As"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := newHeadlessSpy()
			spy.hClickErr = uia.ErrNoClickRoute
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", tc.args)
			if cmdErr == nil {
				t.Fatal("an Element with no click route must fail")
			}
			if !strings.HasSuffix(cmdErr.Msg, "; nothing was sent") || !strings.Contains(cmdErr.Msg, "cannot be clicked headless") {
				t.Errorf("message = %q, want the no-route refusal with nothing sent", cmdErr.Msg)
			}
			if !strings.Contains(cmdErr.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to contain %q", cmdErr.Hint, tc.wantHint)
			}
			if cmdErr.Exit != 1 {
				t.Errorf("exit = %d, want 1", cmdErr.Exit)
			}
			if containsStep(spy.recorded(), "cursor-click") {
				t.Error("nothing was delivered: no click effect")
			}
			assertNoV1(t, spy.recorded())
		})
	}
}

func TestHeadlessRefusalsBeforeAnyPatternCall(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(*headlessSpy)
		wantMsg  string
		wantHint string
		noArm    bool // refused before the passive scope
	}{
		{"locked desktop", func(s *headlessSpy) { s.locked = true },
			"Windows desktop is locked; unlock before using winuse; nothing was sent", "unlock", true},
		{"administrator window", func(s *headlessSpy) { s.refuse = &win.AdminWindowError{WindowID: 101, App: "Notepad.exe"} },
			"window 101 (Notepad.exe) is running as administrator; winuse cannot send it input; nothing was sent", "elevated", true},
		{"minimized", func(s *headlessSpy) { s.readyErr = &win.MinimizedError{WindowID: 101, Class: "Chrome_WidgetWin_1"} },
			"window 101 is minimized; nothing was sent", "winuse activate-window 101", true},
		{"minimized Store app", func(s *headlessSpy) { s.readyErr = &win.MinimizedError{WindowID: 101, Class: "ApplicationFrameWindow"} },
			"window 101 is minimized, and a minimized Store app has no UI to act on; nothing was sent", "comes to the front when restored", true},
		{"modal dialog", func(s *headlessSpy) { s.readyErr = &win.ModalDialogError{WindowID: 101, DialogID: 202} },
			"window 101 has a modal dialog open (window 202); act on the dialog instead; nothing was sent", "get-window-state 202", true},
		{"stale index", func(s *headlessSpy) {
			s.hClickErr = fmt.Errorf("%w: element 2 is gone", uia.ErrStale)
		}, "element 2 is not available in the cached Window State for window 101; nothing was sent", "fresh Element Indexes", false},
		{"no state", func(s *headlessSpy) { s.hClickErr = fmt.Errorf("%w: window 101", uia.ErrNoState) },
			"no cached Window State is available for window 101; nothing was sent", "get-window-state 101", false},
		{"disabled", func(s *headlessSpy) { s.hClickErr = uia.ErrDisabled },
			"the Element is disabled; nothing was sent", "disabled", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newHeadlessSpy()
			tc.setup(spy)
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
			if cmdErr == nil || cmdErr.Msg != tc.wantMsg {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantMsg)
			}
			if !strings.Contains(cmdErr.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to mention %q", cmdErr.Hint, tc.wantHint)
			}
			if strings.HasPrefix(tc.name, "minimized") && !strings.Contains(cmdErr.Hint, "--headed") {
				t.Errorf("hint = %q, want the --headed alternative", cmdErr.Hint)
			}
			if tc.noArm {
				if b := spy.esc.brackets(); len(b) != 0 {
					t.Errorf("a refusal before winuse acts must arm nothing: %v", b)
				}
				if containsStep(spy.recorded(), "headless-click") {
					t.Error("a refused Window must not reach the pattern call")
				}
			}
			assertNoV1(t, spy.recorded())
		})
	}

	// The Secondary Action the Element does not offer, and set-value's
	// not-settable refusal, are pre-call refusals too.
	spy := newHeadlessSpy()
	spy.hSecErr = &uia.UnavailableActionError{Action: "Expand", Index: 4, Offered: []string{"Toggle"}}
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", secondaryArgs("101", 4, "Expand"))
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, "available actions: Toggle; nothing was sent") {
		t.Errorf("unavailable action: %v", cmdErr)
	}
	spy = newHeadlessSpy()
	spy.hValueErr = uia.ErrNotSettable
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("101", 3, "x"))
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, "; nothing was sent") || !strings.Contains(cmdErr.Hint, "type-text") {
		t.Errorf("not settable: %v", cmdErr)
	}

	// A toggle Element refuses a value that is not one of the six words,
	// and a toggle state it cannot read, before any Toggle call.
	spy = newHeadlessSpy()
	spy.hValueErr = fmt.Errorf("%w: maybe", uia.ErrToggleValue)
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("101", 3, "maybe"))
	if cmdErr == nil || cmdErr.Msg != "toggle value must be on, off, true, false, checked or unchecked: maybe; nothing was sent" ||
		!strings.Contains(cmdErr.Hint, "on or off") {
		t.Errorf("toggle value: %v", cmdErr)
	}
	spy = newHeadlessSpy()
	spy.hValueErr = uia.ErrToggleUnreadable
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("101", 3, "on"))
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, "; nothing was sent") || !strings.Contains(cmdErr.Hint, "Toggle") {
		t.Errorf("unreadable toggle state: %v", cmdErr)
	}
}

func TestHeadlessWriteAndVerifyFailureWording(t *testing.T) {
	// A failed write may have taken effect; a failed read-back was
	// delivered. Neither says nothing was sent, and both send the agent to
	// the Window State instead of repeating.
	spy := newHeadlessSpy()
	spy.hSecErr = &uia.WriteError{What: "toggling the Element", HR: -0x7fffbffb}
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", secondaryArgs("101", 4, "Toggle"))
	if cmdErr == nil || !strings.HasPrefix(cmdErr.Msg, "toggling the Element failed") || strings.Contains(cmdErr.Msg, "nothing was sent") {
		t.Fatalf("write failure: %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "may have taken effect") || !strings.Contains(cmdErr.Hint, "get-window-state 101") {
		t.Errorf("hint = %q, want the may-have-taken-effect wording", cmdErr.Hint)
	}
	if containsStep(spy.recorded(), "cursor-click") {
		t.Error("an undelivered write gets no click effect")
	}

	spy = newHeadlessSpy()
	spy.hSecErr = &uia.VerifyError{Action: "Expand", Expected: "expanded", Observed: "collapsed"}
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", secondaryArgs("101", 4, "Expand"))
	if cmdErr == nil || cmdErr.Msg != "Expand was delivered but the Element reads collapsed afterwards, not expanded" {
		t.Fatalf("verify failure: %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "the Action was delivered; do not repeat it, read the Window State") {
		t.Errorf("hint = %q, want the do-not-repeat wording", cmdErr.Hint)
	}
	if !containsStep(spy.recorded(), "cursor-click") {
		t.Error("a delivered Action whose read-back failed still gets the click effect")
	}

	// A password read-back names no value (uia withholds both sides).
	spy = newHeadlessSpy()
	spy.hValueErr = &uia.VerifyError{Action: "set-value", Expected: "the value that was set"}
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("101", 3, "hunter2"))
	if cmdErr == nil || strings.Contains(cmdErr.Msg+cmdErr.Hint, "hunter2") {
		t.Errorf("password mismatch must not echo the value: %v", cmdErr)
	}
}

func TestHeadlessCursorOrderAndBestEffort(t *testing.T) {
	// A bounds read failure skips the cursor but still acts; the act
	// reports its own outcome.
	spy := newHeadlessSpy()
	spy.boundsErr = errors.New("bounds unavailable")
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("click with unreadable bounds: %q, %v", out, cmdErr)
	}
	if got := spy.recorded(); !equalStrings(got, []string{"locked", "integrity", "ready", "bounds", "headless-click"}) {
		t.Errorf("steps = %v, want the act without the cursor", got)
	}

	// A stale Element: the bounds read fails, the act reports stale.
	spy = newHeadlessSpy()
	spy.boundsErr = fmt.Errorf("%w: gone", uia.ErrStale)
	spy.hClickErr = fmt.Errorf("%w: gone", uia.ErrStale)
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "is not available in the cached Window State") {
		t.Errorf("stale: %v, want the act's real error", cmdErr)
	}

	// A cursor that panics never fails the Action.
	spy = newHeadlessSpy()
	spy.cursor.panics = true
	if out, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left")); cmdErr != nil || out != "ok\n" {
		t.Errorf("click with a failing cursor: %q, %v", out, cmdErr)
	}

	// No cursor wired: no bounds read at all.
	spy = newHeadlessSpy()
	spy.svc.Cursor = nil
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left")); cmdErr != nil {
		t.Fatalf("click without a cursor: %v", cmdErr.Error())
	}
	if containsStep(spy.recorded(), "bounds") {
		t.Errorf("steps = %v: no cursor, no bounds read", spy.recorded())
	}
}

func TestHeadlessEscStopsWithoutSwallowing(t *testing.T) {
	// A press during the cursor's travel stops before the pattern call:
	// exit 130, nothing sent.
	spy := newHeadlessSpy()
	spy.cursor.esc = spy.esc
	spy.esc.pressOn = "travel"
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc || cmdErr.Msg != escStopMessage {
		t.Fatalf("press during travel: %v, want the exit-130 stop", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "nothing was sent") {
		t.Errorf("hint = %q, want nothing was sent", cmdErr.Hint)
	}
	if containsStep(spy.recorded(), "headless-click") {
		t.Error("a stop before the act must not call the pattern")
	}
	if b := spy.esc.brackets(); !equalStrings(b, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want only the passive scope (never swallowing)", b)
	}

	// A press during the act outranks its outcome, like v1.
	spy = newHeadlessSpy()
	spy.esc.pressOn = "act"
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("101", 3, "x"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc || cmdErr.Hint != escStopHint {
		t.Fatalf("press during the act: %v, want the plain exit-130 stop", cmdErr)
	}

	// An Esc service without the passive half (v1's fake) still stops a
	// Headless Action through its latch.
	spy = newHeadlessSpy()
	v1 := &escFake{}
	spy.svc.Esc = v1
	spy.svc.Ready = func(hwnd uint64) error { v1.Arm(); v1.press(); return nil } // the latch is set before the travel
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc {
		t.Errorf("v1 Esc fake: %v, want the stop", cmdErr)
	}
}

func TestBatchScopeAllHeadlessIsPassiveWithNoBanner(t *testing.T) {
	spy := newHeadlessSpy()
	ov := &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("set-value", protocol.SetValueArgs{ElementIndex: intPtrBatch(3), Value: "x"}),
			stepOf("perform-secondary-action", protocol.SecondaryActionArgs{ElementIndex: intPtrBatch(4), Action: "Toggle"}),
		},
	})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("batch: %q, %v", out, cmdErr)
	}
	assertNoV1(t, spy.recorded())
	b := spy.esc.brackets()
	if containsStep(b, "arm") || len(b) == 0 || b[0] != "arm-passive" || b[len(b)-1] != "disarm-passive" {
		t.Errorf("Esc brackets = %v, want one passive scope around passive steps", b)
	}
	for _, h := range []string{"headless-click", "headless-set-value", "headless-secondary"} {
		if countSteps(spy.recorded(), h) != 1 {
			t.Errorf("%s ran %d times, want 1: %v", h, countSteps(spy.recorded(), h), spy.recorded())
		}
	}
}

func TestBatchScopeWithAHeadedStepSwallowsWithTheBanner(t *testing.T) {
	spy := newHeadlessSpy()
	ov := &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello"}),
		},
	})
	if cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	steps := spy.recorded()
	if b := spy.esc.brackets(); len(b) == 0 || b[0] != "arm" || b[len(b)-1] != "disarm" {
		t.Errorf("Esc brackets = %v, want v1's swallowing scope around the whole run", b)
	}
	if len(steps) == 0 || !strings.HasPrefix(steps[0], "overlay-show(") || steps[len(steps)-1] != "overlay-hide" {
		t.Errorf("steps = %v, want the banner around the whole run", steps)
	}
	// The click step itself still ran Headless inside the Batch.
	if !containsStep(steps, "headless-click") || countSteps(steps, "activate") != 1 {
		t.Errorf("steps = %v, want a Headless click and one activation (type-text)", steps)
	}
}

func TestBatchHeadedRunsEveryStepV1(t *testing.T) {
	spy := newHeadlessSpy()
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Headed: true,
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("set-value", protocol.SetValueArgs{ElementIndex: intPtrBatch(3), Value: "x"}),
			stepOf("perform-secondary-action", protocol.SecondaryActionArgs{ElementIndex: intPtrBatch(4), Action: "Toggle"}),
		},
	})
	if cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	want := []string{
		"locked", "integrity", "activate", "point", "click",
		"locked", "integrity", "activate", "set-value",
		"locked", "integrity", "activate", "secondary",
	}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want every step v1: %v", got, want)
	}
	if b := spy.esc.brackets(); len(b) == 0 || b[0] != "arm" || containsStep(b, "arm-passive") {
		t.Errorf("Esc brackets = %v, want only swallowing scopes", b)
	}
}

func TestBatchHeadedInjectsOnlyWhereTheFlagExists(t *testing.T) {
	got := map[string]json.RawMessage{}
	record := func(kind string) Command {
		return func(ctx context.Context, args json.RawMessage) (string, *Error) {
			got[kind] = args
			return okOutput, nil
		}
	}
	cmds := map[string]Command{}
	for _, k := range []string{"click", "set-value", "perform-secondary-action", "type-text", "activate-window"} {
		cmds[k] = record(k)
	}
	raw, err := json.Marshal(protocol.BatchArgs{
		Window: "101",
		Headed: true,
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("set-value", protocol.SetValueArgs{ElementIndex: intPtrBatch(3), Value: "x"}),
			stepOf("perform-secondary-action", protocol.SecondaryActionArgs{ElementIndex: intPtrBatch(4), Action: "Toggle"}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "hi"}),
			stepOf("activate-window", nil),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, cmdErr := batch(cmds, Services{Windows: func() ([]win.WindowInfo, error) { return fakeWindows(), nil }})(context.Background(), raw); cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	for kind, args := range got {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(args, &fields); err != nil {
			t.Fatal(err)
		}
		_, has := fields["headed"]
		if has != takesHeaded(kind) {
			t.Errorf("%s: headed injected = %v, want %v (%s)", kind, has, takesHeaded(kind), args)
		}
		if has && string(fields["headed"]) != "true" {
			t.Errorf("%s: headed = %s, want true", kind, fields["headed"])
		}
	}
}

func TestCaptureHidesTheAgentCursor(t *testing.T) {
	// Cursor only: wrapped and hidden during the capture.
	cur := &cursorSpy{share: func(string) {}}
	var hiddenAtTake bool
	method := captureFunc(func(hwnd uint64) (shot.Frame, error) {
		hiddenAtTake = cur.isHidden()
		return shot.Frame{}, nil
	})
	methods := captureMethodsHiding(nil, cur, method)
	if _, wrapped := methods[0].(hiddenDuringCapture); !wrapped {
		t.Fatal("with a cursor wired, every method must be wrapped")
	}
	if _, err := methods[0].CaptureWindow(101); err != nil {
		t.Fatal(err)
	}
	if !hiddenAtTake || cur.isHidden() {
		t.Errorf("hidden during capture = %v, after = %v; want hidden, then restored", hiddenAtTake, cur.isHidden())
	}

	// Both: the overlay's bracket wraps the cursor's.
	var steps []string
	ov := &overlaySpy{share: func(s string) { steps = append(steps, s) }}
	cur = &cursorSpy{share: func(s string) { steps = append(steps, s) }}
	wrapped := hiddenDuringCapture{overlay: ov, cursor: cur, method: method}
	if _, err := wrapped.CaptureWindow(101); err != nil {
		t.Fatal(err)
	}
	want := []string{"overlay-capture-hide", "cursor-capture-hide", "cursor-capture-restore", "overlay-capture-restore"}
	if !equalStrings(steps, want) {
		t.Errorf("capture brackets = %v, want %v", steps, want)
	}
}

// captureFunc adapts a function to shot.Method.
type captureFunc func(hwnd uint64) (shot.Frame, error)

func (f captureFunc) Name() string                                  { return "func" }
func (f captureFunc) CaptureWindow(hwnd uint64) (shot.Frame, error) { return f(hwnd) }

func TestCursorLabel(t *testing.T) {
	for _, tc := range []struct{ verb, name, want string }{
		{"click", "Save", `click "Save"`},
		{"set value", "Search  box\n", `set value "Search box"`},
		{"toggle", "", "toggle"},
		{"scroll down", "   ", "scroll down"},
	} {
		if got := cursorLabel(tc.verb, tc.name); got != tc.want {
			t.Errorf("cursorLabel(%q, %q) = %q, want %q", tc.verb, tc.name, got, tc.want)
		}
	}
}
