package helper

// The ContextMenu Secondary Action (headless-max ticket 09,
// headless-100.md G6; ADR 0013 rule 1): `perform-secondary-action
// <window> <element> ContextMenu` opens the Element's context menu
// without the foreground, reads back that a menu opened, and reports the
// menu's Window ID, so the agent reads it with get-window-state and acts
// on it. The route depends on the Element's framework (probes rounds 4 and
// 5, section 2):
//
//	classic control (Win32, WinForms, with its own HWND)  WM_CONTEXTMENU(control, -1), posted
//	Chromium, WinUI 3 and anything else                    UIA ShowContextMenu
//	WPF                                                     refused: no background route
//
// Neither call proves anything by itself (ShowContextMenu returns S_OK on
// a classic Edit and opens nothing), so the read-back is the desktop
// parity 02 snapshot (opened.go): a new menu or popup Window of the
// target's App within contextMenuWait. None fails as delivered with no
// menu. WPF opens no menu in a background Window from either call (ADR
// 0013, "What stays impossible"): it is refused with nothing sent and the
// --headed retry, which right-clicks the Element with the real pointer.
// The @x,y form has no Headless route either.
//
// The menu is topmost over the user's work, so it does not outlive the
// agent's interest in it: the Agent's next Action on another Window closes
// the menu its own ContextMenu opened, and says so in a Note
// (closeLeftMenu). A menu the user opened is never tracked, so never
// closed. `press-key <menu> Escape` closes a `#32768` or a WinUI 3
// PopupHost with Escape posted where each takes it, and reads the close
// back (pressMenuEscape); a Chromium menu popup is a Chrome_WidgetWin_1,
// so the key takes ticket 20's window-message route there. Picking an item
// of a `#32768` is a click by Element Index (menuitem.go); Chromium and
// WinUI 3 items take today's Headless click (UIA Invoke keeps the
// foreground there).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// ContextMenuActor is the element side of the ContextMenu Secondary
// Action, on the UIA worker: the route facts and ShowContextMenu. The real
// implementation is *uia.Client; Services.Actions is type-asserted to it,
// so a fake without it gets the --headed refusal.
type ContextMenuActor interface {
	// ContextMenuTarget reads the Element's framework and own window.
	ContextMenuTarget(ctx context.Context, hwnd uint64, index int) (uia.MenuTarget, error)
	// ShowContextMenu calls IUIAutomationElement3::ShowContextMenu.
	ShowContextMenu(ctx context.Context, hwnd uint64, index int) error
}

var _ ContextMenuActor = (*uia.Client)(nil)

const (
	// contextMenuWait bounds the read-back of an opened menu, and
	// menuCloseWait the read-back of a closed one; contextMenuPoll paces
	// both.
	contextMenuWait = 2 * time.Second
	menuCloseWait   = 2 * time.Second
	contextMenuPoll = 50 * time.Millisecond
)

// menuRoute is how a context menu opens on an Element.
type menuRoute int

const (
	menuRouteNone menuRoute = iota // no background route (WPF)
	menuRoutePost                  // WM_CONTEXTMENU posted to the control's own window
	menuRouteUIA                   // UIA ShowContextMenu
)

// contextMenuRoute picks the route from the Element's facts: a classic
// control with its own window takes the posted message (ShowContextMenu
// opens nothing there), WPF has none, and everything else takes
// ShowContextMenu (Chromium and WinUI 3 measured; the rest is read back).
func contextMenuRoute(t uia.MenuTarget) menuRoute {
	switch t.Framework {
	case "WPF":
		return menuRouteNone
	case "Win32", "WinForm", "":
		if t.Native != 0 {
			return menuRoutePost
		}
	}
	return menuRouteUIA
}

// The context menu route's own failures, mapped by contextMenuRefusal.
var (
	// errMenuWPF: a WPF Element (nothing was sent).
	errMenuWPF = errors.New("its context menu has no background route: WPF opens no menu in a background Window, from UI Automation's ShowContextMenu or a posted WM_CONTEXTMENU")
	// errMenuNoReadback: the desktop cannot be enumerated to read a menu
	// back (nothing was sent).
	errMenuNoReadback = errors.New("winuse cannot list the App's Windows to read an opened menu back")
)

// menuNotOpenedError is a delivered open that showed no menu.
type menuNotOpenedError struct{ route menuRoute }

