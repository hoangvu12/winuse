package helper

// Polite real input's offline tests (multi-agent ticket 08): a Headed
// Action waits for the user's input to go quiet (winuse's own input does
// not count) and refuses with nothing sent past the wait; it confirms the
// activation with a round trip before any input; it gives the cursor back
// under the 2 px rule and the foreground back unless the Action left
// something open that needs it, says so in a Note when it keeps it, and
// keeps it for activate-window, a shell surface and a moved foreground;
// hover keeps the pointer; a Batch saves and gives back once; every exit
// path releases what winuse holds; the hook's yield stops like Esc and
// says the user took over. The desktop is a fake.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// politeDesk fakes what polite input reads and restores: the user's
// Window 500 is in front with the cursor at (5, 5); the clock moves only
// when winuse sleeps.
type politeDesk struct {
	fg          uint64
	cx, cy      int32
	idle        []time.Duration // InputIdle's answers in turn; the last repeats
	clock       time.Time
	all         []win.WindowInfo
	activations []uint64
	puts        int
	releases    int
	syncErr     error
	syncs       int
}

func newPoliteDesk() *politeDesk {
	return &politeDesk{fg: 500, cx: 5, cy: 5, idle: []time.Duration{time.Hour}, clock: time.Unix(1000, 0),
		all: []win.WindowInfo{{ID: 101, PID: 7}, {ID: 500, PID: 9}}}
}

func (d *politeDesk) wire(svc *Services) {
	activate := svc.Activate
	svc.Activate = func(hwnd uint64) error {
		d.activations = append(d.activations, hwnd)
		d.fg = hwnd
		if activate != nil {
			return activate(hwnd)
		}
		return nil
	}
	svc.Foreground = func() uint64 { return d.fg }
	svc.CursorPos = func() (int32, int32, bool) { return d.cx, d.cy, true }
	svc.PutCursor = func(x, y int32) error { d.puts++; d.cx, d.cy = x, y; return nil }
	svc.InputIdle = func() time.Duration {
		v := d.idle[0]
		if len(d.idle) > 1 {
			d.idle = d.idle[1:]
		}
		return v
	}
	svc.Now = func() time.Time { return d.clock }
	svc.Sleep = func(t time.Duration) { d.clock = d.clock.Add(t) }
	svc.AllWindows = func() ([]win.WindowInfo, error) { return append([]win.WindowInfo(nil), d.all...), nil }
	svc.SyncWindow = func(uint64) error { d.syncs++; return d.syncErr }
	svc.ReleaseHeld = func() (int, error) { d.releases++; return 0, nil }
}

// politeSpy is the Headless spy on the polite desk (Notepad 101 is
// process 7), its Headed click moving the cursor to the Element.
func politeSpy() (*headlessSpy, *politeDesk) {
	spy := newHeadlessSpy()
	d := newPoliteDesk()
	d.wire(&spy.svc)
	spy.svc.Windows = func() ([]win.WindowInfo, error) {
		ws := fakeWindows()
		ws[0].PID = 7
		return ws, nil
	}
	click := spy.svc.Click
	spy.svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		d.cx, d.cy = x, y
		return click(target, x, y, button, count)
	}
	return spy, d
}

func headedClick(t *testing.T, svc Services) (string, *Error) {
	t.Helper()
	return runCommand(t, CommandsWith(svc), "click", headedArgs(clickArgs("101", 2, 2, "left")))
}

func TestPoliteGivesTheForegroundAndCursorBack(t *testing.T) {
	spy, d := politeSpy()
	out, e := headedClick(t, spy.svc)
	if e != nil || out != "ok\n" {
		t.Fatalf("headed click: %q, %+v", out, e)
	}
	if d.fg != 500 || d.cx != 5 || d.cy != 5 {
		t.Errorf("after the click: foreground %d, cursor (%d, %d); want 500 at (5, 5)", d.fg, d.cx, d.cy)
	}
	if want := []uint64{101, 500}; len(d.activations) != 2 || d.activations[0] != 101 || d.activations[1] != 500 {
		t.Errorf("activations %v, want %v", d.activations, want)
	}
	if d.syncs != 2 || d.releases != 1 {
		t.Errorf("%d round trips and %d releases, want 2 (the activation, the drain) and 1", d.syncs, d.releases)
	}
}

