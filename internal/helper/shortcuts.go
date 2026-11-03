package helper

// press-key's shortcut lookup (headless-max ticket 06, like cua's hotkey
// tool; ADR 0013 rule 1): before an unheaded chord with Control, Shift or
// Alt is refused for want of a background route, winuse looks for the
// command the Window labels with that shortcut and runs it Headless:
//
//   - a classic (HMENU) menu bar: every submenu is read (WM_INITMENUPOPUP
//     first, as invoke-menu does), and an item whose name ends in a tab
//     and the shortcut ("&Save\tCtrl+S") is run with its posted
//     WM_COMMAND (invokemenu.go);
//   - otherwise the UI Automation tree: menu items, buttons and split
//     buttons whose AcceleratorKey or name shows the shortcut
//     (keys.LabelShortcuts), invoked through the Headless click's route.
//     WinForms and WinUI 3 menus show their items only once expanded, so
//     there the menu bar items are expanded, searched and collapsed one at
//     a time (uia.InvokeAccelerator).
//
// One enabled match runs. Matches that are all disabled are refused with
// nothing sent: a real key would do nothing either. No match, or several
// enabled ones, fall through to press-key's other routes, and winuse
// never guesses. Few Apps label their shortcuts (probes round 5, section
// 4): WinForms, WinUI 3 menus, Paint-like ribbons and classic menus do;
// WPF never does.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// AcceleratorInvoker is the shortcut lookup's element side: *uia.Client.
type AcceleratorInvoker interface {
	InvokeAccelerator(ctx context.Context, hwnd uint64, shown func(acceleratorKey, name string, menuItem bool) bool, expandMenus bool) (uia.AccelResult, error)
}

var _ AcceleratorInvoker = (*uia.Client)(nil)

// maxShortcutMenuItems and maxShortcutMenuDepth bound the HMENU scan.
const (
	maxShortcutMenuItems = 512
	maxShortcutMenuDepth = 4
)

// menuHit is a classic menu item that shows the shortcut: its path in the
// menu's own names, the item, the menu bar item it sits under, and
// whether it or a menu above it is greyed.
type menuHit struct {
	names    []string
	item     win.MenuItem
	barPos   int
	disabled bool
}

// menuShortcutHits scans the Window's classic menu bar for items showing
// sc. Each submenu gets WM_INITMENUPOPUP before it is read, so lazily
// greyed items read current. A Window with no classic menu bar has none.
func (s Services) menuShortcutHits(w win.WindowInfo, sc keys.Shortcut) []menuHit {
	if s.MenuBar == nil || s.ReadMenu == nil {
		return nil
	}
	bar := s.MenuBar(w.ID)
	if bar == 0 {
		return nil
	}
	var hits []menuHit
	read := 0
	var scan func(menu uint64, path []string, barPos, depth int, disabled bool)
	scan = func(menu uint64, path []string, barPos, depth int, disabled bool) {
		items, err := s.ReadMenu(menu)
		if err != nil {
			return
		}
		for _, it := range items {
			if read++; read > maxShortcutMenuItems {
				return
			}
			if it.Separator() || it.Text == "" {
				continue
			}
			pos := barPos
			if depth == 0 {
				pos = it.Pos
			}
			names := append(append([]string(nil), path...), menuName(it.Text))
			off := disabled || it.Unavailable()
			if it.Popup() {
				if depth+1 < maxShortcutMenuDepth && it.Submenu != 0 {
					s.initMenuPopup(w, it)
					scan(it.Submenu, names, pos, depth+1, off)
				}
				continue
			}
			if sc.ShownBy("", it.Text, true) {
				hits = append(hits, menuHit{names: names, item: it, barPos: pos, disabled: off})
			}
		}
	}
	scan(bar, nil, 0, 0, false)
	return hits
}

// shortcutExpandsMenus reports a top-level class whose menu items exist
// only once their menu bar item is expanded and that labels shortcuts:
// WinForms and WinUI 3 (probes round 5).
func shortcutExpandsMenus(class string) bool {
	return strings.HasPrefix(class, "WindowsForms10.") || class == "WinUIDesktopWin32WindowClass"
}

