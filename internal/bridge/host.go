package bridge

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
	"unicode/utf8"
)

// HostConfig is what a native host needs from the machine. HostMain fills
// the real one; tests fake every field, so an offline test never touches
// the real bridges directory or a real browser.
type HostConfig struct {
	Dir    string // the bridges directory the bridge file goes in
	Origin string // the caller origin the browser passed (os.Args[1])
	PID    int    // this host's pid
	Exe    string // this host's image path

	// Browser names the browser process that started this host.
	Browser func() (pid int, exe string)
	// Listen opens the loopback listener helper connections arrive on.
	Listen func() (net.Listener, error)
	// Token makes the connection token; nil uses 16 random bytes.
	Token func() string
	// Log receives one line per event (the host's stdout belongs to the
	// browser); nil discards.
	Log io.Writer
	// Republish is how often the host checks that its bridge file is still
	// there and writes it again if not (0: republishEvery). A winuse that
	// took the host for gone deleted it, and without it no helper finds the
	// extension while the host keeps its port.
	Republish time.Duration
}

// republishEvery is HostConfig.Republish's default.
const republishEvery = 5 * time.Second

// How long the host waits on a helper connection.
const (
	authWait  = 5 * time.Second // the token frame after connecting
	stuckWait = 5 * time.Second // a helper not reading its replies is dropped after this
)

// host is one running native host.
type host struct {
	cfg   HostConfig
	out   io.Writer
	outMu sync.Mutex
	token string

	mu      sync.Mutex
	clients map[int]*hostClient
	nextID  int
}

// hostClient is one helper connection.
type hostClient struct {
	id    int
	conn  net.Conn
	queue chan []byte
	done  chan struct{}
	once  sync.Once
}

func (c *hostClient) close() {
	c.once.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

// hello is the extension's first message.
type hello struct {
	Hello struct {
		Browser  string `json:"browser"`
		Instance string `json:"instance"`
		Version  string `json:"version"`
		Build    string `json:"build"`
	} `json:"hello"`
}

// ServeHost runs a native host: the extension speaks native messaging on
// in and out, helper connections arrive on a loopback listener. It
// publishes its bridge file once the extension said hello and removes it
// on the way out. It returns when the extension's side ends (the browser
// closes stdin when the extension's port goes away: worker stopped,
// extension reloaded or browser closed); io.EOF there is a clean end.
func ServeHost(cfg HostConfig, in io.Reader, out io.Writer) error {
	if cfg.Origin != Origin {
		return fmt.Errorf("started by %q, which is not the winuse extension (%s)", cfg.Origin, Origin)
	}
	h := &host{cfg: cfg, out: out, clients: map[int]*hostClient{}}
	br := bufio.NewReaderSize(in, 64<<10)
	first, err := ReadFrame(br, 64<<10)
	if err != nil {
		return fmt.Errorf("no hello from the extension: %w", err)
	}
	var hi hello
	json.Unmarshal(first, &hi)
	if !validBrowser(hi.Hello.Browser) || !validInstance(hi.Hello.Instance) {
		return fmt.Errorf("a bad hello from the extension: %s", first)
	}

	ln, err := cfg.Listen()
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()
	h.token = newToken(cfg.Token)
	info := Info{
		PID: cfg.PID, Exe: cfg.Exe, Port: ln.Addr().(*net.TCPAddr).Port, Token: h.token,
		Browser: hi.Hello.Browser, Instance: hi.Hello.Instance, Version: hi.Hello.Version, Build: hi.Hello.Build,
	}
	if cfg.Browser != nil {
		info.BrowserPID, info.BrowserExe = cfg.Browser()
	}
	path, err := writeInfo(cfg.Dir, info)
	if err != nil {
		return fmt.Errorf("writing the bridge file: %w", err)
	}
	stop := make(chan struct{})
	defer func() {
		close(stop)
		os.Remove(path)
	}()
	h.logf("%s instance %s (extension %s, build %s) started by %s (pid %d), listening on %s",
		info.Browser, info.Instance, info.Version, info.Build, info.BrowserExe, info.BrowserPID, info.Addr())
	every := cfg.Republish
	if every <= 0 {
		every = republishEvery
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
					h.logf("the bridge file was removed while this host runs; writing it again")
					writeInfo(cfg.Dir, info)
				}
			}
		}
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go h.serveClient(conn)
		}
	}()

	defer h.closeClients()
	for {
		msg, err := ReadFrame(br, MaxFrame)
		if err != nil {
			if errors.Is(err, io.EOF) {
				h.logf("the extension went away")
				return nil
			}
			return fmt.Errorf("reading from the extension: %w", err)
		}
		var env struct {
			C int             `json:"c"`
			M json.RawMessage `json:"m"`
		}
		if json.Unmarshal(msg, &env) != nil || len(env.M) == 0 {
			continue
		}
		h.deliver(env.C, env.M)
	}
}

func newToken(gen func() string) string {
	if gen != nil {
		return gen()
	}
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *host) logf(format string, a ...any) {
	if h.cfg.Log != nil {
		fmt.Fprintf(h.cfg.Log, time.Now().Format("2006-01-02 15:04:05")+" pid "+fmt.Sprint(h.cfg.PID)+": "+format+"\n", a...)
	}
}

