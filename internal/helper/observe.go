package helper

// --observe (speed observe, ADR 0021): an Action or a Batch that prints the
// Window State of its Window after it ran, so the agent sees the effect in
// the same call instead of spending a turn on get-window-state.
//
//	winuse <action> ... --observe[=diff|full] [--interactive] [--depth N] [--compact] [--max-chars N | --out <file>] [--full]
//	winuse batch <window> --observe[=diff|full] [...] <action> ... [then ...]
//
// The read after the Action is a full get-window-state of the Action's
// Window (or Tab target): it replaces the Agent's Window State and its
// Element Indexes, as any full read does. full prints it whole; diff
// prints what changed by Element Index since the Agent's latest Window
// State of that Window (wstate.RenderChanges): the baseline is exactly the
// Elements whose indexes the Agent could still use before the read, the
// full read's and those find or --root added after it. An index the diff
// does not list names an Element with the same line as before; one listed
// gone names nothing now, and an Action on it is refused as stale with
// nothing sent. Past wstate.DiffThreshold, or with no baseline, the whole
// Window State prints with a note.
//
// The observation never changes the Action's outcome: a read that fails
// becomes a note after the Action's own output. A failed Action prints no
// observation; a Batch that ran past failed steps prints one under its
// steps' lines, since the agent must read the Window before repeating an
// uncertain step anyway.
//
// The Window the Action opened (learn-from 01, ADR 0021 amendment): when
// the opened-Window settle (opened.go) or a Headless ContextMenu found
// exactly one new Window, a menu, a dialog or a popup, it is read too,
// after the Action's own Window, and printed whole under one line naming
// it: it has no earlier read worth diffing against. Like get-window-state
// of it, the read replaces the Agent's Window State of that Window. With
// several new Windows none is read and a note says so. The read shares
// the observation's budget and takes its own view (its own cap); a read
// that fails, or a Window that closed meanwhile, is a note.
//
// The Page settles first (bladebro-ideas 07, ADR 0021 amendment): on a
// browser Window or a Tab target, the read waits for the Page to stop
// changing (page.Settle: a DOM-quiet window and a window in which no
// request finished), up to page.SettleBound. A Page still changing then is
// read anyway, with a note saying so and why. A settle that cannot run (no
// Browser Connection, a browser page, a dialog) is skipped: the read says
// what it finds.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

type observeKey struct{}

// withObserve carries a request's --observe to the registry's wrapper.
func withObserve(ctx context.Context, o *protocol.Observe) context.Context {
	return context.WithValue(ctx, observeKey{}, o)
}

// observeOf is the request's --observe, or nil.
func observeOf(ctx context.Context) *protocol.Observe {
	o, _ := ctx.Value(observeKey{}).(*protocol.Observe)
	return o
}

// observeUsage is --observe's usage line.
const observeUsage = "usage: winuse <action> ... --observe[=diff|full] [--interactive] [--depth N] [--compact] [--max-chars N | --out <file>] [--full]"

// checkObserve refuses an --observe the read cannot take, before the
// Action sends anything.
func checkObserve(name string, o *protocol.Observe) *Error {
	usage := func(msg string) *Error {
		return nothingSent(&Error{Msg: msg, Hint: observeUsage, Exit: 1, Code: protocol.CodeUsage})
	}
	switch {
	case !protocol.Observable(name):
		return usage(fmt.Sprintf("%s does not take --observe: it goes with an Action or batch", name))
	case o.Mode != protocol.ObserveDiff && o.Mode != protocol.ObserveFull:
		return usage(fmt.Sprintf("--observe=%s is not a mode: pass --observe=diff or --observe=full", o.Mode))
	}
	if e := budgetError(observeRef(o)); e != nil {
		return usage("--observe: " + strings.TrimPrefix(e.Msg, "get-window-state: "))
	}
	return nil
}

// observeRef is the read o asks for, as get-window-state's arguments: its budgets
// and their defaults (views.go) are the read's.
func observeRef(o *protocol.Observe) windowRef {
	return windowRef{Interactive: o.Interactive, Depth: o.Depth, Compact: o.Compact, MaxChars: o.MaxChars, Out: o.Out, Full: o.Full, URLs: o.URLs}
}

