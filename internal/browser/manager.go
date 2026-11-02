package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// ApprovalWait is how long a command holds a pending handshake before it
// fails with AwaitingApproval (agent-browser holds it 20 s,
// ab/cli/src/native/cdp/chrome.rs:1243-1248). The dial itself keeps going
// for approvalWindow, so a retry after the user clicks Allow joins it
// instead of raising a second prompt.
const (
	ApprovalWait   = 20 * time.Second
	approvalWindow = 2 * time.Minute
)

// Manager holds the helper's Browser Connections: one connection per
// browser process for the helper's lifetime, reused by every command, and
// dialed again when the browser restarted (the old connection's reader
// sees the socket end). Several processes of one browser are held at once
// (the user's Chrome through the extension beside a Chrome winuse
// launched). At most one dial per process is in flight, so a Chrome 144+
// browser shows its "Allow remote debugging?" prompt once per helper, not
// once per command.
type Manager struct {
	sys *System

	// ApprovalWait and the dial window are fields so tests shrink them.
	ApprovalWait   time.Duration
	approvalWindow time.Duration

	mu     sync.Mutex
	conns  map[string]*Connection  // by Endpoint.key: the browser process
	dials  map[string]*pendingDial // the one dial in flight per process
	closed bool
}

type pendingDial struct {
	ep   Endpoint
	done chan struct{}
	conn *Connection
	err  error
}

// NewManager builds a manager over a System (Live for the helper, fakes
// in tests). Nothing is probed or dialed until a command asks.
func NewManager(sys *System) *Manager {
	return &Manager{
		sys:            sys,
		ApprovalWait:   ApprovalWait,
		approvalWindow: approvalWindow,
		conns:          map[string]*Connection{},
		dials:          map[string]*pendingDial{},
	}
}

// Connection is one live Browser Connection.
type Connection struct {
	Browser string // short name
	Mode    Mode
	WSURL   string
	Port    int
	PID     uint32 // the browser process that owns the endpoint
	Dir     string
	Client  *cdp.Client
}

// Target is one page target of a connection.
type Target struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
}

// Pages lists the connection's page targets, browser-internal pages left
// out (ub/cdp.go:395-413).
func (c *Connection) Pages(ctx context.Context) ([]Target, error) {
	res, err := c.Client.Call(ctx, "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		TargetInfos []Target `json:"targetInfos"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	var pages []Target
	for _, t := range r.TargetInfos {
		if t.Type == "page" && !IsInternal(t.URL) {
			pages = append(pages, t)
		}
	}
	return pages, nil
}

// AttachPage attaches a flattened session to a page target and prepares
// it for Headless input:
//
//   - Emulation.setFocusEmulationEnabled: an occluded window or a
//     background tab throttles its renderer and silently drops synthetic
//     input, while Runtime.evaluate keeps working; forcing focus keeps
//     the input pipeline alive (ub/cdp.go:431-437). Errors are ignored:
//     internal pages and old browsers lack it, and commands still work.
//   - Runtime.runIfWaitingForDebugger: a target paused for the debugger
//     on attach would never load otherwise (agent-browser's changelog,
//     ab/docs/content/docs/changelog.mdx:740). Ignored when not paused.
//
// It never calls Page.bringToFront or Target.activateTarget: attaching
// must not take the user's foreground.
func (c *Connection) AttachPage(ctx context.Context, targetID string) (string, error) {
	res, err := c.Client.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true})
	if err != nil {
		return "", err
	}
	var r struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(res, &r) != nil || r.SessionID == "" {
		return "", fmt.Errorf("attaching to %s: no session id returned", targetID)
	}
	// Both bounded together: a page that shows a JavaScript dialog answers
	// them only once it closes, and the browser keeps them queued, in
	// order, ahead of anything sent to the session later.
	sctx, cancel := context.WithTimeout(ctx, attachSetupTimeout)
	defer cancel()
	c.Client.CallSession(sctx, r.SessionID, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true})
	c.Client.CallSession(sctx, r.SessionID, "Runtime.runIfWaitingForDebugger", nil)
	return r.SessionID, nil
}

// attachSetupTimeout bounds AttachPage's two setup calls together.
const attachSetupTimeout = time.Second

