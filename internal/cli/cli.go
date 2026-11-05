package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/apps"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// Main runs the CLI for args (without the program name) and returns the exit
// code: 0 success (help included), 1 command failure, 2 usage error. The
// global flags after the command are moved before it first (headed.go).
// With --json among them, the command's output becomes one JSON line
// (json.go, desktop-parity ticket 07); runCommand runs it either way.
func Main(args []string) int {
	args, asJSON, command := extractJSON(hoistGlobalFlags(args))
	if asJSON {
		return runJSON(command, func() int { return runCommand(args) })
	}
	return runCommand(args)
}

// runCommand runs one command line and prints its text. The first command
// invocation starts the background helper automatically; later ones reuse
// it. status never starts it (status.go).
//
// The global --headed flag (headed.go, ADR 0010) is taken out first,
// before the command or anywhere after it outside literal text, and
// forwarded to the Actions that have a Headless form, and to
// get-window-state and zoom, whose captures are headless (ADR 0011).
func runCommand(args []string) (code int) {
	// The session trace (trace.go, desktop-parity ticket 19): one line per
	// command, written when it exits.
	args, trace, ok := startTrace(args, os.Getenv)
	if !ok {
		return 2
	}
	activeTrace = trace
	defer func() { trace.finish(code) }()
	args, headed := extractHeaded(args)
	if len(args) == 0 {
		return helpCmd(nil) // help.go: the command index
	}
	switch args[0] {
	case "help", helpFlag, helpShortFlag:
		return helpCmd(args[1:])
	case "version":
		fmt.Println(version.Version)
		return 0
	case "list-windows":
		return listWindowsCmd(args[1:])
	case "get-window":
		return getWindowCmd(args[1:])
	case "find":
		return findCmd(args[1:]) // find.go, desktop-parity 04
	case "get-window-state":
		return getWindowStateCmd(args[1:], headed)
	case "record-tree":
		return recordTreeCmd(args[1:])
	case "list-apps":
		return listAppsCmd(args[1:])
	case "launch-app":
		return launchAppCmd(args[1:])
	case "close-app":
		return closeAppCmd(args[1:]) // closeapp.go, desktop-parity ticket 10
	case "zoom":
		return zoomCmd(args[1:], headed)
	case "click":
		return clickCmd(args[1:], headed)
	case "type-text":
		return typeTextCmd(args[1:], headed)
	case "press-key":
		return pressKeyCmd(args[1:], headed)
	case "set-value":
		return setValueCmd(args[1:], headed)
	case "perform-secondary-action":
		return performSecondaryActionCmd(args[1:], headed)
	case "batch":
		return batchCmd(args[1:], headed)
	case "scroll":
		return scrollCmd(args[1:], headed)
	case "select-text":
		return selectTextCmd(args[1:])
	case "invoke-menu":
		return invokeMenuCmd(args[1:])
	case "drag":
		return dragCmd(args[1:], headed)
	case "hover":
		return hoverCmd(args[1:], headed)
	case "activate-window":
		return activateWindowCmd(args[1:])
	case "restore-window", "minimize-window", "close-window":
		return windowActionCmd(args[0], args[1:])
	case "move-window":
		return moveWindowCmd(args[1:])
	case "resize-window":
		return resizeWindowCmd(args[1:])
	case "maximize-window":
		return maximizeWindowCmd(args[1:], headed) // window.go, desktop-parity ticket 11
	case "browser":
		return browserCmd(args[1:])
	case "tab":
		return tabCmd(args[1:], headed)
	case "navigate", "back", "forward", "reload", "upload", "dialog", "wait", "eval":
		return pageCmd(args[0], args[1:]) // page.go, browser merge ticket 06
	case "list-displays":
		return listDisplaysCmd(args[1:]) // display.go, desktop-parity ticket 08
	case "screenshot":
		return screenshotCmd(args[1:])
	case "clipboard-get":
		return clipboardGetCmd(args[1:]) // clipboard.go, desktop-parity ticket 01
	case "clipboard-set":
		return clipboardSetCmd(args[1:])
	case "clipboard-clear":
		return clipboardClearCmd(args[1:])
	case "list-notifications":
		return listNotificationsCmd(args[1:]) // notifications.go, desktop-parity ticket 18
	case "notification-action":
		return notificationActionCmd(args[1:])
	case "notification-dismiss":
		return notificationDismissCmd(args[1:])
	case "open-shell":
		return openShellCmd(args[1:], headed) // desktop-parity tickets 09 and 18
	case "skill":
		return skillCmd(args[1:])
	case "status":
		return statusCmd(args[1:])
	case "trace":
		return traceCmd(args[1:]) // trace.go, desktop-parity ticket 19
	default:
		return unknownCommand(args[0]) // help.go: did you mean, and its usage line
	}
}

