package helper

// Tab targets (background-tabs, ADR 0017): a Page command names a Tab of a
// browser Window as <window>:<tab>, the Window as everywhere and a tab id
// from tab list or tab new, and acts on that Tab whether or not its window
// shows it. The user keeps the Tab they look at; agents work in theirs.
//
//   - tabTargeted wraps every command: a Tab target is resolved over the
//     Window's Browser Connection (any window of that browser), held
//     (page.HoldTab: focus emulation, an active lifecycle, and through the
//     extension no discarding and the guard on the Tabs it opens), and put
//     in the request's context; the command then runs on the Window part.
//     A command with no Page route, --headed, --post, @x,y and the partial
//     reads are refused first, with nothing sent.
//   - get-window-state reads the Tab over CDP alone (tabWindowState): a
//     document Element 0 named after the Tab and the Page under it. Its
//     Element Indexes are this Agent's for this Tab (tabStateStore), apart
//     from the Window's own Window State.
//   - The Page routes ask tabTargetOf at their choke points: pageResolve
//     (the Tab's store, never UI Automation), pageTabCheck (the Tab, not the
//     Window's active one), pageWindow, pageKeys, pageDoc, the Agent Cursor
//     (none while the Tab is in the background), and lanesFor (a lane per
//     Tab).
//   - Failures and Note lines name the Tab: "window 301" becomes "tab
//     ab12cd34 of window 301", and a hint's "winuse <command> 301" becomes
//     "winuse <command> 301:ab12cd34" (retarget).
//
// A hold ends tabHoldIdle after the Agent's last command on the Tab
// (page.ReleaseTab); the extension also ends it when its debugger detaches.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

const (
	// tabHoldIdle is how long a Tab stays held after the last command on it:
	// the extension's own idle detach (IDLE_DETACH_MS).
	tabHoldIdle = 5 * time.Minute
	// maxTabStates caps the Tab Window States one Agent keeps; the oldest
	// goes first.
	maxTabStates = 32
)

// tabShotWait bounds a Tab target's screenshot: a hidden Tab's capture can
// hang (ADR 0017), and the tree is answered without it then. Tests shrink
// it.
var tabShotWait = 3 * time.Second

// tabRefPattern is a Tab target: the Window part, a colon, and a tab id of
// 4 to 32 letters or digits.
var tabRefPattern = regexp.MustCompile(`^(.+):([0-9A-Za-z]{4,32})$`)

