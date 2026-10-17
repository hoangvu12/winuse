package cli

// The ticket-07 Action commands' CLI side: set-value and
// perform-secondary-action. Argument parsing is local (usage errors exit 2
// and never dial the helper); the value and the action name are passed
// through verbatim for the helper to act on (Codex's parameters).

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// setValueUsage is the set-value command's usage line.
const setValueUsage = "usage: winuse set-value <window> <element index> <value>"

// secondaryActionUsage is the perform-secondary-action command's usage
// line.
const secondaryActionUsage = "usage: winuse perform-secondary-action <window> <element index> <action>"

// parseSetValueArgs parses set-value's arguments: a Window reference and
// an Element Index positionally, then the value — every remaining
// argument joined with single spaces, so an unquoted value with spaces
// (hello world) still reaches the Element as one string.
func parseSetValueArgs(args []string) (window string, index int, value string, err error) {
	if len(args) < 3 {
		return "", 0, "", errors.New("set-value takes a window reference, an element index and a value")
	}
	index, err = strconv.Atoi(args[1])
	if err != nil || index < 0 {
		return "", 0, "", fmt.Errorf("the element index must be a non-negative integer, got %q", args[1])
	}
	return args[0], index, strings.Join(args[2:], " "), nil
}

// parseSecondaryActionArgs parses perform-secondary-action's arguments:
// a Window reference and an Element Index positionally, then the action
// name — every remaining argument joined with single spaces, so an
// unquoted two-word action (scroll down) still names one action.
func parseSecondaryActionArgs(args []string) (window string, index int, action string, err error) {
	if len(args) < 3 {
		return "", 0, "", errors.New("perform-secondary-action takes a window reference, an element index and an action")
	}
	index, err = strconv.Atoi(args[1])
	if err != nil || index < 0 {
		return "", 0, "", fmt.Errorf("the element index must be a non-negative integer, got %q", args[1])
	}
	return args[0], index, strings.Join(args[2:], " "), nil
}

// setValueCmd implements `winuse set-value <window> <element index>
// <value>`: set an Element's value directly.
func setValueCmd(args []string) int {
	window, index, value, err := parseSetValueArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, setValueUsage)
		return 2
	}
	if err := runWithArgs("set-value", protocol.SetValueArgs{
		Window:       window,
		ElementIndex: &index,
		Value:        value,
	}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// performSecondaryActionCmd implements `winuse perform-secondary-action
// <window> <element index> <action>`: perform one of the Element's
// Secondary Actions (Raise, Scroll Down, Expand, Collapse, Toggle,
// Select, ...).
func performSecondaryActionCmd(args []string) int {
	window, index, action, err := parseSecondaryActionArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, secondaryActionUsage)
		return 2
	}
	if err := runWithArgs("perform-secondary-action", protocol.SecondaryActionArgs{
		Window:       window,
		ElementIndex: &index,
		Action:       action,
	}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
