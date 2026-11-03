package page

// Offline tests of the page-only Actions' CDP side (browser merge ticket
// 06), over cdptest fakes: the dialog and file chooser watch, the input
// fast failure while a dialog shows, drag sequences (plain and HTML5
// through Input.dragIntercepted), option picking, navigation and its load
// wait, waits, eval formatting, and downloads with their renames. No
// browser runs.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// fakeTab is a cdptest endpoint and a client to it.
type fakeTab struct {
	srv *cdptest.Server
	c   *cdp.Client

	mu   sync.Mutex
	sent []string // Input events, "type x,y"
}

func newFakeTab(t *testing.T) *fakeTab {
	t.Helper()
	f := &fakeTab{srv: cdptest.NewServer()}
	t.Cleanup(f.srv.Close)
	ws, err := cdp.DialWS(context.Background(), f.srv.URL(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	f.c = cdp.NewClient(ws)
	t.Cleanup(func() { f.c.Close() })
	record := func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p map[string]any
		json.Unmarshal(m.Params, &p)
		f.mu.Lock()
		f.sent = append(f.sent, fmt.Sprintf("%s %v %v,%v", strings.TrimPrefix(m.Method, "Input."), p["type"], p["x"], p["y"]))
		f.mu.Unlock()
		return nil, nil
	}
	f.srv.Handle("Input.dispatchMouseEvent", record)
	f.srv.Handle("Input.dispatchDragEvent", record)
	return f
}

func (f *fakeTab) events() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

// waitFor polls cond for up to a second.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestWatchFollowsDialogsAndChoosers(t *testing.T) {
	f := newFakeTab(t)
	w := Watch(context.Background(), f.c, "S")
	t.Cleanup(func() { Unwatch(f.c, "S") })
	// No file chooser interception at the start: the user's own clicks in
	// the Tab open their chooser as usual (Intercept turns it on per Action).
	if got := f.srv.Methods(); !reflect.DeepEqual(got, []string{"Page.enable"}) {
		t.Errorf("start sent %v", got)
	}
	if w.Intercepting() || w.InterceptingNow() || Watch(context.Background(), f.c, "S") != w {
		t.Errorf("intercepting %v, or a second watch", w.Intercepting())
	}
	f.srv.Event("S", "Page.javascriptDialogOpening", map[string]any{"type": "confirm", "message": "Leave\n this page?", "url": "https://a.test/"})
	waitFor(t, "the dialog", func() bool { return OpenDialog(f.c, "S") != nil })
	if d := OpenDialog(f.c, "S"); d.Describe() != `a confirm dialog: "Leave this page?"` {
		t.Errorf("describe %q", d.Describe())
	}
	if OpenDialog(f.c, "OTHER") != nil {
		t.Error("another session sees the dialog")
	}
	f.srv.Event("S", "Page.javascriptDialogClosed", map[string]any{"result": true})
	waitFor(t, "the close", func() bool { return OpenDialog(f.c, "S") == nil })

	f.srv.Event("S", "Page.fileChooserOpened", map[string]any{"frameId": "F1", "mode": "selectMultiple", "backendNodeId": 42})
	waitFor(t, "the chooser", func() bool { ch, _ := w.Chooser(); return ch != nil })
	if ch, n := w.Chooser(); ch.BackendNodeID != 42 || !ch.Multiple || n != 1 {
		t.Errorf("chooser %+v %d", ch, n)
	}
	f.srv.Event("S", "Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "F2", "parentId": "F1"}})
	f.srv.Event("S", "Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "F1"}})
	waitFor(t, "the navigation", func() bool { ch, _ := w.Chooser(); return ch == nil })
	if _, n := w.Chooser(); n != 1 {
		t.Errorf("count %d", n)
	}
}

// interceptCalls is the enabled values of every
// Page.setInterceptFileChooserDialog the fake received.
func interceptCalls(srv *cdptest.Server) []bool {
	var out []bool
	for _, m := range srv.Received() {
		if m.Method != "Page.setInterceptFileChooserDialog" {
			continue
		}
		var p struct {
			Enabled bool `json:"enabled"`
		}
		json.Unmarshal(m.Params, &p)
		out = append(out, p.Enabled)
	}
	return out
}

// TestInterceptHoldsNestAndRelease: the first hold turns interception on,
// nested holds send nothing, the last release turns it off, and a second
// call of a release is a no-op. A chooser caught while on stays after.
func TestInterceptHoldsNestAndRelease(t *testing.T) {
	f := newFakeTab(t)
	w := Watch(context.Background(), f.c, "S")
	t.Cleanup(func() { Unwatch(f.c, "S") })
	outer := w.Intercept(context.Background())
	inner := w.Intercept(context.Background())
	if !w.InterceptingNow() || !w.Intercepting() {
		t.Error("not on while held")
	}
	f.srv.Event("S", "Page.fileChooserOpened", map[string]any{"frameId": "F1", "mode": "selectSingle", "backendNodeId": 9})
	waitFor(t, "the chooser", func() bool { ch, _ := w.Chooser(); return ch != nil })
	if err := inner(); err != nil || !w.InterceptingNow() {
		t.Errorf("inner release %v, on %v", err, w.InterceptingNow())
	}
	if err := outer(); err != nil || w.InterceptingNow() {
		t.Errorf("outer release %v, on %v", err, w.InterceptingNow())
	}
	outer()
	if got := interceptCalls(f.srv); !reflect.DeepEqual(got, []bool{true, false}) {
		t.Errorf("calls %v", got)
	}
	if ch, _ := w.Chooser(); ch == nil || ch.BackendNodeID != 9 {
		t.Errorf("the chooser was lost with interception: %+v", ch)
	}
	// Again: one more on and off pair.
	w.Intercept(context.Background())()
	if got := interceptCalls(f.srv); !reflect.DeepEqual(got, []bool{true, false, true, false}) {
		t.Errorf("calls %v", got)
	}
	// A nil watch holds nothing.
	var none *TabWatch
	if err := none.Intercept(context.Background())(); err != nil || none.InterceptingNow() {
		t.Error("nil watch")
	}
}

// TestInterceptRefusedOrStuck: a browser that refuses interception is not
// asked again; a release whose off call fails returns the error, keeps
// the state on, and the next release tries again.
func TestInterceptRefusedOrStuck(t *testing.T) {
	f := newFakeTab(t)
	f.srv.Handle("Page.setInterceptFileChooserDialog", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return nil, &cdptest.ReplyError{Code: -32601, Message: "'Page.setInterceptFileChooserDialog' wasn't found"}
	})
	w := Watch(context.Background(), f.c, "S")
	t.Cleanup(func() { Unwatch(f.c, "S") })
	if err := w.Intercept(context.Background())(); err != nil || w.Intercepting() {
		t.Errorf("refused: %v %v", err, w.Intercepting())
	}
	w.Intercept(context.Background())()
	if got := interceptCalls(f.srv); len(got) != 1 {
		t.Errorf("asked again after a refusal: %v", got)
	}

	g := newFakeTab(t)
	fail := true
	var mu sync.Mutex
	g.srv.Handle("Page.setInterceptFileChooserDialog", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail && strings.Contains(string(m.Params), "false") {
			return nil, &cdptest.ReplyError{Code: -32000, Message: "boom"}
		}
		return nil, nil
	})
	w2 := Watch(context.Background(), g.c, "S")
	t.Cleanup(func() { Unwatch(g.c, "S") })
	if err := w2.Intercept(context.Background())(); err == nil || !w2.InterceptingNow() {
		t.Errorf("a failed off: %v, on %v", err, w2.InterceptingNow())
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	if err := w2.Intercept(context.Background())(); err != nil || w2.InterceptingNow() {
		t.Errorf("the retry: %v, on %v", err, w2.InterceptingNow())
	}
	// The second hold found it still on, so it sent no second on.
	if got := interceptCalls(g.srv); !reflect.DeepEqual(got, []bool{true, false, false}) {
		t.Errorf("calls %v", got)
	}
	// A cancelled Action still turns it off.
	ctx, cancel := context.WithCancel(context.Background())
	release := w2.Intercept(ctx)
	cancel()
	if err := release(); err != nil || w2.InterceptingNow() {
		t.Errorf("after cancel: %v, on %v", err, w2.InterceptingNow())
	}
}

// TestInputFailsAtOnceWhileADialogShows: 05's 5 s wait is gone; nothing is
// sent.
func TestInputFailsAtOnceWhileADialogShows(t *testing.T) {
	f := newFakeTab(t)
	Watch(context.Background(), f.c, "S")
	t.Cleanup(func() { Unwatch(f.c, "S") })
	f.srv.Event("S", "Page.javascriptDialogOpening", map[string]any{"type": "alert", "message": "Saved"})
	waitFor(t, "the dialog", func() bool { return OpenDialog(f.c, "S") != nil })
	start := time.Now()
	in, err := NewInput(context.Background(), f.c, "S", nil)
	var open *DialogOpenError
	if in != nil || !errors.As(err, &open) || open.Dialog.Type != "alert" || time.Since(start) > time.Second {
		t.Fatalf("NewInput = %v, %v", in, err)
	}
	if err.Error() != `the Page shows an alert dialog: "Saved", and takes no input until it is answered` {
		t.Errorf("message %q", err.Error())
	}
	if len(f.events()) != 0 {
		t.Errorf("sent %v", f.events())
	}
}

func TestDragEvents(t *testing.T) {
	evs := DragEvents(Point{10, 20}, Point{110, 220}, 4)
	var got []string
	for _, e := range evs {
		got = append(got, fmt.Sprintf("%v %v,%v b%v", e["type"], e["x"], e["y"], e["buttons"]))
	}
	want := []string{"mouseMoved 10,20 b0", "mousePressed 10,20 b1", "mouseMoved 35,70 b1", "mouseMoved 60,120 b1",
		"mouseMoved 85,170 b1", "mouseMoved 110,220 b1", "mouseReleased 110,220 b0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("drag events\n got %q\nwant %q", got, want)
	}
	if h := HoverEvents(Point{3, 4}); len(h) != 1 || h[0]["type"] != "mouseMoved" || h[0]["buttons"] != 0 {
		t.Errorf("hover %v", h)
	}
}

func TestDragPlainAndHTML5(t *testing.T) {
	old := dragPace
	dragPace = 0
	t.Cleanup(func() { dragPace = old })

	f := newFakeTab(t)
	in, err := NewInput(context.Background(), f.c, "S", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	kind, err := in.Drag(context.Background(), Point{0, 0}, Point{100, 0}, 0)
	if err != nil || kind != DragMouse {
		t.Fatalf("plain drag %v %v", kind, err)
	}
	ev := f.events()
	if len(ev) != DragSteps+3 || ev[0] != "dispatchMouseEvent mouseMoved 0,0" || ev[len(ev)-1] != "dispatchMouseEvent mouseReleased 100,0" {
		t.Errorf("plain events %v", ev)
	}
	methods := strings.Join(f.srv.Methods(), ",")
	if !strings.Contains(methods, "Input.setInterceptDrags") {
		t.Errorf("no interception: %s", methods)
	}

	// HTML5: the browser hands the drag over after the third move.
	f2 := newFakeTab(t)
	moves := 0
	f2.srv.Handle("Input.dispatchMouseEvent", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p map[string]any
		json.Unmarshal(m.Params, &p)
		f2.mu.Lock()
		f2.sent = append(f2.sent, fmt.Sprintf("dispatchMouseEvent %v %v,%v", p["type"], p["x"], p["y"]))
		f2.mu.Unlock()
		if p["type"] == "mouseMoved" && p["buttons"] == 1.0 {
			if moves++; moves == 3 {
				c.Event(m.SessionID, "Input.dragIntercepted", map[string]any{"data": map[string]any{"items": []any{map[string]any{"mimeType": "text/plain", "data": "x"}}, "dragOperationsMask": 1}})
			}
		}
		return nil, nil
	})
	in2, _ := NewInput(context.Background(), f2.c, "S", nil)
	defer in2.Close()
	kind, err = in2.Drag(context.Background(), Point{0, 0}, Point{100, 50}, 0)
	if err != nil || kind != DragHTML5 {
		t.Fatalf("html5 drag %v %v (%v)", kind, err, f2.events())
	}
	ev = f2.events()
	tail := ev[len(ev)-4:]
	want := []string{"dispatchDragEvent dragEnter 100,50", "dispatchDragEvent dragOver 100,50", "dispatchDragEvent drop 100,50", "dispatchMouseEvent mouseReleased 100,50"}
	if !reflect.DeepEqual(tail, want) {
		t.Errorf("html5 tail %v", ev)
	}
}

// TestDragHoldsForTheDropDelay: --drop-delay (desktop-parity 13) holds a
// plain drag between its last move and the release, and an HTML5 drag
// between dragOver and drop.
func TestDragHoldsForTheDropDelay(t *testing.T) {
	old := dragPace
	dragPace = 0
	t.Cleanup(func() { dragPace = old })
	const drop = 120 * time.Millisecond

	for _, html5 := range []bool{false, true} {
		f := newFakeTab(t)
		var mu sync.Mutex
		at := map[string]time.Time{}
		moves := 0
		stamp := func(c *cdptest.Conn, m cdptest.Message) (any, error) {
			var p map[string]any
			json.Unmarshal(m.Params, &p)
			mu.Lock()
			defer mu.Unlock()
			at[fmt.Sprint(p["type"])] = time.Now()
			if p["type"] == "mouseMoved" && p["buttons"] == 1.0 {
				if moves++; html5 && moves == 3 {
					c.Event(m.SessionID, "Input.dragIntercepted", map[string]any{"data": map[string]any{"items": []any{}, "dragOperationsMask": 1}})
				}
			}
			return nil, nil
		}
		f.srv.Handle("Input.dispatchMouseEvent", stamp)
		f.srv.Handle("Input.dispatchDragEvent", stamp)
		in, err := NewInput(context.Background(), f.c, "S", nil)
		if err != nil {
			t.Fatal(err)
		}
		kind, err := in.Drag(context.Background(), Point{0, 0}, Point{100, 0}, drop)
		in.Close()
		if err != nil || (kind == DragHTML5) != html5 {
			t.Fatalf("html5 %v: drag %v %v", html5, kind, err)
		}
		mu.Lock()
		before, after := at["mouseMoved"], at["mouseReleased"]
		if html5 {
			before, after = at["dragOver"], at["drop"]
		}
		mu.Unlock()
		if gap := after.Sub(before); gap < drop {
			t.Errorf("html5 %v: held %s before the drop, want at least %s", html5, gap, drop)
		}
	}
}

func TestPickOption(t *testing.T) {
	opts := []Option{{Label: "Red", Value: "r"}, {Label: "Green", Value: "g"}, {Label: "Blue sky", Value: "Red"}, {Label: "green", Value: "g2"}}
	cases := []struct {
		want string
		idx  int
		err  string
	}{
		{"Green", 1, ""},
		{"Red", 0, ""},           // the label wins over another option's value
		{"g2", 3, ""},            // by value
		{"  blue   SKY ", 2, ""}, // case and spaces ignored last
		{"GREEN", -1, "several options match \"GREEN\""},
		{"Purple", -1, `the list has no option "Purple"; its options are "Red", "Green", "Blue sky", "green"`},
	}
	for _, c := range cases {
		i, err := PickOption(opts, c.want)
		if i != c.idx || (c.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%q: %d %v", c.want, i, err)
		}
	}
	for v, want := range map[string]bool{"on": true, "Checked": true, " true ": true, "off": false, "UNCHECKED": false} {
		if got, err := ParseOnOff(v); err != nil || got != want {
			t.Errorf("%q: %v %v", v, got, err)
		}
	}
	if _, err := ParseOnOff("yes"); err == nil {
		t.Error("yes parsed")
	}
}

func TestNavigate(t *testing.T) {
	f := newFakeTab(t)
	answer := map[string]any{"frameId": "F1", "loaderId": "L2"}
	f.srv.Handle("Page.navigate", func(c *cdptest.Conn, m cdptest.Message) (any, error) { return answer, nil })
	loader, err := Navigate(context.Background(), f.c, "S", "https://a.test/")
	if err != nil || loader != "L2" {
		t.Fatalf("navigate %q %v", loader, err)
	}
	var p struct {
		URL string `json:"url"`
	}
	json.Unmarshal(f.srv.Received()[0].Params, &p)
	if p.URL != "https://a.test/" || NavURL("a.test/x") != "https://a.test/x" {
		t.Errorf("url %q", p.URL)
	}
	answer = map[string]any{"frameId": "F1", "loaderId": "L3", "errorText": "net::ERR_NAME_NOT_RESOLVED"}
	_, err = Navigate(context.Background(), f.c, "S", "https://nowhere.test/")
	var nav *NavigateError
	if !errors.As(err, &nav) || err.Error() != "the browser could not open https://nowhere.test/: net::ERR_NAME_NOT_RESOLVED" {
		t.Errorf("errorText: %v", err)
	}
	answer = map[string]any{"frameId": "F1", "errorText": "net::ERR_ABORTED", "isDownload": true}
	if _, err = Navigate(context.Background(), f.c, "S", "https://a.test/f.pdf"); !errors.Is(err, ErrDownloadStarted) {
		t.Errorf("download: %v", err)
	}
}

// TestNavigateRacesABeforeunloadDialog: the browser holds Page.navigate
// while the Page asks whether to leave; the dialog is reported.
func TestNavigateRacesABeforeunloadDialog(t *testing.T) {
	f := newFakeTab(t)
	Watch(context.Background(), f.c, "S")
	t.Cleanup(func() { Unwatch(f.c, "S") })
	f.srv.Handle("Page.navigate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		c.Event("S", "Page.javascriptDialogOpening", map[string]any{"type": "beforeunload", "message": ""})
		return nil, cdptest.ErrNoReply
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Navigate(ctx, f.c, "S", "https://b.test/")
	var d *DialogError
	if !errors.As(err, &d) || d.Type != "beforeunload" || ctx.Err() != nil {
		t.Errorf("navigate: %v", err)
	}
}

// TestWaitLoad: commit first (the loaderId), then the ready state; a page
// that never completes is a *LoadTimeoutError.
func TestWaitLoad(t *testing.T) {
	old := pollEvery
	pollEvery = 5 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	f := newFakeTab(t)
	var mu sync.Mutex
	state, loader := "loading", "L1"
	polls := 0
	f.srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if polls++; polls == 3 {
			state = "complete"
		}
		return map[string]any{"result": map[string]any{"type": "string", "value": state}}, nil
	})
	f.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		l := loader
		loader = "L2" // commits on the second look
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": l, "url": "https://a.test/"}}}, nil
	})
	committed := func(ctx context.Context) bool {
		l, _, err := MainFrame(ctx, f.c, "S")
		return err == nil && l == "L2"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitLoad(ctx, f.c, "S", LoadComplete, committed, nil); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if polls != 3 {
		t.Errorf("polls %d", polls)
	}

	mu.Lock()
	state = "interactive"
	polls = -100
	mu.Unlock()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel2()
	err := WaitLoad(ctx2, f.c, "S", LoadComplete, nil, nil)
	var slow *LoadTimeoutError
	if !errors.As(err, &slow) || slow.State != "interactive" {
		t.Errorf("timeout: %v", err)
	}
	if err := WaitLoad(context.Background(), f.c, "S", LoadDOM, nil, nil); err != nil {
		t.Errorf("domcontentloaded: %v", err)
	}
	stop := func() bool { return true }
	if err := WaitLoad(context.Background(), f.c, "S", LoadComplete, nil, stop); !errors.Is(err, ErrStopped) {
		t.Errorf("stop: %v", err)
	}
	if _, err := ParseLoadState("networkidle"); err == nil {
		t.Error("networkidle parsed")
	}
}

