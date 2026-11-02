package helper

// Element scroll's offline tests (headless-parity spec, "Element scroll"):
// the Headless route by steps and into view (no activation, no wheel, the
// Agent Cursor labels), the argument checks, the failure wording (nothing
// was sent, delivered, may have taken effect), the --headed forms through
// v1's pipeline (the wheel at the Element's point, steps x 120), the
// routing predicate and the Batch's scope. Everything element-side and
// Win32 is faked; no desktop is touched.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
)

// scrollRecord is what the headlessSpy's Element scroll fakes record and
// return.
type scrollRecord struct {
	index    int
	dir      uia.ScrollDirection
	steps    int
	err      error // ScrollElement's outcome
	intoErr  error // ScrollElementIntoView's outcome
	wheeled  []int32
	wheelErr error
}

// ScrollElement implements HeadlessActor.
func (s *headlessSpy) ScrollElement(ctx context.Context, hwnd uint64, index int, dir uia.ScrollDirection, steps int) error {
	s.step("headless-scroll")
	s.scroll.index, s.scroll.dir, s.scroll.steps = index, dir, steps
	s.esc.pressIf("act")
	return s.scroll.err
}

// ScrollElementIntoView implements HeadlessActor.
func (s *headlessSpy) ScrollElementIntoView(ctx context.Context, hwnd uint64, index int) error {
	s.step("into-view")
	s.scroll.index = index
	s.esc.pressIf("act")
	return s.scroll.intoErr
}

// newScrollSpy is a headlessSpy whose wheel is faked and recorded.
func newScrollSpy() *headlessSpy {
	s := newHeadlessSpy()
	s.svc.Scroll = func(target uint64, x, y, dx, dy int32) error {
		s.step("wheel")
		s.scroll.wheeled = []int32{int32(target), x, y, dx, dy}
		return s.scroll.wheelErr
	}
	return s
}

func elementScroll(index int, dir string, steps ...int) protocol.ScrollArgs {
	a := protocol.ScrollArgs{Window: "101", ElementIndex: intPtrBatch(index), Direction: dir}
	if len(steps) > 0 {
		a.Steps = intPtrBatch(steps[0])
	}
	return a
}

func TestHeadlessElementScrollBySteps(t *testing.T) {
	spy := newScrollSpy()
	spy.boundsName = "List"
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "scroll", elementScroll(2, "down"))
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("scroll: %q, %v", out, cmdErr)
	}
	want := []string{"locked", "integrity", "ready", "bounds", "cursor-travel", "headless-scroll", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if containsStep(spy.recorded(), "wheel") {
		t.Error("a Headless scroll must not touch the wheel")
	}
	if spy.scroll.index != 2 || spy.scroll.dir != uia.ScrollDown || spy.scroll.steps != uia.DefaultScrollSteps {
		t.Errorf("scrolled %+v, want element 2 down by the default 3", spy.scroll)
	}
	if got := spy.cursor.labels; len(got) != 1 || got[0] != `scroll down "List"` {
		t.Errorf("labels = %q, want scroll down \"List\"", got)
	}
	if b := spy.esc.brackets(); !equalStrings(b, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want one passive scope", b)
	}

	// Steps, a reference, and a direction in any case.
	spy = newScrollSpy()
	spy.refIndex = 9
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "scroll",
		protocol.ScrollArgs{Window: "101", Ref: "name:Files", Direction: "LEFT", Steps: intPtrBatch(50)})
	if cmdErr != nil {
		t.Fatalf("scroll by reference: %v", cmdErr.Error())
	}
	if spy.scroll.index != 9 || spy.scroll.dir != uia.ScrollLeft || spy.scroll.steps != 50 {
		t.Errorf("scrolled %+v, want element 9 left by 50", spy.scroll)
	}
}

func TestHeadlessElementScrollIntoView(t *testing.T) {
	spy := newScrollSpy()
	spy.boundsName = "Row 40"
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "scroll", elementScroll(5, "into-view"))
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("into-view: %q, %v", out, cmdErr)
	}
	want := []string{"locked", "integrity", "ready", "bounds", "cursor-travel", "into-view", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if spy.scroll.index != 5 {
		t.Errorf("scrolled element %d into view, want 5", spy.scroll.index)
	}
	if got := spy.cursor.labels; len(got) != 1 || got[0] != `scroll into view "Row 40"` {
		t.Errorf("labels = %q, want scroll into view \"Row 40\"", got)
	}
}

