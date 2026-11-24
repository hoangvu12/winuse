//go:build e2e

// The exception listener (bladebro-ideas ticket 30).
//
// TestExceptionListener starts Chrome or Edge headless on a throwaway
// profile (startIsolatedBrowser) and drives internal/page in process, as
// the helper does: page.TabLog.Capture installs the main-world listener and
// the isolated buffer, and PullExceptions reads them, never with
// Runtime.enable.
//
//   - throws: a page that throws synchronously (in a script file, so the
//     record names it and a line), in a timer, and in a promise nobody
//     catches. All three join the Tab's ring as source exception with a
//     stack; the page counts as covered.
//   - detection: one fixture in a Tab that captures and in one that does
//     not. The page patches dispatchEvent, addEventListener, CustomEvent,
//     JSON.stringify and Object.prototype getters after it loads, then
//     throws and rejects: none of its patches sees a call, window.onerror
//     stays null, the window has the same own properties as in the Tab
//     without capture, the page's own handlers run, and defaultPrevented is
//     what it is without capture. The capturing Tab still records both.
//   - loaded before: a page loaded before capture is not covered until it
//     loads again.
//
// User-run only (see the package comment); an agent must never start it.

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
)

const exceptionsBlank = `<!doctype html><title>e2e blank</title><p>blank</p>`

// The throwing script: line 2 throws while the page loads.
const exceptionsApp = "// e2e-app.js\nfunction render() { throw new Error('e2e-sync'); }\nrender();\n"

const exceptionsThrows = `<!doctype html><title>e2e throws</title>
<script src="/e2e-app.js"></script>
<script>
setTimeout(function tick() { throw new TypeError('e2e-timer'); }, 20);
Promise.reject(new Error('e2e-rejection'));
</script><p>throws</p>`

// The detection fixture: it reads its window first, then patches what a
// listener would call, throws and rejects, and stores what it saw as JSON
// in window.__detResult (its own global, read by the test).
const exceptionsDetect = `<!doctype html><title>e2e detect</title><script>
(() => {
  const names = Object.getOwnPropertyNames(window).sort().join(',');
  const onerrorNull = window.onerror === null;
  const seen = [];
  window.addEventListener('error', (e) => seen.push({m: e.message, dp: e.defaultPrevented}));
  window.addEventListener('unhandledrejection', (e) => seen.push({m: String(e.reason && e.reason.message), dp: e.defaultPrevented}));
  const calls = [];
  const stringify = JSON.stringify;
  const dispatch = EventTarget.prototype.dispatchEvent;
  EventTarget.prototype.dispatchEvent = function (ev) { calls.push('dispatchEvent ' + ev.type); return dispatch.call(this, ev); };
  const add = EventTarget.prototype.addEventListener;
  EventTarget.prototype.addEventListener = function (type, ...rest) { calls.push('addEventListener ' + type); return add.call(this, type, ...rest); };
  const Custom = window.CustomEvent;
  window.CustomEvent = function (...a) { calls.push('CustomEvent'); return new Custom(...a); };
  JSON.stringify = function (...a) { calls.push('JSON.stringify'); return stringify.apply(this, a); };
  Object.defineProperty(Object.prototype, 'detail', {get() { calls.push('detail getter'); return undefined; }, configurable: true});
  Object.defineProperty(Object.prototype, 'toJSON', {get() { calls.push('toJSON getter'); return undefined; }, configurable: true});
  setTimeout(() => { throw new Error('e2e-detect'); }, 0);
  Promise.reject(new Error('e2e-detect-rejection'));
  setTimeout(() => {
    delete Object.prototype.detail;
    delete Object.prototype.toJSON;
    window.__detResult = stringify({names, onerrorNull, onerrorAfter: window.onerror === null, seen, calls});
  }, 400);
})();
</script><p>detect</p>`

type detection struct {
	Names        string `json:"names"`
	OnerrorNull  bool   `json:"onerrorNull"`
	OnerrorAfter bool   `json:"onerrorAfter"`
	Seen         []struct {
		M  string `json:"m"`
		DP bool   `json:"dp"`
	} `json:"seen"`
	Calls []string `json:"calls"`
}