// splitTabRef splits a Tab target into its Window and tab id parts; ok is
// false for a plain Window reference.
func splitTabRef(ref string) (window, tab string, ok bool) {
	m := tabRefPattern.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil || strings.TrimSpace(m[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(m[1]), m[2], true
}

// tabTargetCommands are the commands a Tab target takes: the Page's own
// reads and Actions. Every one of them reaches the Tab over CDP alone.
var tabTargetCommands = map[string]bool{
	"get-window-state": true,
	"click":            true, "set-value": true, "scroll": true, "hover": true, "drag": true,
	"select-text": true, "type-text": true, "press-key": true, "upload": true,
	"navigate": true, "back": true, "forward": true, "reload": true,
	"dialog": true, "wait": true, "eval": true,
	"batch": true,
}

// tabTargetsWord names them in a refusal's hint.
const tabTargetsWord = "get-window-state, click, set-value, scroll, hover, drag, select-text, type-text, press-key, upload, navigate, back, forward, reload, dialog, wait, eval and batch"

// pageWaitKinds are the waits a Tab target takes: the Page's.
var pageWaitKinds = map[string]bool{page.WaitLoadKind: true, page.WaitURL: true, page.WaitDownload: true, "selector": true, "text": true}

// bgTab is the Tab a command names as <window>:<tab>, resolved and held
// before the command runs.
type bgTab struct {
	browserConn
	tab     page.Tab
	session string // the Tab's attached session; "" for a browser page
	// active says the Tab is the one its window shows (the extension's
	// chrome.tabs flag): only then does the Agent Cursor show its Actions.
	active bool
}

// short is the Tab's id as tab list prints it.
func (bt *bgTab) short() string { return page.TabID(bt.tab.TargetID) }

// ref is the Tab target in canonical form, for hints: 301:ab12cd34.
func (bt *bgTab) ref() string { return bt.id() + ":" + bt.short() }

// docOrigin is the Tab's document: what keys and the Page-wide checks run
// on.
func (bt *bgTab) docOrigin() *page.Origin {
	return &page.Origin{Browser: bt.name, Client: bt.conn.Client, TargetID: bt.tab.TargetID, SessionID: bt.session}
}

type tabTargetKey struct{}

func withTabTarget(ctx context.Context, bt *bgTab) context.Context {
	return context.WithValue(ctx, tabTargetKey{}, bt)
}

// tabTargetOf is the request's Tab target, or nil when it names a Window.
func tabTargetOf(ctx context.Context) *bgTab {
	bt, _ := ctx.Value(tabTargetKey{}).(*bgTab)
	return bt
}

// tabTargeted is the registry's outermost wrapper: a command whose window
// argument is a Tab target runs on the Tab (see the file comment); any
// other runs as before. The tab commands take the Window part alone.
func (s Services) tabTargeted(name string, cmd Command) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a struct {
			Window string `json:"window"`
		}
		if len(args) == 0 || json.Unmarshal(args, &a) != nil {
			return cmd(ctx, args)
		}
		window, tabRef, ok := splitTabRef(a.Window)
		if !ok {
			return cmd(ctx, args)
		}
		if first, _, twice := strings.Cut(window, ":"); twice {
			// 301:301:ab12cd34: the Window named twice. Refused rather than
			// guessed, so a typo never lands in another Window or Tab.
			return "", nothingSent(Fail(fmt.Sprintf("%s names more than one Window: a Tab target is one Window and one tab id, <window>:<tab>", a.Window),
				"run it on "+strings.TrimSpace(first)+":"+tabRef))
		}
		plain, err := withWindow(window, args)
		if err != nil {
			return cmd(ctx, args)
		}
		if strings.HasPrefix(name, "tab-") {
			return cmd(ctx, plain) // the tab commands act on the Tabs of the Window
		}
		if rerr := refuseOnTab(name, args, a.Window); rerr != nil {
			return "", rerr
		}
		bt, rerr := s.resolveTabTarget(ctx, window, tabRef)
		if rerr != nil {
			if name == "get-window-state" {
				return "", rerr
			}
			return "", nothingSent(rerr)
		}
		ctx = withTabTarget(ctx, bt)
		var out string
		var e *Error
		if name == "get-window-state" {
			// Worded for the Tab already, and the rest is the Page's own text.
			return s.tabWindowState(ctx, bt, plain)
		}
		out, e = cmd(ctx, plain)
		return bt.retargetOut(out), bt.retargetErr(e)
	}
}

