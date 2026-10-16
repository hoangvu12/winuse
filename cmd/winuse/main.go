// Command winuse is a Windows computer-use tool for coding agents: it lists
// Apps and Windows, reads a Window State (accessibility tree, screenshot on
// request) and performs Actions, through a background helper that starts on
// demand and exits when idle.
//
// The same binary runs the background helper via the hidden __helper
// subcommand (see internal/helper); the CLI is a thin client that talks to
// it over a named pipe restricted to the current user.
package main

import (
	"fmt"
	"os"

	"github.com/hoangvu12/winuse/internal/cli"
	"github.com/hoangvu12/winuse/internal/helper"
	"github.com/hoangvu12/winuse/internal/win"
)

func main() {
	// Per-monitor DPI awareness (v2) before any window call so every
	// coordinate is physical pixels. The manifest embedded in the binary
	// covers normal builds; this call covers `go run` and friends. Failing
	// because it is already set (the manifest) is success.
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		fmt.Fprintf(os.Stderr, "winuse: per-monitor DPI awareness: %v\n", err)
	}

	if len(os.Args) > 1 && os.Args[1] == win.HelperCommand {
		os.Exit(helper.Main())
	}
	os.Exit(cli.Main(os.Args[1:]))
}
