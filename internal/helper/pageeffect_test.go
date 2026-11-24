package helper

// What a Page click or key did (.scratch/bladebro-ideas ticket 06): the
// note after a Headless Page click's or key's `ok`, per outcome, its
// agreement with --observe, the --json effect, and single dispatch. The
// Page is pageRig's cdptest fake, its isolated world answering the effect
// watch's scripts (page/effect.go) by their markers. No browser runs.

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// effectWorld gives pageRig's Page the effect watch's isolated world: final
// is what the read script answers, onPoll runs on each poll (n from 1) to
// send the Page's events. It returns the point the watch was armed at
// (nil for none), once the command ran.
type effectWorld struct {
	mu      sync.Mutex
	armed   bool
	armArgs []any
}

func (r *pageRig) effectWorld(t *testing.T, final map[string]any, onPoll func(c *cdptest.Conn, n int)) *effectWorld {
	t.Helper()
	saveQ, saveB := page.EffectQuiet, page.EffectBound
	page.EffectQuiet, page.EffectBound = 20*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { page.EffectQuiet, page.EffectBound = saveQ, saveB })
	ew := &effectWorld{}
	polls := 0
	r.srv.Handle("Page.createIsolatedWorld", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"executionContextId": 7}, nil
	})
	r.srv.Handle("Runtime.callFunctionOn", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			Fn        string `json:"functionDeclaration"`
			ContextID int    `json:"executionContextId"`
			Args      []struct {
				Value any `json:"value"`
			} `json:"arguments"`
		}
		json.Unmarshal(m.Params, &p)
		var v any
		switch {
		case strings.Contains(p.Fn, "winuse:effect-arm"):
			ew.mu.Lock()
			ew.armed = true
			for _, a := range p.Args {
				ew.armArgs = append(ew.armArgs, a.Value)
			}
			ew.mu.Unlock()
			v = map[string]any{"ok": true}
		case strings.Contains(p.Fn, "winuse:effect-poll"):
			ew.mu.Lock()
			polls++
			n := polls
			ew.mu.Unlock()
			if onPoll != nil {
				onPoll(c, n)
			}
			v = map[string]any{"n": 0, "ago": 1000}
		case strings.Contains(p.Fn, "winuse:effect-read"):
			v = final
		case strings.Contains(p.Fn, "winuse:aim"):
			v = map[string]any{"state": "ok", "l": 10.0, "t": 20.0, "w": 50.0, "h": 10.0, "x": 35.0, "y": 25.0, "chain": true, "dpr": 1.5, "name": "page name"}
		case strings.Contains(p.Fn, "isConnected"):
			v = true
		}
		return map[string]any{"result": map[string]any{"type": "object", "value": v}}, nil
	})
	return ew
}

func (ew *effectWorld) point() []any {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	if !ew.armed {
		return nil
	}
	return append([]any{}, ew.armArgs...)
}

// runEffect runs a command on the rig with an effect record, as the server
// runs every request; under marks it as running under --observe, as the
// --observe wrapper runs the Action.
func (r *pageRig) runEffect(t *testing.T, cmd string, args any, observing bool) (string, *Error, string) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, rec := withEffectRecord(context.Background())
	if observing {
		ctx = context.WithValue(ctx, openedRecordKey{}, &openedRecord{})
	}
	out, e := CommandsWith(r.svc)[cmd](ctx, raw)
	return out, e, rec.get()
}

// pressed counts the mouse presses the Page got.
func (r *pageRig) pressed() int {
	n := 0
	for _, s := range r.sent() {
		if strings.HasPrefix(s, "dispatchMouseEvent mousePressed") {
			n++
		}
	}
	return n
}

