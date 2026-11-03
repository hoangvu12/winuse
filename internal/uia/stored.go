package uia

import (
	"context"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// StoredElements returns a copy of the Elements of the requesting Agent's
// latest Window State of hwnd, the one it saw, with no COM call and no
// worker job; false when the Agent has none. The tab commands (browser merge ticket 07)
// find a browser Window's tab strip items in it, for the Agent Cursor and
// the Headed tab switch.
func (c *Client) StoredElements(ctx context.Context, hwnd uint64) ([]wstate.Element, bool) {
	els, ok := c.store.state(storeAgent(ctx), hwnd)
	if !ok {
		return nil, false
	}
	return append([]wstate.Element(nil), els...), true
}
