package page

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
)

// clickablesJS is the cursor-pointer pass (clickables.js).
//
//go:embed clickables.js
var clickablesJS string

// The object groups the reads put their JS objects in, released after.
const (
	objectGroup = "winuse-page" // Origin checks and rects
	marksGroup  = "winuse-marks"
)

// settleAfterAutoAttach is how long a Tab's first read waits after
// Target.setAutoAttach for the attachedToTarget events of the iframes
// already there.
const settleAfterAutoAttach = 100 * time.Millisecond

// Reader reads Pages over Browser Connections. It keeps one flattened
// session per Tab per connection, with auto-attach on, so the iframe
// sessions it learns about stay known across reads; a session the browser
// detached is attached again once. One Reader serves the helper.
type Reader struct {
	// Settle overrides settleAfterAutoAttach (tests).
	Settle time.Duration

	mu   sync.Mutex
	tabs map[tabKey]*tabSession
}

// NewReader builds a Reader with no sessions yet.
func NewReader() *Reader { return &Reader{tabs: map[tabKey]*tabSession{}} }

type tabKey struct {
	c      *cdp.Client
	target string
}

// tabSession is one attached Tab and the iframe sessions auto-attached
// under it.
type tabSession struct {
	id   string
	subs []*cdp.Subscription

	mu       sync.Mutex
	children map[string]string // iframe target id (= its frame id) to its session id
}

// Read finds the Tab the browser Window g shows over conn (MatchTab) and
// reads its Page: the accessibility tree, the cursor-pointer pass, the
// frames' loader ids, and the documents of iframes not in the main tree,
// one level deep.
func (r *Reader) Read(ctx context.Context, conn *browser.Connection, g Geometry) (*Raw, error) {
	tab, tabs, err := windowTab(ctx, conn, g)
	if err != nil {
		return nil, err
	}
	if tab.Internal {
		return nil, &InternalPageError{URL: tab.URL} // never attached, never read
	}
	raw, err := r.readTab(ctx, conn, tab)
	if err != nil && isGone(err) && ctx.Err() == nil {
		// The cached session was detached (a cross-process navigation, the
		// extension's idle detach): attach again, once.
		r.drop(tabKey{conn.Client, tab.TargetID})
		raw, err = r.readTab(ctx, conn, tab)
	}
	if raw != nil {
		for _, t := range tabs {
			if t.TargetID != tab.TargetID && t.URL != "" {
				raw.Others = append(raw.Others, t.URL)
			}
		}
	}
	return raw, err
}

// FindTab lists the connection's Tabs (pageTabs, browser pages included),
// reads the window bounds of the ones whose title the Window shows, and
// matches (MatchTab). On the extension, where Browser.* is refused, the
// bounds come from the bridge's Winuse.windows (chrome.windows). A Tab
// that shows a browser page is matched like any other, marked Internal.
//
// With g.Memory, a unique match is remembered, and an ambiguous one is
// settled by the browser window the Window last mapped to (WindowMemory).
func FindTab(ctx context.Context, conn *browser.Connection, g Geometry) (Tab, error) {
	tab, _, err := windowTab(ctx, conn, g)
	return tab, err
}

