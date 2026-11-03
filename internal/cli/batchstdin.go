package cli

// A Batch from stdin (desktop-parity ticket 20, agent-desktop PR #439):
// `winuse batch -` reads the Batch from stdin instead of the command line,
// so long or quote-heavy text does not fight the shell. The first line is
// the Window and the Batch flags (--timeout, --stop-on-error); each
// further line is one step in the step syntax, without `then` (a `then`
// on a line is an ordinary word). Words split at spaces and tabs; double
// quotes group words, and inside them \" is a quote and \\ a backslash.
// Blank lines and lines whose first character is # are skipped. A usage
// error names its line and, like the command-line form, exits 2 without
// dialing the helper. The global --headed stays on the command line:
// `winuse --headed batch -`.

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// batchStdinArg is the Window argument that reads the Batch from stdin.
const batchStdinArg = "-"

// batchFromStdin reads and parses a stdin Batch. rest is what followed
// `-` on the command line, which must be nothing.
func batchFromStdin(rest []string, r io.Reader) (window string, steps []protocol.Step, timeout time.Duration, stop bool, err error) {
	if len(rest) != 0 {
		return "", nil, 0, false, fmt.Errorf("batch - takes nothing after the -; put the Window and the Batch flags on stdin's first line, got %q", strings.Join(rest, " "))
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", nil, 0, false, fmt.Errorf("reading the Batch from stdin: %v", err)
	}
	return parseBatchScript(string(raw))
}

// parseBatchScript parses a stdin Batch: the Window line, then one step
// per line. Every failure names its line.
func parseBatchScript(script string) (window string, steps []protocol.Step, timeout time.Duration, stop bool, err error) {
	script = strings.TrimPrefix(script, string(rune(0xFEFF))) // a BOM some shells write
	head := false
	for i, line := range strings.Split(script, "\n") {
		n := i + 1
		line = strings.TrimSuffix(line, "\r")
		if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		words, werr := splitBatchLine(line)
		if werr != nil {
			return "", nil, 0, false, fmt.Errorf("batch stdin line %d: %v", n, werr)
		}
		if !head {
			window, timeout, stop, err = parseBatchHead(words)
			if err != nil {
				return "", nil, 0, false, fmt.Errorf("batch stdin line %d: %v", n, err)
			}
			head = true
			continue
		}
		step, serr := parseBatchStep(words)
		if serr != nil {
			return "", nil, 0, false, fmt.Errorf("batch stdin line %d: %v", n, serr)
		}
		steps = append(steps, step)
	}
	if !head {
		return "", nil, 0, false, errors.New("batch - read no Window from stdin: the first line is the Window and the Batch flags")
	}
	if len(steps) == 0 {
		return "", nil, 0, false, errors.New("batch - read no Action from stdin: put one Action per line after the Window's line")
	}
	return window, steps, timeout, stop, nil
}

// parseBatchHead parses a stdin Batch's first line: the Window, and
// --timeout and --stop-on-error in any order.
func parseBatchHead(words []string) (window string, timeout time.Duration, stop bool, err error) {
	for i := 0; i < len(words); i++ {
		switch w := words[i]; {
		case w == stopOnErrorFlag:
			stop = true
		case isTimeoutFlag(w):
			d, used, terr := takeTimeoutAt("batch", words, i)
			if terr != nil {
				return "", 0, false, terr
			}
			timeout = d
			i += used - 1
		case w == headedFlag:
			return "", 0, false, errors.New("--headed goes on the command line: winuse --headed batch -")
		case window == "" && !strings.HasPrefix(w, "--"):
			window = w
		default:
			return "", 0, false, fmt.Errorf("the first line takes the Window and the Batch flags (--timeout, --stop-on-error); %q is neither: put each Action on a line of its own", w)
		}
	}
	if window == "" {
		return "", 0, false, errors.New("the first line needs the Window")
	}
	return window, timeout, stop, nil
}

// splitBatchLine splits one stdin line into words: spaces and tabs
// separate words, double quotes group them (an empty "" is a word), and
// inside quotes \" is a quote and \\ a backslash. Outside quotes a
// backslash is itself, so Windows paths need no escaping.
func splitBatchLine(line string) ([]string, error) {
	var words []string
	var b strings.Builder
	quoted, inWord := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quoted && c == '\\' && i+1 < len(line) && (line[i+1] == '"' || line[i+1] == '\\'):
			i++
			b.WriteByte(line[i])
		case c == '"':
			quoted, inWord = !quoted, true
		case !quoted && (c == ' ' || c == '\t'):
			if inWord {
				words = append(words, b.String())
				b.Reset()
				inWord = false
			}
		default:
			b.WriteByte(c)
			inWord = true
		}
	}
	if quoted {
		return nil, errors.New("a double quote is not closed")
	}
	if inWord {
		words = append(words, b.String())
	}
	return words, nil
}
