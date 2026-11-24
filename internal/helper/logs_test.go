package helper

// Failed requests and CSP errors (.scratch/bladebro-ideas ticket 29) over
// the fake launched Chrome (winRig) and 05's Page (pageRig): `winuse logs`
// with the entries Log.enable replays, --clear, --json, the cut and --out;
// the note after a Page Action that saw new error entries, with same-site
// paths and at most 3 named. JavaScript exceptions (ticket 30): on by
// default in a launched browser and a Tab tab new opened, after --capture
// in the user's; the listener goes in before navigate, and an exception
// becomes a note. Log is the only domain turned on: never Runtime.enable
// or Network.enable. No browser runs.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
)

const status404 = "Failed to load resource: the server responded with a status of 404 (Not Found)"

func httpFailure(code int, text string) string {
	return fmt.Sprintf("Failed to load resource: the server responded with a status of %d (%s)", code, text)
}

// logParams is a Log.entryAdded event's params, stamped at t.
func logParams(t time.Time, source, level, text, url string) map[string]any {
	e := map[string]any{"source": source, "level": level, "text": text, "timestamp": float64(t.UnixNano()) / 1e6}
	if url != "" {
		e["url"] = url
	}
	return map[string]any{"entry": e}
}

// replayLog makes Log.enable replay entries first, as Chrome does with
// what the Page logged before the domain was on.
func replayLog(srv *cdptest.Server, entries ...map[string]any) {
	srv.Handle("Log.enable", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		for _, e := range entries {
			c.Event(m.SessionID, "Log.entryAdded", e)
		}
		return nil, nil
	})
}

func onlyLogDomain(t *testing.T, srv *cdptest.Server) {
	t.Helper()
	logOn := false
	for _, m := range srv.Received() {
		switch {
		case m.Method == "Runtime.enable" || strings.HasPrefix(m.Method, "Network."):
			t.Errorf("%s was sent", m.Method)
		case m.Method == "Log.enable":
			logOn = true
		}
	}
	if !logOn {
		t.Error("Log.enable was never sent")
	}
}

func clock(t time.Time) string { return t.Local().Format("15:04:05.000") }

func TestLogsPrintsWhatThePageLoggedBeforeWinuseAttached(t *testing.T) {
	r := newWinRig(t)
	early := time.Now().Add(-time.Minute)
	csp := `Refused to load the image 'https://x.test/a.png' because it violates the following Content Security Policy directive: "img-src 'self'".`
	replayLog(r.browser.Srv,
		logParams(early, "network", "error", status404, "https://docs.test/api/save"),
		logParams(early.Add(time.Second), "security", "error", csp, ""))
	out, e := r.run(t, "logs", protocol.LogsArgs{Window: "301"})
	if e != nil {
		t.Fatalf("logs: %+v", e)
	}
	// A launched browser's Tab captures its exceptions (ticket 30).
	want := "Page: https://docs.test/ - 2 log entries, newest last (failed requests, CSP or security errors and JavaScript exceptions; winuse keeps the last 50 per Tab)\n" +
		clock(early) + " network error " + status404 + " https://docs.test/api/save\n" +
		clock(early.Add(time.Second)) + " security error " + csp + "\n"
	if out != want {
		t.Errorf("logs printed\n%s\nwant\n%s", out, want)
	}
	onlyLogDomain(t, r.browser.Srv)
	if len(r.cursor.labels) != 0 {
		t.Errorf("logs showed the Agent Cursor: %v", r.cursor.labels)
	}
	r.passiveOnly(t)
}

func TestLogsWithNothingLogged(t *testing.T) {
	r := newWinRig(t)
	out, e := r.run(t, "logs", protocol.LogsArgs{Window: "301"})
	want := "Page: https://docs.test/ - no log entries (failed requests, CSP or security errors and JavaScript exceptions show here)\n"
	if e != nil || out != want {
		t.Errorf("logs: %q %+v, want %q", out, e, want)
	}
	// In the user's own browser exceptions need --capture.
	r = newWinRig(t)
	r.conn.Mode = browser.ModeToggle
	out, e = r.run(t, "logs", protocol.LogsArgs{Window: "301"})
	want = "Page: https://docs.test/ - no log entries (failed requests and CSP or security errors show here; --capture adds JavaScript exceptions)\n"
	if e != nil || out != want {
		t.Errorf("logs in the user's browser: %q %+v, want %q", out, e, want)
	}
}

