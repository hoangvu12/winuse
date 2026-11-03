package helper

import (
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// TestRegistryHasEveryCLICommand guards the command registry the helper
// actually serves (helper.Main builds it with CommandsWith): every
// subcommand the CLI dispatches must be registered there.
func TestRegistryHasEveryCLICommand(t *testing.T) {
	cmds := CommandsWith(Services{Trees: &fakeTrees{}})
	for _, name := range append([]string{
		"list-windows", "list-apps", "launch-app", "get-window-state", "record-tree",
		"zoom", "click", "activate-window", "type-text", "press-key",
		"set-value", "perform-secondary-action", "batch", "scroll", "select-text", "invoke-menu", "drag", "hover",
		"restore-window", "minimize-window", "move-window", "resize-window", "close-window",
		"browser-status", "browser-use", "browser-launch", "browser-clone", "browser-connect", "browser-stop",
		"browser-ext-install", "browser-ext-status",
		"tab-list", "tab-new", "tab-close", "tab-switch",
		"clipboard-get", "clipboard-set", "clipboard-clear",
		"close-app",
		"list-displays", "screenshot",
	}, protocol.PageCommands...) {
		if _, ok := cmds[name]; !ok {
			t.Errorf("command %q is not registered in CommandsWith", name)
		}
	}
}
