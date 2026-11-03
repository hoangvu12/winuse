package helper

// Offline tests of the file chooser interception's scope (browser merge
// ticket 06's follow-up): Page.setInterceptFileChooserDialog is on only
// while winuse acts on the Tab, so a file input the user clicks in their
// own browser opens their chooser as usual. Over 05's cdptest Page
// (pageRig) and the fake winuse extension. No browser runs.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// chooserScope follows the interception state a fake browser holds, and
// writes each change and each input event (with the state at that moment)
// to the rig's log.
type chooserScope struct {
	mu sync.Mutex
	on bool
}

func (s *chooserScope) set(params json.RawMessage) string {
	var p struct {
		Enabled bool `json:"enabled"`
	}
	json.Unmarshal(params, &p)
	s.mu.Lock()
	s.on = p.Enabled
	s.mu.Unlock()
	return fmt.Sprintf("intercept %v", p.Enabled)
}

func (s *chooserScope) now() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on
}

// inputEvent is an input event's log line: its kind and whether choosers
// were intercepted when it arrived.
func (s *chooserScope) inputEvent(method string, params json.RawMessage) (string, string) {
	var p struct {
		Type string `json:"type"`
	}
	json.Unmarshal(params, &p)
	kind := p.Type
	if kind == "" {
		kind = strings.TrimPrefix(method, "Input.")
	}
	return kind, fmt.Sprintf("%s on=%v", kind, s.now())
}

// checkScoped fails unless the log is one Action inside one interception
// window: on first, off last, and every input event in between saw it on.
func checkScoped(t *testing.T, what string, log []string) {
	t.Helper()
	if len(log) < 3 || log[0] != "intercept true" || log[len(log)-1] != "intercept false" {
		t.Errorf("%s: not one interception window: %v", what, log)
		return
	}
	for _, s := range log[1 : len(log)-1] {
		if strings.HasPrefix(s, "intercept ") || strings.HasSuffix(s, "on=false") {
			t.Errorf("%s: %q inside the window: %v", what, s, log)
		}
	}
}

// scopedPageRig is a pageRig with a Tab watch on "S" and the interception
// state followed; a mouse release opens a file chooser when opens is set.
func scopedPageRig(t *testing.T, opens bool) (*pageRig, *chooserScope, *page.TabWatch) {
	t.Helper()
	r := newPageRig(t)
	w := page.Watch(context.Background(), r.client, "S")
	t.Cleanup(func() { page.Unwatch(r.client, "S") })
	sc := &chooserScope{}
	r.srv.Handle("Page.setInterceptFileChooserDialog", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.record(sc.set(m.Params))
		return nil, nil
	})
	input := func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		kind, line := sc.inputEvent(m.Method, m.Params)
		r.record(line)
		if opens && kind == "mouseReleased" {
			c.Event("S", "Page.fileChooserOpened", map[string]any{"frameId": "F1", "mode": "selectSingle", "backendNodeId": 70})
		}
		return nil, nil
	}
	r.srv.Handle("Input.dispatchMouseEvent", input)
	r.srv.Handle("Input.dispatchKeyEvent", input)
	r.srv.Handle("Input.insertText", input)
	return r, sc, w
}

// TestChooserInterceptionOnlyDuringAClick: off after the Tab is watched,
// on for the click's events (and its settle), off when the click is done;
// the caught chooser is reported, and a later upload answers it on the
// input's backendNodeId with interception off again afterwards.
func TestChooserInterceptionOnlyDuringAClick(t *testing.T) {
	r, sc, w := scopedPageRig(t, true)
	if sc.now() || w.InterceptingNow() || len(r.sent()) != 0 {
		t.Fatalf("interception outside an Action: %v", r.sent())
	}
	out, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e != nil || out != "ok\nNote: the click opened a file chooser, which the browser held back so nothing opened on the screen: answer it with winuse upload 301 <file>...\n" {
		t.Fatalf("click: %q %+v", out, e)
	}
	checkScoped(t, "click", r.sent())
	if !strings.Contains(strings.Join(r.sent(), ","), "mousePressed on=true") {
		t.Errorf("no press: %v", r.sent())
	}
	if sc.now() || w.InterceptingNow() {
		t.Error("interception left on after the click")
	}
	if ch, _ := w.Chooser(); ch == nil || ch.BackendNodeID != 70 {
		t.Fatalf("the chooser was not kept: %+v", ch)
	}

	// The upload answers the stored chooser: element 7 is no file input,
	// so its click's chooser gets the files, in the Tab's session.
	r.set(func() { r.inputs = nil })
	r.pageFunctions(func(fn string, args []json.RawMessage) (any, bool) {
		if strings.Contains(fn, "winuse:file") {
			return map[string]any{"file": false}, true
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
	if out, e := r.run(t, "upload", protocol.UploadArgs{Window: "301", ElementIndex: idx(7), Files: []string{file}}); e != nil || out != okOutput {
		t.Fatalf("upload %q %+v", out, e)
	}
	want, _ := json.Marshal(map[string]any{"backendNodeId": 70, "files": []string{file}})
	if len(set) != 1 || set[0] != "S "+string(want) {
		t.Errorf("set %v", set)
	}
	if ch, _ := w.Chooser(); ch != nil || sc.now() {
		t.Errorf("after the upload: chooser %+v, on %v", ch, sc.now())
	}
	r.noForeground(t)
}

// TestChooserInterceptionOffAfterAFailedClick: a click whose events fail
// still turns interception off.
func TestChooserInterceptionOffAfterAFailedClick(t *testing.T) {
	r, sc, w := scopedPageRig(t, false)
	r.srv.Handle("Input.dispatchMouseEvent", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		_, line := sc.inputEvent(m.Method, m.Params)
		r.record(line)
		if strings.Contains(string(m.Params), "mousePressed") {
			return nil, &cdptest.ReplyError{Code: -32000, Message: "the target crashed"}
		}
		return nil, nil
	})
	_, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e == nil {
		t.Fatal("the click did not fail")
	}
	checkScoped(t, "failed click", r.sent())
	if sc.now() || w.InterceptingNow() {
		t.Error("interception left on after a failure")
	}

	// A refusal before any input never turns it on.
	r.set(func() { r.inputs = nil; r.aim = map[string]any{"state": "disabled"} })
	if _, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}); e == nil {
		t.Fatal("a disabled element was clicked")
	}
	if got := r.sent(); len(got) != 0 {
		t.Errorf("a refusal sent %v", got)
	}
}

