package notes

// The `winuse note` command line, parsed here rather than in the CLI
// package so it is tested on its own. The CLI reads the text (literal, or
// --stdin / --file <path>) from Parsed.Text and sends Parsed.Args to the
// helper as Parsed.Wire.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// Usage is the note command's usage block.
const Usage = `usage: winuse note add <target> [--app] <text> | --stdin | --file <path>
       winuse note list [<target> [--app]]
       winuse note remove <target> [--app] <n>
       winuse note clear <target> [--app]`

const appFlag = "--app"

// Parsed is a parsed `winuse note` command line.
type Parsed struct {
	Wire string // note-add, note-list, note-remove or note-clear
	Args protocol.NoteArgs
	// Text is note add's text tokens as given: the words of a literal
	// text, or --stdin, or --file <path>.
	Text []string
}

// ParseArgs parses the arguments after `note`.
func ParseArgs(args []string) (Parsed, error) {
	if len(args) == 0 {
		return Parsed{}, errors.New("note needs a subcommand: add, list, remove or clear")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		start := TextStart(args)
		if start < 0 || start > len(args) {
			return Parsed{}, errors.New("note add takes a target and a text")
		}
		p := Parsed{Wire: "note-add"}
		for _, tok := range args[1:start] {
			if tok == appFlag {
				p.Args.App = true
				continue
			}
			p.Args.Target = tok
		}
		if p.Args.Target == "" {
			return Parsed{}, errors.New("note add takes a target and a text")
		}
		p.Text = args[start:]
		if len(p.Text) == 0 {
			return Parsed{}, errors.New("note add takes the Note's text, --stdin or --file <path>")
		}
		return p, nil
	case "list", "remove", "clear":
		var pos []string
		p := Parsed{Wire: "note-" + sub}
		for _, tok := range rest {
			switch {
			case tok == appFlag:
				p.Args.App = true
			case strings.HasPrefix(tok, "--"):
				return Parsed{}, fmt.Errorf("note %s has no option %s", sub, tok)
			default:
				pos = append(pos, tok)
			}
		}
		return finishParse(sub, p, pos)
	}
	return Parsed{}, fmt.Errorf("unknown note subcommand %q; expected add, list, remove or clear", sub)
}

func finishParse(sub string, p Parsed, pos []string) (Parsed, error) {
	switch sub {
	case "list":
		if len(pos) > 1 {
			return Parsed{}, errors.New("note list takes at most one target")
		}
		if len(pos) == 1 {
			p.Args.Target = pos[0]
		} else if p.Args.App {
			return Parsed{}, errors.New("note list: --app goes with a Window ID")
		}
	case "remove":
		if len(pos) != 2 {
			return Parsed{}, errors.New("note remove takes a target and a Note's number from winuse note list")
		}
		n, err := strconv.Atoi(pos[1])
		if err != nil || n < 1 {
			return Parsed{}, fmt.Errorf("note remove: %q is not a Note's number (1, 2, ... from winuse note list)", pos[1])
		}
		p.Args.Target, p.Args.N = pos[0], n
	case "clear":
		if len(pos) != 1 {
			return Parsed{}, errors.New("note clear takes one target")
		}
		p.Args.Target = pos[0]
	}
	return p, nil
}

// TextStart is the index in a note command line (the arguments after
// `note`) where note add's literal text starts: after "add", the target
// and an --app before or after it. -1 for any other subcommand. The CLI's
// global-flag hoisting leaves everything from there alone.
func TextStart(args []string) int {
	if len(args) == 0 || args[0] != "add" {
		return -1
	}
	n := 2
	for i := 1; i < len(args) && i <= 2; i++ {
		if args[i] == appFlag {
			n = 3
			break
		}
	}
	return n
}
