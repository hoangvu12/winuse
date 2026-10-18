package cli

// The batch command's CLI side (ticket 11): several Actions on one Window
// in a single call. Steps are separated by `then` tokens, and each step
// is an Action command without its Window argument, parsed exactly like
// the standalone form — so `winuse batch 101 click name:OK then type-text
// hello then press-key Return` sends three steps. The pointer Actions
// (scroll, drag, hover — ticket 10) are steps too (ticket 12). Step syntax
// is parsed entirely here (usage errors exit 2 and never dial the helper);
// value validation (key names, element availability) stays in the helper so
// the CLI and any other client agree on the messages.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// batchUsage is the batch command's usage line.
const batchUsage = "usage: winuse batch <window> <action> [arguments] [then <action> [arguments]] ..."

// batchKindsWord renders the batchable Actions for usage errors.
func batchKindsWord() string {
	return strings.Join(protocol.BatchStepKinds, ", ")
}

// parseBatchArgs parses batch's arguments: a Window reference, then one
// or more Action steps separated by `then` tokens (case-insensitive). A
// step is an Action's command name followed by that command's arguments
// (without the Window). Variadic step arguments (type-text's text,
// set-value's value) join the rest of their step with single spaces, the
// way the standalone commands join theirs — so a `then` inside unquoted
// text still separates steps: quote the text to keep it.
func parseBatchArgs(args []string) (window string, steps []protocol.Step, err error) {
	if len(args) < 2 {
		return "", nil, errors.New("batch takes a window reference and at least one Action")
	}
	window, rest := args[0], args[1:]
	var tokens []string
	flush := func() error {
		if len(tokens) == 0 {
			return errors.New("a batch Action is empty; write `then` only between two Actions")
		}
		step, serr := parseBatchStep(tokens)
		if serr != nil {
			return serr
		}
		steps = append(steps, step)
		tokens = nil
		return nil
	}
	for _, tok := range rest {
		if strings.EqualFold(tok, "then") {
			if ferr := flush(); ferr != nil {
				return "", nil, ferr
			}
			continue
		}
		tokens = append(tokens, tok)
	}
	if ferr := flush(); ferr != nil {
		return "", nil, ferr
	}
	return window, steps, nil
}

// parseBatchStep parses one batch step — the Action's name and its
// arguments, the same arguments the standalone command takes — into the
// wire Step (the Window is the Batch's; the helper injects it).
func parseBatchStep(tokens []string) (protocol.Step, error) {
	kind, rest := tokens[0], tokens[1:]
	var args any
	switch kind {
	case "click":
		// One positional: the target (an Element Index, name:, id: or
		// @x,y), with --count and --button allowed anywhere.
		pos, o, err := parseClickFlags(rest)
		if err != nil {
			return protocol.Step{}, err
		}
		if len(pos) != 1 {
			if len(pos) > 1 && isRefToken(pos[0]) {
				return protocol.Step{}, errors.New(unquotedRefHint(pos[0] + " " + pos[1]))
			}
			return protocol.Step{}, errors.New("a batch click Action takes one element index, name:/id: reference or @x,y")
		}
		target, terr := parseClickTargetToken(pos[0])
		if terr != nil {
			return protocol.Step{}, terr
		}
		args = protocol.ClickArgs{
			ElementIndex: target.index,
			Ref:          target.ref,
			Point:        target.point,
			ClickCount:   o.count,
			MouseButton:  o.button,
		}
	case "type-text":
		if len(rest) == 0 {
			return protocol.Step{}, errors.New("a batch type-text Action needs text")
		}
		args = protocol.TypeTextArgs{Text: strings.Join(rest, " ")}
	case "press-key":
		if len(rest) == 0 {
			return protocol.Step{}, errors.New("a batch press-key Action needs a key")
		}
		args = protocol.PressKeyArgs{Key: strings.Join(rest, " ")}
	case "set-value":
		if len(rest) < 2 {
			return protocol.Step{}, errors.New("a batch set-value Action takes an element index, name:/id: reference or @x,y, and a value")
		}
		index, ref, err := parseElementRefToken(rest[0])
		if err != nil {
			return protocol.Step{}, err
		}
		args = protocol.SetValueArgs{ElementIndex: index, Ref: ref, Value: strings.Join(rest[1:], " ")}
	case "perform-secondary-action":
		if len(rest) < 2 {
			return protocol.Step{}, errors.New("a batch perform-secondary-action Action takes an element index, name:/id: reference or @x,y, and an action")
		}
		index, ref, err := parseElementRefToken(rest[0])
		if err != nil {
			return protocol.Step{}, err
		}
		args = protocol.SecondaryActionArgs{ElementIndex: index, Ref: ref, Action: strings.Join(rest[1:], " ")}
	case "activate-window":
		if len(rest) != 0 {
			return protocol.Step{}, errors.New("a batch activate-window Action takes no arguments")
		}
		return protocol.Step{Kind: kind}, nil // no arguments at all
	case "scroll":
		// The pointer Actions as steps (ticket 10's Actions, batchable
		// from ticket 12): the same arguments the standalone commands take,
		// minus the Window.
		if len(rest) != 3 {
			return protocol.Step{}, errors.New("a batch scroll Action takes an @x,y point and two scroll amounts")
		}
		x, y, err := parsePointRef(rest[0])
		if err != nil {
			return protocol.Step{}, err
		}
		dx, err := parseScrollAmount(rest[1])
		if err != nil {
			return protocol.Step{}, err
		}
		dy, err := parseScrollAmount(rest[2])
		if err != nil {
			return protocol.Step{}, err
		}
		args = protocol.ScrollArgs{X: x, Y: y, ScrollX: dx, ScrollY: dy}
	case "drag":
		if len(rest) != 2 {
			return protocol.Step{}, errors.New("a batch drag Action takes two @x,y points (from, to)")
		}
		fromX, fromY, err := parsePointRef(rest[0])
		if err != nil {
			return protocol.Step{}, err
		}
		toX, toY, err := parsePointRef(rest[1])
		if err != nil {
			return protocol.Step{}, err
		}
		args = protocol.DragArgs{FromX: fromX, FromY: fromY, ToX: toX, ToY: toY}
	case "hover":
		if len(rest) != 1 {
			return protocol.Step{}, errors.New("a batch hover Action takes an @x,y point")
		}
		x, y, err := parsePointRef(rest[0])
		if err != nil {
			return protocol.Step{}, err
		}
		args = protocol.HoverArgs{X: x, Y: y}
	default:
		return protocol.Step{}, fmt.Errorf("unknown batch action %q; expected one of %s", kind, batchKindsWord())
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return protocol.Step{}, err
	}
	return protocol.Step{Kind: kind, Args: raw}, nil
}

// batchCmd implements `winuse batch <window> <action> [arguments]
// [then <action> [arguments]]...`: run several Actions on one Window in
// order, stopping at the first failure (which names the failed Action). A
// successful Batch prints `ok`, Codex-style.
func batchCmd(args []string) int {
	window, steps, err := parseBatchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, batchUsage)
		return 2
	}
	if err := runWithArgs("batch", protocol.BatchArgs{Window: window, Steps: steps}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
