package helper

// The pointer Action commands' offline tests (ticket 10): scroll, drag
// and hover. The commands' own work is coordinate mapping (through the
// same recorded Mapping the @x,y click uses) and argument validation —
// both pure, asserted exactly here. Everything Win32 (activation,
// cursor moves, wheel/button injection) is faked through Services; no
// desktop is touched. The refusals share the Action pipeline's contract
// with click's tests (actions_test.go), so these pin only what differs:
// the no-screenshot failures name the right verb, and the amounts keep
// Codex's signs all the way to the injected scroll.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// pointerSpy fakes the pointer services over the standard fake windows
// and the real shot.Store: a configurable frame origin (the anchor
// @x,y references re-read at Action time) and recording Scroll/Drag/
// Hover implementations.
type pointerSpy struct {
	svc Services

	origin    image.Point
	originErr error

	locked      bool
	activateErr error
	activations int

	scrollErr error
	dragErr   error
	hoverErr  error

	scrolled [5]int32 // target, x, y, dx, dy
	dragged  [5]int32 // target, fx, fy, tx, ty
	hovered  [3]int32 // target, x, y
}

func newPointerSpy(t *testing.T) *pointerSpy {
	t.Helper()
	s := &pointerSpy{origin: image.Point{X: 100, Y: 200}}
	s.svc = testServices(&fakeTrees{trees: map[uint64]*wstate.Tree{101: cannedTree()}})
	s.svc.Shots = shot.NewStore()
	// A default synthetic capture (800×470 at (100, 200)); set before
	// CommandsWith, which copies Services by value and defaults a nil
	// Capture to the real capture chain.
	s.svc.Capture = func(hwnd uint64) (shot.Frame, error) {
		return syntheticFrame(800, 470, 100, 200), nil
	}
	s.svc.Locked = func() (bool, string) { return s.locked, "" }
	s.svc.Integrity = func(win.WindowInfo) error { return nil }
	s.svc.Activate = func(hwnd uint64) error { s.activations++; return s.activateErr }
	s.svc.FrameOrigin = func(hwnd uint64) (image.Point, error) {
		if s.originErr != nil {
			return image.Point{}, s.originErr
		}
		return s.origin, nil
	}
	s.svc.Scroll = func(target uint64, x, y, dx, dy int32) error {
		s.scrolled = [5]int32{int32(target), x, y, dx, dy}
		return s.scrollErr
	}
	s.svc.Drag = func(target uint64, fx, fy, tx, ty int32, stopped func() bool) error {
		s.dragged = [5]int32{int32(target), fx, fy, tx, ty}
		return s.dragErr
	}
	s.svc.Hover = func(target uint64, x, y int32) error {
		s.hovered = [3]int32{int32(target), x, y}
		return s.hoverErr
	}
	return s
}

// recordScreenshot takes a screenshot of window 101 so the spy's Store
// holds a Mapping to resolve @x,y references against (the default
// 800×470 synthetic frame, recorded at the frame's own anchor).
func (s *pointerSpy) recordScreenshot(t *testing.T, cmds map[string]Command) {
	t.Helper()
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("recording screenshot: %v", cmdErr.Error())
	}
}

