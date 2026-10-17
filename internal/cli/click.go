package cli

// The Action commands' CLI side (ticket 05): click by Element Index and
// activate-window. Argument parsing is local (usage errors exit 2 and
// never dial the helper); value validation stays in the helper so the
// CLI and any other client agree on the messages.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// clickUsage is the click command's usage line.
const clickUsage = "usage: winuse click <window> <element index | name:<text> | id:<id> | @x,y> [--count 1|2|3] [--button left|middle|right]"

// clickOptions carries click's flags.
type clickOptions struct {
	count  int    // --count: 1, 2 (double) or 3 (triple)
	button string // --button: left, middle or right (canonicalized by the helper)
}

// clickTarget is the Element Reference to click: an Element Index, a
// name:/id: reference, or an @x,y coordinate of the Window's latest
// screenshot.
type clickTarget struct {
	index *int            // the Element Index form
	ref   string          // the name:<text> / id:<AutomationId> form
	point *protocol.Point // the @x,y coordinate form
}

// parseClickArgs parses click's arguments: a Window reference and an
// Element Reference positionally — an Element Index, name:<text>,
// id:<AutomationId> or `@x,y` — with --count/-c and --button/-b allowed
// anywhere (before, between or after the positionals; values may be
// separate arguments or =joined). Element Indexes are non-negative and
// @x,y parts are too, so a token that looks like a negative number is
// never mistaken for a flag.
func parseClickArgs(args []string) (window string, target clickTarget, o clickOptions, err error) {
	o = clickOptions{count: 1, button: "left"}
	pos, o, err := parseClickFlags(args)
	if err != nil {
		return "", target, o, err
	}
	if len(pos) != 2 {
		// A name: or id: reference the shell split (it was not quoted) is
		// the common harness mistake; name it.
		if len(pos) > 2 && isRefToken(pos[1]) {
			return "", target, o, errors.New(unquotedRefHint(pos[1] + " " + pos[2]))
		}
		return "", target, o, errors.New("click takes a window reference and an element index, name:/id: reference or @x,y")
	}
	target, err = parseClickTargetToken(pos[1])
	if err != nil {
		return "", target, o, err
	}
	return pos[0], target, o, nil
}

// parseClickFlags parses click's flags (--count/-c, --button/-b, anywhere,
// separate or =joined values) and returns the positional arguments in
// order. Both the standalone click (two positionals: the Window and the
// target) and a batch's click step (one positional: the target) parse
// through it.
func parseClickFlags(args []string) (pos []string, o clickOptions, err error) {
	o = clickOptions{count: 1, button: "left"}
	var afterDashDash bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		if afterDashDash || a == "-" || a == "--" {
			if a == "--" && !afterDashDash {
				afterDashDash = true
				continue
			}
			pos = append(pos, a)
			continue
		}
		if !strings.HasPrefix(a, "-") || isNegativeNumber(a) {
			pos = append(pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		value, hasValue := "", false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			value, hasValue, name = name[eq+1:], true, name[:eq]
		}
		switch name {
		case "count", "c":
			if !hasValue {
				if i+1 >= len(args) {
					return pos, o, errors.New("--count needs a value (1, 2 or 3)")
				}
				i++
				value = args[i]
			}
			n, convErr := strconv.Atoi(value)
			if convErr != nil || n < 1 {
				return pos, o, fmt.Errorf("--count needs a value (1, 2 or 3), got %q", value)
			}
			o.count = n
		case "button", "b":
			if !hasValue {
				if i+1 >= len(args) {
					return pos, o, errors.New("--button needs a value (left, middle or right)")
				}
				i++
				value = args[i]
			}
			o.button = value
		default:
			return pos, o, fmt.Errorf("unknown flag %s", a)
		}
	}
	return pos, o, nil
}

// parseClickTargetToken interprets click's target token: an @x,y
// coordinate of the Window's latest screenshot, or an Element Reference
// (an Element Index, name:<text> or id:<AutomationId>) the helper
// resolves against the Window's latest Window State.
func parseClickTargetToken(token string) (target clickTarget, err error) {
	if strings.HasPrefix(token, "@") || strings.HasPrefix(token, " @") {
		x, y, perr := parsePointRef(token)
		if perr != nil {
			return target, perr
		}
		target.point = &protocol.Point{X: x, Y: y}
		return target, nil
	}
	index, ref, rerr := parseElementRefToken(token)
	if rerr != nil {
		return target, rerr
	}
	target.index, target.ref = index, ref
	return target, nil
}

// isNegativeNumber reports whether s looks like a negative integer (an
// invalid Element Index, not a flag).
func isNegativeNumber(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	_, err := strconv.Atoi(s[1:])
	return err == nil
}

// clickCmd implements `winuse click <window> <element index | name: |
// id: | @x,y> [options]`: single, double or right click on an Element of
// a Window (by Element Index, or by a name:/id: reference resolved
// against the Window's latest Window State), or at a coordinate of its
// latest screenshot.
func clickCmd(args []string) int {
	window, target, opts, err := parseClickArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, clickUsage)
		return 2
	}
	if err := runWithArgs("click", protocol.ClickArgs{
		Window:       window,
		ElementIndex: target.index,
		Ref:          target.ref,
		Point:        target.point,
		ClickCount:   opts.count,
		MouseButton:  opts.button,
	}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// activateWindowCmd implements `winuse activate-window <window>`: bring a
// Window to the foreground (the same activation every Action performs
// first).
func activateWindowCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: winuse activate-window <window>")
		return 2
	}
	if err := runWithArgs("activate-window", protocol.ActivateArgs{Window: args[0]}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