// Connection returns the live connection to a browser (name, else the
// pin, else the one debuggable browser), dialing it when there is none or
// the old one ended. The browser commands call this; a command that names
// a Window calls WindowConnection instead.
func (m *Manager) Connection(ctx context.Context, name string) (*Connection, error) {
	want, err := m.target(name)
	if err != nil {
		return nil, err
	}
	if c := m.live(want); c != nil {
		return c, nil
	}
	ep, err := m.sys.discover(ctx, want)
	if err != nil {
		return nil, err
	}
	return m.dial(ctx, ep)
}

// WindowConnection returns the live connection to the browser process
// that owns a Window (pid, the Window's process: Chromium's top-level
// windows belong to its browser process), dialing it when there is none
// or the old one ended. It is how ticket 04's Window State, the page
// Actions and the tab commands reach a Window's Page: the pin does not
// apply, since a Window belongs to exactly one process. A pid of 0
// (unknown) falls back to Connection.
func (m *Manager) WindowConnection(ctx context.Context, name string, pid uint32) (*Connection, error) {
	if pid == 0 {
		return m.Connection(ctx, name)
	}
	m.mu.Lock()
	c := m.conns[Endpoint{PID: pid}.key()]
	m.mu.Unlock()
	if c != nil && c.Client.Alive() {
		return c, nil
	}
	ep, err := m.sys.discoverFor(ctx, normalizeName(name), pid)
	if err != nil {
		return nil, err
	}
	return m.dial(ctx, ep)
}

// live is the held, still-open connection for want (any browser for ""),
// when there is exactly one. With several (the user's Chrome and a Chrome
// winuse launched), discovery picks as it always did and dial reuses the
// held connection it names.
func (m *Manager) live(want string) *Connection {
	open := m.open(want)
	if len(open) == 1 {
		return open[0]
	}
	return nil
}

// open lists the held, still-open connections to want (every browser for
// ""), by browser and pid.
func (m *Manager) open(want string) []*Connection {
	m.mu.Lock()
	var out []*Connection
	for _, c := range m.conns {
		if c.Client.Alive() && (want == "" || c.Browser == want) {
			out = append(out, c)
		}
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Browser != out[j].Browser {
			return out[i].Browser < out[j].Browser
		}
		return out[i].PID < out[j].PID
	})
	return out
}

// dial returns the connection for a verified endpoint: the held one when
// it is still open on that endpoint, else the result of the one dial in
// flight for that browser process, starting it when there is none. The
// caller waits ApprovalWait at most; a dial still held then (the approval
// prompt) fails with AwaitingApproval and keeps going in the background.
func (m *Manager) dial(ctx context.Context, ep Endpoint) (*Connection, error) {
	key := ep.key()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, cdp.ErrClosed
	}
	if c := m.conns[key]; c != nil && c.Client.Alive() && c.WSURL == ep.WSURL {
		m.mu.Unlock()
		return c, nil
	}
	pd := m.dials[key]
	if pd == nil {
		pd = &pendingDial{ep: ep, done: make(chan struct{})}
		m.dials[key] = pd
		go m.runDial(pd)
	}
	m.mu.Unlock()
	return m.await(ctx, pd)
}

// await waits for a pending dial within ApprovalWait and the caller's
// context.
func (m *Manager) await(ctx context.Context, pd *pendingDial) (*Connection, error) {
	timer := time.NewTimer(m.ApprovalWait)
	defer timer.Stop()
	select {
	case <-pd.done:
		return pd.conn, pd.err
	case <-timer.C:
	case <-ctx.Done():
	}
	return nil, &Problem{Kind: AwaitingApproval, Browser: pd.ep.Browser}
}

// runDial performs one dial in the background, bounded by the approval
// window rather than by any one request.
func (m *Manager) runDial(pd *pendingDial) {
	ctx, cancel := context.WithTimeout(context.Background(), m.approvalWindow)
	defer cancel()
	var t cdp.Transport
	var err error
	if pd.ep.Open != nil {
		t, err = pd.ep.Open(ctx, m.approvalWindow)
	} else {
		t, err = m.sys.Dial(ctx, pd.ep.WSURL, m.approvalWindow)
	}
	var conn *Connection
	if err == nil {
		ep := pd.ep
		conn = &Connection{Browser: ep.Browser, Mode: ep.Mode, WSURL: ep.WSURL, Port: ep.Port, PID: ep.PID, Dir: ep.Dir, Client: cdp.NewClient(t)}
	} else {
		err = dialProblem(pd.ep, err)
	}
	key := pd.ep.key()
	m.mu.Lock()
	if m.dials[key] == pd {
		delete(m.dials, key)
	}
	var old *Connection
	if conn != nil {
		if m.closed {
			conn.Client.Close()
			conn, err = nil, cdp.ErrClosed
		} else {
			old = m.conns[key]
			m.conns[key] = conn
			// A browser that restarted is a new process: its old
			// connection has ended and is forgotten here.
			for k, c := range m.conns {
				if k != key && !c.Client.Alive() {
					delete(m.conns, k)
				}
			}
		}
	}
	pd.conn, pd.err = conn, err
	m.mu.Unlock()
	close(pd.done)
	if old != nil && old != conn {
		old.Client.Close()
	}
}

