package helper

// A Batch across several Windows (desktop-parity ticket 24): a step's own
// Window (`in <window>`), the Window the step before it opened (`in new`,
// found by the opened-Window diff), the refusals before any step and at
// an `in new` step's turn, the lanes each takes, and the final report
// naming every uncertain step's Window. Everything Win32 and element-side
// is faked; no desktop is touched.

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// inNewStep is a Step in the Window the step before it opened.
func inNewStep(kind string, args any) protocol.Step {
	st := stepOf(kind, args)
	st.InNew = true
	return st
}

// inStep is a Step in its own Window.
func inStep(window, kind string, args any) protocol.Step {
	st := stepOf(kind, args)
	st.Window = window
	return st
}

// openingDesk fakes the unfiltered enumeration around a click that opens
// windows: before the spy's first Headless click it shows the main Window
// and another App's, after it opened as well. Every Window runs on one UI
// thread, as a dialog runs on its owner's.
func openingDesk(spy *headlessSpy, opened ...win.WindowInfo) {
	spy.svc.WindowThread = func(uint64) uint32 { return 11 }
	spy.svc.Surfaces = func() ([]win.WindowInfo, error) {
		ws := []win.WindowInfo{notepadMain, otherApp}
		if slices.Contains(spy.recorded(), "headless-click") {
			ws = append(append([]win.WindowInfo(nil), opened...), ws...)
		}
		return ws, nil
	}
	spy.svc.Sleep = func(time.Duration) {}
}

func TestBatchStepInItsOwnWindow(t *testing.T) {
	spy := newHeadlessSpy()
	spy.refIndex = 4
	out, e := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
		inStep("203", "set-value", protocol.SetValueArgs{Ref: "name:Address", Value: "x"}),
	}})
	if e != nil {
		t.Fatalf("batch: %v", e.Error())
	}
	if want := "Action 1 (click): ok\nAction 2 (set-value in 203): ok\n"; out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if spy.refHwnd != 203 {
		t.Errorf("name:Address resolved on window %d, want the step's own 203", spy.refHwnd)
	}
}

func TestBatchStepWindowThatDoesNotResolveFailsBeforeAnyStep(t *testing.T) {
	spy := newHeadlessSpy()
	_, e := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
		inStep("999", "click", protocol.ClickArgs{Ref: "name:OK"}),
	}})
	if e == nil || !strings.HasPrefix(e.Msg, "batch: Action 2 (click): ") || e.Out != "" {
		t.Errorf("batch: %+v, want Action 2's Window refused before any step", e)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("steps ran: %v", got)
	}
}

func TestBatchInNewActsInTheWindowTheStepBeforeOpened(t *testing.T) {
	spy := newHeadlessSpy()
	spy.refIndex = 6
	openingDesk(spy, saveDialog)
	out, e := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
		inNewStep("set-value", protocol.SetValueArgs{Ref: "name:File name", Value: "report.txt"}),
	}})
	if e != nil {
		t.Fatalf("batch: %v\n%s", e.Error(), e.Out)
	}
	want := "Action 1 (click): ok\nAction 2 (set-value in 777): ok\nNote: a new Window opened: 777 Notepad \"Save As\"\n"
	if out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if spy.refHwnd != 777 || spy.hValue != "report.txt" {
		t.Errorf("set-value on window %d with %q, want 777 and report.txt", spy.refHwnd, spy.hValue)
	}
}

func TestBatchInNewRefusedWithNothingSent(t *testing.T) {
	for _, c := range []struct {
		name    string
		opened  []win.WindowInfo
		clickOK bool
		want    string
	}{
		{"nothing opened", nil, true, "Action 1 opened no Window within 2s; nothing was sent"},
		{"two opened", []win.WindowInfo{saveDialog, editMenu}, true, `Action 1 opened 2 Windows (777 "Save As", 778 ""), and ` + "`in new`" + ` needs one; nothing was sent`},
		{"the step before failed", []win.WindowInfo{saveDialog}, false, "Action 1 failed, so no Window it opened is known; nothing was sent"},
	} {
		t.Run(c.name, func(t *testing.T) {
			spy := newHeadlessSpy()
			if !c.clickOK {
				spy.hClickErr = errFake("pattern call failed")
			}
			openingDesk(spy, c.opened...)
			_, e := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
				stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
				inNewStep("set-value", protocol.SetValueArgs{Ref: "name:File name", Value: "x"}),
			}})
			msg, _, ok := failedStep(e, 2, "set-value in new")
			if !ok || msg != c.want {
				t.Errorf("Action 2 failed with %q (%v), want %q; out %q", msg, ok, c.want, e.Out)
			}
			if slices.Contains(spy.recorded(), "headless-set-value") {
				t.Error("the `in new` step was sent")
			}
		})
	}
}

