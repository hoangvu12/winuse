package helper

// The Chromium and Electron message route (headless-max ticket 20): text,
// keys and clicks into a Chrome_WidgetWin_1 Window without a Browser
// Connection, the read-back, and the refusals. The senders are faked;
// nothing is sent.

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
)

const chromeID = 401

// chromeSpy is a headlessSpy over a Chromium Window, its senders faked and
// recorded, and a FocusReader whose answer the test sets.
type chromeSpy struct {
	*headlessSpy
	texts     []string
	keys      []string
	clicks    []postedWheel // target, the screen point, the count in dx
	chords    []win.ChordPlan
	sendErr   error
	sendN     int
	value     string
	found     bool
	readValue bool // whether FocusedValue is offered at all
}

func (c *chromeSpy) FocusedValue(ctx context.Context, hwnd uint64) (string, bool, error) {
	c.step("focused-value")
	return c.value, c.found, nil
}

// noFocusReader hides chromeSpy's FocusedValue.
type noFocusReader struct{ *headlessSpy }

func newChromeSpy(readValue bool) *chromeSpy {
	c := &chromeSpy{headlessSpy: newHeadlessSpy(), readValue: readValue, sendN: 3}
	chrome := win.WindowInfo{ID: chromeID, App: "Google Chrome", Class: win.ChromiumClass, Title: "Page - Google Chrome",
		Bounds: win.Rect{Left: 0, Top: 0, Right: 800, Bottom: 600}}
	c.svc.Windows = func() ([]win.WindowInfo, error) { return append(fakeWindows(), chrome), nil }
	if readValue {
		c.svc.Actions = c
	} else {
		c.svc.Actions = noFocusReader{c.headlessSpy}
	}
	c.svc.Sleep = func(time.Duration) {}
	c.svc.Shots = shot.NewStore()
	c.svc.Shots.Record(agent.DefaultID, chromeID, shot.Mapping{Scale: 1, Width: 800, Height: 600, Coverage: image.Rect(0, 0, 800, 600)})
	c.svc.FrameOrigin = func(uint64) (image.Point, error) { return image.Point{X: 100, Y: 50}, nil }
	c.svc.ChromiumText = func(top uint64, text string, stopped func() bool) (int, error) {
		c.step("chromium-text")
		c.texts = append(c.texts, text)
		if c.sendErr != nil {
			return c.sendN, c.sendErr
		}
		return 3 * len(text), nil
	}
	c.svc.ChromiumKey = func(top uint64, k keys.Key) (int, error) {
		c.step("chromium-key")
		c.keys = append(c.keys, k.Name)
		return c.sendN, c.sendErr
	}
	c.svc.ChromiumClick = func(top uint64, sx, sy int32, button win.MouseButton, count int) (int, error) {
		c.step("chromium-click")
		c.clicks = append(c.clicks, postedWheel{top, sx, sy, int32(count), int32(button)})
		return 4, c.sendErr
	}
	c.svc.PostChord = func(top, target uint64, plan win.ChordPlan) (int, error) {
		c.step("post-chord")
		c.chords = append(c.chords, plan)
		return 2, nil
	}
	return c
}

func (c *chromeSpy) noInput(t *testing.T, what string) {
	t.Helper()
	for _, s := range c.recorded() {
		switch s {
		case "activate", "click", "type-text", "press-key":
			t.Errorf("%s ran %q: %v", what, s, c.recorded())
		}
	}
}

