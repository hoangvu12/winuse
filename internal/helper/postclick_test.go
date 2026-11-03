package helper

// Posted Clicks and posted wheel scrolls (headless-max ticket 12): the
// control under the point is found from the target Window down, only an
// allow-listed classic class takes the messages, and nothing goes through
// the v1 pipeline. Everything Win32 is faked.

import (
	"errors"
	"image"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
)

// postSpy is a headlessSpy with the posting seams faked: the control the
// point names, and the messages posted to it.
type postSpy struct {
	*headlessSpy
	child   uint64 // ChildAtPoint's answer
	class   string
	asked   []image.Point // screen points ChildAtPoint was asked about
	clicks  []postedClick
	wheels  []postedWheel
	postN   int // how many messages the fake reports posted (with postErr)
	postErr error
}

type postedClick struct {
	target uint64
	cx, cy int32
	button win.MouseButton
	count  int
}

type postedWheel struct {
	target         uint64
	sx, sy, dx, dy int32
}

func newPostSpy() *postSpy {
	p := &postSpy{headlessSpy: newHeadlessSpy(), child: 777, class: "Edit"}
	p.svc.Shots = shot.NewStore()
	p.svc.Shots.Record(agent.DefaultID, 101, shot.Mapping{Scale: 1, Width: 800, Height: 600, Coverage: image.Rect(0, 0, 800, 600)})
	p.svc.FrameOrigin = func(uint64) (image.Point, error) { return image.Point{X: 1000, Y: 500}, nil }
	p.svc.ChildAtPoint = func(top uint64, sx, sy int32) (uint64, int32, int32, string) {
		p.step("child-at-point")
		p.asked = append(p.asked, image.Point{X: int(sx), Y: int(sy)})
		return p.child, 12, 34, p.class
	}
	p.svc.PostClick = func(target uint64, cx, cy int32, button win.MouseButton, count int, mods win.Modifiers) (int, error) {
		p.step("post-click")
		p.clicks = append(p.clicks, postedClick{target, cx, cy, button, count})
		if p.postErr != nil {
			return p.postN, p.postErr
		}
		return 2 + 2*count, nil
	}
	p.svc.PostWheel = func(target uint64, sx, sy, dx, dy int32, mods win.Modifiers) (int, error) {
		p.step("post-wheel")
		p.wheels = append(p.wheels, postedWheel{target, sx, sy, dx, dy})
		return 1, p.postErr
	}
	return p
}

// noV1 fails if the v1 pipeline ran.
func (p *postSpy) noV1(t *testing.T, what string) {
	t.Helper()
	for _, step := range p.recorded() {
		if step == "activate" || step == "click" || step == "headless-click" {
			t.Errorf("%s ran %q: %v", what, step, p.recorded())
		}
	}
}

func TestPostedClickAtAPoint(t *testing.T) {
	p := newPostSpy()
	out, e := runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), MouseButton: "right", Post: true})
	if e != nil || out != postedClickOutput {
		t.Fatalf("click @40,50 --button right --post = %q, %v", out, e)
	}
	if len(p.asked) != 1 || p.asked[0] != (image.Point{X: 1040, Y: 550}) {
		t.Errorf("asked for the control at %v, want 1040,550 (the frame's origin plus the point)", p.asked)
	}
	if len(p.clicks) != 1 || p.clicks[0] != (postedClick{777, 12, 34, win.MouseRight, 1}) {
		t.Errorf("posted %+v, want a right click to control 777 at its client 12,34", p.clicks)
	}
	p.noV1(t, "a posted click")
}

func TestPostedDoubleClickOnAnElement(t *testing.T) {
	p := newPostSpy()
	two := 2
	out, e := runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", ElementIndex: &two, ClickCount: 2, Post: true})
	if e != nil || out != postedClickOutput {
		t.Fatalf("click 2 --count 2 --post = %q, %v", out, e)
	}
	// The Element's live bounds are 300,400-380,430: the centre is posted at.
	if len(p.asked) != 1 || p.asked[0] != (image.Point{X: 340, Y: 415}) {
		t.Errorf("asked for the control at %v, want the Element's centre 340,415", p.asked)
	}
	if len(p.clicks) != 1 || p.clicks[0].count != 2 || p.clicks[0].button != win.MouseLeft {
		t.Errorf("posted %+v, want a left double click", p.clicks)
	}
	p.noV1(t, "a posted double click")
}

