package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/version"
)

// fakeConn plays the helper's side of one connection for the client: the
// request line written by the client is captured, and a canned response
// (or a drop) is readable.
type fakeConn struct {
	readFrom io.Reader    // canned response bytes, or a reader that errors
	written  bytes.Buffer // captured request line
	mu       sync.Mutex
	closed   bool
}

func (c *fakeConn) Read(b []byte) (int, error) { return c.readFrom.Read(b) }
func (c *fakeConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.written.Write(b)
}
func (c *fakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}
func (c *fakeConn) LocalAddr() net.Addr                { return nil }
func (c *fakeConn) RemoteAddr() net.Addr               { return nil }
func (c *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(t time.Time) error { return nil }

func (c *fakeConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// respondingConn builds a connection that answers with one response.
func respondingConn(resp *protocol.Response) *fakeConn {
	line, _ := json.Marshal(resp)
	return &fakeConn{readFrom: strings.NewReader(string(line) + "\n")}
}

// droppingConn builds a connection that answers with nothing (EOF before a
// response): the helper vanished mid-exchange.
func droppingConn() *fakeConn {
	return &fakeConn{readFrom: strings.NewReader("")}
}

// dialScript drives the fake Dial function: each dial consumes one step.
// A step is either an error or a connection to hand back.
type dialScript struct {
	mu    sync.Mutex
	steps []dialStep
}

type dialStep struct {
	err  error
	conn *fakeConn
}

func (s *dialScript) dial(ctx context.Context) (net.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) == 0 {
		return nil, errors.New("dialScript exhausted")
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if step.err != nil {
		return nil, step.err
	}
	return step.conn, nil
}

func (s *dialScript) remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.steps)
}

func notFoundErr() error {
	return errors.Join(ErrNoHelper, errors.New("winuse helper is not running: \\\\.\\pipe\\winuse-x"))
}

func testClient(script *dialScript, spawns *int, verifyErr error) *Client {
	return &Client{
		Dial:   script.dial,
		Spawn:  func() error { *spawns++; return nil },
		Verify: func(net.Conn) error { return verifyErr },
	}
}

func testRequest() *protocol.Request {
	return &protocol.Request{V: protocol.Version, Ver: version.Version, Cmd: "list-windows"}
}

