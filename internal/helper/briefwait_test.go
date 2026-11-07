package helper

// The brief wait's offline tests (reliability-council 06): a disabled
// Element that enables within the wait, one that stays disabled (the old
// refusal, after the wait), a modal dialog that closes (Headless and
// Headed), a name: reference that appears, the lanes free while the poll
// runs, --no-wait's immediate refusal, the Esc stop, a Batch step that
// does not wait, and a write whose outcome is uncertain never tried
// again. The clock moves only when the wait sleeps.

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

// briefSpy is a headlessSpy whose clicks answer in turn (clickErrs), whose
// Element reads enabled from the enabledAt'th poll on, and whose tree
// shows the Element named Next from the appearAt'th read on (the
// reference then resolves to index 7). Every poll records whether window
// 101's lanes were free.
type briefSpy struct {
	*headlessSpy

	bmu       sync.Mutex
	clickErrs []error // the nth click's outcome; past the list, ok
	clicks    int
	enabledAt int // 0: never enables
	appearAt  int // 0: never appears
	polls     int
	freeLanes []bool
	slept     time.Duration
	clock     time.Time
	onPoll    func(n int) // called at each poll, the lanes free or not
}

func newBriefSpy(clickErrs ...error) *briefSpy {
	s := &briefSpy{headlessSpy: newHeadlessSpy(), clickErrs: clickErrs, clock: time.Unix(1_000_000, 0)}
	s.svc.Actions = s
	s.svc.Trees = s
	s.svc.lanes = newLaneManager()
	s.svc.Sleep = func(d time.Duration) {
		s.bmu.Lock()
		s.slept += d
		s.clock = s.clock.Add(d)
		s.bmu.Unlock()
	}
	s.svc.Now = func() time.Time {
		s.bmu.Lock()
		defer s.bmu.Unlock()
		return s.clock
	}
	return s
}

// lanesFree reports whether window 101's lane and the desktop lane can be
// taken now; they are given back at once.
func lanesFree(m *laneManager) bool {
	for _, key := range []string{windowLane(101), desktopLane} {
		lane := m.lane(key)
		select {
		case lane <- struct{}{}:
			<-lane
		default:
			return false
		}
	}
	return true
}

// poll records one poll and returns its number.
func (s *briefSpy) poll() int {
	free := lanesFree(s.svc.lanes)
	s.bmu.Lock()
	s.polls++
	n := s.polls
	s.freeLanes = append(s.freeLanes, free)
	on := s.onPoll
	s.bmu.Unlock()
	if on != nil {
		on(n)
	}
	return n
}

// HeadlessClick implements HeadlessActor: the nth click's outcome.
func (s *briefSpy) HeadlessClick(ctx context.Context, hwnd uint64, index int) (string, error) {
	if lanesFree(s.svc.lanes) {
		return "", errors.New("the click ran without its lane")
	}
	s.headlessSpy.HeadlessClick(ctx, hwnd, index)
	s.bmu.Lock()
	defer s.bmu.Unlock()
	s.clicks++
	if s.clicks <= len(s.clickErrs) {
		return "Invoke", s.clickErrs[s.clicks-1]
	}
	return "Invoke", nil
}

// HoldElement implements ElementHolder.
func (s *briefSpy) HoldElement(ctx context.Context, hwnd uint64, index int) (uia.Held, error) {
	return briefHeld{s}, nil
}

type briefHeld struct{ s *briefSpy }

func (h briefHeld) State(ctx context.Context) (uia.ElementState, error) {
	n := h.s.poll()
	return uia.ElementState{Enabled: h.s.enabledAt > 0 && n >= h.s.enabledAt}, nil
}

func (h briefHeld) Release() {}

// Capture implements TreeReader (no read in these tests captures).
func (s *briefSpy) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	return &wstate.Tree{FocusedIndex: -1}, nil
}

// PeekTree implements TreePeeker: Next shows from the appearAt'th read,
// and from then on the reference resolves to index 7.
func (s *briefSpy) PeekTree(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	n := s.poll()
	tree := &wstate.Tree{FocusedIndex: -1, Elements: []wstate.Element{{Role: "button", Name: "Back"}}}
	if s.appearAt > 0 && n >= s.appearAt {
		tree.Elements = append(tree.Elements, wstate.Element{Role: "button", Name: "Next"})
		s.mu.Lock()
		s.refIndex, s.refErr = 7, nil
		s.mu.Unlock()
	}
	return tree, nil
}

func (s *briefSpy) counts() (clicks, polls int, slept time.Duration, free []bool) {
	s.bmu.Lock()
	defer s.bmu.Unlock()
	return s.clicks, s.polls, s.slept, append([]bool(nil), s.freeLanes...)
}

// assertLanesFree fails when a poll ran while a lane was held.
func assertLanesFree(t *testing.T, free []bool) {
	t.Helper()
	if len(free) == 0 {
		t.Fatal("nothing polled")
	}
	for i, f := range free {
		if !f {
			t.Errorf("poll %d ran while window 101's lane or the desktop lane was held", i+1)
		}
	}
}