func TestBatchInNewRefusedBeforeAnyStep(t *testing.T) {
	spy := newHeadlessSpy()
	for _, c := range []struct {
		name  string
		steps []protocol.Step
		want  string
	}{
		{"first step", []protocol.Step{inNewStep("press-key", protocol.PressKeyArgs{Key: "Return"})},
			"batch: Action 1 (press-key): `in new` needs an Action before it"},
		{"with its own Window too", []protocol.Step{stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
			{Kind: "press-key", Args: batchStepArgs(protocol.PressKeyArgs{Key: "Return"}), Window: "203", InNew: true}},
			"batch: Action 2 (press-key): `in new` needs an Action before it and names no other Window"},
		{"an Element Index", []protocol.Step{stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
			inNewStep("click", protocol.ClickArgs{ElementIndex: idx(3)})},
			"batch: Action 2 (click) is in a new Window, which has no Window State or screenshot yet; nothing was sent"},
		{"an @x,y point", []protocol.Step{stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
			inNewStep("set-value", protocol.SetValueArgs{Point: &protocol.Point{X: 1, Y: 2}, Value: "x"})},
			"batch: Action 2 (set-value) is in a new Window"},
	} {
		_, e := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: c.steps})
		if e == nil || !strings.HasPrefix(e.Msg, c.want) || e.Out != "" {
			t.Errorf("%s: %+v, want %q before any step", c.name, e, c.want)
		}
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("steps ran: %v", got)
	}
}

func TestBatchInNewHeadedStepMakesTheBatchHeaded(t *testing.T) {
	// The new Window cannot be probed before it opens: an `in new` step's
	// own headed makes the Batch's scope Headed up front. (It is refused at
	// its turn here: nothing opened.)
	spy := newHeadlessSpy()
	openingDesk(spy)
	runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
		inNewStep("press-key", protocol.PressKeyArgs{Key: "Return", Headed: true}),
	}})
	if log := strings.Join(spy.esc.log, " "); !strings.HasPrefix(log, "arm ") {
		t.Errorf("Esc scopes %q, want the swallowing scope armed first", log)
	}
}

