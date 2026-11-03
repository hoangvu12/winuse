package browser

// The attached Electron App's Browser Connection (headless-max ticket 15,
// probe round 3): a browser-level CDP endpoint served from inside the
// App's main process. winuse opens the main process's Node inspector
// (win.SignalNodeDebug), installs electronShim there with one
// Runtime.evaluate, and from then on every CDP command is one
// Runtime.evaluate of the shim's call, which runs it through Electron's
// webContents.debugger. Events come back through a Runtime binding.
//
// The shim mirrors the winuse extension's routing (bridge/extension/
// background.js): a message without a sessionId is browser-level, and only
// Target.getTargets, Target.attachToTarget and Target.detachFromTarget are
// emulated there, on the App's webContents; a page session's id is its
// target id ("electron-<webContents id>"); a child session (an iframe
// auto-attached with flatten) keeps the id Chromium gave it. Every other
// browser-level method is refused. Page.bringToFront and
// Target.activateTarget are refused everywhere, as on the extension:
// winuse never takes the foreground.
//
// Closing the transport detaches every webContents and closes the
// inspector, so no debugging port stays open in the user's App after the
// helper lets it go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// electronShim is the JavaScript installed in the App's main process. It
// returns "ok" (also when already installed, by an earlier helper).
// require comes from the inspector's command-line API, else the main
// module.
const electronShim = `(() => {
  if (globalThis.__winuseShim) return "ok";
  const req = typeof require === "function" ? require : process.mainModule.require.bind(process.mainModule);
  const { webContents } = req("electron");
  const REFUSED = -32010, FOREGROUND = -32013;
  const attached = new Map(); // webContents id -> webContents
  const childOf = new Map();  // child session id -> webContents id
  const tid = (wc) => "electron-" + wc.id;
  const emit = (o) => { try { globalThis.__winuseEmit(JSON.stringify(o)); } catch (_) {} };
  const fail = (code, message) => Object.assign(new Error(message), { code });
  const pages = () => webContents.getAllWebContents().filter((wc) => !wc.isDestroyed() && ["window", "browserView", "webview"].includes(wc.getType()));
  const find = (id) => pages().find((wc) => tid(wc) === id);
  function attach(wc) {
    if (attached.has(wc.id)) return;
    if (!wc.debugger.isAttached()) wc.debugger.attach("1.3");
    attached.set(wc.id, wc);
    wc.debugger.on("message", (_e, method, params, sessionId) => {
      if (method === "Target.attachedToTarget" && params && params.sessionId) childOf.set(params.sessionId, wc.id);
      if (method === "Target.detachedFromTarget" && params && params.sessionId) childOf.delete(params.sessionId);
      emit({ method, params, sessionId: sessionId || tid(wc) });
    });
    wc.debugger.on("detach", () => {
      attached.delete(wc.id);
      emit({ method: "Target.detachedFromTarget", params: { sessionId: tid(wc), targetId: tid(wc) } });
    });
  }
  async function call(m) {
    const p = m.params || {};
    if (m.method === "Page.bringToFront" || m.method === "Target.activateTarget") {
      throw fail(FOREGROUND, m.method + " would bring the App's window to the front, and winuse never takes the foreground.");
    }
    if (!m.sessionId) {
      switch (m.method) {
        case "Target.getTargets":
          return { targetInfos: pages().map((wc) => ({ targetId: tid(wc), type: "page", title: wc.getTitle(), url: wc.getURL(), attached: attached.has(wc.id) })) };
        case "Target.attachToTarget": {
          const wc = find(p.targetId);
          if (!wc) throw fail(-32000, "no target with id " + p.targetId);
          attach(wc);
          return { sessionId: tid(wc) };
        }
        case "Target.detachFromTarget":
          return {};
      }
      throw fail(REFUSED, m.method + " is not available on an Electron App winuse attached to.");
    }
    const child = childOf.has(m.sessionId);
    const wc = child ? attached.get(childOf.get(m.sessionId)) : pages().find((w) => tid(w) === m.sessionId && attached.has(w.id));
    if (!wc) throw fail(-32001, "no session with id " + m.sessionId);
    return child ? wc.debugger.sendCommand(m.method, p, m.sessionId) : wc.debugger.sendCommand(m.method, p);
  }
  function release() {
    for (const wc of attached.values()) { try { wc.debugger.detach(); } catch (_) {} }
    attached.clear();
    childOf.clear();
    delete globalThis.__winuseShim;
    setTimeout(() => { try { req("inspector").close(); } catch (_) {} }, 100);
  }
  globalThis.__winuseShim = {
    call: (json) => call(JSON.parse(json)).then((r) => ({ result: r || {} }), (e) => ({ error: { code: e.code || -32000, message: String((e && e.message) || e) } })),
    release,
  };
  return "ok";
})()`