func TestBriefWaitEnablesWithinTheWait(t *testing.T) {
	spy := newBriefSpy(uia.ErrDisabled)
	spy.enabledAt = 4
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr != nil {
		t.Fatalf("click: %v (hint %q)", cmdErr, cmdErr.Hint)
	}
	if want := "ok\nNote: waited 0.3 s for element 2 to enable\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	clicks, polls, _, free := spy.counts()
	if clicks != 2 || polls != 4 {
		t.Errorf("clicks %d, polls %d: want the click again once the Element read enabled (2, 4)", clicks, polls)
	}
	assertLanesFree(t, free)
}

func TestBriefWaitStaysDisabled(t *testing.T) {
	spy := newBriefSpy(uia.ErrDisabled)
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Msg != "the Element is disabled; nothing was sent" {
		t.Fatalf("click = %v, want the disabled refusal", cmdErr)
	}
	for _, want := range []string{"a disabled Element cannot be acted on", "winuse waited 1.8 s for element 2 to enable", "winuse wait 101 2 enabled"} {
		if !strings.Contains(cmdErr.Hint, want) {
			t.Errorf("hint = %q, want %q", cmdErr.Hint, want)
		}
	}
	if !SentNothing(cmdErr) {
		t.Errorf("delivery %q: the refusal still sent nothing", Delivery(cmdErr))
	}
	clicks, _, slept, free := spy.counts()
	if clicks != 1 || slept != briefWaitLimit {
		t.Errorf("clicks %d after %v: want one click and the whole wait", clicks, slept)
	}
	assertLanesFree(t, free)
}

func TestBriefWaitForAModalDialogToClose(t *testing.T) {
	spy := newBriefSpy()
	readies := 0
	spy.svc.Ready = func(hwnd uint64) error {
		spy.step("ready")
		spy.bmu.Lock()
		readies++
		n := readies
		spy.bmu.Unlock()
		if n > 1 {
			spy.poll() // the click's own check is the first
		}
		if n <= 3 {
			return &win.ModalDialogError{WindowID: 101, DialogID: 202}
		}
		return nil
	}
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr != nil {
		t.Fatalf("click: %v", cmdErr)
	}
	if want := "ok\nNote: waited 0.2 s for the modal dialog over window 101 to close\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	clicks, _, _, free := spy.counts()
	if clicks != 1 {
		t.Errorf("clicks = %d, want one, once the dialog closed", clicks)
	}
	assertLanesFree(t, free[:len(free)-1]) // the last is the click's own check, on its lane
}

func TestBriefWaitHeadedForAModalDialog(t *testing.T) {
	// A Headed Action's activation refused by the dialog sent nothing, and
	// the poll holds neither the desktop lane nor the Window's.
	spy := &actionSpy{point: uia.Point{X: 100, Y: 200}}
	svc := spy.services()
	svc.lanes = newLaneManager()
	svc.Sleep = func(time.Duration) {}
	activations := 0
	svc.Activate = func(hwnd uint64) error {
		spy.step("activate")
		activations++
		if activations == 1 {
			return &win.ModalDialogError{WindowID: 101, DialogID: 202}
		}
		return nil
	}
	var free []bool
	probes := 0
	svc.Ready = func(hwnd uint64) error {
		free = append(free, lanesFree(svc.lanes))
		probes++
		if probes < 3 {
			return &win.ModalDialogError{WindowID: 101, DialogID: 202}
		}
		return nil
	}
	out, cmdErr := runCommand(t, CommandsWith(svc), "click", headedArgs(clickArgs("101", 2, 1, "left")))
	if cmdErr != nil {
		t.Fatalf("click: %v", cmdErr)
	}
	if !strings.Contains(out, "Note: waited ") || !strings.Contains(out, "for the modal dialog over window 101 to close") {
		t.Errorf("output = %q, want the wait's note", out)
	}
	if activations != 2 || !containsStep(spy.recorded(), "click") {
		t.Errorf("activations %d, steps %v: want the click after the second activation", activations, spy.recorded())
	}
	assertLanesFree(t, free)
}

func TestBriefWaitForANameToAppear(t *testing.T) {
	spy := newBriefSpy()
	spy.refErr = &wstate.NoMatchRefError{Kind: wstate.RefName, Text: "Next"}
	spy.appearAt = 3
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Next"})
	if cmdErr != nil {
		t.Fatalf("click: %v", cmdErr)
	}
	if want := "ok\nNote: waited 0.5 s for name:Next to appear in window 101\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	clicks, _, _, free := spy.counts()
	if clicks != 1 || spy.hIndex != 7 {
		t.Errorf("clicks %d on element %d: want one click on the Element that appeared (7)", clicks, spy.hIndex)
	}
	assertLanesFree(t, free)

	// One that never appears is the old refusal, after the wait.
	spy = newBriefSpy()
	spy.refErr = &wstate.NoMatchRefError{Kind: wstate.RefName, Text: "Next"}
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Next"})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "Next") || !SentNothing(cmdErr) {
		t.Fatalf("click = %v, want the reference's refusal", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "winuse waited 1.8 s for name:Next to appear in window 101") {
		t.Errorf("hint = %q, want the wait", cmdErr.Hint)
	}
}