func (e *menuNotOpenedError) Error() string {
	how := "UI Automation's ShowContextMenu was delivered"
	if e.route == menuRoutePost {
		how = "WM_CONTEXTMENU to the Element's window was delivered"
	}
	return how + ", but no menu opened"
}

// contextMenuOpened is what the route opened: the menu (the first new
// Window, topmost), every new Window for the notes, and the window the
// menu closes through (WM_CANCELMODE).
type contextMenuOpened struct {
	menu   win.WindowInfo
	opened []win.WindowInfo
	owner  uint64
}

// contextMenu runs perform-secondary-action ContextMenu. Headed, the
// Element is right-clicked with the real pointer (headedContextMenu).
func (s Services) contextMenu(ctx context.Context, a protocol.SecondaryActionArgs, target elementTarget) (string, *Error) {
	at := target.token()
	if target.point != nil {
		at = pointToken(target.point.X, target.point.Y)
	}
	retry := func(id string) string {
		return "perform-secondary-action " + id + " " + at + " " + uia.ContextMenuAction
	}
	if a.Headed {
		return s.headedContextMenu(ctx, a.Window, target)
	}
	if target.point != nil {
		return "", s.headedOnly(a.Window,
			"perform-secondary-action ContextMenu at a point has no background route", retry,
			"to open it without the foreground, name the Element by its Element Index, name: or id: from winuse get-window-state")
	}
	actor, ok := s.Actions.(HeadlessActor)
	cm, cmOK := s.Actions.(ContextMenuActor)
	if !ok || !cmOK {
		return "", s.headedOnly(a.Window, "perform-secondary-action ContextMenu has no background route on this helper", retry, "")
	}
	var got contextMenuOpened
	var target0 win.WindowInfo
	out, rerr := s.runHeadless(ctx, a.Window, actor, headlessRun{
		cmd:    "perform-secondary-action",
		verb:   "context menu",
		target: target,
		act: func(ctx context.Context, w win.WindowInfo, index int) error {
			target0 = w
			var err error
			got, err = s.openContextMenu(ctx, cm, w, index)
			return err
		},
		refusal: func(err error, w win.WindowInfo, index int) *Error {
			return contextMenuRefusal(err, w, index, retry)
		},
	})
	if rerr != nil {
		return out, rerr
	}
	s.Menus.set(AgentOf(ctx).ID, leftMenu{menu: got.menu, owner: got.owner, reopen: retry(strconv.FormatUint(target0.ID, 10))})
	return contextMenuOutput(got, target0), nil
}

// openContextMenu opens the menu on the route the Element's facts pick and
// reads it back.
func (s Services) openContextMenu(ctx context.Context, cm ContextMenuActor, w win.WindowInfo, index int) (contextMenuOpened, error) {
	t, err := cm.ContextMenuTarget(ctx, w.ID, index)
	if err != nil {
		return contextMenuOpened{}, err
	}
	route := contextMenuRoute(t)
	if route == menuRouteNone {
		return contextMenuOpened{}, errMenuWPF
	}
	before, ok := s.snapshotOf(w)
	if !ok {
		return contextMenuOpened{}, errMenuNoReadback
	}
	owner := w.ID
	if route == menuRoutePost {
		owner = t.Native
		if err := s.PostContextMenu(t.Native); err != nil {
			return contextMenuOpened{}, &notSentError{err: err, hint: "the Window may have closed; run winuse list-windows"}
		}
	} else if err := cm.ShowContextMenu(ctx, w.ID, index); err != nil {
		return contextMenuOpened{}, err
	}
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for waited := time.Duration(0); waited < contextMenuWait; waited += contextMenuPoll {
		sleep(contextMenuPoll)
		if ctx.Err() != nil {
			break
		}
		after, err := s.Surfaces()
		if err != nil {
			break
		}
		if opened, _ := openedChanges(before, after); len(opened) > 0 {
			return contextMenuOpened{menu: opened[0], opened: opened, owner: owner}, nil
		}
	}
	return contextMenuOpened{}, &menuNotOpenedError{route: route}
}

