package helper

// The point forms resolved to an Element in the target Window
// (headless-max ticket 11): the point maps through the latest screenshot,
// the fake resolver names an Element, and the Headless route runs on it,
// never the v1 pipeline. Refusals send nothing and give the --headed retry.

import (
	"context"
	"encoding/json"
	"image"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
)

// pointSpy is a headlessSpy that also resolves points: it records the
// screen point and need it was asked for and answers hit or err.
type pointSpy struct {
	*headlessSpy
	hit     uia.PointHit
	err     error
	asked   []uia.Point
	need    uia.PointNeed
	action  string
	scrolls []scrollCall
}

type scrollCall struct {
	index int
	dir   uia.ScrollDirection
	steps int
}

func newPointSpy() *pointSpy {
	p := &pointSpy{headlessSpy: newHeadlessSpy(), hit: uia.PointHit{Index: 5, Role: "button", Name: "Save"}}
	p.svc.Actions = p
	// The latest screenshot of Window 101: scale 1, the Window's frame at
	// 1000,500 on screen.
	p.svc.Shots = shot.NewStore()
	p.svc.Shots.Record(agent.DefaultID, 101, shot.Mapping{Scale: 1, Width: 800, Height: 600, Coverage: image.Rect(0, 0, 800, 600)})
	p.svc.FrameOrigin = func(uint64) (image.Point, error) { return image.Point{X: 1000, Y: 500}, nil }
	return p
}

// ElementAtPoint implements PointResolver.
func (p *pointSpy) ElementAtPoint(ctx context.Context, hwnd uint64, x, y int32, need uia.PointNeed, action string) (uia.PointHit, error) {
	p.step("hit-test")
	p.asked = append(p.asked, uia.Point{X: x, Y: y})
	p.need, p.action = need, action
	return p.hit, p.err
}

// ScrollElement records every call (a scroll at a point can make two).
func (p *pointSpy) ScrollElement(ctx context.Context, hwnd uint64, index int, dir uia.ScrollDirection, steps int) error {
	p.scrolls = append(p.scrolls, scrollCall{index, dir, steps})
	return p.headlessSpy.ScrollElement(ctx, hwnd, index, dir, steps)
}

func pt(x, y int) *protocol.Point { return &protocol.Point{X: x, Y: y} }

func TestClickAtAPointClicksTheWindowsOwnElement(t *testing.T) {
	p := newPointSpy()
	out, e := runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50)})
	if e != nil {
		t.Fatalf("click @40,50: %v", e.Error())
	}
	if want := `ok (clicked element 5 button "Save" at @40,50)` + "\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if len(p.asked) != 1 || p.asked[0] != (uia.Point{X: 1040, Y: 550}) || p.need != uia.NeedClick {
		t.Errorf("hit-test asked %v for need %d, want 1040,550 (the frame's origin plus the point) for a click", p.asked, p.need)
	}
	if p.hIndex != 5 {
		t.Errorf("clicked element %d, want the resolved 5", p.hIndex)
	}
	for _, step := range p.recorded() {
		if step == "activate" || step == "click" {
			t.Errorf("the point click ran the v1 pipeline: %v", p.recorded())
		}
	}
}

func TestPointFormsRouteToTheirHeadlessActions(t *testing.T) {
	p := newPointSpy()
	p.hit = uia.PointHit{Index: 7, Role: "edit", Name: "Search"}
	out, e := runCommand(t, CommandsWith(p.svc), "set-value", protocol.SetValueArgs{Window: "101", Point: pt(10, 20), Value: "pw"})
	if e != nil || out != `ok (set the value of element 7 edit "Search" at @10,20)`+"\n" || p.hValue != "pw" || p.hIndex != 7 || p.need != uia.NeedValue {
		t.Errorf("set-value @10,20 = %q, %v; set %q on %d (need %d)", out, e, p.hValue, p.hIndex, p.need)
	}

	p = newPointSpy()
	p.hit = uia.PointHit{Index: 3, Role: "tree item"}
	out, e = runCommand(t, CommandsWith(p.svc), "perform-secondary-action", protocol.SecondaryActionArgs{Window: "101", Point: pt(1, 2), Action: "expand"})
	if e != nil || out != "ok (performed Expand on element 3 tree item at @1,2)\n" || p.hAction != "Expand" || p.action != "Expand" || p.need != uia.NeedAction {
		t.Errorf("perform-secondary-action @1,2 expand = %q, %v; action %q, asked for %q", out, e, p.hAction, p.action)
	}
}

