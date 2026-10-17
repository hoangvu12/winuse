package helper

// The @x,y Element Reference tests for set-value and
// perform-secondary-action (finding 8): the point is mapped through the
// Window's latest screenshot exactly like the coordinate click, and the
// element side acts on the Element under the mapped point. Everything
// Win32 and element-side is faked; no desktop is touched.

import (
	"image"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
)

// atPointSpy wires an actorSpy over the pointer tests' screenshot
// machinery: a recorded screenshot of window 101 (the synthetic 800x470
// frame anchored at (100, 200)) and a fresh anchor re-read at Action
// time, so the @x,y mapping is exercised for real.
type atPointSpy struct {
	*actorSpy
	origin image.Point
}

func newAtPointSpy(t *testing.T) *atPointSpy {
	t.Helper()
	base := newActorSpy()
	s := &atPointSpy{actorSpy: base, origin: image.Point{X: 150, Y: 260}}
	base.svc.Shots = shot.NewStore()
	base.svc.Capture = func(hwnd uint64) (shot.Frame, error) {
		return syntheticFrame(800, 470, 100, 200), nil
	}
	base.svc.FrameOrigin = func(hwnd uint64) (image.Point, error) { return s.origin, nil }
	return s
}

// recordScreenshot takes the screenshot the @x,y references resolve
// against (before CommandsWith copies the services).
func (s *atPointSpy) recordScreenshot(t *testing.T, cmds map[string]Command) {
	t.Helper()
	_, cmdErr := runCommand(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("recording screenshot: %v", cmdErr.Error())
	}
}

func TestSetValueAtPointMapsThroughTheScreenshot(t *testing.T) {
	s := newAtPointSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)

	out, cmdErr := runCommand(t, cmds, "set-value", protocol.SetValueArgs{
		Window: "101", Point: &protocol.Point{X: 400, Y: 235}, Value: "typed at a point",
	})
	if cmdErr != nil {
		t.Fatalf("set-value @x,y: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	// Screenshot (400, 235) is window-space (400, 235) at scale 1; the
	// frame bounds moved to (150, 260) since the screenshot, so the
	// Element is looked up under screen (550, 495) — the same mapping the
	// @x,y click pins.
	if s.valueAtX != 550 || s.valueAtY != 495 {
		t.Errorf("set value at screen (%d, %d), want (550, 495)", s.valueAtX, s.valueAtY)
	}
	if s.valueValue != "typed at a point" {
		t.Errorf("value = %q", s.valueValue)
	}
	if containsStep(s.recorded(), "set-value") {
		t.Error("the @x,y form must not run the index-based step")
	}
}

func TestPerformSecondaryActionAtPointMapsAndCanonicalizes(t *testing.T) {
	s := newAtPointSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)

	out, cmdErr := runCommand(t, cmds, "perform-secondary-action", protocol.SecondaryActionArgs{
		Window: "101", Point: &protocol.Point{X: 400, Y: 235}, Action: "toggle",
	})
	if cmdErr != nil {
		t.Fatalf("perform-secondary-action @x,y: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	if s.actionAtX != 550 || s.actionAtY != 495 {
		t.Errorf("acted at screen (%d, %d), want (550, 495)", s.actionAtX, s.actionAtY)
	}
	if s.actionPerformed != "Toggle" {
		t.Errorf("performed %q, want the canonical Toggle", s.actionPerformed)
	}
}

func TestElementAtPointFailuresShareTheMapping(t *testing.T) {
	// No screenshot: the shared no-screenshot failure names the verb.
	s := newAtPointSpy(t)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runCommand(t, cmds, "set-value", protocol.SetValueArgs{
		Window: "101", Point: &protocol.Point{X: 10, Y: 10}, Value: "x",
	})
	if cmdErr == nil || cmdErr.Msg != "no screenshot is available for window 101" {
		t.Fatalf("message = %v, want the shared no-screenshot failure", cmdErr)
	}
	if !containsAny(cmdErr.Hint, "get-window-state 101 --screenshot", "set-value @x,y") {
		t.Errorf("hint = %q, want the take-a-screenshot instruction naming set-value", cmdErr.Hint)
	}

	// Outside the screenshot: the shared outside failure.
	s.recordScreenshot(t, cmds)
	_, cmdErr = runCommand(t, cmds, "perform-secondary-action", protocol.SecondaryActionArgs{
		Window: "101", Point: &protocol.Point{X: 5000, Y: 10}, Action: "Toggle",
	})
	if cmdErr == nil || cmdErr.Msg != "point @5000,10 is outside the latest screenshot of window 101 (800x470 px)" {
		t.Fatalf("message = %v, want the outside-the-screenshot failure", cmdErr)
	}

	// The element side's failures keep their own mapping (the not-settable
	// refusal points at type-text, without an Element Index to name).
	s.setValueErr = uia.ErrNotSettable
	_, cmdErr = runCommand(t, cmds, "set-value", protocol.SetValueArgs{
		Window: "101", Point: &protocol.Point{X: 400, Y: 235}, Value: "x",
	})
	if cmdErr == nil || cmdErr.Msg != "Cannot set a value for an element that is not settable" {
		t.Fatalf("message = %v, want the not-settable refusal", cmdErr)
	}

	// Several forms at once is refused before anything runs.
	index := 2
	_, cmdErr = runCommand(t, cmds, "set-value", protocol.SetValueArgs{
		Window: "101", ElementIndex: &index, Point: &protocol.Point{X: 1, Y: 1}, Value: "x",
	})
	if cmdErr == nil || cmdErr.Msg != "set-value: pass an element index, a name:/id: reference or @x,y coordinates, not several" {
		t.Fatalf("message = %v, want the several-forms refusal", cmdErr)
	}
}

// containsAny reports whether s contains every given substring.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// TestUnavailableActionAtPointNamesThePoint checks the point form of the
// unsupported-Secondary-Action error: there is no Element Index to name.
func TestUnavailableActionAtPointNamesThePoint(t *testing.T) {
	s := newAtPointSpy(t)
	cmds := CommandsWith(s.svc)
	s.recordScreenshot(t, cmds)
	s.secondaryErr = &uia.UnavailableActionError{Action: "Expand", Index: -1, Offered: []string{"Toggle"}}

	_, cmdErr := runCommand(t, cmds, "perform-secondary-action", protocol.SecondaryActionArgs{
		Window: "101", Point: &protocol.Point{X: 400, Y: 235}, Action: "Expand",
	})
	if cmdErr == nil || cmdErr.Msg != "secondary action 'Expand' is not available for the Element at the point; available actions: Toggle" {
		t.Fatalf("message = %v, want the point form of the unavailable-action failure", cmdErr)
	}
}