// TestChooserInterceptionOffAfterEsc: an Esc stop between the click's
// events turns interception off too.
func TestChooserInterceptionOffAfterEsc(t *testing.T) {
	r, sc, w := scopedPageRig(t, false)
	pressed := false
	r.srv.Handle("Input.dispatchMouseEvent", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		_, line := sc.inputEvent(m.Method, m.Params)
		r.record(line)
		if !pressed {
			pressed = true
			r.esc.mu.Lock()
			r.esc.pressed = true
			r.esc.mu.Unlock()
		}
		return nil, nil
	})
	_, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e == nil || e.Exit != protocol.ExitEsc {
		t.Fatalf("not stopped: %+v", e)
	}
	checkScoped(t, "Esc", r.sent())
	if sc.now() || w.InterceptingNow() {
		t.Error("interception left on after Esc")
	}
}

// TestChooserInterceptionDuringTyping: text and keys routed to the Page
// hold interception too (Enter or Space on a file input opens a chooser).
func TestChooserInterceptionDuringTyping(t *testing.T) {
	r, sc, _ := scopedPageRig(t, false)
	if _, e := r.run(t, "type-text", protocol.TypeTextArgs{Window: "301", Text: "hi"}); e != nil {
		t.Fatal(e.Error())
	}
	checkScoped(t, "type-text", r.sent())
	r.set(func() { r.inputs = nil })
	if _, e := r.run(t, "press-key", protocol.PressKeyArgs{Window: "301", Key: "Enter"}); e != nil {
		t.Fatal(e.Error())
	}
	checkScoped(t, "press-key", r.sent())
	if sc.now() {
		t.Error("interception left on after the keys")
	}
}

