package cli

// The global --headed flag (ADR 0010, headless spec "CLI"): Element
// Actions are Headless by default, and --headed forces v1's foreground,
// real-input pipeline for any Action or Batch. It is accepted before the
// command and anywhere after it, with one exception: inside a literal-text
// position — type-text's text and set-value's value, standalone or as a
// Batch step, and clipboard-set's text — the token is text like any
// other, so an agent typing the literal "--headed" never has it silently
// eaten. Put the flag before the command (or before the text) there.
// Commands that are always Headed, and the Desktop commands, accept the
// flag and ignore it.
//
// The same rule places the other global flags (--json, --trace,
// --trace-unredacted) and --help: hoistGlobalFlags moves them from after
// the command to before it, outside literal text, so each is read where
// it has always been read, among the leading flags.

import "strings"

// headedFlag is the global flag's spelling.
const headedFlag = "--headed"

// extractHeaded removes every --headed flag from a full command line
// (command name first, after any leading flags) and reports whether one
// was given. Tokens in literal-text positions are kept verbatim.
func extractHeaded(args []string) (rest []string, headed bool) {
	for len(args) > 0 && args[0] == headedFlag {
		headed = true
		args = args[1:]
	}
	if len(args) == 0 {
		return args, headed
	}
	cmdArgs, taken := stripCommandFlags(args[0], args[1:], headedFlagLen)
	return append([]string{args[0]}, cmdArgs...), headed || len(taken) > 0
}

// headedFlagLen is a flagLen for --headed alone.
func headedFlagLen(args []string) int {
	if args[0] == headedFlag {
		return 1
	}
	return 0
}

// hoistGlobalFlags moves the global flags a command line carries after its
// command (trailingFlagLen's) to just before the command, in order,
// leaving literal text alone: `winuse click 101 3 --json` runs as
// `winuse --json click 101 3`, and `winuse click --help` as
// `winuse --help click`.
func hoistGlobalFlags(args []string) []string {
	i := 0
	for i < len(args) {
		n := leadingFlag(args[i:])
		if n == 0 {
			break
		}
		i += n
	}
	if i >= len(args) {
		return args
	}
	rest, taken := stripCommandFlags(args[i], args[i+1:], trailingFlagLen)
	if len(taken) == 0 {
		return args
	}
	out := make([]string, 0, len(args))
	out = append(append(append(out, args[:i]...), taken...), args[i])
	return append(out, rest...)
}

// trailingFlagLen is how many tokens the global flag at args[0] takes when
// it comes after the command: leadingFlag's flags and --help/-h. A bare
// --trace at the end takes none, since moved before the command it would
// take the command as its file; the command's own parser refuses it.
func trailingFlagLen(args []string) int {
	switch args[0] {
	case helpFlag, helpShortFlag:
		return 1
	case traceFlag:
		if len(args) < 2 {
			return 0
		}
	}
	return leadingFlag(args)
}

// stripCommandFlags takes the flags flagLen recognises (it returns how
// many tokens the flag at args[0] spans, 0 for none) out of cmd's
// arguments, outside its literal-text positions, and returns the rest and
// the flags taken, both in order.
func stripCommandFlags(cmd string, args []string, flagLen func([]string) int) (rest, taken []string) {
	switch cmd {
	case "type-text":
		return stripFlagsBefore(args, typeTextTextAfter(args, flagLen), flagLen) // <window> [--into <element>] <text...>
	case "set-value":
		return stripFlagsBefore(args, 2, flagLen) // <window> <element> <value...>
	case "clipboard-set":
		return stripFlagsBefore(args, 0, flagLen) // <text...>, a Desktop command with no headed form
	case "batch":
		return stripFlagsBatch(args, flagLen)
	case "launch-app":
		return stripFlagsLaunch(args, flagLen)
	}
	return stripFlagsBefore(args, -1, flagLen)
}

// stripFlagsBefore takes flags out until n positionals have been seen;
// from there on every token is literal text, kept verbatim. n < 0 means
// the command has no literal-text position.
func stripFlagsBefore(args []string, n int, flagLen func([]string) int) (rest, taken []string) {
	rest = make([]string, 0, len(args))
	seen := 0
	for i := 0; i < len(args); i++ {
		if n >= 0 && seen >= n {
			rest = append(rest, args[i:]...)
			break
		}
		if k := flagLen(args[i:]); k > 0 {
			taken = append(taken, args[i:i+k]...)
			i += k - 1
			continue
		}
		rest = append(rest, args[i])
		seen++
	}
	return rest, taken
}

// stripFlagsLaunch takes flags out of a launch-app command line except
// where one is the value of --arg, --cwd or --env: the App gets that
// token verbatim.
func stripFlagsLaunch(args []string, flagLen func([]string) int) (rest, taken []string) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if launchValueFlag(tok) && i+1 < len(args) {
			rest = append(rest, tok, args[i+1])
			i++
			continue
		}
		if k := flagLen(args[i:]); k > 0 {
			taken = append(taken, args[i:i+k]...)
			i += k - 1
			continue
		}
		rest = append(rest, tok)
	}
	return rest, taken
}

// typeTextTextAfter is how many positionals standalone type-text takes
// before its literal text: the Window, and with --into right after it
// the flag and its Element too. Flags before the text do not count.
func typeTextTextAfter(args []string, flagLen func([]string) int) int {
	var pos []string
	for i := 0; i < len(args) && len(pos) < 2; i++ {
		if k := flagLen(args[i:]); k > 0 {
			i += k - 1
			continue
		}
		pos = append(pos, args[i])
	}
	if len(pos) == 2 && pos[1] == intoFlag {
		return 3
	}
	return 1
}

// stripFlagsBatch takes flags out of a batch command line (the Window,
// then steps separated by `then`), keeping the literal text of type-text
// and set-value steps verbatim. Any --headed outside it makes the whole
// Batch Headed.
func stripFlagsBatch(args []string, flagLen func([]string) int) (rest, taken []string) {
	rest = make([]string, 0, len(args))
	windowSeen := false
	kind := ""     // the current step's Action; "" between steps
	seen := 0      // positionals of the current step after its Action
	textAfter := 0 // batchTextAfter(kind), or 2 for type-text --into
	take := func(i int) int {
		k := flagLen(args[i:])
		taken = append(taken, args[i:i+k]...)
		return k
	}
	for i := 0; i < len(args); i++ {
		tok := args[i]
		switch {
		case !windowSeen:
			if flagLen(args[i:]) > 0 {
				i += take(i) - 1
				continue
			}
			windowSeen = true
		case strings.EqualFold(tok, "then"):
			kind, seen = "", 0
		case kind == "":
			if flagLen(args[i:]) > 0 {
				i += take(i) - 1
				continue
			}
			kind, textAfter = tok, batchTextAfter(tok)
		default:
			if kind == "type-text" && seen == 0 && tok == intoFlag {
				textAfter = 2 // --into <element>, then the text
			}
			if textAfter >= 0 && seen >= textAfter {
				seen++
				break // literal text: kept verbatim
			}
			if flagLen(args[i:]) > 0 {
				i += take(i) - 1
				continue
			}
			seen++
		}
		rest = append(rest, tok)
	}
	return rest, taken
}

// batchTextAfter is how many positionals a batch step takes before its
// literal text starts (-1: the step has none): type-text's text is all of
// its arguments (after --into and its Element, when the step starts with
// --into), set-value's value follows the Element Reference.
func batchTextAfter(kind string) int {
	switch kind {
	case "type-text":
		return 0
	case "set-value":
		return 1
	}
	return -1
}
