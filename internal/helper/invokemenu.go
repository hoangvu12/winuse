package helper

// invoke-menu (headless-max ticket 08, headless-100.md G7; ADR 0013 rule
// 1): `invoke-menu <window> "File > Save As..."` runs a command of a
// Window's classic (HMENU) menu bar without opening the menu, without keys
// and without the foreground. UI Automation cannot reach these menus
// headless: Expand on a classic menu bar item of a background Window fails
// with UIA_E_INVALIDOPERATION and opens nothing, and UIA Invoke on an item
// of an open `#32768` menu takes the foreground (probes round 4).
//
// The route reads the menu itself (win.ReadMenu) level by level, matching
// each name of the path case-insensitively, without the & markers and the
// tab-separated shortcut. Before a submenu is read, WM_INITMENUPOPUP lets
// the App grey, check or fill its items as it would before showing them;
// it runs no command and is safe to repeat, so a refusal after it still
// says "nothing was sent". The readback ADR 0013 asks for is that state
// check: an item that is greyed or disabled, at any level of the path, is
// refused before anything is posted. Then WM_COMMAND(MAKEWPARAM(id, 0)) is
// posted to the Window, the message a chosen menu item sends.
//
// It runs in runKeyHeadless: the Window refusals (locked desktop,
// administrator Window, a modal dialog or a minimized Window) come first,
// so no init message reaches a Window winuse refuses; the menu is walked
// in its prepare step; then the passive Esc scope, the Agent Cursor's
// travel to the menu bar item, and the post.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// invokeMenuUsageHint is the recovery hint for invoke-menu's argument
// failures.
const invokeMenuUsageHint = `usage: winuse invoke-menu <window> "<menu> > <item>", such as "File > Save As..."`

// menuPath checks invoke-menu's path: at least one name, none empty, each
// trimmed.
func menuPath(path []string) ([]string, *Error) {
	if len(path) == 0 {
		return nil, Fail("invoke-menu: a menu path is required", invokeMenuUsageHint)
	}
	out := make([]string, len(path))
	for i, name := range path {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, Fail("invoke-menu: the menu path has an empty item name; separate the names with >", invokeMenuUsageHint)
		}
		out[i] = name
	}
	return out, nil
}

// headlessInvokeMenu is the routing predicate's half for invoke-menu: a
// valid path. It has no headed form.
func headlessInvokeMenu(a protocol.InvokeMenuArgs) bool {
	_, perr := menuPath(a.Path)
	return perr == nil
}

// menuName is an item's caption as a person reads it: without the
// tab-separated shortcut and the & markers, whitespace collapsed.
// "Save &As...\tCtrl+Shift+S" reads "Save As...".
func menuName(text string) string {
	text, _, _ = strings.Cut(text, "\t")
	return strings.Join(strings.Fields(mnemonicFree(text)), " ")
}

// pathName is the agent's name for one level in the same form, minus a
// shortcut copied along with it; its & stay literal.
func pathName(name string) string {
	name, _, _ = strings.Cut(name, "\t")
	return strings.Join(strings.Fields(name), " ")
}

// trimEllipsis drops a trailing "..." or "…": "Save As..." also answers
// to "Save As".
func trimEllipsis(s string) string {
	s = strings.TrimSuffix(strings.TrimSuffix(s, "..."), "…")
	return strings.TrimSpace(s)
}

