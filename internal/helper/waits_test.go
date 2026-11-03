package helper

// The desktop waits' offline tests (desktop-parity ticket 03): window,
// text, element and pause waits over faked state that changes from one
// poll to the next. Sleep is faked, so every poll runs instantly and the
// tests count the polls; no desktop is touched.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// waitDesk fakes the desktop over time for the waits: the Windows, a
// Window's tree and one held Element each change at a poll the test
// names. It is the Services' Trees (TreeReader and TreePeeker) and Actions
// (ElementPointer, ElementResolver, ElementHolder and PageSplicer).
type waitDesk struct {
	mu sync.Mutex

	windows func(poll int) []win.WindowInfo // the visible Windows at each Window list read
	lists   int                             // Window list reads

	trees    func(peek int) *wstate.Tree // the Window's tree at each peek
	peeks    int
	peekErr  error
	captures int // Capture calls: a wait must never replace the stored Window State

	states  func(read int) (uia.ElementState, error) // the held Element at each read
	reads   int
	holds   int
	holdErr error
	release int
	held    []int // the Element Indexes held

	open  func(poll int) bool // the Window is open at each WindowOpen probe
	opens int

	refIndex int
	refs     []string
	pageEls  map[int]bool // Element Indexes that are Page Elements

	sleeps []time.Duration
	esc    *scopeEsc
	stopAt int // the read (element, peek or sleep) at which the user presses Esc; 0: never
}

func newWaitDesk() *waitDesk {
	d := &waitDesk{esc: &scopeEsc{}}
	d.windows = func(int) []win.WindowInfo { return fakeWindows() }
	d.open = func(int) bool { return true }
	return d
}

func (d *waitDesk) services() Services {
	svc := testServices(d)
	svc.Actions = d
	svc.Esc = d.esc
	svc.Windows = func() ([]win.WindowInfo, error) {
		d.mu.Lock()
		d.lists++
		n := d.lists
		d.mu.Unlock()
		return d.windows(n), nil
	}
	svc.WindowOpen = func(uint64) bool {
		d.mu.Lock()
		d.opens++
		n := d.opens
		d.mu.Unlock()
		return d.open(n)
	}
	svc.Sleep = func(dur time.Duration) {
		d.mu.Lock()
		d.sleeps = append(d.sleeps, dur)
		n := len(d.sleeps)
		d.mu.Unlock()
		d.maybeStop(n)
	}
	return svc
}

func (d *waitDesk) maybeStop(n int) {
	if d.stopAt != 0 && n >= d.stopAt {
		d.esc.mu.Lock()
		d.esc.pressed = true
		d.esc.mu.Unlock()
	}
}

func (d *waitDesk) slept() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	var total time.Duration
	for _, s := range d.sleeps {
		total += s
	}
	return total
}

func (d *waitDesk) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.captures++
	return &wstate.Tree{Title: title, App: app, FocusedIndex: -1}, nil
}

func (d *waitDesk) PeekTree(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.peeks++
	if d.peekErr != nil {
		return nil, d.peekErr
	}
	return d.trees(d.peeks), nil
}

func (d *waitDesk) ElementPoint(ctx context.Context, hwnd uint64, index int) (uia.Point, error) {
	return uia.Point{}, errors.New("a wait never aims at a point")
}

func (d *waitDesk) ResolveElementRef(ctx context.Context, hwnd uint64, ref string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refs = append(d.refs, ref)
	return d.refIndex, nil
}

func (d *waitDesk) SplicePage(ctx context.Context, hwnd uint64, base, merged []wstate.Element, from []int, origins map[int]any) error {
	return nil
}

func (d *waitDesk) ElementOrigin(ctx context.Context, hwnd uint64, index int) (any, error) {
	if d.pageEls[index] {
		return struct{}{}, nil
	}
	return nil, nil
}

func (d *waitDesk) HoldElement(ctx context.Context, hwnd uint64, index int) (uia.Held, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.holds++
	d.held = append(d.held, index)
	if d.holdErr != nil {
		return nil, d.holdErr
	}
	return &waitHeldElement{d: d}, nil
}

// waitHeldElement is the waitDesk's uia.Held.
type waitHeldElement struct{ d *waitDesk }

func (h *waitHeldElement) State(ctx context.Context) (uia.ElementState, error) {
	h.d.mu.Lock()
	h.d.reads++
	n := h.d.reads
	h.d.mu.Unlock()
	return h.d.states(n)
}