// dialProblem names why a dial failed: a refused handshake, the approval
// window running out, or an endpoint that stopped answering.
func dialProblem(ep Endpoint, err error) error {
	if ep.Mode == ModeExtension {
		return &Problem{Kind: ExtUnreachable, Browser: ep.Browser, Detail: err.Error()}
	}
	var he *cdp.HandshakeError
	switch {
	case errors.As(err, &he):
		return &Problem{Kind: Refused, Browser: ep.Browser, Port: ep.Port, Detail: he.Status}
	case errors.Is(err, context.DeadlineExceeded):
		return &Problem{Kind: AwaitingApproval, Browser: ep.Browser, Port: ep.Port}
	}
	return &Problem{Kind: Unreachable, Browser: ep.Browser, Port: ep.Port, Detail: err.Error()}
}

// drop closes and forgets the held connections to a browser that match
// (every one for nil).
func (m *Manager) drop(name string, match func(c *Connection) bool) {
	m.mu.Lock()
	var gone []*Connection
	for k, c := range m.conns {
		if c.Browser == name && (match == nil || match(c)) {
			gone = append(gone, c)
			delete(m.conns, k)
		}
	}
	m.mu.Unlock()
	for _, c := range gone {
		c.Client.Close()
	}
}

// dropEndpoint closes and forgets the held connection to an endpoint's
// browser process.
func (m *Manager) dropEndpoint(ep Endpoint) {
	m.mu.Lock()
	c := m.conns[ep.key()]
	delete(m.conns, ep.key())
	m.mu.Unlock()
	if c != nil {
		c.Client.Close()
	}
}

// held is a held connection to a browser, open or not (the one with the
// lowest pid when there are several).
func (m *Manager) held(name string) *Connection {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out *Connection
	for _, c := range m.conns {
		if c.Browser == name && (out == nil || c.PID < out.PID) {
			out = c
		}
	}
	return out
}

// Close ends every held connection; later commands fail. The helper calls
// it on exit.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	conns := m.conns
	m.conns = map[string]*Connection{}
	m.mu.Unlock()
	for _, c := range conns {
		c.Client.Close()
	}
	return nil
}

// target resolves which browser a command means: the name given, else the
// pin, else "" (whichever is debuggable). A named or pinned browser must
// be known and installed.
func (m *Manager) target(name string) (string, error) {
	name = normalizeName(name)
	if name != "" {
		b, err := m.installed(name, false)
		return b.Name, err
	}
	pin := m.sys.Pin()
	if pin == "" {
		return "", nil
	}
	b, err := m.installed(pin, true)
	return b.Name, err
}

// installed finds an installed browser by short name.
func (m *Manager) installed(name string, pinned bool) (Browser, error) {
	if !Known(name) {
		return Browser{}, &Problem{Kind: UnknownBrowser, Browser: name}
	}
	found := m.sys.detect()
	if len(found) == 0 {
		return Browser{}, &Problem{Kind: NoBrowser}
	}
	var names []string
	for _, b := range found {
		if b.Name == name {
			return b, nil
		}
		names = append(names, b.Name)
	}
	return Browser{}, &Problem{Kind: NotInstalled, Browser: name, Pinned: pinned, Detail: strings.Join(names, ", ")}
}

// pick resolves the browser a lifecycle command acts on: the name, else
// the pin, else the first running browser (preferRunning) or the first
// installed one (ub/browsers.go:348-406).
func (m *Manager) pick(name string, preferRunning bool) (Browser, error) {
	want, err := m.target(name)
	if err != nil {
		return Browser{}, err
	}
	if want != "" {
		return m.installed(want, false)
	}
	found := m.sys.detect()
	if len(found) == 0 {
		return Browser{}, &Problem{Kind: NoBrowser}
	}
	if preferRunning {
		for _, b := range found {
			if m.sys.running(b) {
				return b, nil
			}
		}
	}
	return found[0], nil
}

func normalizeName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }
