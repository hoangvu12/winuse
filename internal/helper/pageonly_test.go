package helper

// Offline tests of the page-only Actions (browser merge ticket 06): the
// window-level commands over a fake launched Chrome (pagetest) and the
// fake winuse extension, and the Page Element forms over 05's cdptest
// Page (pageRig). Per command: navigate (success, errorText, a slow load),
// the ends of the history, reload, the dialog watch's fast failures and
// its Window State note, upload (a missing file, an intercepted chooser, a
// file input), hover and drag, set-value on a list and a check box, wait
// (success, timeout, Esc), eval (value, exception, the cut), downloads
// (the path, the extension's refusal), the right click's context menu
// guard, a click that opens a chooser, and the Batch's scope. No browser
// runs.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// docsSession is the session the launched fake gives window 301's Tab.
const docsSession = "session-" + tDocs

// winRig is window 301 of a fake launched Chrome, showing the Docs Tab,
// with the address bar (5) and the document (4) in its latest Window
// State.
type winRig struct {
	*tabRig
	conn *browser.Connection

	mu        sync.Mutex
	loader    string
	url       string
	ready     string
	cur       int
	entries   []page.HistoryEntry
	evalReply any
	waitAfter int // the wait predicate is true from this poll on
	polls     int
	onPoll    func(n int)
	calls     []string // what the commands sent, as "method params"
	dialog    bool
}

var (
	docRect  = win.Rect{Left: 108, Top: 140, Right: 1292, Bottom: 852}
	addrRect = win.Rect{Left: 300, Top: 100, Right: 900, Bottom: 128}
)

func newWinRig(t *testing.T) *winRig {
	t.Helper()
	b := launchedBrowser(t)
	r := &winRig{conn: b.Connection(t), loader: "L1", url: "https://docs.test/", ready: "complete",
		entries: []page.HistoryEntry{{ID: 7, URL: "https://docs.test/"}}}
	actions := &stripActions{
		els: map[uint64][]wstate.Element{301: {
			{Depth: 0, Role: "window", Name: "Docs - Google Chrome"},
			{Depth: 1, Role: "tab", Name: "Inbox"},
			{Depth: 1, Role: "tab", Name: "Docs"},
			{Depth: 1, Role: "tab", Name: "News"},
			{Depth: 1, Role: "document", Name: "Docs"},
			{Depth: 1, Role: "edit", Name: "Address and search bar"},
		}},
		bounds: map[int]win.Rect{4: docRect, 5: addrRect},
	}
	r.tabRig = newTabRig(t, r.conn, nil, b, actions)
	r.svc.Pages.(*livePages).reader.Settle = time.Millisecond
	r.handlers(b.Srv)
	t.Cleanup(func() { page.Unwatch(r.conn.Client, docsSession) })
	return r
}

func (r *winRig) note(method string, params json.RawMessage) {
	r.mu.Lock()
	r.calls = append(r.calls, method+" "+string(params))
	r.mu.Unlock()
}

func (r *winRig) sent(method string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.calls {
		if strings.HasPrefix(c, method+" ") {
			out = append(out, strings.TrimPrefix(c, method+" "))
		}
	}
	return out
}

func (r *winRig) handlers(s *cdptest.Server) {
	frame := func() any {
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": r.loader, "url": r.url}}}
	}
	s.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return frame(), nil
	})
	s.Handle("Page.navigate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.note(m.Method, m.Params)
		var p struct {
			URL string `json:"url"`
		}
		json.Unmarshal(m.Params, &p)
		r.mu.Lock()
		defer r.mu.Unlock()
		if strings.Contains(p.URL, "nowhere") {
			return map[string]any{"frameId": "F1", "loaderId": "LX", "errorText": "net::ERR_NAME_NOT_RESOLVED"}, nil
		}
		r.loader, r.url = "L2", p.URL
		r.entries = append(r.entries[:r.cur+1], page.HistoryEntry{ID: 8 + r.cur, URL: p.URL})
		r.cur++
		return map[string]any{"frameId": "F1", "loaderId": "L2"}, nil
	})
	s.Handle("Page.getNavigationHistory", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return map[string]any{"currentIndex": r.cur, "entries": r.entries}, nil
	})
	s.Handle("Page.navigateToHistoryEntry", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.note(m.Method, m.Params)
		var p struct {
			EntryID int `json:"entryId"`
		}
		json.Unmarshal(m.Params, &p)
		r.mu.Lock()
		defer r.mu.Unlock()
		for i, e := range r.entries {
			if e.ID == p.EntryID {
				r.cur, r.url = i, e.URL
			}
		}
		return nil, nil
	})
	s.Handle("Page.reload", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.note(m.Method, m.Params)
		r.mu.Lock()
		r.loader = "L9"
		r.mu.Unlock()
		return nil, nil
	})
	s.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			Expression string `json:"expression"`
		}
		json.Unmarshal(m.Params, &p)
		r.mu.Lock()
		switch {
		case p.Expression == "document.readyState":
			defer r.mu.Unlock()
			return map[string]any{"result": map[string]any{"type": "string", "value": r.ready}}, nil
		case p.Expression == "location.href":
			defer r.mu.Unlock()
			return map[string]any{"result": map[string]any{"type": "string", "value": r.url}}, nil
		case strings.Contains(p.Expression, "winuse:wait-"):
			r.polls++
			n, ok, onPoll := r.polls, r.polls >= r.waitAfter, r.onPoll
			r.mu.Unlock()
			if onPoll != nil {
				onPoll(n)
			}
			return map[string]any{"result": map[string]any{"type": "boolean", "value": ok}}, nil
		}
		reply := r.evalReply
		r.mu.Unlock()
		r.note("eval", m.Params)
		return reply, nil
	})
	s.Handle("Page.handleJavaScriptDialog", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.note(m.Method, m.Params)
		r.mu.Lock()
		open := r.dialog
		r.dialog = false
		r.mu.Unlock()
		if !open {
			return nil, &cdptest.ReplyError{Code: -32602, Message: "No dialog is showing"}
		}
		c.Event(m.SessionID, "Page.javascriptDialogClosed", map[string]any{"result": true})
		return nil, nil
	})
	s.Handle("DOM.setFileInputFiles", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.note(m.Method+"@"+m.SessionID, m.Params)
		return nil, nil
	})
	s.Handle("Browser.setDownloadBehavior", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.note(m.Method, m.Params)
		return nil, nil
	})
}