func (h *waitHeldElement) Release() {
	h.d.mu.Lock()
	h.d.release++
	h.d.mu.Unlock()
}

// runWait runs one wait through the registry the helper serves.
func runWait(t *testing.T, d *waitDesk, a protocol.WaitArgs) (string, *Error) {
	t.Helper()
	return runCommand(t, CommandsWith(d.services()), "wait", a)
}

func intp(n int) *int { return &n }

func TestWaitWindowAppearsAndPrintsItsLine(t *testing.T) {
	dialog := win.WindowInfo{ID: 404, App: "Notepad", ExePath: `C:\Windows\notepad.exe`, Title: "Save As"}
	d := newWaitDesk()
	d.windows = func(n int) []win.WindowInfo {
		if n < 4 {
			return fakeWindows()
		}
		return append([]win.WindowInfo{dialog}, fakeWindows()...)
	}
	out, e := runWait(t, d, protocol.WaitArgs{For: "window", Value: "save as"})
	if e != nil {
		t.Fatalf("wait window: %v", e)
	}
	if out != win.FormatWindows([]win.WindowInfo{dialog}) {
		t.Errorf("output %q, want the dialog's list-windows line", out)
	}
	if len(d.sleeps) != 3 || d.sleeps[0] != windowWaitPoll {
		t.Errorf("sleeps %v, want 3 of %s", d.sleeps, windowWaitPoll)
	}
}

func TestWaitWindowByAppPrintsEveryWindowOfIt(t *testing.T) {
	d := newWaitDesk()
	out, e := runWait(t, d, protocol.WaitArgs{For: "window", Value: "chrome"})
	if e != nil {
		t.Fatalf("wait window: %v", e)
	}
	if want := win.FormatWindows(fakeWindows()[1:]); out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if len(d.sleeps) != 0 {
		t.Errorf("an open Window holds at once; slept %v", d.sleeps)
	}
}

func TestWaitWindowGone(t *testing.T) {
	d := newWaitDesk()
	d.windows = func(n int) []win.WindowInfo {
		if n < 3 {
			return fakeWindows()
		}
		return fakeWindows()[1:]
	}
	out, e := runWait(t, d, protocol.WaitArgs{For: "window", Value: "Notepad", Gone: true})
	if e != nil || out != okOutput {
		t.Fatalf("wait window --gone = %q, %v", out, e)
	}
}

func TestWaitWindowTimesOut(t *testing.T) {
	d := newWaitDesk()
	_, e := runWait(t, d, protocol.WaitArgs{For: "window", Value: "Calculator", TimeoutMS: 1000})
	if e == nil || !strings.Contains(e.Msg, `waiting for a Window of "Calculator": the condition did not hold within 1s`) ||
		!strings.Contains(e.Hint, "up to 300") {
		t.Fatalf("timeout = %v", e)
	}
	if got := d.slept(); got != time.Second {
		t.Errorf("slept %s, want the 1 s wait", got)
	}
	_, e = runWait(t, newWaitDesk(), protocol.WaitArgs{For: "window", Value: "Notepad", Gone: true, TimeoutMS: 500})
	if e == nil || !strings.Contains(e.Msg, "still open:\n101\tNotepad\tUntitled - Notepad") {
		t.Fatalf("--gone timeout = %v, want the open Window named", e)
	}
}

func TestWindowsMatchingAppOrTitle(t *testing.T) {
	ws := fakeWindows()
	for target, want := range map[string][]uint64{
		"notepad.exe":   {101},
		"NOTEPAD":       {101},
		"untitled":      {101},
		"Chrome":        {202, 203},
		"two":           {203},
		"o":             {101, 202, 203}, // "Notepad" and "One" hold it; "Two" too
		"nothing here!": nil,
	} {
		var got []uint64
		for _, w := range windowsMatching(ws, target) {
			got = append(got, w.ID)
		}
		if len(got) != len(want) || (len(got) > 0 && !equalIDs(got, want)) {
			t.Errorf("%q: %v, want %v", target, got, want)
		}
	}
}

