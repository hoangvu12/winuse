package helper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// observeTrees is a TreeReader whose store holds the latest tree it
// returned (or the baseline it starts with), as the uia Client's does.
type observeTrees struct {
	mu     sync.Mutex
	stored []wstate.Element // nil: no Window State
	next   *wstate.Tree
	err    error
	reads  int
}

func (o *observeTrees) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reads++
	if o.err != nil {
		return nil, o.err
	}
	o.stored = o.next.Elements
	return o.next, nil
}

func (o *observeTrees) StoredElements(ctx context.Context, hwnd uint64) ([]wstate.Element, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]wstate.Element(nil), o.stored...), o.stored != nil
}

// observeFixture is the recorded fixture App before and after an Action
// that ticked "Notify me" and showed a status line.
func observeFixture(t *testing.T) (before, after *wstate.Tree) {
	t.Helper()
	before, err := wstate.LoadFixture(filepath.Join("..", "wstate", "testdata", "fixture-app.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := *before
	after = &c
	after.Elements = append([]wstate.Element(nil), before.Elements...)
	after.Elements[13].Toggle = "on"
	after.Elements = append(after.Elements, wstate.Element{Depth: 1, Role: "text", Name: "Saved"})
	return before, after
}

func observedCmd(t *testing.T, trees *observeTrees, name string, cmd Command, o *protocol.Observe) (string, *Error) {
	t.Helper()
	s := testServices(trees)
	ctx, cancel := context.WithTimeout(context.Background(), protocol.RequestTimeout+protocol.ObserveTimeout)
	defer cancel()
	ctx = withObserve(ctx, o)
	return s.observed(name, cmd)(ctx, json.RawMessage(`{"window":"101"}`))
}

func TestObserveDiffAfterAnAction(t *testing.T) {
	before, after := observeFixture(t)
	trees := &observeTrees{stored: before.Elements, next: after}
	var actLeft time.Duration
	act := func(ctx context.Context, args json.RawMessage) (string, *Error) {
		if observeOf(ctx) != nil {
			t.Error("the Action must not see --observe")
		}
		dl, _ := ctx.Deadline()
		actLeft = time.Until(dl)
		return "ok\n", nil
	}
	out, err := observedCmd(t, trees, "click", act, &protocol.Observe{Mode: protocol.ObserveDiff})
	if err != nil {
		t.Fatal(err)
	}
	want := `ok
Window: "winuse fixture app", App: winusefixture.exe.
Changed since this agent's last read of window 101 (~ changed, * new, - gone); Element Indexes not listed are unchanged:
		~13 check box Notify me on Secondary Actions: Toggle ID: checkbox-notify (was: check box Notify me off Secondary Actions: Toggle ID: checkbox-notify)
		*26 text Saved
`
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if actLeft > protocol.RequestTimeout || actLeft < protocol.RequestTimeout-time.Second {
		t.Errorf("the Action ran with %s left; it keeps its own budget (%s)", actLeft, protocol.RequestTimeout)
	}
}

func TestObserveFullAndViews(t *testing.T) {
	before, after := observeFixture(t)
	trees := &observeTrees{stored: before.Elements, next: after}
	ok := func(context.Context, json.RawMessage) (string, *Error) { return "ok\n", nil }
	out, err := observedCmd(t, trees, "click", ok, &protocol.Observe{Mode: protocol.ObserveFull, Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := "ok\n" + wstate.RenderView(after, wstate.View{Interactive: true, Ref: "101"}); out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}

	file := filepath.Join(t.TempDir(), "observed.txt")
	trees = &observeTrees{stored: before.Elements, next: after}
	out, err = observedCmd(t, trees, "click", ok, &protocol.Observe{Mode: protocol.ObserveDiff, Out: file})
	if err != nil {
		t.Fatal(err)
	}
	data, rerr := os.ReadFile(file)
	if rerr != nil || !strings.Contains(string(data), "*26 text Saved") {
		t.Fatalf("the file holds the diff: %q (%v)", data, rerr)
	}
	if !strings.Contains(out, "Observed Window State: "+file+" (27 Elements") {
		t.Errorf("the output names the file:\n%s", out)
	}
}

func TestObserveNoBaselinePrintsWhole(t *testing.T) {
	_, after := observeFixture(t)
	trees := &observeTrees{next: after}
	ok := func(context.Context, json.RawMessage) (string, *Error) { return "ok\n", nil }
	out, _ := observedCmd(t, trees, "press-key", ok, &protocol.Observe{Mode: protocol.ObserveDiff})
	if !strings.Contains(out, "\t\t26 text Saved\n") || !strings.Contains(out, "had no earlier read of window 101") {
		t.Errorf("got:\n%s", out)
	}
}

func TestObserveFailuresKeepTheAction(t *testing.T) {
	before, after := observeFixture(t)
	ok := func(context.Context, json.RawMessage) (string, *Error) { return "ok\n", nil }

	// A read that fails is a note after the Action's own output.
	trees := &observeTrees{stored: before.Elements, next: after, err: uia.ErrWindowGone}
	out, err := observedCmd(t, trees, "close-window", ok, &protocol.Observe{Mode: protocol.ObserveDiff})
	if err != nil || !strings.HasPrefix(out, "ok\nNote: --observe could not read 101 after the Action, which ran as reported above: ") {
		t.Errorf("got %q, %v", out, err)
	}

	// A failed Action prints no observation and reads nothing.
	trees = &observeTrees{stored: before.Elements, next: after}
	failed := func(context.Context, json.RawMessage) (string, *Error) {
		return "", Fail("element 7 is not available", "read again")
	}
	if _, err := observedCmd(t, trees, "click", failed, &protocol.Observe{Mode: protocol.ObserveDiff}); err == nil || err.Out != "" || trees.reads != 0 {
		t.Errorf("a failed Action: %v, %d reads", err, trees.reads)
	}

	// A Batch that ran past a failed step observes under its lines.
	batchFailed := func(context.Context, json.RawMessage) (string, *Error) {
		return "", &Error{Msg: "batch: 1 of 2 Actions failed", Exit: 1, Out: "Action 1 (click): ok\nAction 2 (click): failed: x\n"}
	}
	_, err = observedCmd(t, trees, "batch", batchFailed, &protocol.Observe{Mode: protocol.ObserveDiff})
	if err == nil || !strings.Contains(err.Out, "Action 2 (click): failed: x\nWindow: \"winuse fixture app\"") || !strings.Contains(err.Out, "*26 text Saved") {
		t.Errorf("a failed Batch observes after its lines: %+v", err)
	}
}

func TestObserveRefusals(t *testing.T) {
	trees := &observeTrees{}
	ran := false
	act := func(context.Context, json.RawMessage) (string, *Error) { ran = true; return "ok\n", nil }
	cases := []struct {
		name string
		o    protocol.Observe
		want string
	}{
		{"list-windows", protocol.Observe{Mode: "diff"}, "list-windows does not take --observe"},
		{"click", protocol.Observe{Mode: "changes"}, "--observe=changes is not a mode"},
		{"click", protocol.Observe{Mode: "diff", Out: `C:\x`, MaxChars: intPtrBatch(10)}, "pass --out or --max-chars"},
		{"click", protocol.Observe{Mode: "full", Full: true, Compact: true}, "--observe: --full prints the Window State as Codex does"},
	}
	for _, c := range cases {
		o := c.o
		_, err := observedCmd(t, trees, c.name, act, &o)
		if err == nil || !strings.Contains(err.Msg, c.want) || !strings.Contains(err.Msg, "nothing was sent") || err.Code != protocol.CodeUsage {
			t.Errorf("%s %+v: got %+v, want %q", c.name, c.o, err, c.want)
		}
	}
	if ran {
		t.Error("a refused --observe must not run the command")
	}
}

// The server hands a request's --observe to the registry, and its budget
// grows by the read's.
func TestServerPassesObserve(t *testing.T) {
	var got *protocol.Observe
	var left time.Duration
	srv := New("v1", time.Minute, protocol.RequestTimeout, map[string]Command{
		"click": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			got = observeOf(ctx)
			dl, _ := ctx.Deadline()
			left = time.Until(dl)
			return "ok\n", nil
		},
	}, nil)
	resp := srv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: "click", Observe: &protocol.Observe{Mode: "full"}})
	if !resp.OK || got == nil || got.Mode != "full" {
		t.Fatalf("resp %+v, observe %+v", resp, got)
	}
	if left < protocol.RequestTimeout+protocol.ObserveTimeout-time.Second {
		t.Errorf("budget left %s, want about %s", left, protocol.RequestTimeout+protocol.ObserveTimeout)
	}
}