// openDialog shows a dialog in the Tab once its session (and watch) exist.
func (r *winRig) openDialog(t *testing.T, typ, msg string) {
	t.Helper()
	if _, err := r.svc.Pages.(*livePages).reader.TabSession(context.Background(), r.conn, tDocs); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.dialog = true
	r.mu.Unlock()
	r.browser.Srv.Event(docsSession, "Page.javascriptDialogOpening", map[string]any{"type": typ, "message": msg})
	waitUntil(t, "the dialog", func() bool { return page.OpenDialog(r.conn.Client, docsSession) != nil })
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// passiveOnly fails unless the run used passive Esc scopes only, with no
// banner, activation, keys or clicks.
func (r *winRig) passiveOnly(t *testing.T) {
	t.Helper()
	for _, b := range r.esc.brackets() {
		if b == "arm" {
			t.Errorf("a swallowing scope: %v", r.esc.brackets())
		}
	}
	for _, s := range r.log() {
		if s == "activate" || strings.HasPrefix(s, "press ") {
			t.Errorf("a Headed step: %v", r.log())
		}
	}
	noForegroundCalls(t, r.browser.Methods())
}

func TestNavigate(t *testing.T) {
	r := newWinRig(t)
	out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "new.test/a"})
	if e != nil || out != "ok https://new.test/a\n" {
		t.Fatalf("navigate: %q %+v", out, e)
	}
	if got := r.sent("Page.navigate"); len(got) != 1 || !strings.Contains(got[0], `"url":"https://new.test/a"`) {
		t.Errorf("sent %v", got)
	}
	if len(r.cursor.labels) != 1 || r.cursor.labels[0] != `go to "new.test"` || r.cursor.targets[0] != addrRect || len(r.cursor.clicks) != 1 {
		t.Errorf("cursor %q %v clicks %v", r.cursor.labels, r.cursor.targets, r.cursor.clicks)
	}
	r.passiveOnly(t)

	// errorText: delivered (the Tab shows the error page), so no "nothing
	// was sent".
	_, e = r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://nowhere.test/"})
	if e == nil || e.Msg != "the browser could not open https://nowhere.test/: net::ERR_NAME_NOT_RESOLVED" || !strings.Contains(e.Hint, "error page") {
		t.Errorf("errorText: %+v", e)
	}

	// A page that keeps loading: ok, with the URL and a note.
	r.mu.Lock()
	r.ready = "interactive"
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	raw, _ := json.Marshal(protocol.NavigateArgs{Window: "301", URL: "https://slow.test/"})
	out, e = r.cmds["navigate"](ctx, raw)
	if e != nil || !strings.HasPrefix(out, "ok https://slow.test/ (the page was still interactive after ") || !strings.Contains(out, "winuse wait 301 load waits longer") {
		t.Errorf("slow: %q %+v", out, e)
	}
	// --wait none returns at once.
	if out, e = r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://fast.test/", Wait: "none"}); e != nil || out != "ok https://fast.test/\n" {
		t.Errorf("none: %q %+v", out, e)
	}
	if _, e = r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "x", Wait: "idle"}); e == nil || !strings.Contains(e.Msg, "load, domcontentloaded or none") {
		t.Errorf("bad wait: %+v", e)
	}
}

func TestHistoryEndsAndReload(t *testing.T) {
	r := newWinRig(t)
	_, e := r.run(t, "back", protocol.NavigateArgs{Window: "301"})
	if e == nil || e.Msg != "the Tab of window 301 has no earlier page in its history; nothing was sent" {
		t.Errorf("back at the start: %+v", e)
	}
	if _, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://two.test/"}); e != nil {
		t.Fatal(e.Error())
	}
	if out, e := r.run(t, "back", protocol.NavigateArgs{Window: "301"}); e != nil || out != "ok https://docs.test/\n" {
		t.Errorf("back: %q %+v", out, e)
	}
	if got := r.sent("Page.navigateToHistoryEntry"); len(got) != 1 || got[0] != `{"entryId":7}` {
		t.Errorf("history entry %v", got)
	}
	if out, e := r.run(t, "forward", protocol.NavigateArgs{Window: "301"}); e != nil || out != "ok https://two.test/\n" {
		t.Errorf("forward: %q %+v", out, e)
	}
	_, e = r.run(t, "forward", protocol.NavigateArgs{Window: "301"})
	if e == nil || e.Msg != "the Tab of window 301 has no later page in its history; nothing was sent" {
		t.Errorf("forward at the end: %+v", e)
	}
	if out, e := r.run(t, "reload", protocol.NavigateArgs{Window: "301"}); e != nil || out != "ok https://two.test/\n" || len(r.sent("Page.reload")) != 1 {
		t.Errorf("reload: %q %+v", out, e)
	}
	if _, e := r.run(t, "reload", protocol.NavigateArgs{Window: "301", URL: "x"}); e == nil || !strings.Contains(e.Msg, "reload takes no URL") {
		t.Errorf("reload with a URL: %+v", e)
	}
	r.passiveOnly(t)
}

