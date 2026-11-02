package uia

import "github.com/hoangvu12/winuse/internal/wstate"

// StoredElements returns a copy of the Elements of hwnd's latest Window
// State, the one the agent saw, with no COM call and no worker job; false
// when the Window has none. The tab commands (browser merge ticket 07)
// find a browser Window's tab strip items in it, for the Agent Cursor and
// the Headed tab switch.
func (c *Client) StoredElements(hwnd uint64) ([]wstate.Element, bool) {
	els, ok := c.store.state(hwnd)
	if !ok {
		return nil, false
	}
	return append([]wstate.Element(nil), els...), true
}
