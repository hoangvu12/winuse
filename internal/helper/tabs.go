package helper

// The tab commands (browser merge ticket 07, spec "Tabs"): tab-list,
// tab-new, tab-close and tab-switch act on the Tabs of a browser Window
// through its Browser Connection. The Window is mapped to its browser
// window by ticket 04's rule (page.FindTab: title and bounds), and only
// that browser window's Tabs are listed or acted on.
//
// Routing (ADR 0010, 0011, 0012):
//
//   - tab-list reads only. It shows no Agent Cursor and holds no gate.
//   - tab-new and tab-close are Headless: Target.createTarget with
//     background:true (or the extension's background tab in the "winuse"
//     group) and Target.closeTarget. The Agent Cursor glides to the tab
//     strip item when the latest Window State has it (else to the Window),
//     inside a passive Esc scope.
//   - tab-switch is Headless through the extension (Winuse.activateTab,
//     chrome.tabs.update active, never focusing the window), with or
//     without --headed. On a launched, cloned or toggled browser it is
//     Headless too (headless-max ticket 05, ADR 0013 rule 1): the Tab's
//     item in the Window's own UIA tab strip is matched by the Tab's title
//     and selected through SelectionItem.Select, and its IsSelected is
//     read back (probes round 4, section 4: foreground and cursor
//     unchanged on a covered Chrome). No match, several matches or no tab
//     strip refuse with "nothing was sent" and the --headed retry: winuse
//     never switches mode on its own. With --headed the Window comes to
//     the foreground under the banner (runAction), real keys switch the
//     Tab (Ctrl+<n>, or Ctrl+Tab steps), and CDP verifies the Tab became
//     visible. Target.activateTarget and Page.bringToFront stay forbidden.
//
// winuse never sends Target.activateTarget or Page.bringToFront.
//
// Every refusal raised before anything reaches the browser says "nothing
// was sent". The last Tab of a Window is never closed: closing it closes
// the Window, which close-window says explicitly.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

const (
	// tabVerifyWait bounds the read-back of a switch: the extension's
	// active flag, or the Tab's visibility after the keys.
	tabVerifyWait = time.Second
	// tabStepWait is how long one Ctrl+Tab step waits for the Tab to show
	// before the next step.
	tabStepWait = 300 * time.Millisecond
	// tabTitleWidth caps a title in tab list, in characters.
	tabTitleWidth = 80
)

// tabCommands are the tab commands' handlers, by wire name.
func tabCommands(s Services) map[string]Command {
	return map[string]Command{
		"tab-list":   tabList(s),
		"tab-new":    tabNew(s),
		"tab-close":  tabClose(s),
		"tab-switch": tabSwitch(s),
	}
}

// tabUsageHint is a tab command's usage line.
func tabUsageHint(cmd string) string {
	switch cmd {
	case "tab-new":
		return "usage: winuse tab new <window> [url]"
	case "tab-close":
		return "usage: winuse tab close <window> <tab>"
	case "tab-switch":
		return "usage: winuse tab switch <window> <tab>"
	}
	return "usage: winuse tab list <window>"
}

// parseTabArgs parses a tab command's arguments.
func parseTabArgs(cmd string, args json.RawMessage, needTab bool) (protocol.TabArgs, *Error) {
	var a protocol.TabArgs
	if err := parseArgs(args, &a); err != nil {
		return a, Fail(cmd+": "+err.Error(), tabUsageHint(cmd))
	}
	if strings.TrimSpace(a.Window) == "" {
		return a, Fail(cmd+": a window reference is required", tabUsageHint(cmd))
	}
	if needTab && strings.TrimSpace(a.Tab) == "" {
		return a, Fail(cmd+": a tab id from winuse tab list is required", tabUsageHint(cmd))
	}
	return a, nil
}

// tabWindow is a resolved browser Window with its Browser Connection and
// its Tabs.
type tabWindow struct {
	browserConn
	tabs page.WindowTabs
}

// tabWindow resolves the Window, its browser and Browser Connection, and
// lists its Tabs. Nothing is sent to the browser but reads.
func (s Services) tabWindow(ctx context.Context, ref string) (tabWindow, *Error) {
	bc, rerr := s.browserWindow(ctx, ref, "Tabs", "the tab commands")
	if rerr != nil {
		return tabWindow{}, rerr
	}
	tabs, err := page.ListWindowTabs(ctx, bc.conn, bc.g)
	if err != nil {
		return tabWindow{}, tabListError(err, bc.w, bc.name)
	}
	return tabWindow{browserConn: bc, tabs: tabs}, nil
}

