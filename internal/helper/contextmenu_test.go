package helper

// The ContextMenu Secondary Action's offline tests (headless-max ticket
// 09): the route per Element kind (a posted WM_CONTEXTMENU for a classic
// control, ShowContextMenu for Chromium and the rest, WPF refused with
// nothing sent and the --headed retry), the menu read back and reported,
// the read-back timeout, the close of the menu an Agent left open on its
// next Action elsewhere (never another Agent's), Escape on an open menu,
// and picking an item of a `#32768` by its mnemonic or WM_COMMAND, never
// UIA Invoke. Everything Win32 and element-side is faked; no desktop is
// touched.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

const fakeEditHWND = 0x55 // the classic Edit's own window

var chromeMenu = win.WindowInfo{ID: 780, PID: notepadPID, App: "Notepad", Class: win.ChromiumClass, ExStyle: toolWindow, Visible: true, Bounds: popupBounds}

// contextMenuSpy is a headlessSpy whose element side is also a
// ContextMenuActor, with the menu seams faked and recorded.
type contextMenuSpy struct {
	*headlessSpy
	desk *surfaceDesk

	menuTarget uia.MenuTarget
	showErr    error

	postedTo  []uint64 // WM_CONTEXTMENU targets
	shown     int      // ShowContextMenu calls
	cancelled []uint64 // WM_CANCELMODE owners
	escaped   []uint64 // menus Escape was posted to
	keys      []rune   // mnemonics posted
	commands  []uint16 // WM_COMMAND ids
	cmdOwners []uint64

	open      map[uint64]bool // WindowOpen
	menuItems []win.MenuItem
	owner     uint64 // MenuOwner
}

func newContextMenuSpy(after ...[]win.WindowInfo) *contextMenuSpy {
	s := &contextMenuSpy{headlessSpy: newHeadlessSpy(), menuTarget: uia.MenuTarget{Framework: "Win32", Native: fakeEditHWND}}
	s.svc.Actions = s
	s.desk = newDesk(after...)
	s.desk.wire(&s.svc)
	s.open = map[uint64]bool{editMenu.ID: true, chromeMenu.ID: true}
	s.owner = fakeEditHWND
	s.svc.WindowOpen = func(hwnd uint64) bool { return s.open[hwnd] }
	s.svc.PostContextMenu = func(control uint64) error {
		s.step("post-context-menu")
		s.postedTo = append(s.postedTo, control)
		return nil
	}
	s.svc.CancelMenu = func(owner uint64) error {
		s.step("cancel-menu")
		s.cancelled = append(s.cancelled, owner)
		return nil
	}
	s.svc.PostMenuEscape = func(menu uint64) (int, error) {
		s.step("menu-escape")
		s.escaped = append(s.escaped, menu)
		return 2, nil
	}
	s.svc.PopupMenu = func(menu uint64) uint64 { return 0x99 }
	s.svc.ReadMenu = func(menu uint64) ([]win.MenuItem, error) { return s.menuItems, nil }
	s.svc.MenuOwner = func(menu uint64) uint64 { return s.owner }
	s.svc.PostMenuKey = func(menu uint64, key rune) (int, error) {
		s.step("menu-key")
		s.keys = append(s.keys, key)
		return 2, nil
	}
	s.svc.MenuCommand = func(owner uint64, id uint16) error {
		s.step("menu-command")
		s.commands = append(s.commands, id)
		s.cmdOwners = append(s.cmdOwners, owner)
		return nil
	}
	return s
}

// ContextMenuTarget implements ContextMenuActor.
func (s *contextMenuSpy) ContextMenuTarget(ctx context.Context, hwnd uint64, index int) (uia.MenuTarget, error) {
	s.step("menu-target")
	return s.menuTarget, nil
}

// ShowContextMenu implements ContextMenuActor.
func (s *contextMenuSpy) ShowContextMenu(ctx context.Context, hwnd uint64, index int) error {
	s.step("show-context-menu")
	s.shown++
	return s.showErr
}