// refuseOnTab refuses, before anything is resolved or sent, what a Tab
// target cannot take: a command with no Page route, and the forms that
// need the screen or the foreground (--headed, --post, @x,y points, the
// partial reads). A Batch is checked step by step.
func refuseOnTab(name string, args json.RawMessage, ref string) *Error {
	hint := "a Tab target (<window>:<tab>) takes " + tabTargetsWord + "; run the command on the Window to act on the Tab it shows"
	if !tabTargetCommands[name] {
		return nothingSent(Fail(fmt.Sprintf("%s does not take a Tab target (%s): it needs the Window on the screen", name, ref), hint))
	}
	if name == "batch" {
		var b protocol.BatchArgs
		if err := json.Unmarshal(args, &b); err != nil {
			return nil // the Batch reports its own argument failure
		}
		if b.Headed {
			return tabHeadedRefusal(ref)
		}
		for i, st := range b.Steps {
			if st.Window != "" || st.InNew {
				return nothingSent(Fail(fmt.Sprintf("Action %d of a Batch on a Tab target names its own Window; every Action of it acts in %s", i+1, ref),
					"run the Actions for another Window or Tab in a Batch of their own"))
			}
			if st.Kind == "batch" || st.Kind == "get-window-state" || !tabTargetCommands[st.Kind] {
				return nothingSent(Fail(fmt.Sprintf("Action %d (%s) of a Batch on a Tab target has no Page route", i+1, st.Kind), hint))
			}
			if rerr := refuseOnTab(st.Kind, st.Args, ref); rerr != nil {
				return rerr
			}
		}
		return nil
	}
	var f struct {
		Headed       bool            `json:"headed"`
		Post         bool            `json:"post"`
		Point        *protocol.Point `json:"point"`
		ElementIndex *int            `json:"element_index"`
		Ref          string          `json:"ref"`
		FromIndex    *int            `json:"from_index"`
		FromRef      string          `json:"from_ref"`
		ToIndex      *int            `json:"to_index"`
		ToRef        string          `json:"to_ref"`
		Root         string          `json:"root"`
		Skeleton     bool            `json:"skeleton"`
		Depth        int             `json:"depth"`
		Interactive  bool            `json:"interactive"`
		For          string          `json:"for"`
	}
	json.Unmarshal(args, &f)
	point := func() *Error {
		return nothingSent(Fail(fmt.Sprintf("%s at an @x,y point does not take a Tab target (%s): a background Tab has no place on the screen", name, ref),
			"name an Element of the Tab's Window State instead: winuse get-window-state "+ref))
	}
	switch {
	case f.Headed:
		return tabHeadedRefusal(ref)
	case f.Post:
		return nothingSent(Fail(fmt.Sprintf("--post does not reach a Tab target (%s): it posts to the Window's own controls", ref),
			"drop --post: "+name+" reaches the Tab through its Browser Connection"))
	case f.Point != nil:
		return point()
	case name == "get-window-state" && (f.Root != "" || f.Skeleton):
		return Fail("get-window-state reads a Tab target whole: --root and --skeleton read UI Automation, which shows only the Tab a Window shows",
			"run winuse get-window-state "+ref+"; --interactive, --depth, --compact, --max-chars and --out shape what it prints")
	case (name == "scroll" || name == "hover") && f.ElementIndex == nil && f.Ref == "":
		return point()
	case name == "drag" && (f.FromIndex == nil && f.FromRef == "" || f.ToIndex == nil && f.ToRef == ""):
		return point()
	case name == "wait" && !pageWaitKinds[strings.ToLower(strings.TrimSpace(f.For))]:
		return nothingSent(Fail(fmt.Sprintf("wait on a Tab target (%s) waits for the Page: selector, text, url, load or download", ref),
			"wait for an Element's state by reading the Tab again: winuse get-window-state "+ref))
	}
	return nil
}

// tabHeadedRefusal refuses --headed on a Tab target: real input would need
// the Tab shown and its window in front, the user's Tab and foreground.
func tabHeadedRefusal(ref string) *Error {
	return nothingSent(Fail(fmt.Sprintf("--headed does not reach a Tab target (%s): real input needs the Tab shown and its Window in front, which would take the user's Tab and foreground", ref),
		"drop --headed: every Action on a Tab target is headless through its Browser Connection; if it truly needs real input, ask the user, then run winuse tab switch and the command on the Window"))
}

