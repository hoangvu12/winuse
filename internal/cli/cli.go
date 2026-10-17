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
const usage = `usage: winuse <command> [arguments]

commands:
  list-windows                list visible Windows: Window ID, App and title
  list-apps                   list running and recently used Apps, with their Windows
  launch-app <name|exe path>  start an App by name or executable path, report its Window
  get-window-state <window>   print a Window's State: its Element tree as text
  record-tree <window> <file> record a Window's real tree to a fixture file
  click <window> <element index> [options]
                               click an Element of a Window: single, double or
                               right click; options: --count 1|2|3 (default 1),
                               --button left|middle|right (default left)
  type-text <window> <text>    type Unicode text into the Window's focused Element
  press-key <window> <key>     press a key or key combination by Codex's key
                               names, e.g. Return, Tab, Control_L+a, Numpad_0
  activate-window <window>    bring a Window to the foreground
  version                     print the winuse version

a <window> is a Window ID, or an App name when that App has exactly one Window
`

// Main runs the CLI for args (without the program name) and returns the exit
// code: 0 success, 1 command failure, 2 usage error. The first command
// invocation starts the background helper automatically; later ones reuse
// it.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Println(version.Version)
		return 0
	case "list-windows":
		return listWindowsCmd(args[1:])
	case "get-window-state":
		return getWindowStateCmd(args[1:])
	case "record-tree":
		return recordTreeCmd(args[1:])
	case "list-apps":
		return listAppsCmd(args[1:])
	case "launch-app":
		return launchAppCmd(args[1:])
	case "click":
		return clickCmd(args[1:])
	case "type-text":
		return typeTextCmd(args[1:])
	case "press-key":
		return pressKeyCmd(args[1:])
	case "activate-window":
		return activateWindowCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "winuse: unknown command %q\n\n%s", args[0], usage)
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

// windowArgs is the CLI→helper argument shape of commands that name a
// Window.
type windowArgs struct {
	Window string `json:"window"`
}

// recordArgs is record-tree's CLI→helper argument shape.
type recordArgs struct {
	Window string `json:"window"`
	File   string `json:"file"`
}

// getWindowStateCmd prints the Window State: Codex's depth-first,
// indented, numbered Element tree.
func getWindowStateCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: winuse get-window-state <window>")
		return 2
	}
	if err := runWithArgs("get-window-state", windowArgs{Window: args[0]}); err != nil {
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
