package cli

// The tab commands' CLI side (browser merge ticket 07): `winuse tab
// list|new|close|switch <window> ...` on a browser Window with a Browser
// Connection. Parsing is local (usage errors exit 2 and never dial the
// helper). The wire names are tab-list, tab-new, tab-close and tab-switch.
// tab switch picks its route from the Browser Connection: Headless through
// the winuse extension, and Headed otherwise, which needs the global
// --headed (headless-max ticket 21); the CLI forwards it. The others are
// Headless and ignore it.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// tabUsage is the tab command's usage block.
const tabUsage = `usage: winuse tab list <window>
       winuse tab new <window> [url]
       winuse tab close <window> <tab>
       winuse tab switch <window> <tab>`

// parseTabArgs parses `tab <sub> <window> ...` into its wire command and
// arguments.
func parseTabArgs(args []string) (string, protocol.TabArgs, error) {
	if len(args) == 0 {
		return "", protocol.TabArgs{}, errors.New("tab needs a subcommand: list, new, close or switch")
	}
	sub, rest := args[0], args[1:]
	for _, a := range rest {
		if strings.HasPrefix(a, "--") {
			return "", protocol.TabArgs{}, fmt.Errorf("tab %s has no option %s", sub, a)
		}
	}
	switch sub {
	case "list":
		if len(rest) != 1 {
			return "", protocol.TabArgs{}, errors.New("tab list takes a window reference")
		}
		return "tab-list", protocol.TabArgs{Window: rest[0]}, nil
	case "new":
		if len(rest) < 1 || len(rest) > 2 {
			return "", protocol.TabArgs{}, errors.New("tab new takes a window reference and at most one URL")
		}
		a := protocol.TabArgs{Window: rest[0]}
		if len(rest) == 2 {
			a.URL = rest[1]
		}
		return "tab-new", a, nil
	case "close", "switch":
		if len(rest) != 2 {
			return "", protocol.TabArgs{}, fmt.Errorf("tab %s takes a window reference and a tab id from winuse tab list", sub)
		}
		if err := checkTabRef(rest[1]); err != nil {
			return "", protocol.TabArgs{}, err
		}
		return "tab-" + sub, protocol.TabArgs{Window: rest[0], Tab: rest[1]}, nil
	}
	return "", protocol.TabArgs{}, fmt.Errorf("unknown tab subcommand %q; expected list, new, close or switch", sub)
}

// checkTabRef refuses a tab id shorter than the helper accepts (4
// characters of a target id).
func checkTabRef(ref string) error {
	if len(strings.TrimSpace(ref)) < 4 {
		return fmt.Errorf("tab id %q is too short: use at least 4 characters of an id from winuse tab list", ref)
	}
	return nil
}

// tabCmd implements `winuse tab list|new|close|switch <window> ...`.
// headed is the global --headed flag, which only tab switch uses.
func tabCmd(args []string, headed bool) int {
	cmd, a, err := tabRequest(args, headed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, tabUsage)
		return 2
	}
	if err := runWithArgs(cmd, a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// tabRequest is parseTabArgs with the global --headed, which only tab
// switch carries.
func tabRequest(args []string, headed bool) (string, protocol.TabArgs, error) {
	cmd, a, err := parseTabArgs(args)
	if err == nil && cmd == "tab-switch" {
		a.Headed = headed
	}
	return cmd, a, err
}

// parseBatchTabStep parses a tab step of a Batch: `tab new [url]`, `tab
// close <tab>` or `tab switch <tab>` (or the wire names tab-new,
// tab-close and tab-switch), without the Window, which the Batch names.
// tab list is no step: a Batch prints only ok.
func parseBatchTabStep(kind string, rest []string) (protocol.Step, error) {
	sub := strings.TrimPrefix(kind, "tab-")
	if kind == "tab" {
		if len(rest) == 0 {
			return protocol.Step{}, errors.New("a batch tab Action needs new, close or switch")
		}
		sub, rest = rest[0], rest[1:]
	}
	var a protocol.TabArgs
	switch sub {
	case "new":
		if len(rest) > 1 {
			return protocol.Step{}, errors.New("a batch tab new Action takes at most one URL")
		}
		if len(rest) == 1 {
			a.URL = rest[0]
		}
	case "close", "switch":
		if len(rest) != 1 {
			return protocol.Step{}, fmt.Errorf("a batch tab %s Action takes a tab id from winuse tab list", sub)
		}
		if err := checkTabRef(rest[0]); err != nil {
			return protocol.Step{}, err
		}
		a.Tab = rest[0]
	case "list":
		return protocol.Step{}, errors.New("tab list is not a batch Action: it prints the Tabs, and a batch prints only ok; run winuse tab list <window> on its own")
	default:
		return protocol.Step{}, fmt.Errorf("unknown batch tab Action %q; expected new, close or switch", sub)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return protocol.Step{}, err
	}
	return protocol.Step{Kind: "tab-" + sub, Args: raw}, nil
}
