package cli

// The page-only Actions' CLI side (browser merge ticket 06): navigate,
// back, forward, reload, upload, dialog, wait and eval on a browser Window
// with a Browser Connection, plus the Page Element forms of hover and
// drag. Parsing is local (usage errors exit 2 and never dial the helper);
// upload's file paths are made absolute here, where the working directory
// is known. None takes --headed: they are Headless and have no Headed
// form.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// takeWait takes a --wait <state> or --wait=<state> option out of tokens.
func takeWait(tokens []string) (rest []string, wait string, err error) {
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t == "--wait":
			if i+1 >= len(tokens) {
				return nil, "", errors.New("--wait needs load, domcontentloaded or none")
			}
			wait = tokens[i+1]
			i++
		case strings.HasPrefix(t, "--wait="):
			wait = strings.TrimPrefix(t, "--wait=")
		case strings.HasPrefix(t, "--"):
			return nil, "", fmt.Errorf("unknown option %s", t)
		default:
			rest = append(rest, t)
		}
	}
	switch strings.ToLower(wait) {
	case "", "load", "domcontentloaded", "none":
	default:
		return nil, "", fmt.Errorf("--wait must be load, domcontentloaded or none, got %q", wait)
	}
	return rest, wait, nil
}

// parseNavArgs parses navigate, back, forward and reload after the Window
// (a Batch step has none).
func parseNavArgs(cmd string, tokens []string) (protocol.NavigateArgs, error) {
	rest, wait, err := takeWait(tokens)
	if err != nil {
		return protocol.NavigateArgs{}, err
	}
	a := protocol.NavigateArgs{Wait: wait}
	if cmd == "navigate" {
		if len(rest) != 1 {
			return a, errors.New("navigate takes one URL")
		}
		a.URL = rest[0]
		return a, nil
	}
	if len(rest) != 0 {
		return a, fmt.Errorf("%s takes no other arguments; to open a URL, run winuse navigate <window> <url>", cmd)
	}
	return a, nil
}

// parseUploadArgs parses upload's element (optional: an Element Index or
// name:/id: first) and files, made absolute against the working directory.
// A file whose name is a number is written with a folder, like .\3.
func parseUploadArgs(tokens []string) (protocol.UploadArgs, error) {
	var a protocol.UploadArgs
	if len(tokens) > 0 {
		if index, ref, err := parseElementRefToken(tokens[0]); err == nil {
			a.ElementIndex, a.Ref = index, ref
			tokens = tokens[1:]
		}
	}
	if len(tokens) == 0 {
		return a, errors.New("upload needs at least one file")
	}
	for _, f := range tokens {
		if strings.HasPrefix(f, "--") {
			return a, fmt.Errorf("upload has no option %s", f)
		}
		abs, err := filepath.Abs(f)
		if err != nil {
			return a, fmt.Errorf("resolving %s: %v", f, err)
		}
		a.Files = append(a.Files, abs)
	}
	return a, nil
}

// parseDialogArgs parses accept|dismiss and a prompt's text (the rest,
// joined with spaces; "" is an empty answer).
func parseDialogArgs(tokens []string) (protocol.DialogArgs, error) {
	var a protocol.DialogArgs
	if len(tokens) == 0 {
		return a, errors.New("dialog takes accept or dismiss")
	}
	a.Action = strings.ToLower(tokens[0])
	if a.Action != "accept" && a.Action != "dismiss" {
		return a, fmt.Errorf("dialog takes accept or dismiss, got %q", tokens[0])
	}
	if len(tokens) > 1 {
		if a.Action != "accept" {
			return a, errors.New("text answers a prompt, so it goes with accept")
		}
		text := strings.Join(tokens[1:], " ")
		a.Text = &text
	}
	return a, nil
}

// maxWaitSeconds caps --timeout (desktop-parity ticket 14): the request's
// budget follows it, the wait plus protocol.WaitMargin (waitTimeout), up to
// protocol.MaxWaitTimeout. In a Batch the Batch's budget bounds it.
const maxWaitSeconds = int(protocol.MaxWaitTimeout / time.Second)

// waitTimeout is the request budget a wait asks for: its --timeout plus
// protocol.WaitMargin, so the wait runs out before the request does (0:
// no --timeout, the command's own budget).
func waitTimeout(a protocol.WaitArgs) time.Duration {
	if a.TimeoutMS <= 0 {
		return 0
	}
	return time.Duration(a.TimeoutMS)*time.Millisecond + protocol.WaitMargin
}

// parseWaitArgs parses what to wait for, its value and --timeout.
func parseWaitArgs(tokens []string) (protocol.WaitArgs, error) {
	var a protocol.WaitArgs
	var rest []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		secs := ""
		switch {
		case t == "--timeout":
			if i+1 >= len(tokens) {
				return a, errors.New("--timeout needs a number of seconds")
			}
			secs = tokens[i+1]
			i++
		case strings.HasPrefix(t, "--timeout="):
			secs = strings.TrimPrefix(t, "--timeout=")
		default:
			rest = append(rest, t)
			continue
		}
		n, err := strconv.ParseFloat(secs, 64)
		if err != nil || n <= 0 || n > float64(maxWaitSeconds) {
			return a, fmt.Errorf("--timeout must be a number of seconds above 0 and at most %d, got %q", maxWaitSeconds, secs)
		}
		a.TimeoutMS = int(n * 1000)
	}
	if len(rest) == 0 {
		return a, errors.New("wait needs what to wait for: selector, text, url, load or download")
	}
	a.For = strings.ToLower(rest[0])
	value := strings.Join(rest[1:], " ")
	switch a.For {
	case "selector", "text", "url":
		if value == "" {
			return a, fmt.Errorf("wait %s needs a value", a.For)
		}
	case "load":
		if len(rest) > 2 {
			return a, errors.New("wait load takes at most one state: load or domcontentloaded")
		}
	case "download":
		if value != "" {
			return a, errors.New("wait download takes no value")
		}
	default:
		return a, fmt.Errorf("wait for selector, text, url, load or download, not %q", rest[0])
	}
	a.Value = value
	return a, nil
}