// resolveTabTarget finds the Tab a Tab target names among the Tabs of the
// Window's browser (any of its windows), attaches its session and holds
// it. Nothing reaches the Page's input.
func (s Services) resolveTabTarget(ctx context.Context, window, tabRef string) (*bgTab, *Error) {
	bc, rerr := s.browserWindow(ctx, window, "Tabs", "Tab targets")
	if rerr != nil {
		return nil, rerr
	}
	tabs, err := page.ListTabs(ctx, bc.conn.Client)
	if err != nil {
		return nil, tabListError(err, bc.w, bc.name)
	}
	tab, err := page.ResolveTab(tabs, tabRef)
	if err != nil {
		code := protocol.CodeNotFound
		var re *page.TabRefError
		switch {
		case errors.As(err, &re) && re.Short:
			code = protocol.CodeUsage
		case re != nil && len(re.Matches) > 1:
			code = protocol.CodeAmbiguousTarget
		}
		return nil, &Error{Msg: fmt.Sprintf("%s in the %s of window %d", err.Error(), browser.DisplayName(bc.name), bc.w.ID),
			Hint: "list the Tabs and their ids: winuse tab list " + bc.id(), Exit: 1, Code: code}
	}
	bt := &bgTab{browserConn: bc, tab: tab, active: tab.Active != nil && *tab.Active}
	if tab.Internal {
		return bt, nil // a browser page is never attached; navigate leaves it
	}
	session, err := bc.reader.TabSession(ctx, bc.conn, tab.TargetID)
	if err != nil {
		if p, ok := browser.AsProblem(err, bc.name); ok {
			return nil, Fail(p.Sentence(), p.Next())
		}
		return nil, Fail(fmt.Sprintf("attaching to tab %s of window %d failed: %v", page.TabID(tab.TargetID), bc.w.ID, err),
			"retry; run winuse browser status "+bc.name+" if it keeps failing")
	}
	bt.session = session
	s.tabHolds.hold(ctx, bc.conn, session, tab.TargetID)
	return bt, nil
}

// retargetHint is a command in a hint, with the reference after it.
var retargetHint = regexp.MustCompile(`winuse ((?:--headed )?[a-z][a-z-]*) ([^\s]+)`)

// retarget rewrites a failure's or note's text for a Tab target: the
// Window's phrases name the Tab, and a hint's command runs on the Tab.
func (bt *bgTab) retarget(s string) string {
	if s == "" {
		return s
	}
	id := bt.id()
	const mark = "\x00tab\x00"
	for _, p := range []string{"the active Tab of window " + id, "the Tab of window " + id, "tab " + bt.short() + " of window " + id} {
		s = strings.ReplaceAll(s, p, mark)
	}
	s = regexp.MustCompile(`\bwindow `+regexp.QuoteMeta(id)+`\b`).ReplaceAllString(s, mark)
	s = strings.ReplaceAll(s, mark, "tab "+bt.short()+" of window "+id)
	return retargetHint.ReplaceAllStringFunc(s, func(m string) string {
		sub := retargetHint.FindStringSubmatch(m)
		if sub[1] == "tab" || sub[1] == "browser" || strings.TrimRight(sub[2], ".,;)") != id {
			return m
		}
		return "winuse " + sub[1] + " " + strings.Replace(sub[2], id, bt.ref(), 1)
	})
}

// retargetErr is retarget on a failure.
func (bt *bgTab) retargetErr(e *Error) *Error {
	if e == nil {
		return nil
	}
	out := *e
	out.Msg, out.Hint = bt.retarget(e.Msg), bt.retarget(e.Hint)
	return &out
}

// retargetOut is retarget on an output's Note lines and a Batch's step
// lines; any other line is the Page's (an eval's value) and stays as is.
func (bt *bgTab) retargetOut(out string) string {
	lines := strings.SplitAfter(out, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, notePrefix) || strings.HasPrefix(l, "note: ") || strings.HasPrefix(l, "Action ") {
			lines[i] = bt.retarget(l)
		}
	}
	return strings.Join(lines, "")
}

// ---- the user's Tab ----

// How long, and how often, winuse watches the user's window after an
// Action in a background Tab for a new Tab that took the user's place.
// Tests shrink them.
var (
	usersTabWatch = 3 * time.Second
	usersTabPoll  = 150 * time.Millisecond
)