// pressShortcut runs the shortcut lookup for chord on w (see the file
// comment). handled is false when nothing was found, or several enabled
// commands were: press-key's other routes then run.
func (s Services) pressShortcut(ctx context.Context, w win.WindowInfo, chord keys.Chord) (out string, rerr *Error, handled bool) {
	sc, ok := keys.ShortcutOf(chord)
	if !ok {
		return "", nil, false
	}
	if hits := s.menuShortcutHits(w, sc); len(hits) > 0 {
		var enabled []menuHit
		for _, h := range hits {
			if !h.disabled {
				enabled = append(enabled, h)
			}
		}
		switch {
		case len(enabled) == 1:
			return s.runMenuShortcut(ctx, w, chord, enabled[0])
		case len(enabled) == 0:
			return "", disabledShortcut(w, chord, strings.Join(hits[0].names, " > ")), true
		}
		return "", nil, false
	}
	ai, ok := s.Actions.(AcceleratorInvoker)
	if !ok {
		return "", nil, false
	}
	if rerr := s.headlessRefusals(w, adminPatternHint); rerr != nil {
		return "", rerr, true
	}
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()
	res, err := ai.InvokeAccelerator(ctx, w.ID, sc.ShownBy, shortcutExpandsMenus(w.Class))
	if esc.Stopped() {
		if res.Invoked {
			return "", escStopError(), true
		}
		return "", escStopBeforeActError(), true
	}
	switch {
	case err != nil:
		return "", shortcutFailure(err, w), true
	case res.Disabled:
		return "", disabledShortcut(w, chord, res.Name), true
	case !res.Invoked:
		return "", nil, false
	}
	label := cursorLabel("press "+chord.String(), res.Name)
	if rect, shown := s.cursorTravelRect(ctx, w, win.Rect{}, label); shown {
		s.cursorClick(ctx, w, rect)
	}
	return shortcutOutput(res.Name), nil, true
}

// runMenuShortcut runs a classic menu item found by its shortcut: its
// WM_COMMAND posted inside runKeyHeadless, the Agent Cursor on its menu
// bar item.
func (s Services) runMenuShortcut(ctx context.Context, w win.WindowInfo, chord keys.Chord, h menuHit) (string, *Error, bool) {
	path := strings.Join(h.names, " > ")
	if h.item.ID > 0xFFFF {
		return "", nil, false
	}
	target := win.Rect{}
	if s.MenuItemRect != nil {
		target, _ = s.MenuItemRect(w.ID, h.barPos)
	}
	out, rerr := s.runKeyHeadless(ctx, w, keyRun{
		label:  cursorLabel("press "+chord.String(), path),
		target: target,
		act: func() error {
			if err := s.MenuCommand(w.ID, uint16(h.item.ID)); err != nil {
				return &notSentError{err: err, hint: "the Window may have closed; run winuse list-windows"}
			}
			return nil
		},
	})
	if rerr != nil {
		return "", rerr, true
	}
	if out == okOutput {
		out = shortcutOutput(path)
	}
	return out, nil, true
}

// shortcutOutput is the lookup's success: what ran, so the agent can tell
// it from a key press.
func shortcutOutput(name string) string {
	return fmt.Sprintf("ok (invoked %q through its shortcut)\n", strings.Join(strings.Fields(name), " "))
}

// disabledShortcut refuses a shortcut whose command is disabled: nothing
// was sent, and a real key would do nothing either.
func disabledShortcut(w win.WindowInfo, chord keys.Chord, name string) *Error {
	return nothingSent(Fail(fmt.Sprintf("%s runs %q in window %d, which is disabled now", chord, strings.Join(strings.Fields(name), " "), w.ID),
		"the App does not offer this command now; read the Window State (winuse get-window-state "+strconv.FormatUint(w.ID, 10)+") and change what it depends on first"))
}

// shortcutFailure maps a failed lookup or invoke: a failed invoke may
// have taken effect; anything else is reported as it is.
func shortcutFailure(err error, w win.WindowInfo) *Error {
	id := strconv.FormatUint(w.ID, 10)
	var write *uia.WriteError
	if errors.As(err, &write) {
		return Fail(err.Error(), "the command may have run; read the Window State (winuse get-window-state "+id+") before repeating it")
	}
	return Fail(err.Error(), "read the Window State (winuse get-window-state "+id+") and try again")
}
