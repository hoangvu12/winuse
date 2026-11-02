// Package cdp is winuse's Chrome DevTools Protocol client: a minimal,
// dependency-free RFC 6455 WebSocket client (ws.go) and a JSON-RPC client
// over any message transport (client.go) that matches responses to calls
// and routes events to per-session subscribers.
//
// Ported from use-browser v0.9.0 (ws.go, cdp.go, ext.go's msgConn; see
// third_party/use-browser/NOTICE). Unlike use-browser, which drops every
// event, the client keeps one reader goroutine per connection so the
// helper can hold a connection for its lifetime and subscribe to dialogs,
// downloads and auto-attach.
package cdp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Transport is what the CDP client needs from its connection: one JSON
// message out, one in. A WebSocket to a DevTools endpoint (WSConn) is one;
// ticket 02's extension bridge (length-prefixed frames over loopback TCP)
// is the other. This is use-browser's msgConn (ub/ext.go:86-93).
//
// The client calls WriteText from several goroutines but serializes the
// calls itself; ReadMessage is only ever called by the client's one reader
// goroutine, with a zero deadline (block until a message or an error).
// Close must unblock a pending ReadMessage.
type Transport interface {
	WriteText([]byte) error
	ReadMessage(deadline time.Time) ([]byte, error)
	Close() error
}

// MaxMessage bounds one reassembled message (a full accessibility tree of
// a large page is a few megabytes; a screenshot can be tens).
const MaxMessage = 256 << 20

// wsGUID is RFC 6455's fixed key suffix for Sec-WebSocket-Accept.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WSConn is a client WebSocket: text frames, fragmentation, ping/pong and
// close. No compression, no TLS: DevTools endpoints are loopback only.
type WSConn struct {
	conn net.Conn
	br   *bufio.Reader
	wmu  sync.Mutex // writes come from callers and from the reader's pongs
}

// DialWS opens a WebSocket to a ws:// URL. handshake bounds the opening
// handshake on top of ctx: a Chrome 144+ browser driven through its
// inspect toggle holds the handshake open until the user answers its
// "Allow remote debugging?" prompt, so the caller chooses how long to wait.
func DialWS(ctx context.Context, wsURL string, handshake time.Duration) (*WSConn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" {
		return nil, fmt.Errorf("unsupported scheme %q (only ws://)", u.Scheme)
	}
	host := u.Host
	if u.Port() == "" {
		host += ":80"
	}
	if handshake > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, handshake)
		defer cancel()
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	// A context that ends mid-handshake unblocks the read below.
	stop := context.AfterFunc(ctx, func() { conn.SetDeadline(time.Unix(1, 0)) })
	ws, err := handshakeWS(conn, u)
	if !stop() || err != nil {
		conn.Close()
		if err == nil || ctx.Err() != nil {
			return nil, fmt.Errorf("websocket handshake: %w", context.Cause(ctx))
		}
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return ws, nil
}

// handshakeWS sends the upgrade request and checks the 101 answer and its
// Sec-WebSocket-Accept.
func handshakeWS(conn net.Conn, u *url.URL) (*WSConn, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	keyB64 := base64.StdEncoding.EncodeToString(key)
	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, u.Host, keyB64)
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(conn, 64*1024)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket handshake: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		resp.Body.Close()
		return nil, &HandshakeError{Status: resp.Status}
	}
	if got, want := resp.Header.Get("Sec-WebSocket-Accept"), acceptKey(keyB64); got != want {
		return nil, fmt.Errorf("websocket handshake: Sec-WebSocket-Accept is %q, want %q", got, want)
	}
	return &WSConn{conn: conn, br: br}, nil
}

// HandshakeError is an upgrade the server answered with something other
// than 101, e.g. a browser that refused the debugging connection.
type HandshakeError struct{ Status string }

func (e *HandshakeError) Error() string { return "websocket handshake refused: " + e.Status }

// acceptKey is the Sec-WebSocket-Accept a server must answer for a key.
func acceptKey(key string) string {
	h := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h[:])
}

// Close sends a best-effort close frame and closes the socket, which
// unblocks a pending ReadMessage.
func (w *WSConn) Close() error {
	w.writeFrame(0x8, nil)
	return w.conn.Close()
}

// WriteText sends one text message.
func (w *WSConn) WriteText(payload []byte) error {
	return w.writeFrame(0x1, payload)
}

// writeFrame sends a single masked frame (client frames must be masked).
func (w *WSConn) writeFrame(opcode byte, payload []byte) error {
	var hdr []byte
	n := len(payload)
	switch {
	case n < 126:
		hdr = []byte{0x80 | opcode, 0x80 | byte(n)}
	case n < 65536:
		hdr = []byte{0x80 | opcode, 0x80 | 126, 0, 0}
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
	default:
		hdr = make([]byte, 10)
		hdr[0], hdr[1] = 0x80|opcode, 0x80|127
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	frame := make([]byte, 0, len(hdr)+4+n)
	frame = append(frame, hdr...)
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i&3])
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	_, err := w.conn.Write(frame)
	return err
}

// ReadMessage reads one complete message, reassembling fragments and
// answering pings. A zero deadline blocks until a message or an error. A
// close frame is answered and reported as io.EOF.
func (w *WSConn) ReadMessage(deadline time.Time) ([]byte, error) {
	w.conn.SetReadDeadline(deadline)
	defer w.conn.SetReadDeadline(time.Time{})
	var msg []byte
	for {
		var hdr [2]byte
		if _, err := io.ReadFull(w.br, hdr[:]); err != nil {
			return nil, err
		}
		fin := hdr[0]&0x80 != 0
		opcode := hdr[0] & 0x0f
		masked := hdr[1]&0x80 != 0
		n := uint64(hdr[1] & 0x7f)
		switch n {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(ext[:])
		}
		if n > MaxMessage || uint64(len(msg))+n > MaxMessage {
			return nil, fmt.Errorf("websocket message larger than %d bytes", MaxMessage)
		}
		// Servers must not mask, but unmasking costs nothing and keeps a
		// lenient peer working.
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.br, mask[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(w.br, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i&3]
			}
		}
		switch opcode {
		case 0x9: // ping -> pong
			if err := w.writeFrame(0xA, payload); err != nil {
				return nil, err
			}
			continue
		case 0xA: // pong
			continue
		case 0x8: // close: answer it, then report the end
			w.writeFrame(0x8, nil)
			return nil, io.EOF
		case 0x0, 0x1, 0x2: // continuation, text, binary
		default:
			return nil, fmt.Errorf("websocket: unknown opcode %#x", opcode)
		}
		msg = append(msg, payload...)
		if fin {
			return msg, nil
		}
	}
}

// ErrClosed is the error every pending and later call fails with once the
// connection is gone (the browser closed or restarted, or Close was
// called). errors.Is matches it through the wrapping.
var ErrClosed = errors.New("the browser connection closed")