// tabListError maps a failure to find or list a Window's Tabs.
func tabListError(err error, w win.WindowInfo, name string) *Error {
	id := strconv.FormatUint(w.ID, 10)
	var none *page.NoTabError
	var several *page.AmbiguousTabError
	switch {
	case errors.As(err, &none):
		return Fail(fmt.Sprintf("no Tab of the Browser Connection matches window %d's title and bounds", w.ID),
			"the Window may belong to another profile of the browser; run winuse browser status "+name)
	case errors.As(err, &several):
		return Fail(fmt.Sprintf("%s, so winuse cannot tell which browser window window %d is", several.Error(), w.ID),
			"move one of the Windows a little (winuse move-window "+id+" <x> <y>) and retry")
	case errors.Is(err, context.DeadlineExceeded):
		return Fail(fmt.Sprintf("reading the Tabs of window %d took too long", w.ID), "retry; run winuse browser status "+name+" if it keeps failing")
	}
	if p, ok := browser.AsProblem(err, name); ok {
		return Fail(p.Sentence(), p.Next())
	}
	return Fail(fmt.Sprintf("reading the Tabs of window %d through the Browser Connection failed: %v", w.ID, err),
		"run winuse browser status "+name)
}

// tabRefError maps a tab id that names no Tab of the Window, or several.
func tabRefError(err error, tw tabWindow) *Error {
	return Fail(fmt.Sprintf("%s in window %d", err.Error(), tw.w.ID), "list the Tabs and their ids: winuse tab list "+tw.id())
}

// tabCallError maps a failed CDP call that asked the browser to act. An
// error answer means the browser did not do it; any other failure (the
// connection dropped, the budget ran out) leaves it unknown.
func tabCallError(err error, tw tabWindow) *Error {
	if p, ok := browser.AsProblem(err, tw.name); ok {
		return nothingSent(Fail(p.Sentence(), p.Next()))
	}
	var ce *cdp.Error
	if errors.As(err, &ce) {
		return nothingSent(Fail(fmt.Sprintf("the browser refused: %s", ce.Message), "read winuse tab list "+tw.id()))
	}
	return Fail(err.Error(), "the browser may have done it; read winuse tab list "+tw.id()+" before repeating it")
}

// tabList runs `winuse tab list <window>`: one line per Tab of that
// browser window, `*` for the active one, its short id, title and URL.
func tabList(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		a, e := parseTabArgs("tab-list", args, false)
		if e != nil {
			return "", e
		}
		tw, e := s.tabWindow(ctx, a.Window)
		if e != nil {
			return "", e
		}
		return renderTabs(tw.tabs), nil
	}
}

// renderTabs renders tab list's lines.
func renderTabs(wt page.WindowTabs) string {
	var b strings.Builder
	for _, t := range wt.Tabs {
		mark := " "
		if wt.IsActive(t) {
			mark = "*"
		}
		fmt.Fprintf(&b, "%s %s %q %s\n", mark, page.TabID(t.TargetID), tabTitle(t.Title), t.URL)
	}
	return b.String()
}

// tabTitle is a Tab's title for tab list: whitespace collapsed, at most
// tabTitleWidth characters.
func tabTitle(title string) string {
	title = page.Collapse(title)
	if utf8.RuneCountInString(title) <= tabTitleWidth {
		return title
	}
	r := []rune(title)
	return string(r[:tabTitleWidth-3]) + "..."
}

// tabNew runs `winuse tab new <window> [url]`: a Tab opens in the
// background of that browser window, and its id is printed.
func tabNew(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		a, e := parseTabArgs("tab-new", args, false)
		if e != nil {
			return "", e
		}
		tw, e := s.tabWindow(ctx, a.Window)
		if e != nil {
			return "", nothingSent(e)
		}
		return s.tabHeadless(ctx, tw, nil, "open tab", func(ctx context.Context) (string, *Error, bool) {
			id, err := page.NewTab(ctx, tw.conn, tw.tabs.WindowID, a.URL)
			var other *page.OtherWindowError
			switch {
			case errors.As(err, &other):
				return "", s.tabElsewhere(ctx, tw, id), true
			case err != nil && id == "":
				return "", tabCallError(err, tw), false
			case err != nil:
				return "", Fail(fmt.Sprintf("tab %s opened, and reading which window it is in failed: %v", page.TabID(id), err),
					"read winuse tab list "+tw.id()+" before opening another"), true
			}
			// The read-back found the new Tab in the Window's browser window.
			tw.g.Memory.Remember(tw.conn.Client, tw.g, tw.tabs.WindowID)
			return "ok " + page.TabID(id) + "\n", nil, true
		})
	}
}

// tabElsewhere handles a new Tab that a launched or cloned browser put in
// another of its windows: CDP's Target.createTarget cannot name a window,
// and Chrome uses the window it last had in front. The Tab is closed
// again, so nothing is left behind where the agent did not ask for it.
func (s Services) tabElsewhere(ctx context.Context, tw tabWindow, targetID string) *Error {
	display := browser.DisplayName(tw.name)
	hint := display + " opens a new tab in the window it last had in front; bring window " + tw.id() +
		" to the front first with winuse activate-window " + tw.id() + " (this takes the foreground), then run winuse tab new " + tw.id() + " again"
	if err := page.CloseTab(ctx, tw.conn.Client, targetID); err != nil {
		return Fail(fmt.Sprintf("%s opened the new tab %s in another of its windows, and closing it again failed: %v", display, page.TabID(targetID), err), hint)
	}
	return Fail(fmt.Sprintf("%s opened the new tab in another of its windows, so winuse closed it again", display), hint)
}

// tabClose runs `winuse tab close <window> <tab>`. The Window's last Tab
// is refused: closing it would close the Window, and winuse close-window
// says that explicitly.
func tabClose(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		a, e := parseTabArgs("tab-close", args, true)
		if e != nil {
			return "", e
		}
		tw, e := s.tabWindow(ctx, a.Window)
		if e != nil {
			return "", nothingSent(e)
		}
		tab, err := page.ResolveTab(tw.tabs.Tabs, a.Tab)
		if err != nil {
			return "", nothingSent(tabRefError(err, tw))
		}
		short := page.TabID(tab.TargetID)
		if len(tw.tabs.Tabs) == 1 {
			return "", nothingSent(Fail(fmt.Sprintf("tab %s is the last tab of window %d, and closing it would close the Window", short, tw.w.ID),
				"to close the Window, run winuse close-window "+tw.id()))
		}
		return s.tabHeadless(ctx, tw, &tab, "close tab", func(ctx context.Context) (string, *Error, bool) {
			if err := page.CloseTab(ctx, tw.conn.Client, tab.TargetID); err != nil {
				return "", tabCallError(err, tw), false
			}
			gone := s.pollWindow(ctx, closeWait, func() bool {
				open, err := page.TabOpen(ctx, tw.conn.Client, tab.TargetID)
				return err == nil && !open
			})
			if gone {
				return okOutput, nil, true
			}
			return fmt.Sprintf("ok (the close was requested and tab %s is still open; the page may be asking whether to leave: read winuse tab list %d)\n", short, tw.w.ID), nil, true
		})
	}
}

// tabSwitch runs `winuse tab switch <window> <tab>`: Headless through the
// extension; on a launched or cloned browser Headless through the tab
// strip, or Headed with --headed.
func tabSwitch(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		a, e := parseTabArgs("tab-switch", args, true)
		if e != nil {
			return "", e
		}
		tw, e := s.tabWindow(ctx, a.Window)
		if e != nil {
			return "", nothingSent(e)
		}
		tab, err := page.ResolveTab(tw.tabs.Tabs, a.Tab)
		if err != nil {
			return "", nothingSent(tabRefError(err, tw))
		}
		if tw.tabs.IsActive(tab) {
			return okOutput, nil // already the active Tab: nothing to do
		}
		if tw.conn.Mode == browser.ModeExtension {
			return s.tabSwitchExtension(ctx, tw, tab)
		}
		if a.Headed {
			return s.tabSwitchHeaded(ctx, a.Window, tw, tab)
		}
		return s.tabSwitchStrip(ctx, tw, tab)
	}
}

