package cli

// The keyboard Action commands' CLI side (ticket 06): type-text and
// press-key. Everything after the Window reference is the text (or the
// key): arguments are joined with single spaces, so an agent may quote
// the text as one argument or pass it as several — both produce the same
// typed text. Validation of the value (empty text, key names) stays in
// the helper so the CLI and any other client agree on the messages.
//
// Posted Keys (ADR 0011, Decision 5): `--post` asks the helper to post the
// key or text to the Window's focused control without the foreground. On
// type-text it must stand right after the Window, because everything
// after that is the text; on press-key it may stand after the key too.
// `--post` with the global --headed flag is a usage error. type-text's
// `--into <element>` (desktop-parity ticket 23) stands in the same place.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// postFlag is the Posted Keys option's spelling; forceFlag sends a Blocked
// Combo press-key otherwise refuses; intoFlag aims type-text at an Element.
const (
	postFlag     = "--post"
	forceFlag    = "--force"
	intoFlag     = "--into"
	intervalFlag = "--interval-ms" // a pause between typed characters (desktop-parity ticket 23)
)

// maxIntervalMS bounds --interval-ms, as the helper does.
const maxIntervalMS = 1000

// The keyboard commands' usage lines.
const (
	typeTextUsage = "usage: winuse type-text <window> [--post | --into <element>] [--interval-ms <n>] <text> | --stdin | --file <path>"
	pressKeyUsage = "usage: winuse press-key <window> <key> [--post] [--force]"
)

// errPostHeaded is the usage error for --post together with --headed.
var errPostHeaded = errors.New("--post and --headed cannot be combined: --post posts keys to the Window without the foreground, --headed sends real keys in the foreground")

// parseTypeTextArgs splits type-text's arguments into the Window reference
// and the text to type.
func parseTypeTextArgs(args []string) (window, text string, err error) {
	if len(args) < 2 {
		return "", "", errors.New("type-text takes a window reference and text")
	}
	return args[0], strings.Join(args[1:], " "), nil
}

// parsePressKeyArgs splits press-key's arguments into the Window reference
// and the key (a name such as Return, or a chord such as Control_L+a;
// whitespace around the chord's + is ignored by the helper).
func parsePressKeyArgs(args []string) (window, key string, err error) {
	if len(args) < 2 {
		return "", "", errors.New("press-key takes a window reference and a key")
	}
	return args[0], strings.Join(args[1:], " "), nil
}

// typeTextFlags takes type-text's flags from the front of its text
// arguments (right after the Window, or first in a Batch step): --post,
// or --into and the Element Reference after it (desktop-parity ticket
// 23), and --interval-ms and its milliseconds, before or after either.
// Each comes at most once; anything after them is text, flag-looking or
// not. Last, --stdin or --file <path> (textsource.go) stands for the text:
// src says so, and what follows it is returned as text for the caller to
// refuse.
func typeTextFlags(rest []string) (a protocol.TypeTextArgs, src textSource, text []string, err error) {
	routed, paced := false, false
	for len(rest) > 0 {
		switch {
		case rest[0] == postFlag && !routed:
			a.Post, routed = true, true
			rest = rest[1:]
		case rest[0] == intoFlag && !routed:
			if len(rest) < 2 {
				return a, src, nil, errors.New("--into takes an element index, name:<text> or id:<id> reference, then the text")
			}
			if isPointToken(rest[1]) {
				return a, src, nil, errors.New("--into takes an element index or name:/id: reference, not @x,y coordinates")
			}
			a.IntoIndex, a.IntoRef, err = parseElementRefToken(rest[1])
			if err != nil {
				return a, src, nil, err
			}
			routed = true
			rest = rest[2:]
		case rest[0] == intervalFlag && !paced:
			if len(rest) < 2 {
				return a, src, nil, fmt.Errorf("--interval-ms takes milliseconds from 0 to %d, then the text", maxIntervalMS)
			}
			n, perr := strconv.Atoi(rest[1])
			if perr != nil || n < 0 || n > maxIntervalMS {
				return a, src, nil, fmt.Errorf("--interval-ms takes milliseconds from 0 to %d, got %q", maxIntervalMS, rest[1])
			}
			a.IntervalMS, paced = n, true
			rest = rest[2:]
		default:
			s, n, serr := takeTextSource(rest, fileFlag)
			if serr != nil {
				return a, src, nil, serr
			}
			return a, s, rest[n:], nil
		}
	}
	return a, src, rest, nil
}

