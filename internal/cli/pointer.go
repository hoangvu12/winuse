package cli

// The pointer Action commands' CLI side (ticket 10): scroll, drag and
// hover. All three take `@x,y` coordinate Element References of the
// Window's latest screenshot — the same syntax click's coordinate form
// uses — so parsing is positional and strict (usage errors exit 2 and
// never dial the helper); value validation (amount bounds, whether a
// screenshot exists) stays in the helper so the CLI and any other client
// agree on the messages.

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// The commands' usage lines.
const (
	scrollUsage = "usage: winuse scroll <window> @x,y <dx> <dy>   (positive dy scrolls down; one wheel notch is 120)"
	dragUsage   = "usage: winuse drag <window> @x,y @x,y   (from, to — both in the latest screenshot's pixels)"
	hoverUsage  = "usage: winuse hover <window> @x,y   (the point, in the latest screenshot's pixels)"
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

// scrollCmd implements `winuse scroll <window> @x,y <dx> <dy>`: scroll
// the Window at a point of its latest screenshot.
func scrollCmd(args []string) int {
	window, x, y, dx, dy, err := parseScrollArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, scrollUsage)
		return 2
	}
	if err := runWithArgs("scroll", protocol.ScrollArgs{
		Window:  window,
		X:       x,
		Y:       y,
		ScrollX: dx,
		ScrollY: dy,
	}); err != nil {
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
// point of the Window's latest screenshot to another.
func dragCmd(args []string) int {
	window, fromX, fromY, toX, toY, err := parseDragArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, dragUsage)
		return 2
	}
	if err := runWithArgs("drag", protocol.DragArgs{
		Window: window,
		FromX:  fromX,
		FromY:  fromY,
		ToX:    toX,
		ToY:    toY,
	}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
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
// a point of the Window's latest screenshot and leave it there.
func hoverCmd(args []string) int {
	window, x, y, err := parseHoverArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, hoverUsage)
		return 2
	}
	if err := runWithArgs("hover", protocol.HoverArgs{Window: window, X: x, Y: y}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