func TestCallStartsHelperWhenNoneRunning(t *testing.T) {
	script := &dialScript{steps: []dialStep{
		{err: notFoundErr()}, // initial dial: no helper
		{conn: respondingConn(&protocol.Response{OK: true, Out: "1\tApp\tTitle\n"})}, // after spawn
	}}
	spawns := 0
	client := testClient(script, &spawns, nil)

	resp, err := client.Call(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !resp.OK || resp.Out != "1\tApp\tTitle\n" {
		t.Fatalf("response: %+v", resp)
	}
	if spawns != 1 {
		t.Errorf("spawns = %d, want 1", spawns)
	}
	if script.remaining() != 0 {
		t.Errorf("script steps left: %d", script.remaining())
	}
}

func TestCallRetriesDialWithBackoffAfterSpawn(t *testing.T) {
	// The freshly spawned helper takes a couple of dials to appear.
	script := &dialScript{steps: []dialStep{
		{err: notFoundErr()}, // no helper: spawn
		{err: notFoundErr()}, // still starting
		{err: notFoundErr()}, // still starting
		{conn: respondingConn(&protocol.Response{OK: true, Out: "ok"})},
	}}
	spawns := 0
	client := testClient(script, &spawns, nil)

	resp, err := client.Call(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !resp.OK {
		t.Fatalf("response: %+v", resp)
	}
	if spawns != 1 {
		t.Errorf("spawns = %d, want 1 (no double-spawn while dialing)", spawns)
	}
}

func TestCallRestartsStaleHelper(t *testing.T) {
	// An old helper answers "stale" and exits; the client waits for the
	// pipe to go, then spawns a current one and retries.
	oldHelper := respondingConn(&protocol.Response{OK: false, Stale: true, Err: "different build", Exit: 1})
	script := &dialScript{steps: []dialStep{
		{conn: oldHelper},    // old helper answers stale
		{err: notFoundErr()}, // waitPipeGone sees the pipe gone
		{err: notFoundErr()}, // attempt 1 dial: no helper → spawn
		{conn: respondingConn(&protocol.Response{OK: true, Out: "fresh"})}, // new helper
	}}
	spawns := 0
	client := testClient(script, &spawns, nil)

	resp, err := client.Call(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	// The answer notes the restart (desktop-parity ticket 25): the old
	// helper's Window States went with it.
	if !resp.OK || resp.Out != "fresh\n"+restartNote {
		t.Fatalf("response: %+v", resp)
	}
	if spawns != 1 {
		t.Errorf("spawns = %d, want 1", spawns)
	}
}

func TestCallRetriesOnceWhenConnectionDrops(t *testing.T) {
	// Helper accepted the connection but died before answering (idle exit
	// race): retry once, which finds no helper and respawns.
	script := &dialScript{steps: []dialStep{
		{conn: droppingConn()}, // EOF before response
		{err: notFoundErr()},   // retry dial: helper gone → spawn
		{conn: respondingConn(&protocol.Response{OK: true, Out: "ok"})},
	}}
	spawns := 0
	client := testClient(script, &spawns, nil)

	resp, err := client.Call(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !resp.OK {
		t.Fatalf("response: %+v", resp)
	}
	if spawns != 1 {
		t.Errorf("spawns = %d, want 1", spawns)
	}
}

func TestCallReturnsFatalErrorsWithoutRetry(t *testing.T) {
	// Verification failure (pipe squatted by a foreign process) must not be
	// retried: it is fatal, not transient.
	script := &dialScript{steps: []dialStep{
		{conn: respondingConn(&protocol.Response{OK: true})},
	}}
	spawns := 0
	client := testClient(script, &spawns, errors.New("helper pipe is served by a different user"))

	_, err := client.Call(context.Background(), testRequest())
	if err == nil || !strings.Contains(err.Error(), "different user") {
		t.Fatalf("want verification error, got %v", err)
	}
	if spawns != 0 {
		t.Errorf("spawns = %d, want 0", spawns)
	}
}

func TestCallReportsSpawnFailure(t *testing.T) {
	script := &dialScript{steps: []dialStep{
		{err: notFoundErr()},
	}}
	client := &Client{
		Dial:   script.dial,
		Spawn:  func() error { return errors.New("access denied") },
		Verify: nil,
	}
	_, err := client.Call(context.Background(), testRequest())
	if err == nil || !strings.Contains(err.Error(), "starting helper") {
		t.Fatalf("want spawn error, got %v", err)
	}
}

func TestCallReportsHelperStartupTimeout(t *testing.T) {
	// The helper never comes up: dial keeps saying not found until the
	// spawn window closes.
	script := &dialScript{}
	for i := 0; i < 1000; i++ {
		script.steps = append(script.steps, dialStep{err: notFoundErr()})
	}
	spawns := 0
	client := testClient(script, &spawns, nil)
	client.spawnWait = 200 * time.Millisecond // speed the test up

	_, err := client.Call(context.Background(), testRequest())
	if err == nil || !strings.Contains(err.Error(), "helper did not start") {
		t.Fatalf("want startup timeout error, got %v", err)
	}
}

func TestCallSendsWellFormedRequest(t *testing.T) {
	conn := respondingConn(&protocol.Response{OK: true, Out: "ok"})
	script := &dialScript{steps: []dialStep{{conn: conn}}}
	spawns := 0
	client := testClient(script, &spawns, nil)

	if _, err := client.Call(context.Background(), testRequest()); err != nil {
		t.Fatalf("Call: %v", err)
	}
	var req protocol.Request
	if err := json.Unmarshal(conn.written.Bytes(), &req); err != nil {
		t.Fatalf("request line is not JSON: %v (%q)", err, conn.written.Bytes())
	}
	if req.V != protocol.Version || req.Ver != version.Version || req.Cmd != "list-windows" {
		t.Fatalf("request fields wrong: %+v", req)
	}
	if !conn.isClosed() {
		t.Error("client must close its connection after the exchange")
	}
}

func TestRequestMarshalsArgs(t *testing.T) {
	req, err := Request("click", map[string]int{"index": 3})
	if err != nil {
		t.Fatal(err)
	}
	if string(req.Args) != `{"index":3}` {
		t.Fatalf("args = %s", req.Args)
	}
	req, err = Request("list-windows", nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Args != nil {
		t.Fatalf("args should be absent, got %s", req.Args)
	}
}

func TestIsConnDropped(t *testing.T) {
	cases := []struct {
		err  error
		drop bool
	}{
		{io.EOF, true},
		{io.ErrUnexpectedEOF, true},
		{&net.OpError{Op: "read", Err: errors.New("broken pipe")}, true},
		{&net.OpError{Op: "read", Err: errors.New("connection reset by peer")}, true},
		{errors.New("read: The pipe has been ended."), true},
		{context.DeadlineExceeded, false},
		{errors.New("some other failure"), false},
	}
	for _, tc := range cases {
		if got := isConnDropped(tc.err); got != tc.drop {
			t.Errorf("isConnDropped(%v) = %v, want %v", tc.err, got, tc.drop)
		}
	}
}

func TestCallWithoutARestartAddsNoNote(t *testing.T) {
	// Only a stale restart notes lost Window States: a first start or a
	// dropped connection's retry does not (desktop-parity ticket 25).
	script := &dialScript{steps: []dialStep{
		{err: notFoundErr()}, // no helper → spawn
		{conn: respondingConn(&protocol.Response{OK: true, Out: "ok\n"})},
	}}
	spawns := 0
	resp, err := testClient(script, &spawns, nil).Call(context.Background(), testRequest())
	if err != nil || resp.Out != "ok\n" {
		t.Fatalf("Call = %+v, %v; want ok without a note", resp, err)
	}
}

func TestCallStaleTwiceAddsNoNote(t *testing.T) {
	// The restarted helper is stale too: its answer is returned as is.
	stale := &protocol.Response{OK: false, Stale: true, Err: "different build", Exit: 1}
	script := &dialScript{steps: []dialStep{
		{conn: respondingConn(stale)},
		{err: notFoundErr()},
		{conn: respondingConn(stale)},
	}}
	spawns := 0
	resp, err := testClient(script, &spawns, nil).Call(context.Background(), testRequest())
	if err != nil || !resp.Stale || resp.Out != "" {
		t.Fatalf("Call = %+v, %v; want the stale answer without a note", resp, err)
	}
}

func TestRequestCarriesTheBuild(t *testing.T) {
	req, err := Request("list-windows", nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Build == "" || req.Build != version.Build() || req.Ver != version.Version {
		t.Errorf("request = %+v, want version %s build %s", req, version.Version, version.Build())
	}
}

// busyErr is what the real dial returns for a pipe nothing listens on: a
// stale helper still finishing its requests in flight.
func busyErr() error { return context.DeadlineExceeded }

// heldFor fakes the single-instance mutex: held for the first n checks.
func heldFor(n int) func() bool {
	var mu sync.Mutex
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		n--
		return n >= 0
	}
}

func TestCallWaitsForAStaleHelperToFinishItsRequests(t *testing.T) {
	// The old helper answers stale, then stops listening while another
	// Agent's Batch runs (its pipe busy), then holds the mutex a moment on
	// its way out (desktop-parity ticket 25). The CLI spawns only once the
	// mutex is free, instead of failing after spawnDialWait.
	stale := &protocol.Response{OK: false, Stale: true, Err: "different build", Exit: 1}
	script := &dialScript{steps: []dialStep{
		{conn: respondingConn(stale)},
		{err: busyErr()},     // waitPipeGone: busy, not gone; it returns
		{err: busyErr()},     // dialOrStart: busy → wait
		{err: busyErr()},     // still finishing
		{err: notFoundErr()}, // pipe gone, mutex still held
		{err: notFoundErr()}, // pipe gone, mutex free → spawn
		{conn: respondingConn(&protocol.Response{OK: true, Out: "fresh"})},
	}}
	running := heldFor(1)
	spawns := 0
	client := testClient(script, &spawns, nil)
	client.Running = running
	client.Spawn = func() error {
		if running() {
			t.Error("spawned while the old helper held the mutex")
		}
		spawns++
		return nil
	}

	resp, err := client.Call(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !resp.OK || resp.Out != "fresh\n"+drainNote+restartNote {
		t.Fatalf("response: %q", resp.Out)
	}
	if spawns != 1 || script.remaining() != 0 {
		t.Errorf("spawns = %d, steps left = %d; want 1, 0", spawns, script.remaining())
	}
}

func TestCallGivesUpWaitingForTheOldHelperAtTheBudget(t *testing.T) {
	// The old helper's Batch outlasts this request's budget: the CLI says
	// what it waited for, and never spawned a helper meanwhile.
	script := &dialScript{}
	for i := 0; i < 1000; i++ {
		script.steps = append(script.steps, dialStep{err: busyErr()})
	}
	spawns := 0
	client := testClient(script, &spawns, nil)
	client.exitWait = 300 * time.Millisecond

	start := time.Now()
	_, err := client.Call(context.Background(), testRequest())
	elapsed := time.Since(start)
	want := "the old winuse helper was still finishing its requests in flight when this request's budget expired; nothing was sent"
	if err == nil || err.Error() != want {
		t.Fatalf("Call error = %v, want %q", err, want)
	}
	if elapsed < 300*time.Millisecond || elapsed > 3*time.Second {
		t.Errorf("gave up after %s, want at the 300ms budget", elapsed)
	}
	if spawns != 0 {
		t.Errorf("spawns = %d, want 0", spawns)
	}
	// --json reads it as a budget that ran out with nothing sent.
	var ee *exitError
	if !errors.As(err, &ee) || ee.hint == "" {
		t.Fatalf("want an exitError with a hint, got %#v", err)
	}
	if code, _ := protocol.CodeFor(ee.msg, ee.hint, 1); code != protocol.CodeTimeout {
		t.Errorf("code = %s, want %s", code, protocol.CodeTimeout)
	}
	if delivery, _ := protocol.DeliveryOf(ee.msg, ee.hint); delivery != protocol.DeliveryNotDelivered {
		t.Errorf("delivery = %s, want %s", delivery, protocol.DeliveryNotDelivered)
	}
}

func TestCallGivesUpOnAHelperThatNeverLetsGoOfTheMutex(t *testing.T) {
	// No pipe, but the mutex stays held: the old helper is stuck on its way
	// out. Spawning would only lose the mutex.
	script := &dialScript{}
	for i := 0; i < 1000; i++ {
		script.steps = append(script.steps, dialStep{err: notFoundErr()})
	}
	spawns := 0
	client := testClient(script, &spawns, nil)
	client.Running = func() bool { return true }
	client.exitWait = 200 * time.Millisecond

	_, err := client.Call(context.Background(), testRequest())
	if err == nil || !strings.Contains(err.Error(), "did not exit before this request's budget expired; nothing was sent") {
		t.Fatalf("Call error = %v", err)
	}
	if spawns != 0 {
		t.Errorf("spawns = %d, want 0", spawns)
	}
}

func TestCallWaitsBrieflyForTheMutexWithoutANote(t *testing.T) {
	// A helper that had nothing in flight only holds the mutex a moment
	// after its pipe goes: the CLI waits for it, then starts one, and the
	// answer carries no wait Note (a first start, or the idle-exit race).
	script := &dialScript{steps: []dialStep{
		{err: notFoundErr()}, // mutex held
		{err: notFoundErr()}, // mutex free → spawn
		{conn: respondingConn(&protocol.Response{OK: true, Out: "ok\n"})},
	}}
	spawns := 0
	client := testClient(script, &spawns, nil)
	client.Running = heldFor(1)

	resp, err := client.Call(context.Background(), testRequest())
	if err != nil || resp.Out != "ok\n" {
		t.Fatalf("Call = %+v, %v; want ok without a note", resp, err)
	}
	if spawns != 1 {
		t.Errorf("spawns = %d, want 1", spawns)
	}
}

func TestCallUsesAHelperThatCameUpWhileItWaited(t *testing.T) {
	// Another CLI started a helper while this one waited: use it, spawn
	// nothing.
	script := &dialScript{steps: []dialStep{
		{err: busyErr()},
		{err: notFoundErr()}, // mutex held (the new helper, starting)
		{conn: respondingConn(&protocol.Response{OK: true, Out: "ok\n"})},
	}}
	spawns := 0
	client := testClient(script, &spawns, nil)
	client.Running = func() bool { return true }

	resp, err := client.Call(context.Background(), testRequest())
	if err != nil || resp.Out != "ok\n"+drainNote {
		t.Fatalf("Call = %+v, %v", resp, err)
	}
	if spawns != 0 {
		t.Errorf("spawns = %d, want 0", spawns)
	}
}

func TestDialOrStartWaitsUpToTheBudgetItIsGiven(t *testing.T) {
	// Without the test override, the wait runs to the request's budget,
	// which Call passes in (protocol.Budget).
	script := &dialScript{}
	for i := 0; i < 1000; i++ {
		script.steps = append(script.steps, dialStep{err: busyErr()})
	}
	spawns := 0
	start := time.Now()
	_, drained, err := testClient(script, &spawns, nil).dialOrStart(context.Background(), 250*time.Millisecond)
	if elapsed := time.Since(start); err == nil || !drained || elapsed < 250*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("dialOrStart = drained %v, %v after %s; want a give-up at 250ms", drained, err, elapsed)
	}
}
