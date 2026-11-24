package page

// What a Page click or key did (.scratch/bladebro-ideas ticket 06): the
// effect watch armed before the input and read after it, against a cdptest
// fake of the Tab's session. The fake answers the watch's three scripts by
// their markers (winuse:effect-arm, -poll and -read) and sends the Page's
// navigation events. No browser runs.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// effectPage is the fake Tab: its top frame F1, the isolated world 7 the
// watch gets, and what its scripts answer.
type effectPage struct {
	srv    *cdptest.Server
	client *cdp.Client

	mu       sync.Mutex
	world    int            // the isolated world's context id; 0 gives none
	url      string         // the top frame's URL now
	loader   string         // and its loader
	armArgs  []any          // the arm script's point
	polls    int            // poll scripts answered
	busyFor  int            // polls that report a mutation just now
	onPoll   func(n int)    // runs on poll n (from 1), before the answer
	final    map[string]any // the read script's answer
	pollGone bool           // polls fail as a destroyed context does
	scripts  []string       // the markers evaluated, in order
}

func newEffectPage(t *testing.T) *effectPage {
	t.Helper()
	saveQ, saveB, saveP := EffectQuiet, EffectBound, effectPoll
	EffectQuiet, EffectBound, effectPoll = 30*time.Millisecond, 400*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { EffectQuiet, EffectBound, effectPoll = saveQ, saveB, saveP })

	p := &effectPage{srv: cdptest.NewServer(), world: 7, url: "https://site.test/a", loader: "L1", final: map[string]any{}}
	t.Cleanup(p.srv.Close)
	p.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		p.mu.Lock()
		defer p.mu.Unlock()
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": p.loader, "url": p.url}}}, nil
	})
	p.srv.Handle("Page.createIsolatedWorld", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.world == 0 {
			return map[string]any{}, nil
		}
		return map[string]any{"executionContextId": p.world}, nil
	})
	p.srv.Handle("Runtime.callFunctionOn", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var q struct {
			Fn        string `json:"functionDeclaration"`
			ContextID int    `json:"executionContextId"`
			Args      []struct {
				Value any `json:"value"`
			} `json:"arguments"`
		}
		json.Unmarshal(m.Params, &q)
		p.mu.Lock()
		defer p.mu.Unlock()
		if q.ContextID != p.world {
			return nil, &cdptest.ReplyError{Code: -32000, Message: "Cannot find context with specified id"}
		}
		value := func(v any) (any, error) {
			return map[string]any{"result": map[string]any{"type": "object", "value": v}}, nil
		}
		switch {
		case strings.Contains(q.Fn, "winuse:effect-arm"):
			p.scripts = append(p.scripts, "arm")
			p.armArgs = nil
			for _, a := range q.Args {
				p.armArgs = append(p.armArgs, a.Value)
			}
			return value(map[string]any{"ok": true})
		case strings.Contains(q.Fn, "winuse:effect-poll"):
			p.polls++
			n := p.polls
			p.scripts = append(p.scripts, "poll")
			if f := p.onPoll; f != nil {
				p.mu.Unlock()
				f(n)
				p.mu.Lock()
			}
			if p.pollGone {
				return nil, &cdptest.ReplyError{Code: -32000, Message: "Cannot find context with specified id"}
			}
			ago := 1000.0
			if n <= p.busyFor {
				ago = 0
			}
			return value(map[string]any{"n": n, "ago": ago})
		case strings.Contains(q.Fn, "winuse:effect-read"):
			p.scripts = append(p.scripts, "read")
			return value(p.final)
		}
		return value(nil)
	})
	ws, err := cdp.DialWS(context.Background(), p.srv.URL(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	p.client = cdp.NewClient(ws)
	t.Cleanup(func() { p.client.Close() })
	return p
}

func (p *effectPage) set(f func()) {
	p.mu.Lock()
	f()
	p.mu.Unlock()
}

// event sends a Page event on the Tab's session S.
func (p *effectPage) event(method string, params any) {
	p.srv.Event("S", method, params)
}

// watch arms a watch at the point (nil for a key) and reads it.
func (p *effectPage) watch(t *testing.T, at *Point) Effect {
	t.Helper()
	ctx := context.Background()
	w := ArmEffect(ctx, p.client, "S", at)
	return w.Read(ctx)
}

func TestEffectNavigatedByACommittedLoad(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() {
		p.onPoll = func(n int) {
			if n == 1 {
				p.event("Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "F1", "loaderId": "L2", "url": "https://site.test/next"}, "type": "Navigation"})
			}
		}
	})
	got := p.watch(t, &Point{X: 35, Y: 25})
	if got.Kind != EffectNavigated || got.URL != "https://site.test/next" || got.Pending {
		t.Errorf("effect %+v, want navigated to https://site.test/next", got)
	}
	if p.armArgs == nil || p.armArgs[0] != 35.0 || p.armArgs[1] != 25.0 {
		t.Errorf("the watch was armed at %v, want the click point 35,25", p.armArgs)
	}
}

func TestEffectNavigatedWithinTheDocument(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() {
		p.onPoll = func(n int) {
			if n == 1 {
				p.event("Page.navigatedWithinDocument", map[string]any{"frameId": "F1", "url": "https://site.test/a#settings"})
			}
		}
		p.final = map[string]any{"added": 4, "removed": 2}
	})
	if got := p.watch(t, nil); got.Kind != EffectNavigated || got.URL != "https://site.test/a#settings" {
		t.Errorf("effect %+v, want navigated to the new route", got)
	}
}

