package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
// "<Window ID>\t<App>\t<title>", in z-order, topmost first — Codex's
// list_windows. With details (desktop-parity 22) each line gains the
// details column before the title (win.FormatWindowsDetailed); with app
// only that App's Windows are listed, matched as a Window reference
// matches an App name, and an App with no Window fails. With shell
// (desktop-parity 09, shell.go) the shell surfaces join the listing in
// z-order. It only reads
// window metadata (never sends input, never activates).
func listWindows(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ListWindowsArgs
		if len(args) > 0 && string(args) != "null" {
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&a); err != nil {
				return "", Fail("list-windows: invalid arguments: "+err.Error(), "usage: winuse list-windows [--details] [--app <name>] [--shell]")
			}
		}
		ws, err := s.Windows()
		if a.Shell {
			ws, err = s.shellListing()
		}
		if err != nil {
			return "", Fail("enumerating windows: "+err.Error(), "")
		}
		s.sawWindows(ws) // identity.go: what each listed Window ID names
		if app := strings.TrimSpace(a.App); app != "" {
			ws = win.WindowsOfApp(ws, app)
			if len(ws) == 0 {
				return "", Fail(fmt.Sprintf("app %q has no open Windows", app),
					"an App is named by its App name from winuse list-windows or its executable name")
			}
		}
		if !a.Details {
			return win.FormatWindows(ws), nil
		}
		fg := s.Foreground()
		details := make([]win.WindowDetails, len(ws))
		for i, w := range ws {
			var adminErr *win.AdminWindowError
			details[i] = win.WindowDetails{
				Focused:   w.ID == fg,
				Maximized: !w.Minimized && s.Maximized(w.ID),
				Admin:     errors.As(s.Integrity(w), &adminErr),
			}
		}
		return win.FormatWindowsDetailed(ws, details), nil
	}
}

// getWindow answers `winuse get-window <window>`: the resolved Window as
// the one list-windows line — Codex's get_window, the re-resolve step for
// a binding that may have gone stale (a Window ID after a helper restart,
// an App name after Windows opened or closed). Exactly one Window must
// match, else the candidates error. It only reads window metadata: no
// input, no activation.
func getWindow(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.GetWindowArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("get-window: "+err.Error(), "usage: winuse get-window <window>")
		}
		w, rerr := resolveWindow(s, a.Window)
		if rerr != nil {
			return "", rerr
		}
		return win.FormatWindows([]win.WindowInfo{w}), nil
	}
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

// launchDeadlineMargin is kept between the end of a launch's Window poll
// and the request's deadline, so the specific "did not expose a targetable
// window" failure is reported rather than the generic request timeout: it
// covers the poll's last Window listing and the answer.
const launchDeadlineMargin = time.Second

// launchPollBudget is the poll of a launch with no deadline.
const launchPollBudget = protocol.LaunchTimeout - launchDeadlineMargin

// launchPoll is the poll budget of a launch under ctx: what is left of the
// request's budget (15 s, or a caller-set --timeout, desktop-parity ticket
// 14) once the name was resolved and the App started, less
// launchDeadlineMargin. It never reaches past the deadline: the final live
// run of 2026-11-03 resolved for long enough that a fixed 14.5 s poll ended
// after the 15 s budget, and the request timed out instead. Without a
// deadline it is launchPollBudget.
func launchPoll(ctx context.Context) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return launchPollBudget
	}
	return max(time.Until(dl)-launchDeadlineMargin, 0)
}

