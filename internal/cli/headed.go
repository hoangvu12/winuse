package cli

// The global --headed flag (ADR 0010, headless spec "CLI"): Element
// Actions are Headless by default, and --headed forces v1's foreground,
// real-input pipeline for any Action or Batch. It is accepted before the
// command and anywhere after it, with one exception: inside a literal-text
// position — type-text's text and set-value's value, standalone or as a
// Batch step — the token is text like any other, so an agent typing the
// literal "--headed" never has it silently eaten. Put the flag before the
// command (or before the text) there. Commands that are always Headed
// accept the flag and ignore it.

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
	cmd := args[0]
	var cmdArgs []string
	var h bool
	switch cmd {
	case "type-text":
		cmdArgs, h = stripHeadedBefore(args[1:], 1) // <window> <text...>
	case "set-value":
		cmdArgs, h = stripHeadedBefore(args[1:], 2) // <window> <element> <value...>
	case "batch":
		cmdArgs, h = stripHeadedBatch(args[1:])
	default:
		cmdArgs, h = stripHeadedBefore(args[1:], -1)
	}
	return append([]string{cmd}, cmdArgs...), headed || h
}

// stripHeadedBefore removes --headed tokens until n positionals have been
// seen; from there on every token is literal text, kept verbatim. n < 0
// means the command has no literal-text position.
func stripHeadedBefore(args []string, n int) (rest []string, headed bool) {
	rest = make([]string, 0, len(args))
	seen := 0
	for _, tok := range args {
		if n >= 0 && seen >= n {
			rest = append(rest, tok)
			continue
		}
		if tok == headedFlag {
			headed = true
			continue
		}
		rest = append(rest, tok)
		seen++
	}
	return rest, headed
}

// stripHeadedBatch removes --headed tokens from a batch command line (the
// Window, then steps separated by `then`), keeping the literal text of
// type-text and set-value steps verbatim. Any --headed elsewhere makes
// the whole Batch Headed.
func stripHeadedBatch(args []string) (rest []string, headed bool) {
	rest = make([]string, 0, len(args))
	windowSeen := false
	kind := "" // the current step's Action; "" between steps
	seen := 0  // positionals of the current step after its Action
	for _, tok := range args {
		switch {
		case !windowSeen:
			if tok == headedFlag {
				headed = true
				continue
			}
			windowSeen = true
		case strings.EqualFold(tok, "then"):
			kind, seen = "", 0
		case kind == "":
			if tok == headedFlag {
				headed = true
				continue
			}
			kind = tok
		default:
			if n := batchTextAfter(kind); n >= 0 && seen >= n {
				break // literal text: kept verbatim
			}
			if tok == headedFlag {
				headed = true
				continue
			}
			seen++
		}
		rest = append(rest, tok)
	}
	return rest, headed
}

// batchTextAfter is how many positionals a batch step takes before its
// literal text starts (-1: the step has none): type-text's text is all of
// its arguments, set-value's value follows the Element Reference.
func batchTextAfter(kind string) int {
	switch kind {
	case "type-text":
		return 0
	case "set-value":
		return 1
	}
	return -1
}