func TestPageOnlyCommandsNeedABrowserConnection(t *testing.T) {
	r := newTabRig(t, nil, errors.New("nothing debuggable"), nil, nil)
	cases := map[string]any{
		"navigate": protocol.NavigateArgs{Window: "301", URL: "a.test"},
		"back":     protocol.NavigateArgs{Window: "301"},
		"dialog":   protocol.DialogArgs{Window: "301", Action: "accept"},
		"wait":     protocol.WaitArgs{Window: "301", For: "load"},
		"eval":     protocol.EvalArgs{Window: "301", Expression: "1"},
	}
	for cmd, args := range cases {
		_, e := r.run(t, cmd, args)
		if e == nil || e.Msg != "window 301 is a Chrome Window with no Browser Connection; nothing was sent" || e.Hint != "run winuse browser status chrome" {
			t.Errorf("%s: %+v", cmd, e)
		}
	}
	_, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "101", URL: "a.test"})
	if e == nil || !strings.HasPrefix(e.Msg, "window 101 is not a browser Window, so it has no Page") {
		t.Errorf("Notepad: %+v", e)
	}
}

// TestDialogWatchFailsOtherActionsAtOnce: while the Page shows a dialog,
// every page Action fails at once naming it and the dialog command; the
// dialog command answers it, and the Window State says so meanwhile.
func TestDialogWatchFailsOtherActionsAtOnce(t *testing.T) {
	r := newWinRig(t)
	r.openDialog(t, "confirm", "Leave this page?")
	want := `window 301 shows a confirm dialog: "Leave this page?", and its Page takes no input until it is answered; nothing was sent`
	for cmd, args := range map[string]any{
		"navigate": protocol.NavigateArgs{Window: "301", URL: "a.test"},
		"reload":   protocol.NavigateArgs{Window: "301"},
		"wait":     protocol.WaitArgs{Window: "301", For: "text", Value: "x"},
		"eval":     protocol.EvalArgs{Window: "301", Expression: "1"},
	} {
		start := time.Now()
		_, e := r.run(t, cmd, args)
		if e == nil || e.Msg != want || !strings.Contains(e.Hint, "winuse dialog 301 accept or winuse dialog 301 dismiss") || time.Since(start) > time.Second {
			t.Errorf("%s: %+v", cmd, e)
		}
	}
	if len(r.sent("Page.navigate")) != 0 || len(r.sent("eval")) != 0 {
		t.Errorf("sent %v", r.calls)
	}

	// The Window State's note.
	lp := r.svc.Pages.(*livePages)
	_, err := lp.ReadPage(context.Background(), tabTestWindows[0], "chrome")
	var u *page.Unavailable
	if !errors.As(err, &u) || page.FallbackNote(u.Reason) != `the Page is read through UI Automation because it shows a confirm dialog: "Leave this page?"; answer it with winuse dialog 301 accept or winuse dialog 301 dismiss.` {
		t.Errorf("note: %v", err)
	}

	// Text is only for a prompt; then the answer.
	_, e := r.run(t, "dialog", protocol.DialogArgs{Window: "301", Action: "accept", Text: strPtr("hi")})
	if e == nil || !strings.Contains(e.Msg, "only a prompt takes text; nothing was sent") {
		t.Errorf("text on a confirm: %+v", e)
	}
	if out, e := r.run(t, "dialog", protocol.DialogArgs{Window: "301", Action: "Dismiss"}); e != nil || out != okOutput {
		t.Fatalf("dismiss: %q %+v", out, e)
	}
	if got := r.sent("Page.handleJavaScriptDialog"); len(got) != 1 || got[0] != `{"accept":false}` {
		t.Errorf("answer %v", got)
	}
	if page.OpenDialog(r.conn.Client, docsSession) != nil {
		t.Error("the dialog still shows")
	}
	if _, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://after.test/"}); e != nil {
		t.Errorf("after the answer: %+v", e)
	}
	_, e = r.run(t, "dialog", protocol.DialogArgs{Window: "301", Action: "accept"})
	if e == nil || e.Msg != "window 301 shows no JavaScript dialog; nothing was sent" {
		t.Errorf("no dialog: %+v", e)
	}

	// A prompt takes its text.
	r.openDialog(t, "prompt", "Name?")
	if _, e := r.run(t, "dialog", protocol.DialogArgs{Window: "301", Action: "accept", Text: strPtr("Ada")}); e != nil {
		t.Fatal(e.Error())
	}
	if got := r.sent("Page.handleJavaScriptDialog"); len(got) != 3 || got[2] != `{"accept":true,"promptText":"Ada"}` {
		t.Errorf("prompt answer %v", got)
	}
	r.passiveOnly(t)
}

func strPtr(s string) *string { return &s }

// pageThread says whether a request needs the Page's main thread, which a
// JavaScript dialog holds: everything sent to a Tab session but Target.*
// and Page.handleJavaScriptDialog, which the browser answers itself.
func pageThread(m cdptest.Message) bool {
	return m.SessionID != "" && !strings.HasPrefix(m.Method, "Target.") && m.Method != "Page.handleJavaScriptDialog"
}

// blockOnDialog makes the fake answer, while r.dialog is set, only what a
// browser answers with a JavaScript dialog up (pageThread).
func (r *winRig) blockOnDialog() {
	r.browser.Srv.Stall(func(m cdptest.Message) bool {
		r.mu.Lock()
		open := r.dialog
		r.mu.Unlock()
		return open && pageThread(m)
	})
}

// runWithin runs a command with a request deadline, as the helper does,
// and says how long it took.
func runWithin(t *testing.T, cmds map[string]Command, cmd string, args any, d time.Duration) (string, *Error, time.Duration) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	start := time.Now()
	out, e := cmds[cmd](ctx, raw)
	return out, e, time.Since(start)
}