func TestScrollAtAPointConvertsTheWheelToSteps(t *testing.T) {
	cases := []struct {
		dx, dy int
		want   []scrollCall
	}{
		{0, 120, []scrollCall{{5, uia.ScrollDown, 3}}},
		{0, -240, []scrollCall{{5, uia.ScrollUp, 6}}},
		{0, 40, []scrollCall{{5, uia.ScrollDown, 1}}}, // any amount scrolls
		{-120, 360, []scrollCall{{5, uia.ScrollDown, 9}, {5, uia.ScrollLeft, 3}}},
	}
	for _, tc := range cases {
		p := newPointSpy()
		out, e := runCommand(t, CommandsWith(p.svc), "scroll", protocol.ScrollArgs{Window: "101", X: 5, Y: 6, ScrollX: tc.dx, ScrollY: tc.dy})
		if e != nil || !strings.HasPrefix(out, `ok (scrolled element 5 button "Save" at @5,6)`) {
			t.Errorf("scroll %d %d = %q, %v", tc.dx, tc.dy, out, e)
		}
		if len(p.scrolls) != len(tc.want) {
			t.Fatalf("scroll %d %d made %v, want %v", tc.dx, tc.dy, p.scrolls, tc.want)
		}
		for i := range tc.want {
			if p.scrolls[i] != tc.want[i] {
				t.Errorf("scroll %d %d: call %d = %+v, want %+v", tc.dx, tc.dy, i, p.scrolls[i], tc.want[i])
			}
		}
	}

	// More than MaxScrollSteps per axis has no Element route: refused.
	p := newPointSpy()
	out, e := runCommand(t, CommandsWith(p.svc), "scroll", protocol.ScrollArgs{Window: "101", X: 5, Y: 6, ScrollY: 120 * 20})
	wantHeadedRefusal(t, "scroll @5,6 0 2400", out, e)
	if len(p.asked) != 0 || len(p.scrolls) != 0 {
		t.Errorf("a refused scroll hit-tested %v and scrolled %v", p.asked, p.scrolls)
	}
}

func TestPointRefusalsSendNothing(t *testing.T) {
	cases := []struct {
		name string
		err  error
		msg  string
		hint string
	}{
		{"the Window only", &uia.PointError{Kind: uia.PointWindowOnly, Index: -1, Other: -1},
			"only on the Window itself", "winuse --headed click 101 @40,50"},
		{"a canvas", &uia.PointError{Kind: uia.PointNoRoute, Index: 2, Other: -1, Role: "custom"},
			"element 2 (custom)", "winuse --headed click 101 @40,50"},
		{"an overlap", &uia.PointError{Kind: uia.PointAmbiguous, Index: 4, Other: 6},
			"two overlapping Elements, 4 and 6", "Element Index (4 or 6)"},
		{"no Window State", uia.ErrNoState, "no cached Window State", "get-window-state"},
	}
	for _, tc := range cases {
		p := newPointSpy()
		p.err = tc.err
		out, e := runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50)})
		if out != "" || e == nil {
			t.Fatalf("%s: got %q, %v; want a refusal", tc.name, out, e)
		}
		if !strings.Contains(e.Msg, tc.msg) || !strings.HasSuffix(e.Msg, "; nothing was sent") || !strings.Contains(e.Hint, tc.hint) {
			t.Errorf("%s: %q / %q; want %q and %q", tc.name, e.Msg, e.Hint, tc.msg, tc.hint)
		}
		for _, step := range p.recorded() {
			if step == "headless-click" || step == "activate" || step == "click" {
				t.Errorf("%s: a refused point click ran %v", tc.name, p.recorded())
			}
		}
	}

	// No screenshot: nothing was sent, and the hint asks for one.
	p := newPointSpy()
	p.svc.Shots = shot.NewStore()
	_, e := runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50)})
	if e == nil || !strings.Contains(e.Msg, "no screenshot") || !strings.HasSuffix(e.Msg, "; nothing was sent") || len(p.asked) != 0 {
		t.Errorf("click @40,50 without a screenshot = %v, asked %v", e, p.asked)
	}
}