// addScriptIDs answers Page.addScriptToEvaluateOnNewDocument with numbered
// identifiers, as Chrome does.
func addScriptIDs(srv *cdptest.Server) {
	var mu sync.Mutex
	n := 0
	srv.Handle("Page.addScriptToEvaluateOnNewDocument", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return map[string]any{"identifier": strconv.Itoa(n)}, nil
	})
}

// scriptsSent are the addScriptToEvaluateOnNewDocument requests, each
// with the session it went to and whether it names a world.
func scriptsSent(srv *cdptest.Server) []string {
	var out []string
	for _, m := range srv.Received() {
		if m.Method == "Page.addScriptToEvaluateOnNewDocument" {
			var p map[string]any
			json.Unmarshal(m.Params, &p)
			world, _ := p["worldName"].(string)
			out = append(out, m.SessionID+" "+world)
		}
	}
	return out
}

// exceptionBuffer is the isolated world's answer to winuse's read: whether
// the document has the listener, and its records.
func exceptionBuffer(covered bool, records ...map[string]any) any {
	if records == nil {
		records = []map[string]any{}
	}
	data, _ := json.Marshal(map[string]any{"c": covered, "d": 0, "r": records})
	return map[string]any{"result": map[string]any{"type": "string", "value": string(data)}}
}

// TestLogsCaptureInTheUsersTab: in the user's own browser, logs --capture
// installs the listener (main world, and the isolated buffer), says it
// covers pages loaded from now on and offers reload; a later Action's
// exception becomes a note, and logs lists it with its stack.
func TestLogsCaptureInTheUsersTab(t *testing.T) {
	r := newWinRig(t)
	r.conn.Mode = browser.ModeToggle
	addScriptIDs(r.browser.Srv)
	if out, e := r.run(t, "logs", protocol.LogsArgs{Window: "301"}); e != nil || strings.Contains(out, "capturing") {
		t.Fatalf("logs: %q %+v", out, e)
	}
	if got := scriptsSent(r.browser.Srv); len(got) != 0 {
		t.Fatalf("the user's Tab got the listener unasked: %v", got)
	}

	r.evalReply = exceptionBuffer(false)
	out, e := r.run(t, "logs", protocol.LogsArgs{Window: "301", Capture: true})
	want := "Page: https://docs.test/ - no log entries (failed requests, CSP or security errors and JavaScript exceptions show here)\n" +
		"(capturing JavaScript exceptions in this Tab from now on, while it is open: they show for pages it loads from now on; winuse reload 301 covers this one)\n"
	if e != nil || out != want {
		t.Errorf("logs --capture: %+v\n%q\nwant\n%q", e, out, want)
	}
	if got := strings.Join(scriptsSent(r.browser.Srv), ","); got != docsSession+" winuse-exceptions,"+docsSession+" " {
		t.Errorf("scripts sent: %s; want the isolated buffer, then the main-world listener", got)
	}

	at := time.Now().Add(time.Second).Truncate(time.Millisecond)
	r.evalReply = exceptionBuffer(true, map[string]any{"t": float64(at.UnixMilli()), "m": "Uncaught TypeError: x is undefined",
		"u": "https://docs.test/static/app.js", "l": 120,
		"s": "TypeError: x is undefined\n    at render (https://docs.test/static/app.js:120:7)"})
	out, e = r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://docs.test/next"})
	if want := "ok https://docs.test/next\nNote: the Page threw: TypeError: x is undefined (app.js:120)\n"; e != nil || out != want {
		t.Errorf("navigate: %+v\n%q\nwant\n%q", e, out, want)
	}
	out, e = r.run(t, "logs", protocol.LogsArgs{Window: "301"})
	header := " - 1 log entry, newest last (failed requests, CSP or security errors and JavaScript exceptions; winuse keeps the last 50 per Tab)\n"
	entry := clock(at) + " exception error Uncaught TypeError: x is undefined https://docs.test/static/app.js:120\n" +
		"    at render (https://docs.test/static/app.js:120:7)\n"
	if e != nil || !strings.HasSuffix(out, header+entry) || strings.Count(out, "\n") != 3 {
		t.Errorf("logs: %+v\n%q\nwant the header ending %q, then\n%q", e, out, header, entry)
	}
	// Installed once: the Tab keeps it.
	if n := len(scriptsSent(r.browser.Srv)); n != 2 {
		t.Errorf("%d scripts sent, want 2", n)
	}
	onlyLogDomain(t, r.browser.Srv)
}

