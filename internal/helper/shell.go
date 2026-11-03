package helper

// Shell surfaces (desktop-parity 09, ADR 0015): the taskbar, the tray
// overflow, Start and the Action Center. `list-windows --shell` lists
// them, and their Window IDs resolve like any Window's (popupByID), so
// get-window-state reads their trees through UI Automation and the
// Element Actions reach their buttons.
//
// The routes, from the probes (headless-gaps rounds 4 and 5):
//
//   - Reading is Headless: the taskbar's tree reads with the foreground
//     unchanged.
//   - Acting on a shell surface's Elements is Headed only (ADR 0013 rule
//     3: winuse never takes the user's screen without --headed). A
//     Headless click, set-value or Secondary Action on any of them is
//     refused with nothing sent, whatever its form (index, name:/id:,
//     @x,y): a running App's taskbar button activates that App, the
//     hidden-icons chevron opens the tray overflow and takes the
//     foreground, the clock's panel does not open from UI Automation at
//     all, and the rest (Start, Search, Task View, Widgets, the quick
//     settings buttons, Show Desktop, the overflow's tray icons, the
//     items of an open Start or Action Center) are unmeasured flyouts or
//     App activations. An App's button points at
//     `winuse activate-window <its Window>`, the explicit way; every
//     refusal gives the `--headed` retry. Start opens with
//     `winuse --headed press-key <taskbar> Control_L+Escape`.
//   - Element scroll and select-text stay Headless: they activate
//     nothing.
//   - Escape on the open tray overflow is Headless: posted to the XAML
//     island's input windows, read back by the overflow hiding.
//   - Any other Action on a shell surface that would go Headed (a key or
//     text with no Headless route, a double, right or middle click) is
//     refused without --headed like everywhere (headless-max ticket 21).
//     After a --headed Action on a shell surface, a note says where the
//     foreground went.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// shellNoted are the commands noteShell wraps: the Element Actions and
// keys an agent sends to a shell surface.
var shellNoted = []string{"click", "set-value", "perform-secondary-action", "scroll", "press-key", "type-text"}

const (
	// overflowClosePoll and overflowCloseBudget pace the read-back of the
	// overflow's posted Escape (it hid within 1.2 s in the probe).
	overflowClosePoll   = 50 * time.Millisecond
	overflowCloseBudget = 2 * time.Second
)

// shellListing is `list-windows --shell`: the visible Windows and the
// shell surfaces, in z-order, from one unfiltered enumeration.
func (s Services) shellListing() ([]win.WindowInfo, error) {
	if s.Surfaces == nil {
		return nil, errors.New("the shell surfaces cannot be listed here")
	}
	all, err := s.Surfaces()
	if err != nil {
		return nil, err
	}
	return win.WithShellSurfaces(all), nil
}

// shellByID finds a shell surface by its Window ID; false for any other
// reference. Services.ShellKind answers for one window first, so an
// Action on any other Window costs no enumeration.
func (s Services) shellByID(ref string) (win.WindowInfo, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(ref), 10, 64)
	if err != nil || s.ShellKind == nil || s.ShellKind(id) == "" {
		return win.WindowInfo{}, false
	}
	w, ok := s.popupByID(ref)
	return w, ok && w.Shell != ""
}

// noteShell wraps the Actions an agent sends to a shell surface, so that
// one that moved the foreground says where it went (shellNote). The Batch
// dispatches to the same map, so its steps say it too.
func (s Services) noteShell(cmds map[string]Command) {
	for _, name := range shellNoted {
		if cmd, ok := cmds[name]; ok {
			cmds[name] = s.shellNote(cmd)
		}
	}
}

// shellNote is one noted command. On a Window that is not a shell surface
// it changes nothing. On a shell surface, an Action that ran says where
// the foreground went when it moved. Only a --headed Action can move it
// there (it brings the shell forward, and a taskbar button it clicks
// activates its App), and the overflow's Headless Escape leaves the
// foreground on the taskbar.
func (s Services) shellNote(cmd Command) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a struct {
			Window string `json:"window"`
		}
		if s.Foreground == nil || json.Unmarshal(args, &a) != nil {
			return cmd(ctx, args)
		}
		if _, ok := s.shellByID(a.Window); !ok {
			return cmd(ctx, args)
		}
		before := s.Foreground()
		out, rerr := cmd(ctx, args)
		if rerr != nil {
			return out, rerr
		}
		return withNotes(out, s.foregroundNote(before, s.Foreground())), nil
	}
}

// overflowEscapeStep reports whether a press-key request is the tray
// overflow's Headless Escape: plain Escape, neither headed nor posted, on
// the overflow.
func overflowEscapeStep(args json.RawMessage, w win.WindowInfo) bool {
	var a protocol.PressKeyArgs
	if w.Shell != win.ShellTrayOverflow || json.Unmarshal(args, &a) != nil || a.Headed || a.Post {
		return false
	}
	chord, err := keys.Parse(a.Key)
	return err == nil && len(chord) == 1 && chord[0].Name == "Escape"
}

// shellRefusedCmds are the Headless Actions refused on a shell surface's
// Elements: each one can open a flyout or activate an App.
var shellRefusedCmds = map[string]bool{"click": true, "set-value": true, "perform-secondary-action": true}

// shellFlyoutButtons are the AutomationIds of the taskbar's buttons that
// open a shell flyout, with why. Every notification-area button carries
// the one AutomationId "SystemTrayIcon", which, unlike its Name, does not
// change while the overflow is open or with the display language.
var shellFlyoutButtons = map[string]string{
	"SystemTrayIcon": "the notification area's buttons open their flyouts only with real input (the hidden-icons chevron opens the tray overflow and takes the foreground; the clock's panel does not open from UI Automation)",
	"StartButton":    "Start opens only with the foreground",
	"SearchButton":   "Search opens only with the foreground",
	"TaskViewButton": "Task View opens only with the foreground",
	"WidgetsButton":  "Widgets open only with the foreground",
}