// TestDialogBlockedPageFailsFast: while a JavaScript dialog holds the
// Page's main thread (the fake answers only what the browser answers
// itself), every page Action fails within seconds with nothing sent,
// whether the watch saw the dialog open or missed it; the Window State
// says so, and the dialog command answers it.
func TestDialogBlockedPageFailsFast(t *testing.T) {
	// The probe's bound, shrunk: the Page Element test below runs the real
	// one.
	defer func(d time.Duration) { page.AnswerTimeout = d }(page.AnswerTimeout)
	page.AnswerTimeout = 300 * time.Millisecond
	for _, tc := range []struct {
		name           string
		seen, attached bool
	}{
		{"seen", true, true},
		{"missed", false, true},
		// The dialog opened before winuse attached (the helper started
		// since): the attach's own calls must not wait for the Page either.
		{"before-attach", false, false},
	} {
		seen := tc.seen
		t.Run(tc.name, func(t *testing.T) {
			r := newWinRig(t)
			r.blockOnDialog()
			// The Tab's session and watch exist from an earlier Action.
			if tc.attached {
				if _, err := r.svc.Pages.(*livePages).reader.TabSession(context.Background(), r.conn, tDocs); err != nil {
					t.Fatal(err)
				}
			}
			r.mu.Lock()
			r.dialog = true
			r.mu.Unlock()
			want := "shows an alert dialog"
			if seen {
				r.browser.Srv.Event(docsSession, "Page.javascriptDialogOpening", map[string]any{"type": "alert", "message": "Saved"})
				waitUntil(t, "the dialog", func() bool { return page.OpenDialog(r.conn.Client, docsSession) != nil })
			} else {
				want = "JavaScript dialog"
			}
			for _, c := range []struct {
				cmd  string
				args any
			}{
				{"eval", protocol.EvalArgs{Window: "301", Expression: "1"}},
				{"navigate", protocol.NavigateArgs{Window: "301", URL: "https://next.test/"}},
				{"reload", protocol.NavigateArgs{Window: "301"}},
				{"wait", protocol.WaitArgs{Window: "301", For: "text", Value: "x"}},
			} {
				_, e, took := runWithin(t, r.cmds, c.cmd, c.args, 10*time.Second)
				if e == nil || !strings.Contains(e.Msg, want) || !strings.HasSuffix(e.Msg, "nothing was sent") ||
					!strings.Contains(e.Hint, "winuse dialog 301 accept") || took > 5*time.Second {
					t.Errorf("%s took %s: %+v", c.cmd, took, e)
				}
			}
			if len(r.sent("Page.navigate")) != 0 || len(r.sent("Page.reload")) != 0 || len(r.sent("eval")) != 0 {
				t.Errorf("sent %v", r.calls)
			}

			// The Window State's note.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			start := time.Now()
			_, err := r.svc.Pages.(*livePages).ReadPage(ctx, tabTestWindows[0], "chrome")
			var u *page.Unavailable
			if took := time.Since(start); !errors.As(err, &u) || !strings.Contains(u.Reason, want) ||
				!strings.Contains(u.Reason, "winuse dialog 301 accept") || took > 5*time.Second {
				t.Errorf("the Window State took %s: %v", took, err)
			}

			// dialog answers it, seen or not.
			out, e, took := runWithin(t, r.cmds, "dialog", protocol.DialogArgs{Window: "301", Action: "accept"}, 10*time.Second)
			if e != nil || out != okOutput || took > 5*time.Second {
				t.Fatalf("dialog accept took %s: %q %+v", took, out, e)
			}
			if got := r.sent("Page.handleJavaScriptDialog"); len(got) != 1 || got[0] != `{"accept":true}` {
				t.Errorf("answer %v", got)
			}
			if out, e, _ := runWithin(t, r.cmds, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://after.test/"}, 10*time.Second); e != nil || out != "ok https://after.test/\n" {
				t.Errorf("after the answer: %q %+v", out, e)
			}
			_, e, _ = runWithin(t, r.cmds, "dialog", protocol.DialogArgs{Window: "301", Action: "dismiss"}, 10*time.Second)
			if e == nil || e.Msg != "window 301 shows no JavaScript dialog; nothing was sent" {
				t.Errorf("no dialog: %+v", e)
			}
		})
	}
}

func TestWait(t *testing.T) {
	r := newWinRig(t)
	r.waitAfter = 3
	if out, e := r.run(t, "wait", protocol.WaitArgs{Window: "301", For: "selector", Value: "#done"}); e != nil || out != okOutput || r.polls != 3 {
		t.Fatalf("wait: %q %+v after %d polls", out, e, r.polls)
	}
	if len(r.cursor.labels) != 0 {
		t.Errorf("wait showed the cursor: %v", r.cursor.labels)
	}
	r.waitAfter = 1 << 30
	_, e := r.run(t, "wait", protocol.WaitArgs{Window: "301", For: "selector", Value: "#never", TimeoutMS: 250})
	if e == nil || !strings.HasPrefix(e.Msg, `waiting for selector "#never" in window 301: the condition did not hold within `) {
		t.Errorf("timeout: %+v", e)
	}
	r.onPoll = func(n int) {
		r.esc.mu.Lock()
		r.esc.pressed = true
		r.esc.mu.Unlock()
	}
	_, e = r.run(t, "wait", protocol.WaitArgs{Window: "301", For: "url", Value: "*/done"})
	if e == nil || e.Exit != protocol.ExitEsc || !strings.Contains(e.Hint, "nothing was sent") {
		t.Errorf("Esc: %+v", e)
	}
	if _, e := r.run(t, "wait", protocol.WaitArgs{Window: "301", For: "idle"}); e == nil || !strings.Contains(e.Msg, "unknown wait") {
		t.Errorf("bad kind: %+v", e)
	}
	r.passiveOnly(t)
}