// A Batch reads as a step: get-window-state and find print under their
// numbers, between Actions.
func TestBatchReadSteps(t *testing.T) {
	spy := newBatchSpy()
	spy.point = uia.Point{X: 100, Y: 200}
	before, after := observeFixture(t)
	trees := &observeTrees{stored: before.Elements, next: after}
	spy.svc.Trees = trees
	cmds := CommandsWith(spy.svc)
	raw, _ := json.Marshal(protocol.BatchArgs{Window: "101", Headed: true, Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(1)}),
		{Kind: "get-window-state", Args: json.RawMessage(`{"interactive":true}`)},
	}})
	out, err := cmds["batch"](context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Action 1 (click): ok\nAction 2 (get-window-state): Window: \"winuse fixture app\", App: winusefixture.exe.\n  \t0 window winuse fixture app Secondary Actions: Raise ID: window\n") {
		t.Errorf("got:\n%s", out)
	}
	if trees.reads != 1 {
		t.Errorf("reads = %d, want 1", trees.reads)
	}
}

func TestReadStepsTakeNoLane(t *testing.T) {
	s := defaultServices(testServices(&observeTrees{}))
	s.WindowThread = func(uint64) uint32 { return 7 }
	set := s.batchLanes(context.Background(), mustJSON(t, protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		{Kind: "get-window-state"}, {Kind: "find", Args: json.RawMessage(`{"role":"button"}`)},
	}}), fakeWindows()[0])
	if len(set.keys) != 0 {
		t.Errorf("a Batch of reads takes %v, want no lane", set.keys)
	}
	set = s.batchLanes(context.Background(), mustJSON(t, protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		{Kind: "get-window-state", Args: json.RawMessage(`{"screenshot":true}`)},
	}}), fakeWindows()[0])
	if len(set.keys) != 1 || set.keys[0] != desktopLane {
		t.Errorf("a screenshot step takes %v, want the desktop lane", set.keys)
	}
	// serialized does not take the desktop lane again inside a Batch that
	// holds it (lanes are not reentrant).
	ctx := withLaneSet(context.Background(), set)
	done := make(chan struct{})
	s.lanes.lane(desktopLane) <- struct{}{} // the Batch holds it
	go func() {
		s.serialized(func(context.Context, json.RawMessage) (string, *Error) { return "", nil })(ctx, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serialized waited for the lane its Batch holds")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The observation takes the read's defaults (speed 13): the IDs several
// Elements share are left out and the cap applies; --full prints Codex's
// text.
func TestObserveTakesTheReadDefaults(t *testing.T) {
	before, after := observeFixture(t)
	ok := func(context.Context, json.RawMessage) (string, *Error) { return "ok\n", nil }
	trees := &observeTrees{stored: before.Elements, next: after}
	out, _ := observedCmd(t, trees, "click", ok, &protocol.Observe{Mode: protocol.ObserveFull})
	if !strings.Contains(out, "\t\t12 button Solo Two\n") {
		t.Errorf("the shared ID is left out by default:\n%s", out)
	}
	trees = &observeTrees{stored: before.Elements, next: after}
	out, _ = observedCmd(t, trees, "click", ok, &protocol.Observe{Mode: protocol.ObserveFull, Full: true})
	if out != "ok\n"+wstate.Render(after) {
		t.Errorf("--full is Codex's text:\n%s", out)
	}
	trees = &observeTrees{stored: before.Elements, next: after}
	out, _ = observedCmd(t, trees, "click", ok, &protocol.Observe{Mode: protocol.ObserveFull, MaxChars: intPtrBatch(600)})
	if !strings.Contains(out, "Note: --max-chars 600 stopped the output") {
		t.Errorf("--max-chars cuts the observation:\n%s", out)
	}
	if v := observeRef(&protocol.Observe{Mode: "diff"}).view(""); v.MaxChars != defaultMaxChars {
		t.Errorf("the default cap is %d, want %d", v.MaxChars, defaultMaxChars)
	}
}