func TestExceptionListener(t *testing.T) {
	optIn(t)
	sys := browser.Live()
	var b browser.Browser
	found := false
	for _, name := range []string{"chrome", "edge"} {
		if b, found = installedBrowsers(sys)[name]; found {
			break
		}
	}
	if !found {
		t.Skip("neither Chrome nor Edge is installed; nothing was run")
	}
	mux := http.NewServeMux()
	serve := func(path, ctype, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", ctype)
			fmt.Fprint(w, body)
		})
	}
	serve("/e2e-blank", "text/html; charset=utf-8", exceptionsBlank)
	serve("/e2e-app.js", "text/javascript", exceptionsApp)
	serve("/e2e-throws", "text/html; charset=utf-8", exceptionsThrows)
	serve("/e2e-detect", "text/html; charset=utf-8", exceptionsDetect)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	port := freePort(t)
	pid := startIsolatedBrowser(t, b.Path, "--headless=new", fmt.Sprintf("--remote-debugging-port=%d", port), "about:blank")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var ws string
	var err error
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if ws, err = sys.VersionWS(ctx, port); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("/json/version on port %d: %v", port, err)
	}
	tr, err := cdp.DialWS(ctx, ws, 20*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", ws, err)
	}
	c := cdp.NewClient(tr)
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
		c.Call(cctx, "Browser.close", nil)
		ccancel()
		c.Close()
		for deadline := time.Now().Add(10 * time.Second); pidAlive(pid) && time.Now().Before(deadline); {
			time.Sleep(100 * time.Millisecond)
		}
		time.Sleep(time.Second)
	})
	conn := &browser.Connection{Browser: b.Name, Mode: browser.ModeLaunch, WSURL: ws, Port: port, Client: c}

	// tab opens a Tab on the blank page and attaches it.
	tab := func(t *testing.T) (string, string) {
		t.Helper()
		id := openAnsweringTab(t, ctx, conn, srv.URL+"/e2e-blank")
		sess, err := conn.AttachPage(ctx, id)
		if err != nil {
			t.Fatalf("attaching the tab: %v", err)
		}
		return id, sess
	}
	// load navigates and waits for the load event.
	load := func(t *testing.T, sess, path string) {
		t.Helper()
		loader, err := page.Navigate(ctx, c, sess, srv.URL+path)
		if err != nil {
			t.Fatalf("navigate %s: %v", path, err)
		}
		committed := func(ctx context.Context) bool {
			l, _, err := page.MainFrame(ctx, c, sess)
			return err == nil && l == loader
		}
		wctx, wcancel := context.WithTimeout(ctx, 15*time.Second)
		defer wcancel()
		if err := page.WaitLoad(wctx, c, sess, page.LoadComplete, committed, nil); err != nil {
			t.Fatalf("waiting for %s to load: %v", path, err)
		}
	}
	// capture turns capture on and waits until the scripts are in.
	capture := func(t *testing.T, id, sess string) *page.TabLog {
		t.Helper()
		l := page.WatchLog(ctx, c, sess, id)
		l.Capture(ctx, sess)
		if _, ok := l.PullExceptions(ctx, sess); !ok {
			t.Fatal("the listener did not install: the browser gave no script identifiers, or the buffer could not be read")
		}
		return l
	}
	exceptions := func(l *page.TabLog) []page.LogEntry {
		var out []page.LogEntry
		for _, e := range l.Entries() {
			if e.Source == "exception" {
				out = append(out, e)
			}
		}
		return out
	}
	find := func(list []page.LogEntry, text string) *page.LogEntry {
		for i := range list {
			if strings.Contains(list[i].Text, text) {
				return &list[i]
			}
		}
		return nil
	}

	t.Run("throws", func(t *testing.T) {
		id, sess := tab(t)
		l := capture(t, id, sess)
		load(t, sess, "/e2e-throws")
		time.Sleep(500 * time.Millisecond) // the timer
		covered, ok := l.PullExceptions(ctx, sess)
		if !ok || !covered {
			t.Fatalf("pull: covered %v, ok %v", covered, ok)
		}
		got := exceptions(l)
		for _, e := range got {
			t.Logf("MEASURED: %s %s:%d\n%s", e.Text, e.URL, e.Line, e.Stack)
		}
		thrown := find(got, "e2e-sync")
		if thrown == nil || !strings.HasSuffix(thrown.URL, "/e2e-app.js") || thrown.Line != 2 || !strings.Contains(thrown.Stack, "at render") || thrown.Level != "error" {
			t.Errorf("the synchronous throw: %+v", thrown)
		}
		if timer := find(got, "TypeError: e2e-timer"); timer == nil || !strings.Contains(timer.Stack, "tick") {
			t.Errorf("the throw in a timer: %+v", timer)
		}
		if rej := find(got, "e2e-rejection"); rej == nil || !strings.HasPrefix(rej.Text, "Uncaught (in promise) Error: e2e-rejection") || rej.Stack == "" || rej.URL == "" {
			t.Errorf("the unhandled rejection: %+v", rej)
		}
		if len(got) != 3 {
			t.Errorf("%d exceptions, want 3", len(got))
		}
		// Read once: a second pull adds nothing.
		l.PullExceptions(ctx, sess)
		if n := len(exceptions(l)); n != 3 {
			t.Errorf("after a second pull: %d exceptions", n)
		}
	})

	t.Run("detection", func(t *testing.T) {
		_, plainSess := tab(t)
		capID, capSess := tab(t)
		l := capture(t, capID, capSess)
		load(t, plainSess, "/e2e-detect")
		load(t, capSess, "/e2e-detect")
		read := func(sess string) detection {
			t.Helper()
			var d detection
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
				res, err := c.CallSession(ctx, sess, "Runtime.evaluate", map[string]any{"expression": "window.__detResult || ''", "returnByValue": true})
				if err != nil {
					t.Fatalf("reading the fixture: %v", err)
				}
				var ev struct {
					Result struct {
						Value string `json:"value"`
					} `json:"result"`
				}
				json.Unmarshal(res, &ev)
				if ev.Result.Value != "" {
					if err := json.Unmarshal([]byte(ev.Result.Value), &d); err != nil {
						t.Fatalf("the fixture's result: %v", err)
					}
					return d
				}
			}
			t.Fatal("the fixture never stored its result")
			return d
		}
		plain, captured := read(plainSess), read(capSess)
		if !captured.OnerrorNull || !captured.OnerrorAfter {
			t.Errorf("window.onerror was set: null before %v, after %v", captured.OnerrorNull, captured.OnerrorAfter)
		}
		if captured.Names != plain.Names {
			t.Errorf("the capturing Tab's window has other own properties:\n%s\nwithout capture:\n%s", captured.Names, plain.Names)
		}
		if len(captured.Calls) != 0 {
			t.Errorf("the page's patches saw the listener: %v", captured.Calls)
		}
		pj, _ := json.Marshal(plain.Seen)
		cj, _ := json.Marshal(captured.Seen)
		if string(pj) != string(cj) || len(captured.Seen) != 2 {
			t.Errorf("the page's own handlers saw %s with capture, %s without", cj, pj)
		}
		for _, s := range captured.Seen {
			if s.DP {
				t.Errorf("defaultPrevented is set: %+v", s)
			}
		}
		if _, ok := l.PullExceptions(ctx, capSess); !ok {
			t.Fatal("the buffer could not be read")
		}
		got := exceptions(l)
		if find(got, "e2e-detect-rejection") == nil || find(got, "Error: e2e-detect") == nil {
			t.Errorf("the capturing Tab recorded %+v", got)
		}
	})

	t.Run("loaded before", func(t *testing.T) {
		id, sess := tab(t)
		load(t, sess, "/e2e-throws")
		l := page.WatchLog(ctx, c, sess, id)
		l.Capture(ctx, sess)
		if covered, ok := l.PullExceptions(ctx, sess); !ok || covered {
			t.Errorf("a page loaded before capture: covered %v, ok %v", covered, ok)
		}
		if n := len(exceptions(l)); n != 0 {
			t.Errorf("%d exceptions from a page loaded before capture", n)
		}
		load(t, sess, "/e2e-throws")
		time.Sleep(500 * time.Millisecond)
		if covered, ok := l.PullExceptions(ctx, sess); !ok || !covered {
			t.Errorf("after loading it again: covered %v, ok %v", covered, ok)
		}
		if n := len(exceptions(l)); n != 3 {
			t.Errorf("%d exceptions after loading it again, want 3", n)
		}
	})
}