func TestEval(t *testing.T) {
	r := newWinRig(t)
	r.evalReply = map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{"title": "Docs", "n": 2}}}
	if out, e := r.run(t, "eval", protocol.EvalArgs{Window: "301", Expression: "({title: document.title, n: 2})"}); e != nil || out != `{"n":2,"title":"Docs"}`+"\n" {
		t.Errorf("value: %q %+v", out, e)
	}
	if len(r.cursor.labels) != 0 {
		t.Errorf("eval showed the cursor: %v", r.cursor.labels)
	}
	r.evalReply = map[string]any{"result": map[string]any{"type": "object"}, "exceptionDetails": map[string]any{"text": "Uncaught",
		"exception": map[string]any{"description": "ReferenceError: nope is not defined\n    at <anonymous>:1:1"}}}
	_, e := r.run(t, "eval", protocol.EvalArgs{Window: "301", Expression: "nope"})
	if e == nil || e.Msg != "the expression threw: ReferenceError: nope is not defined" {
		t.Errorf("exception: %+v", e)
	}
	r.evalReply = map[string]any{"result": map[string]any{"type": "string", "value": strings.Repeat("a", page.EvalLimit+5)}}
	out, e := r.run(t, "eval", protocol.EvalArgs{Window: "301", Expression: "big"})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if e != nil || len(lines) != 2 || len(lines[0]) != page.EvalLimit || lines[1] != fmt.Sprintf("note: the result has %d characters, and winuse printed the first %d; return less, for example with .slice(0, 1000)", page.EvalLimit+5, page.EvalLimit) {
		t.Errorf("cut: %d lines %+v", len(lines), e)
	}
	r.passiveOnly(t)
}

func TestUploadToAnInterceptedChooser(t *testing.T) {
	r := newWinRig(t)
	file := filepath.Join(t.TempDir(), "photo.png")
	os.WriteFile(file, []byte("png"), 0o644)

	_, e := r.run(t, "upload", protocol.UploadArgs{Window: "301", Files: []string{filepath.Join(t.TempDir(), "missing.png")}})
	if e == nil || !strings.Contains(e.Msg, "does not exist") || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Errorf("missing: %+v", e)
	}
	_, e = r.run(t, "upload", protocol.UploadArgs{Window: "301", Files: []string{file}})
	if e == nil || e.Msg != "window 301 has no file chooser waiting; nothing was sent" {
		t.Errorf("no chooser: %+v", e)
	}
	if len(r.sent("DOM.setFileInputFiles@"+docsSession)) != 0 {
		t.Fatalf("files sent: %v", r.calls)
	}
	// Attaching the Tab does not intercept choosers, and answering one sends
	// no interception either: only Actions that may open one hold it.
	methods := strings.Join(r.browser.Methods(), ",")
	if strings.Contains(methods, "Page.setInterceptFileChooserDialog") {
		t.Errorf("interception outside an Action: %s", methods)
	}

	r.browser.Srv.Event(docsSession, "Page.fileChooserOpened", map[string]any{"frameId": "F1", "mode": "selectSingle", "backendNodeId": 55})
	waitUntil(t, "the chooser", func() bool {
		ch, _ := page.WatchOf(r.conn.Client, docsSession).Chooser()
		return ch != nil
	})
	_, e = r.run(t, "upload", protocol.UploadArgs{Window: "301", Files: []string{file, file}})
	if e == nil || e.Msg != "the file chooser takes one file, and 2 were given; nothing was sent" {
		t.Errorf("two files: %+v", e)
	}
	if out, e := r.run(t, "upload", protocol.UploadArgs{Window: "301", Files: []string{file}}); e != nil || out != okOutput {
		t.Fatalf("upload: %q %+v", out, e)
	}
	want, _ := json.Marshal(map[string]any{"backendNodeId": 55, "files": []string{file}})
	if got := r.sent("DOM.setFileInputFiles@" + docsSession); len(got) != 1 || got[0] != string(want) {
		t.Errorf("files %v, want %s", got, want)
	}
	// The chooser was answered with interception off: DOM.setFileInputFiles
	// needs none.
	if strings.Contains(strings.Join(r.browser.Methods(), ","), "Page.setInterceptFileChooserDialog") {
		t.Error("the upload turned interception on")
	}
	if _, e := r.run(t, "upload", protocol.UploadArgs{Window: "301", Files: []string{file}}); e == nil || !strings.Contains(e.Msg, "no file chooser waiting") {
		t.Errorf("answered twice: %+v", e)
	}
	r.passiveOnly(t)
}

func TestWaitDownloadReportsThePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "winuse")
	old := pageDownloadDir
	pageDownloadDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { pageDownloadDir = old })
	r := newWinRig(t)
	if _, e := r.run(t, "wait", protocol.WaitArgs{Window: "301", For: "load"}); e != nil {
		t.Fatal(e.Error())
	}
	if got := r.sent("Browser.setDownloadBehavior"); len(got) != 1 || !strings.Contains(got[0], `"behavior":"allowAndName"`) {
		t.Fatalf("download behavior %v", got)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		os.WriteFile(filepath.Join(dir, "g-1"), []byte("pdf"), 0o644)
		r.browser.Srv.Event("", "Browser.downloadWillBegin", map[string]any{"guid": "g-1", "url": "https://docs.test/r.pdf", "suggestedFilename": "r.pdf"})
		r.browser.Srv.Event("", "Browser.downloadProgress", map[string]any{"guid": "g-1", "receivedBytes": 3, "totalBytes": 3, "state": "completed"})
	}()
	out, e := r.run(t, "wait", protocol.WaitArgs{Window: "301", For: "download"})
	if e != nil || out != "ok "+filepath.Join(dir, "r.pdf")+"\n" {
		t.Fatalf("download: %q %+v", out, e)
	}
	_, e = r.run(t, "wait", protocol.WaitArgs{Window: "301", For: "download", TimeoutMS: 100})
	if e == nil || !strings.Contains(e.Msg, "no download began within") || !strings.Contains(e.Hint, "start the download first") {
		t.Errorf("none: %+v", e)
	}
}

