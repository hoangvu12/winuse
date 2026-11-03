package uia

// Live re-resolution of name:/id: references (desktop-parity ticket 17):
// winuse's answer, with ticket 04's added numbering, to agent-desktop's
// re-identified refs. A reference resolves against the Agent's cached
// Window State first, as before. When that cannot answer (no Window State
// after a helper restart, no matching Element, or a matching Element that
// is gone), a bounded walk of the live Window applies the same match rules,
// and the Element it finds is added after the Window State's own indexes
// (store.add), so the Window State the agent holds is never replaced.
// Ambiguity still refuses: the candidates a fresh read found are added too
// and listed with their indexes.
//
// Element Indexes stay strict: they are valid only for the Window State
// that printed them. Another Agent's Window State is never a reason to walk:
// an Agent that has not read a Window is told so (ADR 0014 Decision 2).

import (
	"context"
	"errors"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// LiveResolution is how a name:/id: reference resolved: its Element Index,
// and whether a fresh read found it (the cache could not answer).
type LiveResolution struct {
	Index int
	Fresh bool
}

// LiveRefError is a reference that a fresh read could not resolve either:
// it wraps the *wstate.NoMatchRefError or *wstate.AmbiguousRefError of
// that read. An ambiguous one's Matches and Candidates are the Element
// Indexes its candidates were added under.
type LiveRefError struct{ Err error }

func (e *LiveRefError) Error() string { return e.Err.Error() }
func (e *LiveRefError) Unwrap() error { return e.Err }

// ResolveElementRefLive resolves ref for the requesting Agent against its
// cached Window State of hwnd, and when that cannot answer, against a
// fresh bounded walk of the live Window (within budget and the request's
// deadline).
func (c *Client) ResolveElementRefLive(ctx context.Context, hwnd uint64, ref string, budget time.Duration) (LiveResolution, error) {
	if _, _, err := wstate.ParseElementRef(ref); err != nil {
		return LiveResolution{}, err
	}
	who := storeAgent(ctx)
	els, base, ok := c.store.refState(who, hwnd)
	var noState error
	if !ok {
		noState = c.store.noState(who, hwnd)
	}
	return resolveCacheFirst(cacheView{els: els, base: base, ok: ok, noState: noState}, ref,
		func(i int) bool { return c.alive(ctx, hwnd, i) },
		func() (LiveResolution, error) { return c.resolveLive(ctx, hwnd, ref, budget) })
}

// cacheView is what the cache-first decision reads of the store.
type cacheView struct {
	els     []wstate.Element
	base    int
	ok      bool  // the Agent has a Window State of the Window
	noState error // without one, why (ErrNoState, or NotReadByAgentError)
}

// resolveCacheFirst is the decision: the cache's answer when it has one
// and its Element is alive; its ambiguity as is; another Agent's read as a
// refusal; the live walk for everything else (no Window State, no match, a
// gone Element).
func resolveCacheFirst(cv cacheView, ref string, alive func(int) bool, live func() (LiveResolution, error)) (LiveResolution, error) {
	if !cv.ok {
		var other *NotReadByAgentError
		if errors.As(cv.noState, &other) {
			return LiveResolution{}, cv.noState
		}
		return live()
	}
	i, err := resolveBaseFirst(cv.els, cv.base, ref)
	var none *wstate.NoMatchRefError
	switch {
	case err == nil && alive(i):
		return LiveResolution{Index: i}, nil
	case err != nil && !errors.As(err, &none):
		return LiveResolution{}, err // several Elements match: never a guess
	}
	return live()
}

// alive reports whether the Element behind index still exists: its
// RuntimeId reads. A Page Element (no UIA Element) and an unreadable
// probe count as alive: the Action's own step checks them as before.
func (c *Client) alive(ctx context.Context, hwnd uint64, index int) bool {
	gone := false
	c.RunFor(ctx, hwnd, func(w *worker, _ *run) {
		el, err := c.lookup(ctx, hwnd, index)
		if err != nil {
			gone = errors.Is(err, ErrStale)
			return
		}
		defer c.store.release(el)
		if w.thread == nil {
			return
		}
		_, hr := runtimeID(el)
		gone = hrStale(hr)
	})
	return !gone
}

// resolveLive walks the live Window like find (up to FindMaxNodes
// Elements) and resolves ref against what it read, adding the match, or
// every candidate of an ambiguous one, to the Agent's store.
func (c *Client) resolveLive(ctx context.Context, hwnd uint64, ref string, budget time.Duration) (LiveResolution, error) {
	var out LiveResolution
	var jobErr error
	cfg := walkConfig(ctx, budget)
	cfg.MaxNodes = FindMaxNodes
	who := storeAgent(ctx)
	err := c.RunFor(ctx, hwnd, func(w *worker, r *run) {
		if w.thread == nil {
			jobErr = errNoState(hwnd) // fake workers carry no COM thread
			return
		}
		src := &comSource{th: w.thread, hwnd: uintptr(hwnd)}
		defer src.dispose()
		res, err := Walk[comPtr](src, cfg)
		for _, p := range res.Dropped {
			release(p)
		}
		if err != nil {
			jobErr = err
			return
		}
		keep, rerr := liveMatches(res.Elements, ref)
		var live []comPtr
		var elements []wstate.Element
		for i := range res.Elements {
			if keep[i] {
				live = append(live, res.Live[i])
				elements = append(elements, res.Elements[i])
				continue
			}
			release(res.Live[i])
		}
		indexes := c.addRead(who, hwnd, live, elements, r)
		out, jobErr = liveAnswer(rerr, indexes, elements)
	})
	if err != nil {
		return LiveResolution{}, err
	}
	return out, jobErr
}

// liveMatches marks the Elements of a fresh read that ref names (one, or
// every candidate when it is ambiguous) and returns the read's resolution
// error.
func liveMatches(els []wstate.Element, ref string) ([]bool, error) {
	keep := make([]bool, len(els))
	i, err := wstate.ResolveElementRef(els, ref)
	var several *wstate.AmbiguousRefError
	switch {
	case err == nil:
		keep[i] = true
	case errors.As(err, &several):
		for _, m := range several.Matches {
			keep[m] = true
		}
	}
	return keep, err
}

// liveAnswer turns a fresh read's resolution into the answer, with the
// Element Indexes its matches were added under.
func liveAnswer(err error, indexes []int, elements []wstate.Element) (LiveResolution, error) {
	var several *wstate.AmbiguousRefError
	switch {
	case err == nil && len(indexes) == 1:
		return LiveResolution{Index: indexes[0], Fresh: true}, nil
	case errors.As(err, &several) && len(indexes) == len(elements):
		several.Matches = indexes
		several.Candidates = nil
		for k := range elements {
			several.Candidates = append(several.Candidates, wstate.Line(indexes[k], &elements[k]))
		}
		return LiveResolution{}, &LiveRefError{Err: several}
	case err != nil:
		return LiveResolution{}, &LiveRefError{Err: err}
	}
	return LiveResolution{}, &LiveRefError{Err: errors.New("the fresh read's match could not be stored")}
}