func navigatedTo(url string) func(c *cdptest.Conn, n int) {
	return func(c *cdptest.Conn, n int) {
		if n == 1 {
			c.Event("S", "Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "F1", "loaderId": "L2", "url": url}})
		}
	}
}

func TestPageClickSaysWhatItDid(t *testing.T) {
	const deliveredNone = "ok\nNote: no change seen in the Page; the click was delivered, so read the Page before repeating it\n"
	cases := []struct {
		name   string
		final  map[string]any
		onPoll func(c *cdptest.Conn, n int)
		want   string
		effect string
	}{
		{"changed", map[string]any{"added": 2, "removed": 1}, nil, "ok\n", "changed"},
		{"text only", map[string]any{"text": 3}, nil, "ok\nNote: the Page changed (text only)\n", "text"},
		{"no change", map[string]any{}, nil, deliveredNone, "none"},
		{"no change, the point now covered", map[string]any{"by": `<div class="modal">`}, nil,
			"ok\nNote: no change seen in the Page; element 7 is now covered by <div class=\"modal\"> at its centre; the click was delivered, so read the Page before repeating it\n", "none"},
		{"no change, the target gone", map[string]any{"gone": true}, nil,
			"ok\nNote: no change seen in the Page; element 7 is gone from the Page; the click was delivered, so read the Page before repeating it\n", "none"},
		{"navigated", map[string]any{}, navigatedTo("https://site.test/next"), "ok\nNote: the Page navigated to https://site.test/next\n", "navigated"},
		{"still loading", map[string]any{}, func(c *cdptest.Conn, n int) {
			if n == 1 {
				c.Event("S", "Page.frameRequestedNavigation", map[string]any{"frameId": "F1", "reason": "anchorClick", "url": "https://slow.test/"})
			}
		}, "ok\nNote: the Page is navigating to https://slow.test/ and had not loaded when winuse looked; winuse wait 301 load waits for it\n", "navigated"},
		{"a frame winuse cannot watch", map[string]any{"blind": true}, nil, "ok\n", "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newPageRig(t)
			ew := r.effectWorld(t, c.final, c.onPoll)
			out, e, effect := r.runEffect(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}, false)
			if e != nil || out != c.want {
				t.Errorf("output %q, %+v; want %q", out, e, c.want)
			}
			if effect != c.effect {
				t.Errorf("effect %q, want %q", effect, c.effect)
			}
			// Single dispatch: one press, whatever was seen.
			if n := r.pressed(); n != 1 {
				t.Errorf("%d presses sent: %v", n, r.sent())
			}
			if got := ew.point(); len(got) != 2 || got[0] != 35.0 || got[1] != 25.0 {
				t.Errorf("the watch was armed at %v, want the click point 35,25", got)
			}
			r.noForeground(t)
		})
	}
}

func TestPagePressKeySaysWhatItDid(t *testing.T) {
	r := newPageRig(t)
	ew := r.effectWorld(t, map[string]any{}, nil)
	out, e, effect := r.runEffect(t, "press-key", protocol.PressKeyArgs{Window: "301", Key: "Return"}, false)
	want := "ok\nNote: no change seen in the Page; the key was delivered, so read the Page before repeating it\n"
	if e != nil || out != want || effect != "none" {
		t.Errorf("output %q, %+v, effect %q; want %q, none", out, e, effect, want)
	}
	if got := ew.point(); got == nil || len(got) != 0 {
		t.Errorf("a key's watch was armed at %v, want no point", got)
	}

	r = newPageRig(t)
	r.effectWorld(t, map[string]any{}, navigatedTo("https://site.test/search?q=x"))
	out, e, effect = r.runEffect(t, "press-key", protocol.PressKeyArgs{Window: "301", Key: "Return"}, false)
	if e != nil || out != "ok\nNote: the Page navigated to https://site.test/search?q=x\n" || effect != "navigated" {
		t.Errorf("Return that submitted: %q, %+v, effect %q", out, e, effect)
	}
}