func TestPoliteKeepsTheForegroundWhenItIsNeeded(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(spy *headlessSpy, d *politeDesk)
		fg    uint64
		note  string
	}{
		{"a popup menu opened", func(spy *headlessSpy, d *politeDesk) {
			click := spy.svc.Click
			spy.svc.Click = func(target uint64, x, y int32, b win.MouseButton, n int) error {
				d.all = append(d.all, win.WindowInfo{ID: 640, PID: 7, Class: "#32768"})
				return click(target, x, y, b, n)
			}
		}, 101, "Note: the foreground stays on window 101: the Action left a menu or Window of its App open\n"},
		{"Start took it", func(spy *headlessSpy, d *politeDesk) {
			click := spy.svc.Click
			spy.svc.Click = func(target uint64, x, y int32, b win.MouseButton, n int) error {
				d.fg = 960
				return click(target, x, y, b, n)
			}
		}, 960, "Note: the foreground was not given back: it moved to window 960 during the Action\n"},
		{"the user took theirs back", func(spy *headlessSpy, d *politeDesk) {
			click := spy.svc.Click
			spy.svc.Click = func(target uint64, x, y int32, b win.MouseButton, n int) error {
				d.fg = 500
				return click(target, x, y, b, n)
			}
		}, 500, ""},
		{"another App's Window opened", func(spy *headlessSpy, d *politeDesk) {
			click := spy.svc.Click
			spy.svc.Click = func(target uint64, x, y int32, b win.MouseButton, n int) error {
				d.all = append(d.all, win.WindowInfo{ID: 650, PID: 12})
				return click(target, x, y, b, n)
			}
		}, 500, ""},
	} {
		spy, d := politeSpy()
		c.setup(spy, d)
		out, e := headedClick(t, spy.svc)
		if e != nil || out != "ok\n"+c.note {
			t.Errorf("%s: %q, %+v; want ok and %q", c.name, out, e, c.note)
		}
		if d.fg != c.fg {
			t.Errorf("%s: foreground %d, want %d", c.name, d.fg, c.fg)
		}
		if d.cx != 5 || d.cy != 5 {
			t.Errorf("%s: cursor (%d, %d), want it back at (5, 5)", c.name, d.cx, d.cy)
		}
	}
}

func TestPoliteActivateWindowKeepsTheForeground(t *testing.T) {
	spy, d := politeSpy()
	out, e := runCommand(t, CommandsWith(spy.svc), "activate-window", protocol.ActivateArgs{Window: "101"})
	if e != nil || out != "ok\n" || d.fg != 101 {
		t.Errorf("activate-window: %q, %+v (foreground %d, want 101)", out, e, d.fg)
	}
}

func TestPoliteCursorStaysWhenTheUserMovedIt(t *testing.T) {
	// The cursor is read three times: saveDesk, noteLeft (where winuse
	// left it), giveBack. A move of more than 2 px between the last two is
	// the user's: the cursor stays where they put it.
	spy, d := politeSpy()
	reads := 0
	spy.svc.CursorPos = func() (int32, int32, bool) {
		reads++
		if reads == 3 {
			return d.cx + 3, d.cy, true
		}
		return d.cx, d.cy, true
	}
	if _, e := headedClick(t, spy.svc); e != nil {
		t.Fatalf("headed click: %+v", e)
	}
	if d.puts != 0 {
		t.Errorf("the cursor was put back after the user moved it 3 px")
	}
	// Within 2 px it goes back.
	spy, d = politeSpy()
	reads = 0
	spy.svc.CursorPos = func() (int32, int32, bool) {
		reads++
		if reads == 3 {
			return d.cx + 2, d.cy - 2, true
		}
		return d.cx, d.cy, true
	}
	if _, e := headedClick(t, spy.svc); e != nil || d.puts != 1 || d.cx != 5 || d.cy != 5 {
		t.Errorf("within 2 px: %+v, %d puts, cursor (%d, %d)", e, d.puts, d.cx, d.cy)
	}
}

func TestPoliteHoverKeepsThePointer(t *testing.T) {
	s := newPointerSpy(t)
	s.points = map[int]uia.Point{2: {X: 50, Y: 60}}
	s.svc.Actions = s
	d := newPoliteDesk()
	d.wire(&s.svc)
	s.svc.Hover = func(target uint64, x, y int32) error { d.cx, d.cy = x, y; return nil }
	out, e := runCommand(t, CommandsWith(s.svc), "hover", protocol.HoverArgs{Window: "101", ElementIndex: idx(2), Headed: true})
	if e != nil || out != "ok\n" {
		t.Fatalf("hover: %q, %+v", out, e)
	}
	if d.puts != 0 || d.cx != 50 || d.cy != 60 {
		t.Errorf("hover's pointer was moved back: %d puts, cursor (%d, %d)", d.puts, d.cx, d.cy)
	}
	if d.fg != 500 {
		t.Errorf("foreground %d after hover, want it given back to 500", d.fg)
	}
}

func TestPoliteWaitsForTheUserToPause(t *testing.T) {
	spy, d := politeSpy()
	d.idle = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, time.Second}
	start := d.clock
	if _, e := headedClick(t, spy.svc); e != nil {
		t.Fatalf("headed click after a pause: %+v", e)
	}
	if waited := d.clock.Sub(start); waited < 2*politePoll {
		t.Errorf("waited %v, want two polls", waited)
	}

	spy, d = politeSpy()
	d.idle = []time.Duration{50 * time.Millisecond}
	out, e := headedClick(t, spy.svc)
	if e == nil || out != "" || !strings.Contains(e.Msg, "the user is typing or using the mouse") || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Fatalf("busy user: %q, %+v", out, e)
	}
	if len(d.activations) != 0 || hasStep(spy.recorded(), "click") {
		t.Errorf("the refusal sent something: %v, %v", d.activations, spy.recorded())
	}
}