func TestBriefWaitNoWaitFailsAtOnce(t *testing.T) {
	spy := newBriefSpy(uia.ErrDisabled)
	spy.enabledAt = 1
	_, cmdErr := runCommandNoWait(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Msg != "the Element is disabled; nothing was sent" {
		t.Fatalf("click = %v, want the disabled refusal", cmdErr)
	}
	if strings.Contains(cmdErr.Hint, "waited") {
		t.Errorf("hint = %q: --no-wait waits for nothing", cmdErr.Hint)
	}
	if clicks, polls, slept, _ := spy.counts(); clicks != 1 || polls != 0 || slept != 0 {
		t.Errorf("clicks %d, polls %d, slept %v: want the old immediate refusal", clicks, polls, slept)
	}
}

func TestBriefWaitNeverRepeatsAnUncertainWrite(t *testing.T) {
	// A write that may have taken effect is not a refusal: no wait, no
	// second try.
	spy := newBriefSpy(&uia.WriteError{What: "Invoke", HR: -0x7FFEFFF2})
	spy.enabledAt = 1
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Delivery != protocol.DeliveryUncertain {
		t.Fatalf("click = %+v, want the uncertain write", cmdErr)
	}
	if clicks, polls, _, _ := spy.counts(); clicks != 1 || polls != 0 {
		t.Errorf("clicks %d, polls %d: an uncertain write is never tried again", clicks, polls)
	}

	// The try after the wait fails uncertain: it is reported, not tried a
	// third time.
	spy = newBriefSpy(uia.ErrDisabled, &uia.WriteError{What: "Invoke", HR: -0x7FFEFFF2})
	spy.enabledAt = 2
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Delivery != protocol.DeliveryUncertain || !strings.Contains(cmdErr.Msg, "Invoke failed") {
		t.Fatalf("click = %+v, want the second try's uncertain write", cmdErr)
	}
	if clicks, _, _, _ := spy.counts(); clicks != 2 {
		t.Errorf("clicks = %d, want 2", clicks)
	}

	// A refusal marked to wait whose delivery is not "nothing was sent"
	// never waits.
	e := waitsFor(mayHaveActed(Fail("the Element is disabled", "")), refusalWait{kind: waitEnable, index: 2})
	if briefRefusal(e) != nil {
		t.Error("an uncertain failure must not wait")
	}
}

func TestBriefWaitStopsAtEsc(t *testing.T) {
	spy := newBriefSpy(uia.ErrDisabled)
	spy.onPoll = func(n int) {
		if n == 2 {
			spy.esc.mu.Lock()
			spy.esc.pressed = true
			spy.esc.mu.Unlock()
		}
	}
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc || !SentNothing(cmdErr) {
		t.Fatalf("click = %+v, want the Esc stop with nothing sent", cmdErr)
	}
	if clicks, polls, _, _ := spy.counts(); clicks != 1 || polls != 2 {
		t.Errorf("clicks %d, polls %d: the poll ends at the press", clicks, polls)
	}
}

func TestBriefWaitRespectsTheBudget(t *testing.T) {
	// Half the request's remaining budget bounds the wait.
	spy := newBriefSpy(uia.ErrDisabled)
	cmds := CommandsWith(spy.svc)
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	_, cmdErr := cmds["click"](ctx, clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || !strings.HasPrefix(cmdErr.Msg, "the Element is disabled") {
		t.Fatalf("click = %v, want the disabled refusal", cmdErr)
	}
	if _, _, slept, _ := spy.counts(); slept > 300*time.Millisecond {
		t.Errorf("slept %v in a 600 ms request, want at most half of it", slept)
	}
}

func TestBriefWaitNotInABatchStep(t *testing.T) {
	// A Batch holds its lanes from its first step to its last: its steps
	// fail at once.
	spy := newBriefSpy(uia.ErrDisabled)
	spy.enabledAt = 1
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps:  []protocol.Step{stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)})},
	})
	if cmdErr == nil {
		t.Fatal("batch succeeded, want the step's refusal")
	}
	if clicks, polls, _, _ := spy.counts(); clicks != 1 || polls != 0 {
		t.Errorf("clicks %d, polls %d: a Batch step does not wait", clicks, polls)
	}
}

func TestNoWaitTravelsOnTheRequest(t *testing.T) {
	var got []bool
	commands := map[string]Command{
		"probe": func(ctx context.Context, _ json.RawMessage) (string, *Error) {
			got = append(got, noWaitOf(ctx))
			return okOutput, nil
		},
	}
	s := New("0.1.0", time.Hour, time.Second, commands, discardLogger())
	req := testRequest("probe")
	s.handle(req)
	req.NoWait = true
	s.handle(req)
	if len(got) != 2 || got[0] || !got[1] {
		t.Errorf("--no-wait on the context = %v, want [false true]", got)
	}
}
