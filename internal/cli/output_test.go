package cli

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestReportErrorStyles(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "helper timeout message prints verbatim",
			err:  &TimeoutError{Cmd: "list-windows"},
			want: "winuse request timed out: list-windows\n",
		},
		{
			name: "helper failure keeps its exact message plus hint",
			err:  &exitError{msg: "window 123 gone", hint: "read the Window State again", code: 1},
			want: "window 123 gone\nhint: read the Window State again\n",
		},
		{
			name: "helper failure without hint stays single-line",
			err:  &exitError{msg: "ambiguous window name \"Notepad\"", code: 1},
			want: "ambiguous window name \"Notepad\"\n",
		},
		{
			name: "local failure gets the winuse prefix",
			err:  errors.New("starting helper: access denied"),
			want: "winuse: starting helper: access denied\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			reportError(&buf, tc.err)
			if buf.String() != tc.want {
				t.Errorf("reportError:\n got %q\nwant %q", buf.String(), tc.want)
			}
		})
	}
}

func TestFinishSuccessWritesOut(t *testing.T) {
	// stdout capture is tricky in-process; exercise the error path and the
	// success plumbing separately.
	resp := &protocol.Response{OK: true, Out: "101\tApp\tTitle\n"}
	err := finish(resp)
	if err != nil {
		t.Fatalf("finish on success: %v", err)
	}
}

func TestFinishCarriesExitCodeAndHint(t *testing.T) {
	resp := &protocol.Response{OK: false, Err: "the user stopped winuse with the physical Escape key", Hint: "stop and ask the user", Exit: 130}
	err := finish(resp)
	if err == nil || err.Error() != resp.Err {
		t.Fatalf("finish: %v", err)
	}
	if code := ExitCode(err); code != 130 {
		t.Errorf("ExitCode = %d, want 130", code)
	}
	var ee *exitError
	if !errors.As(err, &ee) || ee.hint != resp.Hint {
		t.Errorf("hint lost: %v", err)
	}
}

func TestExitCodeDefaultsToOne(t *testing.T) {
	if got := ExitCode(errors.New("x")); got != 1 {
		t.Errorf("ExitCode = %d, want 1", got)
	}
	if got := ExitCode(nil); got != 1 {
		t.Errorf("ExitCode(nil) = %d, want 1", got)
	}
}

// A connection whose read never completes within the deadline maps to the
// decided timeout error.
type stalledConn struct {
	fakeConn
}

func (c *stalledConn) Read(b []byte) (int, error) {
	time.Sleep(2 * time.Second)
	return 0, os.ErrDeadlineExceeded
}

func TestRoundTripMapsDeadlineToTimeoutError(t *testing.T) {
	conn := &stalledConn{fakeConn: fakeConn{readFrom: strings.NewReader("")}}
	client := &Client{Verify: nil}
	_, err := client.roundTrip(conn, &protocol.Request{Cmd: "get-window-state"})
	var timeout *TimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("want TimeoutError, got %v", err)
	}
	if timeout.Cmd != "get-window-state" || timeout.Error() != "winuse request timed out: get-window-state" {
		t.Fatalf("bad timeout error: %v", timeout)
	}
}

// A TimeoutError must not trigger the dropped-connection retry: a timed-out
// request fails, it does not respawn.
func TestTimeoutIsNotConnDropped(t *testing.T) {
	if isConnDropped(&TimeoutError{Cmd: "x"}) {
		t.Fatal("timeouts must not be treated as a dropped connection")
	}
}

var _ net.Conn = (*stalledConn)(nil)
var _ io.Reader = (*stalledConn)(nil)
