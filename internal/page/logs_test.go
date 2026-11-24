package page

// A Tab's log (.scratch/bladebro-ideas ticket 29) over the cdptest fake:
// Log.enable's replay of the entries from before winuse attached, the ring's
// bound and order, one ring per Tab (and one Tab's sessions sharing it),
// what a mark counts as new, and clearing. Runtime.enable and
// Network.enable are never sent. No browser runs.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// logEntry is a Log.entryAdded event's params, stamped at t to the
// microsecond, as Chrome stamps them.
func logEntry(t time.Time, source, level, text, url string) map[string]any {
	e := map[string]any{"source": source, "level": level, "text": text, "timestamp": float64(t.UnixMicro()) / 1000}
	if url != "" {
		e["url"] = url
	}
	return map[string]any{"entry": e}
}

const notFound = "Failed to load resource: the server responded with a status of 404 (Not Found)"

// replayOn makes Log.enable replay entries on its session first, as Chrome
// does with the entries stored before the domain was on.
func replayOn(srv *cdptest.Server, entries ...map[string]any) {
	srv.Handle("Log.enable", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		for _, e := range entries {
			c.Event(m.SessionID, "Log.entryAdded", e)
		}
		return nil, nil
	})
}

func noRuntimeOrNetwork(t *testing.T, srv *cdptest.Server) {
	t.Helper()
	for _, m := range srv.Methods() {
		if m == "Runtime.enable" || strings.HasPrefix(m, "Network.") {
			t.Errorf("%s was sent", m)
		}
	}
}

func TestLogEnableReplaysTheEntriesFromBefore(t *testing.T) {
	f := newFakeTab(t)
	early := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	replayOn(f.srv,
		logEntry(early, "network", "error", notFound, "https://site.test/api/save"),
		logEntry(early.Add(time.Second), "security", "error", "Refused to load the image 'https://x.test/a.png' because it violates the following Content Security Policy directive: \"img-src 'self'\".", ""))
	l := WatchLog(context.Background(), f.c, "S", "T1")
	l.WaitReplay(context.Background())
	got := l.Entries()
	if len(got) != 2 || got[0].Source != "network" || got[0].URL != "https://site.test/api/save" || got[1].Source != "security" || got[1].Level != "error" {
		t.Fatalf("entries %+v", got)
	}
	// The original times to the microsecond, not when winuse heard of them.
	if !got[0].Time.Equal(early) || !got[1].Time.Equal(early.Add(time.Second)) {
		t.Errorf("times %v %v, want %v and a second later", got[0].Time, got[1].Time, early)
	}
	if TabLogOf(f.c, "T1") != l || WatchLog(context.Background(), f.c, "S", "T1") != l {
		t.Error("a second watch or lookup found another log")
	}
	enables := 0
	for _, m := range f.srv.Received() {
		if m.Method == "Log.enable" {
			enables++
			if m.SessionID != "S" {
				t.Errorf("Log.enable on session %q", m.SessionID)
			}
		}
	}
	if enables != 1 {
		t.Errorf("%d Log.enable, want 1", enables)
	}
	noRuntimeOrNetwork(t, f.srv)
}

func TestTabLogKeepsTheLast50NewestLast(t *testing.T) {
	f := newFakeTab(t)
	l := WatchLog(context.Background(), f.c, "S", "T1")
	l.WaitReplay(context.Background())
	base := time.Now()
	for i := 0; i < LogRingSize+10; i++ {
		f.srv.Event("S", "Log.entryAdded", logEntry(base.Add(time.Duration(i)*time.Millisecond), "network", "error", fmt.Sprintf("e%d", i), ""))
	}
	waitFor(t, "the last entry", func() bool {
		e := l.Entries()
		return len(e) > 0 && e[len(e)-1].Text == fmt.Sprintf("e%d", LogRingSize+9)
	})
	got := l.Entries()
	if len(got) != LogRingSize || got[0].Text != "e10" {
		t.Fatalf("%d entries from %q, want %d from e10", len(got), got[0].Text, LogRingSize)
	}
	// An entry older than the whole full ring was evicted already: a
	// replay does not bring it back.
	f.srv.Event("S", "Log.entryAdded", logEntry(base.Add(-time.Second), "network", "error", "older", ""))
	f.srv.Event("S", "Log.entryAdded", logEntry(base.Add(time.Hour), "network", "error", strings.Repeat("x", 5000), ""))
	waitFor(t, "the long entry", func() bool {
		e := l.Entries()
		return strings.HasPrefix(e[len(e)-1].Text, "xxx")
	})
	got = l.Entries()
	for _, e := range got {
		if e.Text == "older" {
			t.Error("an entry older than the full ring was kept")
		}
	}
	if n := len([]rune(got[len(got)-1].Text)); n > 1100 {
		t.Errorf("a %d-character text was kept whole", n)
	}
}

