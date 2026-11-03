package helper

// The opened-Window reports' offline tests (desktop-parity 02): after
// click, perform-secondary-action Expand or Select, press-key, set-value
// and a Batch, a new dialog, menu or popup of the target's App is
// reported, and so is the target closing; nothing new adds nothing;
// tooltips, other Apps and Windows that were already there are skipped;
// a failed Action keeps its error alone; a reported menu resolves by its
// Window ID; a Page Action reports a new Tab. The desktop is a fake
// snapshot sequence; no window is enumerated.

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

const (
	notepadPID = 7
	otherPID   = 9
	toolWindow = 0x00000080 // WS_EX_TOOLWINDOW
)

var (
	popupBounds = win.Rect{Left: 50, Top: 60, Right: 350, Bottom: 460}
	// The target, as the unfiltered enumeration sees it.
	notepadMain = win.WindowInfo{ID: 101, PID: notepadPID, App: "Notepad", Title: "Untitled - Notepad", Class: "Notepad", Visible: true,
		Bounds: win.Rect{Left: 10, Top: 20, Right: 810, Bottom: 490}}
	otherApp   = win.WindowInfo{ID: 500, PID: otherPID, App: "Paint", Title: "Paint", Visible: true, Bounds: popupBounds}
	saveDialog = win.WindowInfo{ID: 777, PID: notepadPID, App: "Notepad", Title: "Save As", Class: "#32770", Owner: 101, Visible: true, Bounds: popupBounds}
	editMenu   = win.WindowInfo{ID: 778, PID: notepadPID, App: "Notepad", Class: "#32768", ExStyle: toolWindow, Visible: true, Bounds: popupBounds}
	tooltip    = win.WindowInfo{ID: 779, PID: notepadPID, App: "Notepad", Class: "tooltips_class32", ExStyle: toolWindow, Visible: true, Bounds: popupBounds}
	otherNew   = win.WindowInfo{ID: 501, PID: otherPID, App: "Paint", Title: "Colors", Class: "#32770", Visible: true, Bounds: popupBounds}
)

// surfaceDesk is the fake unfiltered enumeration: the first call is the
// snapshot before the Action, each later one the next snapshot in after
// (the last one repeats). sleeps counts the settle's pauses.
type surfaceDesk struct {
	mu     sync.Mutex
	before []win.WindowInfo
	after  [][]win.WindowInfo
	calls  int
	sleeps int
}

func (d *surfaceDesk) surfaces() ([]win.WindowInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.calls == 1 || len(d.after) == 0 {
		return d.before, nil
	}
	i := d.calls - 2
	if i >= len(d.after) {
		i = len(d.after) - 1
	}
	return d.after[i], nil
}

func (d *surfaceDesk) sleep(time.Duration) {
	d.mu.Lock()
	d.sleeps++
	d.mu.Unlock()
}

// wire sets the desk into svc.
func (d *surfaceDesk) wire(svc *Services) {
	svc.Surfaces = d.surfaces
	svc.Sleep = d.sleep
}

func newDesk(after ...[]win.WindowInfo) *surfaceDesk {
	return &surfaceDesk{before: []win.WindowInfo{notepadMain, otherApp}, after: after}
}

func TestOpenedDialogAfterHeadlessClick(t *testing.T) {
	spy := newHeadlessSpy()
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherNew, otherApp})
	desk.wire(&spy.svc)
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e != nil {
		t.Fatalf("click: %v", e.Error())
	}
	if want := "ok\nNote: a new Window opened: 777 Notepad \"Save As\"\n"; out != want {
		t.Errorf("output %q, want %q (another App's new Window is not the Action's)", out, want)
	}
	if desk.sleeps != 1 {
		t.Errorf("the settle paused %d times, want it to stop at the first change", desk.sleeps)
	}
}

func TestOpenedMenuAfterPressKeySkipsTooltips(t *testing.T) {
	spy := newHeadlessSpy()
	desk := newDesk([]win.WindowInfo{tooltip, editMenu, notepadMain, otherApp})
	desk.wire(&spy.svc)
	out, e := runCommand(t, CommandsWith(spy.svc), "press-key", protocol.PressKeyArgs{Window: "101", Key: "Alt_L+e"})
	if e != nil {
		t.Fatalf("press-key: %v", e.Error())
	}
	if want := "ok\nNote: a new Window opened: 778 Notepad \"\"\n"; out != want {
		t.Errorf("output %q, want %q", out, want)
	}
}

