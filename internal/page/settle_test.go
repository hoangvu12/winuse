package page

// Fake-clock tests of the settle (bladebro-ideas 07): the decision loop
// over what the Page reports (how long its DOM and its finished requests
// have been quiet), paced by a clock the test owns, so no test sleeps.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// fakeClock is a settleClock whose Sleep moves time on at once.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) {
	if d > 0 {
		c.t = c.t.Add(d)
	}
}

// settlingPage is a Page on the fake clock: the times (after the start) its
// DOM changed and its requests finished, as the in-page observers would
// see them. The observers start with the first probe, so the DOM counts as
// changed then, and the request clock starts then too: a request still in
// flight at the start is unseen until it finishes, so the settle gives one
// a network-quiet window to finish in.
type settlingPage struct {
	clk       *fakeClock
	start     time.Time
	mutations []time.Duration
	requests  []time.Duration
	loading   time.Duration // loading until then
	errs      int           // the first probes fail (a navigation under way)
	probes    int
}

func (p *settlingPage) probe(ctx context.Context) (SettleSample, error) {
	p.probes++
	if p.probes <= p.errs {
		return SettleSample{}, errors.New("Cannot find context with specified id")
	}
	now := p.clk.Now().Sub(p.start)
	last := func(ts []time.Duration, base time.Duration) time.Duration {
		at := base
		for _, t := range ts {
			if t <= now && t > at {
				at = t
			}
		}
		return now - at
	}
	return SettleSample{
		DOMQuiet: last(p.mutations, 0),
		NetQuiet: last(p.requests, 0),
		Loading:  now < p.loading,
	}, nil
}

func newSettlingPage() *settlingPage {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	return &settlingPage{clk: clk, start: clk.t}
}

func every(step, until time.Duration) []time.Duration {
	var out []time.Duration
	for t := step; t <= until; t += step {
		out = append(out, t)
	}
	return out
}

func TestSettleQuietPageTakesOneQuietWindow(t *testing.T) {
	p := newSettlingPage()
	r, err := settleLoop(context.Background(), p.clk, time.Second, nil, p.probe)
	if err != nil || !r.Settled {
		t.Fatalf("settle: %+v %v", r, err)
	}
	if want := max(SettleDOMQuiet, SettleNetQuiet); r.After != want {
		t.Errorf("settled after %s, want %s", r.After, want)
	}
}

func TestSettleWaitsOutALateRender(t *testing.T) {
	p := newSettlingPage()
	// A fetch the Action started finishes at 200 ms, then a render mutates
	// the DOM until 400 ms.
	p.requests = []time.Duration{200 * time.Millisecond}
	p.mutations = []time.Duration{210 * time.Millisecond, 300 * time.Millisecond, 400 * time.Millisecond}
	r, err := settleLoop(context.Background(), p.clk, time.Second, nil, p.probe)
	if err != nil || !r.Settled {
		t.Fatalf("settle: %+v %v", r, err)
	}
	want := max(400*time.Millisecond+SettleDOMQuiet, 200*time.Millisecond+SettleNetQuiet)
	if r.After < want || r.After > want+SettlePoll {
		t.Errorf("settled after %s, want about %s", r.After, want)
	}
}

func TestSettleIgnoresAQuietSocket(t *testing.T) {
	// A websocket or a long poll that never finishes is no resource entry:
	// a Page whose DOM is quiet settles in one window even though it stays
	// connected.
	p := newSettlingPage()
	p.requests = nil
	r, err := settleLoop(context.Background(), p.clk, time.Second, nil, p.probe)
	if err != nil || !r.Settled || r.After > max(SettleDOMQuiet, SettleNetQuiet) {
		t.Fatalf("settle: %+v %v", r, err)
	}
}

func TestSettleBoundSaysWhatKeptChanging(t *testing.T) {
	cases := []struct {
		name string
		page func(p *settlingPage)
		busy SettleBusy
	}{
		{"dom", func(p *settlingPage) { p.mutations = every(50*time.Millisecond, 5*time.Second) }, SettleBusyDOM},
		{"requests", func(p *settlingPage) { p.requests = every(100*time.Millisecond, 5*time.Second) }, SettleBusyNet},
		{"loading", func(p *settlingPage) { p.loading = time.Hour }, SettleBusyLoading},
		{"navigating", func(p *settlingPage) { p.errs = 1 << 20 }, SettleBusyNavigating},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newSettlingPage()
			c.page(p)
			r, err := settleLoop(context.Background(), p.clk, time.Second, nil, p.probe)
			if err != nil {
				t.Fatal(err)
			}
			if r.Settled || r.Busy != c.busy {
				t.Errorf("got %+v, want unsettled with %q", r, c.busy)
			}
			if r.After < time.Second || r.After > time.Second+SettlePoll {
				t.Errorf("gave up after %s, want the 1s bound", r.After)
			}
		})
	}
}

