package page

// The remembered Window to browser window mapping. Two browser windows at
// the same bounds showing Tabs of the same title cannot be told apart by
// MatchTab: Chrome opens new windows at one default position, so two
// windows of one site do that. A Window that once mapped to one browser
// window without doubt keeps it: a later ambiguous match takes the one
// candidate Tab in that browser window. A Window never mapped uniquely
// stays an error; nothing is guessed.

import (
	"sync"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// WindowMemory remembers, for one helper, the browser window each Window
// last mapped to uniquely: the extension's windowId, or the window id
// Browser.getWindowForTarget gives on a launched browser. It is safe for
// concurrent use; a nil WindowMemory remembers nothing.
//
// An entry is forgotten when its Browser Connection ended or was dialed
// again (window ids belong to one connection), when the extension no
// longer lists its browser window, when another Window maps to that
// browser window, and when a later ambiguous match finds no candidate
// in it (the browser window is gone, or no longer this Window's).
type WindowMemory struct {
	mu sync.Mutex
	m  map[windowKey]remembered
}

// windowKey is one Window: its handle and its browser process.
type windowKey struct {
	hwnd uint64
	pid  uint32
}

type remembered struct {
	client *cdp.Client
	window int
}

// NewWindowMemory builds an empty WindowMemory.
func NewWindowMemory() *WindowMemory {
	return &WindowMemory{m: map[windowKey]remembered{}}
}

// Remember records that the Window g is browser window windowID on the
// connection c: after a unique match, or when winuse put a Tab in that
// window itself (tab new's read-back).
func (m *WindowMemory) Remember(c *cdp.Client, g Geometry, windowID int) {
	if m == nil || g.HWND == 0 || c == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweep()
	key := windowKey{g.HWND, g.PID}
	for k, e := range m.m {
		if k != key && e.client == c && e.window == windowID {
			delete(m.m, k) // a browser window has one Window
		}
	}
	m.m[key] = remembered{client: c, window: windowID}
}

// Window is the browser window remembered for g on c, if any.
func (m *WindowMemory) Window(c *cdp.Client, g Geometry) (int, bool) {
	if m == nil {
		return 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.m[windowKey{g.HWND, g.PID}]
	if !ok || e.client != c {
		return 0, false
	}
	return e.window, true
}

// recall settles an ambiguous match for g: the one candidate in the
// browser window g last mapped to. A candidate is a Tab whose window
// bounds match g, so that browser window still exists and still lines up
// with the Window. Several candidates in it stay ambiguous. None means
// the browser window is gone or no longer g's (the Window's own active
// Tab is always a candidate), and the entry is forgotten.
func (m *WindowMemory) recall(c *cdp.Client, g Geometry, candidates []Tab) (Tab, bool) {
	if m == nil || g.HWND == 0 {
		return Tab{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweep()
	key := windowKey{g.HWND, g.PID}
	e, ok := m.m[key]
	if !ok {
		return Tab{}, false
	}
	if e.client != c {
		delete(m.m, key) // the connection was dialed again
		return Tab{}, false
	}
	var in []Tab
	for _, t := range candidates {
		if t.WindowID == e.window {
			in = append(in, t)
		}
	}
	switch len(in) {
	case 1:
		return in[0], true
	case 0:
		delete(m.m, key)
	}
	return Tab{}, false
}

// prune forgets c's entries whose browser window the extension no longer
// lists (chrome.windows.getAll).
func (m *WindowMemory) prune(c *cdp.Client, windows map[int]DIPBounds) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.m {
		if _, ok := windows[e.window]; e.client == c && !ok {
			delete(m.m, k)
		}
	}
}

// sweep forgets the entries of ended connections. Call with mu held.
func (m *WindowMemory) sweep() {
	for k, e := range m.m {
		if !e.client.Alive() {
			delete(m.m, k)
		}
	}
}