// TabSelector is the tab strip's side of the Headless switch on a
// launched or cloned browser (headless-max 05): the Window's UIA tab-strip
// items, one chosen by pick among their names and selected through
// SelectionItem.Select, read back through IsSelected. The real one is
// *uia.Client; Services.Actions is type-asserted to it, so a fake without
// it has no Headless switch there.
type TabSelector interface {
	SelectTab(ctx context.Context, hwnd uint64, pick func(names []string) (int, error)) error
}

var _ TabSelector = (*uia.Client)(nil)

// tabStripPickError is a tab-strip pick that found no item, or several,
// named after the Tab: nothing was selected.
type tabStripPickError struct {
	title   string
	matches int
}

func (e *tabStripPickError) Error() string {
	if e.matches == 0 {
		return fmt.Sprintf("no item of the tab strip is named %q", tabTitle(e.title))
	}
	return fmt.Sprintf("%d items of the tab strip are named %q", e.matches, tabTitle(e.title))
}

// tabSwitchStrip is the Headless switch on a launched or cloned browser:
// the refusals every Headless Action makes (a locked desktop, an
// administrator Window, a modal dialog or a minimized Window), then, in a
// passive Esc scope with the Agent Cursor, the Tab's tab-strip item
// matched by its title, selected and read back.
func (s Services) tabSwitchStrip(ctx context.Context, tw tabWindow, tab page.Tab) (string, *Error) {
	sel, ok := s.Actions.(TabSelector)
	if !ok {
		return "", nothingSent(Fail("tab switch is unavailable: no UIA client", tabStripRetry(tw, tab)))
	}
	if rerr := s.headlessRefusals(tw.w, adminPatternHint); rerr != nil {
		return "", rerr
	}
	return s.tabHeadless(ctx, tw, &tab, "switch to", func(ctx context.Context) (string, *Error, bool) {
		err := sel.SelectTab(ctx, tw.w.ID, func(names []string) (int, error) {
			m := stripMatches(names, tab.Title)
			if len(m) != 1 {
				return -1, &tabStripPickError{title: tab.Title, matches: len(m)}
			}
			return m[0], nil
		})
		if err == nil {
			return okOutput, nil, true
		}
		var pick *tabStripPickError
		var verify *uia.VerifyError
		var write *uia.WriteError
		switch {
		case errors.As(err, &pick), errors.Is(err, uia.ErrNoTabStrip):
			return "", nothingSent(Fail(fmt.Sprintf("tab %s cannot be switched to headless: %v", page.TabID(tab.TargetID), err),
				tabStripRetry(tw, tab))), false
		case errors.As(err, &verify):
			return "", tabNotActive(tw, tab, "the switch was delivered"), true
		case errors.As(err, &write):
			return "", Fail(err.Error(), "the switch may have taken effect; read winuse tab list "+tw.id()+" before repeating it"), true
		}
		return "", nothingSent(Fail(fmt.Sprintf("reading the tab strip of window %d failed: %v", tw.w.ID, err), tabStripRetry(tw, tab))), false
	})
}

// tabStripRetry is the --headed retry of a refused Headless switch.
func tabStripRetry(tw tabWindow, tab page.Tab) string {
	return "retry with --headed to switch with real keys (this brings the Window to the foreground): winuse --headed tab switch " +
		tw.id() + " " + page.TabID(tab.TargetID)
}

// tabSwitchExtension is the Headless switch: Winuse.activateTab, then the
// extension's active flag read back.
func (s Services) tabSwitchExtension(ctx context.Context, tw tabWindow, tab page.Tab) (string, *Error) {
	return s.tabHeadless(ctx, tw, &tab, "switch to", func(ctx context.Context) (string, *Error, bool) {
		if err := page.ActivateTabThroughExtension(ctx, tw.conn.Client, tab.TargetID); err != nil {
			return "", tabCallError(err, tw), false
		}
		if !s.pollWindow(ctx, tabVerifyWait, func() bool {
			active, err := page.ExtensionTabActive(ctx, tw.conn.Client, tab.TargetID)
			return err == nil && active
		}) {
			return "", tabNotActive(tw, tab, "the switch was delivered"), true
		}
		return okOutput, nil, true
	})
}