const listWindowsUsage = "usage: winuse list-windows [--details] [--app <name>] [--shell]"

// listWindowsCmd prints one line per visible Window:
// "<Window ID>\t<App>\t<title>". --details adds the details column before
// the title; --app keeps one App's Windows (desktop-parity 22); --shell
// adds the shell surfaces: the taskbar, and the tray overflow, Start and
// the Action Center while open (desktop-parity 09). Without flags the
// request carries no arguments, Codex's list_windows.
func listWindowsCmd(args []string) int {
	a, err := parseListWindowsArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, listWindowsUsage)
		return 2
	}
	if a == (protocol.ListWindowsArgs{}) {
		err = runSimple("list-windows")
	} else {
		err = runWithArgs("list-windows", a)
	}
	if err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseListWindowsArgs parses list-windows's flags: --details, and
// --app <name> or --app=<name>, and --shell.
func parseListWindowsArgs(args []string) (protocol.ListWindowsArgs, error) {
	var a protocol.ListWindowsArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--details":
			a.Details = true
		case arg == "--shell":
			a.Shell = true
		case arg == "--app" || strings.HasPrefix(arg, "--app="):
			if a.App != "" {
				return protocol.ListWindowsArgs{}, errors.New("--app is given twice")
			}
			name, ok := strings.CutPrefix(arg, "--app=")
			if !ok {
				if i+1 >= len(args) {
					return protocol.ListWindowsArgs{}, errors.New("--app takes an App name")
				}
				i++
				name = args[i]
			}
			if strings.TrimSpace(name) == "" {
				return protocol.ListWindowsArgs{}, errors.New("--app takes an App name")
			}
			a.App = name
		default:
			return protocol.ListWindowsArgs{}, fmt.Errorf("list-windows: unexpected argument %q", arg)
		}
	}
	return a, nil
}