// TestChooserInterceptionThroughTheExtension: the user's own browser. The
// Tab watch sends no interception; a click holds it for its events only,
// and the chooser it opened is reported.
func TestChooserInterceptionThroughTheExtension(t *testing.T) {
	const tid = "E2000000000000000000000000000002"
	sc := &chooserScope{}
	var mu sync.Mutex
	var log []string
	note := func(s string) {
		mu.Lock()
		log = append(log, s)
		mu.Unlock()
	}
	logged := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), log...)
	}
	var ext *bridgetest.Extension
	ext = bridgetest.Start(t, t.TempDir(), &bridgetest.Extension{
		Tabs:    []bridgetest.Tab{{TargetID: tid, Title: "Docs", URL: "https://docs.test/", TabID: 12, WindowID: 1, Active: true}},
		Windows: []bridgetest.Window{{WindowID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, State: "normal"}},
		Page: func(sessionID, method string, params json.RawMessage) (any, error) {
			switch method {
			case "Page.setInterceptFileChooserDialog":
				note(sc.set(params))
			case "Page.getFrameTree":
				return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": "L1"}}}, nil
			case "DOM.resolveNode":
				return map[string]any{"object": map[string]any{"type": "object", "objectId": "obj"}}, nil
			case "Runtime.callFunctionOn":
				var p struct {
					Fn string `json:"functionDeclaration"`
				}
				json.Unmarshal(params, &p)
				var v any
				switch {
				case strings.Contains(p.Fn, "winuse:aim"):
					v = map[string]any{"state": "ok", "l": 10.0, "t": 20.0, "w": 50.0, "h": 10.0, "x": 35.0, "y": 25.0, "chain": true, "dpr": 1.5, "name": "page name"}
				case strings.Contains(p.Fn, "isConnected"):
					v = true
				}
				return map[string]any{"result": map[string]any{"type": "object", "value": v}}, nil
			case "Runtime.evaluate":
				return map[string]any{"result": map[string]any{"value": 1.5}}, nil
			case "Input.dispatchMouseEvent":
				kind, line := sc.inputEvent(method, params)
				note(line)
				if kind == "mouseReleased" {
					ext.Emit(tid, "", "Page.fileChooserOpened", map[string]any{"frameId": "F1", "mode": "selectSingle", "backendNodeId": 70})
				}
			}
			return nil, nil
		},
	})
	bc, err := bridge.Dial(context.Background(), ext.Info.Addr(), ext.Info.Token)
	if err != nil {
		t.Fatal(err)
	}
	client := cdp.NewClient(bc)
	t.Cleanup(func() { client.Close() })
	if _, err := client.Call(context.Background(), "Target.attachToTarget", map[string]any{"targetId": tid, "flatten": true}); err != nil {
		t.Fatal(err)
	}
	w := page.Watch(context.Background(), client, tid)
	t.Cleanup(func() { page.Unwatch(client, tid) })
	for _, c := range ext.Commands() {
		if c.Method == "Page.setInterceptFileChooserDialog" {
			t.Fatalf("the watch intercepts the user's choosers: %+v", c)
		}
	}

	r := newPageRig(t)
	r.trees.origins[browserWindowID] = map[int]any{7: &page.Origin{Browser: "chrome", Client: client, TargetID: tid, SessionID: tid, FrameID: "F1", LoaderID: "L1", BackendNodeID: 70, DocIndex: 4}}
	r.pages.tab = tid
	out, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e != nil || !strings.Contains(out, "the click opened a file chooser") {
		t.Fatalf("click: %q %+v", out, e)
	}
	checkScoped(t, "extension click", logged())
	if sc.now() || w.InterceptingNow() {
		t.Error("the user's browser keeps intercepting after the click")
	}
	if ch, _ := w.Chooser(); ch == nil || ch.BackendNodeID != 70 {
		t.Errorf("the chooser was not kept: %+v", ch)
	}
	r.noForeground(t)
}

// TestChooserInterceptionLingersOnlyInWinusesOwnBrowsers: a browser winuse
// launched or cloned keeps interception chooserHold past an Action, so a
// chooser the page opens a moment later is held back too; the user's own
// browser (the extension, the inspect toggle) never does, and neither does
// a Page read over another connection.
func TestChooserInterceptionLingersOnlyInWinusesOwnBrowsers(t *testing.T) {
	r := newPageRig(t)
	w := browserWindow()
	o := &page.Origin{Browser: "chrome", Client: r.client, TargetID: "T1", SessionID: "S"}
	for _, tc := range []struct {
		mode browser.Mode
		want time.Duration
	}{
		{browser.ModeLaunch, chooserHold},
		{browser.ModeClone, chooserHold},
		{browser.ModeExtension, 0},
		{browser.ModeToggle, 0},
	} {
		s := Services{Pages: &livePages{browsers: pageConns{conn: &browser.Connection{Browser: "chrome", Mode: tc.mode, PID: w.PID, Client: r.client}}}}
		if got := s.chooserLinger(context.Background(), w, o); got != tc.want {
			t.Errorf("%s: linger %s, want %s", tc.mode, got, tc.want)
		}
	}
	other := &page.Origin{Browser: "chrome", Client: &cdp.Client{}, SessionID: "S"}
	s := Services{Pages: &livePages{browsers: pageConns{conn: &browser.Connection{Browser: "chrome", Mode: browser.ModeLaunch, PID: w.PID, Client: r.client}}}}
	if got := s.chooserLinger(context.Background(), w, other); got != 0 {
		t.Errorf("another connection's Page: linger %s", got)
	}
	if got := (Services{}).chooserLinger(context.Background(), w, o); got != 0 {
		t.Errorf("no Browser Connections: linger %s", got)
	}

	// The release itself: at once without a linger, linger later with one.
	var mu sync.Mutex
	offs := 0
	off := func() error { mu.Lock(); offs++; mu.Unlock(); return nil }
	count := func() int { mu.Lock(); defer mu.Unlock(); return offs }
	releaseChoosers(off, "S", 0)()
	if count() != 1 {
		t.Fatalf("no linger: released %d times", count())
	}
	releaseChoosers(off, "S", 50*time.Millisecond)()
	if count() != 1 {
		t.Error("the linger released at once")
	}
	time.Sleep(200 * time.Millisecond)
	if count() != 2 {
		t.Errorf("the linger never released: %d", count())
	}
}
