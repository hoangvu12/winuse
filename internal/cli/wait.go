package cli

// wait's CLI side (browser merge ticket 06, desktop-parity ticket 03).
// After a Window, wait takes the Page's predicates (selector, text, url,
// load, download) or an Element's state (parseWaitArgs, page.go, which a
// Batch's wait step shares). Three forms name no Window: `wait window <app
// or title> [--gone]`, `wait notification [--app] [--text]` (ticket 18)
// and `wait <milliseconds>`, a plain pause. The helper
// routes text by the Window: a browser Window's Page, any other Window's
// UI Automation tree. Parsing is local (usage errors exit 2 and never
// dial the helper).

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// maxPauseMS caps `wait <milliseconds>`: a pause is a wait, so it has the
// wait's cap (protocol.MaxWaitTimeout).
const maxPauseMS = int(protocol.MaxWaitTimeout / time.Millisecond)

// waitCmd implements `winuse wait ...`.
func waitCmd(args []string) int {
	a, err := parseWaitCommand(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, protocol.PageUsage("wait"))
		return 2
	}
	if err := runWithTimeout("wait", a, waitTimeout(a)); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseWaitCommand parses wait's whole command line: a pause (one number
// and nothing else), a window wait (the word window first), or a Window
// and what to wait for in it.
func parseWaitCommand(args []string) (protocol.WaitArgs, error) {
	if ms, ok, err := parsePause(args); ok || err != nil {
		return protocol.WaitArgs{For: protocol.WaitMS, MS: ms}, err
	}
	if len(args) > 0 && args[0] == protocol.WaitWindow {
		return parseWindowWait(args[1:])
	}
	if len(args) > 0 && args[0] == protocol.WaitNotification {
		return parseNotificationWait(args[1:]) // notifications.go, desktop-parity ticket 18
	}
	if len(args) == 0 {
		return protocol.WaitArgs{}, errors.New("wait takes a window reference, window <app or title>, or a number of milliseconds first")
	}
	a, err := parseWaitArgs(args[1:])
	a.Window = args[0]
	return a, err
}

// parsePause recognizes `wait <milliseconds>`: ok when the only token
// besides --timeout is a whole number. A pause takes no --timeout (its
// length is the number), and is above 0 and at most maxPauseMS.
func parsePause(args []string) (ms int, ok bool, err error) {
	var rest []string
	timed := false
	for i := 0; i < len(args); i++ {
		switch t := args[i]; {
		case t == "--timeout":
			timed = true
			i++
		case strings.HasPrefix(t, "--timeout="):
			timed = true
		default:
			rest = append(rest, t)
		}
	}
	if len(rest) != 1 || !isWholeNumber(rest[0]) {
		return 0, false, nil
	}
	if timed {
		return 0, true, errors.New("a pause takes no --timeout: its number is how long it lasts")
	}
	n, convErr := strconv.Atoi(rest[0])
	if convErr != nil || n <= 0 || n > maxPauseMS {
		return 0, true, fmt.Errorf("a pause is a number of milliseconds above 0 and at most %d, got %q; to wait in a Window, name what for: winuse wait <window> text <text>", maxPauseMS, rest[0])
	}
	return n, true, nil
}

// isWholeNumber reports whether s is digits only.
func isWholeNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseWindowWait parses `wait window <app or title> [--gone]
// [--timeout <s>]` after the word window. The App or title is the rest,
// joined with spaces.
func parseWindowWait(tokens []string) (protocol.WaitArgs, error) {
	a := protocol.WaitArgs{For: protocol.WaitWindow}
	var rest []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t == "--gone":
			a.Gone = true
			continue
		case t == "--timeout":
			if i+1 >= len(tokens) {
				return a, errors.New("--timeout needs a number of seconds")
			}
			i++
			t = "--timeout=" + tokens[i]
		case !strings.HasPrefix(t, "--timeout="):
			if strings.HasPrefix(t, "--") {
				return a, fmt.Errorf("wait window takes --gone and --timeout, not %s", t)
			}
			rest = append(rest, t)
			continue
		}
		ms, err := parseWaitTimeoutMS(strings.TrimPrefix(t, "--timeout="))
		if err != nil {
			return a, err
		}
		a.TimeoutMS = ms
	}
	a.Value = strings.Join(rest, " ")
	if strings.TrimSpace(a.Value) == "" {
		return a, errors.New("wait window needs an App name or a title (or part of one)")
	}
	return a, nil
}

// parseWaitTimeoutMS parses a wait's --timeout seconds into milliseconds.
func parseWaitTimeoutMS(secs string) (int, error) {
	n, err := strconv.ParseFloat(secs, 64)
	if err != nil || n <= 0 || n > float64(maxWaitSeconds) {
		return 0, fmt.Errorf("--timeout must be a number of seconds above 0 and at most %d, got %q", maxWaitSeconds, secs)
	}
	return int(n * 1000), nil
}

// parseElementWait parses an element wait's state after its Element
// Reference: one of protocol.WaitStates, and for value the value (the
// rest, joined with spaces; an empty argument waits for an empty value).
func parseElementWait(a protocol.WaitArgs, ref string, rest []string) (protocol.WaitArgs, error) {
	a.For = protocol.WaitElement
	if len(rest) == 0 {
		return a, fmt.Errorf("wait %s needs a state: %s", ref, strings.Join(protocol.WaitStates, ", "))
	}
	a.State = strings.ToLower(rest[0])
	known := false
	for _, s := range protocol.WaitStates {
		known = known || a.State == s
	}
	switch {
	case !known && isRefToken(ref):
		return a, errors.New(unquotedRefHint(ref + " " + rest[0]))
	case !known:
		return a, fmt.Errorf("wait %s takes a state: %s, not %q", ref, strings.Join(protocol.WaitStates, ", "), rest[0])
	case a.State == "value":
		if len(rest) < 2 {
			return a, fmt.Errorf("wait %s value needs the value to wait for (\"\" for an empty one)", ref)
		}
		a.Value = strings.Join(rest[1:], " ")
	case len(rest) > 1:
		return a, fmt.Errorf("wait %s %s takes nothing after it, got %q", ref, a.State, strings.Join(rest[1:], " "))
	}
	return a, nil
}
