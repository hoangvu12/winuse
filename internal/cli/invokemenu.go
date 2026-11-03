package cli

// invoke-menu's CLI side (headless-max ticket 08): `invoke-menu <window>
// "File > Save As..."` runs a classic menu bar command, Headless. It has
// no --headed form. The path is every argument after the Window, joined
// by spaces and split at each >, so `"File > Save As..."` and `File ">"
// "Save As..."` are the same path. A menu item whose own name holds a >
// cannot be named. Argument parsing is local (usage errors exit 2 and
// never dial the helper); the helper checks the path again for any other
// client.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// invokeMenuUsage is the invoke-menu command's usage line.
const invokeMenuUsage = `usage: winuse invoke-menu <window> "<menu> > <item>", such as "File > Save As..."`

// parseInvokeMenuArgs parses invoke-menu's arguments: a Window reference,
// then parseMenuPathTokens.
func parseInvokeMenuArgs(args []string) (protocol.InvokeMenuArgs, error) {
	if len(args) < 2 {
		return protocol.InvokeMenuArgs{}, errors.New(`invoke-menu takes a window reference and a menu path, such as "File > Save As..."`)
	}
	a, err := parseMenuPathTokens(args[1:])
	a.Window = args[0]
	return a, err
}

// parseMenuPathTokens parses a menu path: the tokens joined by spaces and
// split at each >, every name trimmed and none empty. The Batch step
// parser shares it.
func parseMenuPathTokens(tokens []string) (protocol.InvokeMenuArgs, error) {
	joined := strings.TrimSpace(strings.Join(tokens, " "))
	if joined == "" {
		return protocol.InvokeMenuArgs{}, errors.New(`invoke-menu takes a menu path, such as "File > Save As..."`)
	}
	var path []string
	for _, name := range strings.Split(joined, ">") {
		name = strings.TrimSpace(name)
		if name == "" {
			return protocol.InvokeMenuArgs{}, fmt.Errorf("the menu path %q has an empty item name; separate the names with >", joined)
		}
		path = append(path, name)
	}
	return protocol.InvokeMenuArgs{Path: path}, nil
}

// invokeMenuCmd implements `winuse invoke-menu <window> <path>`: run a
// classic menu bar command, Headless.
func invokeMenuCmd(args []string) int {
	a, err := parseInvokeMenuArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, invokeMenuUsage)
		return 2
	}
	if err := runWithArgs("invoke-menu", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
