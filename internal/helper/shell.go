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
//   - A Headless click on a running App's taskbar button is UIA Invoke;
//     its effect is that the App comes to the foreground (or minimizes,
//     when it was in front). The output says where the foreground went.
//   - The taskbar's flyout buttons (Start, Search, Task View, Widgets and
//     the notification area: the hidden-icons chevron, the clock, the
//     quick settings buttons, Show Desktop) are refused Headless: the
//     chevron's Invoke opens the tray overflow and takes the foreground,
//     and the clock's opens nothing. With --headed they are clicked with
//     the real pointer, and Start opens with
//     `winuse --headed press-key <taskbar> Control_L+Escape`.
//   - Escape on the open tray overflow is Headless: posted to the XAML
//     island's input windows, read back by the overflow hiding.
//   - Any other Action on a shell surface that would go Headed (a key or
//     text with no Headless route, a point form, a double, right or middle
//     click) is refused without --headed like everywhere (headless-max
//     ticket 21), so nothing opens on the user's shell unless the agent
//     asked for it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
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
// the foreground went when it moved: a taskbar button activates its App,
// the tray overflow takes the foreground, a Headed Action brings the shell
// forward.
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

// shellFlyoutButtons are the AutomationIds of the taskbar's buttons that
// open a shell flyout rather than act on an App. A Headless pattern call
// on them is refused: the notification area's chevron opens the tray
// overflow and takes the foreground, its clock opens nothing (probes round
// 5, section 6), and Start, Search, Task View and Widgets are unmeasured
// flyouts that open with the foreground. Every notification-area button
// carries the one AutomationId "SystemTrayIcon", which, unlike its Name,
// does not change while the overflow is open or with the display
// language.
var shellFlyoutButtons = map[string]string{
	"SystemTrayIcon": "the notification area's buttons open their flyouts only with real input (the hidden-icons chevron opens the tray overflow and takes the foreground; the clock's panel does not open from UI Automation)",
	"StartButton":    "Start opens only with the foreground",
	"SearchButton":   "Search opens only with the foreground",
	"TaskViewButton": "Task View opens only with the foreground",
	"WidgetsButton":  "Widgets open only with the foreground",
}

// shellFlyoutRefusal refuses a Headless Action on one of the taskbar's
// flyout buttons (runHeadless, after the Element Index resolves and
// before the cursor or any pattern call); nil for any other Element or
// Window.
func (s Services) shellFlyoutRefusal(ctx context.Context, w win.WindowInfo, index int, run headlessRun) *Error {
	if w.Shell != win.ShellTaskbar {
		return nil
	}
	st, ok := s.Actions.(pageStoredElements)
	if !ok {
		return nil
	}
	els, ok := st.StoredElements(ctx, w.ID)
	if !ok || index < 0 || index >= len(els) {
		return nil
	}
	why, ok := shellFlyoutButtons[els[index].AutomationId]
	if !ok {
		return nil
	}
	id := strconv.FormatUint(w.ID, 10)
	hint := "retry with --headed to click it with the real pointer (this brings the taskbar to the foreground and opens the flyout on the user's screen): winuse --headed click " +
		id + " " + run.target.token()
	if els[index].AutomationId == "StartButton" {
		hint += ", or winuse --headed press-key " + id + " Control_L+Escape"
	}
	return nothingSent(Fail(fmt.Sprintf("%s on the taskbar's %q button has no headless route: %s", run.cmd, els[index].Name, why), hint))
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