// A launched browser's Tab captures by default: the listener goes in
// before navigate sends the Tab to the next page, so that page has it.
func TestALaunchedBrowserCapturesBeforeNavigate(t *testing.T) {
	r := newWinRig(t)
	addScriptIDs(r.browser.Srv)
	r.evalReply = exceptionBuffer(true)
	if out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://docs.test/next"}); e != nil || out != "ok https://docs.test/next\n" {
		t.Fatalf("navigate: %q %+v", out, e)
	}
	scripts, navigated := 0, false
	for _, m := range r.browser.Srv.Received() {
		switch m.Method {
		case "Page.addScriptToEvaluateOnNewDocument":
			if navigated {
				t.Error("the listener went in after Page.navigate")
			}
			scripts++
		case "Page.navigate":
			navigated = true
		}
	}
	if scripts != 2 || !navigated {
		t.Errorf("%d scripts, navigated %v", scripts, navigated)
	}
	out, e := r.run(t, "logs", protocol.LogsArgs{Window: "301", JSON: true})
	if e != nil || !strings.Contains(out, `"capturing":true`) {
		t.Errorf("logs --json: %q %+v", out, e)
	}
	onlyLogDomain(t, r.browser.Srv)
}

// A Tab tab new opened is the agent's: it captures; the user's Tabs next
// to it do not.
func TestTabNewCapturesInItsTab(t *testing.T) {
	_, conn := extensionBrowser(t)
	r := newTabRig(t, conn, nil, nil, nil)
	if out, e := r.run(t, "tab-new", protocol.TabArgs{Window: "302", URL: "https://a.test/"}); e != nil || !strings.HasPrefix(out, "ok newtab5") {
		t.Fatalf("tab new = %q, %+v", out, e)
	}
	if !page.TabLogOf(conn.Client, "NEWTAB5").Capturing() {
		t.Error("the new Tab does not capture")
	}
	out, _ := r.run(t, "tab-list", protocol.TabArgs{Window: "302"})
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		id := strings.Fields(strings.TrimLeft(line, "* "))[0]
		if id != "newtab5" && page.TabLogOf(conn.Client, strings.ToUpper(id)).Capturing() {
			t.Errorf("the user's Tab %s captures", id)
		}
	}
}

func TestLogNoteForExceptions(t *testing.T) {
	at := time.Now()
	thrown := page.LogEntry{Time: at, Source: "exception", Level: "error", Text: "Uncaught TypeError: x is undefined",
		URL: "https://shop.test/static/app.js", Line: 120}
	got := logNoteLines([]page.LogEntry{thrown}, "https://shop.test/", 301)
	if len(got) != 1 || got[0] != "the Page threw: TypeError: x is undefined (app.js:120)" {
		t.Errorf("one: %q", got)
	}
	rejected := page.LogEntry{Time: at, Source: "exception", Level: "error", Text: "Uncaught (in promise) Error: save failed"}
	failed := page.LogEntry{Time: at, Source: "network", Level: "error", Text: status404, URL: "https://shop.test/api/save"}
	got = logNoteLines([]page.LogEntry{thrown, failed, rejected, thrown}, "https://shop.test/", 301)
	want := []string{
		"1 failed request: 404 /api/save",
		"the Page threw 3 times: TypeError: x is undefined (app.js:120); 2 more: winuse logs 301",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("several:\n%q\nwant\n%q", got, want)
	}
	if got := logNoteLines([]page.LogEntry{rejected}, "", 301); len(got) != 1 || got[0] != "the Page threw: (in promise) Error: save failed" {
		t.Errorf("a rejection: %q", got)
	}
}

func TestLogsClear(t *testing.T) {
	r := newWinRig(t)
	early := time.Now().Add(-time.Minute)
	replayLog(r.browser.Srv, logParams(early, "network", "error", status404, "https://docs.test/a"))
	out, e := r.run(t, "logs", protocol.LogsArgs{Window: "301", Clear: true})
	if e != nil || !strings.Contains(out, " network error ") || !strings.HasSuffix(out, "(cleared: the next winuse logs lists only later entries)\n") {
		t.Fatalf("logs --clear: %q %+v", out, e)
	}
	cleared := false
	for _, m := range r.browser.Srv.Received() {
		cleared = cleared || (m.Method == "Log.clear" && m.SessionID == docsSession)
	}
	if !cleared {
		t.Error("Log.clear was not sent to the Tab")
	}
	out, e = r.run(t, "logs", protocol.LogsArgs{Window: "301"})
	if e != nil || !strings.Contains(out, "no log entries") {
		t.Errorf("after --clear: %q %+v", out, e)
	}
}

