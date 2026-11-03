package helper

// browser attach and browser detach (headless-max ticket 15): a running
// Electron App gets a Browser Connection without a restart, through its
// main process's Node.js inspector (browser/attach.go). It runs winuse's
// code inside the user's App, so it is opt-in per App: the agent asks the
// user first (the skill says so), and nothing attaches by default. An App
// whose nodeCliInspect fuse is off is refused with nothing sent.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// AppAttacher is the Browser Connections' side of the attach:
// *browser.Manager. Services.Browser is type-asserted to it.
type AppAttacher interface {
	AttachElectron(ctx context.Context, pid uint32, exePath string) (browser.AppEndpoint, error)
	DetachApp(pid uint32) error
}

var _ AppAttacher = (*browser.Manager)(nil)

// attachArgs parses browser attach's and detach's arguments.
func attachArgs(cmd string, args json.RawMessage) (protocol.AttachArgs, *Error) {
	var a protocol.AttachArgs
	if err := parseArgs(args, &a); err != nil || a.Window == "" {
		return a, Fail(cmd+": a window reference is required", "usage: winuse browser "+cmd[len("browser-"):]+" <window>")
	}
	return a, nil
}

// browserAttach runs `winuse browser attach <window>`.
func browserAttach(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		a, e := attachArgs("browser-attach", args)
		if e != nil {
			return "", e
		}
		w, e := resolveWindow(s, a.Window)
		if e != nil {
			return "", e
		}
		at, ok := s.Browser.(AppAttacher)
		if !ok {
			return "", Fail("this winuse helper has no Browser Connections; nothing was sent", "")
		}
		ep, err := at.AttachElectron(ctx, w.PID, w.ExePath)
		switch {
		case errors.Is(err, browser.ErrNotElectron):
			return "", Fail(fmt.Sprintf("window %d belongs to %s, which is not an Electron App; nothing was sent", w.ID, w.App),
				"browser attach works on Electron Apps only; a browser connects with winuse browser status, and other Apps stay on UI Automation")
		case errors.Is(err, browser.ErrNodeInspectOff):
			return "", Fail(fmt.Sprintf("%s turned its Node.js inspector off (the nodeCliInspect fuse), so winuse cannot attach; nothing was sent", w.App),
				"the App stays on UI Automation; if the user agrees to restart it, launch it with winuse launch-app <exe path> --cdp")
		case err != nil:
			return "", Fail(fmt.Sprintf("attaching to %s failed: %v", w.App, err), "the App stays on UI Automation; read winuse get-window-state "+fmt.Sprint(w.ID))
		case ep.Mode == browser.ModeApp:
			return fmt.Sprintf("ok\nNote: %s already has a Browser Connection: winuse launched it with --cdp\n", w.App), nil
		}
		return fmt.Sprintf("ok\nNote: attached to %s process %d: get-window-state %d reads its pages; winuse browser detach %d ends the attach and closes its inspector\n",
			w.App, w.PID, w.ID, w.ID), nil
	}
}

// browserDetach runs `winuse browser detach <window>`.
func browserDetach(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		a, e := attachArgs("browser-detach", args)
		if e != nil {
			return "", e
		}
		w, e := resolveWindow(s, a.Window)
		if e != nil {
			return "", e
		}
		at, ok := s.Browser.(AppAttacher)
		if !ok {
			return "", Fail("this winuse helper has no Browser Connections", "")
		}
		if err := at.DetachApp(w.PID); err != nil {
			return "", Fail(fmt.Sprintf("winuse is not attached to %s (window %d)", w.App, w.ID),
				"an App winuse launched with --cdp keeps its port until it exits; nothing to detach")
		}
		return "ok\nNote: detached; the App's inspector is closed\n", nil
	}
}