func equalIDs(a, b []uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWaitTreeTextKeepsTheWindowState(t *testing.T) {
	d := newWaitDesk()
	d.trees = func(n int) *wstate.Tree {
		tree := cannedTree()
		if n >= 3 {
			tree.Elements = append(tree.Elements, wstate.Element{Depth: 1, Role: "edit", Name: "Status", Value: "Saved 3 files"})
		}
		return tree
	}
	out, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "text", Value: "Saved 3"})
	if e != nil || out != okOutput {
		t.Fatalf("wait text = %q, %v", out, e)
	}
	if d.peeks != 3 || d.captures != 0 {
		t.Errorf("peeks %d, captures %d: want 3 peeks and the stored Window State untouched", d.peeks, d.captures)
	}
	if len(d.sleeps) != 2 || d.sleeps[0] != treeWaitPoll {
		t.Errorf("sleeps %v, want 2 of %s", d.sleeps, treeWaitPoll)
	}
}

func TestTreeHasTextMatchesNameValueAndDescription(t *testing.T) {
	tree := &wstate.Tree{Elements: []wstate.Element{{Name: "Open file"}, {Value: "42 kB"}, {Description: "Press F1"}}}
	for text, want := range map[string]bool{"Open": true, "42 kB": true, "F1": true, "open": false, "Save": false} {
		if got := treeHasText(tree, text); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}

func TestWaitTreeTextWindowClosed(t *testing.T) {
	d := newWaitDesk()
	d.trees = func(int) *wstate.Tree { return cannedTree() }
	d.open = func(n int) bool { return n < 3 }
	_, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "text", Value: "Done"})
	if e == nil || e.Msg != `waiting for text "Done" in window 101: the Window closed` {
		t.Fatalf("closed = %v", e)
	}
	d = newWaitDesk()
	d.peekErr = uia.ErrWindowGone
	if _, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "text", Value: "Done"}); e == nil || !strings.Contains(e.Msg, "the Window closed") {
		t.Fatalf("a gone read = %v, want the Window closed", e)
	}
}

func TestWaitTextOnABrowserWindowKeepsThePageRoute(t *testing.T) {
	d := newWaitDesk()
	svc := d.services()
	svc.Windows = func() ([]win.WindowInfo, error) {
		return []win.WindowInfo{{ID: 301, App: "Chrome", Class: chromiumWindowClass, Title: "Site"}}, nil
	}
	svc.Pages = &fakePages{}
	if svc.desktopWait(protocol.WaitArgs{Window: "301", For: "text", Value: "x"}) {
		t.Error("a browser Window's text wait left the Page's route")
	}
	if !svc.desktopWait(protocol.WaitArgs{Window: "301", For: "element", ElementIndex: intp(3), State: "enabled"}) {
		t.Error("an element wait takes the desktop route on any Window")
	}
	for _, kind := range []string{"selector", "url", "load", "download"} {
		if svc.desktopWait(protocol.WaitArgs{Window: "101", For: kind, Value: "x"}) {
			t.Errorf("%s left the Page's route", kind)
		}
	}
	if !testServicesDesktop().desktopWait(protocol.WaitArgs{Window: "101", For: "text", Value: "x"}) {
		t.Error("Notepad's text wait is no desktop wait")
	}
}

func testServicesDesktop() Services { return defaultServices(newWaitDesk().services()) }

func TestWaitElementReachesEachState(t *testing.T) {
	cases := []struct {
		state, value string
		before       uia.ElementState
		after        uia.ElementState
	}{
		{"enabled", "", uia.ElementState{}, uia.ElementState{Enabled: true}},
		{"disabled", "", uia.ElementState{Enabled: true}, uia.ElementState{}},
		{"value", "done", uia.ElementState{HasValue: true, Value: "working"}, uia.ElementState{HasValue: true, Value: "done"}},
		{"value", "", uia.ElementState{HasValue: true, Value: "x"}, uia.ElementState{HasValue: true}},
		{"checked", "", uia.ElementState{Toggle: "off"}, uia.ElementState{Toggle: "on"}},
		{"unchecked", "", uia.ElementState{Toggle: "on"}, uia.ElementState{Toggle: "off"}},
		{"checked", "", uia.ElementState{Selectable: true}, uia.ElementState{Selectable: true, Selected: true}},
		{"expanded", "", uia.ElementState{Expand: "collapsed"}, uia.ElementState{Expand: "expanded"}},
		{"collapsed", "", uia.ElementState{Expand: "partially expanded"}, uia.ElementState{Expand: "collapsed"}},
	}
	for _, c := range cases {
		d := newWaitDesk()
		d.states = func(n int) (uia.ElementState, error) {
			if n < 3 {
				return c.before, nil
			}
			return c.after, nil
		}
		out, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(4), State: c.state, Value: c.value})
		if e != nil || out != okOutput {
			t.Errorf("%s %q: %q, %v", c.state, c.value, out, e)
			continue
		}
		if d.holds != 1 || d.held[0] != 4 || d.release != 1 || d.reads != 3 {
			t.Errorf("%s: holds %d of %v, releases %d, reads %d: want element 4 held once, read 3 times, released", c.state, d.holds, d.held, d.release, d.reads)
		}
	}
}

