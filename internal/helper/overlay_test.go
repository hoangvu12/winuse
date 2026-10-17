package helper

// The in-control indicator's offline tests (ticket 13): the show/hide
// bracket around every Action (raised over the target Window after the
// refusals and before the activation; hidden after the action, whatever
// its outcome), and the capture bracket (a capture method runs with the
// indicator hidden, which is restored afterwards). The overlay is faked;
// no desktop is touched.

import (
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// overlaySpy fakes Overlay: it records every bracket step, tracks its own
// visibility (so a fake capture method can check it was hidden), and can
// share its steps with an actionSpy's recorder so tests can assert the
// interleaving with the Action pipeline's steps.
type overlaySpy struct {
	mu      sync.Mutex
	steps   []string
	share   func(string) // optional second recorder (an actionSpy's step)
	visible bool
	showErr error
	hideErr error
	rects   []win.Rect
}

func (s *overlaySpy) record(step string) {
	s.mu.Lock()
	s.steps = append(s.steps, step)
	s.mu.Unlock()
	if s.share != nil {
		s.share(step)
	}
}

func (s *overlaySpy) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

func (s *overlaySpy) shownRects() []win.Rect {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]win.Rect(nil), s.rects...)
}

// Show implements Overlay.
func (s *overlaySpy) Show(r win.Rect) error {
	s.record(fmt.Sprintf("overlay-show(%d,%d %dx%d)", r.Left, r.Top, r.Width(), r.Height()))
	s.mu.Lock()
	s.visible = true
	s.rects = append(s.rects, r)
	err := s.showErr
	s.mu.Unlock()
	return err
}

// Hide implements Overlay.
func (s *overlaySpy) Hide() error {
	s.record("overlay-hide")
	s.mu.Lock()
	s.visible = false
	err := s.hideErr
	s.mu.Unlock()
	return err
}

// HideWhile implements Overlay: f runs hidden, then the previous
// visibility is restored.
func (s *overlaySpy) HideWhile(f func()) {
	s.record("overlay-capture-hide")
	s.mu.Lock()
	was := s.visible
	s.visible = false
	s.mu.Unlock()
	defer func() {
		s.record("overlay-capture-restore")
		s.mu.Lock()
		s.visible = was
		s.mu.Unlock()
	}()
	f()
}

func (s *overlaySpy) isVisible() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.visible
}

// captureSpyMethod fakes one capture method: it records whether the
// overlay was hidden while it captured, and what it returned.
type captureSpyMethod struct {
	overlay    *overlaySpy
	frame      shot.Frame
	err        error
	hidAtTake  bool
	hwnd       uint64
	captureErr error
}

// Name implements shot.Method.
func (m *captureSpyMethod) Name() string { return "spy capture" }

// CaptureWindow implements shot.Method.
func (m *captureSpyMethod) CaptureWindow(hwnd uint64) (shot.Frame, error) {
	m.hwnd = hwnd
	m.hidAtTake = m.overlay != nil && !m.overlay.isVisible()
	if m.captureErr != nil {
		return shot.Frame{}, m.captureErr
	}
	return m.frame, m.err
}

