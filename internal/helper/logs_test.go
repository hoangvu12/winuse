package helper

// Failed requests and CSP errors (.scratch/bladebro-ideas ticket 29) over
// the fake launched Chrome (winRig) and 05's Page (pageRig): `winuse logs`
// with the entries Log.enable replays, --clear, --json, the cut and --out;
// the note after a Page Action that saw new error entries, with same-site
// paths and at most 3 named. Log is the only domain turned on: never
// Runtime.enable or Network.enable. No browser runs.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
)

const status404 = "Failed to load resource: the server responded with a status of 404 (Not Found)"

func httpFailure(code int, text string) string {
	return fmt.Sprintf("Failed to load resource: the server responded with a status of %d (%s)", code, text)
}

// logParams is a Log.entryAdded event's params, stamped at t to the
// microsecond, as Chrome stamps them.
func logParams(t time.Time, source, level, text, url string) map[string]any {
	e := map[string]any{"source": source, "level": level, "text": text, "timestamp": float64(t.UnixMicro()) / 1000}
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
	want := "Page: https://docs.test/ - 2 log entries, newest last (failed requests and CSP or security errors; winuse keeps the last 50 per Tab)\n" +
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
	want := "Page: https://docs.test/ - no log entries (failed requests and CSP or security errors show here)\n"
	if e != nil || out != want {
		t.Errorf("logs: %q %+v, want %q", out, e, want)
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
// same site's by path, another site's by host and path. The browser logs
// them before it answers Page.navigate, and every one is in the note.
func TestNavigateNotesFailedRequests(t *testing.T) {
	r := newWinRig(t)
	r.browser.Srv.Handle("Page.navigate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			URL string `json:"url"`
		}
		json.Unmarshal(m.Params, &p)
		// The browser's clock a little behind winuse's: the entries still
		// count, since they joined after the mark.
		now := time.Now().Add(-5 * time.Millisecond)
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
// the click did; a warning or an entry from before is not counted. The
// click's entry is stamped a little before winuse's clock (the browser's
// clock is not winuse's) and still counts.
func TestPageClickNotesFailedRequests(t *testing.T) {
	r := newPageRig(t)
	// Logged before the click: Log.enable, sent as the click begins,
	// replays it.
	replayLog(r.srv, logParams(time.Now().Add(-time.Hour), "network", "error", status404, "https://site.test/replayed"))
	r.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": r.loader, "url": "https://site.test/app"}}}, nil
	})
	r.srv.Handle("Input.dispatchMouseEvent", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p map[string]any
		json.Unmarshal(m.Params, &p)
		if p["type"] == "mousePressed" {
			now := time.Now().Add(-5 * time.Millisecond)
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