// guardUsersTab is winuse's own check behind the extension's guard (ADR
// 0017 Decision 4), which a link with no opener or an extension that lost
// its holds can slip past (the live check: a target=_blank link's Tab
// stayed selected). Before an Action's input it notes the Tab the user's
// window shows and every open Tab; the returned func, run once the input
// was sent, watches that window for usersTabWatch: a new Tab showing a
// site that became the selected one while the user's Tab is still open is
// the Action's, so the user's Tab is selected again (chrome.tabs inside
// the window, never the window itself) and the new Tab joins the agent
// Tab's group. A Tab the user switched to themselves (one that was open
// before, or a browser page such as Ctrl+T's) ends the watch. Through the
// extension only: nothing else can select a Tab without the foreground.
func (s Services) guardUsersTab(ctx context.Context) func() {
	bt := tabTargetOf(ctx)
	if bt == nil || bt.active || bt.conn.Mode != browser.ModeExtension {
		return func() {}
	}
	lctx, cancel := context.WithTimeout(ctx, pageProbeBudget)
	tabs, err := page.ListTabs(lctx, bt.conn.Client)
	cancel()
	if err != nil {
		return func() {}
	}
	window, users := -1, ""
	known := map[string]bool{}
	for _, t := range tabs {
		known[t.TargetID] = true
		if t.TargetID == bt.tab.TargetID {
			window = t.WindowID
		}
	}
	for _, t := range tabs {
		if t.WindowID == window && t.Active != nil && *t.Active {
			users = t.TargetID
		}
	}
	if users == "" || users == bt.tab.TargetID {
		return func() {}
	}
	c, agentTab := bt.conn.Client, bt.tab.TargetID
	return func() {
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), usersTabWatch)
			defer cancel()
			for {
				select {
				case <-wctx.Done():
					return
				case <-time.After(usersTabPoll):
				}
				now, err := page.ListTabs(wctx, c)
				if err != nil {
					continue
				}
				var shown page.Tab
				open := false
				for _, t := range now {
					open = open || t.TargetID == users
					if t.WindowID == window && t.Active != nil && *t.Active {
						shown = t
					}
				}
				switch {
				case !open:
					return // the user closed their Tab
				case shown.TargetID == "" || shown.TargetID == users:
					continue
				case known[shown.TargetID] || shown.Internal:
					return // the user moved to another Tab themselves
				}
				if err := page.ActivateTabThroughExtension(wctx, c, users); err != nil {
					log.Printf("selecting the user's tab %s again: %v", page.TabID(users), err)
					return
				}
				c.Call(wctx, bridge.MethodAdoptTab, map[string]any{"targetId": shown.TargetID, "openerId": agentTab})
				log.Printf("tab %s, opened by an Action in tab %s, took the user's place; selected the user's tab %s again",
					page.TabID(shown.TargetID), page.TabID(agentTab), page.TabID(users))
				return
			}
		}()
	}
}

// ---- holds ----

// tabHolds are the Tabs the helper holds (ADR 0017), each released
// tabHoldIdle after its last command.
type tabHolds struct {
	mu   sync.Mutex
	held map[holdKey]*tabHold
	idle time.Duration // 0: tabHoldIdle
}

type holdKey struct {
	c      *cdp.Client
	target string
}

type tabHold struct {
	conn    *browser.Connection
	session string
	gen     int // the hold its timer belongs to
	timer   *time.Timer
}

func newTabHolds() *tabHolds { return &tabHolds{held: map[holdKey]*tabHold{}} }

// hold holds a Tab, or renews its hold: the lifecycle and focus calls go
// again (a release turned focus emulation off; Energy Saver may have frozen
// the Tab), and the release moves to tabHoldIdle from now.
func (h *tabHolds) hold(ctx context.Context, conn *browser.Connection, session, target string) {
	if h == nil {
		return
	}
	hctx, cancel := context.WithTimeout(ctx, time.Second)
	if err := page.HoldTab(hctx, conn, session, target); err != nil {
		log.Printf("holding tab %s: %v", page.TabID(target), err)
	}
	cancel()
	idle := h.idle
	if idle <= 0 {
		idle = tabHoldIdle
	}
	k := holdKey{conn.Client, target}
	h.mu.Lock()
	defer h.mu.Unlock()
	x := h.held[k]
	if x == nil {
		x = &tabHold{}
		h.held[k] = x
	} else {
		x.timer.Stop()
	}
	x.conn, x.session = conn, session
	x.gen++
	gen := x.gen
	x.timer = time.AfterFunc(idle, func() { h.release(k, gen) })
}

