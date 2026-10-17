package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hoangvu12/winuse/internal/apps"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// Usage is the short command surface (grows as tickets land).
const Usage = `usage: winuse <command> [arguments]

commands:
  list-windows                list visible Windows: Window ID, App and title
  get-window <window>         print one Window's line (Window ID, App and title),
                               resolved by Window ID or an unambiguous App name
  list-apps                   list running and recently used Apps, with their Windows
  launch-app <name|exe path>  start an App by name or executable path, report its Window
  get-window-state <window> [--screenshot]
                               print a Window's State: its Element tree as text,
                               and with --screenshot a screenshot saved to a file
                               whose path is printed
  record-tree <window> <file> record a Window's real tree to a fixture file
  zoom <window> <x>,<y>,<w>,<h>
                               save a full-resolution crop of a region of the
                               Window's latest screenshot and print its path
  click <window> <element index | name: | id: | @x,y> [options]
                               click an Element of a Window by Element Index,
                               name: or id: reference, or at @x,y
                               coordinates from the Window's latest
                               screenshot; options: --count 1|2|3 (default 1),
                               --button left|middle|right (default left)
  type-text <window> <text>    type Unicode text into the Window's focused Element
  press-key <window> <key>     press a key or key combination by Codex's key
                               names, e.g. Return, Tab, Control_L+a, Numpad_0
  set-value <window> <element index | name: | id: | @x,y> <value>
                               set an Element's value directly (a settable
                               edit or a numeric range); @x,y sets the
                               Element under a point of the latest screenshot
  perform-secondary-action <window> <element index | name: | id: | @x,y> <action>
                               perform one of the Element's Secondary Actions
                               (Raise, Scroll Down, Expand, Collapse, Toggle,
                               Select, ...); @x,y acts on the Element under
                               a point of the latest screenshot
  batch <window> <action> [arguments] [then <action> [arguments]] ...
                               run several Actions on one Window in order,
                               stopping at the first failure (which names
                               the failed step); e.g.
                               winuse batch 101 click name:File then type-text hello then press-key Return
  scroll <window> @x,y <dx> <dy>
                               scroll the Window at a point of its latest
                               screenshot: positive dy scrolls down, positive
                               dx right; one wheel notch is 120
  drag <window> @x,y @x,y     drag the pointer from one point to another,
                               both in the Window's latest screenshot's pixels
  hover <window> @x,y         move the pointer to a point of the Window's
                               latest screenshot and leave it there (reveals
                               hover-only UI and tooltips)
  activate-window <window>    bring a Window to the foreground
  version                     print the winuse version

a <window> is a Window ID, or an App name when that App has exactly one Window;
an element index, name:<text>, id:<AutomationId> or @x,y (the latest
screenshot's pixels) name an Element of the Window's latest Window State
`

// Main runs the CLI for args (without the program name) and returns the exit
// code: 0 success, 1 command failure, 2 usage error. The first command
// invocation starts the background helper automatically; later ones reuse
// it.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, Usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Println(version.Version)
		return 0
	case "list-windows":
		return listWindowsCmd(args[1:])
	case "get-window":
		return getWindowCmd(args[1:])
	case "get-window-state":
		return getWindowStateCmd(args[1:])
	case "record-tree":
		return recordTreeCmd(args[1:])
	case "list-apps":
		return listAppsCmd(args[1:])
	case "launch-app":
		return launchAppCmd(args[1:])
	case "zoom":
		return zoomCmd(args[1:])
	case "click":
		return clickCmd(args[1:])
	case "type-text":
		return typeTextCmd(args[1:])
	case "press-key":
		return pressKeyCmd(args[1:])
	case "set-value":
		return setValueCmd(args[1:])
	case "perform-secondary-action":
		return performSecondaryActionCmd(args[1:])
	case "batch":
		return batchCmd(args[1:])
	case "scroll":
		return scrollCmd(args[1:])
	case "drag":
		return dragCmd(args[1:])
	case "hover":
		return hoverCmd(args[1:])
	case "activate-window":
		return activateWindowCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "winuse: unknown command %q\n\n%s", args[0], Usage)
		return 2
	}
}

// listWindowsCmd prints one line per visible Window:
// "<Window ID>\t<App>\t<title>".
func listWindowsCmd(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "winuse: list-windows takes no arguments")
		return 2
	}
	if err := runSimple("list-windows"); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
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
	Screenshot bool   `json:"screenshot"` // get-window-state --screenshot
}

// recordArgs is record-tree's CLI→helper argument shape.
type recordArgs struct {
	Window string `json:"window"`
	File   string `json:"file"`
}

// getWindowStateCmd prints the Window State: Codex's depth-first,
// indented, numbered Element tree — with --screenshot, the screenshot line
// after it.
func getWindowStateCmd(args []string) int {
	window, screenshot, err := parseWindowStateArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\nusage: winuse get-window-state <window> [--screenshot]\n", err)
		return 2
	}
	if err := runWithArgs("get-window-state", windowArgs{Window: window, Screenshot: screenshot}); err != nil {
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
// Window it opened (one list-windows-style line). Paths are resolved
// against the CLI's working directory — the helper cannot see it.
func launchAppCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: winuse launch-app <name|exe path>")
		return 2
	}
	app := args[0]
	if apps.LooksLikePath(app) {
		if abs, err := filepath.Abs(app); err == nil {
			app = abs
		}
	}
	if err := runWithArgs("launch-app", protocol.LaunchArgs{App: app}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
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
	if !win.HasInputDesktop() {
		return win.ErrNoDesktop
	}
	client, err := DefaultClient()
	if err != nil {
		return err
	}
	req, err := Request(cmd, args)
	if err != nil {
		return err
	}
	resp, err := client.Call(context.Background(), req)
	if err != nil {
		return err
	}
	return finish(resp)
}

// finish turns a response into an error carrying the exit code the CLI
// should use; helper-reported failures keep their exact message text.
func finish(resp *protocol.Response) error {
	if resp.OK {
		os.Stdout.WriteString(resp.Out)
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