// observed is the registry's wrapper of the ObservableCommands: with
// --observe, the command runs under its own budget, then its Window is
// read and the observation follows its output. The read runs after the
// command's lanes are released: a read takes no lane (lanes.go).
func (s Services) observed(name string, cmd Command) Command {
	if !protocol.Observable(name) {
		return func(ctx context.Context, args json.RawMessage) (string, *Error) {
			if o := observeOf(ctx); o != nil {
				return "", checkObserve(name, o)
			}
			return cmd(ctx, args)
		}
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		o := observeOf(ctx)
		if o == nil {
			return cmd(ctx, args)
		}
		if e := checkObserve(name, o); e != nil {
			return "", e
		}
		// The command keeps the budget it has without --observe
		// (protocol.Budget added ObserveTimeout for the read).
		rec := &openedRecord{}
		actCtx := context.WithValue(withObserve(ctx, nil), openedRecordKey{}, rec)
		if dl, ok := ctx.Deadline(); ok {
			var cancel context.CancelFunc
			actCtx, cancel = context.WithDeadline(actCtx, dl.Add(-protocol.ObserveTimeout))
			defer cancel()
		}
		out, err := cmd(actCtx, args)
		if err != nil {
			if name != "batch" || err.Exit == protocol.ExitEsc || err.Out == "" {
				return out, err // a failed Action, a stopped Batch: nothing to observe
			}
			e := *err
			main, mainTree := s.observe(ctx, args, o)
			e.Out += main + s.observeOpened(ctx, rec.get(), o, mainTree)
			return "", &e
		}
		main, mainTree := s.observe(ctx, args, o)
		return out + main + s.observeOpened(ctx, rec.get(), o, mainTree), nil
	}
}

// openedRecord holds the Windows the Action under --observe opened, as the
// last look of its opened-Window settle found them (opened.go), or the menu
// a Headless ContextMenu opened (contextmenu.go).
type openedRecord struct {
	mu      sync.Mutex
	windows []win.WindowInfo
}

type openedRecordKey struct{}

// recordOpened records the Windows an Action opened for the --observe
// under ctx; outside one it does nothing. The latest call wins: a
// Batch's settle around all its steps replaces what a step recorded.
func recordOpened(ctx context.Context, opened []win.WindowInfo) {
	if r, ok := ctx.Value(openedRecordKey{}).(*openedRecord); ok {
		r.mu.Lock()
		r.windows = append([]win.WindowInfo(nil), opened...)
		r.mu.Unlock()
	}
}

func (r *openedRecord) get() []win.WindowInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]win.WindowInfo(nil), r.windows...)
}

// observeOpened reads the one Window the Action opened and prints its
// whole Window State under a line naming it, through o's view; "" when
// none opened. Several opened Windows are a note: which one the agent
// wants is not winuse's guess. A read that fails is a note.
func (s Services) observeOpened(ctx context.Context, opened []win.WindowInfo, o *protocol.Observe, mainTree *wstate.Tree) string {
	switch len(opened) {
	case 0:
		return ""
	case 1:
	default:
		ids := make([]string, len(opened))
		for i, w := range opened {
			ids[i] = strconv.FormatUint(w.ID, 10)
		}
		return fmt.Sprintf("%s--observe read none of the %d Windows the Action opened (%s); read the one you need with winuse get-window-state <Window ID>\n",
			notePrefix, len(opened), strings.Join(ids, ", "))
	}
	w := opened[0]
	id := strconv.FormatUint(w.ID, 10)
	if s.Surfaces != nil {
		// A menu can close between the settle and this read; the fresh
		// listing also carries the Window as it is now.
		all, err := s.Surfaces()
		if err == nil {
			found := false
			for _, x := range all {
				if x.ID == w.ID {
					w, found = x, true
					break
				}
			}
			if !found {
				return observeNote(id, Fail("the Window the Action opened closed before the read", ""))
			}
		}
	}
	if at := windowInRead(mainTree, w); at > 0 {
		return fmt.Sprintf("%sthe Window the Action opened, %s %q, is Element %d of the read above, so it is not read again; act on it through that read, or read it alone with winuse get-window-state %s\n",
			notePrefix, id, w.Title, at, id)
	}
	v := observeRef(o).view(id)
	if o.Out != "" {
		v.MaxChars = 0
	}
	tree, rerr := readWindowTree(s, uia.WithSettle(ctx, stateSettle(ctx)), w)
	if rerr != nil {
		return observeNote(id, rerr)
	}
	s.sawWindow(w.ID)
	head := fmt.Sprintf("The Window the Action opened, %s, read whole; its Element Indexes go with window %s:\n", id, id)
	text := wstate.RenderView(tree, v)
	if o.Out != "" {
		out, werr := writeStateOut(text, tree, openedOut(o.Out, w.ID), "Observed Window State of window "+id)
		if werr != nil {
			return observeNote(id, werr)
		}
		return head + out
	}
	return head + text
}

// windowInRead is the Element Index at which the read tree already holds
// the opened Window w, or 0: UI Automation lists a dialog its main Window
// owns inside that Window's tree (a classic dialog, a WinForms form), so
// the main observation printed it already. It matches a "window" Element
// below the root named as w's title, and only for a Window that has an
// owner.
func windowInRead(tree *wstate.Tree, w win.WindowInfo) int {
	if tree == nil || w.Owner == 0 {
		return 0
	}
	title := wstate.Collapse(w.Title)
	if title == "" {
		return 0
	}
	for i, e := range tree.Elements {
		if i > 0 && e.Depth > 0 && e.Role == "window" && e.Name == title {
			return i
		}
	}
	return 0
}

