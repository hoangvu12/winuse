package helper

// The watched settle's offline tests (speed ticket 01, ADR 0019): with the
// App watch, an Action that opens nothing returns once the App is quiet
// instead of after the whole 300 ms; a busy App or a window still to show
// keeps the looks going to the end of the settle; a window that shows is
// looked at at once; every case reports exactly the notes the polled
// settle reports; and the Windows the watch cannot follow keep the polled
// settle. The App is a scripted fake; no hook is installed.

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// fakeWatch is the scripted App: signals answers each Wait in turn (then
// false), samples each Sample in turn (the last repeats).
type fakeWatch struct {
	mu      sync.Mutex
	signals []bool
	samples []win.AppSample
	pending bool
	waits   int
	sampled int
	closed  bool
}

func (w *fakeWatch) Wait(time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.waits++
	if len(w.signals) == 0 {
		return false
	}
	s := w.signals[0]
	w.signals = w.signals[1:]
	return s
}

func (w *fakeWatch) Pending() bool { return w.pending }

func (w *fakeWatch) Sample() win.AppSample {
	w.mu.Lock()
	defer w.mu.Unlock()
	i := min(w.sampled, len(w.samples)-1)
	w.sampled++
	return w.samples[i]
}

func (w *fakeWatch) Close() { w.closed = true }

var (
	quietApp = win.AppSample{OK: true, UIWaiting: true, Switches: map[uint32]uint32{1: 10}}
	// busyApp's threads run between every two samples.
	busyApp = func(n int) []win.AppSample {
		out := make([]win.AppSample, n)
		for i := range out {
			out[i] = win.AppSample{OK: true, UIWaiting: true, Switches: map[uint32]uint32{1: uint32(i)}}
		}
		return out
	}
)

// wireWatch makes svc watch the settle with w, recording the targets.
func wireWatch(svc *Services, w *fakeWatch, targets *[]win.WindowInfo) {
	svc.WatchApp = func(target win.WindowInfo) (AppWatch, error) {
		*targets = append(*targets, target)
		return w, nil
	}
}

// looks counts the settle's snapshots: every Surfaces call but the one
// before the Action.
func (d *surfaceDesk) looks() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls - 1
}

func TestWatchedSettleStopsOnceTheAppIsQuiet(t *testing.T) {
	spy := newHeadlessSpy()
	desk := newDesk([]win.WindowInfo{notepadMain, otherApp})
	desk.wire(&spy.svc)
	w := &fakeWatch{samples: []win.AppSample{quietApp}}
	var targets []win.WindowInfo
	wireWatch(&spy.svc, w, &targets)

	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e != nil {
		t.Fatalf("click: %v", e.Error())
	}
	if out != "ok\n" {
		t.Errorf("output %q, want ok alone", out)
	}
	if len(targets) != 1 || targets[0].ID != notepadMain.ID || targets[0].PID != notepadPID {
		t.Errorf("watched %+v, want the target as the snapshot saw it", targets)
	}
	// The polled settle looks 6 times over 300 ms; the watched one looks at
	// 50 ms, as before, and once more at 70 ms, where the App is found quiet.
	if got := desk.looks(); got != 2 {
		t.Errorf("the settle looked %d times, want 2 (it stops at %s)", got, openedLooks()[1])
	}
	if at := openedLooks()[1]; at != 70*time.Millisecond {
		t.Errorf("a quiet App ends the settle at %s, want 70ms (it was %s)", at, openedSettle)
	}
	if desk.sleeps != 0 {
		t.Errorf("the watched settle paused %d times through Sleep, want it to wait on the watch", desk.sleeps)
	}
	if !w.closed {
		t.Error("the watch was not closed after the settle")
	}
}