func TestChromiumTypeTextReadsBack(t *testing.T) {
	c := newChromeSpy(true)
	c.value, c.found = "say hello world", true
	out, e := runCommand(t, CommandsWith(c.svc), "type-text", protocol.TypeTextArgs{Window: "401", Text: "hello"})
	if e != nil || out != chromiumTypedOutput || len(c.texts) != 1 || c.texts[0] != "hello" {
		t.Fatalf("type-text = %q, %v, sent %q", out, e, c.texts)
	}
	c.noInput(t, "a Chromium type-text")

	c = newChromeSpy(true)
	c.value, c.found = "something else", true
	out, e = runCommand(t, CommandsWith(c.svc), "type-text", protocol.TypeTextArgs{Window: "401", Text: "hello"})
	if out != "" || e == nil || !strings.Contains(e.Msg, "does not show it") || !strings.Contains(e.Hint, "do not repeat it") {
		t.Errorf("a field that does not show the text = %q, %v", out, e)
	}

	for _, readValue := range []bool{true, false} {
		c = newChromeSpy(readValue)
		c.found = false
		out, e = runCommand(t, CommandsWith(c.svc), "type-text", protocol.TypeTextArgs{Window: "401", Text: "hello"})
		if e != nil || out != chromiumUnverifiedOutput {
			t.Errorf("no readable field (FocusReader %v) = %q, %v; want unverified", readValue, out, e)
		}
	}
}

func TestChromiumPressKeyAndChord(t *testing.T) {
	c := newChromeSpy(false)
	out, e := runCommand(t, CommandsWith(c.svc), "press-key", protocol.PressKeyArgs{Window: "401", Key: "Return"})
	if e != nil || out != chromiumKeyOutput || len(c.keys) != 1 || c.keys[0] != "Return" {
		t.Fatalf("press-key Return = %q, %v, keys %v", out, e, c.keys)
	}

	c = newChromeSpy(false)
	out, e = runCommand(t, CommandsWith(c.svc), "press-key", protocol.PressKeyArgs{Window: "401", Key: "Control_L+a"})
	if e != nil || out != chromiumKeyOutput || len(c.chords) != 1 || !c.chords[0].Activate || c.chords[0].Key.Name != "a" {
		t.Fatalf("press-key Control_L+a = %q, %v, chords %+v; want the attached key state with the activation pair", out, e, c.chords)
	}
	c.noInput(t, "a Chromium chord")

	// A Blocked Combo is never sent as messages, --force or not.
	c = newChromeSpy(false)
	out, e = runCommand(t, CommandsWith(c.svc), "press-key", protocol.PressKeyArgs{Window: "401", Key: "Alt_L+F4", Force: true})
	wantHeadedRefusal(t, "press-key Alt_L+F4 --force", out, e)
	if len(c.chords) != 0 || len(c.keys) != 0 {
		t.Errorf("a Blocked Combo was sent: chords %v, keys %v", c.chords, c.keys)
	}
}

func TestChromiumClicks(t *testing.T) {
	c := newChromeSpy(false)
	two := 2
	out, e := runCommand(t, CommandsWith(c.svc), "click", protocol.ClickArgs{Window: "401", ElementIndex: &two, ClickCount: 2})
	if e != nil || out != chromiumClickOutput {
		t.Fatalf("click 2 --count 2 = %q, %v", out, e)
	}
	// The headlessSpy's Element bounds are 300,400-380,430.
	if len(c.clicks) != 1 || c.clicks[0] != (postedWheel{chromeID, 340, 415, 2, int32(win.MouseLeft)}) {
		t.Errorf("clicked %+v, want a double click at the Element's centre", c.clicks)
	}

	c = newChromeSpy(false)
	out, e = runCommand(t, CommandsWith(c.svc), "click", protocol.ClickArgs{Window: "401", Point: pt(10, 20), MouseButton: "right"})
	if e != nil || out != chromiumClickOutput || len(c.clicks) != 1 || c.clicks[0] != (postedWheel{chromeID, 110, 70, 1, int32(win.MouseRight)}) {
		t.Errorf("click @10,20 --button right = %q, %v, clicked %+v", out, e, c.clicks)
	}
	c.noInput(t, "a Chromium click")
}

