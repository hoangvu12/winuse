package helper

// Picking an item of an open classic menu (headless-max ticket 09): `click
// <menu> <element>` on a `#32768` Window, the menu a ContextMenu Secondary
// Action reported. UIA Invoke on a `#32768` item takes the foreground
// (probes round 4; ADR 0013), so the Headless click never reaches it
// there. Instead the item is found in the menu itself: MN_GETHMENU hands
// the HMENU, read like invoke-menu's (win.ReadMenu), and the Element's
// name picks the item (matchMenuItems). An item with a mnemonic no other
// item shares is chosen by posting that key to the menu, which works for a
// TPM_RETURNCMD menu like the Edit's; any other by posting WM_COMMAND with
// its ID to the window tracking the menu. The read-back is the menu
// closing; one still open is closed with WM_CANCELMODE and the click fails
// as ignored. A submenu, a greyed item, or a name that matches no item
// (or several) is refused with nothing sent and the --headed retry. A
// click at a point on a `#32768` is refused the same way: a point would
// reach the Invoke route.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// menuPick is the item a click on a `#32768` chooses and how: its
// mnemonic, or 0 for WM_COMMAND to owner.
type menuPick struct {
	item  win.MenuItem
	key   rune
	owner uint64
}

// menuMnemonic reads an item's mnemonic, lower case: the letter or digit
// after its single & ("Select &All\tCtrl+A" has 'a'); false when it has
// none, or one that is no letter or digit.
func menuMnemonic(text string) (rune, bool) {
	text, _, _ = strings.Cut(text, "\t")
	for i := 0; i < len(text); i++ {
		if text[i] != '&' {
			continue
		}
		if i+1 < len(text) && text[i+1] == '&' {
			i++ // a literal &
			continue
		}
		if i+1 >= len(text) {
			return 0, false
		}
		c := rune(text[i+1])
		switch {
		case c >= 'A' && c <= 'Z':
			return c - 'A' + 'a', true
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			return c, true
		}
		return 0, false
	}
	return 0, false
}

// uniqueMnemonic is it's mnemonic when no other enabled item of the menu,
// a submenu included, has the same one: a shared mnemonic only moves the
// highlight in a real menu. A greyed item takes no key.
func uniqueMnemonic(items []win.MenuItem, it win.MenuItem) (rune, bool) {
	key, ok := menuMnemonic(it.Text)
	if !ok {
		return 0, false
	}
	for _, other := range items {
		if other.Pos == it.Pos || other.Separator() || other.Unavailable() {
			continue
		}
		if k, ok := menuMnemonic(other.Text); ok && k == key {
			return 0, false
		}
	}
	return key, true
}

// classicMenuClick runs a left single click on an item of a `#32768`
// Window, the route above. ok is false when the click is not aimed at a
// `#32768` (or is headed, posted, or another button or count): the other
// routes run.
func (s Services) classicMenuClick(ctx context.Context, a protocol.ClickArgs, target elementTarget, button win.MouseButton, count int) (string, *Error, bool) {
	if a.Headed || a.Post || button != win.MouseLeft || count != 1 || s.PopupMenu == nil {
		return "", nil, false
	}
	w, rerr := resolveWindow(s, a.Window) // a menu is no listed Window: it resolves as a popup
	if rerr != nil || w.Class != win.MenuClass {
		return "", nil, false
	}
	id := strconv.FormatUint(w.ID, 10)
	if target.point != nil {
		at := pointToken(target.point.X, target.point.Y)
		return "", headedRefusal(w, fmt.Sprintf("a click at %s in classic menu %d has no background route", at, w.ID),
			func(id string) string { return "click " + id + " " + at },
			"to choose an item without the foreground, name it by its Element Index or name: from winuse get-window-state "+id), true
	}
	actor, ok := s.Actions.(HeadlessActor)
	if !ok {
		return "", nil, false
	}
	retry := func(id string) string { return "click " + id + " " + target.token() }
	ignored := false
	out, rerr := s.runKeyHeadless(ctx, w, keyRun{
		prepare: func(run *keyRun) *Error {
			index, rerr := target.resolve(ctx, s, w)
			if rerr != nil {
				return nothingSent(rerr)
			}
			l, t, r, b, name, err := actor.ElementBounds(ctx, w.ID, index)
			if err != nil {
				return nothingSent(clickRefusal(err, w, index))
			}
			p, perr := s.pickMenuItem(w, name, retry)
			if perr != nil {
				return perr
			}
			run.label = cursorLabel("click", menuName(p.item.Text))
			run.target = win.Rect{Left: l, Top: t, Right: r, Bottom: b}
			run.act = func() error {
				if p.key != 0 {
					n, err := s.PostMenuKey(w.ID, p.key)
					if n == 0 && err != nil {
						return &notSentError{err: err, hint: "the menu may have closed; run winuse list-windows"}
					}
					if err != nil {
						return err
					}
				} else if err := s.MenuCommand(p.owner, uint16(p.item.ID)); err != nil {
					return &notSentError{err: err, hint: "the menu may have closed; run winuse list-windows"}
				}
				if s.menuClosed(w.ID) {
					return nil
				}
				ignored = true
				owner := p.owner
				if owner == 0 {
					owner = s.MenuOwner(w.ID)
				}
				if owner != 0 {
					if err := s.CancelMenu(owner); err != nil {
						return &uia.VerifyError{Msg: fmt.Sprintf("%q was chosen in menu %d, but the menu stayed open, and closing it failed: %v", menuName(p.item.Text), w.ID, err)}
					}
				}
				return &uia.VerifyError{Msg: fmt.Sprintf("%q was chosen in menu %d, but the menu stayed open, so winuse closed it", menuName(p.item.Text), w.ID)}
			}
			return nil
		},
	})
	if rerr != nil && ignored {
		e := *rerr
		e.Hint = s.ignoredMenuHint(ctx, w, target)
		return out, &e, true
	}
	return out, rerr, true
}