// type-text keeps its plain `ok`: typing into a field is what it does.
func TestPageTypeTextHasNoEffectNote(t *testing.T) {
	r := newPageRig(t)
	ew := r.effectWorld(t, map[string]any{}, nil)
	out, e, effect := r.runEffect(t, "type-text", protocol.TypeTextArgs{Window: "301", Text: "hi"}, false)
	if e != nil || out != okOutput || effect != "" {
		t.Errorf("type-text: %q, %+v, effect %q", out, e, effect)
	}
	if ew.point() != nil {
		t.Error("type-text armed the watch")
	}
}

// Under --observe the diff shows what changed, so only a navigation gets
// the note; the --json effect is there all the same.
func TestPageEffectUnderObserve(t *testing.T) {
	r := newPageRig(t)
	r.effectWorld(t, map[string]any{}, nil)
	out, e, effect := r.runEffect(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}, true)
	if e != nil || out != okOutput || effect != "none" {
		t.Errorf("no change under --observe: %q, %+v, effect %q; want a plain ok", out, e, effect)
	}

	r = newPageRig(t)
	r.effectWorld(t, map[string]any{"text": 1}, nil)
	if out, _, _ := r.runEffect(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}, true); out != okOutput {
		t.Errorf("text only under --observe: %q, want a plain ok", out)
	}

	r = newPageRig(t)
	r.effectWorld(t, map[string]any{}, navigatedTo("https://site.test/next"))
	out, e, _ = r.runEffect(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}, true)
	if e != nil || out != "ok\nNote: the Page navigated to https://site.test/next\n" {
		t.Errorf("navigated under --observe: %q, %+v", out, e)
	}
}

// A Page with no isolated world for the watch: the click is as before, a
// plain `ok`, and the effect is unknown.
func TestPageEffectUnknownWithoutAWatch(t *testing.T) {
	r := newPageRig(t)
	out, e, effect := r.runEffect(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}, false)
	if e != nil || out != okOutput || effect != "unknown" {
		t.Errorf("click: %q, %+v, effect %q; want ok, unknown", out, e, effect)
	}
	if n := r.pressed(); n != 1 {
		t.Errorf("%d presses", n)
	}
}

// A refused click sent nothing, so it has no effect; a click a dialog
// answered was delivered, and nothing more is known.
func TestPageEffectOnRefusalsAndDialogs(t *testing.T) {
	r := newPageRig(t)
	r.effectWorld(t, map[string]any{}, nil)
	r.svc.Locked = func() (bool, string) { return true, "Winlogon" }
	_, e, effect := r.runEffect(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}, false)
	if e == nil || effect != "" {
		t.Errorf("refused: %+v, effect %q; want a failure and no effect", e, effect)
	}

	r = newPageRig(t)
	r.set(func() { r.holdPress = true })
	ew := r.effectWorld(t, map[string]any{}, nil)
	out, e, effect := r.runEffect(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}, false)
	if e != nil || !strings.Contains(out, "confirm dialog") || strings.Contains(out, "no change seen") || effect != "unknown" {
		t.Errorf("dialog: %q, %+v, effect %q", out, e, effect)
	}
	_ = ew
}

// The server puts the effect a command recorded on its response, for
// --json; a Batch and a failure carry none.
func TestServerReportsTheEffect(t *testing.T) {
	cmds := map[string]Command{
		"click": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			recordEffect(ctx, page.EffectNone)
			return okOutput, nil
		},
		"batch": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			recordEffect(ctx, page.EffectChanged)
			return "Action 1 (click): ok\n", nil
		},
		"press-key": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			recordEffect(ctx, page.EffectText)
			return "", Fail("the Page did not answer", "")
		},
	}
	srv := New("v1", time.Minute, time.Second, cmds, log.New(io.Discard, "", 0))
	if resp := srv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: "click"}); !resp.OK || resp.Effect != "none" {
		t.Errorf("click: %+v, want effect none", resp)
	}
	if resp := srv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: "batch"}); !resp.OK || resp.Effect != "" {
		t.Errorf("batch: %+v, want no effect", resp)
	}
	if resp := srv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: "press-key"}); resp.OK || resp.Effect != "" {
		t.Errorf("failure: %+v, want no effect", resp)
	}
}