// openedOut is the file --out <file> writes the opened Window's State to:
// the observation's file with the Window ID before its extension
// (observed.txt: observed-5678.txt).
func openedOut(out string, id uint64) string {
	ext := filepath.Ext(out)
	return fmt.Sprintf("%s-%d%s", strings.TrimSuffix(out, ext), id, ext)
}

// observe reads the Window (or Tab target) the command named in args and
// prints it as o asks, with the tree it read (nil for a Tab target or a
// read that failed); a read that fails is a note.
func (s Services) observe(ctx context.Context, args json.RawMessage, o *protocol.Observe) (string, *wstate.Tree) {
	var a struct {
		Window string `json:"window"`
	}
	_ = json.Unmarshal(args, &a)
	v := observeRef(o).view("") // the read's budgets and defaults (views.go)
	if o.Out != "" {
		v.MaxChars = 0 // --out writes it uncut, as get-window-state --out does
	}
	var before []wstate.Element
	var tree *wstate.Tree
	var what string
	var rerr *Error
	settled := ""
	if bt := tabTargetOf(ctx); bt != nil {
		what = fmt.Sprintf("tab %s of window %d", bt.short(), bt.w.ID)
		if bt.session != "" && !bt.tab.Internal {
			settled = s.settleNote(ctx, bt.conn.Client, bt.session)
		}
		if st, ok := s.tabStates.get(AgentOf(ctx).ID, tabKeyOf(bt)); ok {
			before = st.elements
		}
		tree, rerr = s.readTabTree(ctx, bt)
	} else {
		w, werr := resolveWindow(s, a.Window)
		if werr != nil {
			return observeNote(a.Window, werr), nil
		}
		what = "window " + strconv.FormatUint(w.ID, 10)
		v.Ref = strconv.FormatUint(w.ID, 10) // the notes' --root commands
		settled = s.settleWindow(ctx, w)
		before = s.storedState(ctx, w.ID)
		tree, rerr = readWindowTree(s, uia.WithSettle(ctx, stateSettle(ctx)), w)
		if rerr == nil {
			s.sawWindow(w.ID) // identity.go: what the read Window ID names
		}
	}
	if rerr != nil {
		return observeNote(a.Window, rerr) + settled, nil
	}
	text := wstate.RenderView(tree, v)
	if o.Mode == protocol.ObserveDiff {
		text = wstate.RenderChanges(before, tree, v, what).Text
	}
	if o.Out != "" {
		out, werr := writeStateOut(text, tree, o.Out, "Observed Window State")
		if werr != nil {
			return observeNote(a.Window, werr) + settled, nil
		}
		return out + settled, nil // the read is in the file: the opened Window gets its own
	}
	if tabTargetOf(ctx) != nil {
		return text + settled, nil
	}
	return text + settled, tree
}

// settleWindow settles the Page a browser Window shows before --observe
// reads it, and returns the note for a Page that did not settle ("" for
// any other Window, or a settle that could not run).
func (s Services) settleWindow(ctx context.Context, w win.WindowInfo) string {
	if s.Pages == nil || s.Pages.Browser(w) == "" || s.live() == nil {
		return ""
	}
	pw, rerr := s.pageWindow(ctx, strconv.FormatUint(w.ID, 10))
	if rerr != nil || pw.tab.Internal || pw.session == "" {
		return ""
	}
	return s.settleNote(ctx, pw.conn.Client, pw.session)
}

// settleNote settles a Page (page.Settle, up to page.SettleBound) and
// returns the note for one that did not settle.
func (s Services) settleNote(ctx context.Context, c *cdp.Client, session string) string {
	var stopped func() bool
	if s.Esc != nil {
		stopped = s.Esc.Stopped
	}
	r, err := page.Settle(ctx, c, session, page.SettleBound, stopped)
	if err != nil || r.Settled {
		return ""
	}
	return notePrefix + r.Note() + "\n"
}

// storedState is the Agent's latest Window State of hwnd, every Element
// Index it holds included (find's and --root's), or nil.
func (s Services) storedState(ctx context.Context, hwnd uint64) []wstate.Element {
	for _, src := range []any{s.Actions, s.Trees} {
		if st, ok := src.(storedElements); ok {
			if els, ok := st.StoredElements(ctx, hwnd); ok {
				return els
			}
			return nil
		}
	}
	return nil
}

// observeNote is the line a failed observation prints after the
// command's own output, which stands.
func observeNote(window string, e *Error) string {
	note := fmt.Sprintf("%s--observe could not read %s after the Action, which ran as reported above: %s", notePrefix, window, e.Msg)
	if e.Hint != "" {
		note += "; " + e.Hint
	}
	return note + "\n"
}
