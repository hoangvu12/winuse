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
// `--post` with the global --headed flag is a usage error.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// postFlag is the Posted Keys option's spelling; forceFlag sends a Blocked
// Combo press-key otherwise refuses.
const (
	postFlag  = "--post"
	forceFlag = "--force"
)

// The keyboard commands' usage lines.
const (
	typeTextUsage = "usage: winuse type-text <window> [--post] <text>"
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

// typeTextRequest builds type-text's request from its arguments and the
// global --headed flag (ADR 0011: text into a focused classic edit control
// is Headless unless headed). A --post right after the Window posts the
// text instead; anywhere later it is text.
func typeTextRequest(args []string, headed bool) (protocol.TypeTextArgs, error) {
	post := false
	if len(args) >= 2 && args[1] == postFlag {
		post = true
		args = append([]string{args[0]}, args[2:]...)
	}
	window, text, err := parseTypeTextArgs(args)
	if err != nil {
		return protocol.TypeTextArgs{}, err
	}
	if post && headed {
		return protocol.TypeTextArgs{}, errPostHeaded
	}
	return protocol.TypeTextArgs{Window: window, Text: text, Headed: headed, Post: post}, nil
}

// typeTextCmd implements `winuse type-text <window> [--post] <text>`: type
// Unicode text into the Window's focused Element. headed is the global
// --headed flag.
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
