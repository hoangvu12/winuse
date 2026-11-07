package cli

// The ticket-07 Action commands' CLI side: set-value and
// perform-secondary-action. Argument parsing is local (usage errors exit 2
// and never dial the helper); the value and the action name are passed
// through verbatim for the helper to act on (Codex's parameters). Ticket
// 11 adds the name:/id: Element Reference forms: the second positional is
// an Element Index or a reference, resolved helper-side against the
// Window's latest Window State.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// setValueUsage is the set-value command's usage line.
const setValueUsage = "usage: winuse set-value <window> <element index | name:<text> | id:<id> | @x,y> <value>"

// secondaryActionUsage is the perform-secondary-action command's usage
// line.
const secondaryActionUsage = "usage: winuse perform-secondary-action <window> <element index | name:<text> | id:<id> | @x,y> <action>"

// parseSetValueArgs parses set-value's arguments: a Window reference and
// an Element Reference (an Element Index, name:<text>, id:<AutomationId>
// or @x,y coordinates) positionally, then the value — every remaining
// argument joined with single spaces, so an unquoted value with spaces
// (hello world) still reaches the Element as one string. A value that
// ends with winuse flags is refused (setValueText).
func parseSetValueArgs(args []string) (window string, index *int, ref string, point *protocol.Point, value string, err error) {
	if len(args) < 3 {
		return "", nil, "", nil, "", errors.New("set-value takes a window reference, an element index, name:/id: reference or @x,y coordinates, and a value")
	}
	index, ref, point, err = parseElementTargetToken(args[1])
	if err != nil {
		return "", nil, "", nil, "", err
	}
	value, err = setValueText(args[2:], false)
	if err != nil {
		var vf *valueFlagsError
		if errors.As(err, &vf) {
			vf.fillStandalone(args[0], args[1])
		}
		return "", nil, "", nil, "", err
	}
	return args[0], index, ref, point, value, nil
}

// A value that ends with winuse flags (2026-11-07): everything after
// set-value's Element is its value (headed.go), so `winuse set-value 101 3
// hi --observe` would set the field to "hi --observe". set-value, and a
// batch set-value step, refuse a value whose last arguments are winuse
// flags it would take in a flag position, with nothing sent; the hint
// moves them where they are flags (the global ones after `winuse`,
// --observe's and a Batch's after the Window). Only whole trailing
// arguments count: a flag
// in the middle of the value, or inside one quoted argument ("hi
// --observe"), is text. A value that really ends so starts with --, which
// ends the flags: `winuse set-value 101 3 -- hi --observe`.

// setValueText joins set-value's value arguments with single spaces: a
// leading -- (with a value after it) is dropped and the rest kept
// verbatim; else a value ending with winuse flags is a *valueFlagsError.
// batch is a batch step's value, where the Batch's flags count too.
func setValueText(value []string, batch bool) (string, error) {
	if len(value) > 1 && value[0] == "--" {
		return strings.Join(value[1:], " "), nil
	}
	if k := trailingValueFlags(value, batch); k > 0 {
		return "", &valueFlagsError{batch: batch, value: value, k: k}
	}
	return strings.Join(value, " "), nil
}

// valueFlagLen is how many arguments the winuse flag at args[0] spans at
// the end of a set-value value, 0 for none; budget reports one of
// --observe's output budgets, which count only with an --observe.
func valueFlagLen(args []string, batch bool) (n int, budget bool) {
	tok := args[0]
	name, value, inline := strings.Cut(tok, "=")
	switch {
	case name == observeFlag:
		return 1, false
	case tok == headedFlag || tok == noWaitFlag || tok == jsonFlag || tok == traceUnredactedFlag:
		return 1, false
	case batch && tok == stopOnErrorFlag:
		return 1, false
	case tok == "--interactive" || tok == "--compact" || tok == "--full":
		return 1, true
	case name == "--depth" || name == "--max-chars" || name == "--out":
		if inline {
			return 1, true
		}
		if len(args) > 1 {
			return 2, true
		}
	case name == traceFlag:
		if inline {
			return 1, false
		}
		if len(args) > 1 {
			return 2, false
		}
	case name == "--timeout":
		n = 1
		if !inline && len(args) > 1 {
			value, n = args[1], 2
		}
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			return n, false
		}
	}
	return 0, false
}

// splitValueFlags splits run into its winuse flags, each with its value;
// ok is false when run is not all flags, or has a budget but no --observe.
func splitValueFlags(run []string, batch bool) (flags [][]string, ok bool) {
	observe, budget := false, false
	for i := 0; i < len(run); {
		n, b := valueFlagLen(run[i:], batch)
		if n == 0 {
			return nil, false
		}
		name, _, _ := strings.Cut(run[i], "=")
		observe, budget = observe || name == observeFlag, budget || b
		flags = append(flags, run[i:i+n])
		i += n
	}
	return flags, observe || !budget
}

