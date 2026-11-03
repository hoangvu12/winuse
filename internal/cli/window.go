package cli

// The Window Actions' CLI side (ADR 0011): restore-window,
// minimize-window, move-window, resize-window and close-window. They are
// always Headless and take no --headed. maximize-window (desktop-parity
// ticket 11) is the one Headed Window Action: it forwards --headed, and
// the helper refuses it without. Argument parsing is local (usage errors
// exit 2 and never dial the helper); the helper checks the same limits
// for any other client.

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/hoangvu12/winuse/internal/protocol"
)

const (
	moveWindowUsage     = "usage: winuse move-window <window> <x> <y>"
	resizeWindowUsage   = "usage: winuse resize-window <window> <width> <height>"
	maximizeWindowUsage = "usage: winuse --headed maximize-window <window>"

	// The coordinate and size limits, as the helper checks them.
	maxWindowCoordinate = 1_000_000
	maxWindowSize       = 1_000_000
)

// windowActionCmd implements the Window Actions that name only the
// Window: `winuse restore-window|minimize-window|close-window <window>`.
func windowActionCmd(cmd string, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: winuse %s <window>\n", cmd)
		return 2
	}
	return runWindowAction(cmd, protocol.WindowActionArgs{Window: args[0]})
}

// moveWindowCmd implements `winuse move-window <window> <x> <y>`: move
// the Window's visible frame top-left (the screenshot line's `window at`
// rectangle) to a screen point, in physical pixels.
func moveWindowCmd(args []string) int {
	a, err := parseMoveWindowArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, moveWindowUsage)
		return 2
	}
	return runWindowAction("move-window", a)
}

// resizeWindowCmd implements `winuse resize-window <window> <width>
// <height>`: set the Window's visible frame size, in physical pixels.
func resizeWindowCmd(args []string) int {
	a, err := parseResizeWindowArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, resizeWindowUsage)
		return 2
	}
	return runWindowAction("resize-window", a)
}

// maximizeWindowCmd implements `winuse --headed maximize-window
// <window>`: maximize the Window for real, which brings it to the
// foreground. headed is the global --headed flag; without it the helper
// sends nothing and prints the --headed retry.
func maximizeWindowCmd(args []string, headed bool) int {
	a, err := parseMaximizeWindowArgs(args, headed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, maximizeWindowUsage)
		return 2
	}
	return runWindowAction("maximize-window", a)
}

// parseMaximizeWindowArgs parses maximize-window's Window and carries the
// global --headed flag.
func parseMaximizeWindowArgs(args []string, headed bool) (protocol.MaximizeWindowArgs, error) {
	if len(args) != 1 {
		return protocol.MaximizeWindowArgs{}, errors.New("maximize-window takes one window reference")
	}
	return protocol.MaximizeWindowArgs{Window: args[0], Headed: headed}, nil
}

func runWindowAction(cmd string, args any) int {
	if err := runWithArgs(cmd, args); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseMoveWindowArgs parses move-window's Window and integer screen
// point. Negative coordinates are valid (a monitor left of or above the
// primary one).
func parseMoveWindowArgs(args []string) (protocol.MoveWindowArgs, error) {
	if len(args) != 3 {
		return protocol.MoveWindowArgs{}, errors.New("move-window takes a window reference, x and y")
	}
	x, err := windowNumber("x", args[1])
	if err != nil {
		return protocol.MoveWindowArgs{}, err
	}
	y, err := windowNumber("y", args[2])
	if err != nil {
		return protocol.MoveWindowArgs{}, err
	}
	if x < -maxWindowCoordinate || x > maxWindowCoordinate || y < -maxWindowCoordinate || y > maxWindowCoordinate {
		return protocol.MoveWindowArgs{}, fmt.Errorf("x and y must be between %d and %d, got %d,%d",
			-maxWindowCoordinate, maxWindowCoordinate, x, y)
	}
	return protocol.MoveWindowArgs{Window: args[0], X: x, Y: y}, nil
}

// parseResizeWindowArgs parses resize-window's Window and positive integer
// size.
func parseResizeWindowArgs(args []string) (protocol.ResizeWindowArgs, error) {
	if len(args) != 3 {
		return protocol.ResizeWindowArgs{}, errors.New("resize-window takes a window reference, a width and a height")
	}
	width, err := windowNumber("width", args[1])
	if err != nil {
		return protocol.ResizeWindowArgs{}, err
	}
	height, err := windowNumber("height", args[2])
	if err != nil {
		return protocol.ResizeWindowArgs{}, err
	}
	if width < 1 || width > maxWindowSize || height < 1 || height > maxWindowSize {
		return protocol.ResizeWindowArgs{}, fmt.Errorf("width and height must be between 1 and %d, got %dx%d",
			maxWindowSize, width, height)
	}
	return protocol.ResizeWindowArgs{Window: args[0], Width: width, Height: height}, nil
}

// windowNumber parses one integer argument of move-window or
// resize-window.
func windowNumber(name, s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer (physical pixels), got %q", name, s)
	}
	return n, nil
}
