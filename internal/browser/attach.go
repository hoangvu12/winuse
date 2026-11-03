package browser

// The attach to a running Electron App (headless-max ticket 15, the user's
// decision: opt-in, the agent asks the user first, refused when the
// nodeCliInspect fuse is off). Measured in probe round 3 on Electron
// 45.2.1: the Node debug signal opened the main process's inspector on
// 127.0.0.1:9229 within 70 ms, the App's webContents.debugger carried
// trusted input with no window activation, and closing the inspector
// removed the port with the App still running.
//
// AttachElectron checks the App is Electron and publishes the Node debug
// handler (the fuse), sends the signal, finds the inspector among the
// loopback ports the App's own process listens on, and opens the
// connection at once, so a shim that does not install fails the attach
// rather than the next read. DetachApp, Manager.Close and a dropped
// connection all close the inspector again (electron.go).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// ModeAttach is a running Electron App winuse attached to.
const ModeAttach Mode = "attach"

// The attach's refusals: nothing was sent to the App.
var (
	ErrNotElectron    = errors.New("the Window's process is not an Electron App")
	ErrNodeInspectOff = errors.New("the App's nodeCliInspect fuse is off, so its Node.js inspector cannot be opened")
)

// inspectorWait bounds the wait for the inspector's port after the signal.
const inspectorWait = 5 * time.Second

// nodeInspectorPort is Node's default inspector port, tried first.
const nodeInspectorPort = 9229

// AttachElectron attaches to the running Electron App whose main process
// pid owns a Window (exePath is its executable) and registers the
// connection for that process. An App winuse launched with --cdp, or
// attached to already, is answered with its endpoint and nothing sent.
func (m *Manager) AttachElectron(ctx context.Context, pid uint32, exePath string) (AppEndpoint, error) {
	if ep, ok := m.appEndpoint(pid); ok {
		return ep, nil
	}
	exists := m.sys.Exists
	if !IsElectron(exePath, exists) {
		return AppEndpoint{}, ErrNotElectron
	}
	if m.sys.HasNodeDebug == nil || !m.sys.HasNodeDebug(pid) {
		return AppEndpoint{}, ErrNodeInspectOff
	}
	if m.sys.NodeDebug == nil || m.sys.ListeningPorts == nil || m.sys.InspectorWS == nil {
		return AppEndpoint{}, errors.New("this helper cannot attach to Apps")
	}
	if err := m.sys.NodeDebug(pid); err != nil {
		return AppEndpoint{}, fmt.Errorf("opening the App's inspector: %w", err)
	}
	port, ws, ok := m.awaitInspector(ctx, pid)
	if !ok {
		return AppEndpoint{}, fmt.Errorf("the App's inspector did not open on a loopback port of process %d within %s", pid, inspectorWait)
	}
	ep := AppEndpoint{Kind: AppElectron, Mode: ModeAttach, PID: pid, Owner: pid, Port: port, WSURL: ws}
	m.mu.Lock()
	if m.apps == nil {
		m.apps = map[uint32]AppEndpoint{}
	}
	m.apps[pid] = ep
	m.mu.Unlock()
	if _, err := m.dial(ctx, m.appDialEndpoint(ep)); err != nil {
		m.mu.Lock()
		delete(m.apps, pid)
		m.mu.Unlock()
		return AppEndpoint{}, err
	}
	return ep, nil
}

// awaitInspector polls the process's loopback listeners for a Node
// inspector, 9229 first.
func (m *Manager) awaitInspector(ctx context.Context, pid uint32) (int, string, bool) {
	deadline := m.appNow().Add(inspectorWait)
	for {
		ports := m.sys.ListeningPorts(pid)
		sort.Slice(ports, func(i, j int) bool {
			return ports[i] == nodeInspectorPort || ports[j] != nodeInspectorPort && ports[i] < ports[j]
		})
		for _, port := range ports {
			if ws, err := m.sys.InspectorWS(ctx, port); err == nil {
				return port, ws, true
			}
		}
		if ctx.Err() != nil || !m.appNow().Before(deadline) {
			return 0, "", false
		}
		m.appSleep(appPollEvery)
	}
}

// appDialEndpoint is the Endpoint a registered App is dialed through:
// keyed by the process that owns its Windows. An attached App's transport
// is the Node inspector with winuse's shim (electron.go).
func (m *Manager) appDialEndpoint(app AppEndpoint) Endpoint {
	ep := Endpoint{Browser: app.Kind, Mode: ModeApp, WSURL: app.WSURL, Port: app.Port, PID: app.PID}
	if app.Mode == ModeAttach {
		ep.Mode = ModeAttach
		ep.Open = func(ctx context.Context, handshake time.Duration) (cdp.Transport, error) {
			ws, err := m.sys.Dial(ctx, app.WSURL, handshake)
			if err != nil {
				return nil, err
			}
			return openElectron(ws)
		}
	}
	return ep
}

// ErrNotAttached is a detach on a Window whose App winuse did not attach
// to.
var ErrNotAttached = errors.New("winuse is not attached to the Window's App")

// DetachApp ends winuse's attach to the App whose Windows belong to pid:
// the connection closes, which detaches its webContents and closes its
// inspector. An App winuse launched with --cdp keeps its port until it
// exits, so it cannot be detached.
func (m *Manager) DetachApp(pid uint32) error {
	m.mu.Lock()
	ep, ok := m.apps[pid]
	if !ok || ep.Mode != ModeAttach {
		m.mu.Unlock()
		return ErrNotAttached
	}
	delete(m.apps, pid)
	key := Endpoint{PID: pid}.key()
	c := m.conns[key]
	delete(m.conns, key)
	m.mu.Unlock()
	if c != nil {
		c.Client.Close()
	}
	return nil
}
