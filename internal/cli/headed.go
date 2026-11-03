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
		cmdArgs, h = stripHeadedBefore(args[1:], typeTextTextAfter(args[1:])) // <window> [--into <element>] <text...>
	case "set-value":
		cmdArgs, h = stripHeadedBefore(args[1:], 2) // <window> <element> <value...>
	case "clipboard-set":
		cmdArgs, h = stripHeadedBefore(args[1:], 0) // <text...>, a Desktop command with no headed form
	case "batch":
		cmdArgs, h = stripHeadedBatch(args[1:])
	case "launch-app":
		cmdArgs, h = stripHeadedLaunch(args[1:])
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

// stripHeadedLaunch removes --headed tokens from a launch-app command line
// except where one is the value of --arg, --cwd or --env: the App gets
// that token verbatim.
func stripHeadedLaunch(args []string) (rest []string, headed bool) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if launchValueFlag(tok) && i+1 < len(args) {
			rest = append(rest, tok, args[i+1])
			i++
			continue
		}
		if tok == headedFlag {
			headed = true
			continue
		}
		rest = append(rest, tok)
	}
	return rest, headed
}

// typeTextTextAfter is how many positionals standalone type-text takes
// before its literal text: the Window, and with --into right after it
// the flag and its Element too. --headed tokens before the text do not
// count.
func typeTextTextAfter(args []string) int {
	var pos []string
	for _, tok := range args {
		if tok != headedFlag {
			pos = append(pos, tok)
		}
		if len(pos) == 2 {
			break
		}
	}
	if len(pos) == 2 && pos[1] == intoFlag {
		return 3
	}
	return 1
}

// stripHeadedBatch removes --headed tokens from a batch command line (the
// Window, then steps separated by `then`), keeping the literal text of
// type-text and set-value steps verbatim. Any --headed elsewhere makes
// the whole Batch Headed.
func stripHeadedBatch(args []string) (rest []string, headed bool) {
	rest = make([]string, 0, len(args))
	windowSeen := false
	kind := ""     // the current step's Action; "" between steps
	seen := 0      // positionals of the current step after its Action
	textAfter := 0 // batchTextAfter(kind), or 2 for type-text --into
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
			kind, textAfter = tok, batchTextAfter(tok)
		default:
			if kind == "type-text" && seen == 0 && tok == intoFlag {
				textAfter = 2 // --into <element>, then the text
			}
			if textAfter >= 0 && seen >= textAfter {
				seen++
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
