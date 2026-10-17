package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/apps"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// Commands is the helper's command registry: the wire-name-to-handler map
// the CLI's subcommands mirror, wired to the real services (live desktop
// enumeration, UI Automation). Tests inject fakes through CommandsWith
// (state.go). Later tickets add the action commands here (click,
// set-value, ...); their handlers reuse the UIA worker and the live-Element
// store the window-state commands create.
func Commands() map[string]Command {
	return CommandsWith(Services{})
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

// listApps answers with the App catalog: one line per App (name, running
// state, last used, use count, launch identifier) and one line per Window
// of running Apps (see apps.FormatApps). It reads window metadata, the
// registry and the filesystem; it sends no input and activates nothing.
func listApps(store *apps.StoreSource) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		if len(args) > 0 && string(args) != "null" && string(args) != "{}" {
			return "", Fail("list-apps takes no arguments", "")
		}
		all, err := win.Collect()
		if err != nil {
			return "", Fail("enumerating windows: "+err.Error(), "")
		}
		sources := apps.Sources{Running: apps.RunningFromWindows(win.VisibleWindows(all))}
		recs, err := apps.ReadUserAssist()
		if err != nil {
			return "", Fail("reading recently used apps: "+err.Error(), "")
		}
		sources.Recent = apps.RecentApps(recs, win.AppName, apps.LnkTargetFile)
		// Best-effort: PowerShell missing or blocked just loses packaged
		// Apps from the catalog.
		sources.Store, _ = store.Apps(ctx)
		return apps.FormatApps(apps.Catalog(sources)), nil
	}
}

// launchPollBudget stays just under the request budget so the specific
// "did not expose a targetable window" failure is reported rather than
// the generic request timeout.
const launchPollBudget = protocol.LaunchTimeout - 500*time.Millisecond

// launchApp starts an App and reports the Window it opened within the
// launch timeout (15 s): one list-windows-style line. Name resolution
// matches running, recently used, installed and packaged Apps; explicit
// identifiers (paths, AUMIDs) skip the lookup.
//
// The launch is an armed Esc scope (ticket 12): the user pressing Esc
// while winuse waits for the App's Window stops the wait with the same
// exit-130 stop as an Action — WaitForWindow's poll loop is the
// checkpoint, and the App that already started keeps running.
func launchApp(store *apps.StoreSource, esc EscWatch) Command {
	if esc == nil {
		esc = noopEscWatch{}
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.LaunchArgs
		if len(args) == 0 || json.Unmarshal(args, &a) != nil || strings.TrimSpace(a.App) == "" {
			return "", Fail("app is required", "pass an App name from winuse list-apps or an executable path")
		}
		esc.Arm()
		defer esc.Disarm()
		// The Window snapshot taken for resolution doubles as the
		// before-start snapshot for recognizing the opened Window.
		all, err := win.Collect()
		if err != nil {
			return "", Fail("enumerating windows: "+err.Error(), "")
		}
		before := win.VisibleWindows(all)

		sources := apps.Sources{Running: apps.RunningFromWindows(before)}
		if !apps.LooksLikeIdentifier(a.App) {
			// A name: resolve it against every source. Best-effort sources
			// (registry, Start menu, store) only widen what a name can mean.
			recs, _ := apps.ReadUserAssist()
			sources.Recent = apps.RecentApps(recs, win.AppName, apps.LnkTargetFile)
			sources.StartMenu, _ = apps.WalkStartMenu()
			sources.Store, _ = store.Apps(ctx)
		}
		spec, err := apps.Resolve(a.App, sources.Candidates())
		if err != nil {
			var resolveErr *apps.ResolveError
			if errors.As(err, &resolveErr) && resolveErr.Kind == apps.ErrAmbiguousApp {
				return "", Fail(err.Error(), "launch one of the matched Apps by its exact identifier")
			}
			return "", Fail(err.Error(), "run winuse list-apps for App names")
		}
		if spec.Kind == apps.IDShortcut && spec.TargetExe == "" {
			// A shortcut launched by path: resolve its target now so the
			// opened Window can be recognized.
			spec.TargetExe = apps.LnkTargetFile(spec.ID)
		}

		fgBefore := win.ForegroundWindow()
		// The checkpoint before the App starts: a stop during the name
		// resolution (the catalog sources can take a second or two) must
		// not start anything.
		if esc.Stopped() {
			return "", escStopError()
		}
		pid, err := apps.StartApp(spec)
		if err != nil {
			return "", Fail(fmt.Sprintf("failed to launch app %s: %v", a.App, err), "")
		}
		window, err := apps.WaitForWindow(ctx, spec, pid, before, fgBefore, launchPollBudget,
			func() ([]win.WindowInfo, error) {
				all, err := win.Collect()
				if err != nil {
					return nil, err
				}
				return win.VisibleWindows(all), nil
			},
			win.ForegroundWindow, time.Sleep, esc.Stopped)
		if err != nil {
			return "", launchError(err)
		}
		return win.FormatWindows([]win.WindowInfo{window}), nil
	}
}

// launchError maps launch-app's wait failures to the user-facing line:
// an Esc stop keeps its exit 130 and message; a Window that never
// appeared keeps Codex's message and its recovery hint.
func launchError(err error) *Error {
	if errors.Is(err, apps.ErrStopped) {
		return escStopError()
	}
	return Fail(err.Error(), "the App may be running without a visible Window; run winuse list-windows")
}