// emitBinding is the Runtime binding the shim reports events through.
const emitBinding = "__winuseEmit"

// electronTransport is a cdp.Transport over the main process's Node
// inspector: each command is a Runtime.evaluate of the shim, each answer
// and event is turned back into the CDP message the client expects.
type electronTransport struct {
	ws cdp.Transport

	mu      sync.Mutex
	next    int64
	pending map[int64]outerRequest // inspector id -> the client's request
	closed  bool

	out  chan []byte
	done chan struct{}
	err  error // why the reader stopped
}

// wireError is a CDP error object on the wire.
type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *wireError) Error() string { return e.Message }

// outerRequest is what an answer needs to go back to the client.
type outerRequest struct {
	id        json.RawMessage
	sessionID string
}

// shimSetupWait bounds the setup calls when the transport opens.
const shimSetupWait = 5 * time.Second

// openElectron installs the shim over an open inspector WebSocket and
// starts translating. The transport owns ws.
func openElectron(ws cdp.Transport) (*electronTransport, error) {
	t := &electronTransport{ws: ws, pending: map[int64]outerRequest{}, out: make(chan []byte, 64), done: make(chan struct{})}
	deadline := time.Now().Add(shimSetupWait)
	setup := []struct {
		method string
		params map[string]any
	}{
		{"Runtime.enable", nil},
		{"Runtime.addBinding", map[string]any{"name": emitBinding}},
		{"Runtime.evaluate", map[string]any{"expression": electronShim, "includeCommandLineAPI": true, "returnByValue": true}},
	}
	for i, s := range setup {
		id := int64(i + 1)
		msg, _ := json.Marshal(map[string]any{"id": id, "method": s.method, "params": s.params})
		if err := ws.WriteText(msg); err != nil {
			ws.Close()
			return nil, err
		}
		res, err := awaitInspector(ws, id, deadline)
		if err != nil {
			ws.Close()
			return nil, fmt.Errorf("setting up the attach (%s): %w", s.method, err)
		}
		if s.method == "Runtime.evaluate" {
			if v, err := evalValue(res); err != nil || string(v) != `"ok"` {
				ws.Close()
				return nil, fmt.Errorf("installing winuse's CDP shim in the App failed: %s", shimError(v, err))
			}
		}
	}
	t.next = int64(len(setup))
	go t.read()
	return t, nil
}

// awaitInspector reads until the answer to id arrives, dropping events.
func awaitInspector(ws cdp.Transport, id int64, deadline time.Time) (json.RawMessage, error) {
	for {
		data, err := ws.ReadMessage(deadline)
		if err != nil {
			return nil, err
		}
		var m struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *wireError      `json:"error"`
		}
		if json.Unmarshal(data, &m) != nil || m.ID != id {
			continue
		}
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	}
}

// evalValue is a Runtime.evaluate result's value, or the exception it
// threw.
func evalValue(res json.RawMessage) (json.RawMessage, error) {
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	if e := r.ExceptionDetails; e != nil {
		msg := e.Exception.Description
		if msg == "" {
			msg = e.Text
		}
		return nil, errors.New(firstLine(msg))
	}
	return r.Result.Value, nil
}

