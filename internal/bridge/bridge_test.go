package bridge_test

// The bridge end to end, offline: winuse's cdp.Client over a bridge.Conn,
// a real native host (bridge.ServeHost) on an ephemeral loopback port,
// and a fake extension (bridgetest) on in-memory pipes playing
// background.js's routing rules. No browser is started.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/cdp"
)

func startExt(t *testing.T, e *bridgetest.Extension) (*bridgetest.Extension, string) {
	t.Helper()
	dir := t.TempDir()
	return bridgetest.Start(t, dir, e), dir
}

func dial(t *testing.T, ext *bridgetest.Extension) *cdp.Client {
	t.Helper()
	conn, err := bridge.Dial(context.Background(), ext.Info.Addr(), ext.Info.Token)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	c := cdp.NewClient(conn)
	t.Cleanup(func() { c.Close() })
	return c
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestBridgeFileRecordsTheBrowserThatStartedTheHost(t *testing.T) {
	ext, dir := startExt(t, &bridgetest.Extension{Browser: "edge", Instance: "e1e1e1e1", BrowserPID: 777, BrowserExe: `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`})
	infos := bridge.List(dir, nil)
	if len(infos) != 1 {
		t.Fatalf("bridge files = %+v", infos)
	}
	i := infos[0]
	if i.Browser != "edge" || i.Instance != "e1e1e1e1" || i.BrowserPID != 777 || !strings.HasSuffix(i.BrowserExe, "msedge.exe") || i.Port == 0 || len(i.Token) != 32 || i.Build != bridge.Build() {
		t.Errorf("bridge file = %+v", i)
	}
	if err := ext.Close(); err != nil {
		t.Errorf("host after stdin closed = %v, want a clean exit", err)
	}
	if got := bridge.List(dir, nil); len(got) != 0 {
		t.Errorf("the host left its bridge file: %+v", got)
	}
}

func TestBridgeRequestsAndResponsesMatchByID(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{Tabs: []bridgetest.Tab{{TargetID: "T1", Title: "One", URL: "https://a.test/", WindowID: 3, TabID: 31, Active: true}}})
	ext.Page = func(sid, method string, params json.RawMessage) (any, error) {
		if method == "Runtime.evaluate" {
			time.Sleep(10 * time.Millisecond)
			return map[string]any{"result": map[string]any{"value": sid + " " + string(params)}}, nil
		}
		return nil, nil
	}
	c := dial(t, ext)
	res, err := c.Call(ctx(t), "Target.getTargets", nil)
	if err != nil {
		t.Fatal(err)
	}
	var tg struct {
		TargetInfos []bridgetest.Tab `json:"targetInfos"`
	}
	json.Unmarshal(res, &tg)
	if len(tg.TargetInfos) != 1 || tg.TargetInfos[0].TargetID != "T1" || tg.TargetInfos[0].WindowID != 3 || tg.TargetInfos[0].Type != "page" {
		t.Fatalf("targets = %s", res)
	}
	res, err = c.Call(ctx(t), "Target.attachToTarget", map[string]any{"targetId": "T1", "flatten": true})
	if err != nil || !strings.Contains(string(res), `"sessionId":"T1"`) {
		t.Fatalf("attach = %s, %v", res, err)
	}
	// Concurrent calls come back to the right caller.
	errs := make(chan error, 8)
	for i := range 8 {
		go func() {
			expr := strings.Repeat("x", i+1)
			res, err := c.CallSession(ctx(t), "T1", "Runtime.evaluate", map[string]any{"expression": expr})
			if err == nil && !strings.Contains(string(res), `T1 {\"expression\":\"`+expr+`\"}`) {
				err = errors.New("mismatched reply " + string(res))
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

func TestBridgeForwardsEventsWithTheirSession(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{Tabs: []bridgetest.Tab{{TargetID: "T1"}, {TargetID: "T2"}}})
	c := dial(t, ext)
	if _, err := c.Call(ctx(t), "Target.attachToTarget", map[string]any{"targetId": "T1", "flatten": true}); err != nil {
		t.Fatal(err)
	}
	tab := c.Subscribe("T1", "")
	defer tab.Close()
	child := c.Subscribe("CHILD-9", "")
	defer child.Close()

	ext.Emit("T2", "", "Page.loadEventFired", map[string]any{}) // a tab this connection never attached
	ext.Emit("T1", "", "Page.loadEventFired", map[string]any{"timestamp": 1})
	ext.Emit("T1", "", "Target.attachedToTarget", map[string]any{"sessionId": "CHILD-9", "targetInfo": map[string]string{"type": "iframe", "targetId": "F9"}})
	ext.Emit("T1", "CHILD-9", "Runtime.consoleAPICalled", map[string]any{"type": "log"})

	want := []string{"Page.loadEventFired", "Target.attachedToTarget"}
	for _, m := range want {
		select {
		case ev := <-tab.Events():
			if ev.Method != m || ev.SessionID != "T1" {
				t.Errorf("tab event = %s on %q, want %s on T1", ev.Method, ev.SessionID, m)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no %s on the tab session", m)
		}
	}
	select {
	case ev := <-child.Events():
		if ev.Method != "Runtime.consoleAPICalled" || ev.SessionID != "CHILD-9" {
			t.Errorf("child event = %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the child session's event never arrived with its sessionId")
	}
}

func TestBridgeRoutesChildSessionCommands(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{Tabs: []bridgetest.Tab{{TargetID: "T1"}}})
	got := make(chan string, 4)
	ext.Page = func(sid, method string, params json.RawMessage) (any, error) {
		got <- sid + " " + method
		return map[string]any{"ok": true}, nil
	}
	c := dial(t, ext)
	c.Call(ctx(t), "Target.attachToTarget", map[string]any{"targetId": "T1", "flatten": true})
	if _, err := c.CallSession(ctx(t), "T1", "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": false, "flatten": true}); err != nil {
		t.Fatal(err)
	}
	ext.Emit("T1", "", "Target.attachedToTarget", map[string]any{"sessionId": "CHILD-1"})
	time.Sleep(50 * time.Millisecond) // the event reaches the fake's child table first
	if _, err := c.CallSession(ctx(t), "CHILD-1", "Runtime.evaluate", map[string]any{"expression": "1"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"T1 Target.setAutoAttach", "CHILD-1 Runtime.evaluate"} {
		if g := <-got; g != want {
			t.Errorf("routed %q, want %q", g, want)
		}
	}
	var sent []string
	for _, cmd := range ext.Commands() {
		sent = append(sent, cmd.SessionID+" "+cmd.Method)
	}
	if !strings.Contains(strings.Join(sent, ","), "CHILD-1 Runtime.evaluate") {
		t.Errorf("the child command went out as %q", sent)
	}

	// The tab detaches: the child goes with it, and the client hears both,
	// as on a DevTools connection: the child's on its tab's session, the
	// tab's at the browser level.
	childGone := c.Subscribe("T1", "Target.detachedFromTarget")
	defer childGone.Close()
	tabGone := c.Subscribe("", "Target.detachedFromTarget")
	defer tabGone.Close()
	ext.Detach("T1", "target_closed")
	for sub, want := range map[*cdp.Subscription]string{childGone: "CHILD-1", tabGone: "T1"} {
		select {
		case ev := <-sub.Events():
			var p struct {
				SessionID string `json:"sessionId"`
			}
			json.Unmarshal(ev.Params, &p)
			if p.SessionID != want {
				t.Errorf("detached %q, want %q", p.SessionID, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no detach event for %s", want)
		}
	}
	if _, err := c.CallSession(ctx(t), "CHILD-1", "Runtime.evaluate", nil); err == nil {
		t.Error("a detached child session still answered")
	}
}

func TestBridgeRefusesBrowserLevelMethodsLocally(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{})
	c := dial(t, ext)
	for _, tc := range []struct {
		session, method string
		code            int
		want            string
	}{
		{"", "Browser.getVersion", bridge.CodeRefused, "Browser.getVersion needs a launched or cloned browser; the winuse extension cannot do it. Run winuse browser launch or winuse browser clone."},
		{"", "Browser.setDownloadBehavior", bridge.CodeRefused, "Browser.setDownloadBehavior needs a launched or cloned browser; the winuse extension cannot do it. Run winuse browser launch or winuse browser clone."},
		{"T1", "Page.bringToFront", bridge.CodeForeground, "Page.bringToFront would bring the browser window to the front, and winuse never takes the foreground."},
		{"", "Target.activateTarget", bridge.CodeForeground, "Target.activateTarget would bring the browser window to the front, and winuse never takes the foreground."},
	} {
		_, err := c.CallSession(ctx(t), tc.session, tc.method, nil)
		var ce *cdp.Error
		if !errors.As(err, &ce) || ce.Code != tc.code || ce.Message != tc.want {
			t.Errorf("%s: err = %v, want code %d %q", tc.method, err, tc.code, tc.want)
		}
	}
	if n := len(ext.Commands()); n != 0 {
		t.Errorf("refused methods reached the extension: %+v", ext.Commands())
	}
}

func TestBridgeActivateTabAndThePausedSwitch(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{Tabs: []bridgetest.Tab{{TargetID: "T1"}}})
	c := dial(t, ext)
	if _, err := c.Call(ctx(t), bridge.MethodActivateTab, map[string]string{"targetId": "T1"}); err != nil {
		t.Fatal(err)
	}
	if a := ext.Activated(); len(a) != 1 || a[0] != "T1" {
		t.Errorf("activated = %v", a)
	}
	ext.SetPaused(true)
	_, err := c.Call(ctx(t), "Target.getTargets", nil)
	var ce *cdp.Error
	if !errors.As(err, &ce) || ce.Code != bridge.CodePaused {
		t.Errorf("a call while paused = %v, want code %d", err, bridge.CodePaused)
	}
	res, err := c.Call(ctx(t), bridge.MethodStatus, nil)
	if err != nil || !strings.Contains(string(res), `"paused":true`) {
		t.Errorf("status while paused = %s, %v", res, err)
	}
}

func TestBridgeChunksCommandsOverTheNativeMessagingLimit(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{Tabs: []bridgetest.Tab{{TargetID: "T1"}}})
	var gotExpr string
	ext.Page = func(sid, method string, params json.RawMessage) (any, error) {
		var p struct {
			Expression string `json:"expression"`
		}
		json.Unmarshal(params, &p)
		gotExpr = p.Expression
		return nil, nil
	}
	c := dial(t, ext)
	expr := strings.Repeat(`é"\€😀<>&`, 150000) // about 3.4 MB with every escape case
	if _, err := c.CallSession(ctx(t), "T1", "Runtime.evaluate", map[string]any{"expression": expr}); err != nil {
		t.Fatal(err)
	}
	if gotExpr != expr {
		t.Fatalf("the joined command differs: %d bytes, want %d", len(gotExpr), len(expr))
	}
	if ext.Chunked() != 1 {
		t.Errorf("chunked commands = %d, want 1", ext.Chunked())
	}
	for _, n := range ext.Envelopes() {
		if n > bridge.MaxToExtension {
			t.Errorf("a %d byte native message", n)
		}
	}
}

func TestBridgeAnswersOversizeCommandsLocally(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{})
	conn, err := bridge.Dial(context.Background(), ext.Info.Addr(), ext.Info.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	big := []byte(`{"id":7,"sessionId":"T1","method":"Input.insertText","params":{"text":"`)
	big = append(big, make([]byte, bridge.MaxCommand)...)
	for i := len(big) - bridge.MaxCommand; i < len(big); i++ {
		big[i] = 'a'
	}
	big = append(big, `"}}`...)
	if err := conn.WriteText(big); err != nil {
		t.Fatal(err)
	}
	reply, err := conn.ReadMessage(time.Now().Add(3 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		ID        int    `json:"id"`
		SessionID string `json:"sessionId"`
		Error     struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(reply, &r)
	if r.ID != 7 || r.SessionID != "T1" || r.Error.Code != bridge.CodeTooLarge {
		t.Errorf("reply = %s", reply)
	}
	if len(ext.Commands()) != 0 {
		t.Error("an oversize command reached the extension")
	}
}

func TestBridgeCloseUnblocksAPendingRead(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{})
	conn, err := bridge.Dial(context.Background(), ext.Info.Addr(), ext.Info.Token)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := conn.ReadMessage(time.Time{})
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	conn.Close()
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Errorf("read after Close = %v, want net.ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock ReadMessage")
	}
	if err := conn.WriteText([]byte(`{"id":1,"method":"Target.getTargets"}`)); err == nil {
		t.Error("a write after Close succeeded")
	}
	// The host tells the extension the connection ended.
	deadline := time.Now().Add(3 * time.Second)
	for ext.Conns() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBridgeHostGoingAwayEndsTheClient(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{})
	c := dial(t, ext)
	ext.Close()
	select {
	case <-c.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the client stayed open after the host exited")
	}
	if _, err := c.Call(ctx(t), "Target.getTargets", nil); !errors.Is(err, cdp.ErrClosed) {
		t.Errorf("call after the host exited = %v", err)
	}
}

func TestBridgeRejectsABadToken(t *testing.T) {
	ext, _ := startExt(t, &bridgetest.Extension{})
	if _, err := bridge.Dial(context.Background(), ext.Info.Addr(), "0123456789abcdef0123456789abcdef"); !errors.Is(err, bridge.ErrTokenRejected) {
		t.Errorf("a wrong token = %v, want ErrTokenRejected", err)
	}
	if _, err := bridge.Dial(context.Background(), ext.Info.Addr(), ""); !errors.Is(err, bridge.ErrTokenRejected) {
		t.Errorf("no token = %v, want ErrTokenRejected", err)
	}
	// A right token still works afterwards.
	dial(t, ext)
}

func TestHostRefusesAStrangerAndAGarbledExtension(t *testing.T) {
	dir := t.TempDir()
	cfg := bridge.HostConfig{Dir: dir, Origin: "chrome-extension://blcbodpjbiimmoenmdneeacdogpglgne/", Listen: func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }}
	if err := bridge.ServeHost(cfg, strings.NewReader(""), os.Stdout); err == nil || !strings.Contains(err.Error(), "not the winuse extension") {
		t.Errorf("another extension's origin = %v", err)
	}
	cfg.Origin = bridge.Origin
	// A BOM and text where the hello's length belongs.
	if err := bridge.ServeHost(cfg, strings.NewReader("\xEF\xBB\xBF{\"hello\":{}}"), os.Stdout); !errors.Is(err, bridge.ErrBadFrame) {
		t.Errorf("a garbled hello = %v, want ErrBadFrame", err)
	}
	var hello []byte
	frame := func(s string) {
		b := make([]byte, 4)
		b[0] = byte(len(s))
		hello = append(append(hello, b...), s...)
	}
	frame(`{"hello":{"browser":"chrome","instance":"../../x"}}`)
	if err := bridge.ServeHost(cfg, strings.NewReader(string(hello)), os.Stdout); err == nil || !strings.Contains(err.Error(), "bad hello") {
		t.Errorf("a hello with a path in it = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a refused host wrote %v", entries)
	}

	// Garbage after a good hello ends the host and removes its file.
	ext := bridgetest.Start(t, filepath.Join(dir, "g"), &bridgetest.Extension{})
	ext.Raw([]byte("\xEF\xBB\xBFgarbage!"))
	done, err := ext.Wait(3 * time.Second)
	if !done || !errors.Is(err, bridge.ErrBadFrame) {
		t.Errorf("garbage from the extension: done=%v err=%v", done, err)
	}
	if got := bridge.List(filepath.Join(dir, "g"), nil); len(got) != 0 {
		t.Errorf("bridge file left: %+v", got)
	}
}
