package cli

// `winuse read <window>[:<tab>] [--root <element>] [--urls full]
// [--max-chars N | --out <file>]` (.scratch/bladebro-ideas ticket 25): a
// Page's main content as Markdown. Parsing is local (usage errors exit 2
// and never dial the helper); --out is made absolute here, where the
// working directory is known.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// parseReadArgs parses read's arguments, the Window first.
func parseReadArgs(args []string) (protocol.ReadArgs, error) {
	var a protocol.ReadArgs
	var rest []string
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if !strings.HasPrefix(tok, "--") {
			rest = append(rest, tok)
			continue
		}
		name, value, inline := strings.Cut(tok[2:], "=")
		switch name {
		case "root", "urls", "max-chars", "out":
		default:
			return a, fmt.Errorf("read has no option %s", tok)
		}
		if !inline {
			if i+1 >= len(args) {
				return a, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = args[i]
		}
		switch name {
		case "root":
			if strings.TrimSpace(value) == "" {
				return a, errors.New("--root needs an element index, name: or id: reference")
			}
			a.Root = value
		case "urls":
			if value != "full" {
				return a, fmt.Errorf("--urls takes full, not %q (without it, links are shortened)", value)
			}
			a.URLs = value
		case "out":
			if strings.TrimSpace(value) == "" {
				return a, errors.New("--out needs a file")
			}
			abs, err := filepath.Abs(value)
			if err != nil {
				return a, fmt.Errorf("--out %q: %v", value, err)
			}
			a.Out = abs
		default:
			n, err := charCap(value)
			if err != nil {
				return a, err
			}
			a.MaxChars = &n
		}
	}
	switch {
	case len(rest) == 0:
		return a, errors.New("read takes a window reference first")
	case len(rest) > 1:
		return a, fmt.Errorf("read takes one window reference, got %q", strings.Join(rest, " "))
	case a.Out != "" && a.MaxChars != nil && *a.MaxChars != 0:
		return a, errors.New("pass --out or --max-chars, not both: --out writes the whole Markdown to the file")
	}
	a.Window = rest[0]
	return a, nil
}

// readCmd implements `winuse read`.
func readCmd(args []string) int {
	a, err := parseReadArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, protocol.ReadUsage)
		return 2
	}
	if err := runWithArgs("read", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