func TestWaitElementResolvesANameOnce(t *testing.T) {
	d := newWaitDesk()
	d.refIndex = 7
	d.states = func(n int) (uia.ElementState, error) { return uia.ElementState{Enabled: n > 2}, nil }
	if _, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", Ref: "name:Save", State: "enabled"}); e != nil {
		t.Fatal(e)
	}
	if len(d.refs) != 1 || d.refs[0] != "name:Save" || d.holds != 1 || d.held[0] != 7 {
		t.Errorf("refs %v, holds %d of %v: want name:Save resolved once to 7", d.refs, d.holds, d.held)
	}
}

func TestWaitElementGone(t *testing.T) {
	stale := errors.New("x")
	stale = errors.Join(uia.ErrStale, stale)
	d := newWaitDesk()
	d.states = func(n int) (uia.ElementState, error) {
		if n < 3 {
			return uia.ElementState{Enabled: true}, nil
		}
		return uia.ElementState{}, stale
	}
	if out, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(2), State: "gone"}); e != nil || out != okOutput {
		t.Fatalf("gone = %q, %v", out, e)
	}
	// Its Window closing takes the Element with it.
	d = newWaitDesk()
	d.states = func(int) (uia.ElementState, error) { return uia.ElementState{}, stale }
	d.open = func(int) bool { return false }
	if out, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(2), State: "gone"}); e != nil || out != "ok\nNote: the Window closed\n" {
		t.Fatalf("gone with its Window = %q, %v", out, e)
	}
}

// TestWaitElementGoneWithItsWindowWhateverTheReadSays follows the live
// path of the e2e dialog test (fx-gone-wait-window-closed): the Element
// reads live, its Window closes, and the next read fails with neither a
// stale HRESULT nor success. The Element went with its Window all the
// same.
func TestWaitElementGoneWithItsWindowWhateverTheReadSays(t *testing.T) {
	for name, err := range map[string]error{
		"a failed read": errors.New("reading the Element's state failed (HRESULT 0x80070578)"),
		"a read that still succeeds": nil,
		"a replaced worker":          uia.ErrWorkerGone,
	} {
		d := newWaitDesk()
		d.refIndex = 2
		d.states = func(n int) (uia.ElementState, error) {
			if n < 3 {
				return uia.ElementState{Enabled: true}, nil
			}
			return uia.ElementState{}, err
		}
		d.open = func(n int) bool { return n < 3 }
		out, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", Ref: "id:1001", State: "gone"})
		if e != nil || out != "ok\nNote: the Window closed\n" {
			t.Errorf("%s: gone with its Window = %q, %v", name, out, e)
		}
		if d.reads != 3 || d.release != 1 {
			t.Errorf("%s: reads %d, releases %d: want 3 reads and the Element released", name, d.reads, d.release)
		}
	}
	// Any other state still fails on a closed Window.
	d := newWaitDesk()
	d.states = func(int) (uia.ElementState, error) { return uia.ElementState{}, errors.New("HRESULT 0x80070578") }
	d.open = func(int) bool { return false }
	if _, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(2), State: "enabled"}); e == nil || !strings.Contains(e.Msg, "the Window closed") {
		t.Fatalf("enabled on a closed Window = %v, want the Window closed", e)
	}
}

func TestWaitElementWindowClosed(t *testing.T) {
	d := newWaitDesk()
	d.states = func(n int) (uia.ElementState, error) {
		if n < 2 {
			return uia.ElementState{}, nil
		}
		return uia.ElementState{}, uia.ErrStale
	}
	d.open = func(n int) bool { return n < 2 }
	_, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(2), State: "enabled"})
	if e == nil || e.Msg != "waiting for element 2 of window 101 to be enabled: the Window closed" {
		t.Fatalf("closed = %v", e)
	}
	// An Element that goes while its Window stays fails as stale.
	d = newWaitDesk()
	d.states = func(int) (uia.ElementState, error) { return uia.ElementState{}, uia.ErrStale }
	if _, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(2), State: "enabled"}); e == nil || !strings.Contains(e.Hint, "get-window-state 101 again") {
		t.Fatalf("stale = %v", e)
	}
}