// launchApp starts an App and reports the Window it opened within the
// launch timeout (15 s, or the caller's --timeout up to 120 s): one
// list-windows-style line. Name resolution
// matches running, recently used, installed and packaged Apps; explicit
// identifiers (paths, AUMIDs) skip the lookup.
//
// The launch is an armed Esc scope (ticket 12): the user pressing Esc
// while winuse waits for the App's Window stops the wait with the same
// exit-130 stop as an Action — WaitForWindow's poll loop is the
// checkpoint, and the App that already started keeps running.
//
// The App starts with a non-activating show state (headless-max ticket 01,
// apps.StartApp), minimized with --minimized. An App can still take the
// foreground itself: a packaged App's activation always does, and a child
// of a helper that holds the foreground right (after a Headed Action's
// input) may. When the opened Window's App holds the foreground and did not
// before, a note line follows the Window line. winuse never takes the
// foreground back.
func launchApp(s Services, store *apps.StoreSource) Command {
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.LaunchArgs
		if len(args) == 0 || json.Unmarshal(args, &a) != nil || strings.TrimSpace(a.App) == "" {
			return "", Fail("app is required", "pass an App name from winuse list-apps or an executable path")
		}
		opts, oerr := launchOptions(a)
		if oerr != nil {
			return "", oerr
		}
		esc.Arm()
		defer esc.Disarm()
		// The Window snapshot taken for resolution doubles as the
		// before-start snapshot for recognizing the opened Window.
		before, err := s.Windows()
		if err != nil {
			return "", Fail("enumerating windows: "+err.Error(), "")
		}

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
		if spec.Kind != apps.IDExe && apps.ExeOnlyOptions(opts) {
			return "", exeOnlyRefusal(a.App, spec)
		}
		var cdp *cdpLaunch // launchcdp.go, headless-max 15
		if a.CDP {
			c, cerr := s.prepareCDP(a.App, spec, &opts)
			if cerr != nil {
				return "", cerr
			}
			cdp = c
		}

		fgBefore := s.Foreground()
		// The checkpoint before the App starts: a stop during the name
		// resolution (the catalog sources can take a second or two) must
		// not start anything.
		if esc.Stopped() {
			return "", escStopError()
		}
		budget := launchPoll(ctx)
		deadline := time.Now().Add(budget)
		pid, err := s.StartApp(spec, opts)
		if err != nil {
			return "", Fail(fmt.Sprintf("failed to launch app %s: %v", a.App, err), "")
		}
		window, err := apps.WaitForWindow(ctx, spec, pid, before, fgBefore, budget,
			s.Windows, s.Foreground, s.Sleep, esc.Stopped)
		if err != nil {
			return "", launchError(err)
		}
		out := win.FormatWindows([]win.WindowInfo{window})
		if s.selfActivated(window.ID, fgBefore) {
			out += launchForegroundNote
		}
		if cdp != nil {
			// What is left of the launch budget: the port answers a moment
			// after the Window (about 3 s for Electron, round 5).
			out += cdp.note(ctx, pid, window, time.Until(deadline))
		}
		return out, nil
	}
}

// launchForegroundNote is the line launch-app adds after the Window line
// when the App took the foreground.
const launchForegroundNote = "Note: the App took the foreground; winuse does not take it back.\n"

// launchOptions checks launch-app's options and turns them into the
// launcher's: every --env entry is KEY=VALUE, and --cwd is an existing
// folder named by an absolute path (the CLI resolves a relative one). A
// refusal starts nothing.
func launchOptions(a protocol.LaunchArgs) (win.LaunchOptions, *Error) {
	const usage = "usage: winuse launch-app <name|exe path> [--minimized] [--cdp] [--arg <a>]... [--cwd <dir>] [--env KEY=VALUE]... [--timeout <seconds>]"
	for _, e := range a.Env {
		if _, _, err := win.ParseEnvEntry(e); err != nil {
			return win.LaunchOptions{}, Fail(err.Error()+"; nothing was started", usage)
		}
	}
	if a.Cwd != "" {
		if !filepath.IsAbs(a.Cwd) {
			return win.LaunchOptions{}, Fail(fmt.Sprintf("--cwd %q is not an absolute path; nothing was started", a.Cwd), usage)
		}
		if fi, err := os.Stat(a.Cwd); err != nil || !fi.IsDir() {
			return win.LaunchOptions{}, Fail(fmt.Sprintf("--cwd %q is not a folder; nothing was started", a.Cwd), "pass an existing folder")
		}
	}
	return win.LaunchOptions{Args: a.Args, Dir: a.Cwd, Env: a.Env, Minimized: a.Minimized}, nil
}

// exeOnlyRefusal refuses --arg, --cwd and --env for an App that resolved to
// a shortcut or a packaged App: the shell starts those with their own
// command line, so the options would be dropped. A shortcut's target
// executable is named in the hint.
func exeOnlyRefusal(app string, spec apps.Spec) *Error {
	kind := "a packaged App"
	hint := "launch the App's executable by its path to pass --arg, --cwd or --env"
	if spec.Kind == apps.IDShortcut {
		kind = "a shortcut (" + spec.ID + ")"
		if spec.TargetExe != "" {
			hint = fmt.Sprintf("launch its executable instead: winuse launch-app %q with the same options", spec.TargetExe)
		}
	}
	return Fail(fmt.Sprintf("--arg, --cwd and --env need an executable, and %s starts %s; nothing was started", app, kind), hint)
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