func TestPoliteOwnInputDoesNotCount(t *testing.T) {
	spy, d := politeSpy()
	svc := defaultServices(spy.svc)
	// winuse's own last input ended a moment ago, 80 ms before the clock.
	svc.polite.ownInputEnded(d.clock.Add(-80 * time.Millisecond))
	d.idle = []time.Duration{80 * time.Millisecond}
	if _, e := headedClick(t, svc); e != nil {
		t.Fatalf("headed click right after winuse's own input: %+v", e)
	}
	// The user's input after winuse's (idle shorter than winuse's last
	// input by more than the slack) still waits.
	d.idle = []time.Duration{10 * time.Millisecond}
	d.clock = d.clock.Add(time.Second)
	if _, e := headedClick(t, svc); e == nil {
		t.Error("the user's input after winuse's did not hold the Action back")
	}
}

func TestPoliteRoundTripFailureSendsNothing(t *testing.T) {
	spy, d := politeSpy()
	d.syncErr = errors.New("window 101 did not answer: timeout")
	out, e := headedClick(t, spy.svc)
	if e == nil || out != "" || !strings.Contains(e.Msg, "after it was brought to the front") || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Fatalf("hung Window: %q, %+v", out, e)
	}
	if hasStep(spy.recorded(), "click") {
		t.Errorf("input went to a Window that did not answer: %v", spy.recorded())
	}
	if d.releases != 1 || d.fg != 500 {
		t.Errorf("after the refusal: %d releases, foreground %d; want 1 and 500", d.releases, d.fg)
	}
}

func TestPoliteReleasesOnFailure(t *testing.T) {
	spy, d := politeSpy()
	spy.clickErr = errors.New("SendInput failed")
	if _, e := headedClick(t, spy.svc); e == nil {
		t.Fatal("the failing click succeeded")
	}
	if d.releases != 1 || d.fg != 500 {
		t.Errorf("after the failure: %d releases, foreground %d; want 1 and 500", d.releases, d.fg)
	}
}

func TestPoliteBatchGivesBackOnce(t *testing.T) {
	spy, d := politeSpy()
	out, e := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Headed: true, Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2), ClickCount: 2}),
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2), ClickCount: 2}),
	}})
	if e != nil || out != "ok\n" {
		t.Fatalf("batch: %q, %+v", out, e)
	}
	// Two activations of 101 (one per step) and one give-back to 500.
	if len(d.activations) != 3 || d.activations[2] != 500 || d.fg != 500 {
		t.Errorf("activations %v, foreground %d; want 101, 101, 500", d.activations, d.fg)
	}
	if d.puts != 1 || d.cx != 5 || d.cy != 5 {
		t.Errorf("%d cursor puts, cursor (%d, %d); want one, at (5, 5)", d.puts, d.cx, d.cy)
	}
}

func TestPoliteChromiumRecheck(t *testing.T) {
	spy, d := politeSpy()
	spy.svc.Windows = func() ([]win.WindowInfo, error) {
		ws := fakeWindows()
		ws[0].Class = "Chrome_WidgetWin_1"
		return ws, nil
	}
	// Chromium takes the foreground back once after the hand-back.
	sleep := spy.svc.Sleep
	took := false
	spy.svc.Sleep = func(t time.Duration) {
		sleep(t)
		if !took && len(d.activations) == 2 {
			took = true
			d.fg = 101
		}
	}
	if _, e := headedClick(t, spy.svc); e != nil {
		t.Fatalf("headed click: %+v", e)
	}
	if d.fg != 500 || len(d.activations) != 3 {
		t.Errorf("foreground %d after %v; want 500, given back twice", d.fg, d.activations)
	}
}

// yieldEsc is a latched stop the hook marked as the user's own input.
type yieldEsc struct{ *scopeEsc }

func (yieldEsc) Yielded() bool { return true }

func TestPoliteYieldSaysTheUserTookOver(t *testing.T) {
	spy, _ := politeSpy()
	spy.esc.pressOn = "act"
	spy.svc.Esc = yieldEsc{spy.esc}
	click := spy.svc.Click
	spy.svc.Click = func(target uint64, x, y int32, b win.MouseButton, n int) error {
		spy.esc.pressIf("act")
		return click(target, x, y, b, n)
	}
	_, e := headedClick(t, spy.svc)
	if e == nil || e.Msg != yieldStopMessage || e.Exit != protocol.ExitEsc {
		t.Fatalf("yield: %+v", e)
	}
	if stopErrorFor(spy.esc).Msg == yieldStopMessage {
		t.Error("a plain Esc stop reads as a yield")
	}
}
