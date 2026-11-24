package cli

// extract, CLI side (bladebro ideas, ticket 26): `winuse extract
// <window>[:<tab>] [--root <element>] [--as list|table|jsonld] [--limit N]
// [--max-chars N | --out <file>]`. Parsing is local and --out becomes
// absolute; the helper checks the values.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func extractCmd(args []string) int {
	a, err := parseExtractArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, protocol.ExtractUsage)
		return 2
	}
	if err := runWithArgs("extract", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseExtractArgs parses extract's Window and flags, flags anywhere, as
// --flag value or --flag=value.
func parseExtractArgs(args []string) (protocol.ExtractArgs, error) {
	var a protocol.ExtractArgs
	var pos []string
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if !strings.HasPrefix(tok, "--") {
			pos = append(pos, tok)
			continue
		}
		name, value, inline := strings.Cut(tok[2:], "=")
		switch name {
		case "root", "as", "limit", "max-chars", "out":
		default:
			return a, fmt.Errorf("unknown flag %s", tok)
		}
		if seen[name] {
			return a, fmt.Errorf("--%s is given twice", name)
		}
		seen[name] = true
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
		case "root":
			a.Root = value
		case "as":
			switch value {
			case "list", "table", "jsonld":
				a.As = value
			default:
				return a, fmt.Errorf("--as takes list, table or jsonld, got %q", value)
			}
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
		case "out":
			abs, err := filepath.Abs(value)
			if err != nil {
				return a, fmt.Errorf("--out %q: %v", value, err)
			}
			a.Out = abs
		}
	}
	if a.Out != "" && a.MaxChars != nil && *a.MaxChars > 0 {
		return a, errors.New("pass --out or --max-chars, not both: --out writes every row to the file")
	}
	if len(pos) != 1 {
		return a, errors.New("extract takes one window reference")
	}
	a.Window = pos[0]
	return a, nil
}