func TestWaitDownloadOnTheExtension(t *testing.T) {
	ext, conn := extensionBrowser(t)
	r := newTabRig(t, conn, nil, nil, nil)
	r.svc.Pages.(*livePages).reader.Settle = time.Millisecond
	_, e := r.run(t, "wait", protocol.WaitArgs{Window: "301", For: "download"})
	if e == nil || e.Msg != "downloads through the winuse extension go to the browser's own download folder, and winuse cannot track them: Browser.setDownloadBehavior needs a launched or cloned browser; the winuse extension cannot do it; nothing was sent" ||
		e.Hint != "run winuse browser launch chrome or winuse browser clone chrome, which take browser-level commands" {
		t.Errorf("extension: %+v", e)
	}
	for _, c := range ext.Commands() {
		if strings.HasPrefix(c.Method, "Browser.") {
			t.Errorf("sent %s to the extension", c.Method)
		}
	}
}

// pageFunctions extends 05's Runtime.callFunctionOn fake with this
// ticket's functions: extra answers by the function's marker, else 05's
// answers apply.
func (r *pageRig) pageFunctions(extra func(fn string, args []json.RawMessage) (any, bool)) {
	r.srv.Handle("Runtime.callFunctionOn", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			Fn   string `json:"functionDeclaration"`
			Args []struct {
				Value json.RawMessage `json:"value"`
			} `json:"arguments"`
		}
		json.Unmarshal(m.Params, &p)
		var args []json.RawMessage
		for _, a := range p.Args {
			args = append(args, a.Value)
		}
		if v, ok := extra(p.Fn, args); ok {
			return map[string]any{"result": map[string]any{"type": "object", "value": v}}, nil
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		var v any
		switch {
		case strings.Contains(p.Fn, "winuse:aim"):
			st := map[string]any{"state": "ok", "l": 10.0, "t": 20.0, "w": 50.0, "h": 10.0, "x": 35.0, "y": 25.0, "chain": true, "dpr": 1.5, "name": "page name"}
			for k, x := range r.aim {
				st[k] = x
			}
			v = st
		case strings.Contains(p.Fn, "isConnected"):
			v = true
		}
		return map[string]any{"result": map[string]any{"type": "object", "value": v}}, nil
	})
}

func TestPageDialogFailsElementActionsAtOnce(t *testing.T) {
	r := newPageRig(t)
	page.Watch(context.Background(), r.client, "S")
	t.Cleanup(func() { page.Unwatch(r.client, "S") })
	r.srv.Event("S", "Page.javascriptDialogOpening", map[string]any{"type": "alert", "message": "Saved"})
	waitUntil(t, "the dialog", func() bool { return page.OpenDialog(r.client, "S") != nil })
	want := `window 301 shows an alert dialog: "Saved", and its Page takes no input until it is answered; nothing was sent`
	for cmd, args := range map[string]any{
		"click":     protocol.ClickArgs{Window: "301", ElementIndex: idx(7)},
		"set-value": protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "x"},
		"hover":     protocol.HoverArgs{Window: "301", ElementIndex: idx(7)},
		"type-text": protocol.TypeTextArgs{Window: "301", Text: "x"},
	} {
		start := time.Now()
		_, e := r.run(t, cmd, args)
		if e == nil || e.Msg != want || time.Since(start) > time.Second {
			t.Errorf("%s: %+v", cmd, e)
		}
	}
	if len(r.sent()) != 0 {
		t.Errorf("sent %v", r.sent())
	}
	r.noForeground(t)
}