// A navigation the events did not show still counts when the top frame's
// URL or loader differs afterwards.
func TestEffectNavigatedByTheFrameTree(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() { p.onPoll = func(n int) { p.set(func() { p.url = "https://site.test/b" }) } })
	if got := p.watch(t, nil); got.Kind != EffectNavigated || got.URL != "https://site.test/b" {
		t.Errorf("effect %+v, want navigated to https://site.test/b", got)
	}
}

// A load that the top frame started but did not commit within the bound is
// a navigation still pending, named by the URL it requested.
func TestEffectNavigationStillLoading(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() {
		p.onPoll = func(n int) {
			if n == 1 {
				p.event("Page.frameRequestedNavigation", map[string]any{"frameId": "F1", "reason": "anchorClick", "url": "https://slow.test/"})
				p.event("Page.frameStartedLoading", map[string]any{"frameId": "F1"})
			}
		}
	})
	start := time.Now()
	got := p.watch(t, &Point{X: 1, Y: 1})
	if got.Kind != EffectNavigated || !got.Pending || got.URL != "https://slow.test/" {
		t.Errorf("effect %+v, want a pending navigation to https://slow.test/", got)
	}
	if time.Since(start) < EffectBound {
		t.Errorf("a pending load was given up after %s, before the bound %s", time.Since(start), EffectBound)
	}
}

// A load that stopped without committing (a download, a 204) is no
// navigation: the DOM counts decide, at once rather than at the bound.
func TestEffectLoadThatNeverCommitted(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() {
		p.onPoll = func(n int) {
			if n == 1 {
				p.event("Page.frameRequestedNavigation", map[string]any{"frameId": "F1", "reason": "anchorClick", "url": "https://site.test/file.zip"})
				p.event("Page.frameStartedLoading", map[string]any{"frameId": "F1"})
				p.event("Page.frameStoppedLoading", map[string]any{"frameId": "F1"})
			}
		}
	})
	start := time.Now()
	if got := p.watch(t, &Point{X: 1, Y: 1}); got != (Effect{Kind: EffectNone}) {
		t.Errorf("effect %+v, want none", got)
	}
	if time.Since(start) >= EffectBound {
		t.Errorf("waited to the bound for a load that had stopped")
	}
}

// A link that opens a new Tab asks for a navigation too: the Tab's own
// Page did not navigate.
func TestEffectNewTabLinkIsNotANavigation(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() {
		p.onPoll = func(n int) {
			if n == 1 {
				p.event("Page.frameRequestedNavigation", map[string]any{"frameId": "F1", "reason": "anchorClick", "url": "https://other.test/", "disposition": "newTab"})
			}
		}
	})
	if got := p.watch(t, &Point{X: 1, Y: 1}); got.Kind != EffectNone {
		t.Errorf("effect %+v, want none", got)
	}
}