// trailingValueFlags is how many of value's last arguments are winuse
// flags: the longest run that splitValueFlags takes whole.
func trailingValueFlags(value []string, batch bool) int {
	for s := range value {
		if _, ok := splitValueFlags(value[s:], batch); ok {
			return len(value) - s
		}
	}
	return 0
}

// valueFlagsError refuses a set-value value that ends with winuse flags.
type valueFlagsError struct {
	batch bool
	value []string   // the value's arguments as given
	k     int        // how many of them, at the end, are winuse flags
	line  *valueLine // nil: a Batch from stdin
}

// valueLine is the refused command line around the value, for the hint.
type valueLine struct {
	cmd, window string
	head        []string // a Batch's own flags, already taken off the line
	before      []string // the arguments after the Window, up to the value
	after       []string // a Batch's arguments after the refused step
}

func (e *valueFlagsError) Error() string {
	what := "set-value's value"
	if e.batch {
		what = "a batch set-value Action's value"
	}
	word := "the winuse flag"
	if len(e.flags()) > 1 {
		word = "the winuse flags"
	}
	return fmt.Sprintf("%s ends with %s %s, but everything after the Element is the value: it would be set to %q; nothing was sent",
		what, word, strings.Join(e.value[len(e.value)-e.k:], " "), strings.Join(e.value, " "))
}

// flags is the value's trailing flags, each with its value.
func (e *valueFlagsError) flags() [][]string {
	flags, _ := splitValueFlags(e.value[len(e.value)-e.k:], e.batch)
	return flags
}

// sortFlags sorts the trailing flags by where the corrected line takes
// them: global (--headed, --no-wait, --json, --trace...) right after
// `winuse`, observe (--observe and its budgets) and batch (a Batch's
// --timeout and --stop-on-error) after the Window. A standalone
// set-value takes no --timeout: dropped.
func (e *valueFlagsError) sortFlags() (global, observe, batch []string, dropped bool) {
	for _, f := range e.flags() {
		switch name, _, _ := strings.Cut(f[0], "="); name {
		case observeFlag, "--interactive", "--compact", "--full", "--depth", "--max-chars", "--out":
			observe = append(observe, f...)
		case "--timeout", stopOnErrorFlag:
			if !e.batch {
				dropped = true
				continue
			}
			batch = append(batch, f...)
		default:
			global = append(global, f...)
		}
	}
	return global, observe, batch, dropped
}

// kept is the value without its trailing flags, or a placeholder when
// nothing else was given.
func (e *valueFlagsError) kept() []string {
	if e.k == len(e.value) {
		return []string{"<value>"}
	}
	return e.value[:len(e.value)-e.k]
}

// fillStandalone sets the command line of a standalone set-value.
func (e *valueFlagsError) fillStandalone(window, element string) {
	e.line = &valueLine{cmd: "set-value", window: window, before: []string{element}}
}

// fillBatch sets the command line of a batch line: before is its
// arguments after the Window up to the end of the refused step, after the
// rest (from the next `then`).
func (e *valueFlagsError) fillBatch(window string, before, after []string) {
	e.line = &valueLine{cmd: "batch", window: window, before: before[:len(before)-len(e.value)], after: after}
}

// afterWindow puts a Batch's own flags, already taken off its line, back
// after its Window.
func (e *valueFlagsError) afterWindow(flags ...string) {
	if e.line != nil {
		e.line.head = append(e.line.head, flags...)
	}
}

// hint is the refusal's hint line: the command line with the flags moved
// out of the value, and the one that sets the text as written. pre and
// observe are the flags already taken off the line (takenFlags); a moved
// --observe replaces the taken one.
func (e *valueFlagsError) hint(pre, observe []string) string {
	if e.line == nil {
		return "put them on the command line (winuse batch - --observe) or the first line (--timeout, --stop-on-error), where they are flags; to set that text, put -- before the value"
	}
	global, movedObserve, batch, dropped := e.sortFlags()
	if len(movedObserve) == 0 {
		movedObserve = observe
	}
	l := e.line
	build := func(global, batch, observe, value []string) string {
		words := append(append([]string{}, pre...), global...)
		words = append(append(append(words, l.cmd, l.window), l.head...), batch...)
		words = append(append(append(words, observe...), l.before...), value...)
		return commandLine(append(words, l.after...))
	}
	n := len(e.flags())
	if dropped {
		n--
	}
	it := "it"
	if n > 1 {
		it = "them"
	}
	fix := fmt.Sprintf("move %s out of the value: ", it)
	switch {
	case n == 0:
		fix = "set-value takes no --timeout (an Action's budget is fixed): "
	case dropped:
		fix = fmt.Sprintf("move %s out of the value (set-value takes no --timeout): ", it)
	}
	return fix + build(global, batch, movedObserve, e.kept()) +
		"; to set that text, put -- before the value: " + build(nil, nil, observe, append([]string{"--"}, e.value...))
}

