package cli

// --observe, CLI side (speed observe, ADR 0021): an Action or a Batch that
// prints the Window State after it.
//
//	winuse <action> ... --observe[=diff|full] [--interactive] [--depth N] [--compact] [--max-chars N | --out <file>] [--full]
//	winuse batch <window> --observe[=diff|full] [...] <action> ... [then ...]
//
// A bare --observe is diff. The output budgets after it shape the
// observation as they shape get-window-state. On an Action they go
// anywhere outside its literal text, as --headed does; on a Batch before
// its first Action, as --stop-on-error does, so a get-window-state step
// keeps its own --interactive. The request carries them
// (protocol.Request.Observe); the helper reads the Window after the
// Action.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// observeFlag is --observe's spelling.
const observeFlag = "--observe"

// observeUsage is --observe's usage line.
const observeUsage = "usage: winuse <action> ... --observe[=diff|full] [--interactive] [--depth N] [--compact] [--max-chars N | --out <file>] [--full]\n" +
	"       winuse batch <window> --observe[=diff|full] [...] <action> ... [then ...]"

// activeObserve is the running command's --observe, which runRequest puts
// on its request; nil without the flag.
var activeObserve *protocol.Observe

// observeFlagLen is a flagLen for --observe and the output budgets that
// go with it: how many tokens the flag at args[0] spans, 0 for none.
func observeFlagLen(args []string) int {
	name, _, inline := strings.Cut(args[0], "=")
	switch name {
	case observeFlag, "--interactive", "--compact", "--full":
		return 1
	case "--depth", "--max-chars", "--out":
		if inline || len(args) < 2 {
			return 1
		}
		return 2
	}
	return 0
}

// extractObserve takes --observe and its output budgets out of a command
// line (the command first) and parses them; nil when there is none. A
// command that does not take --observe keeps every token for its own
// parser, except --observe itself, which is refused here by name.
func extractObserve(args []string) ([]string, *protocol.Observe, error) {
	var lead []string // before the command: winuse --observe set-value ...
	for len(args) > 0 {
		k := observeFlagLen(args)
		if k == 0 {
			break
		}
		lead, args = append(lead, args[:k]...), args[k:]
	}
	if len(args) == 0 {
		if len(lead) > 0 {
			return nil, nil, errors.New("--observe goes with an Action or batch")
		}
		return args, nil, nil
	}
	cmd := args[0]
	if len(lead) > 0 {
		if !protocol.Observable(cmd) {
			return nil, nil, fmt.Errorf("%s does not take --observe: it goes with an Action or batch", cmd)
		}
		args = append(append([]string{cmd}, lead...), args[1:]...)
	}
	var rest, taken []string
	switch {
	case cmd == "batch":
		rest, taken = takeBatchObserve(args[1:])
	case protocol.Observable(cmd):
		rest, taken = stripCommandFlags(cmd, args[1:], observeFlagLen)
	default:
		_, seen := stripCommandFlags(cmd, args[1:], func(a []string) int {
			if name, _, _ := strings.Cut(a[0], "="); name == observeFlag {
				return 1
			}
			return 0
		})
		if len(seen) > 0 {
			return nil, nil, fmt.Errorf("%s does not take --observe: it goes with an Action or batch", cmd)
		}
		return args, nil, nil
	}
	o, err := parseObserve(taken)
	if err != nil {
		return nil, nil, err
	}
	return append([]string{cmd}, rest...), o, nil
}

// takeBatchObserve takes --observe and its output budgets from a batch
// line's part before its first Action: among the Window, --timeout and
// --stop-on-error.
func takeBatchObserve(args []string) (rest, taken []string) {
	i := 0
	for plain := 0; i < len(args) && plain < 2; i++ {
		tok := args[i]
		if k := observeFlagLen(args[i:]); k > 0 {
			taken = append(taken, args[i:i+k]...)
			i += k - 1
			continue
		}
		switch {
		case tok == "--timeout" && i+1 < len(args):
			rest = append(rest, tok, args[i+1])
			i++
			continue
		case isTimeoutFlag(tok) || tok == stopOnErrorFlag:
			rest = append(rest, tok)
			continue
		}
		rest = append(rest, tok)
		plain++ // the Window, then the first Action's name
	}
	return append(rest, args[i:]...), taken
}

// parseObserve parses the tokens extractObserve took: nil when there are
// none, a usage error when the budgets come without --observe.
func parseObserve(taken []string) (*protocol.Observe, error) {
	if len(taken) == 0 {
		return nil, nil
	}
	o := &protocol.Observe{}
	var budgets []string
	for _, tok := range taken {
		name, value, inline := strings.Cut(tok, "=")
		if name != observeFlag {
			budgets = append(budgets, tok)
			continue
		}
		if o.Mode != "" {
			return nil, errors.New("--observe is given twice")
		}
		switch {
		case !inline:
			o.Mode = protocol.ObserveDiff
		case value == protocol.ObserveDiff || value == protocol.ObserveFull:
			o.Mode = value
		default:
			return nil, fmt.Errorf("--observe=%s is not a mode: pass --observe=diff or --observe=full", value)
		}
	}
	if o.Mode == "" {
		return nil, fmt.Errorf("%s shapes the observation and goes with --observe", strings.SplitN(budgets[0], "=", 2)[0])
	}
	rest, sh, err := takeShapeFlags(budgets) // find.go: the same budgets as get-window-state's
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("%s needs a value", rest[0])
	}
	o.Interactive, o.Depth, o.Compact, o.MaxChars, o.Out, o.Full = sh.Interactive, sh.Depth, sh.Compact, sh.MaxChars, sh.Out, sh.Full
	return o, nil
}