// release ends the hold gen of a Tab, unless a later command renewed it.
func (h *tabHolds) release(k holdKey, gen int) {
	h.mu.Lock()
	x := h.held[k]
	if x == nil || x.gen != gen {
		h.mu.Unlock()
		return
	}
	delete(h.held, k)
	h.mu.Unlock()
	if !k.c.Alive() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := page.ReleaseTab(ctx, x.conn, x.session, k.target); err != nil {
		log.Printf("releasing tab %s: %v", page.TabID(k.target), err)
	}
}

// heldTabs lists the target ids held now (tests).
func (h *tabHolds) heldTabs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for k := range h.held {
		out = append(out, k.target)
	}
	return out
}

// ---- Window States of Tab targets ----

// tabState is one Agent's Window State of one Tab: its Elements and, per
// Element Index, the Page node behind it (nil for the document Element 0).
type tabState struct {
	elements []wstate.Element
	origins  []*page.Origin
	used     time.Time
}

// tabStateStore keeps each Agent's Tab Window States, apart from the
// Windows' own (ADR 0017: Element Indexes per Agent per Tab).
type tabStateStore struct {
	mu     sync.Mutex
	states map[string]map[holdKey]*tabState // by Agent ID
}

func newTabStateStore() *tabStateStore {
	return &tabStateStore{states: map[string]map[holdKey]*tabState{}}
}

func (st *tabStateStore) put(agentID string, k holdKey, s *tabState) {
	st.mu.Lock()
	defer st.mu.Unlock()
	m := st.states[agentID]
	if m == nil {
		m = map[holdKey]*tabState{}
		st.states[agentID] = m
	}
	s.used = time.Now()
	m[k] = s
	for key := range m {
		if !key.c.Alive() {
			delete(m, key) // its Browser Connection ended
		}
	}
	for len(m) > maxTabStates {
		var oldest holdKey
		var at time.Time
		for key, old := range m {
			if at.IsZero() || old.used.Before(at) {
				oldest, at = key, old.used
			}
		}
		delete(m, oldest)
	}
}

func (st *tabStateStore) get(agentID string, k holdKey) (*tabState, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.states[agentID][k]
	if ok {
		s.used = time.Now()
	}
	return s, ok
}

// tabKeyOf is a Tab target's key in the stores.
func tabKeyOf(bt *bgTab) holdKey { return holdKey{bt.conn.Client, bt.tab.TargetID} }

// tabResolve is pageResolve for a Tab target: the Element the target names
// in this Agent's Window State of the Tab, never in UI Automation. Every
// failure is a refusal with nothing sent, so no route of the Window runs.
func (s Services) tabResolve(ctx context.Context, bt *bgTab, t elementTarget) (win.WindowInfo, int, *page.Origin, *Error) {
	w := bt.w
	if t.point != nil {
		return w, 0, nil, nothingSent(Fail("an @x,y point does not take a Tab target: a background Tab has no place on the screen", "name an Element of the Tab's Window State instead"))
	}
	st, ok := s.tabStates.get(AgentOf(ctx).ID, tabKeyOf(bt))
	if !ok {
		return w, 0, nil, nothingSent(Fail(fmt.Sprintf("no cached Window State is available for tab %s of window %d", bt.short(), w.ID),
			"run winuse get-window-state "+bt.ref()+" to read the Tab's Elements before acting on them"))
	}
	index := -1
	switch {
	case t.ref != "":
		i, err := wstate.ResolveElementRef(st.elements, t.ref)
		if err != nil {
			return w, 0, nil, nothingSent(refError(err, w))
		}
		index = i
	case t.index != nil:
		index = *t.index
	}
	if index < 0 || index >= len(st.elements) {
		return w, index, nil, nothingSent(elementError(uia.ErrStale, w, index))
	}
	o := st.origins[index]
	if o == nil {
		return w, index, nil, nothingSent(Fail(fmt.Sprintf("element %d is the document of tab %s itself", index, bt.short()),
			"act on an Element inside it; keys and typing go to the Tab's focused element: winuse press-key "+bt.ref()+" <key>"))
	}
	return w, index, o, nil
}

