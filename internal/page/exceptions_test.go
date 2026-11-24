package page

// The exception listener (.scratch/bladebro-ideas ticket 30) over the
// cdptest fake: Capture installs a main-world listener and an isolated
// world's buffer with Page.addScriptToEvaluateOnNewDocument, once per
// session (and again after the extension's idle detach), and
// PullExceptions reads the buffer with Runtime.evaluate in that world and
// adds the records to the Tab's ring. Runtime.enable and Network.enable
// are never sent. No browser runs; test/e2e/exceptions_test.go runs the
// scripts in a headless Chrome.

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// addScripts answers Page.addScriptToEvaluateOnNewDocument with numbered
// identifiers, as Chrome does, and returns the params each got.
func addScripts(srv *cdptest.Server) func() []map[string]any {
	var mu sync.Mutex
	var got []map[string]any
	srv.Handle("Page.addScriptToEvaluateOnNewDocument", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p map[string]any
		json.Unmarshal(m.Params, &p)
		mu.Lock()
		got = append(got, p)
		n := len(got)
		mu.Unlock()
		return map[string]any{"identifier": strconv.Itoa(n)}, nil
	})
	return func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), got...)
	}
}

// exceptionWorldOn answers the isolated world winuse reads the buffer in
// (context 41 for the exception world) and its Runtime.evaluate with reply,
// the buffer's JSON.
func exceptionWorldOn(t *testing.T, srv *cdptest.Server, reply func() string) {
	srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "MAIN", "loaderId": "L1", "url": "https://shop.test/cart"}}}, nil
	})
	srv.Handle("Page.createIsolatedWorld", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			FrameID   string `json:"frameId"`
			WorldName string `json:"worldName"`
		}
		json.Unmarshal(m.Params, &p)
		if p.FrameID != "MAIN" || p.WorldName != exceptionWorld {
			t.Errorf("isolated world %+v, want the main frame's %q", p, exceptionWorld)
		}
		return map[string]any{"executionContextId": 41}, nil
	})
	srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			ContextID int `json:"contextId"`
		}
		json.Unmarshal(m.Params, &p)
		if p.ContextID != 41 {
			t.Errorf("Runtime.evaluate in context %d, want the exception world's 41", p.ContextID)
		}
		return map[string]any{"result": map[string]any{"type": "string", "value": reply()}}, nil
	})
}

func TestCaptureInstallsAMainWorldListenerAndAnIsolatedBuffer(t *testing.T) {
	f := newFakeTab(t)
	scripts := addScripts(f.srv)
	l := WatchLog(context.Background(), f.c, "S", "T1")
	if l.Capturing() {
		t.Fatal("a Tab captures before anyone asked")
	}
	if !l.Capture(context.Background(), "S") {
		t.Error("the first Capture did not say it was the first")
	}
	waitFor(t, "both scripts", func() bool { return len(scripts()) == 2 })
	// Chrome runs the scripts in a new document only with Page on: it goes
	// out first.
	var order []string
	for _, m := range f.srv.Received() {
		if m.SessionID == "S" && (m.Method == "Page.enable" || m.Method == "Page.addScriptToEvaluateOnNewDocument") {
			order = append(order, m.Method)
		}
	}
	if len(order) == 0 || order[0] != "Page.enable" {
		t.Errorf("sent %v; want Page.enable before the scripts", order)
	}
	got := scripts()
	iso, main := got[0], got[1]
	if iso["worldName"] != exceptionWorld || iso["runImmediately"] != true {
		t.Errorf("the buffer's script: worldName %v, runImmediately %v; want %q, true", iso["worldName"], iso["runImmediately"], exceptionWorld)
	}
	if _, named := main["worldName"]; named {
		t.Errorf("the listener runs in world %v, want the main world (no worldName)", main["worldName"])
	}
	if main["runImmediately"] == true {
		t.Error("the listener runs in the document already loaded, after the Page's own scripts")
	}
	src, _ := main["source"].(string)
	for _, want := range []string{"'error'", "'unhandledrejection'", "dispatchEvent", "CustomEvent", "prepareStackTrace"} {
		if !strings.Contains(src, want) {
			t.Errorf("the listener lacks %s", want)
		}
	}
	for _, never := range []string{"onerror", "console", "window.__", "globalThis."} {
		if strings.Contains(src, never) {
			t.Errorf("the listener mentions %q", never)
		}
	}
	// The event is named at random per Tab, and both scripts use the name.
	name := l.eventName()
	if len(name) < 12 || !strings.Contains(src, name) || !strings.Contains(iso["source"].(string), name) {
		t.Errorf("event name %q missing from the scripts", name)
	}
	other := WatchLog(context.Background(), f.c, "S2", "T2")
	other.Capture(context.Background(), "S2")
	if other.eventName() == name {
		t.Error("two Tabs share an event name")
	}

	// Once per session: a second Capture sends nothing.
	if l.Capture(context.Background(), "S") {
		t.Error("a second Capture said it was the first")
	}
	time.Sleep(20 * time.Millisecond)
	if n := len(scripts()); n != 4 {
		t.Errorf("%d scripts sent, want 4 (2 per Tab)", n)
	}
	if !l.Capturing() {
		t.Error("the Tab does not say it captures")
	}
	noRuntimeOrNetwork(t, f.srv)
}

