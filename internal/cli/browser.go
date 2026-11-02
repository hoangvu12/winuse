package cli

// The Browser Connection commands' CLI side (browser merge tickets 01 and
// 02): winuse browser status|use|launch|clone|connect|stop|ext install|ext
// status. Argument parsing is
// local (usage errors exit 2 and never dial the helper); the helper's
// browser-* commands do the work and keep the connections.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

const browserUsage = `usage: winuse browser status [browser]
       winuse browser use [browser|auto]
       winuse browser launch [browser]
       winuse browser clone [browser] [--profile <folder>] [--fresh] [--close-source]
       winuse browser connect [browser]
       winuse browser stop [browser]
       winuse browser ext install [browser|all]
       winuse browser ext status [browser]`

// browserCmd implements `winuse browser <subcommand> [arguments]`.
func browserCmd(args []string) int {
	cmd, req, err := parseBrowserArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, browserUsage)
		return 2
	}
	if err := runWithArgs(cmd, req); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseBrowserArgs turns `browser <subcommand> ...` into the helper
// command and its arguments.
func parseBrowserArgs(args []string) (string, any, error) {
	if len(args) == 0 {
		return "", nil, errors.New("browser needs a subcommand")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "status", "use", "launch", "connect", "stop":
		if len(rest) > 1 {
			return "", nil, fmt.Errorf("browser %s takes at most one browser name", sub)
		}
		var a protocol.BrowserArgs
		if len(rest) == 1 {
			if strings.HasPrefix(rest[0], "-") {
				return "", nil, fmt.Errorf("browser %s has no option %s", sub, rest[0])
			}
			a.Browser = strings.ToLower(rest[0])
		}
		return "browser-" + sub, a, nil
	case "clone":
		a, err := parseBrowserCloneArgs(rest)
		return "browser-clone", a, err
	case "ext":
		return parseBrowserExtArgs(rest)
	default:
		return "", nil, fmt.Errorf("unknown browser subcommand %q", sub)
	}
}

// parseBrowserExtArgs parses `browser ext install [browser|all]` and
// `browser ext status [browser]`.
func parseBrowserExtArgs(args []string) (string, any, error) {
	if len(args) == 0 {
		return "", nil, errors.New("browser ext needs install or status")
	}
	sub, rest := args[0], args[1:]
	if sub != "install" && sub != "status" {
		return "", nil, fmt.Errorf("unknown browser ext subcommand %q", sub)
	}
	if len(rest) > 1 {
		return "", nil, fmt.Errorf("browser ext %s takes at most one browser name", sub)
	}
	var a protocol.BrowserArgs
	if len(rest) == 1 {
		if strings.HasPrefix(rest[0], "-") {
			return "", nil, fmt.Errorf("browser ext %s has no option %s", sub, rest[0])
		}
		a.Browser = strings.ToLower(rest[0])
		if a.Browser == "all" && sub == "status" {
			return "", nil, errors.New("browser ext status takes one browser name, or none for every connected browser")
		}
	}
	return "browser-ext-" + sub, a, nil
}

// parseBrowserCloneArgs parses `browser clone [browser] [--profile
// <folder>] [--fresh] [--close-source]`, flags in any order.
func parseBrowserCloneArgs(args []string) (protocol.BrowserCloneArgs, error) {
	var a protocol.BrowserCloneArgs
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--profile":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return a, errors.New("--profile needs a profile folder, e.g. --profile \"Profile 1\"")
			}
			i++
			a.Profile = args[i]
		case "--fresh":
			a.Fresh = true
		case "--close-source":
			a.CloseSource = true
		default:
			if strings.HasPrefix(arg, "-") {
				return a, fmt.Errorf("browser clone has no option %s", arg)
			}
			if a.Browser != "" {
				return a, errors.New("browser clone takes at most one browser name")
			}
			a.Browser = strings.ToLower(arg)
		}
	}
	return a, nil
}