// taskbarAppButton matches a running App's taskbar button name, "<App> -
// 1 running window" or "<App> - 3 running windows", and keeps the App.
var taskbarAppButton = regexp.MustCompile(`^(.+?) - \d+ running windows?$`)

// shellElementRefusal refuses a Headless click, set-value or Secondary
// Action on an Element of a shell surface (runHeadless, after the Element
// Index resolves and before the cursor or any pattern call); nil for any
// other command or Window. It never lets the call through: an Element it
// cannot read is refused too.
func (s Services) shellElementRefusal(ctx context.Context, w win.WindowInfo, index int, run headlessRun) *Error {
	if w.Shell == "" || !shellRefusedCmds[run.cmd] {
		return nil
	}
	id := strconv.FormatUint(w.ID, 10)
	retry := "pass --headed to do it with the real pointer, which brings the " + w.Shell + " to the foreground and changes the user's screen: winuse --headed " +
		run.cmd + " " + id + " " + run.target.token()
	if run.cmd != "click" {
		retry += " ..."
	}
	var el wstate.Element
	if st, ok := s.Actions.(pageStoredElements); ok {
		if els, ok := st.StoredElements(ctx, w.ID); ok && index >= 0 && index < len(els) {
			el = els[index]
		}
	}
	what := fmt.Sprintf("element %d of the %s", index, w.Shell)
	if name := strings.Join(strings.Fields(el.Name), " "); name != "" {
		what = fmt.Sprintf("the %s's %q", w.Shell, name)
	}
	why := "the shell's Elements open flyouts or activate Apps, which takes the user's foreground"
	hint := retry
	if w.Shell == win.ShellNotification {
		// A toast's own buttons have the checked route (notifications.go,
		// desktop-parity 18): the notification is read again first.
		why = "a notification's buttons are pressed through winuse's notification commands, which check the notification first"
		hint = "run winuse list-notifications, then winuse notification-action <n> <button> or winuse notification-dismiss <n>; or " + retry
	} else if flyout, ok := shellFlyoutButtons[el.AutomationId]; ok && w.Shell == win.ShellTaskbar {
		why = flyout
		if el.AutomationId == "StartButton" {
			hint += ", or winuse --headed press-key " + id + " Control_L+Escape"
		}
	} else if m := taskbarAppButton.FindStringSubmatch(strings.Join(strings.Fields(el.Name), " ")); m != nil && w.Shell == win.ShellTaskbar {
		why = "a taskbar button activates its App, which takes the user's foreground"
		hint = s.activateHint(m[1]) + retry
	}
	return nothingSent(Fail(fmt.Sprintf("%s on %s has no headless route: %s", run.cmd, what, why), hint))
}

// activateHint names the explicit way to bring an App forward, for the
// refusal of its taskbar button: `winuse activate-window <id>` when the
// App has exactly one listed Window, its `list-windows --app` when it has
// several, "" when no listed Window matches.
func (s Services) activateHint(app string) string {
	if s.Windows == nil {
		return ""
	}
	ws, err := s.Windows()
	if err != nil {
		return ""
	}
	switch mine := win.WindowsOfApp(ws, app); len(mine) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("to bring %s forward, ask for the foreground explicitly: winuse activate-window %d; or ", app, mine[0].ID)
	default:
		return fmt.Sprintf("to bring %s forward, pick its Window in winuse list-windows --app %q and run winuse activate-window <window>; or ", app, app)
	}
}

// foregroundNote is the note line after an Action on a shell surface that
// moved the foreground: the Window now in front, as list-windows names it.
func (s Services) foregroundNote(before, after uint64) string {
	if after == before || after == 0 {
		return ""
	}
	line := fmt.Sprintf("window %d", after)
	if s.Surfaces != nil {
		if all, err := s.Surfaces(); err == nil {
			for _, w := range all {
				if w.ID == after {
					line = fmt.Sprintf("%d %s %q", w.ID, w.App, strings.Join(strings.Fields(win.ListedTitle(w)), " "))
					break
				}
			}
		}
	}
	return notePrefix + "the foreground moved to " + line + "\n"
}

// pressOverflowEscape is press-key Escape on the open tray overflow: the
// Headless route through runKeyHeadless. Escape is posted to the XAML
// island's input windows, and the overflow hiding is the read-back. ok is
// false when the request is not that route; the other routes run.
func (s Services) pressOverflowEscape(ctx context.Context, args json.RawMessage, ref string) (string, *Error, bool) {
	if s.PostOverflowEscape == nil {
		return "", nil, false
	}
	w, ok := s.shellByID(ref)
	if !ok || !overflowEscapeStep(args, w) {
		return "", nil, false
	}
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	out, rerr := s.runKeyHeadless(ctx, w, keyRun{
		label: cursorLabel("press Escape", ""),
		act: func() error {
			n, err := s.PostOverflowEscape(w.ID)
			if n == 0 && err != nil {
				return &notSentError{err: err, hint: "the tray overflow may have closed; run winuse list-windows --shell"}
			}
			if err != nil {
				return err
			}
			for waited := time.Duration(0); waited < overflowCloseBudget; waited += overflowClosePoll {
				if !s.WindowOpen(w.ID) {
					return nil
				}
				sleep(overflowClosePoll)
			}
			if !s.WindowOpen(w.ID) {
				return nil
			}
			return &uia.VerifyError{Msg: "Escape was posted to the tray overflow and it is still open"}
		},
	})
	return out, rerr, true
}
