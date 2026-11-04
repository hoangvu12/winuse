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
		case "first", "count":
			if inline {
				return a, fmt.Errorf("--%s is a flag (no value)", name)
			}
			if name == "first" {
				a.First = true
			} else {
				a.Count = true
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
		case "limit", "max-chars":
			n, err := positive(name, value)
			if err != nil {
				return a, err
			}
			if name == "limit" {
				a.Limit = n
			} else {
				a.MaxChars = n
			}
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

// windowShape is get-window-state's partial-read and output-budget flags.
type windowShape struct {
	Root        string
	Depth       int
	Interactive bool
	Skeleton    bool
	Compact     bool
	MaxChars    int
	Out         string // absolute
}

// takeShapeFlags takes --root <element>, --depth N, --interactive,
// --skeleton, --compact, --max-chars N and --out <file> out of
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
		case "interactive", "skeleton", "compact":
			if inline {
				return nil, sh, fmt.Errorf("--%s is a flag (no value)", name)
			}
			switch name {
			case "interactive":
				sh.Interactive = true
			case "skeleton":
				sh.Skeleton = true
			default:
				sh.Compact = true
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
		default:
			n, err := positive(name, value)
			if err != nil {
				return nil, sh, err
			}
			if name == "depth" {
				sh.Depth = n
			} else {
				sh.MaxChars = n
			}
		}
	}
	if sh.Out != "" && sh.MaxChars > 0 {
		return nil, sh, errors.New("pass --out or --max-chars, not both: --out writes the whole Window State to the file")
	}
	return rest, sh, nil
}