// contextMenuRefusal maps the route's failures: WPF, no read-back and a
// message that could not be posted sent nothing; a delivered open with no
// menu says so; the rest is the Secondary Actions' element mapping.
func contextMenuRefusal(err error, w win.WindowInfo, index int, retry func(id string) string) *Error {
	id := strconv.FormatUint(w.ID, 10)
	var notSent *notSentError
	var none *menuNotOpenedError
	switch {
	case errors.Is(err, errMenuWPF):
		return headedRefusal(w, fmt.Sprintf("element %d is a WPF Element: %v", index, err), retry, "")
	case errors.Is(err, errMenuNoReadback), errors.Is(err, uia.ErrNoShowContextMenu):
		return headedRefusal(w, err.Error(), retry, "")
	case errors.As(err, &notSent):
		return nothingSent(Fail(err.Error(), notSent.hint))
	case errors.As(err, &none):
		return Fail(err.Error(),
			"the Element may have no context menu here; read the Window State (winuse get-window-state "+id+
				"), or open it with the real pointer, which brings the Window to the foreground: winuse --headed "+retry(id))
	}
	return secondaryError(err, w, index)
}

// contextMenuOutput is the open's `ok`: how to go on, and the opened
// Windows as desktop-parity 02 notes them. A Chromium menu's edit items
// act on the browser's focus, not on the right-clicked Element (round 5),
// and the output says so.
func contextMenuOutput(got contextMenuOpened, target win.WindowInfo) string {
	id := got.menu.ID
	var b strings.Builder
	fmt.Fprintf(&b, "ok (the context menu opened: read it with winuse get-window-state %d, click an item, or close it with winuse press-key %d Escape", id, id)
	if got.menu.Class == win.ChromiumClass {
		b.WriteString("; its edit items, such as Cut, Paste or Select all, act on the browser's focus, not on this Element")
	}
	b.WriteString(")\n")
	b.WriteString(formatOpened(got.opened, target, false))
	return b.String()
}

// headedContextMenu is perform-secondary-action ContextMenu with
// --headed: a right click on the Element, or at the point, with the real
// pointer, in the Headed pipeline.
func (s Services) headedContextMenu(ctx context.Context, ref string, target elementTarget) (string, *Error) {
	if s.Actions == nil {
		return "", Fail("perform-secondary-action is unavailable: no UIA client", "")
	}
	return s.runAction(ctx, ref, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		var x, y int32
		if target.point != nil {
			pt, serr := shotPoint(ctx, s, w, target.point.X, target.point.Y, "perform-secondary-action")
			if serr != nil {
				return "", serr
			}
			x, y = int32(pt.X), int32(pt.Y)
		} else {
			index, rerr := target.resolve(ctx, s, w)
			if rerr != nil {
				return "", rerr
			}
			pt, err := s.Actions.ElementPoint(ctx, w.ID, index)
			if err != nil {
				return "", elementError(err, w, index)
			}
			x, y = pt.X, pt.Y
		}
		if err := s.Click(w.ID, x, y, win.MouseRight, 1); err != nil {
			return "", inputError(err)
		}
		return okOutput, nil
	})
}

// leftMenu is a menu an Agent's own ContextMenu opened: the menu, the
// window it closes through, and the command that opened it (an ignored
// item's --headed retry reopens it with that, menuitem.go).
type leftMenu struct {
	menu   win.WindowInfo
	owner  uint64
	reopen string
}

// agentMenus holds the last menu each Agent's ContextMenu opened, until
// that Agent's next Action elsewhere closes it. A nil store tracks
// nothing.
type agentMenus struct {
	mu   sync.Mutex
	open map[string]leftMenu
}

func newAgentMenus() *agentMenus { return &agentMenus{open: map[string]leftMenu{}} }

// set records the menu agent's Action opened, replacing an older one.
func (m *agentMenus) set(agent string, lm leftMenu) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.open[agent] = lm
}

// peek is the menu agent left open, if any.
func (m *agentMenus) peek(agent string) (leftMenu, bool) {
	if m == nil {
		return leftMenu{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	lm, ok := m.open[agent]
	return lm, ok
}

// forget drops agent's menu if it is still that one.
func (m *agentMenus) forget(agent string, id uint64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.open[agent].menu.ID == id {
		delete(m.open, agent)
	}
}

// closeLeftMenu wraps an Action command: before it runs on a Window other
// than the menu its Agent's ContextMenu left open, that menu is closed,
// and a Note says so after the Action's `ok` (or in the hint of its
// failure). A command that names no Window leaves the menu open.
func (s Services) closeLeftMenu(name string, cmd Command) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		closed := s.closeAgentMenu(ctx, args)
		out, err := cmd(ctx, args)
		if closed == "" {
			return out, err
		}
		if err != nil {
			e := *err
			if e.Hint != "" {
				e.Hint += "; "
			}
			e.Hint += "winuse also " + closed
			return out, &e
		}
		return withNotes(out, notePrefix+closed+"\n"), nil
	}
}