func TestElementScrollArgumentChecks(t *testing.T) {
	cases := []struct {
		name string
		args protocol.ScrollArgs
		want string
	}{
		{"no direction", elementScroll(2, ""), "a direction is required"},
		{"bad direction", elementScroll(2, "sideways"), "the scroll direction must be up, down, left or right"},
		{"zero steps", elementScroll(2, "down", 0), "steps must be between 1 and 50"},
		{"too many steps", elementScroll(2, "down", 51), "steps must be between 1 and 50"},
		{"into-view with steps", elementScroll(2, "into-view", 2), "into-view takes no steps"},
		{"element and amounts", protocol.ScrollArgs{Window: "101", ElementIndex: intPtrBatch(2), Direction: "down", ScrollY: 120},
			"not both"},
		{"element and point", protocol.ScrollArgs{Window: "101", Ref: "id:list", Direction: "down", X: 4, Y: 4}, "not both"},
		{"index and reference", protocol.ScrollArgs{Window: "101", ElementIndex: intPtrBatch(2), Ref: "id:list", Direction: "down"},
			"not several"},
		{"negative index", elementScroll(-1, "down"), "non-negative"},
		{"direction without an element", protocol.ScrollArgs{Window: "101", X: 4, Y: 4, Direction: "down"},
			"needs an element"},
		{"steps without an element", protocol.ScrollArgs{Window: "101", X: 4, Y: 4, ScrollY: 120, Steps: intPtrBatch(2)},
			"needs an element"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newScrollSpy()
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "scroll", tc.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.want) {
				t.Fatalf("error = %v, want it to contain %q", cmdErr, tc.want)
			}
			if !strings.Contains(cmdErr.Hint, "usage: winuse scroll") {
				t.Errorf("hint = %q, want the usage", cmdErr.Hint)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("a usage failure must touch nothing: %v", got)
			}
		})
	}
}

func TestElementScrollFailureWording(t *testing.T) {
	cases := []struct {
		name      string
		args      protocol.ScrollArgs
		setup     func(*headlessSpy)
		wantMsg   string
		wantHint  string
		delivered bool // the ripple plays
	}{
		{
			name:     "no scrollable ancestor",
			args:     elementScroll(2, "down"),
			setup:    func(s *headlessSpy) { s.scroll.err = fmt.Errorf("%w vertically", uia.ErrNotScrollable) },
			wantMsg:  "neither the Element nor any of its ancestors scrolls vertically; nothing was sent",
			wantHint: "winuse --headed scroll 101 2 down",
		},
		{
			name:     "no ScrollItem",
			args:     elementScroll(2, "into-view"),
			setup:    func(s *headlessSpy) { s.scroll.intoErr = uia.ErrNoScrollItem },
			wantMsg:  uia.ErrNoScrollItem.Error() + "; nothing was sent",
			wantHint: "winuse scroll 101 2 down",
		},
		{
			name:     "stale element",
			args:     elementScroll(2, "up"),
			setup:    func(s *headlessSpy) { s.scroll.err = fmt.Errorf("%w: gone", uia.ErrStale) },
			wantMsg:  "element 2 is not available in the cached Window State for window 101; nothing was sent",
			wantHint: "fresh Element Indexes",
		},
		{
			name: "no movement",
			args: elementScroll(2, "down"),
			setup: func(s *headlessSpy) {
				s.scroll.err = &uia.VerifyError{Action: "scroll", Msg: "the Element did not scroll; it may already be at the end"}
			},
			wantMsg:   "the Element did not scroll; it may already be at the end",
			wantHint:  "do not repeat",
			delivered: true,
		},
		{
			name: "still offscreen",
			args: elementScroll(2, "into-view"),
			setup: func(s *headlessSpy) {
				s.scroll.intoErr = &uia.VerifyError{Action: "scroll into-view", Expected: "on screen", Observed: "offscreen"}
			},
			wantMsg:   "scroll into-view was delivered but the Element reads offscreen afterwards, not on screen",
			wantHint:  "do not repeat",
			delivered: true,
		},
		{
			name:     "a failed pattern call",
			args:     elementScroll(2, "right"),
			setup:    func(s *headlessSpy) { s.scroll.err = &uia.WriteError{What: "scrolling right", HR: -0x7fffbffb} },
			wantMsg:  "scrolling right failed (HRESULT 0x80004005)",
			wantHint: "may have taken effect",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newScrollSpy()
			tc.setup(spy)
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "scroll", tc.args)
			if cmdErr == nil || cmdErr.Msg != tc.wantMsg {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantMsg)
			}
			if !strings.Contains(cmdErr.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to contain %q", cmdErr.Hint, tc.wantHint)
			}
			if got := containsStep(spy.recorded(), "cursor-click"); got != tc.delivered {
				t.Errorf("ripple = %v, want %v", got, tc.delivered)
			}
		})
	}
}