func contextMenuArgs(window string, index int) protocol.SecondaryActionArgs {
	return protocol.SecondaryActionArgs{Window: window, ElementIndex: idx(index), Action: "contextmenu"}
}

// runAs runs a command for one Agent.
func runAs(t *testing.T, cmds map[string]Command, id, cmd string, args any) (string, *Error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return cmds[cmd](withAgent(context.Background(), agent.Identity{ID: id, Label: id}), raw)
}

func TestContextMenuRoutePerElementKind(t *testing.T) {
	for _, c := range []struct {
		t    uia.MenuTarget
		want menuRoute
	}{
		{uia.MenuTarget{Framework: "Win32", Native: 5}, menuRoutePost},
		{uia.MenuTarget{Framework: "WinForm", Native: 5}, menuRoutePost},
		{uia.MenuTarget{Framework: "", Native: 5}, menuRoutePost},
		{uia.MenuTarget{Framework: "Win32"}, menuRouteUIA}, // a classic item with no window of its own
		{uia.MenuTarget{Framework: "Chrome"}, menuRouteUIA},
		{uia.MenuTarget{Framework: "XAML", Native: 5}, menuRouteUIA},
		{uia.MenuTarget{Framework: "WPF", Native: 5}, menuRouteNone},
		{uia.MenuTarget{Framework: "WPF"}, menuRouteNone},
	} {
		if got := contextMenuRoute(c.t); got != c.want {
			t.Errorf("contextMenuRoute(%+v) = %d, want %d", c.t, got, c.want)
		}
	}
}

func TestContextMenuClassicPostsAndReportsTheMenu(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{editMenu, notepadMain, otherApp})
	out, e := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", contextMenuArgs("101", 3))
	if e != nil {
		t.Fatalf("ContextMenu: %v", e.Error())
	}
	want := "ok (the context menu opened: read it with winuse get-window-state 778, click an item, or close it with winuse press-key 778 Escape)\n" +
		"Note: a new Window opened: 778 Notepad \"\"\n"
	if out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if len(spy.postedTo) != 1 || spy.postedTo[0] != fakeEditHWND || spy.shown != 0 {
		t.Errorf("WM_CONTEXTMENU to %v and %d ShowContextMenu calls, want one post to the Edit's window", spy.postedTo, spy.shown)
	}
	for _, step := range spy.recorded() {
		if step == "activate" || step == "click" || step == "headless-secondary" {
			t.Errorf("the Headless ContextMenu reached %q: %v", step, spy.recorded())
		}
	}
}

func TestContextMenuChromiumShowsAndWarnsAboutEditItems(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{chromeMenu, notepadMain, otherApp})
	spy.menuTarget = uia.MenuTarget{Framework: "Chrome"}
	out, e := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", contextMenuArgs("101", 3))
	if e != nil {
		t.Fatalf("ContextMenu: %v", e.Error())
	}
	if spy.shown != 1 || len(spy.postedTo) != 0 {
		t.Errorf("%d ShowContextMenu calls and posts %v, want ShowContextMenu alone", spy.shown, spy.postedTo)
	}
	if !strings.Contains(out, "get-window-state 780") || !strings.Contains(out, "act on the browser's focus, not on this Element") {
		t.Errorf("output %q: want the menu and the edit-items warning", out)
	}
}

func TestContextMenuWPFRefusedWithNothingSent(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{editMenu, notepadMain, otherApp})
	spy.menuTarget = uia.MenuTarget{Framework: "WPF", Native: fakeEditHWND}
	_, e := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", contextMenuArgs("101", 3))
	if e == nil {
		t.Fatal("a WPF Element's ContextMenu succeeded")
	}
	if !strings.Contains(e.Msg, "WPF") || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Errorf("message %q: want the WPF reason and nothing sent", e.Msg)
	}
	if !strings.Contains(e.Hint, "winuse --headed perform-secondary-action 101 3 ContextMenu") {
		t.Errorf("hint %q: want the --headed retry", e.Hint)
	}
	if len(spy.postedTo) != 0 || spy.shown != 0 {
		t.Errorf("sent %v and %d ShowContextMenu calls for WPF", spy.postedTo, spy.shown)
	}
}