func TestWatchedSettleKeepsLookingWhileTheAppIsBusy(t *testing.T) {
	spy := newHeadlessSpy()
	desk := newDesk([]win.WindowInfo{notepadMain, otherApp})
	desk.wire(&spy.svc)
	w := &fakeWatch{samples: busyApp(len(openedLooks()))}
	var targets []win.WindowInfo
	wireWatch(&spy.svc, w, &targets)

	out, e := runCommand(t, CommandsWith(spy.svc), "set-value", protocol.SetValueArgs{Window: "101", ElementIndex: idx(1), Value: "x"})
	if e != nil || out != "ok\n" {
		t.Fatalf("set-value: output %q, error %v", out, e)
	}
	if got, want := desk.looks(), len(openedLooks()); got != want {
		t.Errorf("a busy App's settle looked %d times, want every look to the end (%d)", got, want)
	}
	if last := openedLooks()[len(openedLooks())-1]; last != openedSettle {
		t.Errorf("the last look is at %s, want the polled settle's end %s", last, openedSettle)
	}
}

// A window the App created that has not shown yet holds the settle open
// though the App is quiet: the menu that shows later is still reported.
func TestWatchedSettleWaitsForAPendingWindow(t *testing.T) {
	spy := newHeadlessSpy()
	same := []win.WindowInfo{notepadMain, otherApp}
	desk := newDesk(same, same, same, []win.WindowInfo{editMenu, notepadMain, otherApp})
	desk.wire(&spy.svc)
	w := &fakeWatch{samples: []win.AppSample{quietApp}, pending: true}
	var targets []win.WindowInfo
	wireWatch(&spy.svc, w, &targets)

	out, e := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action",
		protocol.SecondaryActionArgs{Window: "101", ElementIndex: idx(3), Action: "expand"})
	if e != nil {
		t.Fatalf("Expand: %v", e.Error())
	}
	if want := "ok\nNote: a new Window opened: 778 Notepad \"\"\n"; out != want || desk.looks() != 4 {
		t.Errorf("output %q after %d looks, want %q on the fourth", out, desk.looks(), want)
	}
}

// A window that shows wakes the settle: it looks at once, before its
// first timed look, and reports the window as the polled settle does.
func TestWatchedSettleLooksAtOnceWhenAWindowShows(t *testing.T) {
	spy := newHeadlessSpy()
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherNew, otherApp})
	desk.wire(&spy.svc)
	w := &fakeWatch{signals: []bool{true}, samples: []win.AppSample{quietApp}}
	var targets []win.WindowInfo
	wireWatch(&spy.svc, w, &targets)

	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e != nil {
		t.Fatalf("click: %v", e.Error())
	}
	if want := "ok\nNote: a new Window opened: 777 Notepad \"Save As\"\n"; out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if w.waits != 1 || desk.looks() != 1 || w.sampled != 0 {
		t.Errorf("%d waits, %d looks, %d samples; want the signal's look alone", w.waits, desk.looks(), w.sampled)
	}
}

// Every case reports exactly what the polled settle reports: the same
// notes for a dialog, a menu with a tooltip skipped, a late Window, the
// target closing, another App's Window and nothing new. The watched App
// runs until its Window shows, as a real App does.
func TestWatchedSettleReportsWhatThePolledSettleReports(t *testing.T) {
	same := []win.WindowInfo{notepadMain, otherApp}
	cases := []struct {
		name  string
		after [][]win.WindowInfo
	}{
		{"a dialog", [][]win.WindowInfo{{saveDialog, notepadMain, otherApp}}},
		{"a menu and a tooltip", [][]win.WindowInfo{{tooltip, editMenu, notepadMain, otherApp}}},
		{"a late menu", [][]win.WindowInfo{same, same, same, {editMenu, notepadMain, otherApp}}},
		{"the last look's menu", [][]win.WindowInfo{same, same, same, same, same, {editMenu, notepadMain, otherApp}}},
		{"the target closed", [][]win.WindowInfo{{otherApp}}},
		{"another App's Window", [][]win.WindowInfo{{otherNew, notepadMain, otherApp}}},
		{"nothing new", [][]win.WindowInfo{same}},
	}
	for _, c := range cases {
		run := func(watched bool) string {
			spy := newHeadlessSpy()
			desk := newDesk(c.after...)
			desk.wire(&spy.svc)
			if watched {
				// Busy for as many looks as the Window takes to show.
				samples := busyApp(len(c.after) + 1)
				samples = append(samples, samples[len(samples)-1])
				var targets []win.WindowInfo
				wireWatch(&spy.svc, &fakeWatch{samples: samples}, &targets)
			}
			out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
			if e != nil {
				t.Fatalf("%s: click: %v", c.name, e.Error())
			}
			return out
		}
		if polled, watched := run(false), run(true); watched != polled {
			t.Errorf("%s: watched settle reported %q, the polled one %q", c.name, watched, polled)
		}
	}
}