// windowTab is FindTab, also returning every Tab it listed.
func windowTab(ctx context.Context, conn *browser.Connection, g Geometry) (Tab, []Tab, error) {
	tabs, err := pageTabs(ctx, conn.Client)
	if err != nil {
		return Tab{}, nil, err
	}
	if conn.Mode == browser.ModeApp || conn.Mode == browser.ModeAttach {
		// An App launched with --cdp or attached to (its JS shim answers no
		// Browser.getWindowForTarget): its pages match by title alone.
		tab, err := MatchAppTab(g, tabs)
		return tab, tabs, err
	}
	if conn.Mode == browser.ModeExtension {
		windows, err := extensionWindows(ctx, conn.Client)
		if err != nil {
			return Tab{}, nil, err
		}
		for i := range tabs {
			if b, ok := windows[tabs[i].WindowID]; ok {
				tabs[i].Bounds, tabs[i].HasBounds = b, true
			}
		}
		g.Memory.prune(conn.Client, windows)
	} else {
		for i := range tabs {
			if !TitleMatches(g.Title, tabs[i].Title, conn.Browser) {
				continue // the title rules it out; no need for its bounds
			}
			w, err := windowForTarget(ctx, conn.Client, tabs[i].TargetID)
			if err != nil {
				if ctx.Err() != nil {
					return Tab{}, nil, err
				}
				continue
			}
			tabs[i].WindowID, tabs[i].Bounds, tabs[i].HasBounds = w.id, w.bounds, true
		}
	}
	tab, err := MatchTab(g, conn.Browser, tabs)
	var several *AmbiguousTabError
	switch {
	case err == nil:
		g.Memory.Remember(conn.Client, g, tab.WindowID)
	case errors.As(err, &several):
		if t, ok := g.Memory.recall(conn.Client, g, several.Candidates); ok {
			return t, tabs, nil
		}
	}
	return tab, tabs, err
}

type cdpWindow struct {
	id     int
	bounds DIPBounds
}

