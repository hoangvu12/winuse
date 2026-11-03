package helper

// open-shell (desktop-parity tickets 09 and 18, like agent-desktop's
// open-system-surface): `winuse --headed open-shell start | notifications
// | quick-settings | overflow` opens a shell surface and prints its
// list-windows line, so the agent reads it next with get-window-state.
//
// It is Headed only, a Desktop command: none of these opens from UI
// Automation without the foreground (probes round 5, section 6), so
// without --headed it is refused with nothing sent (ADR 0013 rule 3,
// headless-max 21). With --headed, under the in-control indicator over
// the taskbar and an armed Esc scope:
//
//   - start presses Control_L+Escape, as 09's press-key route does;
//   - notifications presses Win+N and quick-settings Win+A, the shell
//     accelerators the user allowed for this command (ADR 0015).
//     press-key keeps refusing the Windows key; no key name reaches
//     keys.ShellAccelerator;
//   - overflow clicks the taskbar's hidden-icons chevron with the real
//     pointer, the route 09 measured live: the chevron is the
//     notification-area button (AutomationId SystemTrayIcon) whose name
//     starts with "Show Hidden Icons".
//
// The surface showing is the read-back (openShellBudget): some setups
// ignore an injected Win+N (measured on the 25H2 laptop: it opened
// nothing, from the helper or a bare SendInput), and open-shell then fails
// with "did not show", the keys sent. A surface that
// is open already is printed with nothing sent: the accelerators toggle,
// so pressing one again would close it. The output says where the
// foreground went: Start and the overflow take it.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

const (
	openShellUsage = "usage: winuse --headed open-shell start|notifications|quick-settings|overflow"
	// openShellBudget bounds the wait for the surface to show; openShellPoll
	// paces it.
	openShellBudget = 2 * time.Second
	openShellPoll   = 100 * time.Millisecond
	// chevronName starts the hidden-icons chevron's name; it gains " Hide"
	// while the overflow is open (probes round 5, section 6).
	chevronName = "Show Hidden Icons"
)

// shellOpening is how open-shell opens one surface and recognises it.
type shellOpening struct {
	kind  string     // the surface kind that shows (win.Shell*)
	host  string     // its host executable; "" for any (quick settings: ShellHost, Windows 11 24H2 on, unmeasured)
	what  string     // the surface, for messages
	chord keys.Chord // the accelerator; nil: the chevron's click
	how   string     // what is pressed, for messages
}

var shellOpenings = map[string]shellOpening{
	"start":          {kind: win.ShellStart, what: "Start", chord: mustChord("Control_L+Escape"), how: "Control_L+Escape"},
	"notifications":  {kind: win.ShellActionCenter, host: "shellexperiencehost.exe", what: "the notification panel", chord: keys.ShellAccelerator('N'), how: "Win+N"},
	"quick-settings": {kind: win.ShellActionCenter, host: "shellhost.exe", what: "quick settings", chord: keys.ShellAccelerator('A'), how: "Win+A"},
	"overflow":       {kind: win.ShellTrayOverflow, what: "the tray overflow", how: "a click on the taskbar's hidden-icons chevron"},
}

// mustChord parses a fixed chord of this file.
func mustChord(s string) keys.Chord {
	c, err := keys.Parse(s)
	if err != nil {
		panic(err)
	}
	return c
}

// isOpening reports whether w is the surface o opens, open: a listed
// shell surface of its kind, from its host when o names one.
func (o shellOpening) isOpening(w win.WindowInfo) bool {
	return w.Shell == o.kind && win.ShellSurface(w) &&
		(o.host == "" || strings.EqualFold(filepath.Base(w.ExePath), o.host))
}

