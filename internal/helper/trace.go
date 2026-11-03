package helper

// The helper's part of a session trace (desktop-parity ticket 19): when a
// request asks for it (protocol.Request.Trace), the server puts trace
// notes on the command's context, the Action pipelines note the Window
// they resolved and the route they took (runAction, runHeadless,
// runKeyHeadless, runPage, runPageInput, runPageWindow, runWindowAction),
// and the server answers them in protocol.Response.Trace. The trace file
// itself is the CLI's (internal/cli/trace.go). Without the request's
// flag there are no notes and every note is a no-op.

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

type traceNotesKey struct{}

// traceNotes is what one traced request's command noted. A Batch's steps
// note into the same notes.
type traceNotes struct {
	mu     sync.Mutex
	window *protocol.TraceWindow
	routes []string
}

// withTraceNotes puts fresh notes on ctx when on; otherwise ctx is
// returned as is, with nil notes.
func withTraceNotes(ctx context.Context, on bool) (context.Context, *traceNotes) {
	if !on {
		return ctx, nil
	}
	n := &traceNotes{}
	return context.WithValue(ctx, traceNotesKey{}, n), n
}

// traceNotesOf returns ctx's notes, nil outside a traced request.
func traceNotesOf(ctx context.Context) *traceNotes {
	n, _ := ctx.Value(traceNotesKey{}).(*traceNotes)
	return n
}

// noteRoute notes the Window an Action resolved and the route it takes.
// The first Window noted stays; each route is noted once, in order.
func noteRoute(ctx context.Context, w win.WindowInfo, route string) {
	n := traceNotesOf(ctx)
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.noteWindowLocked(w)
	if !slices.Contains(n.routes, route) {
		n.routes = append(n.routes, route)
	}
}

// keyRunRoute is runKeyHeadless's route: Posted Keys, or the Headless
// key and edit routes.
func keyRunRoute(posted bool) string {
	if posted {
		return protocol.RoutePosted
	}
	return protocol.RouteHeadless
}

func (n *traceNotes) noteWindowLocked(w win.WindowInfo) {
	if n.window == nil {
		n.window = &protocol.TraceWindow{ID: w.ID, App: w.App, Title: w.Title}
	}
}

// hasWindow reports whether a Window was noted.
func (n *traceNotes) hasWindow() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.window != nil
}

// report is the notes as the response's Trace; nil when nothing was noted.
func (n *traceNotes) report() *protocol.Trace {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.window == nil && len(n.routes) == 0 {
		return nil
	}
	return &protocol.Trace{Window: n.window, Routes: slices.Clone(n.routes)}
}

// tracedWindow wraps a command so that a traced run whose command noted
// no Window (a read, a Tab command, a refusal before any pipeline) notes
// the Window its arguments name, resolved after the run. Untraced runs
// pass through.
func (s Services) tracedWindow(cmd Command) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		out, err := cmd(ctx, args)
		n := traceNotesOf(ctx)
		if n == nil || n.hasWindow() {
			return out, err
		}
		var a struct {
			Window string `json:"window"`
		}
		if json.Unmarshal(args, &a) == nil && a.Window != "" {
			if w, rerr := resolveWindow(s, a.Window); rerr == nil {
				n.mu.Lock()
				n.noteWindowLocked(w)
				n.mu.Unlock()
			}
		}
		return out, err
	}
}
