package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// fakeListener hands the server connections pushed by the test.
type fakeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
	addr   fakeAddr
}

type fakeAddr struct{}

func (fakeAddr) Network() string { return "fake" }
func (fakeAddr) String() string  { return "fake" }

func newFakeListener() *fakeListener {
	return &fakeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *fakeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *fakeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *fakeListener) Addr() net.Addr { return l.addr }

// push connects a client conn to the listener and returns the client side.
func (l *fakeListener) push() net.Conn {
	server, client := net.Pipe()
	l.conns <- server
	return client
}

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// call performs one full client exchange against the server.
func call(client net.Conn, req *protocol.Request) (*protocol.Response, error) {
	if err := protocol.WriteLine(client, req); err != nil {
		return nil, err
	}
	var resp protocol.Response
	if err := protocol.ReadLine(bufio.NewReader(client), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func testRequest(cmd string) *protocol.Request {
	return &protocol.Request{V: protocol.Version, Ver: "0.1.0", Cmd: cmd}
}

func echoCommand(out string) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		return out, nil
	}
}

func failingCommand() Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		return "", Fail("window 123 gone", "read the Window State again")
	}
}

func startServer(t *testing.T, idle time.Duration, commands map[string]Command) (*Server, *fakeListener) {
	t.Helper()
	s := New("0.1.0", idle, protocol.RequestTimeout, commands, discardLogger())
	ln := newFakeListener()
	t.Cleanup(func() { ln.Close() })
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	t.Cleanup(func() {
		s.Shutdown()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	return s, ln
}

func TestServeServesRequest(t *testing.T) {
	_, ln := startServer(t, time.Minute, map[string]Command{
		"list-windows": echoCommand("101\tNotepad\tUntitled\n"),
	})
	client := ln.push()
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))

	resp, err := call(client, testRequest("list-windows"))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !resp.OK || resp.Out != "101\tNotepad\tUntitled\n" {
		t.Fatalf("response: %+v", resp)
	}
}

func TestServeUnknownCommand(t *testing.T) {
	_, ln := startServer(t, time.Minute, map[string]Command{})
	client := ln.push()
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))

	resp, err := call(client, testRequest("nope"))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.OK || resp.Err == "" || resp.Exit != 1 {
		t.Fatalf("want failure response, got %+v", resp)
	}
}

func TestServeCommandErrorCarriesHintAndExit(t *testing.T) {
	_, ln := startServer(t, time.Minute, map[string]Command{"boom": failingCommand()})
	client := ln.push()
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))

	resp, err := call(client, testRequest("boom"))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.OK || resp.Err != "window 123 gone" || resp.Hint != "read the Window State again" || resp.Exit != 1 {
		t.Fatalf("response: %+v", resp)
	}
}

func TestServeRejectsForeignProtocolVersion(t *testing.T) {
	_, ln := startServer(t, time.Minute, map[string]Command{"x": echoCommand("")})
	client := ln.push()
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))

	resp, err := call(client, &protocol.Request{V: 99, Ver: "0.1.0", Cmd: "x"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.OK || resp.Stale {
		t.Fatalf("want a plain version error, got %+v", resp)
	}
	if resp.Err == "" {
		t.Fatal("error message must explain the version mismatch")
	}
}

func TestServeStaleHelperExits(t *testing.T) {
	// A CLI from a different build makes the helper exit itself after
	// answering, so the CLI can restart a current one.
	s := New("0.1.0", time.Hour, protocol.RequestTimeout, map[string]Command{"x": echoCommand("")}, discardLogger())
	ln := newFakeListener()
	defer ln.Close()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()

	client := ln.push()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	resp, err := call(client, &protocol.Request{V: protocol.Version, Ver: "9.9.9", Cmd: "x"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.OK || !resp.Stale {
		t.Fatalf("want stale response, got %+v", resp)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil (clean exit)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not exit after a stale exchange")
	}
}

func TestServeIdleExits(t *testing.T) {
	// No connections: the server exits by itself after the idle period.
	s := New("0.1.0", 80*time.Millisecond, protocol.RequestTimeout, map[string]Command{"x": echoCommand("")}, discardLogger())
	ln := newFakeListener()
	defer ln.Close()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("idle helper did not exit")
	}
}

func TestServeActivityPushesIdleOut(t *testing.T) {
	// A connection arriving just before the idle deadline keeps the helper
	// alive: the timer resets on accepted connections.
	s := New("0.1.0", 150*time.Millisecond, protocol.RequestTimeout, map[string]Command{
		"x": echoCommand("ok"),
	}, discardLogger())
	ln := newFakeListener()
	defer ln.Close()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()

	// First request: timer resets.
	client := ln.push()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := call(client, testRequest("x")); err != nil {
		t.Fatalf("call: %v", err)
	}
	client.Close()

	time.Sleep(80 * time.Millisecond) // past the original deadline
	client2 := ln.push()
	client2.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := call(client2, testRequest("x")); err != nil {
		t.Fatalf("second call after idle would have fired: %v", err)
	}
	client2.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("helper should have exited by now (post-test idle)")
	}
}

func TestServeDropsMalformedRequest(t *testing.T) {
	// Junk connection: no response, no crash.
	_, ln := startServer(t, time.Minute, map[string]Command{"x": echoCommand("ok")})
	client := ln.push()
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := client.Write([]byte("this is not json\n")); err != nil {
		t.Fatalf("write junk: %v", err)
	}
	// The server closes without answering.
	buf := make([]byte, 64)
	if _, err := client.Read(buf); err == nil {
		t.Fatal("expected connection close, got data")
	} else if !errors.Is(err, io.EOF) {
		t.Logf("read result: %v", err)
	}
}

func TestIdleFromEnv(t *testing.T) {
	cases := []struct {
		value string
		want  time.Duration
		err   bool
	}{
		{"", DefaultIdle, false},
		{"90s", 90 * time.Second, false},
		{"2m30s", 150 * time.Second, false},
		{"3", 3 * time.Second, false},
		{"  7 ", 7 * time.Second, false},
		{"bogus", 0, true},
	}
	for _, tc := range cases {
		t.Setenv("WINUSE_HELPER_IDLE", tc.value)
		got, err := idleFromEnv(DefaultIdle)
		if tc.err {
			if err == nil {
				t.Errorf("WINUSE_HELPER_IDLE=%q: want error, got %s", tc.value, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("WINUSE_HELPER_IDLE=%q: got %s, %v; want %s", tc.value, got, err, tc.want)
		}
	}
}

func TestCommandsRegistryHasListWindows(t *testing.T) {
	cmds := Commands()
	if _, ok := cmds["list-windows"]; !ok {
		t.Fatalf("list-windows missing from the command registry: %v", cmds)
	}
}

func TestListWindowsRejectsArguments(t *testing.T) {
	// Arguments (if ever needed) must be validated; garbage is refused
	// before touching the desktop.
	out, cmdErr := Commands()["list-windows"](context.Background(), json.RawMessage(`{"unexpected":1}`))
	if out != "" || cmdErr == nil {
		t.Fatalf("want refusal for non-empty args, got %q, %v", out, cmdErr)
	}
}