func TestChromiumRefusals(t *testing.T) {
	// A minimized Chromium would restore itself and take the foreground.
	c := newChromeSpy(false)
	c.readyErr = &win.MinimizedError{WindowID: chromeID, Class: win.ChromiumClass}
	out, e := runCommand(t, CommandsWith(c.svc), "type-text", protocol.TypeTextArgs{Window: "401", Text: "x"})
	if out != "" || e == nil || !strings.Contains(e.Msg, "minimized") || !strings.HasSuffix(e.Msg, "; nothing was sent") || len(c.texts) != 0 {
		t.Errorf("a minimized Chromium = %q, %v, sent %q", out, e, c.texts)
	}

	// The browser's thread owns the foreground: the user is in it.
	c = newChromeSpy(false)
	c.sendN, c.sendErr = 0, win.ErrChordForeground
	out, e = runCommand(t, CommandsWith(c.svc), "press-key", protocol.PressKeyArgs{Window: "401", Key: "Tab"})
	wantHeadedRefusal(t, "press-key into a Chromium that owns the foreground", out, e)
	if e != nil && (!strings.Contains(e.Msg, "owns the foreground") || !strings.HasSuffix(e.Hint, "winuse --headed press-key 401 Tab")) {
		t.Errorf("message %q, hint %q", e.Msg, e.Hint)
	}

	// Nothing sent at all, and a partial send.
	c = newChromeSpy(false)
	c.sendN, c.sendErr = 0, errors.New("SendMessageTimeout failed")
	_, e = runCommand(t, CommandsWith(c.svc), "type-text", protocol.TypeTextArgs{Window: "401", Text: "abc"})
	if e == nil || !strings.HasSuffix(e.Msg, "; nothing was sent") {
		t.Errorf("nothing sent = %v", e)
	}
	c = newChromeSpy(false)
	c.sendN, c.sendErr = 4, errors.New("SendMessageTimeout failed")
	_, e = runCommand(t, CommandsWith(c.svc), "type-text", protocol.TypeTextArgs{Window: "401", Text: "abc"})
	if e == nil || strings.Contains(e.Msg, "nothing was sent") || !strings.Contains(e.Hint, "may have taken effect") {
		t.Errorf("a partial send = %v", e)
	}

	// Not a Chromium Window: the route stays out, ticket 21's refusal holds.
	c = newChromeSpy(false)
	out, e = runCommand(t, CommandsWith(c.svc), "type-text", protocol.TypeTextArgs{Window: "101", Text: "x"})
	wantHeadedRefusal(t, "type-text into a non-Chromium Window", out, e)
	if len(c.texts) != 0 {
		t.Errorf("the Chromium route ran on a non-Chromium Window: %q", c.texts)
	}
}

func TestBatchChromiumStepsArePassive(t *testing.T) {
	c := newChromeSpy(false)
	ov := &overlaySpy{share: c.step}
	c.svc.Overlay = ov
	out, e := runCommand(t, CommandsWith(c.svc), "batch", protocol.BatchArgs{Window: "401", Steps: []protocol.Step{
		stepOf("type-text", protocol.TypeTextArgs{Text: "hi"}),
		stepOf("press-key", protocol.PressKeyArgs{Key: "Return"}),
		stepOf("click", protocol.ClickArgs{Point: pt(10, 20), ClickCount: 2}),
	}})
	if e != nil || !strings.HasPrefix(out, "Action 1 (type-text): ok\n  Note: typed as window messages") ||
		!strings.Contains(out, "\nAction 2 (press-key): ok\n  Note: pressed as window messages") || !strings.HasSuffix(out, "\nAction 3 (click): ok\n  Note: clicked as window messages\n") {
		t.Fatalf("batch = %q, %v", out, e)
	}
	if hasStepPrefix(c.recorded(), "overlay-show") || containsString(c.recorded(), "activate") {
		t.Errorf("a Batch of Chromium steps went Headed: %v", c.recorded())
	}
	if got := c.esc.brackets(); len(got) == 0 || got[0] != "arm-passive" || containsString(got, "arm") {
		t.Errorf("Esc brackets = %v, want one passive scope", got)
	}
}