func TestHeadedElementScrollWheelsAtTheElement(t *testing.T) {
	cases := []struct {
		dir    string
		steps  []int
		dx, dy int32
	}{
		{"down", nil, 0, 360},
		{"up", []int{1}, 0, -120},
		{"left", []int{2}, -240, 0},
		{"right", []int{50}, 6000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			spy := newScrollSpy()
			a := elementScroll(4, tc.dir, tc.steps...)
			a.Headed = true
			if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "scroll", a); cmdErr != nil {
				t.Fatalf("headed scroll: %v", cmdErr.Error())
			}
			want := []string{"locked", "integrity", "activate", "point", "wheel"}
			if got := spy.recorded(); !equalStrings(got, want) {
				t.Errorf("steps = %v, want %v", got, want)
			}
			if spy.pointIndex != 4 {
				t.Errorf("resolved element %d, want 4", spy.pointIndex)
			}
			wantWheel := []int32{101, spy.point.X, spy.point.Y, tc.dx, tc.dy}
			if !equalInt32s(spy.scroll.wheeled, wantWheel) {
				t.Errorf("wheel = %v, want %v", spy.scroll.wheeled, wantWheel)
			}
			if b := spy.esc.brackets(); len(b) == 0 || b[0] != "arm" {
				t.Errorf("Esc brackets = %v, want v1's swallowing scope", b)
			}
		})
	}

	// into-view: the ScrollItem call after activation, no wheel.
	spy := newScrollSpy()
	a := elementScroll(4, "into-view")
	a.Headed = true
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "scroll", a); cmdErr != nil {
		t.Fatalf("headed into-view: %v", cmdErr.Error())
	}
	if got, want := spy.recorded(), []string{"locked", "integrity", "activate", "into-view"}; !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}

func equalInt32s(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWheelDelta(t *testing.T) {
	for _, tc := range []struct {
		dir    uia.ScrollDirection
		steps  int
		dx, dy int32
	}{
		{uia.ScrollDown, 3, 0, 360},
		{uia.ScrollUp, 3, 0, -360},
		{uia.ScrollRight, 1, 120, 0},
		{uia.ScrollLeft, 50, -6000, 0},
	} {
		if dx, dy := wheelDelta(tc.dir, tc.steps); dx != tc.dx || dy != tc.dy {
			t.Errorf("wheelDelta(%s, %d) = (%d, %d), want (%d, %d)", tc.dir, tc.steps, dx, dy, tc.dx, tc.dy)
		}
	}
}

func TestHeadlessStepElementScroll(t *testing.T) {
	i := intPtrBatch(1)
	for _, tc := range []struct {
		name string
		args protocol.ScrollArgs
		want bool
	}{
		{"by steps", protocol.ScrollArgs{ElementIndex: i, Direction: "down"}, true},
		{"by reference with steps", protocol.ScrollArgs{Ref: "name:List", Direction: "up", Steps: intPtrBatch(5)}, true},
		{"into view", protocol.ScrollArgs{Ref: "id:row", Direction: "into-view"}, true},
		{"headed", protocol.ScrollArgs{ElementIndex: i, Direction: "down", Headed: true}, false},
		{"the point form", protocol.ScrollArgs{X: 4, Y: 4, ScrollY: 120}, false},
		{"bad direction", protocol.ScrollArgs{ElementIndex: i, Direction: "sideways"}, false},
		{"steps out of range", protocol.ScrollArgs{ElementIndex: i, Direction: "down", Steps: intPtrBatch(0)}, false},
		{"element and amounts", protocol.ScrollArgs{ElementIndex: i, Direction: "down", ScrollY: 120}, false},
	} {
		if got := headlessStep("scroll", batchStepArgs(tc.args), nil); got != tc.want {
			t.Errorf("%s: headlessStep = %v, want %v", tc.name, got, tc.want)
		}
	}
	if !takesHeaded("scroll") {
		t.Error("scroll has Element forms, so it takes the headed flag")
	}
}

func TestBatchElementScrollStepsKeepTheHeadlessScope(t *testing.T) {
	spy := newScrollSpy()
	ov := &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("scroll", protocol.ScrollArgs{ElementIndex: intPtrBatch(2), Direction: "down"}),
			stepOf("scroll", protocol.ScrollArgs{ElementIndex: intPtrBatch(3), Direction: "into-view"}),
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(3)}),
		},
	})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("batch: %q, %v", out, cmdErr)
	}
	assertNoV1(t, spy.recorded())
	if b := spy.esc.brackets(); containsStep(b, "arm") || len(b) == 0 || b[0] != "arm-passive" {
		t.Errorf("Esc brackets = %v, want the passive scope", b)
	}

	// The point form is a Headed step: the Batch swallows Esc.
	spy = newScrollSpy()
	_, _ = runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("scroll", protocol.ScrollArgs{ElementIndex: intPtrBatch(2), Direction: "down"}),
			stepOf("scroll", protocol.ScrollArgs{X: 4, Y: 4, ScrollY: 120}),
		},
	})
	if b := spy.esc.brackets(); len(b) == 0 || b[0] != "arm" {
		t.Errorf("Esc brackets = %v, want v1's swallowing scope", b)
	}

	// A headed Batch runs the Element scroll the v1 way.
	spy = newScrollSpy()
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Headed: true,
		Steps:  []protocol.Step{stepOf("scroll", protocol.ScrollArgs{ElementIndex: intPtrBatch(2), Direction: "down"})},
	}); cmdErr != nil {
		t.Fatalf("headed batch: %v", cmdErr.Error())
	}
	if got, want := spy.recorded(), []string{"locked", "integrity", "activate", "point", "wheel"}; !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}