// TestPageElementActionsOnABlockedPageFailFast: with a dialog holding the
// Page's main thread (pageThread), click and set-value fail within seconds
// with nothing sent, whether the watch saw the dialog open or missed it.
func TestPageElementActionsOnABlockedPageFailFast(t *testing.T) {
	for _, seen := range []bool{true, false} {
		r := newPageRig(t)
		page.Watch(context.Background(), r.client, "S")
		t.Cleanup(func() { page.Unwatch(r.client, "S") })
		r.srv.Stall(pageThread)
		want := `shows an alert dialog: "Saved"`
		if seen {
			r.srv.Event("S", "Page.javascriptDialogOpening", map[string]any{"type": "alert", "message": "Saved"})
			waitUntil(t, "the dialog", func() bool { return page.OpenDialog(r.client, "S") != nil })
		} else {
			want = "JavaScript dialog"
		}
		for _, c := range []struct {
			cmd  string
			args any
		}{
			{"click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}},
			{"set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "x"}},
		} {
			_, e, took := runWithin(t, CommandsWith(r.svc), c.cmd, c.args, 10*time.Second)
			if e == nil || !strings.Contains(e.Msg, want) || !strings.HasSuffix(e.Msg, "nothing was sent") || took > 5*time.Second {
				t.Errorf("seen %v: %s took %s: %+v", seen, c.cmd, took, e)
			}
		}
		if len(r.sent()) != 0 {
			t.Errorf("seen %v: sent %v", seen, r.sent())
		}
		page.Unwatch(r.client, "S")
	}
}

func TestPageHoverAndDrag(t *testing.T) {
	r := newPageRig(t)
	if _, e := r.run(t, "hover", protocol.HoverArgs{Window: "301", ElementIndex: idx(7)}); e != nil {
		t.Fatal(e.Error())
	}
	if got := r.sent(); len(got) != 1 || got[0] != "dispatchMouseEvent mouseMoved none x=35 y=25" {
		t.Errorf("hover %v", got)
	}
	if r.cursor.labels[0] != `hover "Sign in"` {
		t.Errorf("label %q", r.cursor.labels)
	}
	// A UIA Element is hovered Headed (desktop-parity 13): refused without
	// --headed, nothing sent.
	_, e := r.run(t, "hover", protocol.HoverArgs{Window: "301", ElementIndex: idx(2)})
	if e == nil || e.Msg != "hover on an Element has no background route outside a Page: hovering moves the real pointer and leaves it there; nothing was sent" ||
		!strings.HasSuffix(e.Hint, "winuse --headed hover 301 2") {
		t.Errorf("UIA hover: %+v", e)
	}
	// The Batch's scope: only the Page Element forms are Headless.
	w := win.WindowInfo{ID: 301}
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	if !r.svc.stepHeadless(context.Background(), "hover", raw(protocol.HoverArgs{ElementIndex: idx(7)}), &w) ||
		!r.svc.stepHeadless(context.Background(), "drag", raw(protocol.DragArgs{FromIndex: idx(7), ToX: 3, Headed: true}), &w) {
		t.Error("a Page Element form is not Headless")
	}
	if r.svc.stepHeadless(context.Background(), "hover", raw(protocol.HoverArgs{ElementIndex: idx(2)}), &w) ||
		r.svc.stepHeadless(context.Background(), "drag", raw(protocol.DragArgs{FromIndex: idx(2), ToIndex: idx(7)}), &w) {
		t.Error("a UIA Element form is Headless")
	}

	r.set(func() { r.inputs = nil })
	if _, e := r.run(t, "drag", protocol.DragArgs{Window: "301", FromIndex: idx(7), ToIndex: idx(8)}); e != nil {
		t.Fatal(e.Error())
	}
	got := r.sent()
	if len(got) != page.DragSteps+3 || got[0] != "dispatchMouseEvent mouseMoved none x=35 y=25" ||
		got[1] != "dispatchMouseEvent mousePressed left x=35 y=25 clickCount=1" || got[len(got)-1] != "dispatchMouseEvent mouseReleased left x=35 y=25 clickCount=1" {
		t.Errorf("drag %v", got)
	}
	if !strings.Contains(strings.Join(r.srv.Methods(), ","), "Input.setInterceptDrags") {
		t.Error("HTML5 drags not intercepted")
	}
	// --drop-delay holds the Page's drag before its release (desktop-parity
	// 13).
	start := time.Now()
	if _, e := r.run(t, "drag", protocol.DragArgs{Window: "301", FromIndex: idx(7), ToIndex: idx(8), DropDelayMS: 150}); e != nil {
		t.Fatal(e.Error())
	}
	if took := time.Since(start); took < 150*time.Millisecond {
		t.Errorf("a drag with --drop-delay 150 took %s", took)
	}
	_, e = r.run(t, "drag", protocol.DragArgs{Window: "301", FromIndex: idx(7), ToIndex: idx(2)})
	if e == nil || !strings.HasPrefix(e.Msg, "a drag from element 7 ends in the same Page, and the target 2 is not in it") {
		t.Errorf("to a UIA Element: %+v", e)
	}
	r.noForeground(t)
}

func TestPageUploadToAFileInput(t *testing.T) {
	r := newPageRig(t)
	files := map[string]any{"file": true}
	r.pageFunctions(func(fn string, args []json.RawMessage) (any, bool) {
		if strings.Contains(fn, "winuse:file") {
			return files, true
		}
		return nil, false
	})
	var set []string
	r.srv.Handle("DOM.setFileInputFiles", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		set = append(set, m.SessionID+" "+string(m.Params))
		return nil, nil
	})
	file := filepath.Join(t.TempDir(), "cv.pdf")
	os.WriteFile(file, []byte("pdf"), 0o644)
	_, e := r.run(t, "upload", protocol.UploadArgs{Window: "301", ElementIndex: idx(8), Files: []string{file, file}})
	if e == nil || e.Msg != "element 8 takes one file, and 2 were given; nothing was sent" {
		t.Errorf("two files: %+v", e)
	}
	if out, e := r.run(t, "upload", protocol.UploadArgs{Window: "301", ElementIndex: idx(8), Files: []string{file}}); e != nil || out != okOutput {
		t.Fatalf("upload %q %+v", out, e)
	}
	want, _ := json.Marshal(map[string]any{"backendNodeId": 80, "files": []string{file}})
	if len(set) != 1 || set[0] != "S "+string(want) {
		t.Errorf("set %v", set)
	}
	if r.cursor.labels[len(r.cursor.labels)-1] != `upload "Email"` {
		t.Errorf("label %q", r.cursor.labels)
	}
	files["file"] = false
	_, e = r.run(t, "upload", protocol.UploadArgs{Window: "301", ElementIndex: idx(8), Files: []string{file}})
	if e == nil || e.Msg != "element 8 is not a file input, and no file chooser is waiting; nothing was sent" {
		t.Errorf("not a file input: %+v", e)
	}
	if len(r.sent()) != 0 {
		t.Errorf("input sent %v", r.sent())
	}
}

func TestPageSetValueChoices(t *testing.T) {
	r := newPageRig(t)
	var mu sync.Mutex
	choice := map[string]any{"kind": "select", "options": []map[string]any{{"label": "Red", "value": "r", "selected": true}, {"label": "Green", "value": "g"}}}
	picked := -1
	r.pageFunctions(func(fn string, args []json.RawMessage) (any, bool) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.Contains(fn, "winuse:choice"):
			if choice["kind"] == "check" {
				c := map[string]any{"kind": "check", "checked": choice["checked"]}
				if choice["flips"] == true && strings.Contains(strings.Join(r.sent(), ","), "mouseReleased") {
					c["checked"] = true
				}
				return c, true
			}
			if picked >= 0 {
				return map[string]any{"kind": "select", "options": []map[string]any{{"label": "Red", "value": "r", "selected": picked == 0}, {"label": "Green", "value": "g", "selected": picked == 1}}}, true
			}
			return choice, true
		case strings.Contains(fn, "winuse:pick"):
			json.Unmarshal(args[0], &picked)
			return true, true
		}
		return nil, false
	})
	if out, e := r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "g"}); e != nil || out != okOutput || picked != 1 {
		t.Fatalf("select by value: %q %+v picked %d", out, e, picked)
	}
	if r.cursor.labels[0] != `select "Email"` || len(r.sent()) != 0 {
		t.Errorf("label %q, input %v", r.cursor.labels, r.sent())
	}
	picked = -1
	_, e := r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "Blue"})
	if e == nil || e.Msg != `cannot set element 8: the list has no option "Blue"; its options are "Red", "Green"; nothing was sent` {
		t.Errorf("no option: %+v", e)
	}

	// A check box: a trusted click, read back.
	mu.Lock()
	choice = map[string]any{"kind": "check", "checked": false, "flips": true}
	mu.Unlock()
	if out, e := r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "on"}); e != nil || out != okOutput {
		t.Fatalf("check: %q %+v", out, e)
	}
	if got := strings.Join(r.sent(), ","); !strings.Contains(got, "mousePressed left x=35 y=25") {
		t.Errorf("check sent %s", got)
	}
	r.set(func() { r.inputs = nil })
	mu.Lock()
	choice = map[string]any{"kind": "check", "checked": false, "flips": false}
	mu.Unlock()
	_, e = r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "checked"})
	if e == nil || !strings.HasPrefix(e.Msg, "the click was delivered, but element 8 is not on afterwards") || !strings.Contains(e.Hint, "do not repeat it") {
		t.Errorf("unchanged: %+v", e)
	}
	r.set(func() { r.inputs = nil })
	if out, e := r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "off"}); e != nil || out != okOutput || len(r.sent()) != 0 {
		t.Errorf("already off: %q %+v %v", out, e, r.sent())
	}
	mu.Lock()
	choice = map[string]any{"kind": "radio", "checked": true}
	mu.Unlock()
	_, e = r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "off"})
	if e == nil || !strings.Contains(e.Msg, "is a radio button") || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Errorf("radio off: %+v", e)
	}
	r.noForeground(t)
}