// The Windows the watch cannot follow keep the polled settle: a Chromium
// or Electron Window, a shell surface, and an App the watch cannot read.
func TestWatchedSettleOnlyWhereTheWatchFollows(t *testing.T) {
	chromium := notepadMain
	chromium.Class = "Chrome_WidgetWin_1"
	taskbar := notepadMain
	taskbar.Class, taskbar.Shell = "Shell_TrayWnd", win.ShellTaskbar
	for _, target := range []win.WindowInfo{chromium, taskbar} {
		if watchesSettle(target) {
			t.Errorf("the settle after an Action on %s %q is watched", target.Class, target.Shell)
		}
	}
	if !watchesSettle(notepadMain) {
		t.Error("the settle after an Action on a classic Window is not watched")
	}

	spy := newHeadlessSpy()
	desk := newDesk([]win.WindowInfo{notepadMain, otherApp})
	desk.wire(&spy.svc)
	spy.svc.WatchApp = func(win.WindowInfo) (AppWatch, error) { return nil, errors.New("no process") }
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e != nil || out != "ok\n" {
		t.Fatalf("click: output %q, error %v", out, e)
	}
	if want := int(openedSettle / openedPoll); desk.sleeps != want {
		t.Errorf("an unwatched settle paused %d times, want the polled settle (%d)", desk.sleeps, want)
	}
}

// A failed Action starts no settle; its watch is still closed.
func TestWatchedSettleClosedAfterAFailedAction(t *testing.T) {
	spy := newHeadlessSpy()
	spy.hClickErr = errFake("pattern call failed")
	desk := newDesk([]win.WindowInfo{saveDialog, notepadMain, otherApp})
	desk.wire(&spy.svc)
	w := &fakeWatch{samples: []win.AppSample{quietApp}}
	var targets []win.WindowInfo
	wireWatch(&spy.svc, w, &targets)
	if _, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left")); e == nil {
		t.Fatal("the click did not fail")
	}
	if !w.closed || w.waits != 0 || desk.looks() != 0 {
		t.Errorf("closed %v, %d waits, %d looks; want a closed watch and no settle", w.closed, w.waits, desk.looks())
	}
}

// timedWatch is a watch on the wall clock: Wait sleeps its whole span (no
// window shows) and Sample costs what win.BenchmarkSampleProcess measured
// for one real sample (≈3 ms).
type timedWatch struct{ samples []win.AppSample }

func (w *timedWatch) Wait(d time.Duration) bool {
	if d > 0 {
		time.Sleep(d)
	}
	return false
}
func (w *timedWatch) Pending() bool { return false }
func (w *timedWatch) Sample() win.AppSample {
	time.Sleep(3 * time.Millisecond)
	s := w.samples[0]
	if len(w.samples) > 1 {
		w.samples = w.samples[1:]
	}
	return s
}
func (w *timedWatch) Close() {}

// BenchmarkOpenedSettle times the settle after an Action that opens
// nothing, on the wall clock: polled (the whole 300 ms), watched with a
// quiet App, and watched with an App that never goes quiet.
func BenchmarkOpenedSettle(b *testing.B) {
	desk := newDesk([]win.WindowInfo{notepadMain, otherApp})
	svc := Services{Surfaces: desk.surfaces, Sleep: time.Sleep, Now: time.Now}
	before, ok := svc.snapshotWindow(notepadMain)
	if !ok {
		b.Fatal("no snapshot")
	}
	for _, c := range []struct {
		name  string
		watch func() AppWatch
	}{
		{"polled", func() AppWatch { return nil }},
		{"watched-quiet", func() AppWatch { return &timedWatch{samples: []win.AppSample{quietApp}} }},
		{"watched-busy", func() AppWatch { return &timedWatch{samples: busyApp(len(openedLooks()))} }},
	} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				if notes := svc.openedNotes(b.Context(), before, true, nil, c.watch()); notes != "" {
					b.Fatalf("notes %q, want none", notes)
				}
			}
		})
	}
}
