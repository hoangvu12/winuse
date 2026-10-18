package cli

import (
	"bufio"
	"context"
	"errors"
	"net"

	"github.com/hoangvu12/winuse/internal/pipe"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// dialHelper dials the helper pipe, chaining cli.ErrNoHelper so the client
// state machine can recognize "no helper running" and spawn one.
func dialHelper(ctx context.Context, id win.Identity) (net.Conn, error) {
	conn, err := pipe.Dial(ctx, id)
	if err != nil {
		if errors.Is(err, pipe.ErrNoHelper) {
			return nil, &noHelperError{err: err}
		}
		return nil, err
	}
	return conn, nil
}

// noHelperError keeps the pipe's message ("winuse helper is not running:
// <pipe>") while matching both cli.ErrNoHelper and pipe.ErrNoHelper.
type noHelperError struct{ err error }

func (e *noHelperError) Error() string   { return e.err.Error() }
func (e *noHelperError) Unwrap() []error { return []error{ErrNoHelper, e.err} }

// verifyHelper checks the serving process runs as the current user
// (anti-squatting).
func verifyHelper(conn net.Conn, id win.Identity) error {
	return pipe.VerifyServer(conn, id.SID)
}

// readResponse reads the single response line from a connection.
func readResponse(conn net.Conn, resp *protocol.Response) error {
	return protocol.ReadLine(bufio.NewReader(conn), resp)
}
