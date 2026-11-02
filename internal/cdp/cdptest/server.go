// Package cdptest is a fake DevTools endpoint for offline tests: an
// httptest server that serves /json/version and upgrades to a WebSocket
// with its own server-side RFC 6455 framing, so the tests exercise winuse's
// client against frames it did not write. It never starts a browser and
// listens on an ephemeral loopback port only.
package cdptest

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

// Message is one request the fake received from a client.
type Message struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
}

// Handler answers one request. A nil result replies {}; ErrNoReply sends
// nothing (the test replies later through Conn.Reply, or never); a
// *ReplyError or any other error replies with a CDP error.
type Handler func(c *Conn, m Message) (result any, err error)

// ErrNoReply tells the fake not to answer a request now.
var ErrNoReply = errors.New("cdptest: no reply")

// ReplyError is a CDP error reply.
type ReplyError struct {
	Code    int
	Message string
}

func (e *ReplyError) Error() string { return e.Message }

// Target is one target the default Target.getTargets handler lists.
type Target struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
}

// Server is the fake endpoint. Set its fields before the first client
// connects.
type Server struct {
	srv *httptest.Server

	// Fragment splits every server message into frames of this many bytes
	// (0 sends each message in one frame).
	Fragment int
	// Ping sends a ping before every message and between fragments, so
	// the client must answer pongs while reassembling.
	Ping bool
	// Reject answers the upgrade with this status ("403 Forbidden")
	// instead of 101, as a browser refusing the connection would.
	Reject string
	// BadAccept answers the upgrade with a wrong Sec-WebSocket-Accept.
	BadAccept bool
	// Targets is what the default Target.getTargets handler lists.
	Targets []Target

	mu       sync.Mutex
	handlers map[string]Handler
	stall    func(m Message) bool
	received []Message
	conns    []*Conn
	hold     chan struct{}
	releases []func()
	upgrades int
	pongs    int
	unmasked int
}

// NewServer starts a fake endpoint on an ephemeral loopback port.
func NewServer() *Server {
	s := &Server{handlers: map[string]Handler{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	return s
}

// URL is the browser-level WebSocket URL, as /json/version reports it.
func (s *Server) URL() string {
	return "ws://" + s.srv.Listener.Addr().String() + "/devtools/browser/fake"
}

// Port is the fake's loopback port.
func (s *Server) Port() int {
	return s.srv.Listener.Addr().(*net.TCPAddr).Port
}

// Handle sets the handler for one method.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	s.handlers[method] = h
	s.mu.Unlock()
}

// Stall leaves unanswered every later request f returns true for (it is
// recorded, never answered), as a browser answers nothing that needs a
// Page's main thread while the Page shows a JavaScript dialog. Nil answers
// every request again.
func (s *Server) Stall(f func(m Message) bool) {
	s.mu.Lock()
	s.stall = f
	s.mu.Unlock()
}

// Hold makes every later upgrade wait (the handshake stays open, as Chrome
// 144+ holds it while its approval prompt is up) until release is called.
func (s *Server) Hold() (release func()) {
	ch := make(chan struct{})
	var once sync.Once
	release = func() {
		once.Do(func() {
			s.mu.Lock()
			if s.hold == ch {
				s.hold = nil
			}
			s.mu.Unlock()
			close(ch)
		})
	}
	s.mu.Lock()
	s.hold = ch
	s.releases = append(s.releases, release)
	s.mu.Unlock()
	return release
}

// Upgrades counts the WebSocket handshakes the fake has seen (including
// held and rejected ones): one per dial.
func (s *Server) Upgrades() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upgrades
}

// Pongs counts the pongs clients sent.
func (s *Server) Pongs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pongs
}

// Unmasked counts client frames that arrived without a mask (RFC 6455
// requires every client frame to be masked).
func (s *Server) Unmasked() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unmasked
}

// Received lists every request the fake has received, in order.
func (s *Server) Received() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.received...)
}

// Methods lists the methods of every received request, in order.
func (s *Server) Methods() []string {
	var out []string
	for _, m := range s.Received() {
		out = append(out, m.Method)
	}
	return out
}

// Event sends an event to every connected client.
func (s *Server) Event(sessionID, method string, params any) {
	s.mu.Lock()
	conns := append([]*Conn(nil), s.conns...)
	s.mu.Unlock()
	for _, c := range conns {
		c.Event(sessionID, method, params)
	}
}

// DropAll closes every client connection abruptly, as a browser that
// exits or restarts does.
func (s *Server) DropAll() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}

// Close drops every connection and stops the fake.
func (s *Server) Close() {
	s.mu.Lock()
	releases := s.releases
	s.mu.Unlock()
	for _, release := range releases {
		release()
	}
	s.DropAll()
	s.srv.Close()
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/json/version" {
		// Content-Length and a kept-alive connection, as DevTools serves it:
		// a client that reads until the connection closes would hang here.
		body, _ := json.Marshal(map[string]string{"Browser": "Fake/1.0", "webSocketDebuggerUrl": s.URL()})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	s.upgrades++
	hold := s.hold
	s.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-r.Context().Done():
			return
		}
	}
	if s.Reject != "" {
		code, _ := strconv.Atoi(strings.Fields(s.Reject)[0])
		http.Error(w, s.Reject, code)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return
	}
	nc, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	accept := acceptKey(r.Header.Get("Sec-WebSocket-Key"))
	if s.BadAccept {
		accept = acceptKey("wrong")
	}
	// Listed before the 101 goes out: a client that has its connection
	// must find it in conns, or a DropAll right after the dial misses it.
	// The write lock keeps an Event from going out ahead of the 101.
	c := &Conn{s: s, nc: nc, br: brw.Reader}
	c.wmu.Lock()
	s.mu.Lock()
	s.conns = append(s.conns, c)
	s.mu.Unlock()
	fmt.Fprintf(nc, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	c.wmu.Unlock()
	go c.serve()
}

