package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// dialWait bounds connecting to a host and its token answer: the host is
// a local process, so anything slower is a dead one.
const dialWait = 3 * time.Second

// ErrTokenRejected means the host refused the token: the bridge file is
// from an older host, or someone else's.
var ErrTokenRejected = errors.New("the winuse native host rejected the connection token")

// Conn is the helper's side of a bridge, a cdp.Transport over the host's
// loopback link. Session ids are CDP's: a tab's session id is its target
// id, a child session (an out-of-process iframe) keeps the id Chrome gave
// it, and both go on the wire as sessionId, as on a DevTools WebSocket.
//
// It applies Refusal before anything leaves the helper: a refused or
// oversized command is answered with a CDP error right here, so the
// cdp.Client sees an ordinary error response.
type Conn struct {
	nc  net.Conn
	wmu sync.Mutex

	mu     sync.Mutex
	queue  [][]byte // replies and events, network and local, in order
	err    error    // why the link ended
	closed bool
	notify chan struct{}

	closeOnce sync.Once
}

// Dial connects to a native host and presents its token.
func Dial(ctx context.Context, addr, token string) (*Conn, error) {
	deadline := time.Now().Add(dialWait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	d := net.Dialer{Deadline: deadline}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("the winuse extension bridge on %s: %w", addr, err)
	}
	auth, _ := json.Marshal(map[string]string{"token": token})
	nc.SetDeadline(deadline)
	if err := WriteFrame(nc, auth); err != nil {
		nc.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(nc, 64<<10)
	answer, err := ReadFrame(br, 4<<10)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("the winuse extension bridge on %s did not answer: %w", addr, err)
	}
	var a struct {
		Bridge string `json:"bridge"`
	}
	json.Unmarshal(answer, &a)
	if a.Bridge != "ready" {
		nc.Close()
		return nil, ErrTokenRejected
	}
	nc.SetDeadline(time.Time{})
	c := &Conn{nc: nc, notify: make(chan struct{}, 1)}
	go c.readLoop(br)
	return c, nil
}

func (c *Conn) readLoop(br *bufio.Reader) {
	for {
		b, err := ReadFrame(br, MaxFrame)
		if err != nil {
			c.fail(err)
			return
		}
		c.push(b)
	}
}

func (c *Conn) push(b []byte) {
	c.mu.Lock()
	c.queue = append(c.queue, b)
	c.mu.Unlock()
	c.wake()
}

func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	c.wake()
}

func (c *Conn) wake() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// WriteText sends one CDP command, or answers it locally when it is
// refused or too large.
func (c *Conn) WriteText(b []byte) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return net.ErrClosed
	}
	var m struct {
		ID        *int64 `json:"id"`
		Method    string `json:"method"`
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(b, &m) == nil && m.ID != nil {
		if code, r := Refusal(m.Method, m.SessionID); code != 0 {
			c.push(errorReply(b, code, r))
			return nil
		}
	}
	if len(b) > MaxCommand {
		c.push(errorReply(b, CodeTooLarge, fmt.Sprintf("the command is %d bytes; the winuse extension bridge carries at most %d", len(b), MaxCommand)))
		return nil
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return WriteFrame(c.nc, b)
}

// ReadMessage returns the next reply or event. A zero deadline waits
// until one arrives or the link ends; Close unblocks it.
func (c *Conn) ReadMessage(deadline time.Time) ([]byte, error) {
	var timeout <-chan time.Time
	if !deadline.IsZero() {
		t := time.NewTimer(time.Until(deadline))
		defer t.Stop()
		timeout = t.C
	}
	for {
		c.mu.Lock()
		switch {
		case c.closed:
			c.mu.Unlock()
			return nil, net.ErrClosed
		case len(c.queue) > 0:
			b := c.queue[0]
			c.queue[0] = nil
			c.queue = c.queue[1:]
			c.mu.Unlock()
			return b, nil
		case c.err != nil:
			err := c.err
			c.mu.Unlock()
			return nil, err
		}
		c.mu.Unlock()
		select {
		case <-c.notify:
		case <-timeout:
			return nil, os.ErrDeadlineExceeded
		}
	}
}

// Close ends the link; a pending ReadMessage returns at once.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		c.wake()
		err = c.nc.Close()
	})
	return err
}