func TestLogsJSON(t *testing.T) {
	r := newWinRig(t)
	early := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	replayLog(r.browser.Srv, logParams(early, "network", "error", status404, "https://docs.test/a"))
	out, e := r.run(t, "logs", protocol.LogsArgs{Window: "301", JSON: true})
	if e != nil {
		t.Fatalf("logs --json: %+v", e)
	}
	var got struct {
		URL     string `json:"url"`
		Entries []struct {
			Time   time.Time `json:"time"`
			Source string    `json:"source"`
			Level  string    `json:"level"`
			Text   string    `json:"text"`
			URL    string    `json:"url"`
		} `json:"entries"`
		Cleared bool `json:"cleared"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.URL != "https://docs.test/" || len(got.Entries) != 1 || got.Cleared {
		t.Fatalf("logs --json: %+v", got)
	}
	en := got.Entries[0]
	if !en.Time.Equal(early) || en.Source != "network" || en.Level != "error" || en.Text != status404 || en.URL != "https://docs.test/a" {
		t.Errorf("entry %+v", en)
	}
	empty, e := r.run(t, "logs", protocol.LogsArgs{Window: "301", JSON: true, Clear: true})
	if e != nil || !strings.Contains(empty, `"cleared":true`) {
		t.Errorf("--json --clear: %q %+v", empty, e)
	}
}

func TestLogsCutKeepsTheNewestAndOutWritesAll(t *testing.T) {
	r := newWinRig(t)
	base := time.Now().Add(-time.Hour)
	var entries []map[string]any
	for i := 0; i < 10; i++ {
		entries = append(entries, logParams(base.Add(time.Duration(i)*time.Second), "network", "error", status404, fmt.Sprintf("https://docs.test/file-%d", i)))
	}
	replayLog(r.browser.Srv, entries...)
	n := 400
	out, e := r.run(t, "logs", protocol.LogsArgs{Window: "301", MaxChars: &n})
	if e != nil {
		t.Fatalf("logs --max-chars: %+v", e)
	}
	if !strings.Contains(out, "https://docs.test/file-9\n") || strings.Contains(out, "https://docs.test/file-0\n") {
		t.Errorf("the cut kept the wrong entries:\n%s", out)
	}
	if !strings.Contains(out, "older entries left out to stay within 400 characters; --max-chars raises the cut, 0 lifts it, --out <file> writes them all)") {
		t.Errorf("the cut does not say so:\n%s", out)
	}

	file := filepath.Join(t.TempDir(), "logs.txt")
	out, e = r.run(t, "logs", protocol.LogsArgs{Window: "301", Out: file})
	if e != nil {
		t.Fatalf("logs --out: %+v", e)
	}
	data, err := os.ReadFile(file)
	if err != nil || strings.Count(string(data), " network error ") != 10 {
		t.Fatalf("file %q %v", data, err)
	}
	if !strings.Contains(out, "Logs: "+file+" (10 entries)") || strings.Contains(out, " network error ") {
		t.Errorf("printed %q", out)
	}
}

func TestLogsRefusals(t *testing.T) {
	r := newWinRig(t)
	neg, some := -1, 10
	cases := []struct {
		a    protocol.LogsArgs
		want string
	}{
		{protocol.LogsArgs{}, "a window reference is required"},
		{protocol.LogsArgs{Window: "101"}, "is not a browser Window"},
		{protocol.LogsArgs{Window: "301", MaxChars: &neg}, "--max-chars takes a number of characters"},
		{protocol.LogsArgs{Window: "301", MaxChars: &some, Out: `C:\x.txt`}, "pass --out or --max-chars"},
	}
	for _, c := range cases {
		_, e := r.run(t, "logs", c.a)
		if e == nil || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%+v: %+v, want %q", c.a, e, c.want)
		}
	}
}

// navigate notes the requests that failed while it loaded the page: the
// same site's by path, another site's by host and path.
func TestNavigateNotesFailedRequests(t *testing.T) {
	r := newWinRig(t)
	r.browser.Srv.Handle("Page.navigate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			URL string `json:"url"`
		}
		json.Unmarshal(m.Params, &p)
		now := time.Now()
		c.Event(m.SessionID, "Log.entryAdded", logParams(now, "network", "error", status404, "https://docs.test/api/save"))
		c.Event(m.SessionID, "Log.entryAdded", logParams(now.Add(time.Millisecond), "network", "error", httpFailure(500, "Internal Server Error"), "https://api.docs.test/user?id=7"))
		c.Event(m.SessionID, "Log.entryAdded", logParams(now.Add(2*time.Millisecond), "network", "error", "Failed to load resource: net::ERR_CONNECTION_REFUSED", "https://cdn.other.test/lib.js"))
		c.Event(m.SessionID, "Log.entryAdded", logParams(now.Add(3*time.Millisecond), "other", "warning", "a warning is no failure", ""))
		r.mu.Lock()
		r.loader, r.url = "L2", p.URL
		r.mu.Unlock()
		return map[string]any{"frameId": "F1", "loaderId": "L2"}, nil
	})
	// An entry from before the navigation is not its doing.
	replayLog(r.browser.Srv, logParams(time.Now().Add(-time.Minute), "network", "error", status404, "https://docs.test/old"))
	out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "301", URL: "https://docs.test/next"})
	want := "ok https://docs.test/next\nNote: 3 failed requests: 404 /api/save, 500 /user?id=7, net::ERR_CONNECTION_REFUSED cdn.other.test/lib.js\n"
	if e != nil || out != want {
		t.Errorf("navigate: %+v\n%q\nwant\n%q", e, out, want)
	}
	onlyLogDomain(t, r.browser.Srv)
}

// A Page click that made requests fail stays ok and says so after what
// the click did; a warning or an entry from before is not counted.
func TestPageClickNotesFailedRequests(t *testing.T) {
	r := newPageRig(t)
	r.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": r.loader, "url": "https://site.test/app"}}}, nil
	})
	r.srv.Handle("Input.dispatchMouseEvent", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p map[string]any
		json.Unmarshal(m.Params, &p)
		if p["type"] == "mousePressed" {
			now := time.Now()
			c.Event("S", "Log.entryAdded", logParams(now.Add(-time.Hour), "network", "error", status404, "https://site.test/replayed"))
			c.Event("S", "Log.entryAdded", logParams(now, "network", "error", status404, "https://site.test/api/save"))
			c.Event("S", "Log.entryAdded", logParams(now, "network", "warning", "slow", "https://site.test/slow"))
		}
		return nil, nil
	})
	r.effectWorld(t, map[string]any{}, nil)
	out, e, _ := r.runEffect(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}, false)
	want := "ok\nNote: no change seen in the Page; the click was delivered, so read the Page before repeating it\nNote: 1 failed request: 404 /api/save\n"
	if e != nil || out != want {
		t.Errorf("click: %+v\n%q\nwant\n%q", e, out, want)
	}
	onlyLogDomain(t, r.srv)

	// A click that logged nothing new keeps its plain output.
	r = newPageRig(t)
	if out, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}); e != nil || out != okOutput {
		t.Errorf("a quiet click: %q %+v", out, e)
	}
}

func TestLogNoteNamesAtMostThree(t *testing.T) {
	at := time.Now()
	var entries []page.LogEntry
	for i, p := range []string{"/a", "/b", "/c", "/d", "/e"} {
		entries = append(entries, page.LogEntry{Time: at.Add(time.Duration(i)), Source: "network", Level: "error", Text: status404, URL: "https://shop.test" + p})
	}
	entries = append(entries, page.LogEntry{Time: at, Source: "security", Level: "error",
		Text: "Refused to load the script 'https://evil.test/x.js' because it violates the following Content Security Policy directive: \"script-src 'self'\". " + strings.Repeat("Note that 'script-src-elem' was not explicitly set. ", 5)})
	got := logNoteLines(entries, "https://www.shop.test/cart", 301)
	if len(got) != 2 {
		t.Fatalf("notes %q", got)
	}
	if want := "5 failed requests: 404 /a, 404 /b, 404 /c, and 2 more: winuse logs 301"; got[0] != want {
		t.Errorf("requests: %q, want %q", got[0], want)
	}
	if !strings.HasPrefix(got[1], "1 security error: Refused to load the script 'https://evil.test/x.js'") || len(got[1]) > 220 {
		t.Errorf("security: %q", got[1])
	}
	// Without the Page's URL every request names its host.
	if got := logNoteLines(entries[:1], "", 301); got[0] != "1 failed request: 404 shop.test/a" {
		t.Errorf("no Page URL: %q", got)
	}
}