func TestBatchLanesAcrossWindows(t *testing.T) {
	spy := newHeadlessSpy()
	spy.svc.WindowThread = func(hwnd uint64) uint32 {
		return map[uint64]uint32{101: 11, 202: 22, 203: 22}[hwnd]
	}
	s := spy.svc
	ctx := context.Background()
	for _, c := range []struct {
		name  string
		steps []protocol.Step
		want  []string
	}{
		{"a step in another App's Window", []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
			inStep("202", "click", protocol.ClickArgs{Ref: "name:OK"}),
		}, []string{threadLane(11), threadLane(22)}},
		{"`in new` takes its opener's lanes", []protocol.Step{
			inStep("203", "click", protocol.ClickArgs{Ref: "name:Open"}),
			inNewStep("press-key", protocol.PressKeyArgs{Key: "Return"}),
		}, []string{threadLane(22)}},
		{"a headed `in new` step takes the desktop", []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
			inNewStep("press-key", protocol.PressKeyArgs{Key: "Return", Headed: true}),
		}, []string{desktopLane, threadLane(11)}},
		{"a step's Window that does not resolve", []protocol.Step{
			inStep("999", "click", protocol.ClickArgs{Ref: "name:OK"}),
		}, []string{desktopLane}},
	} {
		got := s.lanesFor(ctx, "batch", batchStepArgs(protocol.BatchArgs{Window: "101", Steps: c.steps})).keys
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: lanes %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBatchInNewWindowOnAnotherThreadIsRefused(t *testing.T) {
	spy := newHeadlessSpy()
	spy.svc.Surfaces = func() ([]win.WindowInfo, error) { return []win.WindowInfo{saveDialog, notepadMain}, nil }
	threads := map[uint64]uint32{101: 11, 777: 11}
	spy.svc.WindowThread = func(hwnd uint64) uint32 { return threads[hwnd] }
	ctx := withLaneSet(context.Background(), newLaneSet(101, threadLane(11)))
	step := func() *batchStep {
		return &batchStep{kind: "press-key", args: batchStepArgs(protocol.PressKeyArgs{Window: "101", Key: "Return"}), inNew: true, w: saveDialog}
	}
	st := step()
	if err := spy.svc.inNewWindow(ctx, st); err != nil || !strings.Contains(string(st.args), `"window":"777"`) {
		t.Errorf("a dialog on its owner's thread: %+v, args %s", err, st.args)
	}
	threads[777] = 77
	st = step()
	err := spy.svc.inNewWindow(ctx, st)
	if err == nil || !strings.HasPrefix(err.Msg, "window 777 needs a lane this Batch did not take") || !strings.HasSuffix(err.Msg, "nothing was sent") {
		t.Errorf("a dialog on another thread: %+v, want refused", err)
	}
	if err := spy.svc.inNewWindow(context.Background(), step()); err != nil {
		t.Errorf("outside the lanes: %+v, want it to pass", err)
	}
}

func TestBatchReportNamesEachUncertainStepsWindow(t *testing.T) {
	e := batchReport(3, []batchFailure{
		{n: 1, kind: "click", window: 101, uncertain: true},
		{n: 2, kind: "press-key", window: 777, uncertain: true},
		{n: 3, kind: "set-value", window: 101, uncertain: true},
	})
	if want := "read the Window States (winuse get-window-state 101; winuse get-window-state 777) before repeating them"; !strings.Contains(e.Hint, want) {
		t.Errorf("hint %q, want %q", e.Hint, want)
	}
	e = batchReport(2, []batchFailure{{n: 2, kind: "click", window: 101, uncertain: true}})
	if want := "Action 2 may have taken effect: read the Window State (winuse get-window-state 101) before repeating it"; !strings.HasPrefix(e.Hint, want) {
		t.Errorf("hint %q, want %q", e.Hint, want)
	}
}

func TestPoliteBatchGivesBackFromAnyOfItsWindows(t *testing.T) {
	// A Headed Batch whose last step closed the dialog it acted in: the
	// foreground fell back to the dialog's owner, which an earlier step
	// acted in, so it is still the Batch's and is given back (ma-08).
	spy, d := politeSpy()
	d.fg = 101
	main, dialog := win.WindowInfo{ID: 101, PID: 7}, win.WindowInfo{ID: 777, PID: 7}
	r := &politeRun{saved: savedDesk{foreground: 500, windows: map[uint64]bool{101: true, 500: true}}, headed: true,
		target: dialog, visited: []win.WindowInfo{main, dialog}}
	if note := spy.svc.giveBack(r); note != "" || d.fg != 500 {
		t.Errorf("note %q, foreground %d; want it given back to 500", note, d.fg)
	}
	// A foreground on a Window no step acted in is the user's.
	d.fg = 202
	r.visited = []win.WindowInfo{dialog}
	if note := spy.svc.giveBack(r); !strings.Contains(note, "it moved to window 202") || d.fg != 202 {
		t.Errorf("note %q, foreground %d; want it left on 202", note, d.fg)
	}
}

func TestBatchStepInAHungWindowIsRefused(t *testing.T) {
	// ma-07's hung pre-check covers a step's own Window at its turn; the
	// Batch runs on past it.
	spy := newHeadlessSpy()
	spy.svc.WindowHung = func(hwnd uint64) bool { return hwnd == 203 }
	_, e := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		inStep("203", "set-value", protocol.SetValueArgs{Ref: "name:Address", Value: "x"}),
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
	}})
	if msg, _, ok := failedStep(e, 1, "set-value in 203"); !ok || msg != "window 203's App is not responding; nothing was sent" {
		t.Errorf("Action 1: %q (%v); out %q", msg, ok, e.Out)
	}
	if !strings.Contains(e.Out, "Action 2 (click): ok") || slices.Contains(spy.recorded(), "headless-set-value") {
		t.Errorf("out %q, steps %v; want only Action 2 sent", e.Out, spy.recorded())
	}
}

func TestBatchInNewRefusalCodes(t *testing.T) {
	// desktop-parity 07's codes: the `in new` refusals set their own.
	spy := newHeadlessSpy()
	openingDesk(spy, saveDialog, editMenu)
	stopped := noopEscWatch{}
	before, _ := spy.svc.snapshotWindow(notepadMain)
	spy.step("headless-click") // the click opened both
	for _, c := range []struct {
		name string
		err  *Error
		want string
	}{
		{"the step before failed", errOf(spy.svc.openedByStep(context.Background(), stopped, 1, Fail("x", ""), before, true)), protocol.CodeNotFound},
		{"two opened", errOf(spy.svc.openedByStep(context.Background(), stopped, 1, nil, before, true)), protocol.CodeAmbiguousTarget},
	} {
		if c.err == nil || c.err.Code != c.want {
			t.Errorf("%s: %+v, want code %s", c.name, c.err, c.want)
		}
	}
	spy.svc.Surfaces = func() ([]win.WindowInfo, error) { return []win.WindowInfo{notepadMain}, nil }
	if _, e := spy.svc.openedByStep(context.Background(), stopped, 1, nil, before, true); e == nil || e.Code != protocol.CodeNotFound {
		t.Errorf("nothing opened: %+v, want %s", e, protocol.CodeNotFound)
	}
	lane := "window 777 needs a lane this Batch did not take when it started (x); nothing was sent"
	if code, _ := protocol.CodeFor(lane, "", 1); code != protocol.CodeUsage {
		t.Errorf("the lane refusal reads as %s, want %s", code, protocol.CodeUsage)
	}
}

func errOf(_ win.WindowInfo, e *Error) *Error { return e }