// TestPageRightClickKeepsTheContextMenuClosed: the guard goes in before
// the click and comes out after it; a page without a menu of its own gets
// the browser's menu cancelled, and the output says so.
func TestPageRightClickKeepsTheContextMenuClosed(t *testing.T) {
	for _, own := range []bool{false, true} {
		r := newPageRig(t)
		r.pageFunctions(func(fn string, args []json.RawMessage) (any, bool) {
			switch {
			case strings.Contains(fn, "winuse:ctxguard"):
				r.record("guard")
				return true, true
			case strings.Contains(fn, "winuse:ctxresult"):
				r.record("result")
				return map[string]any{"fired": true, "page": own}, true
			}
			return nil, false
		})
		out, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7), MouseButton: "right"})
		want := "ok\nNote: the page has no menu of its own there, so winuse kept the browser's context menu from opening\n"
		if own {
			want = okOutput
		}
		if e != nil || out != want {
			t.Errorf("own menu %v: %q %+v", own, out, e)
		}
		got := r.sent()
		if len(got) != 5 || got[0] != "guard" || !strings.HasPrefix(got[2], "dispatchMouseEvent mousePressed right") || got[4] != "result" {
			t.Errorf("order %v", got)
		}
		r.noForeground(t)
	}
}

// TestPageClickThatOpensAFileChooser: the browser holds the chooser back
// and the click says how to answer it.
func TestPageClickThatOpensAFileChooser(t *testing.T) {
	r := newPageRig(t)
	page.Watch(context.Background(), r.client, "S")
	t.Cleanup(func() { page.Unwatch(r.client, "S") })
	r.srv.Handle("Input.dispatchMouseEvent", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		if strings.Contains(string(m.Params), "mouseReleased") {
			c.Event("S", "Page.fileChooserOpened", map[string]any{"frameId": "F1", "mode": "selectSingle", "backendNodeId": 70})
		}
		return nil, nil
	})
	out, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e != nil || out != "ok\nNote: the click opened a file chooser, which the browser held back so nothing opened on the screen: answer it with winuse upload 301 <file>...\n" {
		t.Errorf("click: %q %+v", out, e)
	}
}

// TestPageOnlyStepsAreHeadless: the Batch's scope.
func TestPageOnlyStepsAreHeadless(t *testing.T) {
	r := newWinRig(t)
	w := tabTestWindows[0]
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	for _, kind := range []string{"navigate", "back", "forward", "reload", "upload", "dialog", "wait", "eval"} {
		if !r.svc.stepHeadless(context.Background(), kind, raw(map[string]any{}), &w) {
			t.Errorf("%s is not Headless", kind)
		}
	}
	if r.svc.stepHeadless(context.Background(), "hover", raw(protocol.HoverArgs{X: 1, Y: 2}), &w) || r.svc.stepHeadless(context.Background(), "drag", raw(protocol.DragArgs{ToX: 3}), &w) {
		t.Error("a point form is Headless")
	}
	// No Page Element here: the Element forms are Headed (desktop-parity
	// 13; TestPageHoverAndDrag pins the Page forms).
	if r.svc.stepHeadless(context.Background(), "hover", raw(protocol.HoverArgs{ElementIndex: idx(4)}), &w) || r.svc.stepHeadless(context.Background(), "drag", raw(protocol.DragArgs{FromRef: "name:a", ToX: 3}), &w) {
		t.Error("a UIA Element form is Headless")
	}
	r.waitAfter = 1
	out, e := r.run(t, "batch", protocol.BatchArgs{Window: "301", Steps: []protocol.Step{
		stepOf("navigate", protocol.NavigateArgs{URL: "https://b.test/"}),
		stepOf("wait", protocol.WaitArgs{For: "text", Value: "Hello"}),
		stepOf("eval", protocol.EvalArgs{Expression: "1"}),
	}})
	// Each step prints its own output (desktop-parity ticket 20): what
	// navigate and eval print comes back.
	if e != nil || out != "Action 1 (navigate): ok https://b.test/\nAction 2 (wait): ok\nAction 3 (eval): null\n" {
		t.Fatalf("batch: %q %+v", out, e)
	}
	r.passiveOnly(t)
}
