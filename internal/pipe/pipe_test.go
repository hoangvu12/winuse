package pipe

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// These tests create real named pipes in the machine namespace under unique
// test names. They never touch the desktop: no input, no window enumeration,
// no screenshots — only kernel pipe objects.

// testIdentity is the real user SID with a random session id, so pipe
// names stay unique per test run (SDDL conversion needs a well-formed SID)
// while the DACL still grants only the current user.
func testIdentity(t *testing.T) win.Identity {
	t.Helper()
	id, err := win.CurrentIdentity()
	if err != nil {
		t.Fatalf("CurrentIdentity: %v", err)
	}
	id.Session = uint32(time.Now().UnixNano() % 900000)
	id.Elevated = false
	return id
}

func TestPipeNameIncludesUserSessionElevation(t *testing.T) {
	// Naming is exercised with fake SIDs: it must never touch SDDL
	// conversion, only string formatting.
	id := win.Identity{SID: "S-1-5-21-1-2-3", Session: 7}
	if got := Name(id); got != `\\.\pipe\winuse-S-1-5-21-1-2-3-7` {
		t.Errorf("Name = %q", got)
	}
	id.Elevated = true
	if got := Name(id); got != `\\.\pipe\winuse-S-1-5-21-1-2-3-7-admin` {
		t.Errorf("elevated Name = %q", got)
	}
	if got := MutexName(id); got != `Local\winuse-helper-S-1-5-21-1-2-3-7-admin` {
		t.Errorf("elevated MutexName = %q", got)
	}
}

func TestSDDLForRestrictsToUser(t *testing.T) {
	got := SDDLFor("S-1-5-21-9")
	want := "D:P(A;;GA;;;S-1-5-21-9)"
	if got != want {
		t.Fatalf("SDDLFor = %q, want %q", got, want)
	}
}

// TestListenDialRoundTrip exercises the real transport: listen with the
// user-restricted DACL, dial, serve one protocol request, verify the server
// process is us.
func TestListenDialRoundTrip(t *testing.T) {
	id := testIdentity(t)
	ln, err := Listen(id)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		var req protocol.Request
		if err := protocol.ReadLine(bufio.NewReader(conn), &req); err != nil {
			done <- err
			return
		}
		resp := protocol.OKf("pong:" + req.Cmd)
		done <- protocol.WriteLine(conn, &resp)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, id)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Anti-squatting: the serving process (this test process) must be us.
	if err := VerifyServer(conn, id.SID); err != nil {
		t.Fatalf("VerifyServer: %v", err)
	}
	pid, err := ServerPID(conn)
	if err != nil {
		t.Fatalf("ServerPID: %v", err)
	}
	if pid != uint32(windows.GetCurrentProcessId()) {
		t.Fatalf("ServerPID = %d, want own pid %d", pid, windows.GetCurrentProcessId())
	}

	if err := protocol.WriteLine(conn, &protocol.Request{V: protocol.Version, Ver: "test", Cmd: "ping"}); err != nil {
		t.Fatalf("write request: %v", err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	var resp protocol.Response
	if err := protocol.ReadLine(bufio.NewReader(conn), &resp); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !resp.OK || resp.Out != "pong:ping" {
		t.Fatalf("response = %+v", resp)
	}
	if err := <-done; err != nil {
		t.Fatalf("server goroutine: %v", err)
	}
}

func TestDialNoHelper(t *testing.T) {
	id := testIdentity(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Dial(ctx, id)
	if !errors.Is(err, ErrNoHelper) {
		t.Fatalf("want ErrNoHelper, got %v", err)
	}
}

func TestListenTwiceFails(t *testing.T) {
	// A second listener on the same name must fail: the pipe's first
	// instance is created with FILE_CREATE. Backstop behind the
	// single-instance mutex.
	id := testIdentity(t)
	ln, err := Listen(id)
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	defer ln.Close()
	if _, err := Listen(id); err == nil {
		t.Fatal("second Listen must fail")
	}
}

func TestVerifyServerRejectsForeignSID(t *testing.T) {
	id := testIdentity(t)
	ln, err := Listen(id)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, id)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	// The check must fail while the connection is live: the SID the pipe
	// is served by is ours, not the requested foreign one.
	err = VerifyServer(conn, "S-1-5-21-definitely-not-us")
	if err == nil || !strings.Contains(err.Error(), "different user") {
		t.Fatalf("want a different-user refusal, got %v", err)
	}
	conn.Close()
	<-serverDone
}

func TestAcquireSingleton(t *testing.T) {
	id := testIdentity(t)
	release, err := AcquireSingleton(id)
	if err != nil {
		t.Fatalf("first AcquireSingleton: %v", err)
	}
	if _, err := AcquireSingleton(id); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second AcquireSingleton: want ErrAlreadyRunning, got %v", err)
	}
	release()
	// After release the mutex is gone: re-acquiring succeeds.
	release2, err := AcquireSingleton(id)
	if err != nil {
		t.Fatalf("re-AcquireSingleton after release: %v", err)
	}
	release2()
}

func TestNameUniquenessPerIdentity(t *testing.T) {
	a := win.Identity{SID: "S-1", Session: 1}
	b := win.Identity{SID: "S-2", Session: 1}
	if Name(a) == Name(b) {
		t.Fatal("different users must get different pipe names")
	}
	b = win.Identity{SID: "S-1", Session: 2}
	if Name(a) == Name(b) {
		t.Fatal("different sessions must get different pipe names")
	}
	b = win.Identity{SID: "S-1", Session: 1, Elevated: true}
	if Name(a) == Name(b) {
		t.Fatal("elevated and plain helpers must get different pipe names")
	}
}
