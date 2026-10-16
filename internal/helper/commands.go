package helper

import (
	"context"
	"encoding/json"

	"github.com/hoangvu12/winuse/internal/win"
)

// Commands is the helper's command registry: the wire-name-to-handler map
// the CLI's subcommands mirror. Later tickets add the observation and action
// commands here (get-window-state, click, ...); handlers that need UI
// Automation will run their COM work on the helper's single locked thread.
func Commands() map[string]Command {
	return map[string]Command{
		"list-windows": listWindows,
	}
}

// listWindows answers with one line per visible Window:
// "<Window ID>\t<App>\t<title>", in z-order, topmost first. It only reads
// window metadata (never sends input, never activates).
func listWindows(ctx context.Context, args json.RawMessage) (string, *Error) {
	if len(args) > 0 && string(args) != "null" && string(args) != "{}" {
		return "", Fail("list-windows takes no arguments", "")
	}
	all, err := win.Collect()
	if err != nil {
		return "", Fail("enumerating windows: "+err.Error(), "")
	}
	return win.FormatWindows(win.VisibleWindows(all)), nil
}