// takenFlags is the running command's flags already taken off its line,
// for a hint that rewrites the line: --headed and --no-wait (pre), and
// --observe with its budgets (observe).
func takenFlags(headed bool) (pre, observe []string) {
	if headed {
		pre = append(pre, headedFlag)
	}
	if activeNoWait {
		pre = append(pre, noWaitFlag)
	}
	o := activeObserve
	if o == nil {
		return pre, nil
	}
	observe = []string{observeFlag}
	if o.Mode != protocol.ObserveDiff {
		observe[0] += "=" + o.Mode
	}
	if o.Interactive {
		observe = append(observe, "--interactive")
	}
	if o.Depth > 0 {
		observe = append(observe, "--depth", strconv.Itoa(o.Depth))
	}
	if o.Compact {
		observe = append(observe, "--compact")
	}
	if o.MaxChars != nil {
		observe = append(observe, "--max-chars", strconv.Itoa(*o.MaxChars))
	}
	if o.Out != "" {
		observe = append(observe, "--out", o.Out)
	}
	if o.Full {
		observe = append(observe, "--full")
	}
	return pre, observe
}

// hasObserve reports whether args hold an --observe.
func hasObserve(args []string) bool {
	for _, a := range args {
		if name, _, _ := strings.Cut(a, "="); name == observeFlag {
			return true
		}
	}
	return false
}

// commandLine renders a winuse command line for a hint, quoting an
// argument with spaces or quotes in it.
func commandLine(words []string) string {
	out := make([]string, len(words))
	for i, w := range words {
		if w == "" || strings.ContainsAny(w, " \t\"") {
			w = strconv.Quote(w)
		}
		out[i] = w
	}
	return "winuse " + strings.Join(out, " ")
}

// usageRefusal prints a usage error, with the hint line of a value that
// ends with winuse flags, and returns exit 2. headed is the global
// --headed.
func usageRefusal(err error, usage string, headed bool) int {
	var vf *valueFlagsError
	if errors.As(err, &vf) {
		fmt.Fprintf(os.Stderr, "winuse: %v\nhint: %s\n%s\n", err, vf.hint(takenFlags(headed)), usage)
		return 2
	}
	fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, usage)
	return 2
}

// parseSecondaryActionArgs parses perform-secondary-action's arguments:
// a Window reference and an Element Reference (an Element Index,
// name:<text>, id:<AutomationId> or @x,y coordinates) positionally, then
// the action name — every remaining argument joined with single spaces,
// so an unquoted two-word action (scroll down) still names one action.
func parseSecondaryActionArgs(args []string) (window string, index *int, ref string, point *protocol.Point, action string, err error) {
	if len(args) < 3 {
		return "", nil, "", nil, "", errors.New("perform-secondary-action takes a window reference, an element index, name:/id: reference or @x,y coordinates, and an action")
	}
	index, ref, point, err = parseElementTargetToken(args[1])
	if err != nil {
		return "", nil, "", nil, "", err
	}
	return args[0], index, ref, point, strings.Join(args[2:], " "), nil
}

// setValueCmd implements `winuse set-value <window> <element index |
// name: | id:> <value>`: set an Element's value directly — Headless on an
// Element unless headed (the global --headed flag, ADR 0010).
func setValueCmd(args []string, headed bool) int {
	window, index, ref, point, value, err := parseSetValueArgs(args)
	if err != nil {
		return usageRefusal(err, setValueUsage, headed)
	}
	if err := runWithArgs("set-value", protocol.SetValueArgs{
		Window:       window,
		ElementIndex: index,
		Ref:          ref,
		Point:        point,
		Value:        value,
		Headed:       headed,
	}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// performSecondaryActionCmd implements `winuse perform-secondary-action
// <window> <element index | name: | id:> <action>`: perform one of the
// Element's Secondary Actions (Raise, Scroll Down, Expand, Collapse,
// Toggle, Select, ...) — Headless on an Element, except Raise, unless
// headed (the global --headed flag, ADR 0010).
func performSecondaryActionCmd(args []string, headed bool) int {
	window, index, ref, point, action, err := parseSecondaryActionArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, secondaryActionUsage)
		return 2
	}
	if err := runWithArgs("perform-secondary-action", protocol.SecondaryActionArgs{
		Window:       window,
		ElementIndex: index,
		Ref:          ref,
		Point:        point,
		Action:       action,
		Headed:       headed,
	}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
