package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// Usage is the short command surface (grows as tickets land).
const usage = `usage: winuse <command> [arguments]

commands:
  list-windows    list visible Windows: Window ID, App and title
  version         print the winuse version
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

// runSimple sends a no-argument command to the helper and prints its
// response Codex-style: output on stdout, a single-line failure with a
// recovery hint on stderr, non-zero exit on failure.
func runSimple(cmd string) error {
	if !win.HasInputDesktop() {
		return win.ErrNoDesktop
	}
	client, err := DefaultClient()
	if err != nil {
		return err
	}
	req, err := Request(cmd, nil)
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