func TestContextMenuReadbackTimeout(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{notepadMain, otherApp})
	_, e := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", contextMenuArgs("101", 3))
	if e == nil {
		t.Fatal("ContextMenu with no menu opened succeeded")
	}
	if !strings.Contains(e.Msg, "no menu opened") || strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Errorf("message %q: want delivered with no menu, not nothing sent", e.Msg)
	}
	if !strings.Contains(e.Hint, "winuse --headed perform-secondary-action 101 3 ContextMenu") {
		t.Errorf("hint %q: want the --headed retry", e.Hint)
	}
	if want := int(contextMenuWait / contextMenuPoll); spy.desk.sleeps != want {
		t.Errorf("the read-back paused %d times, want %d", spy.desk.sleeps, want)
	}
	if _, ok := spy.svc.Menus.peek(agent.DefaultID); ok {
		t.Error("a menu that never opened is tracked")
	}
}

func TestContextMenuPointAndNoActorRefused(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{editMenu, notepadMain, otherApp})
	_, e := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action",
		protocol.SecondaryActionArgs{Window: "101", Point: &protocol.Point{X: 5, Y: 6}, Action: "ContextMenu"})
	if e == nil || !strings.HasSuffix(e.Msg, nothingSentNote) || !strings.Contains(e.Hint, "--headed perform-secondary-action 101 @5,6 ContextMenu") {
		t.Errorf("@x,y: error %+v, want nothing sent and the --headed retry", e)
	}

	plain := newHeadlessSpy() // no ContextMenuActor
	_, e = runCommand(t, CommandsWith(plain.svc), "perform-secondary-action", contextMenuArgs("101", 3))
	if e == nil || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Errorf("no ContextMenuActor: error %+v, want nothing sent", e)
	}
	if len(spy.postedTo) != 0 || spy.shown != 0 {
		t.Errorf("sent %v and %d ShowContextMenu calls", spy.postedTo, spy.shown)
	}
}

func TestHeadedContextMenuRightClicks(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{editMenu, notepadMain, otherApp})
	args := contextMenuArgs("101", 3)
	args.Headed = true
	out, e := runCommand(t, CommandsWith(spy.svc), "perform-secondary-action", args)
	if e != nil {
		t.Fatalf("--headed ContextMenu: %v", e.Error())
	}
	if spy.clickButton != win.MouseRight || spy.clickCount != 1 {
		t.Errorf("clicked %v x%d, want one right click", spy.clickButton, spy.clickCount)
	}
	if !strings.Contains(out, "Note: a new Window opened: 778") {
		t.Errorf("output %q: want the opened menu noted", out)
	}
}

// The Agent's next Action on another Window closes the menu its own
// ContextMenu left open, once; another Agent's Action never does.
func TestLeftMenuClosedOnTheAgentsNextActionElsewhere(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{editMenu, notepadMain, otherApp})
	cmds := CommandsWith(spy.svc)
	if _, e := runAs(t, cmds, "a", "perform-secondary-action", contextMenuArgs("101", 3)); e != nil {
		t.Fatalf("ContextMenu: %v", e.Error())
	}

	out, e := runAs(t, cmds, "b", "click", protocol.ClickArgs{Window: "202", ElementIndex: idx(1)})
	if e != nil {
		t.Fatalf("agent b's click: %v", e.Error())
	}
	if len(spy.cancelled) != 0 || strings.Contains(out, "closed context menu") {
		t.Errorf("another Agent's Action closed the menu: %v, %q", spy.cancelled, out)
	}

	out, e = runAs(t, cmds, "a", "click", protocol.ClickArgs{Window: "202", ElementIndex: idx(1)})
	if e != nil {
		t.Fatalf("agent a's click: %v", e.Error())
	}
	if len(spy.cancelled) != 1 || spy.cancelled[0] != fakeEditHWND {
		t.Errorf("WM_CANCELMODE to %v, want one to the menu's owner", spy.cancelled)
	}
	if !strings.Contains(out, "Note: closed context menu 778, which this Agent's ContextMenu had opened and left open\n") {
		t.Errorf("output %q: want the close noted", out)
	}

	if _, e := runAs(t, cmds, "a", "click", protocol.ClickArgs{Window: "202", ElementIndex: idx(1)}); e != nil {
		t.Fatalf("agent a's second click: %v", e.Error())
	}
	if len(spy.cancelled) != 1 {
		t.Errorf("the menu was closed %d times, want once", len(spy.cancelled))
	}
}