// tabNotActive is the failure of a switch whose read-back did not show
// the Tab active.
func tabNotActive(tw tabWindow, tab page.Tab, what string) *Error {
	return Fail(fmt.Sprintf("tab %s is not the active tab of window %d after the switch", page.TabID(tab.TargetID), tw.w.ID),
		what+"; read winuse tab list "+tw.id()+" before repeating it")
}

// tabHeadless runs one Headless tab Action: a passive Esc scope, the
// Agent Cursor to the Tab's strip item (or the Window), the act, and the
// cursor's click effect once the act was delivered. A stop during the
// cursor's travel sends nothing.
func (s Services) tabHeadless(ctx context.Context, tw tabWindow, tab *page.Tab, verb string, act func(ctx context.Context) (string, *Error, bool)) (string, *Error) {
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()
	rect, shown := s.tabCursor(ctx, tw, tab, verb)
	if esc.Stopped() {
		return "", escStopBeforeActError()
	}
	out, e, delivered := act(ctx)
	if shown && delivered {
		s.cursorClick(ctx, tw.w, rect)
	}
	if esc.Stopped() {
		return "", escStopError()
	}
	return out, e
}

// tabCursor glides the Agent Cursor to the Tab's item in the tab strip
// when the Window's latest Window State shows it, else to the Window.
func (s Services) tabCursor(ctx context.Context, tw tabWindow, tab *page.Tab, verb string) (win.Rect, bool) {
	if s.Cursor == nil {
		return win.Rect{}, false
	}
	label := verb
	var target win.Rect
	if tab != nil {
		label = cursorLabel(verb, tabTitle(tab.Title))
		items := s.tabStrip(ctx, tw.w)
		if pos := stripPosition(items, tab.Title); pos > 0 {
			if b, ok := s.Actions.(elementBounder); ok {
				if l, t, r, bt, _, err := b.ElementBounds(ctx, tw.w.ID, items[pos-1].index); err == nil {
					target = win.Rect{Left: l, Top: t, Right: r, Bottom: bt}
				}
			}
		}
	}
	return s.cursorTravelRect(ctx, tw.w, target, label)
}

// elementBounder reads an Element's live bounds: *uia.Client (a
// HeadlessActor too).
type elementBounder interface {
	ElementBounds(ctx context.Context, hwnd uint64, index int) (left, top, right, bottom int32, name string, err error)
}

// storedElements reads a Window's latest Window State without COM:
// *uia.Client.
type storedElements interface {
	StoredElements(ctx context.Context, hwnd uint64) ([]wstate.Element, bool)
}

// stripItem is one tab item of a browser Window's tab strip in its latest
// Window State.
type stripItem struct {
	index int
	name  string
}

// tabStrip lists the frame's tab items in the Window's latest Window
// State, in tab strip order. Page Elements (a web page's own tabs) are
// left out. Nil when the helper keeps no Window State for the Window.
func (s Services) tabStrip(ctx context.Context, w win.WindowInfo) []stripItem {
	r, ok := s.Actions.(storedElements)
	if !ok {
		return nil
	}
	els, ok := r.StoredElements(ctx, w.ID)
	if !ok {
		return nil
	}
	splicer, _ := s.Actions.(PageSplicer)
	var items []stripItem
	for i, el := range els {
		if el.Role != "tab" {
			continue
		}
		if splicer != nil {
			if o, err := splicer.ElementOrigin(ctx, w.ID, i); err != nil || o != nil {
				continue
			}
		}
		items = append(items, stripItem{index: i, name: el.Name})
	}
	return items
}

// stripPosition is the 1-based position of the one strip item named after
// a Tab's title, or 0 when none or several are. Chrome may add a state
// after the title ("- Audio playing").
func stripPosition(items []stripItem, title string) int {
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.name
	}
	if m := stripMatches(names, title); len(m) == 1 {
		return m[0] + 1
	}
	return 0
}

// stripMatches are the 0-based positions of the strip items named after a
// Tab's title: the title itself, or the title and a state Chrome adds
// after it (" - Memory usage - 17.3 MB", ", Audio playing"). None for an
// empty title.
func stripMatches(names []string, title string) []int {
	title = page.Collapse(title)
	if title == "" {
		return nil
	}
	var m []int
	for i, n := range names {
		name := page.Collapse(n)
		if name == title || strings.HasPrefix(name, title+" ") || strings.HasPrefix(name, title+",") {
			m = append(m, i)
		}
	}
	return m
}