func TestScrollMapsPointAndKeepsCodexSigns(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)

	// The window moved since the screenshot: the anchor is re-read at
	// Action time, exactly like the @x,y click.
	s.origin = image.Point{X: 150, Y: 260}
	out, cmdErr := runJSON(t, cmds, "scroll", protocol.ScrollArgs{
		Window: "101", X: 400, Y: 235, ScrollX: -240, ScrollY: 600,
	})
	if cmdErr != nil {
		t.Fatalf("scroll: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	// Screenshot (400, 235) is the window-space (400, 235) at scale 1;
	// with the frame bounds now at (150, 260) the wheel lands at screen
	// (550, 495). The amounts keep the agent-facing signs (positive dy
	// scrolls down); the win layer flips them to wheel semantics.
	if s.scrolled != [5]int32{101, 550, 495, -240, 600} {
		t.Errorf("scrolled %+v, want target 101 at (550, 495) by (-240, 600)", s.scrolled)
	}
}

func TestScrollHalfScaleFootprintCentre(t *testing.T) {
	s := newPointerSpy(t)
	// Set before CommandsWith (the services are copied by value): a
	// 3136×1960 window over the cap, so the scale is exactly 0.5.
	s.svc.Capture = func(hwnd uint64) (shot.Frame, error) {
		return syntheticFrame(3136, 1960, 100, 200), nil
	}
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	s.origin = image.Point{X: 0, Y: 0}
	_, cmdErr = runJSON(t, cmds, "scroll", protocol.ScrollArgs{Window: "101", X: 400, Y: 300, ScrollY: 360})
	if cmdErr != nil {
		t.Fatalf("scroll: %v", cmdErr.Error())
	}
	// Scale 0.5: screenshot pixel (400, 300) aims at the physical
	// footprint centre floor((400+0.5)/0.5) = 801 of the window — the
	// same pixel-centre rounding the @x,y click tests pin.
	if s.scrolled[1] != 801 || s.scrolled[2] != 601 {
		t.Errorf("scrolled at (%d, %d), want (801, 601) (footprint centre at half scale)", s.scrolled[1], s.scrolled[2])
	}
}

func TestScrollRefusals(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)

	cases := []struct {
		name    string
		args    protocol.ScrollArgs
		wantMsg string
		noInput bool
	}{
		{
			name:    "no screenshot recorded",
			args:    protocol.ScrollArgs{Window: "202", X: 10, Y: 10, ScrollY: 120},
			wantMsg: "no screenshot is available for window 202",
		},
		{
			name:    "point outside the screenshot",
			args:    protocol.ScrollArgs{Window: "101", X: 800, Y: 235, ScrollY: 120},
			wantMsg: "point @800,235 is outside the latest screenshot of window 101 (800x470 px)",
		},
		{
			name:    "negative point",
			args:    protocol.ScrollArgs{Window: "101", X: -1, Y: 10, ScrollY: 120},
			wantMsg: "scroll: the @x,y coordinates must be non-negative integers",
			noInput: true,
		},
		{
			name:    "both amounts zero",
			args:    protocol.ScrollArgs{Window: "101", X: 10, Y: 10},
			wantMsg: "scroll: at least one scroll amount must be non-zero",
			noInput: true,
		},
		{
			name:    "amount too large",
			args:    protocol.ScrollArgs{Window: "101", X: 10, Y: 10, ScrollY: maxScrollUnits + 1},
			wantMsg: "scroll amounts must be within",
			noInput: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := s.activations
			_, cmdErr := runJSON(t, cmds, "scroll", tc.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.wantMsg) {
				t.Fatalf("message = %v, want it to contain %q", cmdErr, tc.wantMsg)
			}
			if cmdErr.Exit != 1 {
				t.Errorf("exit = %d, want 1", cmdErr.Exit)
			}
			if tc.noInput && s.activations != before {
				t.Errorf("an argument failure must not activate anything; %d activations", s.activations-before)
			}
		})
	}

	// The locked desktop refuses before activation, like every Action.
	s.locked = true
	_, cmdErr := runJSON(t, cmds, "scroll", protocol.ScrollArgs{Window: "101", X: 10, Y: 10, ScrollY: 120})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "Windows desktop is locked") {
		t.Fatalf("want the locked-desktop refusal, got %v", cmdErr)
	}
}

// TestScrollWithoutScreenshot uses a spy that never recorded a mapping:
// the failure names scrolling (not clicking) in its recovery hint.
func TestScrollWithoutScreenshotNamesTheVerb(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "scroll", protocol.ScrollArgs{Window: "101", X: 10, Y: 10, ScrollY: 120})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "no screenshot is available for window 101") {
		t.Fatalf("want no-screenshot failure, got %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "scroll @x,y") {
		t.Errorf("hint = %q, want it to name scrolling @x,y", cmdErr.Hint)
	}
}

func TestScrollMapsInputFailures(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)

	s.scrollErr = &win.OcclusionError{X: 550, Y: 495, OverTitle: "Something Else"}
	_, cmdErr := runJSON(t, cmds, "scroll", protocol.ScrollArgs{Window: "101", X: 400, Y: 235, ScrollY: 120})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `is over "Something Else", not the target Window`) {
		t.Fatalf("want the occlusion failure, got %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "covers") {
		t.Errorf("hint = %q, want the covering-window hint", cmdErr.Hint)
	}
}

