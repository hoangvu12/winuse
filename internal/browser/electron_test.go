package browser

// The attach's offline tests (headless-max 15): the shim is installed over
// the Node inspector; a client command becomes one Runtime.evaluate of the
// shim and its answer, error or exception comes back as the CDP message
// the client expects; the shim's events come back through the binding and
// the inspector's own events are dropped; closing releases the App.
// AttachElectron refuses a non-Electron App and a fuse that is off before
// sending anything, finds the inspector on the App's own ports, and
// DetachApp ends only an attach. The inspector is an in-memory fake; no
// process is signalled.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// fakeInspector is a Node inspector: it answers the transport's setup and
// lets the test answer the rest.
type fakeInspector struct {
	mu        sync.Mutex
	writes    []map[string]any
	in        chan []byte
	closed    bool
	shimValue string // what the shim's install evaluates to
}

func newFakeInspector() *fakeInspector {
	return &fakeInspector{in: make(chan []byte, 64), shimValue: "ok"}
}

func (f *fakeInspector) WriteText(b []byte) error {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	f.mu.Lock()
	f.writes = append(f.writes, m)
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return cdp.ErrClosed
	}
	id := int(m["id"].(float64))
	switch {
	case m["method"] == "Runtime.enable" || m["method"] == "Runtime.addBinding":
		f.send(map[string]any{"id": id, "result": map[string]any{}})
		f.send(map[string]any{"method": "Runtime.executionContextCreated", "params": map[string]any{}})
	case id == 3:
		f.send(map[string]any{"id": id, "result": map[string]any{"result": map[string]any{"type": "string", "value": f.shimValue}}})
	}
	return nil
}

func (f *fakeInspector) send(m map[string]any) {
	b, _ := json.Marshal(m)
	f.in <- b
}

func (f *fakeInspector) ReadMessage(deadline time.Time) ([]byte, error) {
	var timeout <-chan time.Time
	if !deadline.IsZero() {
		timeout = time.After(time.Until(deadline))
	}
	select {
	case b, ok := <-f.in:
		if !ok {
			return nil, cdp.ErrClosed
		}
		return b, nil
	case <-timeout:
		return nil, context.DeadlineExceeded
	}
}

func (f *fakeInspector) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.in)
	}
	return nil
}

// last is the last message written, with its id.
func (f *fakeInspector) last(t *testing.T) (int, map[string]any) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.writes[len(f.writes)-1]
	return int(m["id"].(float64)), m
}

