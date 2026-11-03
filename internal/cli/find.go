package cli

// find and get-window-state's partial reads, CLI side (desktop-parity
// ticket 04): `winuse find <window> [--role <r>] [--name <text>] [--text
// <text>] [--state <s>] [--first | --count]`, and get-window-state's
// --root <element> [--depth N] [--interactive] and --skeleton. Parsing is
// local; the helper validates the values (the state names, an Element
// that exists).

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// findUsage is find's usage line.
const findUsage = "usage: winuse find <window> [--role <role>] [--name <text>] [--text <text>] [--state <state>] [--first | --count] [--timeout <seconds>]"

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
		case "role", "name", "text", "state":
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
	if a.First && a.Count {
		return a, errors.New("pass --first or --count, not both")
	}
	a.Window = pos[0]
	return a, nil
}

// windowShape is get-window-state's partial-read flags.
type windowShape struct {
	Root        string
	Depth       int
	Interactive bool
	Skeleton    bool
}

// takeShapeFlags takes --root <element>, --depth N, --interactive and
// --skeleton out of get-window-state's arguments and returns the rest.
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
		case "interactive", "skeleton":
			if inline {
				return nil, sh, fmt.Errorf("--%s is a flag (no value)", name)
			}
			if name == "interactive" {
				sh.Interactive = true
			} else {
				sh.Skeleton = true
			}
			continue
		case "root", "depth":
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
		if name == "root" {
			if strings.TrimSpace(value) == "" {
				return nil, sh, errors.New("--root needs an element index, name: or id: reference")
			}
			sh.Root = value
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return nil, sh, fmt.Errorf("--depth needs a positive number, got %q", value)
		}
		sh.Depth = n
	}
	return rest, sh, nil
}
