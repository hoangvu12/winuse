package helper

// Lanes (multi-agent 06, ADR 0014 Decisions 1, 6 and 7): Headless Actions
// on Windows of two UI threads run at once, on one thread they take turns;
// Page Actions on two browser Windows run at once; a Headed Action holds
// the desktop and its Window's thread; a request that waits past its limit
// is refused with nothing sent, naming the agent and what it uses; lanes
// are taken in one order, so crossing Batches never deadlock; each
// command's lanes come from its own routing; the banner names the agent
// holding the desktop. Esc while waiting is escall_test.go's.

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

func agentCtx(id, label string) context.Context {
	return agent.NewContext(context.Background(), agent.Identity{ID: id, Label: label})
}

// holdLanes runs a command on set that blocks until release is closed,
// and returns once it holds its lanes.
func holdLanes(t *testing.T, s Services, ctx context.Context, set laneSet) (release chan struct{}, done chan *Error) {
	t.Helper()
	started := make(chan struct{})
	release, done = make(chan struct{}), make(chan *Error, 1)
	go func() {
		_, err := s.onLanes(ctx, set, func(context.Context, json.RawMessage) (string, *Error) {
			close(started)
			<-release
			return okOutput, nil
		}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the first request never got its lanes")
	}
	return release, done
}

// runsNow reports whether a request on set runs while another holds its
// lanes, and its refusal when it does not (LaneWait is short).
func runsNow(s Services, ctx context.Context, set laneSet) (bool, *Error) {
	ran := false
	_, err := s.onLanes(ctx, set, func(context.Context, json.RawMessage) (string, *Error) {
		ran = true
		return okOutput, nil
	}, nil)
	return ran, err
}

func laneServices() Services {
	return Services{lanes: newLaneManager(), LaneWait: 50 * time.Millisecond}
}

func TestLanesParallelAndSerial(t *testing.T) {
	alice, bob := agentCtx("a1", "alice"), agentCtx("b2", "bob")
	for _, c := range []struct {
		name     string
		held, at laneSet
		parallel bool
		refusal  string
	}{
		{"Headless on two threads", newLaneSet(101, threadLane(1)), newLaneSet(202, threadLane(2)), true, ""},
		{"Headless on one thread (two Windows of one Chrome)", newLaneSet(101, threadLane(1)), newLaneSet(102, threadLane(1)), false, "agent alice is using window 101; nothing was sent"},
		{"Page Actions in two browser Windows", newLaneSet(301, pageLane(301)), newLaneSet(302, pageLane(302)), true, ""},
		{"Page Actions in one browser Window", newLaneSet(301, pageLane(301)), newLaneSet(301, pageLane(301)), false, "agent alice is using window 301; nothing was sent"},
		{"Page beside Headless on the browser's thread", newLaneSet(301, pageLane(301)), newLaneSet(301, threadLane(7)), true, ""},
		{"Headed beside Headless elsewhere", newLaneSet(101, desktopLane, threadLane(1)), newLaneSet(202, threadLane(2)), true, ""},
		{"Headed beside Headless on its thread", newLaneSet(101, desktopLane, threadLane(1)), newLaneSet(101, threadLane(1)), false, "agent alice is using window 101; nothing was sent"},
		{"two Headed", newLaneSet(101, desktopLane, threadLane(1)), newLaneSet(202, desktopLane, threadLane(2)), false, "agent alice is using the desktop; nothing was sent"},
	} {
		s := laneServices()
		release, done := holdLanes(t, s, alice, c.held)
		ran, err := runsNow(s, bob, c.at)
		if ran != c.parallel {
			t.Errorf("%s: ran = %v, want %v", c.name, ran, c.parallel)
		}
		if c.refusal != "" && (err == nil || err.Msg != c.refusal || err.Hint == "") {
			t.Errorf("%s: refusal %+v, want %q", c.name, err, c.refusal)
		}
		close(release)
		if err := <-done; err != nil {
			t.Errorf("%s: the holder failed: %+v", c.name, err)
		}
		// Released: the waiting side runs now.
		if ran, err := runsNow(s, bob, c.at); !ran || err != nil {
			t.Errorf("%s: after the release: %v, %+v", c.name, ran, err)
		}
	}
}

func TestLaneOrderPreventsDeadlock(t *testing.T) {
	set := newLaneSet(0, pageLane(5), threadLane(9), desktopLane, threadLane(2), threadLane(9))
	if want := []string{desktopLane, threadLane(2), threadLane(9), pageLane(5)}; !slices.Equal(set.keys, want) {
		t.Fatalf("order %q, want %q", set.keys, want)
	}
	// Two Batches whose Windows cross take their lanes in one order: run
	// many at once, every one finishes.
	s := Services{lanes: newLaneManager(), LaneWait: 5 * time.Second}
	var wg sync.WaitGroup
	for i := range 40 {
		set := newLaneSet(0, threadLane(1), threadLane(2))
		if i%2 == 1 {
			set = newLaneSet(0, threadLane(2), threadLane(1))
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ran, err := runsNow(s, context.Background(), set); !ran || err != nil {
				t.Errorf("a crossing Batch: %v, %+v", ran, err)
			}
		}()
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("crossing Batches deadlocked")
	}
}

func TestLaneWaitClampedByTheBudget(t *testing.T) {
	s := Services{lanes: newLaneManager()} // the 12 s default limit
	release, done := holdLanes(t, s, agentCtx("a1", "alice"), newLaneSet(0, desktopLane))
	defer func() { close(release); <-done }()
	ctx, cancel := context.WithTimeout(agentCtx("b2", "bob"), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runsNow(s, ctx, newLaneSet(0, desktopLane))
	if err == nil || err.Msg != "agent alice is using the desktop; nothing was sent" {
		t.Errorf("refusal %+v", err)
	}
	if took := time.Since(start); took > 300*time.Millisecond {
		t.Errorf("answered after %s, past the request's own deadline", took)
	}
}

func TestLanesForCommands(t *testing.T) {
	spy := newHeadlessSpy()
	spy.svc.Windows = func() ([]win.WindowInfo, error) { return fakeWindows(), nil }
	spy.svc.WindowThread = func(hwnd uint64) uint32 {
		return map[uint64]uint32{101: 11, 202: 22, 203: 22}[hwnd]
	}
	s := spy.svc
	ctx := context.Background()
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	for _, c := range []struct {
		name string
		cmd  string
		args json.RawMessage
		want []string
	}{
		{"Headless click", "click", raw(protocol.ClickArgs{Window: "101", ElementIndex: idx(2)}), []string{threadLane(11)}},
		{"Headed click", "click", raw(protocol.ClickArgs{Window: "101", ElementIndex: idx(2), Headed: true}), []string{desktopLane, threadLane(11)}},
		{"a Window Action", "move-window", raw(protocol.MoveWindowArgs{Window: "202", X: 1, Y: 2}), []string{threadLane(22)}},
		{"another Window of one thread", "close-window", raw(protocol.WindowActionArgs{Window: "203"}), []string{threadLane(22)}},
		{"activate-window", "activate-window", raw(protocol.ActivateArgs{Window: "101"}), []string{desktopLane, threadLane(11)}},
		{"press-key without --headed", "press-key", raw(protocol.PressKeyArgs{Window: "101", Key: "Return"}), []string{threadLane(11)}},
		{"press-key --headed", "press-key", raw(protocol.PressKeyArgs{Window: "101", Key: "a", Headed: true}), []string{desktopLane, threadLane(11)}},
		{"a command with no Window", "clipboard-clear", nil, []string{desktopLane}},
		{"an unknown Window", "click", raw(protocol.ClickArgs{Window: "999", ElementIndex: idx(2)}), []string{desktopLane}},
		{"a Headless Batch", "batch", raw(protocol.BatchArgs{Window: "101", Steps: []protocol.Step{stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)})}}), []string{threadLane(11)}},
		{"a Batch with a pause", "batch", raw(protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
			stepOf("wait", protocol.WaitArgs{For: protocol.WaitMS, MS: 100}),
		}}), []string{threadLane(11), pageLane(101)}},
		{"a Batch with a Headed step", "batch", raw(protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "a", Headed: true}),
		}}), []string{desktopLane, threadLane(11)}},
	} {
		if got := s.lanesFor(ctx, c.cmd, c.args).keys; !slices.Equal(got, c.want) {
			t.Errorf("%s: lanes %q, want %q", c.name, got, c.want)
		}
	}

	// A Page Element click and a page-only command take the browser
	// Window's Page lane.
	r := newPageRig(t)
	r.svc.WindowThread = func(uint64) uint32 { return 33 }
	if got := r.svc.lanesFor(ctx, "click", raw(protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})).keys; !slices.Equal(got, []string{pageLane(301)}) {
		t.Errorf("a Page click: %q", got)
	}
	if got := r.svc.lanesFor(ctx, "click", raw(protocol.ClickArgs{Window: "301", ElementIndex: idx(2)})).keys; !slices.Equal(got, []string{threadLane(33)}) {
		t.Errorf("a click on the browser frame: %q", got)
	}
	if got := r.svc.lanesFor(ctx, "eval", raw(protocol.EvalArgs{Window: "301", Expression: "1"})).keys; !slices.Equal(got, []string{pageLane(301)}) {
		t.Errorf("eval: %q", got)
	}
	// A Batch with a Page step and a frame step takes both lanes.
	mixed := raw(protocol.BatchArgs{Window: "301", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(7)}),
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
	}})
	if got := r.svc.lanesFor(ctx, "batch", mixed).keys; !slices.Equal(got, []string{threadLane(33), pageLane(301)}) {
		t.Errorf("a mixed Batch: %q", got)
	}
}