// parsePageArgs parses a page command's arguments after the Window into
// its wire arguments (without the Window).
func parsePageArgs(cmd string, tokens []string) (any, error) {
	switch cmd {
	case "navigate", "back", "forward", "reload":
		return parseNavArgs(cmd, tokens)
	case "upload":
		return parseUploadArgs(tokens)
	case "dialog":
		return parseDialogArgs(tokens)
	case "wait":
		return parseWaitArgs(tokens)
	case "eval":
		if len(tokens) == 0 {
			return nil, errors.New("eval needs an expression")
		}
		return protocol.EvalArgs{Expression: strings.Join(tokens, " ")}, nil
	}
	return nil, fmt.Errorf("unknown page command %q", cmd)
}

// withWindowField sets the Window on a page command's wire arguments.
func withWindowField(args any, window string) any {
	switch a := args.(type) {
	case protocol.NavigateArgs:
		a.Window = window
		return a
	case protocol.UploadArgs:
		a.Window = window
		return a
	case protocol.DialogArgs:
		a.Window = window
		return a
	case protocol.WaitArgs:
		a.Window = window
		return a
	case protocol.EvalArgs:
		a.Window = window
		return a
	}
	return args
}

// pageCmd implements `winuse navigate|back|forward|reload|upload|dialog|
// wait|eval <window> ...`.
func pageCmd(cmd string, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "winuse: %s takes a window reference first\n%s\n", cmd, protocol.PageUsage(cmd))
		return 2
	}
	a, err := parsePageArgs(cmd, args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, protocol.PageUsage(cmd))
		return 2
	}
	var timeout time.Duration
	if w, ok := a.(protocol.WaitArgs); ok {
		timeout = waitTimeout(w)
	}
	if err := runWithTimeout(cmd, withWindowField(a, args[0]), timeout); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseBatchPageStep parses a page command as a Batch step (without the
// Window, which the Batch names).
func parseBatchPageStep(kind string, rest []string) (protocol.Step, error) {
	a, err := parsePageArgs(kind, rest)
	if err != nil {
		return protocol.Step{}, fmt.Errorf("a batch %s Action: %v", kind, err)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return protocol.Step{}, err
	}
	return protocol.Step{Kind: kind, Args: raw}, nil
}

// parseHoverTokens parses hover after the Window: an @x,y point, or an
// Element (an Element Index or name:/id: reference): a Page Element, or a
// UIA Element (desktop-parity 13).
func parseHoverTokens(rest []string) (protocol.HoverArgs, error) {
	var a protocol.HoverArgs
	if len(rest) != 1 {
		if len(rest) > 1 && isRefToken(rest[0]) {
			return a, errors.New(unquotedRefHint(rest[0] + " " + rest[1]))
		}
		return a, errors.New("hover takes an @x,y point or an element")
	}
	if isPointToken(rest[0]) {
		x, y, err := parsePointRef(rest[0])
		a.X, a.Y = x, y
		return a, err
	}
	index, ref, err := parseElementRefToken(rest[0])
	if err != nil {
		if strings.Contains(rest[0], ",") {
			_, _, perr := parsePointRef(rest[0]) // a point missing its @: the point form's own message
			return a, perr
		}
		return a, fmt.Errorf("the hover target must be @x,y or an element index, name:<text> or id:<AutomationId>, got %q", rest[0])
	}
	a.ElementIndex, a.Ref = index, ref
	return a, nil
}

// parseDragTokens parses drag after the Window: two @x,y points, or an
// Element (a Page Element, or a UIA Element: desktop-parity 13) and then
// another Element or an @x,y point.
func parseDragTokens(rest []string) (protocol.DragArgs, error) {
	var a protocol.DragArgs
	if len(rest) != 2 {
		if len(rest) > 2 && isRefToken(rest[0]) {
			return a, errors.New(unquotedRefHint(rest[0] + " " + rest[1]))
		}
		return a, errors.New("drag takes two @x,y points (from, to), or an element and then an element or an @x,y point")
	}
	if isPointToken(rest[0]) {
		fx, fy, err := parsePointRef(rest[0])
		if err != nil {
			return a, err
		}
		tx, ty, err := parsePointRef(rest[1])
		if err != nil {
			return a, err
		}
		return protocol.DragArgs{FromX: fx, FromY: fy, ToX: tx, ToY: ty}, nil
	}
	index, ref, err := parseElementRefToken(rest[0])
	if err != nil {
		if strings.Contains(rest[0], ",") {
			_, _, perr := parsePointRef(rest[0]) // a point missing its @: the point form's own message
			return a, perr
		}
		return a, fmt.Errorf("the drag start must be @x,y or an element index, name:<text> or id:<AutomationId>, got %q", rest[0])
	}
	a.FromIndex, a.FromRef = index, ref
	if isPointToken(rest[1]) {
		a.ToX, a.ToY, err = parsePointRef(rest[1])
		return a, err
	}
	index, ref, err = parseElementRefToken(rest[1])
	if err != nil {
		return a, fmt.Errorf("the drag end must be @x,y or an element index, name:<text> or id:<AutomationId>, got %q", rest[1])
	}
	a.ToIndex, a.ToRef = index, ref
	return a, nil
}
