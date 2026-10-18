package helper

import "testing"

// TestRegistryHasEveryCLICommand guards the command registry the helper
// actually serves (helper.Main builds it with CommandsWith): every
// subcommand the CLI dispatches must be registered there.
func TestRegistryHasEveryCLICommand(t *testing.T) {
	cmds := CommandsWith(Services{Trees: &fakeTrees{}})
	for _, name := range []string{
		"list-windows", "list-apps", "launch-app", "get-window-state", "record-tree",
		"zoom", "click", "activate-window", "type-text", "press-key",
		"set-value", "perform-secondary-action", "batch", "scroll", "drag", "hover",
	} {
		if _, ok := cmds[name]; !ok {
			t.Errorf("command %q is not registered in CommandsWith", name)
		}
	}
}
