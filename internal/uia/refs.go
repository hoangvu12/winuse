package uia

// name:/id: Element Reference resolution against a Window's latest
// Window State (ticket 11): the reference space is the Elements the
// helper stored with the live Elements — the Window State the agent saw —
// so resolution is pure metadata matching (internal/wstate.ResolveElementRef)
// with no COM calls and no worker job. The live Element behind the
// resolved Index is probed by the Action's own step (ElementPoint,
// SetValue, ...), which is where staleness surfaces — exactly like an
// Element Index the agent passed directly.

import (
	"context"
	"errors"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// ResolveElementRef resolves a name:/id: Element Reference against
// the requesting Agent's latest Window State of hwnd: exactly one match
// resolves to its Element Index; several matches and no match fail with
// the candidates (never a guess), and a Window with no cached Window
// State for the Agent fails as ErrNoState (read the Window State again,
// the shared stale contract).
//
// Elements that find or a region read added after the Window State's own
// (desktop-parity 04) often repeat Elements of the read itself, so the
// read's own Elements are searched first, and the added ones only when
// none of them matches: a reference that worked before a find still
// resolves to the same Element after it.
//
// A partial Window State answers only with ambiguity (reliability council
// 03): its one match is a *wstate.PartialRefError and its miss a
// *wstate.NoMatchRefError marked Partial (wstate.InPartialRead); so is a
// one match a partial fresh read found and added (doubted).
func (c *Client) ResolveElementRef(ctx context.Context, hwnd uint64, ref string) (int, error) {
	who := storeAgent(ctx)
	cv, ok := c.store.refState(who, hwnd)
	if !ok {
		return 0, c.store.noState(who, hwnd)
	}
	i, err := resolveBaseFirst(cv.els, cv.base, ref)
	if cv.partial || (err == nil && cv.doubted[i]) {
		return wstate.InPartialRead(cv.els, ref, i, err)
	}
	return i, err
}

// resolveBaseFirst resolves ref against els[:base], then, when nothing
// matched there, against the Elements added after it.
func resolveBaseFirst(els []wstate.Element, base int, ref string) (int, error) {
	if base >= len(els) || base <= 0 {
		return wstate.ResolveElementRef(els, ref)
	}
	i, err := wstate.ResolveElementRef(els[:base], ref)
	var none *wstate.NoMatchRefError
	if !errors.As(err, &none) {
		return i, err
	}
	i, err = wstate.ResolveElementRef(els[base:], ref)
	var several *wstate.AmbiguousRefError
	if errors.As(err, &several) {
		for k := range several.Matches {
			several.Matches[k] += base
		}
		several.Candidates = nil
		for _, m := range several.Matches {
			several.Candidates = append(several.Candidates, wstate.Line(m, &els[m]))
		}
	}
	if err != nil {
		return 0, err
	}
	return base + i, nil
}
