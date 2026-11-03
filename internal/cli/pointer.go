package cli

// The pointer Action commands' CLI side (ticket 10): scroll, drag and
// hover. All three take `@x,y` coordinate Element References of the
// Window's latest screenshot — the same syntax click's coordinate form
// uses — so parsing is positional and strict (usage errors exit 2 and
// never dial the helper); value validation (amount bounds, whether a
// screenshot exists) stays in the helper so the CLI and any other client
// agree on the messages. scroll also has Element forms (headless-parity
// spec, "Element scroll"): an Element Index or name:/id: reference with a
// direction and optional steps, or into-view.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
)

// The commands' usage lines.
const (
	scrollUsage = "usage: winuse scroll <window> <element index | name:<text> | id:<id>> <up|down|left|right> [steps]   (steps 1-50, default 3)\n" +
		"       winuse scroll <window> <element index | name:<text> | id:<id>> into-view\n" +
		"       winuse scroll <window> @x,y <dx> <dy>   (positive dy scrolls down; one wheel notch is 120)"
	dragUsage = "usage: winuse drag <window> @x,y @x,y   (from, to; both in the latest screenshot's pixels)\n" +
		"       winuse drag <window> <page element> <page element | @x,y>"
	hoverUsage = "usage: winuse hover <window> @x,y   (the point, in the latest screenshot's pixels)\n" +
		"       winuse hover <window> <page element>"
)

// parseScrollArgs parses scroll's arguments: a Window reference, an @x,y
// point of the Window's latest screenshot, and the horizontal and
// vertical scroll amounts — signed integers (negative scrolls left or
// up). Strictly positional, so a negative amount is never mistaken for a
// flag.
func parseScrollArgs(args []string) (window string, x, y, dx, dy int, err error) {
	if len(args) != 4 {
		return "", 0, 0, 0, 0, errors.New("scroll takes a window reference, an @x,y point and two scroll amounts")
	}
	x, y, err = parsePointRef(args[1])
	if err != nil {
		return "", 0, 0, 0, 0, err
	}
	dx, err = parseScrollAmount(args[2])
	if err != nil {
		return "", 0, 0, 0, 0, err
	}
	dy, err = parseScrollAmount(args[3])
	if err != nil {
		return "", 0, 0, 0, 0, err
	}
	return args[0], x, y, dx, dy, nil
}

// parseScrollAmount parses one scroll amount: a signed integer (the
// unit — one wheel notch — and the ±bound are validated in the helper).
func parseScrollAmount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("the scroll amount must be an integer, got %q", s)
	}
	return n, nil
}

// isPointToken reports whether a target token is the @x,y form.
func isPointToken(token string) bool {
	return strings.HasPrefix(token, "@") || strings.HasPrefix(token, " @")
}

// parseScrollCommand parses scroll's arguments in any of its forms: a
// Window reference, then an @x,y point and two amounts (parseScrollArgs),
// or an Element with a direction and optional steps, or into-view
// (parseElementScrollTokens).
func parseScrollCommand(args []string) (protocol.ScrollArgs, error) {
	if len(args) >= 2 && isPointToken(args[1]) {
		window, x, y, dx, dy, err := parseScrollArgs(args)
		if err != nil {
			return protocol.ScrollArgs{}, err
		}
		return protocol.ScrollArgs{Window: window, X: x, Y: y, ScrollX: dx, ScrollY: dy}, nil
	}
	if len(args) < 3 || len(args) > 4 && !isRefToken(args[1]) {
		return protocol.ScrollArgs{}, errors.New("scroll takes a window reference and an element with a direction (up, down, left, right or into-view), or an @x,y point and two scroll amounts")
	}
	a, err := parseElementScrollTokens(args[1:])
	if err != nil {
		return protocol.ScrollArgs{}, err
	}
	a.Window = args[0]
	return a, nil
}

