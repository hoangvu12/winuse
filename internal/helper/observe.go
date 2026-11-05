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

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
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
	return windowRef{Interactive: o.Interactive, Depth: o.Depth, Compact: o.Compact, MaxChars: o.MaxChars, Out: o.Out, Full: o.Full}
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
		actCtx := withObserve(ctx, nil)
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
			e.Out += s.observe(ctx, args, o)
			return "", &e
		}
		return out + s.observe(ctx, args, o), nil
	}
}

// observe reads the Window (or Tab target) the command named in args and
// prints it as o asks; a read that fails is a note.
func (s Services) observe(ctx context.Context, args json.RawMessage, o *protocol.Observe) string {
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
	if bt := tabTargetOf(ctx); bt != nil {
		what = fmt.Sprintf("tab %s of window %d", bt.short(), bt.w.ID)
		if st, ok := s.tabStates.get(AgentOf(ctx).ID, tabKeyOf(bt)); ok {
			before = st.elements
		}
		tree, rerr = s.readTabTree(ctx, bt)
	} else {
		w, werr := resolveWindow(s, a.Window)
		if werr != nil {
			return observeNote(a.Window, werr)
		}
		what = "window " + strconv.FormatUint(w.ID, 10)
		v.Ref = strconv.FormatUint(w.ID, 10) // the notes' --root commands
		before = s.storedState(ctx, w.ID)
		tree, rerr = readWindowTree(s, uia.WithSettle(ctx, stateSettle(ctx)), w)
		if rerr == nil {
			s.sawWindow(w.ID) // identity.go: what the read Window ID names
		}
	}
	if rerr != nil {
		return observeNote(a.Window, rerr)
	}
	text := wstate.RenderView(tree, v)
	if o.Mode == protocol.ObserveDiff {
		text = wstate.RenderChanges(before, tree, v, what).Text
	}
	if o.Out != "" {
		out, werr := writeStateOut(text, tree, o.Out, "Observed Window State")
		if werr != nil {
			return observeNote(a.Window, werr)
		}
		return out
	}
	return text
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