// pickMenuItem finds the item named name in the open `#32768` and how to
// choose it. Every failure is a refusal raised before anything was sent.
func (s Services) pickMenuItem(w win.WindowInfo, name string, retry func(id string) string) (menuPick, *Error) {
	menu := s.PopupMenu(w.ID)
	if menu == 0 {
		return menuPick{}, headedRefusal(w, fmt.Sprintf("the items of classic menu %d cannot be read (MN_GETHMENU)", w.ID), retry, "")
	}
	items, err := s.ReadMenu(menu)
	if err != nil {
		return menuPick{}, headedRefusal(w, err.Error(), retry, "")
	}
	matches := matchMenuItems(items, name)
	switch {
	case len(matches) == 0:
		return menuPick{}, headedRefusal(w, fmt.Sprintf("classic menu %d has no item named %q; its items are %s", w.ID, menuName(name), menuNames(items)), retry, "")
	case len(matches) > 1:
		return menuPick{}, headedRefusal(w, fmt.Sprintf("classic menu %d has %d items named %q, and winuse cannot tell them apart", w.ID, len(matches), menuName(name)), retry, "")
	}
	it := matches[0]
	at := strconv.Quote(menuName(it.Text))
	switch {
	case it.Popup():
		return menuPick{}, headedRefusal(w, fmt.Sprintf("the menu item %s opens a submenu, which winuse does not open without the foreground", at), retry, "")
	case it.Unavailable():
		return menuPick{}, headedRefusal(w, fmt.Sprintf("the menu item %s is greyed out", at), retry,
			"the App does not offer this command now; close the menu with winuse press-key "+strconv.FormatUint(w.ID, 10)+" Escape")
	}
	if key, ok := uniqueMnemonic(items, it); ok {
		return menuPick{item: it, key: key}, nil
	}
	owner := s.MenuOwner(w.ID)
	switch {
	case owner == 0:
		return menuPick{}, headedRefusal(w, fmt.Sprintf("the menu item %s has no mnemonic of its own, and no window tracks menu %d to send its command to", at, w.ID), retry, "")
	case it.ID > 0xFFFF:
		return menuPick{}, headedRefusal(w, fmt.Sprintf("the menu item %s has command ID %d, which WM_COMMAND cannot carry", at, it.ID), retry, "")
	}
	return menuPick{item: it, owner: owner}, nil
}

// ignoredMenuHint is the hint of a choice the menu ignored: the item may
// need the real pointer, and the exact commands to retry it. The menu is
// closed by then, so a menu this Agent's ContextMenu opened is reopened
// with the command that opened it; any other names the item for the menu
// the agent opens again.
func (s Services) ignoredMenuHint(ctx context.Context, w win.WindowInfo, target elementTarget) string {
	const why = "the menu ignored the posted choice, so the item may need the real pointer, which brings the Window to the foreground: "
	if lm, ok := s.Menus.peek(AgentOf(ctx).ID); ok && lm.menu.ID == w.ID && lm.reopen != "" {
		return why + "reopen the menu with winuse --headed " + lm.reopen + ", then click the item in it with winuse --headed click <menu> " + target.token()
	}
	return why + "open the menu again, then click the item with winuse --headed click <menu> " + target.token()
}