func TestOpenedLateWindowWithinTheSettle(t *testing.T) {
	spy := newHeadlessSpy()
	same := []win.WindowInfo{notepadMain, otherApp}
	desk := newDesk(same, same, []win.WindowInfo{editMenu, notepadMain, otherApp})
	desk.wire(&spy.svc)
	out, e := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action",
		protocol.SecondaryActionArgs{Window: "101", ElementIndex: idx(3), Action: "expand"})
	if e != nil {
		t.Fatalf("Expand: %v", e.Error())
	}
	if !strings.Contains(out, "Note: a new Window opened: 778 Notepad") || desk.sleeps != 3 {
		t.Errorf("output %q after %d pauses, want the menu found on the third", out, desk.sleeps)
	}
}

func TestOpenedTargetClosed(t *testing.T) {
	spy := newHeadlessSpy()
	desk := newDesk([]win.WindowInfo{otherApp})
	desk.wire(&spy.svc)
	out, e := runCommand(t, CommandsWith(spy.svc), "set-value", protocol.SetValueArgs{Window: "101", ElementIndex: idx(1), Value: "x"})
	if e != nil {
		t.Fatalf("set-value: %v", e.Error())
	}
	if want := "ok\nNote: window 101 closed\n"; out != want {
		t.Errorf("output %q, want %q", out, want)
	}
}

func TestOpenedNothingNew(t *testing.T) {
	spy := newHeadlessSpy()
	// A Window of the same App that was already open is not new.
	desk := &surfaceDesk{before: []win.WindowInfo{saveDialog, notepadMain}, after: [][]win.WindowInfo{{saveDialog, notepadMain}}}
	desk.wire(&spy.svc)
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e != nil {
		t.Fatalf("click: %v", e.Error())
	}
	if out != "ok\n" {
		t.Errorf("output %q, want ok alone", out)
	}
	if want := int(openedSettle / openedPoll); desk.sleeps != want {
		t.Errorf("the settle paused %d times, want the whole settle (%d)", desk.sleeps, want)
	}
}

// Only the listed Actions are reported: a Toggle, a scroll and the Window
// Actions take no snapshot.
func TestOpenedOnlyAfterTheListedActions(t *testing.T) {
	spy := newHeadlessSpy()
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	desk.wire(&spy.svc)
	out, e := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action",
		protocol.SecondaryActionArgs{Window: "101", ElementIndex: idx(3), Action: "Toggle"})
	if e != nil {
		t.Fatalf("Toggle: %v", e.Error())
	}
	if out != "ok\n" || desk.calls != 0 {
		t.Errorf("Toggle: output %q, %d snapshots; want ok and none", out, desk.calls)
	}
	for _, c := range []struct {
		name string
		args json.RawMessage
		want bool
	}{
		{"click", nil, true},
		{"press-key", nil, true},
		{"set-value", nil, true},
		{"batch", nil, true},
		{"invoke-menu", nil, true},
		{"perform-secondary-action", json.RawMessage(`{"action":"Select"}`), true},
		{"perform-secondary-action", json.RawMessage(`{"action":"Collapse"}`), false},
		{"scroll", nil, false},
		{"type-text", nil, false},
		{"close-window", nil, false},
	} {
		if got := reportsOpened(c.name, c.args); got != c.want {
			t.Errorf("reportsOpened(%s %s) = %v, want %v", c.name, c.args, got, c.want)
		}
	}
}

func TestOpenedFailedActionKeepsItsError(t *testing.T) {
	spy := newHeadlessSpy()
	spy.hClickErr = errFake("pattern call failed")
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	desk.wire(&spy.svc)
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e == nil || strings.Contains(e.Msg, "Note:") || out != "" {
		t.Errorf("failed click: output %q, error %+v; want the error alone", out, e)
	}
	if desk.sleeps != 0 {
		t.Errorf("a failed Action waited %d pauses for Windows", desk.sleeps)
	}
}