func TestWaitExprQuotesTheValue(t *testing.T) {
	expr, err := WaitExpr(WaitText, `a "quote" ); alert(1); (`)
	if err != nil || !strings.Contains(expr, `includes("a \"quote\" ); alert(1); (")`) {
		t.Errorf("text expr %q %v", expr, err)
	}
	for _, kind := range []string{WaitSelector, WaitURL} {
		if expr, err := WaitExpr(kind, "x"); err != nil || !strings.Contains(expr, `"x"`) {
			t.Errorf("%s: %q %v", kind, expr, err)
		}
	}
	if expr, _ := WaitExpr(WaitLoadKind, "domcontentloaded"); expr != `/* winuse:wait-load */ document.readyState !== 'loading'` {
		t.Errorf("load expr %q", expr)
	}
	for _, bad := range [][2]string{{"selector", " "}, {"text", ""}, {"load", "none"}, {"idle", "x"}} {
		if _, err := WaitExpr(bad[0], bad[1]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestPoll(t *testing.T) {
	old := pollEvery
	pollEvery = 5 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	f := newFakeTab(t)
	n := 0
	f.srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		n++
		if strings.Contains(string(m.Params), "bad[") {
			return map[string]any{"result": map[string]any{"type": "object"}, "exceptionDetails": map[string]any{"text": "Uncaught",
				"exception": map[string]any{"description": "SyntaxError: 'bad[' is not a valid selector\n    at <anonymous>:1:1"}}}, nil
		}
		return map[string]any{"result": map[string]any{"type": "boolean", "value": n >= 3}}, nil
	})
	if err := Poll(context.Background(), f.c, "S", "ok()", nil); err != nil || n != 3 {
		t.Errorf("poll %v after %d", err, n)
	}
	err := Poll(context.Background(), f.c, "S", "bad[", nil)
	var ee *EvalError
	if !errors.As(err, &ee) || ee.Msg != "SyntaxError: 'bad[' is not a valid selector" {
		t.Errorf("invalid: %v", err)
	}
	n = -1000
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	var timeout *WaitTimeoutError
	if err := Poll(ctx, f.c, "S", "ok()", nil); !errors.As(err, &timeout) {
		t.Errorf("timeout: %v", err)
	}
	if err := Poll(context.Background(), f.c, "S", "ok()", func() bool { return true }); !errors.Is(err, ErrStopped) {
		t.Errorf("stop: %v", err)
	}
}

func TestFormatResult(t *testing.T) {
	cases := []struct {
		obj  RemoteObject
		want string
	}{
		{RemoteObject{Type: "string", Value: json.RawMessage(`"hi\nthere"`)}, "hi\nthere"},
		{RemoteObject{Type: "object", Value: json.RawMessage(`{ "a": [1, 2],  "b": null }`)}, `{"a":[1,2],"b":null}`},
		{RemoteObject{Type: "undefined"}, "undefined"},
		{RemoteObject{Type: "number", UnserializableValue: "NaN"}, "NaN"},
		{RemoteObject{Type: "number", Value: json.RawMessage(`42`)}, "42"},
		{RemoteObject{Type: "object", Subtype: "null", Value: json.RawMessage(`null`)}, "null"},
	}
	for _, c := range cases {
		if got := FormatResult(c.obj); got.Text != c.want || got.Cut() {
			t.Errorf("%+v: %q", c.obj, got.Text)
		}
	}
	long, _ := json.Marshal(strings.Repeat("é", EvalLimit+10))
	r := FormatResult(RemoteObject{Type: "string", Value: long})
	if !r.Cut() || r.Total != EvalLimit+10 || len([]rune(r.Text)) != EvalLimit {
		t.Errorf("cut: total %d, kept %d", r.Total, len([]rune(r.Text)))
	}
}

func TestEval(t *testing.T) {
	f := newFakeTab(t)
	var answer any
	f.srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) { return answer, nil })
	answer = map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{"n": 1}}}
	res, err := Eval(context.Background(), f.c, "S", "({n:1})", nil)
	if err != nil || res.Text != `{"n":1}` {
		t.Errorf("value %q %v", res.Text, err)
	}
	var p map[string]any
	json.Unmarshal(f.srv.Received()[0].Params, &p)
	if p["returnByValue"] != true || p["awaitPromise"] != true || p["expression"] != "({n:1})" {
		t.Errorf("params %v", p)
	}
	answer = map[string]any{"result": map[string]any{"type": "object"}, "exceptionDetails": map[string]any{"text": "Uncaught",
		"exception": map[string]any{"description": "TypeError: x is undefined\n    at <anonymous>:1:3"}}}
	_, err = Eval(context.Background(), f.c, "S", "x.y", nil)
	var ee *EvalError
	if !errors.As(err, &ee) || ee.Msg != "TypeError: x is undefined" {
		t.Errorf("exception %v", err)
	}
	answer = map[string]any{"result": map[string]any{"type": "object", "subtype": "node", "value": map[string]any{}}}
	if _, err = Eval(context.Background(), f.c, "S", "document.body", nil); !errors.Is(err, ErrNotJSON) {
		t.Errorf("node %v", err)
	}

	// An alert in the expression: the call stays pending, the dialog is
	// reported.
	Watch(context.Background(), f.c, "S")
	t.Cleanup(func() { Unwatch(f.c, "S") })
	f.srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		c.Event("S", "Page.javascriptDialogOpening", map[string]any{"type": "alert", "message": "hi"})
		return nil, cdptest.ErrNoReply
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = Eval(ctx, f.c, "S", "alert('hi')", nil)
	var d *DialogError
	if !errors.As(err, &d) || d.Type != "alert" || ctx.Err() != nil {
		t.Errorf("alert: %v", err)
	}
}