// A menu that closed already (an item was chosen) is not closed again, and
// an Action on the menu itself keeps it.
func TestLeftMenuKeptForTheMenuAndGoneMenus(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{editMenu, notepadMain, otherApp})
	cmds := CommandsWith(spy.svc)
	if _, e := runAs(t, cmds, "a", "perform-secondary-action", contextMenuArgs("101", 3)); e != nil {
		t.Fatalf("ContextMenu: %v", e.Error())
	}
	spy.svc.Menus.set("a", leftMenu{menu: editMenu, owner: fakeEditHWND})
	if got := spy.svc.closeAgentMenu(withAgent(context.Background(), agent.Identity{ID: "a"}), json.RawMessage(`{"window":"778"}`)); got != "" {
		t.Errorf("an Action on the menu closed it: %q", got)
	}
	spy.open[editMenu.ID] = false
	if got := spy.svc.closeAgentMenu(withAgent(context.Background(), agent.Identity{ID: "a"}), json.RawMessage(`{"window":"202"}`)); got != "" || len(spy.cancelled) != 0 {
		t.Errorf("a closed menu was closed again: %q, %v", got, spy.cancelled)
	}
}

// A Chromium or WinUI menu closes with Escape, never WM_CANCELMODE or
// Collapse.
func TestLeftChromiumMenuClosedWithEscape(t *testing.T) {
	spy := newContextMenuSpy()
	spy.svc.Menus = newAgentMenus()
	spy.svc.Menus.set("a", leftMenu{menu: chromeMenu, owner: 101})
	defaulted := defaultServices(spy.svc)
	if got := defaulted.closeAgentMenu(withAgent(context.Background(), agent.Identity{ID: "a"}), json.RawMessage(`{"window":"101"}`)); got == "" {
		t.Fatal("the Chromium menu was not closed")
	}
	if len(spy.escaped) != 1 || spy.escaped[0] != chromeMenu.ID || len(spy.cancelled) != 0 {
		t.Errorf("Escape to %v, WM_CANCELMODE to %v; want Escape to the menu alone", spy.escaped, spy.cancelled)
	}
}

func TestPressEscapeOnAMenu(t *testing.T) {
	spy := newContextMenuSpy([]win.WindowInfo{editMenu, notepadMain, otherApp})
	spy.desk.before = []win.WindowInfo{editMenu, notepadMain, otherApp}
	spy.svc.PostMenuEscape = func(menu uint64) (int, error) {
		spy.step("menu-escape")
		spy.escaped = append(spy.escaped, menu)
		spy.open[menu] = false
		return 2, nil
	}
	out, e := runCommand(t, CommandsWith(spy.svc), "press-key", protocol.PressKeyArgs{Window: "778", Key: "Escape"})
	if e != nil {
		t.Fatalf("press-key 778 Escape: %v", e.Error())
	}
	if !strings.HasPrefix(out, "ok\n") || len(spy.escaped) != 1 || spy.escaped[0] != editMenu.ID {
		t.Errorf("output %q, Escape to %v; want ok and one Escape to the menu", out, spy.escaped)
	}

	stuck := newContextMenuSpy([]win.WindowInfo{editMenu, notepadMain, otherApp})
	stuck.desk.before = []win.WindowInfo{editMenu, notepadMain, otherApp}
	_, e = runCommand(t, CommandsWith(stuck.svc), "press-key", protocol.PressKeyArgs{Window: "778", Key: "Escape"})
	if e == nil || !strings.Contains(e.Msg, "still open") || !strings.Contains(e.Hint, "do not repeat it") {
		t.Errorf("a menu that stays open: error %+v, want the failed read-back", e)
	}
}