// closeAgentMenu closes the menu the request's Agent left open when the
// request targets another Window, and returns the note's text; "" when
// nothing was closed.
func (s Services) closeAgentMenu(ctx context.Context, args json.RawMessage) string {
	agent := AgentOf(ctx).ID
	lm, ok := s.Menus.peek(agent)
	if !ok {
		return ""
	}
	var a struct {
		Window string `json:"window"`
	}
	if json.Unmarshal(args, &a) != nil || strings.TrimSpace(a.Window) == "" {
		return ""
	}
	if strings.TrimSpace(a.Window) == strconv.FormatUint(lm.menu.ID, 10) {
		return "" // an Action on the menu itself: an item, Escape
	}
	if w, rerr := resolveWindow(s, a.Window); rerr == nil && w.ID == lm.menu.ID {
		return ""
	}
	s.Menus.forget(agent, lm.menu.ID)
	if s.WindowOpen == nil || !s.WindowOpen(lm.menu.ID) {
		return "" // closed already: an item was chosen, or Escape
	}
	if err := s.closeMenu(lm); err != nil {
		log.Printf("closing context menu %d: %v", lm.menu.ID, err)
		return ""
	}
	return fmt.Sprintf("closed context menu %d, which this Agent's ContextMenu had opened and left open", lm.menu.ID)
}

// closeMenu closes a menu the way its kind closes without the foreground:
// a `#32768` through WM_CANCELMODE to the window tracking it (round 4),
// any other with Escape posted where it takes it (a Chromium menu popup
// itself, a WinUI 3 PopupHost's input windows; round 5). Chromium menus
// are never collapsed through UI Automation: that hung the client once.
func (s Services) closeMenu(lm leftMenu) error {
	if lm.menu.Class == win.MenuClass {
		owner := lm.owner
		if s.MenuOwner != nil {
			if o := s.MenuOwner(lm.menu.ID); o != 0 {
				owner = o
			}
		}
		if owner == 0 || s.CancelMenu == nil {
			return fmt.Errorf("menu %d has no owner to cancel it through", lm.menu.ID)
		}
		return s.CancelMenu(owner)
	}
	if s.PostMenuEscape == nil {
		return errors.New("no Escape route")
	}
	_, err := s.PostMenuEscape(lm.menu.ID)
	return err
}

// menuEscapeStep is the Batch's scope question for a menu's Escape:
// Escape alone, unheaded and unposted, on a `#32768` or a WinUI 3
// PopupHost.
func menuEscapeStep(args json.RawMessage, w win.WindowInfo) bool {
	var a protocol.PressKeyArgs
	if (w.Class != win.MenuClass && w.Class != win.PopupHostClass) || json.Unmarshal(args, &a) != nil || a.Headed || a.Post {
		return false
	}
	chord, err := keys.Parse(a.Key)
	return err == nil && len(chord) == 1 && chord[0].Name == "Escape"
}

// pressMenuEscape is press-key Escape on an open `#32768` or WinUI 3
// PopupHost: Escape posted where the menu takes it (win.PostMenuEscape),
// and the menu closing is the read-back. ok is false when the request is
// not that route; the other routes run.
func (s Services) pressMenuEscape(ctx context.Context, args json.RawMessage, ref string) (string, *Error, bool) {
	if s.PostMenuEscape == nil {
		return "", nil, false
	}
	w, rerr := resolveWindow(s, ref)
	if rerr != nil || !menuEscapeStep(args, w) {
		return "", nil, false
	}
	out, rerr := s.runKeyHeadless(ctx, w, keyRun{
		label: cursorLabel("press Escape", ""),
		act: func() error {
			n, err := s.PostMenuEscape(w.ID)
			if n == 0 && err != nil {
				return &notSentError{err: err, hint: "the menu may have closed; run winuse list-windows"}
			}
			if err != nil {
				return err
			}
			if !s.menuClosed(w.ID) {
				return &uia.VerifyError{Msg: fmt.Sprintf("Escape to menu %d was delivered, but the menu is still open", w.ID)}
			}
			return nil
		},
	})
	return out, rerr, true
}

// menuClosed waits up to menuCloseWait for a menu window to close.
func (s Services) menuClosed(id uint64) bool {
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for waited := time.Duration(0); waited < menuCloseWait; waited += contextMenuPoll {
		if !s.WindowOpen(id) {
			return true
		}
		sleep(contextMenuPoll)
	}
	return !s.WindowOpen(id)
}