func TestPostedClickRefusals(t *testing.T) {
	cases := []struct {
		name  string
		child uint64
		class string
		msg   string
	}{
		{"a Chromium surface", 778, "Chrome_RenderWidgetHostHWND", `a "Chrome_RenderWidgetHostHWND" control, not a classic control`},
		{"the Window itself", 101, "Notepad", "the Window itself"},
		{"no control", 0, "", "the Window itself"},
	}
	for _, tc := range cases {
		p := newPostSpy()
		p.child, p.class = tc.child, tc.class
		out, e := runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), MouseButton: "right", Post: true})
		wantHeadedRefusal(t, tc.name, out, e)
		if e == nil {
			continue
		}
		if !strings.Contains(e.Msg, tc.msg) {
			t.Errorf("%s: message %q, want %q", tc.name, e.Msg, tc.msg)
		}
		if !strings.HasSuffix(e.Hint, "winuse --headed click 101 @40,50 --button right") {
			t.Errorf("%s: hint %q, want the exact --headed retry", tc.name, e.Hint)
		}
		if len(p.clicks) != 0 {
			t.Errorf("%s: posted %+v", tc.name, p.clicks)
		}
	}

	// --post with --headed.
	p := newPostSpy()
	_, e := runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Post: true, Headed: true})
	if e == nil || !strings.Contains(e.Msg, "--post and --headed cannot be combined") || len(p.asked) != 0 {
		t.Errorf("--post --headed = %v, asked %v", e, p.asked)
	}

	// A locked desktop is refused before the control is looked up.
	p = newPostSpy()
	p.locked, p.lockedDesktop = true, "Winlogon"
	_, e = runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Post: true})
	if e == nil || !strings.HasSuffix(e.Msg, "; nothing was sent") || len(p.asked) != 0 {
		t.Errorf("a posted click on a locked desktop = %v, asked %v", e, p.asked)
	}

	// No screenshot: nothing was sent.
	p = newPostSpy()
	p.svc.Shots = shot.NewStore()
	_, e = runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Post: true})
	if e == nil || !strings.Contains(e.Msg, "no screenshot") || !strings.HasSuffix(e.Msg, "; nothing was sent") {
		t.Errorf("a posted click without a screenshot = %v", e)
	}
}

func TestPostedClickFailures(t *testing.T) {
	p := newPostSpy()
	p.postN, p.postErr = 0, errors.New("PostMessage failed")
	_, e := runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Post: true})
	if e == nil || !strings.HasSuffix(e.Msg, "; nothing was sent") {
		t.Errorf("nothing posted = %v, want nothing was sent", e)
	}
	p = newPostSpy()
	p.postN, p.postErr = 2, errors.New("PostMessage failed after 2 of 4 messages")
	_, e = runCommand(t, CommandsWith(p.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Post: true})
	if e == nil || strings.Contains(e.Msg, "nothing was sent") || !strings.Contains(e.Hint, "may have taken effect") {
		t.Errorf("a partial post = %v, want may have taken effect", e)
	}
}

func TestPostedScroll(t *testing.T) {
	p := newPostSpy()
	p.class = "ListBox"
	out, e := runCommand(t, CommandsWith(p.svc), "scroll", protocol.ScrollArgs{Window: "101", X: 5, Y: 6, ScrollX: -120, ScrollY: 240, Post: true})
	if e != nil || out != postedScrollOutput {
		t.Fatalf("scroll @5,6 -120 240 --post = %q, %v", out, e)
	}
	if len(p.wheels) != 1 || p.wheels[0] != (postedWheel{777, 1005, 506, -120, 240}) {
		t.Errorf("posted %+v, want the wheel to control 777 at the screen point 1005,506", p.wheels)
	}
	if containsString(p.recorded(), "headless-scroll") || containsString(p.recorded(), "activate") {
		t.Errorf("a posted scroll ran %v", p.recorded())
	}

	// An Element form takes no --post.
	p = newPostSpy()
	two := 2
	_, e = runCommand(t, CommandsWith(p.svc), "scroll", protocol.ScrollArgs{Window: "101", ElementIndex: &two, Direction: "down", Post: true})
	if e == nil || !strings.Contains(e.Msg, "--post scrolls at an @x,y point") {
		t.Errorf("scroll 2 down --post = %v", e)
	}
}

func TestBatchPostedStepsArePassive(t *testing.T) {
	p := newPostSpy()
	ov := &overlaySpy{share: p.step}
	p.svc.Overlay = ov
	out, e := runCommand(t, CommandsWith(p.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{Point: pt(40, 50), ClickCount: 2, Post: true}),
		stepOf("scroll", protocol.ScrollArgs{X: 5, Y: 6, ScrollY: 120, Post: true}),
	}})
	if e != nil || out != "Action 1 (click): ok\n  Note: posted; the app may ignore posted clicks, read the Window State to check\n"+
		"Action 2 (scroll): ok\n  Note: posted; the app may ignore posted scrolls, read the Window State to check\n" {
		t.Fatalf("batch = %q, %v", out, e)
	}
	if hasStepPrefix(p.recorded(), "overlay-show") || containsString(p.recorded(), "activate") {
		t.Errorf("a Batch of posted steps went Headed: %v", p.recorded())
	}
	if len(p.clicks) != 1 || len(p.wheels) != 1 {
		t.Errorf("posted clicks %+v and wheels %+v, want one each", p.clicks, p.wheels)
	}

	// A headed Batch refuses a posted step before any step runs.
	p = newPostSpy()
	_, e = runCommand(t, CommandsWith(p.svc), "batch", protocol.BatchArgs{Window: "101", Headed: true, Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
		stepOf("click", protocol.ClickArgs{Point: pt(40, 50), Post: true}),
	}})
	if e == nil || !strings.Contains(e.Msg, "batch: Action 2 (click): click: --post and --headed cannot be combined") || len(p.recorded()) != 0 {
		t.Errorf("headed batch with a posted click = %v, steps %v", e, p.recorded())
	}
}