// A child frame's navigation is not the Page's: the DOM counts decide.
func TestEffectChildFrameNavigationIsNotThePages(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() {
		p.onPoll = func(n int) {
			if n == 1 {
				p.event("Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "F9", "parentId": "F1", "loaderId": "L9", "url": "https://ads.test/"}})
				p.event("Page.frameStartedLoading", map[string]any{"frameId": "F9"})
			}
		}
	})
	if got := p.watch(t, &Point{X: 1, Y: 1}); got.Kind != EffectNone {
		t.Errorf("effect %+v, want none", got)
	}
}

func TestEffectDOMChanges(t *testing.T) {
	cases := []struct {
		name  string
		final map[string]any
		want  Effect
	}{
		{"nodes added and removed", map[string]any{"added": 3, "removed": 1, "text": 2}, Effect{Kind: EffectChanged, Added: 3, Removed: 1}},
		{"an attribute", map[string]any{"attrs": 1}, Effect{Kind: EffectChanged}},
		{"a control's state or the focus", map[string]any{"state": true}, Effect{Kind: EffectChanged}},
		{"text only", map[string]any{"text": 2}, Effect{Kind: EffectText}},
		{"nothing", map[string]any{}, Effect{Kind: EffectNone}},
		{"nothing, now covered", map[string]any{"by": `<div class="modal">`}, Effect{Kind: EffectNone, Blocker: `<div class="modal">`}},
		{"nothing, the target gone", map[string]any{"gone": true}, Effect{Kind: EffectNone, Gone: true}},
		{"nothing seen, but the point is in a frame winuse cannot watch", map[string]any{"blind": true}, Effect{Kind: EffectUnknown}},
		{"text in a frame winuse cannot watch", map[string]any{"blind": true, "text": 1}, Effect{Kind: EffectText}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newEffectPage(t)
			p.set(func() { p.final = c.final })
			if got := p.watch(t, &Point{X: 1, Y: 1}); got != c.want {
				t.Errorf("effect %+v, want %+v", got, c.want)
			}
		})
	}
}

// The read waits for the DOM to be quiet: a Page that renders a moment
// after the click is read after it, not before.
func TestEffectWaitsForAQuietDOM(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() { p.busyFor = 4; p.final = map[string]any{"added": 1} })
	if got := p.watch(t, nil); got.Kind != EffectChanged {
		t.Errorf("effect %+v, want changed", got)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.polls <= 4 || p.scripts[len(p.scripts)-1] != "read" {
		t.Errorf("scripts %v: the read came before the DOM was quiet", p.scripts)
	}
}

// A Page that never goes quiet (a clock, an animation) is read at the
// bound.
func TestEffectReadsAtTheBound(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() { p.busyFor = 1 << 30; p.final = map[string]any{"text": 5} })
	start := time.Now()
	got := p.watch(t, nil)
	if got.Kind != EffectText {
		t.Errorf("effect %+v, want text", got)
	}
	if d := time.Since(start); d < EffectBound || d > EffectBound+time.Second {
		t.Errorf("read after %s, want at the bound %s", d, EffectBound)
	}
}

// Without an isolated world, or when the world is lost without a
// navigation, nothing is known: the effect is unknown, never none.
func TestEffectUnknown(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() { p.world = 0 })
	if got := p.watch(t, nil); got.Kind != EffectUnknown {
		t.Errorf("no world: effect %+v, want unknown", got)
	}
	p.mu.Lock()
	scripts := len(p.scripts)
	p.mu.Unlock()
	if scripts != 0 {
		t.Errorf("scripts ran with no world: %d", scripts)
	}

	p = newEffectPage(t)
	p.set(func() { p.pollGone = true })
	if got := p.watch(t, nil); got.Kind != EffectUnknown {
		t.Errorf("world lost: effect %+v, want unknown", got)
	}
}

// The world lost to a load that committed: navigated.
func TestEffectWorldLostToANavigation(t *testing.T) {
	p := newEffectPage(t)
	p.set(func() {
		p.pollGone = true
		// The arm saw L1; the load commits before the first poll.
		p.onPoll = func(n int) { p.set(func() { p.loader, p.url = "L2", "https://site.test/after" }) }
	})
	if got := p.watch(t, nil); got.Kind != EffectNavigated || got.URL != "https://site.test/after" {
		t.Errorf("effect %+v, want navigated to https://site.test/after", got)
	}
}