// getWindowCmd implements `winuse get-window <window>`: print the one
// list-windows line of the resolved Window (Codex's get_window — the
// re-resolve step after a helper restart or a stale Window ID).
func getWindowCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: winuse get-window <window>")
		return 2
	}
	if err := runWithArgs("get-window", protocol.GetWindowArgs{Window: args[0]}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// windowArgs is the CLI→helper argument shape of commands that name a
// Window.
type windowArgs struct {
	Window     string `json:"window"`
	Screenshot bool   `json:"screenshot"`       // get-window-state --screenshot
	Headed     bool   `json:"headed,omitempty"` // the global --headed flag: the screenshot activates first (ADR 0011)

	// The partial reads (desktop-parity 04, find.go).
	Root        string `json:"root,omitempty"`
	Depth       int    `json:"depth,omitempty"`
	Interactive bool   `json:"interactive,omitempty"`
	Skeleton    bool   `json:"skeleton,omitempty"`

	// The output budgets of a read (speed 05, find.go).
	Compact  bool   `json:"compact,omitempty"`
	MaxChars *int   `json:"max_chars,omitempty"` // nil: the helper's default cap
	Full     bool   `json:"full,omitempty"`
	Out      string `json:"out,omitempty"` // absolute
}

// recordArgs is record-tree's CLI→helper argument shape.
type recordArgs struct {
	Window string `json:"window"`
	File   string `json:"file"`
}

// windowStateUsage is get-window-state's usage line.
const windowStateUsage = "usage: winuse get-window-state <window> [--screenshot] [--skeleton | --root <element>] [--depth N] [--interactive] [--compact] [--max-chars N | --out <file>] [--timeout <seconds>]"

// getWindowStateCmd prints the Window State: Codex's depth-first,
// indented, numbered Element tree — with --screenshot, the screenshot line
// after it. headed is the global --headed flag; --timeout widens the
// request budget (up to 60 s) and the Chromium settle with it.
func getWindowStateCmd(args []string, headed bool) int {
	args, timeout, err := takeTimeout("get-window-state", args)
	var req windowArgs
	if err == nil {
		req, err = windowStateRequest(args, headed)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, windowStateUsage)
		return 2
	}
	if err := runWithTimeout("get-window-state", req, timeout); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// recordTreeCmd records a real Window's tree to a fixture file (the
// recorder; see the README).
func recordTreeCmd(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: winuse record-tree <window> <file>")
		return 2
	}
	file, err := filepath.Abs(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: resolving %s: %v\n", args[1], err)
		return 2
	}
	if err := runWithArgs("record-tree", recordArgs{Window: args[0], File: file}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// listAppsCmd prints one line per App (name, running state, last used,
// use count, launch identifier) and one line per Window of running Apps.
func listAppsCmd(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "winuse: list-apps takes no arguments")
		return 2
	}
	if err := runSimple("list-apps"); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// launchAppCmd starts an App by name or executable path and prints the
// Window it opened (one list-windows-style line, then a note line when the
// App took the foreground). Paths — the App's and --cwd — are resolved
// against the CLI's working directory: the helper cannot see it.
// --timeout widens the 15 s launch budget (up to 120 s) for a slow App.
func launchAppCmd(args []string) int {
	const usage = "usage: winuse launch-app <name|exe path> [--minimized] [--cdp] [--arg <a>]... [--cwd <dir>] [--env KEY=VALUE]... [--timeout <seconds>]"
	a, timeout, err := parseLaunchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, usage)
		return 2
	}
	if err := runWithTimeout("launch-app", a, timeout); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// launchValueFlag reports whether a launch-app flag takes the next token
// as its value, whatever that token looks like.
func launchValueFlag(tok string) bool {
	return tok == "--arg" || tok == "--cwd" || tok == "--env"
}

// parseLaunchArgs parses launch-app's command line in order: the App, and
// the flags anywhere around it. --arg and --env repeat and keep their
// order; the token after --arg, --cwd or --env is its value even when it
// starts with "-" (a flag of the App's own), and --flag=value works too.
// The App's path and --cwd become absolute here.
func parseLaunchArgs(args []string) (protocol.LaunchArgs, time.Duration, error) {
	var a protocol.LaunchArgs
	var timeout time.Duration
	var positional []string
	cwdSet := false
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if isTimeoutFlag(tok) {
			d, n, err := takeTimeoutAt("launch-app", args, i)
			if err != nil {
				return protocol.LaunchArgs{}, 0, err
			}
			timeout = d
			i += n - 1
			continue
		}
		if tok == "--minimized" {
			a.Minimized = true
			continue
		}
		if tok == "--cdp" {
			a.CDP = true
			continue
		}
		name, value, inline := strings.Cut(tok, "=")
		if !launchValueFlag(name) {
			if strings.HasPrefix(tok, "--") {
				return protocol.LaunchArgs{}, 0, fmt.Errorf("unknown flag %s", tok)
			}
			positional = append(positional, tok)
			continue
		}
		if !inline {
			if i+1 >= len(args) {
				return protocol.LaunchArgs{}, 0, fmt.Errorf("%s needs a value", name)
			}
			i++
			value = args[i]
		}
		switch name {
		case "--arg":
			a.Args = append(a.Args, value)
		case "--env":
			if _, _, err := win.ParseEnvEntry(value); err != nil {
				return protocol.LaunchArgs{}, 0, err
			}
			a.Env = append(a.Env, value)
		case "--cwd":
			if cwdSet {
				return protocol.LaunchArgs{}, 0, errors.New("--cwd given twice")
			}
			if strings.TrimSpace(value) == "" {
				return protocol.LaunchArgs{}, 0, errors.New("--cwd needs a folder")
			}
			cwdSet = true
			abs, err := filepath.Abs(value)
			if err != nil {
				return protocol.LaunchArgs{}, 0, fmt.Errorf("--cwd %q: %v", value, err)
			}
			a.Cwd = abs
		}
	}
	if len(positional) != 1 {
		return protocol.LaunchArgs{}, 0, errors.New("launch-app takes exactly one App")
	}
	a.App = positional[0]
	if apps.LooksLikePath(a.App) {
		if abs, err := filepath.Abs(a.App); err == nil {
			a.App = abs
		}
	}
	return a, timeout, nil
}