// parseElementScrollTokens parses an Element form after the Window: the
// Element (an Element Index or name:/id: reference), then a direction
// (up, down, left or right, any case) with optional steps, or into-view.
// The steps range is the helper's to check; a name: or id: reference the
// shell split shows up as a bad direction and is named.
func parseElementScrollTokens(rest []string) (protocol.ScrollArgs, error) {
	var a protocol.ScrollArgs
	if len(rest) < 2 {
		return a, errors.New("scroll takes an element and a direction (up, down, left, right or into-view)")
	}
	index, ref, err := parseElementRefToken(rest[0])
	if err != nil {
		if strings.Contains(rest[0], ",") {
			// A point missing its @: the point form's own message.
			if _, _, perr := parsePointRef(rest[0]); perr != nil {
				return a, perr
			}
		}
		return a, fmt.Errorf("the scroll target must be an element index, name:<text>, id:<AutomationId> or @x,y, got %q", rest[0])
	}
	a.ElementIndex, a.Ref = index, ref
	word := rest[1]
	if strings.EqualFold(word, "into-view") {
		if len(rest) != 2 {
			return a, errors.New("into-view takes no steps")
		}
		a.Direction = "into-view"
		return a, nil
	}
	dir, derr := uia.ParseScrollDirection(word)
	if derr != nil || len(rest) > 3 {
		if isRefToken(rest[0]) && len(rest) > 2 {
			return a, errors.New(unquotedRefHint(rest[0] + " " + rest[1]))
		}
		if derr != nil {
			return a, fmt.Errorf("the scroll direction must be up, down, left, right or into-view, got %q", word)
		}
		return a, errors.New("scroll takes an element, a direction and at most one steps count")
	}
	a.Direction = dir.String()
	if len(rest) == 3 {
		n, cerr := strconv.Atoi(rest[2])
		if cerr != nil {
			return a, fmt.Errorf("steps must be an integer, got %q", rest[2])
		}
		a.Steps = &n
	}
	return a, nil
}

// scrollCmd implements `winuse scroll <window> @x,y <dx> <dy>` (scroll the
// Window at a point of its latest screenshot, always Headed) and the
// Element forms `winuse scroll <window> <element> <direction> [steps]` and
// `winuse scroll <window> <element> into-view` (Headless unless headed,
// the global --headed flag).
func scrollCmd(args []string, headed bool) int {
	a, err := parseScrollCommand(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, scrollUsage)
		return 2
	}
	// The point form needs it too: outside a Page it is Headed only with
	// --headed (headless-max ticket 21).
	a.Headed = headed
	if err := runWithArgs("scroll", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseDragArgs parses drag's arguments: a Window reference and two @x,y
// points — from and to, both in the latest screenshot's pixels.
func parseDragArgs(args []string) (window string, fromX, fromY, toX, toY int, err error) {
	if len(args) != 3 {
		return "", 0, 0, 0, 0, errors.New("drag takes a window reference and two @x,y points (from, to)")
	}
	fromX, fromY, err = parsePointRef(args[1])
	if err != nil {
		return "", 0, 0, 0, 0, err
	}
	toX, toY, err = parsePointRef(args[2])
	if err != nil {
		return "", 0, 0, 0, 0, err
	}
	return args[0], fromX, fromY, toX, toY, nil
}

// dragCmd implements `winuse drag <window> @x,y @x,y`: drag from one
// point of the Window's latest screenshot to another, with the real
// pointer only under the global --headed (headless-max ticket 21).
func dragCmd(args []string, headed bool) int {
	a, err := dragRequest(args, headed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, dragUsage)
		return 2
	}
	if err := runWithArgs("drag", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// dragRequest builds drag's request: the Window, then points or Page
// Elements, and the global --headed.
func dragRequest(args []string, headed bool) (protocol.DragArgs, error) {
	if len(args) == 0 {
		return protocol.DragArgs{}, errors.New("drag takes a window reference first")
	}
	a, err := parseDragTokens(args[1:]) // page.go: points, or Page Elements (ticket 06)
	if err != nil {
		return protocol.DragArgs{}, err
	}
	a.Window, a.Headed = args[0], headed
	return a, nil
}

// hoverRequest builds hover's request: the Window, then a point or a Page
// Element, and the global --headed.
func hoverRequest(args []string, headed bool) (protocol.HoverArgs, error) {
	if len(args) == 0 {
		return protocol.HoverArgs{}, errors.New("hover takes a window reference first")
	}
	a, err := parseHoverTokens(args[1:]) // page.go: a point, or a Page Element (ticket 06)
	if err != nil {
		return protocol.HoverArgs{}, err
	}
	a.Window, a.Headed = args[0], headed
	return a, nil
}

// parseHoverArgs parses hover's arguments: a Window reference and one
// @x,y point of the Window's latest screenshot.
func parseHoverArgs(args []string) (window string, x, y int, err error) {
	if len(args) != 2 {
		return "", 0, 0, errors.New("hover takes a window reference and an @x,y point")
	}
	x, y, err = parsePointRef(args[1])
	if err != nil {
		return "", 0, 0, err
	}
	return args[0], x, y, nil
}

// hoverCmd implements `winuse hover <window> @x,y`: move the pointer to
// a point of the Window's latest screenshot and leave it there, only
// under the global --headed (headless-max ticket 21).
func hoverCmd(args []string, headed bool) int {
	a, err := hoverRequest(args, headed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, hoverUsage)
		return 2
	}
	if err := runWithArgs("hover", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