// windowForTarget is Browser.getWindowForTarget.
func windowForTarget(ctx context.Context, c *cdp.Client, targetID string) (cdpWindow, error) {
	res, err := c.Call(ctx, "Browser.getWindowForTarget", map[string]any{"targetId": targetID})
	if err != nil {
		return cdpWindow{}, err
	}
	var w struct {
		WindowID int `json:"windowId"`
		Bounds   struct {
			Left        int    `json:"left"`
			Top         int    `json:"top"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
			WindowState string `json:"windowState"`
		} `json:"bounds"`
	}
	if err := json.Unmarshal(res, &w); err != nil {
		return cdpWindow{}, err
	}
	b := w.Bounds
	return cdpWindow{id: w.WindowID, bounds: DIPBounds{Left: b.Left, Top: b.Top, Width: b.Width, Height: b.Height, State: b.WindowState}}, nil
}

// extensionWindows is the bridge's Winuse.windows: every browser window's
// bounds by windowId.
func extensionWindows(ctx context.Context, c *cdp.Client) (map[int]DIPBounds, error) {
	res, err := c.Call(ctx, bridge.MethodWindows, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Windows []struct {
			WindowID int    `json:"windowId"`
			Left     int    `json:"left"`
			Top      int    `json:"top"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			State    string `json:"state"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	out := map[int]DIPBounds{}
	for _, w := range r.Windows {
		out[w.WindowID] = DIPBounds{Left: w.Left, Top: w.Top, Width: w.Width, Height: w.Height, State: w.State}
	}
	return out, nil
}

// readTab reads one Tab's Page over its (cached) session.
func (r *Reader) readTab(ctx context.Context, conn *browser.Connection, tab Tab) (*Raw, error) {
	c := conn.Client
	ts, err := r.session(ctx, conn, tab.TargetID)
	if err != nil {
		return nil, err
	}
	// A Page a JavaScript dialog holds answers none of the read's calls
	// until the dialog is answered (watch.go, ticket 06): fail at once on a
	// dialog the watch saw, and within AnswerTimeout on one it missed.
	if err := Ready(ctx, c, ts.id); err != nil {
		return nil, err
	}
	main, ft, err := readDoc(ctx, c, ts.id, "", 0)
	if err != nil {
		return nil, err
	}
	raw := &Raw{Browser: conn.Browser, TargetID: tab.TargetID, Title: tab.Title, URL: tab.URL, Main: main, Client: c}

	// Iframes, one level deep: a cross-origin one through its own
	// session, a same-origin one the main tree left out through the Tab's
	// session with its frameId.
	inTree := map[string]bool{}
	for _, n := range main.Nodes {
		if n.FrameID != "" {
			inTree[n.FrameID] = true
		}
	}
	children := ts.snapshot()
	done := map[string]bool{}
	var frameIDs []string
	for _, cf := range ft.ChildFrames {
		frameIDs = append(frameIDs, cf.Frame.ID)
	}
	var extra []string
	for id := range children {
		extra = append(extra, id)
	}
	sort.Strings(extra)
	frameIDs = append(frameIDs, extra...)
	for _, id := range frameIDs {
		if done[id] || ctx.Err() != nil {
			continue
		}
		done[id] = true
		session, own := children[id]
		if !own && inTree[id] {
			continue
		}
		owner, err := frameOwner(ctx, c, ts.id, id)
		if err != nil || owner == 0 {
			continue
		}
		var doc Doc
		if own {
			doc, _, err = readDoc(ctx, c, session, "", 0)
		} else {
			doc, err = sameProcessFrame(ctx, c, ts.id, id, main.Loaders[id])
		}
		if err != nil {
			continue // the iframe is left out; the rest of the Page stands
		}
		raw.Frames = append(raw.Frames, Frame{Owner: owner, Doc: doc})
	}
	return raw, nil
}

// readDoc reads one document over a session: DOM and Accessibility on, the
// frame tree's loader ids, the accessibility tree (of frameID when set)
// and the cursor-pointer pass (in contextID when set).
func readDoc(ctx context.Context, c *cdp.Client, session, frameID string, contextID int) (Doc, frameTree, error) {
	for _, m := range []string{"DOM.enable", "Accessibility.enable"} {
		if _, err := c.CallSession(ctx, session, m, nil); err != nil && isGone(err) {
			return Doc{}, frameTree{}, err
		}
	}
	res, err := c.CallSession(ctx, session, "Page.getFrameTree", nil)
	if err != nil {
		return Doc{}, frameTree{}, err
	}
	var f struct {
		FrameTree frameTree `json:"frameTree"`
	}
	if err := json.Unmarshal(res, &f); err != nil {
		return Doc{}, frameTree{}, err
	}
	doc := Doc{SessionID: session, FrameID: f.FrameTree.Frame.ID, Loaders: map[string]string{}}
	f.FrameTree.loaders(doc.Loaders, -1)
	var params any
	if frameID != "" {
		doc.FrameID = frameID
		params = map[string]any{"frameId": frameID}
	}
	res, err = c.CallSession(ctx, session, "Accessibility.getFullAXTree", params)
	if err != nil {
		return Doc{}, frameTree{}, err
	}
	var ax struct {
		Nodes []AXNode `json:"nodes"`
	}
	if err := json.Unmarshal(res, &ax); err != nil {
		return Doc{}, frameTree{}, err
	}
	doc.Nodes = ax.Nodes
	doc.Marks = readMarks(ctx, c, session, contextID)
	return doc, f.FrameTree, nil
}

// sameProcessFrame reads a same-origin iframe the main tree left out: its
// accessibility tree by frameId, and the cursor-pointer pass in an
// isolated world of that frame.
func sameProcessFrame(ctx context.Context, c *cdp.Client, session, frameID, loader string) (Doc, error) {
	world := 0
	if res, err := c.CallSession(ctx, session, "Page.createIsolatedWorld", map[string]any{"frameId": frameID, "worldName": "winuse"}); err == nil {
		var w struct {
			ExecutionContextID int `json:"executionContextId"`
		}
		json.Unmarshal(res, &w)
		world = w.ExecutionContextID
	}
	doc, _, err := readDoc(ctx, c, session, frameID, world)
	if err != nil {
		return Doc{}, err
	}
	if world == 0 {
		doc.Marks = nil // the pass ran in the main frame, not this one
	}
	if loader != "" {
		doc.Loaders[frameID] = loader
	}
	return doc, nil
}

// frameOwner is the backendNodeId of the iframe element that holds a
// frame, in the session that holds the element.
func frameOwner(ctx context.Context, c *cdp.Client, session, frameID string) (int64, error) {
	res, err := c.CallSession(ctx, session, "DOM.getFrameOwner", map[string]any{"frameId": frameID})
	if err != nil {
		return 0, err
	}
	var o struct {
		BackendNodeID int64 `json:"backendNodeId"`
	}
	if err := json.Unmarshal(res, &o); err != nil {
		return 0, err
	}
	return o.BackendNodeID, nil
}

// readMarks runs the cursor-pointer pass and turns its elements into
// backendNodeIds. Nil when the pass failed.
func readMarks(ctx context.Context, c *cdp.Client, session string, contextID int) *Marks {
	params := map[string]any{"expression": clickablesJS, "objectGroup": marksGroup, "returnByValue": false, "silent": true}
	if contextID != 0 {
		params["contextId"] = contextID
	}
	res, err := c.CallSession(ctx, session, "Runtime.evaluate", params)
	if err != nil {
		return nil
	}
	defer c.CallSession(context.WithoutCancel(ctx), session, "Runtime.releaseObjectGroup", map[string]any{"objectGroup": marksGroup})
	var ev struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if json.Unmarshal(res, &ev) != nil || ev.ExceptionDetails != nil || ev.Result.ObjectID == "" {
		return nil
	}
	res, err = c.CallSession(ctx, session, "Runtime.getProperties", map[string]any{"objectId": ev.Result.ObjectID, "ownProperties": true})
	if err != nil {
		return nil
	}
	var props struct {
		Result []struct {
			Name  string `json:"name"`
			Value *struct {
				ObjectID string          `json:"objectId"`
				Value    json.RawMessage `json:"value"`
			} `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(res, &props) != nil {
		return nil
	}
	type slot struct {
		object string
		text   string
	}
	items := map[int]slot{}
	max := -1
	for _, p := range props.Result {
		i, err := strconv.Atoi(p.Name)
		if err != nil || p.Value == nil {
			continue
		}
		var s string
		json.Unmarshal(p.Value.Value, &s)
		items[i] = slot{object: p.Value.ObjectID, text: s}
		if i > max {
			max = i
		}
	}
	type entry struct {
		object, kind, text string
		backend            int64
	}
	var entries []*entry
	for i := 0; i+2 <= max; i += 3 {
		entries = append(entries, &entry{object: items[i].object, kind: items[i+1].text, text: items[i+2].text})
	}
	// describeNode for each element, a few at a time.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, e := range entries {
		if e.object == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(e *entry) {
			defer wg.Done()
			defer func() { <-sem }()
			res, err := c.CallSession(ctx, session, "DOM.describeNode", map[string]any{"objectId": e.object})
			if err != nil {
				return
			}
			var d struct {
				Node struct {
					BackendNodeID int64 `json:"backendNodeId"`
				} `json:"node"`
			}
			if json.Unmarshal(res, &d) == nil {
				e.backend = d.Node.BackendNodeID
			}
		}(e)
	}
	wg.Wait()
	m := &Marks{}
	for _, e := range entries {
		switch {
		case e.backend == 0:
			if e.kind == "p" {
				return nil // a password field we cannot name: show no values at all
			}
		case e.kind == "p":
			m.Passwords = append(m.Passwords, e.backend)
		case e.kind == "c":
			m.Clickables = append(m.Clickables, Clickable{BackendNodeID: e.backend, Text: e.text})
		}
	}
	return m
}

// session returns the Tab's cached session, attaching (with focus
// emulation, ticket 01's AttachPage) and turning auto-attach on the first
// time.
func (r *Reader) session(ctx context.Context, conn *browser.Connection, targetID string) (*tabSession, error) {
	key := tabKey{conn.Client, targetID}
	r.mu.Lock()
	if r.tabs == nil {
		r.tabs = map[tabKey]*tabSession{}
	}
	for k, ts := range r.tabs {
		if !k.c.Alive() {
			delete(r.tabs, k)
			ts.close()
		}
	}
	ts := r.tabs[key]
	r.mu.Unlock()
	if ts != nil {
		return ts, nil
	}
	c := conn.Client
	id, err := conn.AttachPage(ctx, targetID)
	if err != nil {
		return nil, err
	}
	ts = &tabSession{id: id, children: map[string]string{}}
	Watch(ctx, c, id) // dialogs and file choosers (watch.go, ticket 06)
	attached := c.Subscribe(id, "Target.attachedToTarget")
	detached := c.Subscribe(id, "Target.detachedFromTarget")
	ts.subs = []*cdp.Subscription{attached, detached}
	go ts.track(c, attached, detached)
	if _, err := c.CallSession(ctx, id, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": false, "flatten": true}); err == nil {
		settle := r.Settle
		if settle == 0 {
			settle = settleAfterAutoAttach
		}
		select {
		case <-time.After(settle):
		case <-ctx.Done():
		}
	}
	r.mu.Lock()
	if old := r.tabs[key]; old != nil {
		// A concurrent read attached first; keep one.
		r.mu.Unlock()
		ts.close()
		return old, nil
	}
	r.tabs[key] = ts
	r.mu.Unlock()
	return ts, nil
}

// drop forgets a Tab's session.
func (r *Reader) drop(key tabKey) {
	r.mu.Lock()
	ts := r.tabs[key]
	delete(r.tabs, key)
	r.mu.Unlock()
	if ts != nil {
		ts.close()
		Unwatch(key.c, ts.id)
	}
}

func (ts *tabSession) close() {
	for _, s := range ts.subs {
		s.Close()
	}
}

// snapshot copies the iframe sessions known now.
func (ts *tabSession) snapshot() map[string]string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make(map[string]string, len(ts.children))
	for k, v := range ts.children {
		out[k] = v
	}
	return out
}