func TestPointFormsThatStayHeadedOnly(t *testing.T) {
	cases := []struct {
		name, cmd string
		args      any
	}{
		{"a double click", "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), ClickCount: 2}},
		{"a right click", "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), MouseButton: "right"}},
		{"Raise", "perform-secondary-action", protocol.SecondaryActionArgs{Window: "101", Point: pt(40, 50), Action: "Raise"}},
	}
	for _, tc := range cases {
		p := newPointSpy()
		out, e := runCommand(t, CommandsWith(p.svc), tc.cmd, tc.args)
		wantHeadedRefusal(t, tc.name, out, e)
		if len(p.asked) != 0 {
			t.Errorf("%s: hit-tested %v", tc.name, p.asked)
		}
	}

	// With --headed, the point click is v1's real click.
	p := newPointSpy()
	raw, _ := json.Marshal(protocol.ClickArgs{Window: "101", Point: pt(40, 50), Headed: true})
	if _, e := CommandsWith(p.svc)["click"](context.Background(), raw); e != nil {
		t.Fatalf("--headed click @40,50: %v", e.Error())
	}
	if len(p.asked) != 0 || !containsString(p.recorded(), "activate") || !containsString(p.recorded(), "click") {
		t.Errorf("--headed click @40,50 steps %v, asked %v; want the v1 pipeline", p.recorded(), p.asked)
	}
}

func TestBatchPointStepsKeepTheHeadlessScope(t *testing.T) {
	p := newPointSpy()
	ov := &overlaySpy{share: p.step}
	p.svc.Overlay = ov
	out, e := runCommand(t, CommandsWith(p.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{Point: pt(40, 50)}),
		stepOf("scroll", protocol.ScrollArgs{X: 5, Y: 6, ScrollY: 120}),
	}})
	if e != nil || out != okOutput {
		t.Fatalf("batch = %q, %v", out, e)
	}
	if containsString(p.recorded(), "activate") || hasStepPrefix(p.recorded(), "overlay-show") {
		t.Errorf("a Batch of point steps went Headed: %v", p.recorded())
	}
	if got := p.esc.brackets(); len(got) == 0 || got[0] != "arm-passive" || containsString(got, "arm") {
		t.Errorf("Esc brackets = %v, want a passive scope", got)
	}
}

func TestWithNote(t *testing.T) {
	cases := []struct{ out, want string }{
		{"ok\n", "ok (n)\n"},
		{"ok (the App took the foreground)\n", "ok (n; the App took the foreground)\n"},
		{"ok\nNote: a new Window opened: 7\n", "ok (n)\nNote: a new Window opened: 7\n"},
		{"something else\n", "something else\n"},
	}
	for _, tc := range cases {
		if got := withNote(tc.out, "n"); got != tc.want {
			t.Errorf("withNote(%q) = %q, want %q", tc.out, got, tc.want)
		}
	}
}

func TestPointStepPredicate(t *testing.T) {
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	cases := []struct {
		kind string
		args json.RawMessage
		want bool
	}{
		{"click", raw(protocol.ClickArgs{Point: pt(1, 2)}), true},
		{"click", raw(protocol.ClickArgs{Point: pt(1, 2), ClickCount: 2}), false},
		{"click", raw(protocol.ClickArgs{Point: pt(1, 2), Headed: true}), false},
		{"click", raw(protocol.ClickArgs{ElementIndex: idx(2)}), false},
		{"set-value", raw(protocol.SetValueArgs{Point: pt(1, 2), Value: "x"}), true},
		{"perform-secondary-action", raw(protocol.SecondaryActionArgs{Point: pt(1, 2), Action: "toggle"}), true},
		{"perform-secondary-action", raw(protocol.SecondaryActionArgs{Point: pt(1, 2), Action: "raise"}), false},
		{"scroll", raw(protocol.ScrollArgs{X: 1, Y: 2, ScrollY: 120}), true},
		{"scroll", raw(protocol.ScrollArgs{X: 1, Y: 2, ScrollY: 120 * 17}), false},
		{"drag", raw(protocol.DragArgs{FromX: 1, ToX: 2}), false},
	}
	for _, tc := range cases {
		if got := pointStep(tc.kind, tc.args); got != tc.want {
			t.Errorf("pointStep(%s, %s) = %v, want %v", tc.kind, tc.args, got, tc.want)
		}
	}
}