// tabElementName is an Element's name in the Tab's Window State.
func (s Services) tabElementName(ctx context.Context, bt *bgTab, index int) string {
	if st, ok := s.tabStates.get(AgentOf(ctx).ID, tabKeyOf(bt)); ok && index >= 0 && index < len(st.elements) {
		return st.elements[index].Name
	}
	return ""
}

// tabWindowState answers get-window-state on a Tab target: the Tab's Page
// read over CDP alone under a document Element 0, kept as this Agent's
// Window State of the Tab, and with --screenshot a capture of the Tab,
// bounded (tabShotWait) and replaced by a note when it does not come.
func (s Services) tabWindowState(ctx context.Context, bt *bgTab, args json.RawMessage) (string, *Error) {
	var a windowRef
	if err := parseArgs(args, &a); err != nil {
		return "", Fail("get-window-state: "+err.Error(), windowStateUsage)
	}
	if e := budgetError(a); e != nil {
		return "", e
	}
	if bt.tab.Internal {
		return "", Fail(fmt.Sprintf("tab %s of window %d shows a browser page (%s), which winuse does not read or act in", bt.short(), bt.w.ID, bt.tab.URL),
			"run winuse navigate "+bt.ref()+" <url> to open a site")
	}
	if now, err := page.TabNow(ctx, bt.conn.Client, bt.tab.TargetID); err == nil {
		bt.tab.Title, bt.tab.URL = now.Title, now.URL
	}
	raw, err := bt.reader.ReadTab(ctx, bt.conn, bt.tab)
	if err != nil {
		return "", tabReadError(err, bt)
	}
	res := page.Build(raw, page.Limits{
		MaxNodes: page.MaxNodes - 1, MaxDepth: page.MaxDepth - 1, MaxChildren: page.MaxChildren,
		NodeLabel: page.MaxNodes, DepthLabel: page.MaxDepth,
	})
	doc := wstate.Element{Depth: 0, Role: "document", Name: page.Collapse(raw.Title), Value: raw.URL, Truncated: res.DocTruncated}
	els := append(make([]wstate.Element, 0, len(res.Elements)+1), doc)
	origins := append(make([]*page.Origin, 0, len(res.Elements)+1), nil)
	for i, e := range res.Elements {
		e.Depth++
		els = append(els, e)
		o := res.Origins[i]
		o.DocIndex = 0
		origins = append(origins, &o)
	}
	tree := &wstate.Tree{Title: page.Collapse(raw.Title), App: win.AppExeName(bt.w), Elements: els, FocusedIndex: -1}
	switch {
	case res.Focused >= 0:
		tree.FocusedIndex = res.Focused + 1
	case res.RootFocused:
		tree.FocusedIndex = 0
	}
	if len(res.Reasons) > 0 {
		tree.Notes = append(tree.Notes, "this Window State is partial: "+strings.Join(res.Reasons, "; "))
	}
	tree.Notes = append(tree.Notes, bt.stateNote())
	s.tabStates.put(AgentOf(ctx).ID, tabKeyOf(bt), &tabState{elements: els, origins: origins})
	out, rerr := renderState(tree, a, "") // no --root hint: a Tab target takes none
	if rerr != nil {
		return "", rerr
	}
	if a.Screenshot {
		out += s.tabScreenshot(ctx, bt)
	}
	return out, nil
}