// bannerSpy is an indicator that names agents.
type bannerSpy struct {
	overlaySpy
	agents []string
}

func (b *bannerSpy) SetAgent(label string) error { b.agents = append(b.agents, label); return nil }

func TestDesktopLaneNamesItsAgentOnTheBanner(t *testing.T) {
	banner := &bannerSpy{}
	s := Services{lanes: newLaneManager(), Overlay: banner}
	runsNow(s, agentCtx("a1", "alice"), newLaneSet(0, desktopLane))
	runsNow(s, agentCtx("a1", "alice"), newLaneSet(101, threadLane(1))) // Headless: no banner
	runsNow(s, context.Background(), newLaneSet(0, desktopLane))        // the default agent: unnamed
	if !slices.Equal(banner.agents, []string{"alice", ""}) {
		t.Errorf("banner agents %q", banner.agents)
	}
	if win.OverlayBannerText("alice") != "winuse is in control for alice — press Esc to stop" || win.OverlayBannerText("") != "winuse is in control — press Esc to stop" {
		t.Error("banner text")
	}
}

// A Headless Action beside another agent's Headed one does not blame its
// App for the foreground: the Headed Action may have moved it.
func TestSelfActivationBesideAHeadedAction(t *testing.T) {
	spy, d := fgSpy(0)
	lanes := newLaneManager()
	spy.svc.lanes = lanes
	spy.svc.Ready = func(uint64) error {
		lanes.setHolder(desktopLane, laneHolder{agent: "bob", window: 777}) // bob's Headed Action
		d.fg = 777
		return nil
	}
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	if e != nil || out != "ok\n" {
		t.Errorf("output %q, %+v; want ok with no foreground note", out, e)
	}
}
