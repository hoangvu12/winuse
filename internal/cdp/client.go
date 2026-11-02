package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultTimeout bounds a call whose context has no deadline (use-browser's
// defaultTimeout, ub/cdp.go:34).
const DefaultTimeout = 15 * time.Second

// Client is a CDP JSON-RPC client over one Transport, with flattened
// sessions: a page or frame session is addressed by the sessionId on each
// message, all over the one browser-level connection. One reader goroutine
// matches responses to pending calls by id and routes events to their
// subscribers; calls are safe from any number of goroutines. When the
// connection ends, every pending and later call fails with ErrClosed.
type Client struct {
	t Transport

	writeMu sync.Mutex // one message on the wire at a time

	mu      sync.Mutex
	nextID  int64
	pending map[int64]*pendingCall
	subs    map[*Subscription]struct{}
	err     error // why the connection ended; nil while open

	done chan struct{}
}

type pendingCall struct {
	method    string
	sessionID string
	ch        chan callResult // buffered 1: the reader never blocks on it
}

type callResult struct {
	raw json.RawMessage
	err error
}

// Error is a CDP error response to a call.
type Error struct {
	Method  string
	Code    int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Method, e.Message) }

// Event is one CDP event: its method, the session it came from ("" for
// the browser-level connection) and its params.
type Event struct {
	Method    string
	SessionID string
	Params    json.RawMessage
}