func TestDragMapsBothPoints(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)
	s.origin = image.Point{X: 150, Y: 260}

	out, cmdErr := runJSON(t, cmds, "drag", protocol.DragArgs{
		Window: "101", FromX: 100, FromY: 50, ToX: 400, ToY: 235,
	})
	if cmdErr != nil {
		t.Fatalf("drag: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	// Window-space (100, 50) and (400, 235) plus the fresh anchor
	// (150, 260): the drag runs between screen (250, 310) and (550, 495).
	if s.dragged != [5]int32{101, 250, 310, 550, 495} {
		t.Errorf("dragged %+v, want 101 from (250, 310) to (550, 495)", s.dragged)
	}
}

func TestDragRefusals(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)

	// The to point is outside the screenshot: no drag starts.
	_, cmdErr := runJSON(t, cmds, "drag", protocol.DragArgs{
		Window: "101", FromX: 10, FromY: 10, ToX: 800, ToY: 10,
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "point @800,10 is outside the latest screenshot of window 101") {
		t.Fatalf("want out-of-screenshot failure for the to point, got %v", cmdErr)
	}

	// Negative points are usage errors.
	_, cmdErr = runJSON(t, cmds, "drag", protocol.DragArgs{
		Window: "101", FromX: -1, FromY: 10, ToX: 10, ToY: 10,
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "non-negative") {
		t.Fatalf("want negative-point refusal, got %v", cmdErr)
	}
}

// TestDragWithoutScreenshotNamesTheVerb: the failure's recovery hint
// names dragging.
func TestDragWithoutScreenshotNamesTheVerb(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "drag", protocol.DragArgs{
		Window: "101", FromX: 10, FromY: 10, ToX: 20, ToY: 20,
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "no screenshot is available for window 101") {
		t.Fatalf("want no-screenshot failure, got %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "drag @x,y") {
		t.Errorf("hint = %q, want it to name dragging @x,y", cmdErr.Hint)
	}
}

func TestDragMapsInputFailures(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)

	// A drag whose end point is over another window refuses with the
	// covering window named, like a click.
	s.dragErr = &win.OcclusionError{X: 550, Y: 495}
	_, cmdErr := runJSON(t, cmds, "drag", protocol.DragArgs{
		Window: "101", FromX: 100, FromY: 50, ToX: 400, ToY: 235,
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "point (550, 495) is over another window") {
		t.Fatalf("want the occlusion failure, got %v", cmdErr)
	}
}

func TestHoverMapsPoint(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)
	s.origin = image.Point{X: 150, Y: 260}

	out, cmdErr := runJSON(t, cmds, "hover", protocol.HoverArgs{Window: "101", X: 200, Y: 42})
	if cmdErr != nil {
		t.Fatalf("hover: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	if s.hovered != [3]int32{101, 350, 302} {
		t.Errorf("hovered %+v, want 101 at (350, 302)", s.hovered)
	}
}

func TestHoverRefusals(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)

	// Out-of-screenshot points are refused.
	_, cmdErr := runJSON(t, cmds, "hover", protocol.HoverArgs{Window: "101", X: 800, Y: 42})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "outside the latest screenshot") {
		t.Fatalf("want out-of-screenshot failure, got %v", cmdErr)
	}

	// Malformed arguments are refused, not panicked on.
	_, cmdErr = CommandsWith(s.svc)["hover"](context.Background(), json.RawMessage(`not json`))
	if cmdErr == nil {
		t.Fatal("malformed arguments must be refused")
	}
}

// TestHoverWithoutScreenshotNamesTheVerb: the failure's recovery hint
// names hovering.
func TestHoverWithoutScreenshotNamesTheVerb(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "hover", protocol.HoverArgs{Window: "101", X: 10, Y: 10})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "no screenshot is available for window 101") {
		t.Fatalf("want no-screenshot failure, got %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "hover @x,y") {
		t.Errorf("hint = %q, want it to name hovering @x,y", cmdErr.Hint)
	}
}

