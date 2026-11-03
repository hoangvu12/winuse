package cli

// The displays' Desktop commands, CLI side (desktop-parity ticket 08):
// list-displays and screenshot --display <n>. Argument parsing is local
// (usage errors exit 2 and never dial the helper).

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

const screenshotUsage = "usage: winuse screenshot --display <n>   (n from winuse list-displays; 0 is the primary display)"

// listDisplaysCmd implements `winuse list-displays`: one line per display,
// its index, rectangle in physical pixels, scale and the primary flag.
func listDisplaysCmd(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "winuse: list-displays takes no arguments")
		return 2
	}
	if err := runSimple("list-displays"); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// screenshotCmd implements `winuse screenshot --display <n>`: a
// screenshot of one display.
func screenshotCmd(args []string) int {
	a, err := parseScreenshotArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, screenshotUsage)
		return 2
	}
	if err := runWithArgs("screenshot", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseScreenshotArgs parses --display <n> or --display=<n>, a display
// index from 0.
func parseScreenshotArgs(args []string) (protocol.ScreenshotArgs, error) {
	var a protocol.ScreenshotArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg != "--display" && !strings.HasPrefix(arg, "--display=") {
			return protocol.ScreenshotArgs{}, fmt.Errorf("screenshot: unexpected argument %q (a Window's screenshot is winuse get-window-state <window> --screenshot)", arg)
		}
		if a.Display != nil {
			return protocol.ScreenshotArgs{}, errors.New("--display is given twice")
		}
		value, ok := strings.CutPrefix(arg, "--display=")
		if !ok {
			if i+1 >= len(args) {
				return protocol.ScreenshotArgs{}, errors.New("--display takes a display number")
			}
			i++
			value = args[i]
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return protocol.ScreenshotArgs{}, fmt.Errorf("--display takes a display number from 0, got %q", value)
		}
		a.Display = &n
	}
	if a.Display == nil {
		return protocol.ScreenshotArgs{}, errors.New("screenshot takes --display <n>; a Window's screenshot is winuse get-window-state <window> --screenshot")
	}
	return a, nil
}
