package cli

// The ticket-07 Action commands' CLI side: set-value and
// perform-secondary-action. Argument parsing is local (usage errors exit 2
// and never dial the helper); the value and the action name are passed
// through verbatim for the helper to act on (Codex's parameters). Ticket
// 11 adds the name:/id: Element Reference forms: the second positional is
// an Element Index or a reference, resolved helper-side against the
// Window's latest Window State.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// setValueUsage is the set-value command's usage line.
const setValueUsage = "usage: winuse set-value <window> <element index | name:<text> | id:<id>> <value>"

// secondaryActionUsage is the perform-secondary-action command's usage
// line.
const secondaryActionUsage = "usage: winuse perform-secondary-action <window> <element index | name:<text> | id:<id>> <action>"

// parseSetValueArgs parses set-value's arguments: a Window reference and
// an Element Reference (an Element Index, name:<text> or
// id:<AutomationId>) positionally, then the value — every remaining
// argument joined with single spaces, so an unquoted value with spaces
// (hello world) still reaches the Element as one string.
func parseSetValueArgs(args []string) (window string, index *int, ref string, value string, err error) {
	if len(args) < 3 {
		return "", nil, "", "", errors.New("set-value takes a window reference, an element index or name:/id: reference, and a value")
	}
	index, ref, err = parseElementRefToken(args[1])
	if err != nil {
		return "", nil, "", "", err
	}
	return args[0], index, ref, strings.Join(args[2:], " "), nil
}

// parseSecondaryActionArgs parses perform-secondary-action's arguments:
// a Window reference and an Element Reference (an Element Index,
// name:<text> or id:<AutomationId>) positionally, then the action name —
// every remaining argument joined with single spaces, so an unquoted
// two-word action (scroll down) still names one action.
func parseSecondaryActionArgs(args []string) (window string, index *int, ref string, action string, err error) {
	if len(args) < 3 {
		return "", nil, "", "", errors.New("perform-secondary-action takes a window reference, an element index or name:/id: reference, and an action")
	}
	index, ref, err = parseElementRefToken(args[1])
	if err != nil {
		return "", nil, "", "", err
	}
	return args[0], index, ref, strings.Join(args[2:], " "), nil
}

// setValueCmd implements `winuse set-value <window> <element index |
// name: | id:> <value>`: set an Element's value directly.
func setValueCmd(args []string) int {
	window, index, ref, value, err := parseSetValueArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, setValueUsage)
		return 2
	}
	if err := runWithArgs("set-value", protocol.SetValueArgs{
		Window:       window,
		ElementIndex: index,
		Ref:          ref,
		Value:        value,
	}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// performSecondaryActionCmd implements `winuse perform-secondary-action
// <window> <element index | name: | id:> <action>`: perform one of the
// Element's Secondary Actions (Raise, Scroll Down, Expand, Collapse,
// Toggle, Select, ...).
func performSecondaryActionCmd(args []string) int {
	window, index, ref, action, err := parseSecondaryActionArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, secondaryActionUsage)
		return 2
	}
	if err := runWithArgs("perform-secondary-action", protocol.SecondaryActionArgs{
		Window:       window,
		ElementIndex: index,
		Ref:          ref,
		Action:       action,
	}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
