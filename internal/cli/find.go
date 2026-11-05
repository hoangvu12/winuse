package cli

// find and get-window-state's partial reads, CLI side (desktop-parity
// ticket 04): `winuse find <window> [--role <r>] [--name <text>] [--text
// <text>] [--state <s>] [--first | --limit N | --count] [--max-chars N]`,
// and get-window-state's --root <element> [--depth N] [--interactive],
// --skeleton and the output budgets of a full read (--interactive, --depth,
// --compact, --max-chars, --out). Parsing is local; the helper validates
// the values (the state names, an Element that exists).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// findUsage is find's usage line.
const findUsage = "usage: winuse find <window> [--role <role>] [--name <text>] [--text <text>] [--state <state>] [--first | --limit N | --count] [--max-chars N] [--timeout <seconds>]"

// findCmd runs `winuse find <window> ...`.
func findCmd(args []string) int {
	args, timeout, err := takeTimeout("find", args)
	var a protocol.FindArgs
	if err == nil {
		a, err = parseFindArgs(args)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, findUsage)
		return 2
	}
	if err := runWithTimeout("find", a, timeout); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseFindArgs parses find's Window and filters, flags anywhere, as
// --flag value or --flag=value.
func parseFindArgs(args []string) (protocol.FindArgs, error) {
	var a protocol.FindArgs
	var pos []string
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if !strings.HasPrefix(tok, "--") {
			pos = append(pos, tok)
			continue
		}
		name, value, inline := strings.Cut(tok[2:], "=")
		switch name {
		case "first", "count", "full":
			if inline {
				return a, fmt.Errorf("--%s is a flag (no value)", name)
			}
			switch name {
			case "first":
				a.First = true
			case "count":
				a.Count = true
			default:
				a.Full = true
			}
			continue
		case "role", "name", "text", "state", "limit", "max-chars":
		default:
			return a, fmt.Errorf("unknown flag %s", tok)
		}
		if !inline {
			if i+1 >= len(args) {
				return a, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = args[i]
		}
		if strings.TrimSpace(value) == "" {
			return a, fmt.Errorf("--%s needs a value", name)
		}
		switch name {
		case "limit":
			n, err := positive(name, value)
			if err != nil {
				return a, err
			}
			a.Limit = n
		case "max-chars":
			n, err := charCap(value)
			if err != nil {
				return a, err
			}
			a.MaxChars = &n
		case "role":
			a.Role = value
		case "name":
			a.Name = value
		case "text":
			a.Text = value
		case "state":
			a.State = value
		}
	}
	if len(pos) != 1 {
		return a, errors.New("find takes one window reference")
	}
	if a.First && a.Count || a.Limit > 0 && (a.First || a.Count) {
		return a, errors.New("pass one of --first, --limit and --count")
	}
	if a.Full && a.MaxChars != nil {
		return a, errors.New("pass --full or --max-chars, not both: --full prints every match uncut")
	}
	a.Window = pos[0]
	return a, nil
}

// positive parses a flag's value as a number of at least 1.
func positive(name, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("--%s needs a positive number, got %q", name, value)
	}
	return n, nil
}

// charCap parses --max-chars: a number of characters, 0 for no cap.
func charCap(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("--max-chars needs a number of characters (0: no cap), got %q", value)
	}
	return n, nil
}

// windowShape is get-window-state's partial-read and output-budget flags.
type windowShape struct {
	Root        string
	Depth       int
	Interactive bool
	Skeleton    bool
	Compact     bool
	MaxChars    *int   // nil: the helper's default cap (speed 13)
	Out         string // absolute
	Full        bool
}

// takeShapeFlags takes --root <element>, --depth N, --interactive,
// --skeleton, --compact, --max-chars N, --out <file> and --full out of
// get-window-state's arguments and returns the rest.
func takeShapeFlags(args []string) ([]string, windowShape, error) {
	var sh windowShape
	var rest []string
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if !strings.HasPrefix(tok, "--") {
			rest = append(rest, tok)
			continue
		}
		name, value, inline := strings.Cut(tok[2:], "=")
		switch name {
		case "interactive", "skeleton", "compact", "full":
			if inline {
				return nil, sh, fmt.Errorf("--%s is a flag (no value)", name)
			}
			switch name {
			case "interactive":
				sh.Interactive = true
			case "skeleton":
				sh.Skeleton = true
			case "compact":
				sh.Compact = true
			default:
				sh.Full = true
			}
			continue
		case "root", "depth", "max-chars", "out":
		default:
			rest = append(rest, tok)
			continue
		}
		if !inline {
			if i+1 >= len(args) {
				return nil, sh, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = args[i]
		}
		switch name {
		case "root":
			if strings.TrimSpace(value) == "" {
				return nil, sh, errors.New("--root needs an element index, name: or id: reference")
			}
			sh.Root = value
		case "out":
			if strings.TrimSpace(value) == "" {
				return nil, sh, errors.New("--out needs a file")
			}
			abs, err := filepath.Abs(value)
			if err != nil {
				return nil, sh, fmt.Errorf("--out %q: %v", value, err)
			}
			sh.Out = abs
		case "depth":
			n, err := positive(name, value)
			if err != nil {
				return nil, sh, err
			}
			sh.Depth = n
		default:
			n, err := charCap(value)
			if err != nil {
				return nil, sh, err
			}
			sh.MaxChars = &n
		}
	}
	if sh.Out != "" && sh.MaxChars != nil && *sh.MaxChars > 0 {
		return nil, sh, errors.New("pass --out or --max-chars, not both: --out writes the whole Window State to the file")
	}
	if sh.Full && (sh.Compact || sh.MaxChars != nil || sh.Root == "" && (sh.Interactive || sh.Depth != 0)) {
		return nil, sh, errors.New("--full prints the Window State as Codex does, whole and uncut: it takes no --interactive, --depth, --compact or --max-chars (with --root, --depth and --interactive still shape the read)")
	}
	return rest, sh, nil
}
