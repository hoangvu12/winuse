package cli

// Caller-set timeouts (desktop-parity ticket 14): --timeout <seconds> on
// get-window-state, launch-app and batch widens the request's budget, up
// to the command's cap (protocol.MaxTimeout); wait's own --timeout widens
// it the same way (page.go). The value travels as Request.TimeoutMS, so
// the CLI's pipe deadline and the helper's request context both follow
// it. Action commands keep their fixed budget.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// isTimeoutFlag reports whether a token is --timeout or --timeout=<s>.
func isTimeoutFlag(tok string) bool {
	return tok == "--timeout" || strings.HasPrefix(tok, "--timeout=")
}

// parseTimeoutSeconds parses a --timeout value: a number of seconds above
// 0 and at most the command's cap.
func parseTimeoutSeconds(cmd, secs string) (time.Duration, error) {
	limit, ok := protocol.MaxTimeout(cmd)
	if !ok {
		return 0, fmt.Errorf("%s takes no --timeout", cmd)
	}
	maxSecs := int(limit / time.Second)
	n, err := strconv.ParseFloat(secs, 64)
	if err != nil || n <= 0 || n > float64(maxSecs) {
		return 0, fmt.Errorf("--timeout must be a number of seconds above 0 and at most %d, got %q", maxSecs, secs)
	}
	return time.Duration(n * float64(time.Second)), nil
}

// takeTimeoutAt parses the --timeout flag at args[i] and reports how many
// tokens it took (1 for --timeout=<s>, 2 for --timeout <s>).
func takeTimeoutAt(cmd string, args []string, i int) (time.Duration, int, error) {
	if v, ok := strings.CutPrefix(args[i], "--timeout="); ok {
		d, err := parseTimeoutSeconds(cmd, v)
		return d, 1, err
	}
	if i+1 >= len(args) {
		return 0, 0, errors.New("--timeout needs a number of seconds")
	}
	d, err := parseTimeoutSeconds(cmd, args[i+1])
	return d, 2, err
}

// takeTimeout removes every --timeout flag from args (a command whose
// arguments hold no literal text: get-window-state, launch-app) and
// returns the rest and the last timeout given (0: none).
func takeTimeout(cmd string, args []string) ([]string, time.Duration, error) {
	var rest []string
	var timeout time.Duration
	for i := 0; i < len(args); {
		if !isTimeoutFlag(args[i]) {
			rest = append(rest, args[i])
			i++
			continue
		}
		d, n, err := takeTimeoutAt(cmd, args, i)
		if err != nil {
			return nil, 0, err
		}
		timeout = d
		i += n
	}
	return rest, timeout, nil
}

// takeBatchTimeout removes batch's --timeout, which goes before the first
// Action (before or after the Window): a later --timeout belongs to a
// step (wait's own).
func takeBatchTimeout(args []string) ([]string, time.Duration, error) {
	var rest []string
	var timeout time.Duration
	i := 0
	for plain := 0; i < len(args) && plain < 2; {
		if !isTimeoutFlag(args[i]) {
			rest = append(rest, args[i])
			i++
			plain++ // the Window, then the first Action's name
			continue
		}
		d, n, err := takeTimeoutAt("batch", args, i)
		if err != nil {
			return nil, 0, err
		}
		timeout = d
		i += n
	}
	return append(rest, args[i:]...), timeout, nil
}

// runWithTimeout is runWithArgs with a caller-set budget (0: the
// command's own).
func runWithTimeout(cmd string, args any, timeout time.Duration) error {
	return runRequest(cmd, args, func(req *protocol.Request) {
		req.TimeoutMS = int(timeout / time.Millisecond)
	})
}