// classicMenuItems is the faked `#32768` of a classic Edit.
var classicMenuItems = []win.MenuItem{
	{Pos: 0, Text: "&Undo\tCtrl+Z", ID: 772},
	{Pos: 1, Flags: win.MenuSeparator},
	{Pos: 2, Text: "Cu&t", ID: 768},
	{Pos: 3, Text: "&Paste", ID: 770, Flags: win.MenuGrayed},
	{Pos: 4, Text: "Select &All", ID: 177},
	{Pos: 5, Text: "Show Unicode", ID: 32769},
	{Pos: 6, Text: "&Insert", Flags: win.MenuPopup, Submenu: 0x77},
	{Pos: 7, Text: "&Imagine", ID: 300},
	{Pos: 8, Text: "Re&do", ID: 301},
	{Pos: 9, Text: "&Redo all", ID: 302, Flags: win.MenuGrayed},
}

func menuClickSpy(name string) *contextMenuSpy {
	spy := newContextMenuSpy()
	spy.svc.Surfaces = func() ([]win.WindowInfo, error) { // the menu is listed while it is open
		if spy.open[editMenu.ID] {
			return []win.WindowInfo{editMenu, notepadMain, otherApp}, nil
		}
		return []win.WindowInfo{notepadMain, otherApp}, nil
	}
	spy.menuItems = classicMenuItems
	spy.boundsName = name
	spy.svc.PostMenuKey = func(menu uint64, key rune) (int, error) {
		spy.step("menu-key")
		spy.keys = append(spy.keys, key)
		spy.open[menu] = false
		return 2, nil
	}
	spy.svc.MenuCommand = func(owner uint64, id uint16) error {
		spy.step("menu-command")
		spy.commands = append(spy.commands, id)
		spy.cmdOwners = append(spy.cmdOwners, owner)
		spy.open[editMenu.ID] = false
		return nil
	}
	return spy
}

func TestMenuMnemonic(t *testing.T) {
	for in, want := range map[string]rune{"Select &All\tCtrl+A": 'a', "Cu&t": 't', "&9 items": '9', "Save && E&xit": 'x'} {
		if got, ok := menuMnemonic(in); !ok || got != want {
			t.Errorf("menuMnemonic(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"Show Unicode", "Save && Exit", "Trailing&", "&-dash", "Paste\t&Ctrl+V"} {
		if got, ok := menuMnemonic(in); ok {
			t.Errorf("menuMnemonic(%q) = %q, want none", in, got)
		}
	}
	// "&Insert" (a submenu) shares 'i' with "&Imagine"; the greyed "&Redo
	// all" takes no key, so "Re&do" keeps 'd' and 'r' is free.
	if _, ok := uniqueMnemonic(classicMenuItems, classicMenuItems[7]); ok {
		t.Error("a mnemonic shared with a submenu counts as unique")
	}
	if k, ok := uniqueMnemonic(classicMenuItems, classicMenuItems[8]); !ok || k != 'd' {
		t.Errorf("Re&do: %q, %v; want its own 'd'", k, ok)
	}
}

func TestClassicMenuItemByMnemonic(t *testing.T) {
	spy := menuClickSpy("Select All")
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("778", 4, 1, "left"))
	if e != nil {
		t.Fatalf("click 778 4: %v", e.Error())
	}
	if len(spy.keys) != 1 || spy.keys[0] != 'a' || len(spy.commands) != 0 {
		t.Errorf("posted keys %q and commands %v, want the mnemonic 'a' alone", spy.keys, spy.commands)
	}
	if !strings.HasPrefix(out, "ok\n") {
		t.Errorf("output %q", out)
	}
	for _, step := range spy.recorded() {
		if step == "headless-click" || step == "activate" {
			t.Errorf("a #32768 item reached %q (UIA Invoke takes the foreground there)", step)
		}
	}
}

func TestClassicMenuItemByCommandWithoutAMnemonic(t *testing.T) {
	for _, name := range []string{"Show Unicode", "Imagine"} { // none, and one shared with a submenu
		spy := menuClickSpy(name)
		if _, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("778", 5, 1, "left")); e != nil {
			t.Fatalf("click %q: %v", name, e.Error())
		}
		if len(spy.keys) != 0 || len(spy.commands) != 1 || spy.cmdOwners[0] != fakeEditHWND {
			t.Errorf("%q: keys %q, commands %v to %v; want one WM_COMMAND to the owner", name, spy.keys, spy.commands, spy.cmdOwners)
		}
	}
}