// stateNote is the note every Tab target's Window State ends with.
func (bt *bgTab) stateNote() string {
	where := "in the background of"
	if bt.active {
		where = "the Tab shown in"
	}
	n := fmt.Sprintf("this is tab %s, %s window %d, read through its Browser Connection alone: act on it as %s; its Element Indexes are not the Window's",
		bt.short(), where, bt.w.ID, bt.ref())
	if bt.conn.Mode != browser.ModeExtension {
		n += "; without the winuse extension, a link that opens a new Tab may show it in its window"
	}
	return n
}

// tabReadError maps a failed read of a Tab target.
func tabReadError(err error, bt *bgTab) *Error {
	var open *page.DialogOpenError
	var quiet *page.NotAnsweringError
	switch {
	case errors.As(err, &open):
		return Fail(fmt.Sprintf("tab %s of window %d shows %s, and its Page answers nothing until it is answered", bt.short(), bt.w.ID, open.Dialog.Describe()),
			dialogHint(bt.ref()))
	case errors.As(err, &quiet):
		return Fail(fmt.Sprintf("tab %s of window %d did not answer within %s: it most likely shows a JavaScript dialog, or a script keeps it busy", bt.short(), bt.w.ID, quiet.After),
			dialogHint(bt.ref())+"; if it shows no dialog, retry in a moment")
	case errors.Is(err, context.DeadlineExceeded):
		return Fail(fmt.Sprintf("reading tab %s of window %d took too long", bt.short(), bt.w.ID),
			"retry with a longer budget: winuse get-window-state "+bt.ref()+" --timeout 30")
	}
	if p, ok := browser.AsProblem(err, bt.name); ok {
		return Fail(p.Sentence(), p.Next())
	}
	return Fail(fmt.Sprintf("reading tab %s of window %d failed: %v", bt.short(), bt.w.ID, err),
		"retry; run winuse browser status "+bt.name+" if it keeps failing")
}

// captureTab is page.CaptureTab, a seam for the tests.
var captureTab = page.CaptureTab

// tabScreenshot captures a Tab target within tabShotWait and returns its
// Screenshot line, or a Note line saying why there is none: the tree above
// it stands either way. @x,y points do not bind to it.
func (s Services) tabScreenshot(ctx context.Context, bt *bgTab) string {
	cctx, cancel := context.WithTimeout(ctx, tabShotWait)
	defer cancel()
	data, err := captureTab(cctx, bt.conn, bt.session, jpegQuality)
	if err != nil {
		why := err.Error()
		if errors.Is(err, context.DeadlineExceeded) || cctx.Err() != nil {
			why = fmt.Sprintf("the browser gave no frame within %s, as a hidden Tab sometimes does", tabShotWait)
		}
		return notePrefix + "no screenshot of tab " + bt.short() + ": " + why + "; the tree above is current\n"
	}
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return notePrefix + "no screenshot of tab " + bt.short() + ": the browser's image did not decode (" + err.Error() + "); the tree above is current\n"
	}
	b := decoded.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), decoded, b.Min, draw.Src)
	scale := shot.FitScale(b.Dx(), b.Dy())
	scaled := shot.ScaleImage(img, scale)
	path, err := saveShot(scaled, bt.w.ID, "tab-"+bt.short(), "jpg", func(wtr io.Writer) error {
		return jpeg.Encode(wtr, scaled, &jpeg.Options{Quality: jpegQuality})
	})
	if err != nil {
		return notePrefix + "no screenshot of tab " + bt.short() + ": saving it failed: " + err.Error() + "\n"
	}
	sb := scaled.Bounds()
	return fmt.Sprintf("Screenshot: %s (%dx%d px, scale %s, tab %s; @x,y points do not apply to a Tab target)\n",
		path, sb.Dx(), sb.Dy(), formatScale(scale), bt.short())
}
