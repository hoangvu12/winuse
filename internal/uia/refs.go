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

	"github.com/hoangvu12/winuse/internal/wstate"
)

// ResolveElementRef resolves a name:/id: Element Reference against
// the requesting Agent's latest Window State of hwnd: exactly one match
// resolves to its Element Index; several matches and no match fail with
// the candidates (never a guess), and a Window with no cached Window
// State for the Agent fails as ErrNoState (read the Window State again,
// the shared stale contract).
func (c *Client) ResolveElementRef(ctx context.Context, hwnd uint64, ref string) (int, error) {
	who := storeAgent(ctx)
	els, ok := c.store.state(who, hwnd)
	if !ok {
		return 0, c.store.noState(who, hwnd)
	}
	return wstate.ResolveElementRef(els, ref)
}