func TestPointerWindowGone(t *testing.T) {
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)
	s.originErr = fmt.Errorf("%w (window 101): reading the extended frame bounds failed", win.ErrGone)

	for _, tc := range []struct {
		cmd  string
		args any
	}{
		{"scroll", protocol.ScrollArgs{Window: "101", X: 10, Y: 10, ScrollY: 120}},
		{"drag", protocol.DragArgs{Window: "101", FromX: 1, FromY: 1, ToX: 2, ToY: 2}},
		{"hover", protocol.HoverArgs{Window: "101", X: 10, Y: 10}},
	} {
		_, cmdErr := runJSON(t, cmds, tc.cmd, tc.args)
		if cmdErr == nil || !strings.Contains(cmdErr.Msg, "window is gone") {
			t.Fatalf("%s: want window-gone failure, got %v", tc.cmd, cmdErr)
		}
		if !strings.Contains(cmdErr.Hint, "list-windows") {
			t.Errorf("%s: hint = %q, want the list-windows hint", tc.cmd, cmdErr.Hint)
		}
	}
}

func TestPointerCommandsRegistered(t *testing.T) {
	cmds := Commands()
	for _, name := range []string{"scroll", "drag", "hover"} {
		if _, ok := cmds[name]; !ok {
			t.Errorf("%s missing from the command registry: %v", name, cmds)
		}
	}
	// The default services wire the real SendInput implementations.
	s := defaultServices(Services{})
	if s.Scroll == nil || s.Drag == nil || s.Hover == nil {
		t.Error("CommandsWith must default Scroll, Drag and Hover")
	}
}

func TestBatchRunsThePointerSteps(t *testing.T) {
	// Ticket 12: scroll, drag and hover are Batch steps, running through
	// the same pipeline and coordinate mapping their standalone forms use,
	// in order, on the Batch's one Window.
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)
	s.origin = image.Point{X: 150, Y: 260}

	out, cmdErr := runJSON(t, cmds, "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("scroll", protocol.ScrollArgs{X: 400, Y: 235, ScrollX: -240, ScrollY: 600}),
			stepOf("drag", protocol.DragArgs{FromX: 400, FromY: 235, ToX: 500, ToY: 300}),
			stepOf("hover", protocol.HoverArgs{X: 400, Y: 235}),
		},
	})
	if cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	// All three ran on window 101, at the screen points the screenshot
	// pixels map to with the frame bounds re-read at (150, 260):
	// (400, 235) -> (550, 495), (500, 300) -> (650, 560).
	if s.scrolled != [5]int32{101, 550, 495, -240, 600} {
		t.Errorf("scrolled %+v, want target 101 at (550, 495) by (-240, 600)", s.scrolled)
	}
	if s.dragged != [5]int32{101, 550, 495, 650, 560} {
		t.Errorf("dragged %+v, want target 101 from (550, 495) to (650, 560)", s.dragged)
	}
	if s.hovered != [3]int32{101, 550, 495} {
		t.Errorf("hovered %+v, want target 101 at (550, 495)", s.hovered)
	}
	// Each step activated its Window once: three activations (plus the
	// one the recorded screenshot's capture performed).
	if s.activations != 4 {
		t.Errorf("activations = %d, want 4 (one per step plus the screenshot's)", s.activations)
	}
}

func TestDragStopsOnTheRequestDeadline(t *testing.T) {
	// Finding 10: the drag's interpolation checkpoint also returns on the
	// request's deadline, and the failure reports the budget (never a
	// fake Esc stop — a real Esc press still wins, checked first).
	s := newPointerSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)
	// The fake drag honors the checkpoint the command passes it, exactly
	// like win.MouseDrag's interpolation loop.
	stoppedByDeadline := false
	s.svc.Drag = func(target uint64, fx, fy, tx, ty int32, stopped func() bool) error {
		if stopped != nil && stopped() {
			stoppedByDeadline = true
			return win.ErrEscStopped
		}
		return nil
	}
	// Rebuild the registry over the changed services (they are copied by
	// value at CommandsWith).
	cmds = CommandsWith(s.svc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the budget is already spent
	c := cmds["drag"]
	raw, _ := json.Marshal(protocol.DragArgs{Window: "101", FromX: 10, FromY: 10, ToX: 400, ToY: 300})
	_, cmdErr := c(ctx, raw)
	if cmdErr == nil {
		t.Fatal("a drag past the request's deadline must fail")
	}
	if !strings.Contains(cmdErr.Msg, "the request's budget expired during the drag") {
		t.Fatalf("message = %q, want the budget-expired failure", cmdErr.Msg)
	}
	if !stoppedByDeadline {
		t.Error("the drag must receive the deadline-aware checkpoint")
	}
}
