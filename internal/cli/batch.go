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
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// batchUsage is the batch command's usage line.
const batchUsage = "usage: winuse batch <window> [--timeout <seconds>] [--stop-on-error] [in <window>|new] <action> [arguments] [then [in <window>|new] <action> [arguments]] ...\n" +
	"       winuse batch -   (stdin: the Window and the Batch flags, then one Action per line)"

// stopOnErrorFlag stops a Batch at its first failed Action
// (desktop-parity ticket 20); without it the Batch runs on.
const stopOnErrorFlag = "--stop-on-error"

// takeBatchStopOnError removes batch's --stop-on-error, which goes before
// the first Action (before or after the Window, among --timeout): a later
// one is a step's argument, such as literal text.
func takeBatchStopOnError(args []string) ([]string, bool) {
	var rest []string
	stop := false
	i := 0
	for plain := 0; i < len(args) && plain < 2; i++ {
		switch tok := args[i]; {
		case tok == stopOnErrorFlag:
			stop = true
			continue
		case tok == "--timeout" && i+1 < len(args):
			rest = append(rest, tok, args[i+1]) // takeBatchTimeout's
			i++
			continue
		case isTimeoutFlag(tok):
			rest = append(rest, tok)
			continue
		}
		rest = append(rest, args[i])
		plain++ // the Window, then the first Action's name
	}
	return append(rest, args[i:]...), stop
}

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
// wire Step (the Window is the Batch's; the helper injects it). A step
// that starts `in <window>` acts in that Window instead, and `in new` in
// the Window the step before it opened (desktop-parity ticket 24).
func parseBatchStep(tokens []string) (protocol.Step, error) {
	if strings.EqualFold(tokens[0], "in") {
		if len(tokens) < 3 {
			return protocol.Step{}, errors.New("a batch step's `in` takes a Window and an Action: in <window>|new <action> [arguments]")
		}
		step, err := parseBatchStep(tokens[2:])
		if err != nil {
			return protocol.Step{}, err
		}
		if step.Window != "" || step.InNew {
			return protocol.Step{}, errors.New("a batch step takes one `in <window>`")
		}
		if strings.EqualFold(tokens[1], "new") {
			step.InNew = true
		} else {
			step.Window = tokens[1]
		}
		return step, nil
	}
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
			Post:         o.post,
			Modifiers:    o.mods,
		}
	case "type-text":
		// --post first in the step posts the text (Posted Keys, ticket 07),
		// and --into <element> there types into that Element
		// (desktop-parity ticket 23); anywhere later either is text, as in
		// the standalone form.
		ta, text, err := typeTextFlags(rest)
		if err != nil {
			return protocol.Step{}, fmt.Errorf("a batch type-text Action: %v", err)
		}
		if len(text) == 0 {
			return protocol.Step{}, errors.New("a batch type-text Action needs text")
		}
		ta.Text = strings.Join(text, " ")
		args = ta
	case "press-key":
		// --post before or after the key posts it (Posted Keys, ticket 07);
		// --force sends a Blocked Combo, for this step only.
		stripped, post := stripFlag(tokens, postFlag)
		stripped, force := stripFlag(stripped, forceFlag)
		rest = stripped[1:]
		if len(rest) == 0 {
			return protocol.Step{}, errors.New("a batch press-key Action needs a key")
		}
		args = protocol.PressKeyArgs{Key: strings.Join(rest, " "), Post: post, Force: force}
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
	case "select-text":
		sa, err := parseSelectTextTokens(rest)
		if err != nil {
			return protocol.Step{}, fmt.Errorf("a batch select-text Action: %v", err)
		}
		args = sa
	case "invoke-menu":
		ma, err := parseMenuPathTokens(rest)
		if err != nil {
			return protocol.Step{}, fmt.Errorf("a batch invoke-menu Action: %v", err)
		}
		args = ma
	case "activate-window":
		if len(rest) != 0 {
			return protocol.Step{}, errors.New("a batch activate-window Action takes no arguments")
		}
		return protocol.Step{Kind: kind}, nil // no arguments at all
	case "scroll":
		// The pointer Actions as steps (ticket 10's Actions, batchable
		// from ticket 12): the same arguments the standalone commands take,
		// minus the Window. scroll's Element forms (an Element with a
		// direction and optional steps, or into-view) are Headless steps.
		// --post anywhere posts the wheel at the point (headless-max
		// ticket 12); it has no Element form.
		stripped, post := stripFlag(tokens, postFlag)
		stripped, mods, merr := takeModifiers(stripped)
		if merr != nil {
			return protocol.Step{}, merr
		}
		rest = stripped[1:]
		if len(rest) == 0 || !isPointToken(rest[0]) {
			if post || mods != nil {
				return protocol.Step{}, errors.New("a batch scroll Action with --post or --modifiers takes an @x,y point and two scroll amounts")
			}
			if len(rest) < 2 {
				return protocol.Step{}, errors.New("a batch scroll Action takes an element and a direction (up, down, left, right or into-view), or an @x,y point and two scroll amounts")
			}
			sa, err := parseElementScrollTokens(rest)
			if err != nil {
				return protocol.Step{}, err
			}
			args = sa
			break
		}
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
		args = protocol.ScrollArgs{X: x, Y: y, ScrollX: dx, ScrollY: dy, Post: post, Modifiers: mods}
	case "drag":
		// --drop-delay <ms> anywhere in the step (desktop-parity 13).
		dragRest, delay, err := stripDropDelay(rest)
		if err != nil {
			return protocol.Step{}, fmt.Errorf("a batch drag Action: %v", err)
		}
		if len(dragRest) > 0 && !isPointToken(dragRest[0]) {
			// From an Element (page.go, browser merge ticket 06).
			da, err := parseDragTokens(dragRest)
			if err != nil {
				return protocol.Step{}, fmt.Errorf("a batch drag Action: %v", err)
			}
			da.DropDelayMS = delay
			args = da
			break
		}
		if len(dragRest) != 2 {
			return protocol.Step{}, errors.New("a batch drag Action takes two @x,y points (from, to)")
		}
		fromX, fromY, err := parsePointRef(dragRest[0])
		if err != nil {
			return protocol.Step{}, err
		}
		toX, toY, err := parsePointRef(dragRest[1])
		if err != nil {
			return protocol.Step{}, err
		}
		args = protocol.DragArgs{FromX: fromX, FromY: fromY, ToX: toX, ToY: toY, DropDelayMS: delay}
	case "hover":
		if len(rest) > 0 && !isPointToken(rest[0]) {
			// A Page Element (page.go, browser merge ticket 06).
			ha, err := parseHoverTokens(rest)
			if err != nil {
				return protocol.Step{}, fmt.Errorf("a batch hover Action: %v", err)
			}
			args = ha
			break
		}
		if len(rest) != 1 {
			return protocol.Step{}, errors.New("a batch hover Action takes an @x,y point")
		}
		x, y, err := parsePointRef(rest[0])
		if err != nil {
			return protocol.Step{}, err
		}
		args = protocol.HoverArgs{X: x, Y: y}
	case "tab", "tab-list", "tab-new", "tab-close", "tab-switch":
		return parseBatchTabStep(kind, rest) // tab.go: tab list|new|close|switch
	case "navigate", "back", "forward", "reload", "upload", "dialog", "wait", "eval":
		return parseBatchPageStep(kind, rest) // page.go: the page-only Actions
	default:
		return protocol.Step{}, fmt.Errorf("unknown batch action %q; expected one of %s", kind, batchKindsWord())
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return protocol.Step{}, err
	}
	return protocol.Step{Kind: kind, Args: raw}, nil
}