// wireMessage is any message from the browser: a response (ID set) or an
// event (Method set).
type wireMessage struct {
	ID        *int64          `json:"id"`
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Params    json.RawMessage `json:"params"`
	Result    json.RawMessage `json:"result"`
	Error     *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// NewClient starts a client over an open transport. The client owns the
// transport from here on: Close closes it.
func NewClient(t Transport) *Client {
	c := &Client{
		t:       t,
		pending: map[int64]*pendingCall{},
		subs:    map[*Subscription]struct{}{},
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// Call sends a browser-level command (no session) and waits for its result.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.CallSession(ctx, "", method, params)
}

// CallSession sends a command to a flattened session ("" is the browser)
// and waits for its result, ctx's end, or the connection's end. A context
// without a deadline gets DefaultTimeout.
func (c *Client) CallSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
	if params == nil {
		params = struct{}{}
	}
	call := &pendingCall{method: method, sessionID: sessionID, ch: make(chan callResult, 1)}
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = call
	c.mu.Unlock()

	msg := map[string]any{"id": id, "method": method, "params": params}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	data, err := json.Marshal(msg)
	if err != nil {
		c.forget(id)
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	c.writeMu.Lock()
	err = c.t.WriteText(data)
	c.writeMu.Unlock()
	if err != nil {
		c.forget(id)
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	select {
	case r := <-call.ch:
		return r.raw, r.err
	case <-ctx.Done():
		c.forget(id)
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

// forget drops a pending call whose caller gave up; a late response is
// then ignored.
func (c *Client) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// Done is closed once the connection has ended.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err reports why the connection ended (wrapping ErrClosed), or nil while
// it is open.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Alive reports whether the connection is still open.
func (c *Client) Alive() bool { return c.Err() == nil }

// Close ends the connection: pending calls fail with ErrClosed and every
// subscription's channel closes once its queued events are read.
func (c *Client) Close() error {
	c.shutdown(ErrClosed)
	err := c.t.Close()
	<-c.done
	return err
}

// readLoop is the one reader: it runs until the transport fails.
func (c *Client) readLoop() {
	defer close(c.done)
	for {
		data, err := c.t.ReadMessage(time.Time{})
		if err != nil {
			c.shutdown(fmt.Errorf("%w: %v", ErrClosed, err))
			return
		}
		var m wireMessage
		if json.Unmarshal(data, &m) != nil {
			continue // not CDP; ignore it like use-browser does
		}
		if m.ID != nil {
			c.resolve(*m.ID, &m)
			continue
		}
		if m.Method != "" {
			c.dispatch(Event{Method: m.Method, SessionID: m.SessionID, Params: m.Params})
		}
	}
}

// resolve hands a response to its pending call.
func (c *Client) resolve(id int64, m *wireMessage) {
	c.mu.Lock()
	call := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if call == nil {
		return // the caller gave up
	}
	if m.Error != nil {
		call.ch <- callResult{err: &Error{Method: call.method, Code: m.Error.Code, Message: m.Error.Message}}
		return
	}
	call.ch <- callResult{raw: m.Result}
}

// dispatch queues an event on every matching subscription. A session the
// browser detached fails its pending calls at once instead of leaving them
// to time out.
func (c *Client) dispatch(ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ev.Method == "Target.detachedFromTarget" {
		var p struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(ev.Params, &p) == nil && p.SessionID != "" {
			for id, call := range c.pending {
				if call.sessionID == p.SessionID {
					delete(c.pending, id)
					call.ch <- callResult{err: fmt.Errorf("%s: %w", call.method, ErrSessionDetached)}
				}
			}
		}
	}
	for s := range c.subs {
		if s.matches(ev) {
			s.push(ev)
		}
	}
}

// ErrSessionDetached fails the pending calls of a session the browser
// detached (the tab closed or navigated to another process).
var ErrSessionDetached = errors.New("the page session was detached")

// shutdown records why the connection ended, fails every pending call and
// ends every subscription. Only the first reason counts.
func (c *Client) shutdown(reason error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = reason
	for id, call := range c.pending {
		delete(c.pending, id)
		call.ch <- callResult{err: fmt.Errorf("%s: %w", call.method, reason)}
	}
	for s := range c.subs {
		delete(c.subs, s)
		s.end()
	}
}

// Subscribe starts receiving the events of one session ("" for the
// browser-level connection) and one method ("" for every method of that
// session). Events are queued without bound, so a slow reader never
// stalls the connection or loses a dialog; Close the subscription when
// done. On a closed client the subscription is already ended.
func (c *Client) Subscribe(sessionID, method string) *Subscription {
	s := &Subscription{
		c:         c,
		sessionID: sessionID,
		method:    method,
		notify:    make(chan struct{}, 1),
		stop:      make(chan struct{}),
		out:       make(chan Event),
	}
	go s.pump()
	c.mu.Lock()
	if c.err != nil {
		s.end()
	} else {
		c.subs[s] = struct{}{}
	}
	c.mu.Unlock()
	return s
}

// Subscription delivers one filter's events in arrival order.
type Subscription struct {
	c         *Client
	sessionID string
	method    string

	mu       sync.Mutex
	queue    []Event
	ended    bool // the client is gone: deliver what is queued, then close
	notify   chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
	out      chan Event
}

// Events is the channel the subscription's events arrive on. It closes
// after Close, or once the connection ended and the queue is drained.
func (s *Subscription) Events() <-chan Event { return s.out }

// Close stops the subscription and discards anything still queued.
func (s *Subscription) Close() {
	s.c.mu.Lock()
	delete(s.c.subs, s)
	s.c.mu.Unlock()
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *Subscription) matches(ev Event) bool {
	return ev.SessionID == s.sessionID && (s.method == "" || ev.Method == s.method)
}

func (s *Subscription) push(ev Event) {
	s.mu.Lock()
	s.queue = append(s.queue, ev)
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *Subscription) end() {
	s.mu.Lock()
	s.ended = true
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// pump moves queued events to the output channel, one at a time.
func (s *Subscription) pump() {
	defer close(s.out)
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			ended := s.ended
			s.mu.Unlock()
			if ended {
				return
			}
			select {
			case <-s.notify:
			case <-s.stop:
				return
			}
			continue
		}
		ev := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		select {
		case s.out <- ev:
		case <-s.stop:
			return
		}
	}
}
