package helper

// launch-app --cdp (headless-max ticket 15, ADR 0012 amendment): an
// Electron or WebView2 executable starts with a loopback debugging port,
// and its Windows then get a Browser Connection, so get-window-state reads
// their Pages over CDP and Page Actions on them are Headless, as in a
// browser winuse launched.
//
// Before the start, winuse picks a free ephemeral port and hands it to the
// App: an Electron App (browser.IsElectron: an .asar next to the
// executable) gets `--remote-debugging-port` in front of its own arguments,
// any other executable gets WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS in its
// own environment only. After its Window appears, winuse waits what is
// left of the launch budget for the port to answer, accepts it only when
// its listener belongs to the launched process tree, and registers it for
// the Window's process (browser.Manager.AwaitApp). A port that never
// appears leaves the App on UI Automation, said in a note: the App is
// running either way. An App that is already running is never restarted.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hoangvu12/winuse/internal/apps"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/win"
)

// AppConnector is the Browser Connections' side of launch-app --cdp:
// *browser.Manager. Services.Browser is type-asserted to it, so a helper
// whose browser service lacks it refuses --cdp before starting anything.
type AppConnector interface {
	AppPort() (int, error)
	AwaitApp(ctx context.Context, kind string, launched, windowPID uint32, port int, wait time.Duration) (browser.AppEndpoint, bool)
}

var _ AppConnector = (*browser.Manager)(nil)

// cdpLaunch is a --cdp launch prepared before the App starts.
type cdpLaunch struct {
	apps AppConnector
	kind string // browser.AppElectron or browser.AppWebView2
	port int
}

// prepareCDP refuses --cdp for anything but an executable (a shortcut or a
// packaged App starts with its own command line and environment) and on a
// helper with no Browser Connections, picks the port, and adds it to the
// launch options. A refusal starts nothing.
func (s Services) prepareCDP(app string, spec apps.Spec, opts *win.LaunchOptions) (*cdpLaunch, *Error) {
	if spec.Kind != apps.IDExe {
		e := exeOnlyRefusal(app, spec)
		return nil, Fail(fmt.Sprintf("--cdp needs an executable, and %s starts %s; nothing was started", app, startsWhat(spec)), e.Hint)
	}
	ac, ok := s.Browser.(AppConnector)
	if !ok {
		return nil, Fail("--cdp is unavailable: this winuse helper has no Browser Connections; nothing was started", "launch the App without --cdp to work through UI Automation")
	}
	port, err := ac.AppPort()
	if err != nil {
		return nil, Fail(fmt.Sprintf("--cdp could not pick a loopback port: %v; nothing was started", err), "launch the App without --cdp to work through UI Automation")
	}
	c := &cdpLaunch{apps: ac, kind: browser.AppWebView2, port: port}
	if browser.IsElectron(spec.ID, nil) {
		c.kind = browser.AppElectron
		opts.Args = browser.ElectronArgs(port, opts.Args)
	} else {
		opts.Env = browser.WebView2Env(port, opts.Env, os.Getenv(browser.WebView2ArgsVar))
	}
	return c, nil
}

// startsWhat names what a shortcut or packaged App starts, for --cdp's
// refusal.
func startsWhat(spec apps.Spec) string {
	if spec.Kind == apps.IDShortcut {
		return "a shortcut (" + spec.ID + ")"
	}
	return "a packaged App"
}

// note waits up to wait for the launched App's port and says how its
// Pages are reached: through the Browser Connection, or not at all.
func (c *cdpLaunch) note(ctx context.Context, launched uint32, w win.WindowInfo, wait time.Duration) string {
	ep, ok := c.apps.AwaitApp(ctx, c.kind, launched, w.PID, c.port, wait)
	if !ok {
		return "Note: no debugging port appeared, so the App stays on UI Automation\n"
	}
	return fmt.Sprintf("Note: a Browser Connection reaches the App's pages (%s, port %d): get-window-state %d reads them through it\n",
		browser.DisplayName(ep.Kind), ep.Port, w.ID)
}