// track keeps the Tab's iframe sessions up to date from its auto-attach
// events, until the subscriptions end.
func (ts *tabSession) track(c *cdp.Client, attached, detached *cdp.Subscription) {
	a, d := attached.Events(), detached.Events()
	for a != nil || d != nil {
		select {
		case ev, ok := <-a:
			if !ok {
				a = nil
				continue
			}
			var p struct {
				SessionID  string `json:"sessionId"`
				TargetInfo struct {
					TargetID string `json:"targetId"`
					Type     string `json:"type"`
				} `json:"targetInfo"`
				WaitingForDebugger bool `json:"waitingForDebugger"`
			}
			if json.Unmarshal(ev.Params, &p) != nil || p.SessionID == "" || p.TargetInfo.Type != "iframe" {
				continue
			}
			ts.mu.Lock()
			ts.children[p.TargetInfo.TargetID] = p.SessionID
			ts.mu.Unlock()
			if p.WaitingForDebugger {
				go func(s string) {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					c.CallSession(ctx, s, "Runtime.runIfWaitingForDebugger", nil)
				}(p.SessionID)
			}
		case ev, ok := <-d:
			if !ok {
				d = nil
				continue
			}
			var p struct {
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(ev.Params, &p) != nil {
				continue
			}
			ts.mu.Lock()
			for k, v := range ts.children {
				if v == p.SessionID {
					delete(ts.children, k)
				}
			}
			ts.mu.Unlock()
		}
	}
}

// isGone reports a CDP failure that means the session is gone (detached,
// or unknown to the browser).
func isGone(err error) bool {
	if errors.Is(err, cdp.ErrSessionDetached) || errors.Is(err, cdp.ErrClosed) {
		return true
	}
	var ce *cdp.Error
	return errors.As(err, &ce) && strings.Contains(strings.ToLower(ce.Message), "session")
}

// Reason words why a Page could not be read, to follow "the Page is read
// through UI Automation because".
func Reason(err error) string {
	var u *Unavailable
	var none *NoTabError
	var amb *AmbiguousTabError
	var internal *InternalPageError
	switch {
	case errors.As(err, &u):
		return u.Reason
	case errors.As(err, &none):
		return none.Error()
	case errors.As(err, &amb):
		return amb.Error()
	case errors.As(err, &internal):
		return internal.Error()
	}
	return fmt.Sprintf("reading it through the Browser Connection failed: %v", err)
}
