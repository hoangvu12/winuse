// Package pagetest is a fake launched Chromium browser for the tab
// commands' offline tests (browser merge ticket 07): browser windows with
// bounds and Tabs in tab strip order over a cdptest endpoint, answering
// Target.getTargets, Browser.getWindowForTarget, Target.createTarget,
// Target.closeTarget and document.visibilityState, plus the keyboard
// shortcuts Chrome uses to switch Tabs. No browser is started.
package pagetest

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// Tab is one Tab of a fake browser window.
type Tab struct {
	TargetID string
	Title    string
	URL      string
}

// Window is one fake browser window: its bounds in DIPs, its Tabs in tab
// strip order and the selected one.
type Window struct {
	ID                       int
	Left, Top, Width, Height int
	Tabs                     []Tab
	Active                   int // index into Tabs
}

// Other is a page target that is no Tab of a tab strip, as Chrome lists
// them: a DevTools window, the WebUI Chrome draws in a window's frame (tab
// search, side panels), an extension's popup. Browser.getWindowForTarget
// puts it in window Window.
type Other struct {
	TargetID, Type, Title, URL string
	Window                     int
}

// Browser is the fake.
type Browser struct {
	Srv *cdptest.Server
	// Others are listed after the Tabs; set them before the first call.
	Others []Other

	mu      sync.Mutex
	windows []*Window
	// LastActive is the window Target.createTarget puts a new Tab in, as
	// Chrome uses the profile's last active window.
	LastActive int
	// Foreground is the window real keys reach; 0 is none.
	Foreground int
	// SlowSwitch delays how long a switched Tab takes to become visible.
	SlowSwitch time.Duration
	// KeepOpen keeps closed Tabs listed (a page asking whether to leave).
	KeepOpen bool
	created  int
	shownAt  map[string]time.Time
}

// New starts a fake browser with these windows; the first is the last
// active one.
func New(t testing.TB, windows ...Window) *Browser {
	t.Helper()
	b := &Browser{Srv: cdptest.NewServer(), shownAt: map[string]time.Time{}}
	t.Cleanup(b.Srv.Close)
	for i := range windows {
		w := windows[i]
		b.windows = append(b.windows, &w)
	}
	if len(windows) > 0 {
		b.LastActive = windows[0].ID
	}
	s := b.Srv
	s.Handle("Target.getTargets", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		var infos []map[string]any
		// Chrome does not list targets in tab strip order: newest first.
		for wi := len(b.windows) - 1; wi >= 0; wi-- {
			w := b.windows[wi]
			for ti := len(w.Tabs) - 1; ti >= 0; ti-- {
				t := w.Tabs[ti]
				infos = append(infos, map[string]any{"targetId": t.TargetID, "type": "page", "title": t.Title, "url": t.URL})
			}
		}
		infos = append(infos, map[string]any{"targetId": "SW1", "type": "service_worker", "title": "", "url": "https://a.test/sw.js"})
		for _, o := range b.Others {
			infos = append(infos, map[string]any{"targetId": o.TargetID, "type": o.Type, "title": o.Title, "url": o.URL})
		}
		return map[string]any{"targetInfos": infos}, nil
	})
	s.Handle("Browser.getWindowForTarget", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		id := targetParam(m)
		b.mu.Lock()
		defer b.mu.Unlock()
		w, _ := b.find(id)
		for _, o := range b.Others {
			for _, x := range b.windows {
				if o.TargetID == id && x.ID == o.Window {
					w = x
				}
			}
		}
		if w == nil {
			return nil, &cdptest.ReplyError{Code: -32000, Message: "No web contents for the given target id"}
		}
		return map[string]any{"windowId": w.ID, "bounds": map[string]any{
			"left": w.Left, "top": w.Top, "width": w.Width, "height": w.Height, "windowState": "normal"}}, nil
	})
	s.Handle("Target.createTarget", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			URL string `json:"url"`
		}
		json.Unmarshal(m.Params, &p)
		b.mu.Lock()
		defer b.mu.Unlock()
		b.created++
		id := fmt.Sprintf("C0FFEE%02d00000000000000000000000000", b.created)
		for _, w := range b.windows {
			if w.ID == b.LastActive {
				w.Tabs = append(w.Tabs, Tab{TargetID: id, Title: p.URL, URL: p.URL})
			}
		}
		return map[string]any{"targetId": id}, nil
	})
	s.Handle("Target.closeTarget", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		id := targetParam(m)
		b.mu.Lock()
		defer b.mu.Unlock()
		w, i := b.find(id)
		if w == nil {
			return nil, &cdptest.ReplyError{Code: -32602, Message: "No target with given id found"}
		}
		if !b.KeepOpen {
			activeID := w.Tabs[w.Active].TargetID
			w.Tabs = append(w.Tabs[:i:i], w.Tabs[i+1:]...)
			w.Active = 0
			for j, t := range w.Tabs {
				if t.TargetID == activeID {
					w.Active = j
				}
			}
		}
		return map[string]any{"success": true}, nil
	})
	s.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		target := strings.TrimPrefix(m.SessionID, "session-")
		b.mu.Lock()
		defer b.mu.Unlock()
		state := "hidden"
		// Not Before: with no SlowSwitch the Tab shows at once, even when
		// the clock reads the same instant as the switch.
		if w, i := b.find(target); w != nil && w.Active == i && !time.Now().Before(b.shownAt[target]) {
			state = "visible"
		}
		return map[string]any{"result": map[string]any{"type": "string", "value": state}}, nil
	})
	return b
}

