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
const clickUsage = "usage: winuse click <window> <element index | @x,y> [--count 1|2|3] [--button left|middle|right]"

// clickOptions carries click's flags.
type clickOptions struct {
	count  int    // --count: 1, 2 (double) or 3 (triple)
	button string // --button: left, middle or right (canonicalized by the helper)
}

// clickTarget is the Element Reference to click: an Element Index, or an
// @x,y coordinate of the Window's latest screenshot.
type clickTarget struct {
	index *int            // the Element Index form
	point *protocol.Point // the @x,y coordinate form
}

// parseClickArgs parses click's arguments: a Window reference and an
// Element Reference positionally — an Element Index or `@x,y` — with
// --count/-c and --button/-b allowed anywhere (before, between or after
// the positionals; values may be separate arguments or =joined). Element
// Indexes are non-negative and @x,y parts are too, so a token that looks
// like a negative number is never mistaken for a flag.
func parseClickArgs(args []string) (window string, target clickTarget, o clickOptions, err error) {
	o = clickOptions{count: 1, button: "left"}
	var pos []string
	afterDashDash := false
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
					return "", target, o, errors.New("--count needs a value (1, 2 or 3)")
				}
				i++
				value = args[i]
			}
			n, convErr := strconv.Atoi(value)
			if convErr != nil || n < 1 {
				return "", target, o, fmt.Errorf("--count needs a value (1, 2 or 3), got %q", value)
			}
			o.count = n
		case "button", "b":
			if !hasValue {
				if i+1 >= len(args) {
					return "", target, o, errors.New("--button needs a value (left, middle or right)")
				}
				i++
				value = args[i]
			}
			o.button = value
		default:
			return "", target, o, fmt.Errorf("unknown flag %s", a)
		}
	}
	if len(pos) != 2 {
		return "", target, o, errors.New("click takes a window reference and an element index or @x,y")
	}
	ref := pos[1]
	if strings.HasPrefix(ref, "@") || strings.HasPrefix(ref, " @") {
		x, y, perr := parsePointRef(ref)
		if perr != nil {
			return "", target, o, perr
		}
		target.point = &protocol.Point{X: x, Y: y}
		return pos[0], target, o, nil
	}
	index, err := strconv.Atoi(ref)
	if err != nil || index < 0 {
		return "", target, o, fmt.Errorf("the element index must be a non-negative integer, got %q", ref)
	}
	target.index = &index
	return pos[0], target, o, nil
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

// clickCmd implements `winuse click <window> <element index | @x,y>
// [options]`: single, double or right click on an Element of a Window by
// Element Index, or at a coordinate of its latest screenshot.
func clickCmd(args []string) int {
	window, target, opts, err := parseClickArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, clickUsage)
		return 2
	}
	if err := runWithArgs("click", protocol.ClickArgs{
		Window:       window,
		ElementIndex: target.index,
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