// After the extension's silent idle detach the scripts are gone: Capture
// removes the old ones (in case they were not) and adds them again.
func TestCaptureAgainAfterTheIdleDetach(t *testing.T) {
	f := newFakeTab(t)
	scripts := addScripts(f.srv)
	l := WatchLog(context.Background(), f.c, "S", "T1")
	l.Capture(context.Background(), "S")
	waitFor(t, "the scripts", func() bool { return len(scripts()) == 2 })
	// As if the last use were enableAgain ago.
	l.mu.Lock()
	l.captures["S"].used = time.Now().Add(-enableAgain - time.Second)
	l.mu.Unlock()
	if l.Capture(context.Background(), "S") {
		t.Error("capture was on already")
	}
	waitFor(t, "the scripts again", func() bool { return len(scripts()) == 4 })
	var removed []string
	for _, m := range f.srv.Received() {
		if m.Method == "Page.removeScriptToEvaluateOnNewDocument" {
			var p struct {
				Identifier string `json:"identifier"`
			}
			json.Unmarshal(m.Params, &p)
			removed = append(removed, p.Identifier)
		}
	}
	if strings.Join(removed, ",") != "1,2" {
		t.Errorf("removed %v, want the old scripts 1,2", removed)
	}
	if got := scripts(); got[2]["source"] != got[0]["source"] || got[3]["source"] != got[1]["source"] {
		t.Error("the scripts changed: the Tab's event name must stay, so its loaded Page's listener reaches the new buffer")
	}
}

func TestPullExceptionsAddsTheRecordsToTheRing(t *testing.T) {
	f := newFakeTab(t)
	addScripts(f.srv)
	// Times to the microsecond, the finest a record keeps (a float64 of
	// nanoseconds since the epoch is not exact).
	at := time.Now().Truncate(time.Microsecond)
	ms := func(t time.Time) float64 { return float64(t.UnixMicro()) / 1000 }
	buffer := map[string]any{"c": true, "d": 0, "r": []map[string]any{
		{"t": ms(at), "m": "Uncaught TypeError: x is undefined", "u": "https://shop.test/static/app.js", "l": 120,
			"s": "TypeError: x is undefined\n    at render (https://shop.test/static/app.js:120:7)\n    at https://shop.test/static/app.js:200:3"},
		{"t": ms(at.Add(time.Millisecond)), "m": "Uncaught (in promise) Error: save failed",
			"s": "Error: save failed\n    at save (https://shop.test/static/api.js:33:11)"},
		{"t": ms(at.Add(2 * time.Millisecond)), "m": "Uncaught (in promise) 42"},
	}}
	data, _ := json.Marshal(buffer)
	var mu sync.Mutex
	reads := 0
	exceptionWorldOn(t, f.srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		reads++
		if reads > 1 {
			return `{"c":true,"d":0,"r":[]}` // the buffer empties on read
		}
		return string(data)
	})
	l := WatchLog(context.Background(), f.c, "S", "T1")
	l.Capture(context.Background(), "S")
	covered, ok := l.PullExceptions(context.Background(), "S")
	if !ok || !covered {
		t.Fatalf("pull: covered %v, ok %v", covered, ok)
	}
	got := l.Entries()
	if len(got) != 3 {
		t.Fatalf("entries %+v", got)
	}
	thrown := got[0]
	if thrown.Source != "exception" || thrown.Level != "error" || thrown.Text != "Uncaught TypeError: x is undefined" ||
		thrown.URL != "https://shop.test/static/app.js" || thrown.Line != 120 || !thrown.Time.Equal(at) {
		t.Errorf("the throw: %+v", thrown)
	}
	// The frames only: the stack's first line repeats the message.
	if want := "at render (https://shop.test/static/app.js:120:7)\nat https://shop.test/static/app.js:200:3"; thrown.Stack != want {
		t.Errorf("stack %q, want %q", thrown.Stack, want)
	}
	// A rejection has no file or line of its own: its first frame's.
	if rej := got[1]; rej.URL != "https://shop.test/static/api.js" || rej.Line != 33 || rej.Stack != "at save (https://shop.test/static/api.js:33:11)" {
		t.Errorf("the rejection: %+v", rej)
	}
	if plain := got[2]; plain.Text != "Uncaught (in promise) 42" || plain.URL != "" || plain.Stack != "" {
		t.Errorf("a rejected value that is no Error: %+v", plain)
	}
	if _, ok := l.PullExceptions(context.Background(), "S"); !ok || len(l.Entries()) != 3 {
		t.Errorf("a second pull: %+v", l.Entries())
	}
	noRuntimeOrNetwork(t, f.srv)
}

// A session the listener was never installed in (the browser gave no
// identifier) is not read: nothing is sent.
func TestPullExceptionsWithoutTheListener(t *testing.T) {
	f := newFakeTab(t) // answers addScriptToEvaluateOnNewDocument with {}
	l := WatchLog(context.Background(), f.c, "S", "T1")
	if _, ok := l.PullExceptions(context.Background(), "S"); ok {
		t.Error("a Tab that never captured was read")
	}
	l.Capture(context.Background(), "S")
	if _, ok := l.PullExceptions(context.Background(), "S"); ok {
		t.Error("a session without the scripts was read")
	}
	for _, m := range f.srv.Methods() {
		if m == "Page.createIsolatedWorld" || m == "Runtime.evaluate" {
			t.Errorf("%s was sent", m)
		}
	}
}

// Capture on a Tab with no session (a browser page) only turns it on: the
// Tab's next session gets the listener.
func TestCaptureWithoutASession(t *testing.T) {
	f := newFakeTab(t)
	scripts := addScripts(f.srv)
	l := WatchLog(context.Background(), f.c, "", "T1")
	if !l.Capture(context.Background(), "") || !l.Capturing() {
		t.Fatal("capture is not on")
	}
	time.Sleep(20 * time.Millisecond)
	if n := len(scripts()); n != 0 {
		t.Errorf("%d scripts sent without a session", n)
	}
	if l.Capture(context.Background(), "S") {
		t.Error("the session's Capture said it was the first")
	}
	waitFor(t, "the scripts", func() bool { return len(scripts()) == 2 })
}