func TestWaitElementTimeoutSaysWhatItRead(t *testing.T) {
	d := newWaitDesk()
	d.states = func(int) (uia.ElementState, error) { return uia.ElementState{}, nil }
	_, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(2), State: "enabled", TimeoutMS: 1000})
	if e == nil || e.Msg != "waiting for element 2 of window 101 to be enabled: the condition did not hold within 1s; it reads disabled" {
		t.Fatalf("timeout = %v", e)
	}
	d = newWaitDesk()
	d.states = func(int) (uia.ElementState, error) {
		return uia.ElementState{HasValue: true, Secret: true, Value: "hunter2"}, nil
	}
	_, e = runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(2), State: "value", Value: "secret", TimeoutMS: 500})
	if e == nil || strings.Contains(e.Msg, "hunter2") || !strings.Contains(e.Msg, "a password field's value is not shown") {
		t.Fatalf("a password field's timeout = %v", e)
	}
}

func TestWaitElementRefusesAStateItLacks(t *testing.T) {
	for _, state := range []string{"value", "checked", "expanded"} {
		d := newWaitDesk()
		d.states = func(int) (uia.ElementState, error) { return uia.ElementState{Enabled: true, Expand: "a leaf"}, nil }
		_, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(2), State: state, Value: map[bool]string{true: "x"}[state == "value"]})
		if e == nil || !strings.Contains(e.Msg, "so it cannot be waited on for "+state) || d.reads != 1 {
			t.Errorf("%s: %v after %d reads, want a refusal on the first", state, e, d.reads)
		}
	}
}

func TestWaitElementRefusals(t *testing.T) {
	d := newWaitDesk()
	d.pageEls = map[int]bool{5: true}
	if _, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(5), State: "enabled"}); e == nil || !strings.Contains(e.Hint, "winuse wait 101 selector <css>") || d.holds != 0 {
		t.Errorf("a Page Element = %v (holds %d)", e, d.holds)
	}
	d = newWaitDesk()
	d.holdErr = uia.ErrNoState
	if _, e := runWait(t, d, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: intp(1), State: "gone"}); e == nil || !strings.Contains(e.Msg, "no cached Window State") {
		t.Errorf("no Window State = %v", e)
	}
	for _, a := range []protocol.WaitArgs{
		{Window: "101", For: "element", ElementIndex: intp(1), State: "visible"},
		{Window: "101", For: "element", ElementIndex: intp(1), State: "enabled", Value: "x"},
		{Window: "101", For: "element", State: "enabled"},
		{Window: "101", For: "element", ElementIndex: intp(1), Ref: "name:x", State: "enabled"},
		{For: "element", ElementIndex: intp(1), State: "enabled"},
	} {
		if _, e := runWait(t, newWaitDesk(), a); e == nil {
			t.Errorf("%+v was accepted", a)
		}
	}
}

func TestWaitPause(t *testing.T) {
	d := newWaitDesk()
	out, e := runWait(t, d, protocol.WaitArgs{For: "ms", MS: 1250})
	if e != nil || out != okOutput {
		t.Fatalf("pause = %q, %v", out, e)
	}
	if got := d.slept(); got != 1250*time.Millisecond || d.sleeps[0] != pauseStep {
		t.Errorf("slept %s in %v, want 1.25 s in %s steps", got, d.sleeps, pauseStep)
	}
	for _, ms := range []int{0, -1, 300001} {
		if _, e := runWait(t, newWaitDesk(), protocol.WaitArgs{For: "ms", MS: ms}); e == nil {
			t.Errorf("a pause of %d was accepted", ms)
		}
	}
	// A Window ID alone reads as a pause: refused, not sat out.
	d = newWaitDesk()
	if _, e := runWait(t, d, protocol.WaitArgs{For: "ms", MS: 203}); e == nil || !strings.Contains(e.Msg, "203 is the Window ID") || len(d.sleeps) != 0 {
		t.Errorf("a Window ID's pause = %v after %v", e, d.sleeps)
	}
}