// The diff is skipped when the target is not in the snapshot: the
// enumeration refused (as in any offline test), or it does not show the
// target.
func TestOpenedSkippedWithoutTheTarget(t *testing.T) {
	spy := newHeadlessSpy()
	desk := &surfaceDesk{before: []win.WindowInfo{otherApp}, after: [][]win.WindowInfo{{saveDialog, otherApp}}}
	desk.wire(&spy.svc)
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e != nil || out != "ok\n" || desk.sleeps != 0 {
		t.Errorf("output %q, error %+v, %d pauses; want ok and no settle", out, e, desk.sleeps)
	}

	spy = newHeadlessSpy() // Surfaces left to the default: refused in a test binary
	out, e = runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e != nil || out != "ok\n" {
		t.Errorf("default enumeration: output %q, error %+v", out, e)
	}
}

// A Batch is diffed once, around all its steps.
func TestOpenedBatchDiffsOnce(t *testing.T) {
	spy := newHeadlessSpy()
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	desk.wire(&spy.svc)
	out, e := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("set-value", protocol.SetValueArgs{ElementIndex: idx(1), Value: "notes"}),
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
	}})
	if e != nil {
		t.Fatalf("batch: %v", e.Error())
	}
	if want := "ok\nNote: a new Window opened: 777 Notepad \"Save As\"\n"; out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if desk.calls != 2 {
		t.Errorf("%d snapshots, want one before the Batch and one after", desk.calls)
	}
}

// A reported menu that list-windows drops resolves by its Window ID; a
// tooltip does not.
func TestOpenedMenuResolvesByWindowID(t *testing.T) {
	spy := newHeadlessSpy()
	desk := &surfaceDesk{before: []win.WindowInfo{editMenu, tooltip, notepadMain}}
	desk.wire(&spy.svc)
	cmds := CommandsWith(spy.svc)
	out, e := runCommand(t, cmds, "get-window", protocol.GetWindowArgs{Window: "778"})
	if e != nil || out != "778\tNotepad\t\n" {
		t.Errorf("get-window 778: output %q, error %+v", out, e)
	}
	if _, e := runCommand(t, cmds, "get-window", protocol.GetWindowArgs{Window: "779"}); e == nil || !strings.Contains(e.Msg, "no open Window with ID 779") {
		t.Errorf("get-window on a tooltip: %+v", e)
	}
}

func TestOpenedNotesFormat(t *testing.T) {
	got := formatOpened([]win.WindowInfo{{ID: 9, App: "Fixture", Title: " Say  \"hi\"\t"}}, notepadMain, true)
	want := "Note: a new Window opened: 9 Fixture \"Say \\\"hi\\\"\"\nNote: window 101 closed\n"
	if got != want {
		t.Errorf("formatOpened = %q, want %q", got, want)
	}
	if got := stepNotes("ok (a download)\nNote: one\nnoise\nNote: two"); got != "Note: one\nNote: two\n" {
		t.Errorf("stepNotes = %q", got)
	}
	if got := withNotes("ok", "Note: x\n"); got != "ok\nNote: x\n" {
		t.Errorf("withNotes = %q", got)
	}
}

// A Page click that opens a Tab is reported with the Tab's id, title and
// URL; one that opens nothing adds nothing.
func TestOpenedPageClickNewTab(t *testing.T) {
	r := newPageRig(t)
	var mu sync.Mutex
	opened := false
	r.srv.Handle("Target.getTargets", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		infos := []map[string]any{{"targetId": "T1", "type": "page", "title": "Docs", "url": "https://example.com/"}}
		if opened {
			infos = append(infos, map[string]any{"targetId": "T9", "type": "page", "title": "New  page", "url": "https://example.com/new"})
		}
		return map[string]any{"targetInfos": infos}, nil
	})
	r.svc.Sleep = func(time.Duration) {}
	out, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e != nil || out != "ok\n" {
		t.Fatalf("click opening nothing: output %q, error %+v", out, e)
	}

	r.srv.Handle("Input.dispatchMouseEvent", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		mu.Lock()
		opened = true
		mu.Unlock()
		return nil, nil
	})
	out, e = r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e != nil {
		t.Fatalf("click: %+v", e)
	}
	if want := "ok\nNote: a new Tab opened: " + page.TabID("T9") + " \"New page\" https://example.com/new\n"; out != want {
		t.Errorf("output %q, want %q", out, want)
	}
}
