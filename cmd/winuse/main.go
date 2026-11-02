// Command winuse is a Windows computer-use tool for coding agents: it lists
// Apps and Windows, reads a Window State (accessibility tree, screenshot on
// request) and performs Actions, through a background helper that starts on
// demand and exits when idle.
//
// The same binary runs the background helper via the hidden __helper
// subcommand (see internal/helper); the CLI is a thin client that talks to
// it over a named pipe restricted to the current user. The browser also
// starts it as the winuse extension's native messaging host (see
// internal/bridge).
package main

import (
	"fmt"
	"os"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cli"
	"github.com/hoangvu12/winuse/internal/helper"
	"github.com/hoangvu12/winuse/internal/win"
)

func main() {
	// The browser starts winuse as the winuse extension's native messaging
	// host, with the extension's origin as the first argument. Stdout then
	// belongs to the browser, so this comes before anything that could
	// print.
	if bridge.IsHostLaunch(os.Args[1:]) {
		os.Exit(browser.NativeHost(os.Args[1]))
	}

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