// matchMenuItems finds the items of one menu level that name answers
// to, case-insensitively: the exact names first, and only when none
// matches, the names with a trailing ellipsis ignored on both sides.
// Separators and items with no text never match.
func matchMenuItems(items []win.MenuItem, name string) []win.MenuItem {
	want := strings.ToLower(pathName(name))
	var exact, loose []win.MenuItem
	for _, it := range items {
		if it.Separator() || it.Text == "" {
			continue
		}
		got := strings.ToLower(menuName(it.Text))
		switch {
		case got == want:
			exact = append(exact, it)
		case trimEllipsis(got) == trimEllipsis(want) && trimEllipsis(want) != "":
			loose = append(loose, it)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return loose
}

// menuNames lists a level's item names for a refusal, quoted, in menu
// order.
func menuNames(items []win.MenuItem) string {
	var names []string
	for _, it := range items {
		if it.Separator() || it.Text == "" {
			continue
		}
		names = append(names, strconv.Quote(menuName(it.Text)))
	}
	if len(names) == 0 {
		return "none with text"
	}
	return strings.Join(names, ", ")
}

// menuTarget is a walked path: the bar item's position (for the Agent
// Cursor), the command at the end, and the path in the menu's own names.
type menuTarget struct {
	barPos  int
	command win.MenuItem
	names   []string
}

// walkMenu reads the Window's menu bar down the path and returns the
// command it names. Every failure is a refusal raised before anything was
// posted: no menu bar, a name that matches no item or several, a greyed
// or disabled item, a command where the path goes on, a submenu where it
// ends, or an ID that WM_COMMAND cannot carry.
func (s Services) walkMenu(w win.WindowInfo, path []string) (menuTarget, *Error) {
	id := strconv.FormatUint(w.ID, 10)
	menu := s.MenuBar(w.ID)
	if menu == 0 {
		return menuTarget{}, nothingSent(Fail(fmt.Sprintf("window %d has no classic menu bar", w.ID),
			"click the menu Elements instead: read the Window State (winuse get-window-state "+id+
				"), click the menu by name:, read it again and click the item"))
	}
	var t menuTarget
	for i, name := range path {
		at := strconv.Quote(strings.Join(path[:i+1], " > "))
		items, err := s.ReadMenu(menu)
		if err != nil {
			return menuTarget{}, nothingSent(Fail(err.Error(), "the Window may have closed or changed its menu; run winuse list-windows"))
		}
		matches := matchMenuItems(items, name)
		where := "the menu bar"
		if i > 0 {
			where = strconv.Quote(strings.Join(t.names, " > "))
		}
		switch {
		case len(matches) == 0:
			return menuTarget{}, nothingSent(Fail(fmt.Sprintf("%s has no item %q; its items are %s", where, name, menuNames(items)),
				"name the item as the menu spells it; "+invokeMenuUsageHint))
		case len(matches) > 1:
			return menuTarget{}, nothingSent(Fail(fmt.Sprintf("%s has %d items named %q", where, len(matches), menuName(matches[0].Text)),
				"winuse cannot tell them apart; click the item in the open menu with winuse --headed instead"))
		}
		it := matches[0]
		t.names = append(t.names, menuName(it.Text))
		if i == 0 {
			t.barPos = it.Pos
		}
		if it.Unavailable() {
			return menuTarget{}, nothingSent(Fail(fmt.Sprintf("the menu item %s is greyed out", strconv.Quote(strings.Join(t.names, " > "))),
				"the App does not offer this command now; read the Window State and change what it depends on first, such as a selection"))
		}
		last := i == len(path)-1
		if it.Popup() {
			s.initMenuPopup(w, it)
			menu = it.Submenu
			if last {
				sub, err := s.ReadMenu(menu)
				names := "unreadable"
				if err == nil {
					names = menuNames(sub)
				}
				return menuTarget{}, nothingSent(Fail(fmt.Sprintf("%s opens a submenu, not a command; its items are %s", at, names),
					"add one of its items to the path; "+invokeMenuUsageHint))
			}
			continue
		}
		if !last {
			return menuTarget{}, nothingSent(Fail(fmt.Sprintf("%s is a command with no submenu, so %q cannot follow it", at, path[i+1]),
				"end the path at that command; "+invokeMenuUsageHint))
		}
		if it.ID > 0xFFFF {
			return menuTarget{}, nothingSent(Fail(fmt.Sprintf("the menu item %s has command ID %d, which WM_COMMAND cannot carry", at, it.ID),
				"click the item in the open menu with winuse --headed instead"))
		}
		t.command = it
	}
	return t, nil
}

// initMenuPopup sends WM_INITMENUPOPUP for a submenu before it is read. A
// failure (a hung or busy App) is logged and the submenu read as it is.
func (s Services) initMenuPopup(w win.WindowInfo, it win.MenuItem) {
	if it.Submenu == 0 {
		return
	}
	if err := s.InitMenuPopup(w.ID, it.Submenu, it.Pos); err != nil {
		log.Printf("invoke-menu: %v; reading the submenu as it is", err)
	}
}

// invokeMenu runs `winuse invoke-menu <window> <path>`: the Window
// resolves, and runKeyHeadless refuses it or walks and checks the path
// (walkMenu) and posts the command, with the Agent Cursor on the menu bar
// item, labelled `menu "File > Save As..."`. There is nothing more to read back: the
// output is `ok`, and a command that opens a dialog opens a new Window.
func invokeMenu(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.InvokeMenuArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("invoke-menu: "+err.Error(), invokeMenuUsageHint)
		}
		path, perr := menuPath(a.Path)
		if perr != nil {
			return "", perr
		}
		w, rerr := resolveWindow(s, a.Window)
		if rerr != nil {
			return "", rerr
		}
		return s.runKeyHeadless(ctx, w, keyRun{
			prepare: func(run *keyRun) *Error {
				t, merr := s.walkMenu(w, path)
				if merr != nil {
					return merr
				}
				run.label = cursorLabel("menu", strings.Join(t.names, " > "))
				run.target, _ = s.MenuItemRect(w.ID, t.barPos)
				run.act = func() error {
					if err := s.MenuCommand(w.ID, uint16(t.command.ID)); err != nil {
						return &notSentError{err: err, hint: "the Window may have closed; run winuse list-windows"}
					}
					return nil
				}
				return nil
			},
		})
	}
}
