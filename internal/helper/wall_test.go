package helper

// The wall Note after navigate, back, forward and reload
// (.scratch/bladebro-ideas ticket 05), over the fake launched Chrome
// (winRig): the Page's block.js answer becomes one Note line, the command
// stays ok, nothing more is sent to the Page, and --wait none (whose
// document may still be the old one) is not checked. What block.js finds
// on real pages is test/e2e's TestWallCheck. No browser runs.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// wallPage answers the wall check (block.js, by its marker) with kind, or
// with a throw; other functions get the fake's empty answer.
type wallPage struct {
	mu     sync.Mutex
	kind   string
	throw  bool
	checks int
	worlds []any // the execution context each check ran in
}

func (p *wallPage) set(kind string) {
	p.mu.Lock()
	p.kind = kind
	p.mu.Unlock()
}

func (p *wallPage) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.checks
}

func (p *wallPage) handle(s *cdptest.Server) {
	s.Handle("Runtime.callFunctionOn", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var call map[string]any
		json.Unmarshal(m.Params, &call)
		if fn, _ := call["functionDeclaration"].(string); !strings.Contains(fn, "/* winuse:block */") {
			return map[string]any{}, nil
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.checks++
		p.worlds = append(p.worlds, call["executionContextId"])
		if p.throw {
			return map[string]any{"result": map[string]any{"type": "object"},
				"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "TypeError: boom"}}}, nil
		}
		return map[string]any{"result": map[string]any{"type": "string", "value": p.kind}}, nil
	})
}

func TestNavigationNotesAWall(t *testing.T) {
	r := newWinRig(t)
	p := &wallPage{kind: "Cloudflare"}
	p.handle(r.browser.Srv)

	out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://shop.test/"})
	if want := "ok https://shop.test/\n" + protocol.WallNote("Cloudflare") + "\n"; e != nil || out != want {
		t.Fatalf("navigate onto a wall: %q %+v, want %q", out, e, want)
	}
	if p.count() != 1 || p.worlds[0] != float64(77) {
		t.Errorf("the check ran %d times, in %v; want once in the isolated world 77", p.count(), p.worlds)
	}
	worlds := r.sent("Page.createIsolatedWorld")
	if len(worlds) == 0 {
		t.Error("no isolated world for the check")
	}
	for _, w := range worlds {
		if !strings.Contains(w, `"frameId":"F1"`) {
			t.Errorf("an isolated world %s, want the main frame F1", w)
		}
	}

	p.set("rate-limit")
	if out, e := r.run(t, "reload", protocol.NavigateArgs{Window: "301"}); e != nil || out != "ok https://shop.test/\n"+protocol.WallNote("rate-limit")+"\n" {
		t.Errorf("reload onto a wall: %q %+v", out, e)
	}
	p.set("")
	if out, e := r.run(t, "back", protocol.NavigateArgs{Window: "301"}); e != nil || out != "ok https://docs.test/\n" {
		t.Errorf("back to an ordinary page: %q %+v", out, e)
	}
	p.set("DataDome")
	if out, e := r.run(t, "forward", protocol.NavigateArgs{Window: "301"}); e != nil || out != "ok https://shop.test/\n"+protocol.WallNote("DataDome")+"\n" {
		t.Errorf("forward onto a wall: %q %+v", out, e)
	}
	if p.count() != 4 {
		t.Errorf("%d checks for 4 navigations", p.count())
	}

	// --wait none prints at once: the document may still be the old one, so
	// it is not checked.
	if out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://fast.test/", Wait: "none"}); e != nil || out != "ok https://fast.test/\n" || p.count() != 4 {
		t.Errorf("--wait none: %q %+v, %d checks", out, e, p.count())
	}
	r.passiveOnly(t)
}

// A 429 wall gets both Notes: the HTTP status (bladebro-ideas 02) first,
// then the wall. Chrome's own error page for an empty body is no wall, so
// it is not checked.
func TestNavigationWallNoteFollowsTheStatusNote(t *testing.T) {
	r := newWinRig(t)
	r.statuses = map[string]int{"https://api.test/busy": 429, "https://api.test/empty": 429}
	p := &wallPage{kind: "rate-limit"}
	p.handle(r.browser.Srv)
	out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://api.test/busy"})
	if want := "ok https://api.test/busy\nNote: the Page answered HTTP 429 Too Many Requests\n" + protocol.WallNote("rate-limit") + "\n"; e != nil || out != want {
		t.Errorf("a 429 wall: %q %+v, want %q", out, e, want)
	}
	before := p.count()
	out, e = r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://api.test/empty"})
	if e != nil || strings.Contains(out, "wall") || p.count() != before {
		t.Errorf("Chrome's error page: %q %+v, %d checks (before %d)", out, e, p.count(), before)
	}
	// A reload there still shows Chrome's page: not checked either.
	out, e = r.run(t, "reload", protocol.NavigateArgs{Window: "301"})
	if e != nil || strings.Contains(out, "wall") || p.count() != before {
		t.Errorf("reload of Chrome's error page: %q %+v, %d checks (before %d)", out, e, p.count(), before)
	}
}

// runFields runs a command as the server does, with the record of what its
// response carries beside its text for --json (responsefields.go).
func runFields(t *testing.T, cmds map[string]Command, cmd string, args any) (string, *Error, responseFields) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, rec := withResponseRecord(context.Background())
	out, e := cmds[cmd](ctx, raw)
	return out, e, rec.fields()
}

// The status and the wall a navigation noted travel as the response's own
// fields (--json's http_status and wall), not read back from its text.
func TestNavigationRecordsTheStatusAndTheWall(t *testing.T) {
	r := newWinRig(t)
	r.statuses = map[string]int{"https://api.test/busy": 429}
	p := &wallPage{kind: "rate-limit"}
	p.handle(r.browser.Srv)
	_, e, f := runFields(t, r.cmds, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://api.test/busy"})
	if e != nil || f.HTTPStatus != 429 || f.Wall != "rate-limit" {
		t.Errorf("a 429 wall: %+v %+v, want status 429 and wall rate-limit", f, e)
	}
	p.set("")
	_, e, f = runFields(t, r.cmds, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://shop.test/"})
	if e != nil || f != (responseFields{}) {
		t.Errorf("an ordinary page: %+v %+v, want no fields", f, e)
	}
}

// A check that fails says nothing: the navigation landed, and the Note is
// only ever a hint.
func TestNavigationWallCheckFailureStaysQuiet(t *testing.T) {
	r := newWinRig(t)
	p := &wallPage{throw: true}
	p.handle(r.browser.Srv)
	if out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://shop.test/"}); e != nil || out != "ok https://shop.test/\n" || p.count() != 1 {
		t.Errorf("a failed check: %q %+v, %d checks", out, e, p.count())
	}
}