// batchPosts reports whether any step is Posted Keys (`--post`), which a
// headed Batch refuses as a usage error.
func batchPosts(steps []protocol.Step) bool {
	for _, st := range steps {
		var flags struct {
			Post bool `json:"post"`
		}
		if json.Unmarshal(st.Args, &flags) == nil && flags.Post {
			return true
		}
	}
	return false
}

// batchCmd implements `winuse batch <window> <action> [arguments]
// [then <action> [arguments]]...`: run several Actions on one Window in
// order and print each one's output under its number. A failed Action
// does not stop the Batch unless --stop-on-error; a Batch with a failed
// Action prints its steps' lines, then fails naming every failed Action
// (desktop-parity ticket 20). headed (the global --headed flag, anywhere
// on a batch line outside literal text) runs every step the v1 way (ADR
// 0010). --timeout before the first Action widens the 60 s budget (up to
// 300 s). `winuse batch -` reads the Batch from stdin (batchstdin.go).
func batchCmd(args []string, headed bool) int {
	var window string
	var steps []protocol.Step
	var timeout time.Duration
	var stopOnError bool
	var err error
	if len(args) > 0 && args[0] == batchStdinArg {
		window, steps, timeout, stopOnError, err = batchFromStdin(args[1:], os.Stdin)
	} else {
		args, stopOnError = takeBatchStopOnError(args)
		args, timeout, err = takeBatchTimeout(args)
		if err == nil {
			window, steps, err = parseBatchArgs(args)
		}
	}
	if err == nil && headed && batchPosts(steps) {
		err = errPostHeaded
	}
	if err == nil && steps[0].InNew {
		err = errors.New("the first batch Action cannot be `in new`: no Action before it opened a Window")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, batchUsage)
		return 2
	}
	if err := runWithTimeout("batch", protocol.BatchArgs{Window: window, Steps: steps, Headed: headed, StopOnError: stopOnError}, timeout); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