// openShell is `winuse --headed open-shell <surface>`.
func openShell(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.OpenShellArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("open-shell: "+err.Error(), openShellUsage)
		}
		surface := strings.ToLower(strings.TrimSpace(a.Surface))
		o, ok := shellOpenings[surface]
		if !ok {
			return "", Fail(fmt.Sprintf("open-shell opens one of %s, not %q", strings.Join(protocol.ShellSurfaces, ", "), a.Surface), openShellUsage)
		}
		if !a.Headed {
			return "", nothingSent(Fail(fmt.Sprintf("open-shell %s opens %s with %s, which takes the user's foreground and changes their screen; it has no headless route", surface, o.what, o.how),
				"pass --headed to open it: winuse --headed open-shell "+surface))
		}
		if locked, desktop := s.Locked(); locked {
			return "", lockedDesktopFailure(desktop)
		}
		if s.Surfaces == nil {
			return "", Fail("the shell surfaces cannot be listed here", "")
		}
		before, err := s.Surfaces()
		if err != nil {
			return "", Fail("enumerating windows: "+err.Error(), "")
		}
		if at := slices.IndexFunc(before, o.isOpening); at >= 0 {
			return win.FormatWindows(before[at:at+1]) + notePrefix + o.what + " was open already; nothing was sent\n", nil
		}
		at := slices.IndexFunc(before, func(w win.WindowInfo) bool { return w.Class == "Shell_TrayWnd" })
		if at < 0 {
			return "", nothingSent(&Error{Msg: "open-shell found no taskbar", Hint: "run winuse list-windows --shell", Exit: 1, Code: protocol.CodeNotFound})
		}
		taskbar := before[at]
		open := map[uint64]bool{}
		for _, w := range before {
			open[w.ID] = win.ShellSurface(w)
		}
		fg := s.Foreground()
		if o.chord == nil {
			if e := s.clickChevron(ctx, taskbar); e != nil {
				return "", e
			}
		} else if e := s.pressShellAccelerator(ctx, taskbar, o.chord); e != nil {
			return "", e
		}
		w, shown := s.awaitShellSurface(ctx, o, open)
		if !shown {
			return "", Fail(fmt.Sprintf("open-shell %s sent %s and %s did not show within %s", surface, o.how, o.what, openShellBudget),
				"some setups ignore an injected shell accelerator (on the laptop it was measured on, Win+N opened nothing); read winuse list-windows --shell, the shell may have opened something else; or ask the user to open it")
		}
		return win.FormatWindows([]win.WindowInfo{w}) + s.foregroundNote(fg, s.Foreground()), nil
	}
}

// pressShellAccelerator presses a shell accelerator with the real
// keyboard: Headed, under the in-control indicator over the taskbar and an
// armed Esc scope. No Window is activated: the shell takes the keys
// whatever has the foreground. It is polite like the Headed pipeline
// (polite.go): it waits for the user's input to go quiet and releases
// what it holds on every exit, and the surface it opens keeps the
// foreground, as a shell surface does after runAction.
func (s Services) pressShellAccelerator(ctx context.Context, taskbar win.WindowInfo, chord keys.Chord) *Error {
	noteRoute(ctx, taskbar, protocol.RouteHeaded) // trace.go
	if rerr := s.waitForQuietInput(ctx); rerr != nil {
		return rerr
	}
	defer s.releaseHeld()
	s.showOverlay(taskbar)
	defer s.hideOverlay()
	s.Esc.Arm()
	defer s.Esc.Disarm()
	markHeaded(ctx) // selfactivation.go: the foreground moves on purpose
	if s.Esc.Stopped() {
		return stopErrorFor(s.Esc)
	}
	if err := s.PressKeys(chord); err != nil {
		return inputError(err)
	}
	if s.Esc.Stopped() {
		return stopErrorFor(s.Esc)
	}
	return nil
}

// clickChevron clicks the taskbar's hidden-icons chevron with the real
// pointer: the taskbar's tree is read (becoming its Window State), and the
// chevron is clicked through the Headed pipeline, as
// `winuse --headed click <taskbar> <chevron>` does.
func (s Services) clickChevron(ctx context.Context, taskbar win.WindowInfo) *Error {
	tree, err := s.Trees.Capture(ctx, taskbar.ID, taskbar.Title, win.AppExeName(taskbar), s.Budget)
	if err != nil {
		return treeReadError(err)
	}
	index := chevronIndex(tree)
	id := strconv.FormatUint(taskbar.ID, 10)
	if index < 0 {
		return nothingSent(Fail("the taskbar shows no hidden-icons chevron (a button named \""+chevronName+"\")",
			"read winuse get-window-state "+id+"; every tray icon may be shown already, or the shell's language names the chevron otherwise: click it with winuse --headed click "+id+" <its element>"))
	}
	_, e := s.runAction(ctx, id, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		pt, err := s.Actions.ElementPoint(ctx, w.ID, index)
		if err != nil {
			return "", elementError(err, w, index)
		}
		if err := s.Click(w.ID, pt.X, pt.Y, win.MouseLeft, 1); err != nil {
			return "", inputError(err)
		}
		return okOutput, nil
	})
	return e
}

// chevronIndex is the Element Index of the hidden-icons chevron in a
// taskbar's tree, -1 when it has none. Pure.
func chevronIndex(tree *wstate.Tree) int {
	for i, el := range tree.Elements {
		if el.AutomationId == "SystemTrayIcon" && strings.HasPrefix(wstate.Collapse(el.Name), chevronName) {
			return i
		}
	}
	return -1
}

// awaitShellSurface polls up to openShellBudget for the surface o opens
// to show, one that was not open before.
func (s Services) awaitShellSurface(ctx context.Context, o shellOpening, open map[uint64]bool) (win.WindowInfo, bool) {
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for waited := time.Duration(0); ; waited += openShellPoll {
		if all, err := s.Surfaces(); err == nil {
			for _, w := range all {
				if o.isOpening(w) && !open[w.ID] {
					return w, true
				}
			}
		}
		if waited >= openShellBudget || ctx.Err() != nil {
			return win.WindowInfo{}, false
		}
		sleep(openShellPoll)
	}
}
