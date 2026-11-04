package cli

// The Browser Connection commands' CLI side (browser merge tickets 01 and
// 02): winuse browser status|use|launch|clone|connect|stop|ext install|ext
// status, and attach|detach <window> (headless-max 15). Argument parsing is
// local (usage errors exit 2 and never dial the helper); the helper's
// browser-* commands do the work and keep the connections.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

const browserUsage = `usage: winuse browser status [browser]
       winuse browser use [browser|auto]
       winuse browser launch [browser]
       winuse browser clone [browser] [--profile <folder>] [--fresh] [--close-source]
       winuse browser connect [browser]
       winuse browser stop [browser]
       winuse browser ext install [browser|all] [--open]
       winuse browser ext status [browser] [--wait <seconds>]
       winuse browser attach <window>
       winuse browser detach <window>`

// browserCmd implements `winuse browser <subcommand> [arguments]`.
func browserCmd(args []string) int {
	cmd, req, err := parseBrowserArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, browserUsage)
		return 2
	}
	run := func() error { return runWithArgs(cmd, req) }
	if a, ok := req.(protocol.BrowserExtArgs); ok && a.Wait > 0 {
		run = func() error {
			// A failed status prints nothing on stdout, so only the last
			// failure is reported, below.
			return extStatusWait(a, func() error { return runWithArgs(cmd, req) }, time.Sleep, time.Now)
		}
	}
	if err := run(); err != nil {
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
	case "attach", "detach":
		if len(rest) != 1 || strings.HasPrefix(rest[0], "-") {
			return "", nil, fmt.Errorf("browser %s takes one window: a Window of the Electron App", sub)
		}
		return "browser-" + sub, protocol.AttachArgs{Window: rest[0]}, nil
	default:
		return "", nil, fmt.Errorf("unknown browser subcommand %q", sub)
	}
}

// maxExtWait caps `browser ext status --wait`.
const maxExtWait = 600

// parseBrowserExtArgs parses `browser ext install [browser|all] [--open]`
// and `browser ext status [browser] [--wait <seconds>]`.
func parseBrowserExtArgs(args []string) (string, any, error) {
	if len(args) == 0 {
		return "", nil, errors.New("browser ext needs install or status")
	}
	sub, rest := args[0], args[1:]
	if sub != "install" && sub != "status" {
		return "", nil, fmt.Errorf("unknown browser ext subcommand %q", sub)
	}
	var a protocol.BrowserExtArgs
	for i := 0; i < len(rest); i++ {
		switch arg := rest[i]; {
		case arg == "--open" && sub == "install":
			a.Open = true
		case arg == "--wait" && sub == "status":
			if i+1 >= len(rest) {
				return "", nil, errors.New("--wait needs a number of seconds, e.g. --wait 120")
			}
			i++
			n, err := strconv.Atoi(rest[i])
			if err != nil || n < 1 || n > maxExtWait {
				return "", nil, fmt.Errorf("--wait takes 1 to %d seconds, not %q", maxExtWait, rest[i])
			}
			a.Wait = n
		case strings.HasPrefix(arg, "-"):
			return "", nil, fmt.Errorf("browser ext %s has no option %s", sub, arg)
		case a.Browser != "":
			return "", nil, fmt.Errorf("browser ext %s takes at most one browser name", sub)
		default:
			a.Browser = strings.ToLower(arg)
		}
	}
	if a.Browser == "all" && sub == "status" {
		return "", nil, errors.New("browser ext status takes one browser name, or none for every connected browser")
	}
	return "browser-ext-" + sub, a, nil
}

// extStatusWait runs `browser ext status --wait <seconds>` (browser-ux 02):
// the status once a second until it succeeds or the time is up, then that
// last answer. Each call is a short helper request, so nothing runs long
// in the helper while the user loads the extension.
func extStatusWait(a protocol.BrowserExtArgs, call func() error, sleep func(time.Duration), now func() time.Time) error {
	deadline := now().Add(time.Duration(a.Wait) * time.Second)
	for {
		err := call()
		if err == nil || !now().Before(deadline) {
			return err
		}
		var ee *exitError
		if !errors.As(err, &ee) || !extWaitable(ee.msg) {
			return err // nothing the user's next step in the browser changes
		}
		sleep(time.Second)
	}
}

// extWaitable reports whether a status failure can clear while the user
// works in the browser: the extension not yet loaded, switched off, its
// browser closed, or not yet connected. Not installed or not registered
// need a command, and polling would only repeat them.
func extWaitable(msg string) bool {
	for _, s := range []string{"is not loaded in", "but switched off", "is not running", "has not connected to winuse"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
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