// TestEvalStopTerminatesTheScript: Esc during a long expression.
func TestEvalStopTerminatesTheScript(t *testing.T) {
	old := pollEvery
	pollEvery = 5 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	f := newFakeTab(t)
	f.srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) { return nil, cdptest.ErrNoReply })
	_, err := Eval(context.Background(), f.c, "S", "while(1){}", func() bool { return true })
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("stop: %v", err)
	}
	waitFor(t, "terminateExecution", func() bool { return strings.Contains(strings.Join(f.srv.Methods(), ","), "Runtime.terminateExecution") })
}

func TestDownloads(t *testing.T) {
	old := pollEvery
	pollEvery = 5 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	f := newFakeTab(t)
	dir := filepath.Join(t.TempDir(), "winuse")
	d, err := EnableDownloads(context.Background(), f.c, dir)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	json.Unmarshal(f.srv.Received()[0].Params, &p)
	if f.srv.Received()[0].Method != "Browser.setDownloadBehavior" || p["behavior"] != "allowAndName" || p["downloadPath"] != dir || p["eventsEnabled"] != true {
		t.Errorf("sent %v", f.srv.Received()[0])
	}
	if again, _ := EnableDownloads(context.Background(), f.c, dir); again != d || DownloadsOf(f.c) != d || len(f.srv.Received()) != 1 {
		t.Error("enabled twice")
	}
	os.WriteFile(filepath.Join(dir, "report.pdf"), []byte("taken"), 0o644)
	finish := func(guid, name string) {
		os.WriteFile(filepath.Join(dir, guid), []byte("data"), 0o644)
		f.srv.Event("", "Browser.downloadWillBegin", map[string]any{"guid": guid, "url": "https://a.test/" + name, "suggestedFilename": name})
		f.srv.Event("", "Browser.downloadProgress", map[string]any{"guid": guid, "receivedBytes": 4, "totalBytes": 4, "state": "inProgress"})
		f.srv.Event("", "Browser.downloadProgress", map[string]any{"guid": guid, "receivedBytes": 4, "totalBytes": 4, "state": "completed"})
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		finish("g-1", "report.pdf")
	}()
	got, err := d.Wait(context.Background(), nil)
	if err != nil || got.Path != filepath.Join(dir, "report (2).pdf") || got.Name != "report.pdf" {
		t.Fatalf("wait %+v %v", got, err)
	}
	if b, _ := os.ReadFile(got.Path); string(b) != "data" {
		t.Errorf("renamed file holds %q", b)
	}
	if d.Seq() != 1 || len(d.Since(0)) != 1 {
		t.Errorf("seq %d", d.Seq())
	}

	f.srv.Event("", "Browser.downloadWillBegin", map[string]any{"guid": "g-2", "url": "https://a.test/x", "suggestedFilename": "x.zip"})
	f.srv.Event("", "Browser.downloadProgress", map[string]any{"guid": "g-2", "state": "canceled"})
	if _, err := d.Wait(context.Background(), nil); !errors.Is(err, ErrDownloadCanceled) {
		t.Errorf("canceled: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var timeout *DownloadTimeoutError
	if _, err := d.Wait(ctx, nil); !errors.As(err, &timeout) || timeout.Started || !strings.HasPrefix(timeout.Error(), "no download began within") {
		t.Errorf("none: %v", err)
	}
}

func TestSafeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"report.pdf": "report.pdf", `..\..\evil.exe`: "evil.exe", "a/b/c.txt": "c.txt",
		`what?.txt`: "what_.txt", "": "download", "  . ": "download", "name. ": "name",
	} {
		if got := SafeFileName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