func TestSettleOutlivesANavigation(t *testing.T) {
	p := newSettlingPage()
	p.errs = 2 // the old document's world is gone for two probes
	r, err := settleLoop(context.Background(), p.clk, time.Second, nil, p.probe)
	if err != nil || !r.Settled {
		t.Fatalf("settle: %+v %v", r, err)
	}
}

func TestSettleStopsAtADialogAndEsc(t *testing.T) {
	p := newSettlingPage()
	dialog := &DialogOpenError{Dialog: Dialog{Type: "alert", Message: "hi"}}
	_, err := settleLoop(context.Background(), p.clk, time.Second, nil, func(ctx context.Context) (SettleSample, error) {
		return SettleSample{}, dialog
	})
	var d *DialogOpenError
	if !errors.As(err, &d) {
		t.Errorf("dialog: %v", err)
	}
	_, err = settleLoop(context.Background(), p.clk, time.Second, func() bool { return true }, p.probe)
	if !errors.Is(err, ErrStopped) {
		t.Errorf("esc: %v", err)
	}
}

func TestSettleEndsWithItsContext(t *testing.T) {
	p := newSettlingPage()
	p.mutations = every(50*time.Millisecond, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	r, err := settleLoop(ctx, p.clk, time.Hour, nil, func(ctx context.Context) (SettleSample, error) {
		if n++; n == 3 {
			// The probe in flight when the context ends fails with it; the
			// reason stays what the Page showed last.
			cancel()
			return SettleSample{}, ctx.Err()
		}
		return p.probe(ctx)
	})
	if err != nil || r.Settled || r.Busy != SettleBusyDOM {
		t.Errorf("got %+v %v", r, err)
	}
}

// TestSettleOverCDP: the probe runs in an isolated world of the main
// frame; a navigation that takes the world away makes a new one; the
// observers are disconnected at the end; nothing turns Network or Runtime
// on.
func TestSettleOverCDP(t *testing.T) {
	f := newFakeTab(t)
	var mu sync.Mutex
	worlds, evals := 0, 0
	var stopped []int
	f.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": "L1", "url": "https://a.test/"}}}, nil
	})
	f.srv.Handle("Page.createIsolatedWorld", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		var p struct {
			FrameID string `json:"frameId"`
		}
		json.Unmarshal(m.Params, &p)
		if p.FrameID != "F1" {
			t.Errorf("world in frame %q", p.FrameID)
		}
		worlds++
		return map[string]any{"executionContextId": 10 + worlds}, nil
	})
	f.srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		var p struct {
			Expression string `json:"expression"`
			ContextID  int    `json:"contextId"`
		}
		json.Unmarshal(m.Params, &p)
		if strings.Contains(p.Expression, "s.stop()") && !strings.Contains(p.Expression, "winuse:settle") {
			stopped = append(stopped, p.ContextID)
			return map[string]any{"result": map[string]any{"type": "undefined"}}, nil
		}
		evals++
		if evals == 1 {
			return nil, &cdptest.ReplyError{Code: -32000, Message: "Cannot find context with specified id"}
		}
		return map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{"dom": 900, "net": 900, "loading": false}}}, nil
	})
	r, err := Settle(context.Background(), f.c, "S", time.Second, nil)
	if err != nil || !r.Settled {
		t.Fatalf("settle: %+v %v", r, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if worlds != 2 || evals != 2 {
		t.Errorf("worlds %d, probes %d; want a new world after the navigation", worlds, evals)
	}
	if len(stopped) != 1 || stopped[0] != 12 {
		t.Errorf("observers disconnected in %v, want the last world 12", stopped)
	}
	for _, m := range f.srv.Methods() {
		if strings.HasPrefix(m, "Network.") || m == "Runtime.enable" {
			t.Errorf("the settle sent %s", m)
		}
	}
}