func targetParam(m cdptest.Message) string {
	var p struct {
		TargetID string `json:"targetId"`
	}
	json.Unmarshal(m.Params, &p)
	return p.TargetID
}

// find is the window and index of a Tab. Call with mu held.
func (b *Browser) find(targetID string) (*Window, int) {
	for _, w := range b.windows {
		for i, t := range w.Tabs {
			if t.TargetID == targetID {
				return w, i
			}
		}
	}
	return nil, -1
}

// Window returns a copy of a window's state.
func (b *Browser) Window(id int) Window {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, w := range b.windows {
		if w.ID == id {
			c := *w
			c.Tabs = append([]Tab(nil), w.Tabs...)
			return c
		}
	}
	return Window{}
}

// Set replaces the browser's windows, as the user opening, closing and
// navigating them would.
func (b *Browser) Set(windows ...Window) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.windows = nil
	for i := range windows {
		w := windows[i]
		w.Tabs = append([]Tab(nil), w.Tabs...)
		b.windows = append(b.windows, &w)
	}
}

// ActiveTab is the selected Tab's target id of a window.
func (b *Browser) ActiveTab(id int) string {
	w := b.Window(id)
	if len(w.Tabs) == 0 {
		return ""
	}
	return w.Tabs[w.Active].TargetID
}

// Press plays a key chord (keys.Chord's String form, e.g.
// "Control_L+Tab") in the foreground window, as Chrome handles its tab
// shortcuts: Ctrl+1 to Ctrl+8 select that Tab, Ctrl+9 the last one,
// Ctrl+Tab the next and Ctrl+Shift+Tab the previous, wrapping.
func (b *Browser) Press(chord string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var w *Window
	for _, x := range b.windows {
		if x.ID == b.Foreground {
			w = x
		}
	}
	if w == nil || len(w.Tabs) == 0 {
		return
	}
	n := len(w.Tabs)
	before := w.Active
	switch chord {
	case "Control_L+Tab":
		w.Active = (w.Active + 1) % n
	case "Control_L+Shift_L+Tab":
		w.Active = (w.Active - 1 + n) % n
	case "Control_L+9":
		w.Active = n - 1
	default:
		if d, ok := strings.CutPrefix(chord, "Control_L+"); ok {
			if k, err := strconv.Atoi(d); err == nil && k >= 1 && k <= 8 && k <= n {
				w.Active = k - 1
			}
		}
	}
	if w.Active != before {
		b.shownAt[w.Tabs[w.Active].TargetID] = time.Now().Add(b.SlowSwitch)
	}
}

// Connection is a launched-browser Browser Connection to the fake.
func (b *Browser) Connection(t testing.TB) *browser.Connection {
	t.Helper()
	ws, err := cdp.DialWS(context.Background(), b.Srv.URL(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(ws)
	t.Cleanup(func() { c.Close() })
	return &browser.Connection{Browser: "chrome", Mode: browser.ModeLaunch, PID: 4242, Client: c}
}

// Methods lists the CDP methods the fake received.
func (b *Browser) Methods() []string { return b.Srv.Methods() }