func TestClassicMenuItemRefusals(t *testing.T) {
	for name, wantMsg := range map[string]string{
		"Paste":  "greyed out",
		"Insert": "opens a submenu",
		"Fly":    "has no item named \"Fly\"",
	} {
		spy := menuClickSpy(name)
		_, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("778", 3, 1, "left"))
		if e == nil {
			t.Errorf("%q: the click succeeded", name)
			continue
		}
		if !strings.Contains(e.Msg, wantMsg) || !strings.HasSuffix(e.Msg, nothingSentNote) || !strings.Contains(e.Hint, "winuse --headed click 778 3") {
			t.Errorf("%q: error %+v; want %q, nothing sent and the --headed retry", name, e, wantMsg)
		}
		if len(spy.keys)+len(spy.commands) != 0 {
			t.Errorf("%q: sent keys %q and commands %v", name, spy.keys, spy.commands)
		}
	}

	spy := menuClickSpy("Select All")
	_, e := runCommand(t, CommandsWith(spy.svc), "click", protocol.ClickArgs{Window: "778", Point: &protocol.Point{X: 1, Y: 2}})
	if e == nil || !strings.HasSuffix(e.Msg, nothingSentNote) || len(spy.keys) != 0 {
		t.Errorf("a click at a point in a #32768: error %+v, keys %q; want refused", e, spy.keys)
	}
	for _, step := range spy.recorded() {
		if step == "headless-click" {
			t.Error("a click at a point in a #32768 reached UIA Invoke")
		}
	}
}

func TestClassicMenuItemIgnoredIsClosedAndFails(t *testing.T) {
	spy := menuClickSpy("Show Unicode")
	spy.svc.MenuCommand = func(owner uint64, id uint16) error { // the menu stays open (TPM_RETURNCMD)
		spy.commands = append(spy.commands, id)
		return nil
	}
	spy.svc.Menus = newAgentMenus()
	spy.svc.Menus.set(agent.DefaultID, leftMenu{menu: editMenu, owner: fakeEditHWND, reopen: "perform-secondary-action 101 3 ContextMenu"})
	_, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("778", 5, 1, "left"))
	if e == nil {
		t.Fatal("an ignored choice succeeded")
	}
	if !strings.Contains(e.Msg, "stayed open, so winuse closed it") || len(spy.cancelled) != 1 {
		t.Errorf("message %q, WM_CANCELMODE %v; want the menu closed and the failure", e.Msg, spy.cancelled)
	}
	if strings.Contains(e.Hint, "may have taken effect") ||
		!strings.Contains(e.Hint, "winuse --headed perform-secondary-action 101 3 ContextMenu") ||
		!strings.Contains(e.Hint, "winuse --headed click <menu> 5") {
		t.Errorf("hint %q: want the exact --headed retry", e.Hint)
	}
}