func TestTabLogIsPerTab(t *testing.T) {
	f := newFakeTab(t)
	at := time.Now()
	one := WatchLog(context.Background(), f.c, "S1", "T1")
	two := WatchLog(context.Background(), f.c, "S2", "T2")
	if one == two {
		t.Fatal("two Tabs share a log")
	}
	f.srv.Event("S1", "Log.entryAdded", logEntry(at, "network", "error", notFound, "https://a.test/x"))
	waitFor(t, "Tab 1's entry", func() bool { return len(one.Entries()) == 1 })
	time.Sleep(20 * time.Millisecond)
	if len(two.Entries()) != 0 {
		t.Errorf("Tab 2 got Tab 1's entry: %+v", two.Entries())
	}
	// Tab 1 attached again (the extension detached it): Log.enable replays
	// what Tab 1's log holds, plus what came while it was detached.
	replayOn(f.srv,
		logEntry(at, "network", "error", notFound, "https://a.test/x"),
		logEntry(at.Add(time.Second), "network", "error", notFound, "https://a.test/y"))
	if again := WatchLog(context.Background(), f.c, "S3", "T1"); again != one {
		t.Fatal("a new session of Tab 1 got a log of its own")
	}
	one.WaitReplay(context.Background())
	got := one.Entries()
	if len(got) != 2 || got[0].URL != "https://a.test/x" || got[1].URL != "https://a.test/y" {
		t.Errorf("after the replay: %+v", got)
	}
	if len(two.Entries()) != 0 {
		t.Errorf("Tab 2: %+v", two.Entries())
	}
}

// A mark right after the session's Log.enable went out waits for its
// replay, so what the Page logged before is not counted after the mark.
// What joins later counts whatever its time: the browser's clock is not
// winuse's, and one a little behind must not lose an entry.
func TestTabLogSinceAMark(t *testing.T) {
	f := newFakeTab(t)
	replayOn(f.srv, logEntry(time.Now().Add(-time.Minute), "network", "error", "replayed", ""))
	l := WatchLog(context.Background(), f.c, "S", "T1")
	m := l.Mark(context.Background()) // no Settle first
	if got := l.Entries(); len(got) != 1 || got[0].Text != "replayed" {
		t.Fatalf("the mark did not wait for the replay: %+v", got)
	}
	if got := l.Since(m); len(got) != 0 {
		t.Errorf("the replay counted after the mark: %+v", got)
	}
	f.srv.Event("S", "Log.entryAdded", logEntry(time.Now().Add(-5*time.Millisecond), "network", "error", "during", ""))
	waitFor(t, "the entry", func() bool { return len(l.Entries()) == 2 })
	got := l.Since(m)
	if len(got) != 1 || got[0].Text != "during" {
		t.Errorf("since the mark: %+v", got)
	}
	// Ticket 30's records join the same ring.
	l.Add(LogEntry{Time: time.Now().Add(2 * time.Millisecond), Source: "exception", Level: "error", Text: "TypeError: x is undefined", Stack: "at f (app.js:120:3)"})
	if got := l.Since(m); len(got) != 2 || got[1].Source != "exception" || got[1].Stack == "" {
		t.Errorf("with an added record: %+v", got)
	}
}

func TestTabLogClear(t *testing.T) {
	f := newFakeTab(t)
	old := time.Now().Add(-time.Minute)
	replayOn(f.srv, logEntry(old, "network", "error", notFound, "https://a.test/x"))
	l := WatchLog(context.Background(), f.c, "S", "T1")
	l.WaitReplay(context.Background())
	if len(l.Entries()) != 1 {
		t.Fatalf("entries %+v", l.Entries())
	}
	l.Clear(context.Background())
	if len(l.Entries()) != 0 {
		t.Errorf("after Clear: %+v", l.Entries())
	}
	cleared := false
	for _, m := range f.srv.Received() {
		cleared = cleared || (m.Method == "Log.clear" && m.SessionID == "S")
	}
	if !cleared {
		t.Errorf("Log.clear was not sent: %v", f.srv.Methods())
	}
	// A browser that kept its store (Log.clear failed) replays the old
	// entry to a new session: it stays cleared.
	WatchLog(context.Background(), f.c, "S2", "T1")
	l.WaitReplay(context.Background())
	f.srv.Event("S2", "Log.entryAdded", logEntry(time.Now().Add(time.Millisecond), "network", "error", "new", ""))
	waitFor(t, "the new entry", func() bool { return len(l.Entries()) == 1 })
	if got := l.Entries(); got[0].Text != "new" {
		t.Errorf("after the replay: %+v", got)
	}
}

// A Tab that shows a browser page has no session yet: its log exists, with
// nothing turned on, so an Action's mark there counts what the Page logs
// once navigate attaches it.
func TestWatchLogWithoutASession(t *testing.T) {
	f := newFakeTab(t)
	l := WatchLog(context.Background(), f.c, "", "T1")
	if l == nil {
		t.Fatal("no log")
	}
	m := l.Mark(context.Background())
	replayOn(f.srv, logEntry(time.Now(), "network", "error", notFound, "https://a.test/x"))
	if WatchLog(context.Background(), f.c, "S", "T1") != l {
		t.Fatal("the attached Tab got another log")
	}
	l.WaitReplay(context.Background())
	if got := l.Since(m); len(got) != 1 {
		t.Errorf("since the mark: %+v", got)
	}
	if WatchLog(context.Background(), nil, "S", "T1") != nil {
		t.Error("a nil client has a log")
	}
}
