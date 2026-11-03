package helper

// Self-activation's offline tests (headless-max 03, detect and report): a
// Headless Action whose App took the foreground says so after its ok; a
// foreground that stayed put, went to another App, or was already the
// App's adds nothing; a Headed Action or a Batch with a Headed step never
// reports its own activation; a failed Action keeps its error alone; a
// dialog that takes the foreground a moment after it opened is still
// seen. The foreground and window owners are fakes.

import (
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// fgDesk fakes the foreground and the window owners: the user's Window 500
// (process 9) is in front; Window 101 and its dialog 777 belong to
// process 7.
type fgDesk struct {
	fg uint64
}

func (d *fgDesk) wire(svc *Services) {
	svc.Foreground = func() uint64 { return d.fg }
	svc.WindowPID = func(hwnd uint64) uint32 {
		switch hwnd {
		case 101, 777:
			return 7
		case 500:
			return 9
		case 600:
			return 11
		}
		return 0
	}
}

// fgSpy is a headless spy whose Action moves the foreground to takeTo (0:
// leaves it).
func fgSpy(takeTo uint64) (*headlessSpy, *fgDesk) {
	spy := newHeadlessSpy()
	d := &fgDesk{fg: 500}
	d.wire(&spy.svc)
	spy.svc.Ready = func(uint64) error {
		if takeTo != 0 {
			d.fg = takeTo
		}
		return nil
	}
	return spy, d
}

func TestHeadlessActionReportsSelfActivation(t *testing.T) {
	for _, c := range []struct {
		name   string
		takeTo uint64
		before uint64
		note   bool
	}{
		{"the dialog took it", 777, 500, true},
		{"the target took it", 101, 500, true},
		{"nothing moved", 0, 500, false},
		{"another App took it", 600, 500, false},
		{"the App already had it", 777, 101, false},
	} {
		spy, d := fgSpy(c.takeTo)
		d.fg = c.before
		out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
		if e != nil {
			t.Fatalf("%s: %+v", c.name, e)
		}
		want := "ok\n"
		if c.note {
			want += launchForegroundNote
		}
		if out != want {
			t.Errorf("%s: output %q, want %q", c.name, out, want)
		}
	}
}

func TestHeadedActionReportsNothing(t *testing.T) {
	spy, d := fgSpy(0)
	activate := spy.svc.Activate
	spy.svc.Activate = func(hwnd uint64) error { d.fg = hwnd; return activate(hwnd) }
	// A double click is Headed, with --headed (headless-max ticket 21):
	// winuse activates the Window itself.
	out, e := runCommand(t, CommandsWith(spy.svc), "click", headedArgs(clickArgs("101", 2, 2, "left")))
	if e != nil || out != "ok\n" || d.fg != 101 {
		t.Errorf("headed click: %q, %+v (foreground %d)", out, e, d.fg)
	}
	// A Batch with a Headed step neither, even when a Headless step ran
	// first; an all-Headless Batch whose App took the foreground does.
	spy, d = fgSpy(777)
	activate = spy.svc.Activate
	spy.svc.Activate = func(hwnd uint64) error { d.fg = hwnd; return activate(hwnd) }
	out, e = runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2), ClickCount: 2, Headed: true}),
	}})
	if e != nil || strings.Contains(out, "foreground") {
		t.Errorf("mixed batch: %q, %+v", out, e)
	}
	spy, _ = fgSpy(777)
	out, e = runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
	}})
	if e != nil || out != "Action 1 (click): ok\n"+launchForegroundNote {
		t.Errorf("headless batch: %q, %+v", out, e)
	}
	// A Batch that ran past a failed step gets the note after its steps'
	// lines (desktop-parity ticket 20).
	spy, _ = fgSpy(777)
	_, e = runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2), ClickCount: 2}), // Headed only: refused
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
	}})
	if e == nil || !strings.HasPrefix(e.Out, "Action 1 (click): failed: ") || !strings.HasSuffix(e.Out, "Action 2 (click): ok\n"+launchForegroundNote) {
		t.Errorf("batch past a failed step: %+v", e)
	}
}

func TestSelfActivationFailedActionKeepsItsError(t *testing.T) {
	spy, _ := fgSpy(777)
	spy.hClickErr = errFake("pattern call failed")
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e == nil || out != "" || strings.Contains(e.Msg, "foreground") {
		t.Errorf("failed click: %q, %+v", out, e)
	}
}

// A dialog that opens during the settle and takes the foreground a moment
// later is still reported, after its own note.
func TestSelfActivationOfADialogAfterItOpened(t *testing.T) {
	spy, d := fgSpy(0)
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	desk.wire(&spy.svc)
	spy.svc.Sleep = func(time.Duration) {
		desk.sleep(0)
		if desk.calls >= 2 { // the dialog was seen: it activates itself now
			d.fg = 777
		}
	}
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	want := "ok\nNote: a new Window opened: 777 Notepad \"Save As\"\n" + launchForegroundNote
	if e != nil || out != want {
		t.Errorf("output %q, %+v; want %q", out, e, want)
	}
}

func TestWatchesForeground(t *testing.T) {
	for name, want := range map[string]bool{
		"click": true, "press-key": true, "type-text": true, "batch": true, "close-window": true, "tab-switch": true, "navigate": true,
		"restore-window": false, "activate-window": false, "drag": false, "hover": false, "zoom": false, "clipboard-set": false, "get-window-state": false,
	} {
		if got := watchesForeground(name); got != want {
			t.Errorf("watchesForeground(%s) = %v, want %v", name, got, want)
		}
	}
}