// tabKeys is how the Headed switch presses its way to a Tab.
type tabKeys struct {
	direct string // Control_L+<n>: the Tab's position is known and 1 to 8, or it is the last Tab
	back   bool   // step with Ctrl+Shift+Tab: the Tab is nearer to the left
}

// planTabKeys reads the Tab's position from the Window's latest Window
// State, trusted only when its tab strip holds as many items as the
// browser window has Tabs.
func planTabKeys(items []stripItem, wt page.WindowTabs, tab page.Tab) tabKeys {
	n := len(items)
	if n == 0 || n != len(wt.Tabs) {
		return tabKeys{}
	}
	pos := stripPosition(items, tab.Title)
	if pos == 0 {
		return tabKeys{}
	}
	var k tabKeys
	switch {
	case pos <= 8:
		k.direct = "Control_L+" + strconv.Itoa(pos)
	case pos == n:
		k.direct = "Control_L+9"
	}
	if cur := stripPosition(items, wt.Active.Title); cur != 0 {
		forward := (pos - cur + n) % n
		k.back = forward > n-forward
	}
	return k
}

// tabSwitchHeaded is the switch on a launched, cloned or toggled browser:
// runAction brings the Window to the foreground under the banner, inside
// a swallowing Esc scope; then Ctrl+<n> when the Tab's position is known,
// else Ctrl+Tab steps, each checked by reading the Tab's
// document.visibilityState over CDP. Everything before runAction sends
// nothing.
func (s Services) tabSwitchHeaded(ctx context.Context, ref string, tw tabWindow, tab page.Tab) (string, *Error) {
	plan := planTabKeys(s.tabStrip(ctx, tw.w), tw.tabs, tab)
	vis := page.NewVisibility(tw.conn)
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		vis.Close(dctx)
	}()
	return s.runAction(ctx, ref, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		shows := func(wait time.Duration) bool {
			return s.pollWindow(ctx, wait, func() bool {
				v, err := vis.Visible(ctx, tab.TargetID)
				return err == nil && v
			})
		}
		press := func(chord string) *Error {
			c, err := keys.Parse(chord)
			if err == nil {
				err = s.PressKeys(c)
			}
			if err != nil {
				return Fail(fmt.Sprintf("pressing %s in window %d failed: %v", chord, w.ID, err),
					"read winuse tab list "+tw.id()+" before repeating the switch")
			}
			return nil
		}
		if plan.direct != "" {
			if e := press(plan.direct); e != nil {
				return "", e
			}
			if shows(tabVerifyWait) {
				return okOutput, nil
			}
		}
		step := "Control_L+Tab"
		if plan.back {
			step = "Control_L+Shift_L+Tab"
		}
		for i := 0; i < len(tw.tabs.Tabs) && ctx.Err() == nil; i++ {
			if s.Esc.Stopped() {
				return "", escStopError()
			}
			if e := press(step); e != nil {
				return "", e
			}
			if shows(tabStepWait) {
				return okOutput, nil
			}
		}
		if shows(tabVerifyWait) {
			return okOutput, nil
		}
		return "", tabNotActive(tw, tab, "keys were sent to the Window")
	})
}

// tabStepHeadless is the Batch's scope question for a tab step: tab-new
// and tab-close are Headless; tab-switch is Headless unless it is headed
// (a Headless switch on a launched or cloned browser goes through the tab
// strip or sends nothing, headless-max 05), and a headed one is Headless
// only when the Window's Browser Connection goes through the extension.
// Anything unknown counts as Headed, the safe side (a swallowing scope and
// the banner).
func (s Services) tabStepHeadless(kind string, args json.RawMessage, w *win.WindowInfo) bool {
	switch kind {
	case "tab-new", "tab-close":
		return true
	case "tab-switch":
	default:
		return false
	}
	var a protocol.TabArgs
	if len(args) > 0 && json.Unmarshal(args, &a) != nil {
		return false
	}
	if _, ok := s.Actions.(TabSelector); ok && !a.Headed {
		return true
	}
	if w == nil {
		return false
	}
	lp := s.live()
	if lp == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), pageProbeBudget)
	defer cancel()
	name, conn, err := lp.open(ctx, *w)
	return name != "" && err == nil && conn.Mode == browser.ModeExtension
}