func acceptKey(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}

// Conn is one client connection to the fake.
type Conn struct {
	s   *Server
	nc  net.Conn
	br  *bufio.Reader
	wmu sync.Mutex
}

// serve reads client frames until the connection ends, answering each
// request through its handler.
func (c *Conn) serve() {
	defer c.nc.Close()
	var msg []byte
	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return
		}
		switch opcode {
		case 0x8:
			c.writeFrame(true, 0x8, nil)
			return
		case 0x9:
			c.writeFrame(true, 0xA, payload)
			continue
		case 0xA:
			c.s.mu.Lock()
			c.s.pongs++
			c.s.mu.Unlock()
			continue
		}
		msg = append(msg, payload...)
		if !fin {
			continue
		}
		var m Message
		if json.Unmarshal(msg, &m) == nil {
			c.handle(m)
		}
		msg = nil
	}
}

func (c *Conn) handle(m Message) {
	s := c.s
	s.mu.Lock()
	s.received = append(s.received, m)
	h := s.handlers[m.Method]
	stall := s.stall
	s.mu.Unlock()
	if stall != nil && stall(m) {
		return
	}
	if h == nil {
		h = s.defaultHandler(m.Method)
	}
	result, err := h(c, m)
	switch {
	case errors.Is(err, ErrNoReply):
	case err != nil:
		var re *ReplyError
		if !errors.As(err, &re) {
			re = &ReplyError{Code: -32000, Message: err.Error()}
		}
		c.ReplyError(m.ID, m.SessionID, re.Code, re.Message)
	default:
		c.Reply(m.ID, m.SessionID, result)
	}
}

// defaultHandler answers what a browser always answers: attachToTarget
// with a session id derived from the target, getTargets with Targets, and
// anything else with {}.
func (s *Server) defaultHandler(method string) Handler {
	switch method {
	case "Target.attachToTarget":
		return func(c *Conn, m Message) (any, error) {
			var p struct {
				TargetID string `json:"targetId"`
			}
			json.Unmarshal(m.Params, &p)
			return map[string]string{"sessionId": "session-" + p.TargetID}, nil
		}
	case "Target.getTargets":
		return func(c *Conn, m Message) (any, error) {
			s.mu.Lock()
			targets := append([]Target{}, s.Targets...)
			s.mu.Unlock()
			return map[string]any{"targetInfos": targets}, nil
		}
	}
	return func(c *Conn, m Message) (any, error) { return nil, nil }
}

// Reply answers a request (sessionID echoes the request's session).
func (c *Conn) Reply(id int64, sessionID string, result any) {
	if result == nil {
		result = struct{}{}
	}
	msg := map[string]any{"id": id, "result": result}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	c.send(msg)
}

// ReplyError answers a request with a CDP error.
func (c *Conn) ReplyError(id int64, sessionID string, code int, message string) {
	msg := map[string]any{"id": id, "error": map[string]any{"code": code, "message": message}}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	c.send(msg)
}

// Event sends one event on this connection.
func (c *Conn) Event(sessionID, method string, params any) {
	if params == nil {
		params = struct{}{}
	}
	msg := map[string]any{"method": method, "params": params}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	c.send(msg)
}

// Close drops the connection without a close frame.
func (c *Conn) Close() { c.nc.Close() }

// send writes one message, fragmented and interleaved with pings as the
// server is configured.
func (c *Conn) send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.s.Ping {
		c.writeFrameLocked(true, 0x9, []byte("ping"))
	}
	size := c.s.Fragment
	if size <= 0 || size >= len(data) {
		c.writeFrameLocked(true, 0x1, data)
		return
	}
	opcode := byte(0x1)
	for len(data) > 0 {
		n := min(size, len(data))
		chunk := data[:n]
		data = data[n:]
		c.writeFrameLocked(len(data) == 0, opcode, chunk)
		opcode = 0x0
		if c.s.Ping && len(data) > 0 {
			c.writeFrameLocked(true, 0x9, []byte("mid"))
		}
	}
}

func (c *Conn) writeFrame(fin bool, opcode byte, payload []byte) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.writeFrameLocked(fin, opcode, payload)
}

// writeFrameLocked writes one unmasked server frame.
func (c *Conn) writeFrameLocked(fin bool, opcode byte, payload []byte) {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	n := len(payload)
	var hdr []byte
	switch {
	case n < 126:
		hdr = []byte{b0, byte(n)}
	case n < 65536:
		hdr = []byte{b0, 126, 0, 0}
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
	default:
		hdr = make([]byte, 10)
		hdr[0], hdr[1] = b0, 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	c.nc.Write(append(hdr, payload...))
}

// readFrame reads one client frame, unmasking it and counting unmasked
// ones.
func (c *Conn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(c.br, hdr[:]); err != nil {
		return
	}
	fin = hdr[0]&0x80 != 0
	opcode = hdr[0] & 0x0f
	masked := hdr[1]&0x80 != 0
	n := uint64(hdr[1] & 0x7f)
	switch n {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(ext[:])
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	} else {
		c.s.mu.Lock()
		c.s.unmasked++
		c.s.mu.Unlock()
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return
}