// inputDesktopExitError classifies an unavailable input desktop for the
// CLI: an interactive session whose input desktop cannot be opened is the
// lock screen / secure desktop, so the answer is the locked-desktop
// refusal — the same message and hint the helper's Actions use, exit 1 —
// never the session-0 message, which would send the agent looking for a
// service that is not the problem.
func inputDesktopExitError() error {
	err := win.InputDesktopRefusal()
	var locked *win.LockedDesktopError
	if errors.As(err, &locked) {
		return &exitError{msg: err.Error(), hint: win.LockedDesktopHint, code: 1}
	}
	return err
}

// runSimple sends a no-argument command to the helper and prints its
// response Codex-style: output on stdout, a single-line failure with a
// recovery hint on stderr, non-zero exit on failure.
func runSimple(cmd string) error {
	return runWithArgs(cmd, nil)
}

// runWithArgs sends one command (with its JSON arguments) to the helper and
// prints its response Codex-style.
func runWithArgs(cmd string, args any) error {
	return runRequest(cmd, args, nil)
}

// runRequest is runWithArgs with a hook that adjusts the request before
// it is sent (a caller-set timeout, timeout.go). edit may be nil.
func runRequest(cmd string, args any, edit func(*protocol.Request)) error {
	if !win.HasInputDesktop() {
		return inputDesktopExitError()
	}
	client, err := DefaultClient()
	if err != nil {
		return err
	}
	req, err := Request(cmd, args)
	if err != nil {
		return err
	}
	if edit != nil {
		edit(req)
	}
	req.Trace = tracing()
	resp, err := client.Call(context.Background(), req)
	activeTrace.observe(req, resp, err) // trace.go
	if err != nil {
		return err
	}
	return finish(resp)
}

// finish turns a response into an error carrying the exit code the CLI
// should use; helper-reported failures keep their exact message text. A
// failure's output (the steps a Batch ran) is printed first. --json reads
// the failure's code and delivery from the response it records (json.go).
func finish(resp *protocol.Response) error {
	lastResponse = resp
	os.Stdout.WriteString(resp.Out)
	if resp.OK {
		return nil
	}
	code := resp.Exit
	if code == 0 {
		code = 1
	}
	return &exitError{msg: resp.Err, hint: resp.Hint, code: code}
}

// exitError is a failure whose message is already the user-facing line,
// with a helper-suggested exit code (e.g. 130 for Esc stops).
type exitError struct {
	msg  string
	hint string
	code int
}

func (e *exitError) Error() string { return e.msg }

// ExitCode extracts the process exit code an error prescribes (default 1).
func ExitCode(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

// reportError prints a failure, Codex-style: one line, then an optional
// hint line. Helper-reported messages and request timeouts are printed
// verbatim (their text is the decided user-facing style, e.g. "winuse
// request timed out: <command>"); purely local failures get a "winuse: "
// prefix so no failure line is ever bare.
func reportError(w io.Writer, err error) {
	var timeout *TimeoutError
	if errors.As(err, &timeout) {
		fmt.Fprintln(w, timeout)
		return
	}
	var ee *exitError
	if errors.As(err, &ee) {
		fmt.Fprintln(w, ee.msg)
		if ee.hint != "" {
			fmt.Fprintf(w, "hint: %s\n", ee.hint)
		}
		return
	}
	fmt.Fprintf(w, "winuse: %v\n", err)
}