func readJSON(t *testing.T, tr cdp.Transport) map[string]any {
	t.Helper()
	b, err := tr.ReadMessage(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestElectronTransportTranslates(t *testing.T) {
	ins := newFakeInspector()
	tr, err := openElectron(ins)
	if err != nil {
		t.Fatalf("openElectron: %v", err)
	}
	setup := ins.writes[2]["params"].(map[string]any)
	if ins.writes[1]["params"].(map[string]any)["name"] != emitBinding || setup["includeCommandLineAPI"] != true || !strings.Contains(setup["expression"].(string), "webContents.getAllWebContents") {
		t.Errorf("setup = %v", ins.writes)
	}

	// A session command becomes one Runtime.evaluate of the shim.
	cmd := `{"id":7,"method":"DOM.focus","params":{"backendNodeId":3},"sessionId":"electron-1"}`
	if err := tr.WriteText([]byte(cmd)); err != nil {
		t.Fatal(err)
	}
	id, w := ins.last(t)
	p := w["params"].(map[string]any)
	if w["method"] != "Runtime.evaluate" || p["awaitPromise"] != true || p["returnByValue"] != true {
		t.Errorf("command sent as %v", w)
	}
	arg, _ := json.Marshal(cmd)
	if p["expression"] != "globalThis.__winuseShim.call("+string(arg)+")" {
		t.Errorf("expression %q", p["expression"])
	}
	// The inspector's own event is dropped; the answer comes back under
	// the client's id and session.
	ins.send(map[string]any{"method": "Runtime.consoleAPICalled", "params": map[string]any{}})
	ins.send(map[string]any{"id": id, "result": map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{"result": map[string]any{"ok": 1}}}}})
	if m := readJSON(t, tr); m["id"] != float64(7) || m["sessionId"] != "electron-1" || m["result"].(map[string]any)["ok"] != float64(1) {
		t.Errorf("answer = %v", m)
	}

	// The shim's error, and an exception, come back as CDP errors.
	tr.WriteText([]byte(`{"id":8,"method":"Browser.close"}`))
	id, _ = ins.last(t)
	ins.send(map[string]any{"id": id, "result": map[string]any{"result": map[string]any{"value": map[string]any{"error": map[string]any{"code": -32010, "message": "refused"}}}}})
	if m := readJSON(t, tr); m["id"] != float64(8) || m["error"].(map[string]any)["code"] != float64(-32010) || m["sessionId"] != nil {
		t.Errorf("refusal = %v", m)
	}
	tr.WriteText([]byte(`{"id":9,"method":"Target.getTargets"}`))
	id, _ = ins.last(t)
	ins.send(map[string]any{"id": id, "result": map[string]any{"result": map[string]any{}, "exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "TypeError: x\n    at y"}}}})
	if m := readJSON(t, tr); m["error"].(map[string]any)["message"] != "TypeError: x" {
		t.Errorf("exception = %v", m)
	}

	// An event the shim emitted comes back as is; another binding's not.
	ins.send(map[string]any{"method": "Runtime.bindingCalled", "params": map[string]any{"name": "other", "payload": "{}"}})
	ins.send(map[string]any{"method": "Runtime.bindingCalled", "params": map[string]any{"name": emitBinding, "payload": `{"method":"Page.loadEventFired","params":{},"sessionId":"electron-1"}`}})
	if m := readJSON(t, tr); m["method"] != "Page.loadEventFired" || m["sessionId"] != "electron-1" {
		t.Errorf("event = %v", m)
	}

	// Close releases the App: the debuggers detach and the inspector closes.
	tr.Close()
	_, w = ins.last(t)
	if !strings.Contains(w["params"].(map[string]any)["expression"].(string), "__winuseShim.release()") {
		t.Errorf("close sent %v", w)
	}
	if err := tr.WriteText([]byte(`{"id":10,"method":"x"}`)); !errors.Is(err, cdp.ErrClosed) {
		t.Errorf("write after close: %v", err)
	}
}

func TestElectronTransportShimFails(t *testing.T) {
	ins := newFakeInspector()
	ins.shimValue = "no"
	if _, err := openElectron(ins); err == nil || !strings.Contains(err.Error(), "installing winuse's CDP shim") || !ins.closed {
		t.Errorf("openElectron: %v (inspector closed %v)", err, ins.closed)
	}
}

// attachMachine is a fake machine with an Electron App, process 600, whose
// inspector opens on 9229 after the signal.
func attachMachine(t *testing.T) (*fakeMachine, *Manager, string, *[]uint32) {
	f, m := appMachine(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "resources", "app.asar"), "x")
	exe := filepath.Join(dir, "Slack.exe")
	var signalled []uint32
	open := false
	f.sys.HasNodeDebug = func(pid uint32) bool { return pid == 600 }
	f.sys.NodeDebug = func(pid uint32) error { signalled = append(signalled, pid); open = true; return nil }
	f.sys.ListeningPorts = func(pid uint32) []int {
		if pid == 600 && open {
			return []int{5000, 9229}
		}
		return nil
	}
	f.sys.InspectorWS = func(ctx context.Context, port int) (string, error) {
		if port == 9229 {
			return "ws://127.0.0.1:9229/node", nil
		}
		return "", errors.New("not an inspector")
	}
	f.sys.Dial = func(ctx context.Context, wsURL string, handshake time.Duration) (cdp.Transport, error) {
		if wsURL != "ws://127.0.0.1:9229/node" {
			return nil, errors.New("dialed " + wsURL)
		}
		return newFakeInspector(), nil
	}
	return f, m, exe, &signalled
}

func TestAttachElectron(t *testing.T) {
	_, m, exe, signalled := attachMachine(t)
	ctx := context.Background()

	if _, err := m.AttachElectron(ctx, 600, filepath.Join(t.TempDir(), "notepad.exe")); !errors.Is(err, ErrNotElectron) {
		t.Errorf("not Electron: %v", err)
	}
	if _, err := m.AttachElectron(ctx, 601, exe); !errors.Is(err, ErrNodeInspectOff) {
		t.Errorf("fuse off: %v", err)
	}
	if len(*signalled) != 0 {
		t.Fatalf("a refusal signalled %v", *signalled)
	}

	ep, err := m.AttachElectron(ctx, 600, exe)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if ep.Port != 9229 || ep.Mode != ModeAttach || ep.Kind != AppElectron || m.AppKind(600) != AppElectron {
		t.Errorf("endpoint %+v", ep)
	}
	c, err := m.WindowConnection(ctx, AppElectron, 600)
	if err != nil || c.Mode != ModeAttach || c.PID != 600 {
		t.Fatalf("connection %+v, %v", c, err)
	}
	// Attaching again sends nothing.
	if _, err := m.AttachElectron(ctx, 600, exe); err != nil || len(*signalled) != 1 {
		t.Errorf("again: %v, signalled %v", err, *signalled)
	}
	if len(m.open("")) != 0 {
		t.Error("an attached App counts for the browser lookups")
	}

	if err := m.DetachApp(600); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if c.Client.Alive() || m.AppKind(600) != "" {
		t.Error("the attach outlived the detach")
	}
	if err := m.DetachApp(600); !errors.Is(err, ErrNotAttached) {
		t.Errorf("detach twice: %v", err)
	}
}

func TestDetachLeavesALaunchedApp(t *testing.T) {
	f, m := appMachine(t)
	f.listen(51234, "ws://x", 500)
	if _, ok := m.AwaitApp(context.Background(), AppElectron, 500, 500, 51234, time.Second); !ok {
		t.Fatal("not registered")
	}
	if err := m.DetachApp(500); !errors.Is(err, ErrNotAttached) || m.AppKind(500) != AppElectron {
		t.Errorf("detach of a launched App: %v", err)
	}
}

func TestAttachNoInspector(t *testing.T) {
	f, m, exe, _ := attachMachine(t)
	f.sys.ListeningPorts = func(uint32) []int { return nil }
	if _, err := m.AttachElectron(context.Background(), 600, exe); err == nil || !strings.Contains(err.Error(), "did not open on a loopback port") || m.AppKind(600) != "" {
		t.Errorf("no inspector: %v", err)
	}
}