func TestActionShowsOverlayOverTargetWindowAndHidesAfter(t *testing.T) {
	spy := &actionSpy{point: uia.Point{X: 100, Y: 200}}
	ov := &overlaySpy{}
	ov.share = spy.step // one interleaved step list
	svc := spy.services()
	svc.Overlay = ov

	out, cmdErr := runCommand(t, CommandsWith(svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr != nil {
		t.Fatalf("click: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	// The pipeline order with the indicator bracket: checks, indicator
	// up, activation, element resolution, input, indicator down.
	want := []string{
		"locked", "integrity",
		"overlay-show(10,20 800x470)",
		"activate", "point", "click",
		"overlay-hide",
	}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
	// The indicator was raised exactly over the resolved Window's bounds.
	rects := ov.shownRects()
	if len(rects) != 1 || rects[0] != (win.Rect{Left: 10, Top: 20, Right: 810, Bottom: 490}) {
		t.Errorf("overlay rects = %v, want the target Window's bounds once", rects)
	}
	if ov.isVisible() {
		t.Error("the indicator must be hidden after the Action")
	}
}

func TestOverlayHiddenAfterActionFailure(t *testing.T) {
	// Whatever the Action's outcome, the indicator comes down again.
	for _, tc := range []struct {
		name string
		spy  *actionSpy
	}{
		{"activation failure", &actionSpy{activateErr: &win.NotForegroundError{WindowID: 101}}},
		{"input failure", &actionSpy{point: uia.Point{X: 5, Y: 5}, clickErr: errors.New("the pointer did not reach (5, 5)")}},
		{"stale element", &actionSpy{pointErr: fmt.Errorf("%w: element 1 is gone", uia.ErrStale)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ov := &overlaySpy{}
			svc := tc.spy.services()
			svc.Overlay = ov
			_, _ = runCommand(t, CommandsWith(svc), "click", clickArgs("101", 1, 1, "left"))
			if got := ov.recorded(); !hasStepPrefix(got, "overlay-show") || !containsStep(got, "overlay-hide") {
				t.Errorf("bracket steps = %v, want a show and a hide around the failed Action", got)
			}
			if ov.isVisible() {
				t.Error("the indicator must be hidden after a failed Action")
			}
		})
	}
}

// hasStepPrefix reports whether steps contains one starting with prefix
// (the overlay-show step carries its rectangle).
func hasStepPrefix(steps []string, prefix string) bool {
	for _, s := range steps {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func TestRefusalsNeverShowOverlay(t *testing.T) {
	// The pre-input refusals happen before winuse takes control: no
	// indicator may be raised for them.
	for _, tc := range []struct {
		name string
		spy  *actionSpy
	}{
		{"locked desktop", &actionSpy{locked: true}},
		{"administrator window", &actionSpy{refuse: &win.AdminWindowError{WindowID: 101, App: "Notepad.exe"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ov := &overlaySpy{}
			svc := tc.spy.services()
			svc.Overlay = ov
			_, _ = runCommand(t, CommandsWith(svc), "click", clickArgs("101", 1, 1, "left"))
			if got := ov.recorded(); len(got) != 0 {
				t.Errorf("refused Action steps = %v, want no indicator activity", got)
			}
		})
	}
	// A Window that does not resolve (before the pipeline) shows nothing.
	ov := &overlaySpy{}
	svc := (&actionSpy{}).services()
	svc.Overlay = ov
	_, _ = runCommand(t, CommandsWith(svc), "click", clickArgs("Paint", 1, 1, "left"))
	if got := ov.recorded(); len(got) != 0 {
		t.Errorf("unresolved window steps = %v, want no indicator activity", got)
	}
}

func TestOverlayFailureNeverFailsAction(t *testing.T) {
	// The indicator is best effort: a failed Show or Hide leaves the
	// Action itself untouched.
	spy := &actionSpy{point: uia.Point{X: 100, Y: 200}}
	ov := &overlaySpy{showErr: errors.New("wedge"), hideErr: errors.New("wedge")}
	svc := spy.services()
	svc.Overlay = ov
	out, cmdErr := runCommand(t, CommandsWith(svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr != nil {
		t.Fatalf("click with a failing overlay: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
}

func TestCaptureMethodRunsHiddenAndRestores(t *testing.T) {
	frame := shot.Frame{
		Rect:   image.Rect(0, 0, 100, 50), // the wrapper passes the frame through
		Anchor: image.Point{X: 0, Y: 0},
	}
	// A shown indicator is hidden for the capture and restored after.
	ov := &overlaySpy{visible: true}
	method := &captureSpyMethod{overlay: ov, frame: frame}
	wrapped := hiddenDuringCapture{overlay: ov, method: method}

	got, err := wrapped.CaptureWindow(101)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got.Rect != frame.Rect {
		t.Errorf("frame rect = %v, want the method's frame passed through", got.Rect)
	}
	if !method.hidAtTake {
		t.Error("the capture method ran while the indicator was shown")
	}
	if method.hwnd != 101 {
		t.Errorf("captured window %d, want 101", method.hwnd)
	}
	if !ov.isVisible() {
		t.Error("the indicator must be restored after the capture")
	}
	if got := ov.recorded(); !equalStrings(got, []string{"overlay-capture-hide", "overlay-capture-restore"}) {
		t.Errorf("bracket steps = %v, want exactly the capture bracket", got)
	}
}

func TestCaptureBracketNoOpWhenNotShown(t *testing.T) {
	// Between Actions the indicator is not shown: the bracket still runs
	// (a uniform path) but restores nothing visible.
	ov := &overlaySpy{}
	method := &captureSpyMethod{overlay: ov}
	wrapped := hiddenDuringCapture{overlay: ov, method: method}
	if _, err := wrapped.CaptureWindow(101); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if ov.isVisible() {
		t.Error("a capture between Actions must not show the indicator")
	}
}

func TestCaptureBracketPassesFailureThrough(t *testing.T) {
	ov := &overlaySpy{visible: true}
	method := &captureSpyMethod{overlay: ov, captureErr: errors.New("BitBlt failed")}
	wrapped := hiddenDuringCapture{overlay: ov, method: method}
	if _, err := wrapped.CaptureWindow(101); err == nil || !strings.Contains(err.Error(), "BitBlt") {
		t.Fatalf("error = %v, want the method's failure passed through", err)
	}
	if !ov.isVisible() {
		t.Error("the indicator must be restored even after a failed capture")
	}
}

func TestCaptureMethodsWithoutOverlayArePlain(t *testing.T) {
	// With no indicator wired (offline tests, a failed overlay start),
	// the chain's methods stay undecorated.
	methods := captureMethods(nil)
	if len(methods) != 2 {
		t.Fatalf("methods = %d, want the two GDI fallbacks", len(methods))
	}
	if _, wrapped := methods[0].(hiddenDuringCapture); wrapped {
		t.Error("without an overlay the methods must not be wrapped")
	}
	if _, wrapped := methods[1].(hiddenDuringCapture); wrapped {
		t.Error("without an overlay the methods must not be wrapped")
	}
	// With one wired, every method is bracketed.
	methods = captureMethods(&overlaySpy{})
	for i, m := range methods {
		if _, wrapped := m.(hiddenDuringCapture); !wrapped {
			t.Errorf("method %d is not wrapped in the capture bracket", i)
		}
	}
}

func TestCommandsRegisteredWithoutOverlayByDefault(t *testing.T) {
	// CommandsWith (and Commands, used by the offline command tests)
	// defaults to no indicator: building a registry never starts a UI
	// thread and never creates a window.
	cmds := CommandsWith(testServices(&fakeTrees{}))
	if cmds["click"] == nil {
		t.Error("click missing from the command registry")
	}
}

// TestBatchShowsOneOverlayBracketForTheWholeRun pins finding 12: a Batch
// raises the indicator once over its Window for the whole run, and the
// steps' own brackets ride inside it — the real hide lands only when the
// Batch ends, instead of blinking between every Action.
func TestBatchShowsOneOverlayBracketForTheWholeRun(t *testing.T) {
	spy := newBatchSpy()
	spy.point = uia.Point{X: 100, Y: 200}
	ov := &overlaySpy{}
	ov.share = spy.step // one interleaved step list with the pipeline
	spy.svc.Overlay = ov
	cmds := CommandsWith(spy.svc)

	out, cmdErr := runCommand(t, cmds, "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(1)}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello"}),
			stepOf("activate-window", nil),
		},
	})
	if cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}

	// Exactly one real show over the Batch's Window and one real hide,
	// after the last step — the steps' brackets did not blink it.
	shows := 0
	hides := 0
	var steps []string
	for _, st := range spy.recorded() {
		steps = append(steps, st)
		if strings.HasPrefix(st, "overlay-show(") {
			shows++
		}
		if st == "overlay-hide" {
			hides++
		}
	}
	if shows != 1 || hides != 1 {
		t.Errorf("the indicator was shown %d times and hidden %d times, want 1 and 1 (one bracket for the whole Batch): %v", shows, hides, steps)
	}
	if !strings.HasPrefix(steps[len(steps)-1], "overlay-hide") {
		t.Errorf("the indicator must hide after the last step, got %v", steps)
	}
	// The bracket opened before the first step's own bracket: the show
	// precedes the first step's locked check.
	for i, st := range steps {
		if strings.HasPrefix(st, "overlay-show(") {
			if i > 0 && steps[i-1] != "locked" && steps[i-1] != "resolve" {
				// The batch resolves the Window, then brackets, then the
				// first step's pipeline starts.
				t.Errorf("the Batch's overlay bracket opened at %d (%v), before the first step's pipeline", i, steps)
			}
			break
		}
	}

	// A standalone Action still brackets exactly once (the nesting does
	// not leak: the count returns to zero).
	spy2 := &actionSpy{point: uia.Point{X: 1, Y: 2}}
	ov2 := &overlaySpy{}
	ov2.share = spy2.step
	svc := spy2.services()
	svc.Overlay = ov2
	cmds2 := CommandsWith(svc)
	if _, cmdErr := runCommand(t, cmds2, "click", clickArgs("101", 2, 1, "left")); cmdErr != nil {
		t.Fatalf("standalone click: %v", cmdErr.Error())
	}
	if got := ov2.recorded(); len(got) != 2 || !strings.HasPrefix(got[0], "overlay-show(") || got[1] != "overlay-hide" {
		t.Errorf("a standalone Action's overlay steps = %v, want one show and one hide", got)
	}
}
