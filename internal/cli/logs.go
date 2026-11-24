package cli

// `winuse logs <window>[:<tab>] [--clear] [--max-chars N | --out <file>]`
// (.scratch/bladebro-ideas ticket 29): a Tab's failed requests and CSP or
// security errors. Parsing is local (usage errors exit 2 and never dial
// the helper); --out is made absolute here, where the working directory is
// known; the global --json asks the helper for its JSON answer.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// parseLogsArgs parses logs' arguments, the Window first.
func parseLogsArgs(args []string) (protocol.LogsArgs, error) {
	var a protocol.LogsArgs
	var rest []string
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if !strings.HasPrefix(tok, "--") {
			rest = append(rest, tok)
			continue
		}
		name, value, inline := strings.Cut(tok[2:], "=")
		switch name {
		case "clear":
			if inline {
				return a, errors.New("--clear takes no value")
			}
			a.Clear = true
			continue
		case "max-chars", "out":
		default:
			return a, fmt.Errorf("logs has no option %s", tok)
		}
		if !inline {
			if i+1 >= len(args) {
				return a, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = args[i]
		}
		if name == "out" {
			if strings.TrimSpace(value) == "" {
				return a, errors.New("--out needs a file")
			}
			abs, err := filepath.Abs(value)
			if err != nil {
				return a, fmt.Errorf("--out %q: %v", value, err)
			}
			a.Out = abs
			continue
		}
		n, err := charCap(value)
		if err != nil {
			return a, err
		}
		a.MaxChars = &n
	}
	switch {
	case len(rest) == 0:
		return a, errors.New("logs takes a window reference first")
	case len(rest) > 1:
		return a, fmt.Errorf("logs takes one window reference, got %q", strings.Join(rest, " "))
	case a.Out != "" && a.MaxChars != nil && *a.MaxChars != 0:
		return a, errors.New("pass --out or --max-chars, not both: --out writes every entry to the file")
	}
	a.Window = rest[0]
	return a, nil
}

// logsCmd implements `winuse logs`.
func logsCmd(args []string) int {
	a, err := parseLogsArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, protocol.LogsUsage)
		return 2
	}
	a.JSON = activeJSON
	if err := runWithArgs("logs", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
