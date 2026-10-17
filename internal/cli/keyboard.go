package cli

// The keyboard Action commands' CLI side (ticket 06): type-text and
// press-key. Everything after the Window reference is the text (or the
// key): arguments are joined with single spaces, so an agent may quote
// the text as one argument or pass it as several — both produce the same
// typed text. Validation of the value (empty text, key names) stays in
// the helper so the CLI and any other client agree on the messages.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

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

// typeTextCmd implements `winuse type-text <window> <text>`: type Unicode
// text into the Window's focused Element.
func typeTextCmd(args []string) int {
	window, text, err := parseTypeTextArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\nusage: winuse type-text <window> <text>\n", err)
		return 2
	}
	if err := runWithArgs("type-text", protocol.TypeTextArgs{Window: window, Text: text}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// pressKeyCmd implements `winuse press-key <window> <key>`: press a key or
// key combination by Codex's key names.
func pressKeyCmd(args []string) int {
	window, key, err := parsePressKeyArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\nusage: winuse press-key <window> <key>\n", err)
		return 2
	}
	if err := runWithArgs("press-key", protocol.PressKeyArgs{Window: window, Key: key}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