func shimError(v json.RawMessage, err error) string {
	if err != nil {
		return err.Error()
	}
	return "it answered " + string(v)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// WriteText sends one client command as a Runtime.evaluate of the shim.
func (t *electronTransport) WriteText(msg []byte) error {
	var m struct {
		ID        json.RawMessage `json:"id"`
		SessionID string          `json:"sessionId"`
	}
	if err := json.Unmarshal(msg, &m); err != nil {
		return err
	}
	arg, _ := json.Marshal(string(msg)) // a JSON string is a JavaScript string literal
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return cdp.ErrClosed
	}
	t.next++
	id := t.next
	t.pending[id] = outerRequest{id: m.ID, sessionID: m.SessionID}
	t.mu.Unlock()
	inner, _ := json.Marshal(map[string]any{"id": id, "method": "Runtime.evaluate", "params": map[string]any{
		"expression": "globalThis.__winuseShim.call(" + string(arg) + ")", "awaitPromise": true, "returnByValue": true,
	}})
	return t.ws.WriteText(inner)
}

// read turns the inspector's messages into the client's until the
// inspector closes.
func (t *electronTransport) read() {
	defer close(t.done)
	for {
		data, err := t.ws.ReadMessage(time.Time{})
		if err != nil {
			t.mu.Lock()
			t.err = err
			t.mu.Unlock()
			return
		}
		if out := t.translate(data); out != nil {
			select {
			case t.out <- out:
			case <-t.closing():
				return
			}
		}
	}
}

// closing is closed once Close ran.
func (t *electronTransport) closing() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		c := make(chan struct{})
		close(c)
		return c
	}
	return nil
}

// translate turns one inspector message into a client message: the answer
// to a client command, or an event the shim emitted; nil for anything else
// (the inspector's own events).
func (t *electronTransport) translate(data []byte) []byte {
	var m struct {
		ID     int64           `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *wireError      `json:"error"`
	}
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	if m.Method == "Runtime.bindingCalled" {
		var p struct {
			Name    string `json:"name"`
			Payload string `json:"payload"`
		}
		if json.Unmarshal(m.Params, &p) != nil || p.Name != emitBinding {
			return nil
		}
		return []byte(p.Payload)
	}
	if m.ID == 0 {
		return nil
	}
	t.mu.Lock()
	req, ok := t.pending[m.ID]
	delete(t.pending, m.ID)
	t.mu.Unlock()
	if !ok {
		return nil
	}
	reply := map[string]any{"id": req.id}
	if req.sessionID != "" {
		reply["sessionId"] = req.sessionID
	}
	switch {
	case m.Error != nil:
		reply["error"] = m.Error
	default:
		v, err := evalValue(m.Result)
		var answer struct {
			Result json.RawMessage `json:"result"`
			Error  *wireError      `json:"error"`
		}
		switch {
		case err != nil:
			reply["error"] = &wireError{Code: -32000, Message: err.Error()}
		case json.Unmarshal(v, &answer) != nil:
			reply["error"] = &wireError{Code: -32000, Message: "the App's shim answered " + string(v)}
		case answer.Error != nil:
			reply["error"] = answer.Error
		default:
			reply["result"] = answer.Result
		}
	}
	out, _ := json.Marshal(reply)
	return out
}

// ReadMessage returns the next client message, waiting until deadline
// (none when zero).
func (t *electronTransport) ReadMessage(deadline time.Time) ([]byte, error) {
	var timeout <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case msg := <-t.out:
		return msg, nil
	case <-t.done:
		select {
		case msg := <-t.out:
			return msg, nil
		default:
		}
		t.mu.Lock()
		err := t.err
		t.mu.Unlock()
		if err == nil {
			err = cdp.ErrClosed
		}
		return nil, err
	case <-timeout:
		return nil, context.DeadlineExceeded
	}
}

// Close detaches every webContents and closes the App's inspector (the
// shim's release), then the WebSocket.
func (t *electronTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.next++
	id := t.next
	t.mu.Unlock()
	release, _ := json.Marshal(map[string]any{"id": id, "method": "Runtime.evaluate", "params": map[string]any{
		"expression": "globalThis.__winuseShim && globalThis.__winuseShim.release()",
	}})
	t.ws.WriteText(release) // best effort: the App may be gone
	return t.ws.Close()
}