// deliver queues a message from the extension for its helper connection.
// A connection that stops reading is dropped rather than left to stall
// every other one.
func (h *host) deliver(id int, m []byte) {
	h.mu.Lock()
	c := h.clients[id]
	h.mu.Unlock()
	if c == nil {
		return // the connection closed; its late replies have nowhere to go
	}
	select {
	case c.queue <- m:
	case <-c.done:
	case <-time.After(stuckWait):
		h.logf("connection %d is not reading its replies; closing it", id)
		c.close()
	}
}

func (h *host) closeClients() {
	h.mu.Lock()
	cs := h.clients
	h.clients = map[int]*hostClient{}
	h.mu.Unlock()
	for _, c := range cs {
		c.close()
	}
}

// writeOut sends one native message to the extension.
func (h *host) writeOut(b []byte) error {
	h.outMu.Lock()
	defer h.outMu.Unlock()
	return WriteFrame(h.out, b)
}

// serveClient authenticates one helper connection and relays its commands
// to the extension until it closes.
func (h *host) serveClient(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReaderSize(conn, 64<<10)
	conn.SetReadDeadline(time.Now().Add(authWait))
	first, err := ReadFrame(br, 4<<10)
	conn.SetReadDeadline(time.Time{})
	if err != nil {
		return
	}
	var auth struct {
		Token string `json:"token"`
	}
	json.Unmarshal(first, &auth)
	if subtle.ConstantTimeCompare([]byte(auth.Token), []byte(h.token)) != 1 {
		h.logf("rejected a connection with a bad token")
		WriteFrame(conn, []byte(`{"bridge":"refused"}`))
		return
	}
	if err := WriteFrame(conn, []byte(`{"bridge":"ready"}`)); err != nil {
		return
	}

	h.mu.Lock()
	h.nextID++
	c := &hostClient{id: h.nextID, conn: conn, queue: make(chan []byte, 256), done: make(chan struct{})}
	h.clients[c.id] = c
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, c.id)
		h.mu.Unlock()
		c.close()
		h.writeOut(fmt.Appendf(nil, `{"c":%d,"close":true}`, c.id))
	}()

	go func() {
		for {
			select {
			case m := <-c.queue:
				if WriteFrame(conn, m) != nil {
					c.close()
					return
				}
			case <-c.done:
				return
			}
		}
	}()

	for {
		msg, err := ReadFrame(br, MaxFrame)
		if err != nil {
			return
		}
		if len(msg) > MaxCommand {
			// Answer it here: the extension never sees it.
			h.deliver(c.id, errorReply(msg, CodeTooLarge, fmt.Sprintf("the command is %d bytes; the winuse extension bridge carries at most %d", len(msg), MaxCommand)))
			continue
		}
		if err := h.toExtension(c.id, msg); err != nil {
			return
		}
	}
}

// toExtension sends one command to the extension: in one native message
// when it fits under the browser's 1 MB limit, else in chunks of
// ChunkSize the extension joins back ({c, chunk, last}).
func (h *host) toExtension(id int, msg []byte) error {
	if len(msg)+64 <= MaxToExtension {
		env := fmt.Appendf(nil, `{"c":%d,"m":`, id)
		env = append(env, msg...)
		return h.writeOut(append(env, '}'))
	}
	parts := splitChunks(msg, ChunkSize)
	for i, part := range parts {
		env, err := chunkEnvelope(id, part, i == len(parts)-1)
		if err != nil {
			return err
		}
		if err := h.writeOut(env); err != nil {
			return err
		}
	}
	return nil
}

// chunkEnvelope is one chunk's native message.
func chunkEnvelope(id int, part []byte, last bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(struct {
		C     int    `json:"c"`
		Chunk string `json:"chunk"`
		Last  bool   `json:"last"`
	}{id, string(part), last})
	if err != nil {
		return nil, err
	}
	b := bytes.TrimRight(buf.Bytes(), "\n")
	if len(b) > MaxToExtension {
		return nil, fmt.Errorf("a chunk of %d bytes is over the 1 MB native messaging limit", len(b))
	}
	return b, nil
}

// splitChunks cuts text into pieces of at most size bytes, never inside a
// UTF-8 sequence (a split rune would turn into U+FFFD when encoded).
func splitChunks(b []byte, size int) [][]byte {
	var out [][]byte
	for len(b) > 0 {
		n := min(size, len(b))
		for n < len(b) && n > 0 && !utf8.RuneStart(b[n]) {
			n--
		}
		if n == 0 {
			n = min(size, len(b))
		}
		out = append(out, b[:n])
		b = b[n:]
	}
	return out
}

// errorReply is a CDP error response to a command, built without the
// extension.
func errorReply(cmd []byte, code int, message string) []byte {
	var m struct {
		ID        *int64 `json:"id"`
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(cmd, &m)
	reply := map[string]any{"id": m.ID, "error": map[string]any{"code": code, "message": message}}
	if m.SessionID != "" {
		reply["sessionId"] = m.SessionID
	}
	b, _ := json.Marshal(reply)
	return b
}