// typeTextSourceEnd is a sourceEnd for type-text's text position (headed.go):
// past type-text's own flags, how far a --stdin or --file <path> there
// reaches, or -1 when the text is literal.
func typeTextSourceEnd(region []string) int {
	for i := 0; i < len(region); {
		switch region[i] {
		case postFlag:
			i++
		case intoFlag, intervalFlag:
			i += 2
		default:
			_, n, err := takeTextSource(region[i:], fileFlag)
			if n == 0 || err != nil {
				return -1
			}
			return i + n
		}
	}
	return -1
}

// errTextAndSource refuses literal text given with --stdin or --file.
func errTextAndSource(cmd string, src textSource, text []string) error {
	return fmt.Errorf("%s takes the text or %s, not both; %q follows %s (%s's own flags go before it)", cmd, src.flag, strings.Join(text, " "), src.flag, cmd)
}

// typeTextRequest builds type-text's request from its arguments and the
// global --headed flag (ADR 0011: text into a focused classic edit control
// is Headless unless headed). A --post right after the Window posts the
// text instead, and --into <element> there types into that Element;
// anywhere later either is text. --stdin or --file <path> there, after
// those flags, reads the text instead (textsource.go).
func typeTextRequest(args []string, headed bool) (protocol.TypeTextArgs, error) {
	var req protocol.TypeTextArgs
	var src textSource
	if len(args) >= 2 {
		flags, s, text, err := typeTextFlags(args[1:])
		if err != nil {
			return protocol.TypeTextArgs{}, err
		}
		if s.given() && len(text) > 0 {
			return protocol.TypeTextArgs{}, errTextAndSource("type-text", s, text)
		}
		req, src = flags, s
		args = append([]string{args[0]}, text...)
	}
	var window, text string
	var err error
	if src.given() {
		window = args[0]
		text, err = src.read()
	} else {
		window, text, err = parseTypeTextArgs(args)
	}
	if err != nil {
		return protocol.TypeTextArgs{}, err
	}
	if req.Post && headed {
		return protocol.TypeTextArgs{}, errPostHeaded
	}
	req.Window, req.Text, req.Headed = window, text, headed
	return req, nil
}

// typeTextCmd implements `winuse type-text <window> [--post | --into
// <element>] <text>`: type Unicode text into the Window's focused Element,
// or into the one --into names. headed is the global --headed flag.
func typeTextCmd(args []string, headed bool) int {
	req, err := typeTextRequest(args, headed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, typeTextUsage)
		return 2
	}
	if err := runWithArgs("type-text", req); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// pressKeyRequest builds press-key's request from its arguments and the
// global --headed flag (ADR 0011: Return and Escape on a classic dialog
// are Headless unless headed). A --post after the Window, before or after
// the key, posts the key instead.
//
// A --force there too sends a Blocked Combo (Alt+F4, Alt+Tab,
// Ctrl+Shift+Escape and their supersets), which the helper otherwise
// refuses.
func pressKeyRequest(args []string, headed bool) (protocol.PressKeyArgs, error) {
	args, post := stripFlag(args, postFlag)
	args, force := stripFlag(args, forceFlag)
	window, key, err := parsePressKeyArgs(args)
	if err != nil {
		return protocol.PressKeyArgs{}, err
	}
	if post && headed {
		return protocol.PressKeyArgs{}, errPostHeaded
	}
	return protocol.PressKeyArgs{Window: window, Key: key, Headed: headed, Post: post, Force: force}, nil
}

// stripFlag removes every flag (--post, --force) after the first argument
// (the Window, or a batch step's Action name) and reports whether there
// was one. Key names never spell a flag.
func stripFlag(args []string, flag string) (rest []string, found bool) {
	rest = make([]string, 0, len(args))
	for i, tok := range args {
		if i > 0 && tok == flag {
			found = true
			continue
		}
		rest = append(rest, tok)
	}
	return rest, found
}

// pressKeyCmd implements `winuse press-key <window> <key> [--post]
// [--force]`: press a key or key combination by Codex's key names. headed
// is the global --headed flag.
func pressKeyCmd(args []string, headed bool) int {
	req, err := pressKeyRequest(args, headed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, pressKeyUsage)
		return 2
	}
	if err := runWithArgs("press-key", req); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
