package cli

// `winuse note add|list|remove|clear` (ADR 0024, bladebro-ideas ticket
// 22), CLI side. The command line is parsed by notes.ParseArgs (usage
// errors exit 2 and never dial the helper); note add's text is literal
// from its first token, like type-text's, or read with --stdin or --file
// <path> (textsource.go). With the global --json the helper answers with
// JSON, which becomes the line's out.

import (
	"fmt"
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/notes"
)

// activeJSON is the global --json of the running command line (Main sets
// it): a command with a JSON answer of its own asks the helper for it.
var activeJSON bool

// noteCmd implements `winuse note`.
func noteCmd(args []string) int {
	p, err := notes.ParseArgs(args)
	if err == nil && p.Wire == "note-add" {
		p.Args.Text, err = noteText(p.Text)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, notes.Usage)
		return 2
	}
	p.Args.JSON = activeJSON
	if err := runWithArgs(p.Wire, p.Args); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// noteText is note add's text: the tokens joined by spaces, or read from
// --stdin or --file <path>.
func noteText(tokens []string) (string, error) {
	src, n, err := takeTextSource(tokens, fileFlag)
	if err != nil {
		return "", err
	}
	if !src.given() {
		return strings.Join(tokens, " "), nil
	}
	if extra := tokens[n:]; len(extra) > 0 {
		return "", errTextAndSource("note add", src, extra)
	}
	return src.read()
}

// stripFlagsNote is the global-flag stripping of a note command line:
// note add's text is literal (notes.TextStart), like type-text's.
func stripFlagsNote(args []string, flagLen func([]string) int) (rest, taken []string) {
	start := notes.TextStart(args)
	if start < 0 {
		return stripFlagsBefore(args, -1, flagLen)
	}
	return stripFlagsAroundText(args, start, flagLen, textSourceLen(fileFlag))
}
