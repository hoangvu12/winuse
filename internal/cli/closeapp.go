package cli

// close-app's CLI side (desktop-parity ticket 10): `winuse close-app <app>
// [--force]`. Headless, with no headed form. Argument parsing is local
// (usage errors exit 2 and never dial the helper).

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

const closeAppUsage = "usage: winuse close-app <app> [--force]"

// closeAppCmd implements `winuse close-app <app> [--force]`: ask every
// Window of the App to close and wait for it to exit; --force ends the
// App's processes still running after that.
func closeAppCmd(args []string) int {
	a, err := parseCloseAppArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, closeAppUsage)
		return 2
	}
	if err := runWithArgs("close-app", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseCloseAppArgs parses close-app's App name and --force, in either
// order.
func parseCloseAppArgs(args []string) (protocol.CloseAppArgs, error) {
	var a protocol.CloseAppArgs
	var positional []string
	for _, arg := range args {
		switch {
		case arg == "--force":
			a.Force = true
		case strings.HasPrefix(arg, "--"):
			return protocol.CloseAppArgs{}, fmt.Errorf("unknown flag %s", arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return protocol.CloseAppArgs{}, errors.New("close-app takes exactly one App")
	}
	a.App = positional[0]
	return a, nil
}
