package helper

// The hung pre-check's offline tests (multi-agent 07): an Action on a
// Window whose UI thread does not respond is refused with nothing sent; a
// responding Window acts; the Window identity check answers first; a Page
// Action, which reaches the Page through the browser, is not checked; a
// read is not checked. WindowHung is a fake.

import (
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// hungSet is a fake WindowHung: the Windows in it are hung.
type hungSet map[uint64]bool

func (h hungSet) hung(hwnd uint64) bool { return h[hwnd] }

func TestHungAppIsRefusedWithNothingSent(t *testing.T) {
	spy := newHeadlessSpy()
	hung := hungSet{101: true}
	spy.svc.WindowHung = hung.hung
	cmds := CommandsWith(spy.svc)
	_, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left"))
	if e == nil || e.Msg != "window 101's App is not responding; nothing was sent" || !strings.Contains(e.Hint, "another App's Window") {
		t.Errorf("a hung App: %+v", e)
	}
	if clicked(spy) {
		t.Fatal("the hung App was clicked")
	}
	for _, c := range []struct {
		cmd  string
		args any
	}{
		{"set-value", protocol.SetValueArgs{Window: "101", ElementIndex: idx(2), Value: "x"}},
		{"press-key", protocol.PressKeyArgs{Window: "101", Key: "Return"}},
		{"move-window", protocol.MoveWindowArgs{Window: "101", X: 1, Y: 2}},
		{"activate-window", protocol.ActivateArgs{Window: "101"}},
	} {
		if _, e := runCommand(t, cmds, c.cmd, c.args); e == nil || !strings.Contains(e.Msg, "not responding; nothing was sent") {
			t.Errorf("%s on a hung App: %+v", c.cmd, e)
		}
	}
	if steps := spy.recorded(); len(steps) != 0 {
		t.Errorf("a refused Action reached the App: %v", steps)
	}

	// The App responds again: the Action acts.
	delete(hung, 101)
	if out, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left")); e != nil || out != "ok\n" || !clicked(spy) {
		t.Errorf("after the App responds: %q, %+v", out, e)
	}
}

func TestHungCheckComesAfterTheIdentityCheck(t *testing.T) {
	spy, owners := identitySpy()
	spy.svc.WindowHung = hungSet{101: true}.hung
	cmds := CommandsWith(spy.svc)
	runCommand(t, cmds, "list-windows", nil)
	owners[101] = reusedOwner
	if _, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left")); e == nil || !strings.Contains(e.Msg, "a new window reused its ID") {
		t.Errorf("a reused, hung Window ID: %+v, want the identity refusal", e)
	}
}

func TestHungCheckSkipsReadsAndCommandsWithNoWindow(t *testing.T) {
	spy := newHeadlessSpy()
	asked := 0
	spy.svc.WindowHung = func(uint64) bool { asked++; return true }
	cmds := CommandsWith(spy.svc)
	runCommand(t, cmds, "list-windows", nil)
	runCommand(t, cmds, "get-window", protocol.GetWindowArgs{Window: "101"})
	runCommand(t, cmds, "clipboard-clear", nil)
	if asked != 0 {
		t.Errorf("reads and Desktop commands asked whether a Window hangs %d times", asked)
	}
}

func TestHungCheckSkipsPageActions(t *testing.T) {
	r := newPageRig(t)
	asked := 0
	r.svc.WindowHung = func(uint64) bool { asked++; return true }
	if _, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}); e != nil {
		t.Errorf("a Page click in a browser whose UI thread hangs: %+v", e)
	}
	if asked != 0 {
		t.Errorf("a Page Action asked whether the Window hangs")
	}
	// A click on the browser's frame reaches its UI thread: checked.
	if _, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(2)}); e == nil || !strings.Contains(e.Msg, "not responding") {
		t.Errorf("a frame click in a hung browser: %+v", e)
	}
}