func TestDesktopWaitsStopOnEsc(t *testing.T) {
	for name, a := range map[string]protocol.WaitArgs{
		"pause":   {For: "ms", MS: 5000},
		"window":  {For: "window", Value: "Calculator"},
		"element": {Window: "101", For: "element", ElementIndex: intp(1), State: "enabled"},
	} {
		d := newWaitDesk()
		d.states = func(int) (uia.ElementState, error) { return uia.ElementState{}, nil }
		d.stopAt = 3
		_, e := runWait(t, d, a)
		if e == nil || e.Exit != protocol.ExitEsc {
			t.Errorf("%s: %v, want the Esc stop", name, e)
		}
		if len(d.sleeps) != 3 {
			t.Errorf("%s: slept %v, want it stopped at the third", name, d.sleeps)
		}
		if !strings.Contains(strings.Join(d.esc.log, " "), "arm-passive") {
			t.Errorf("%s: Esc scope %v, want a passive one", name, d.esc.log)
		}
	}
}

// TestDesktopWaitsSkipTheActionGate: a desktop wait only reads, so it runs
// while an Action holds the gate; a Page wait still queues behind it.
func TestDesktopWaitsSkipTheActionGate(t *testing.T) {
	spy := &actionSpy{point: uia.Point{X: 1, Y: 2}}
	svc := spy.services()
	inClick := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	svc.Click = func(uint64, int32, int32, win.MouseButton, int) error {
		once.Do(func() { close(inClick) })
		<-release
		return nil
	}
	cmds := CommandsWith(svc)
	// A Headed click (headless-max ticket 21) holds the gate through the v1
	// fakes' injected click.
	raw, _ := json.Marshal(protocol.ClickArgs{Window: "101", ElementIndex: intp(1), Headed: true})
	go cmds["click"](context.Background(), raw)
	<-inClick
	defer close(release)

	if out, e := runCommand(t, cmds, "wait", protocol.WaitArgs{For: "window", Value: "Notepad"}); e != nil || !strings.HasPrefix(out, "101\t") {
		t.Errorf("wait window behind a running Action = %q, %v", out, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page, _ := json.Marshal(protocol.WaitArgs{Window: "101", For: "selector", Value: "#a"})
	if _, e := cmds["wait"](ctx, page); e == nil || e.Msg != queuedOutError().Msg {
		t.Errorf("a queued Page wait = %v, want the queued-past-budget failure", e)
	}
}

func TestDesktopWaitIsABatchStep(t *testing.T) {
	d := newWaitDesk()
	d.states = func(n int) (uia.ElementState, error) { return uia.ElementState{Enabled: n > 1}, nil }
	step, _ := json.Marshal(protocol.WaitArgs{For: "element", ElementIndex: intp(3), State: "enabled"})
	out, e := runCommand(t, CommandsWith(d.services()), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{{Kind: "wait", Args: step}}})
	if e != nil || out != "Action 1 (wait): "+okOutput || d.held[0] != 3 {
		t.Fatalf("batch wait = %q, %v (held %v)", out, e, d.held)
	}
}

// TestAWaitEndsAtItsDeadlineWhenAPollBlocks: a poll that never answers (a
// Window list read stuck on a window, a UIA read stuck in a provider) does
// not hold the wait past its timeout.
func TestAWaitEndsAtItsDeadlineWhenAPollBlocks(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	for name, a := range map[string]protocol.WaitArgs{
		"window":  {For: "window", Value: "Calculator", TimeoutMS: 300},
		"text":    {Window: "101", For: "text", Value: "Done", TimeoutMS: 300},
		"element": {Window: "101", For: "element", ElementIndex: intp(1), State: "enabled", TimeoutMS: 300},
	} {
		d := newWaitDesk()
		d.windows = func(n int) []win.WindowInfo {
			if name == "window" {
				<-block
			}
			return fakeWindows()
		}
		d.trees = func(int) *wstate.Tree { <-block; return cannedTree() }
		d.states = func(int) (uia.ElementState, error) { <-block; return uia.ElementState{}, nil }
		start := time.Now()
		_, e := runWait(t, d, a)
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("%s: a blocked poll held the 300 ms wait for %s", name, took)
		}
		if e == nil || !strings.Contains(e.Msg, "the condition did not hold within") {
			t.Errorf("%s: %v, want the timeout", name, e)
		}
	}
}
