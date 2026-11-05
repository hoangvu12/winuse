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

// attachTimeout bounds attaching to a Tab. An attach takes well under a
// second; a Tab that has not attached in this long does not answer, and
// the read fails with *NotAnsweringError instead of waiting out its
// request.
var attachTimeout = 10 * time.Second

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
	// JSMarks finds role-less clickables and password fields with the
	// clickables.js pass instead of DOMSnapshot.captureSnapshot. A read
	// falls back to the pass on its own where the browser refuses the
	// snapshot; tests and the live comparison set it.
	JSMarks bool

	mu         sync.Mutex
	tabs       map[tabKey]*tabSession
	noSnapshot map[*cdp.Client]bool // connections that refused DOMSnapshot
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
	children map[string]string    // iframe target id (= its frame id) to its session id
	read     map[string]time.Time // session to its last read, once its domains are on
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

// How long FindTabSettled keeps trying while no Tab matches.
const (
	tabSettle      = time.Second
	tabSettleEvery = 150 * time.Millisecond
)

// FindTabSettled is FindTab for an Action: while no Tab matches (a
// *NoTabError), it reads the Window (geo) and the Tabs again for up to
// tabSettle. Right after a navigation the Window's caption and the Tab's
// title change at different moments, and the first match failed
// ("no Tab ... matches"), the next one a moment later worked
// (browser-ux 05). Any other answer returns at once.
func FindTabSettled(ctx context.Context, conn *browser.Connection, geo func() Geometry) (Tab, error) {
	deadline := time.Now().Add(tabSettle)
	for {
		tab, err := FindTab(ctx, conn, geo())
		var none *NoTabError
		if !errors.As(err, &none) || !time.Now().Before(deadline) {
			return tab, err
		}
		select {
		case <-ctx.Done():
			return tab, err
		case <-time.After(tabSettleEvery):
		}
	}
}

// windowTab is FindTab, also returning every Tab it listed.
func windowTab(ctx context.Context, conn *browser.Connection, g Geometry) (Tab, []Tab, error) {
	// On the extension the windows' bounds are asked for alongside the Tabs.
	var windows map[int]DIPBounds
	var werr error
	wdone := make(chan struct{})
	if conn.Mode == browser.ModeExtension {
		go func() {
			defer close(wdone)
			windows, werr = extensionWindows(ctx, conn.Client)
		}()
	} else {
		close(wdone)
	}
	tabs, err := pageTabs(ctx, conn.Client)
	<-wdone
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
		if werr != nil {
			return Tab{}, nil, werr
		}
		for i := range tabs {
			if b, ok := windows[tabs[i].WindowID]; ok {
				tabs[i].Bounds, tabs[i].HasBounds = b, true
			}
		}
		g.Memory.prune(conn.Client, windows)
	} else {
		// The bounds of every Tab the title does not rule out, asked for
		// together.
		ws := make([]*cdp.Pending, len(tabs))
		for i := range tabs {
			if TitleMatches(g.Title, tabs[i].Title, conn.Browser) {
				ws[i] = conn.Client.Send("", "Browser.getWindowForTarget", map[string]any{"targetId": tabs[i].TargetID})
			}
		}
		for i, p := range ws {
			if p == nil {
				continue
			}
			w, err := windowOf(p.Wait(ctx))
			if err != nil {
				if ctx.Err() != nil {
					giveUp(ws[i:]...)
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
	return windowOf(c.Call(ctx, "Browser.getWindowForTarget", map[string]any{"targetId": targetID}))
}

// windowOf reads Browser.getWindowForTarget's answer.
func windowOf(res json.RawMessage, err error) (cdpWindow, error) {
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

// readTab reads one Tab's Page over its (cached) session, in as few round
// trips as the answers allow (speed issue 07): the Tab's document and
// every cross-origin iframe's are read at once, each document's commands
// sent together (readDoc), and the same-origin iframes the Tab's tree left
// out are read together after it. The dialog probe goes alongside.
func (r *Reader) readTab(ctx context.Context, conn *browser.Connection, tab Tab) (*Raw, error) {
	c := conn.Client
	ts, err := r.session(ctx, conn, tab.TargetID)
	if err != nil {
		return nil, err
	}
	// A Page a JavaScript dialog holds answers none of the read's calls
	// until the dialog is answered (watch.go, ticket 06): fail at once on a
	// dialog the watch saw, and within AnswerTimeout on one it missed; the
	// read's commands, sent meanwhile, are then given up.
	if d := OpenDialog(c, ts.id); d != nil {
		return nil, &DialogOpenError{Dialog: *d}
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(chan error, 1)
	go func() {
		err := Ready(ctx, c, ts.id)
		if err != nil {
			cancel()
		}
		ready <- err
	}()
	snap := !r.JSMarks && !r.refused(c)

	// Cross-origin iframes, one level deep, each through its own session,
	// alongside the Tab's own document: the iframe element that holds it
	// (in the Tab's session) and its document.
	type frameRead struct {
		owner int64
		doc   Doc
		err   error
	}
	children := ts.snapshot()
	own := map[string]chan frameRead{}
	for id, session := range children {
		ch := make(chan frameRead, 1)
		own[id] = ch
		go func(id, session string) {
			owner := c.Send(ts.id, "DOM.getFrameOwner", map[string]any{"frameId": id})
			doc, err := r.readDoc(rctx, c, ts, session, snap)
			o, oerr := ownerOf(owner.Wait(rctx))
			if err == nil {
				err = oerr
			}
			ch <- frameRead{owner: o, doc: doc.Doc, err: err}
		}(id, session)
	}

	main, err := r.readDoc(rctx, c, ts, ts.id, snap)
	if rerr := <-ready; rerr != nil {
		return nil, rerr
	}
	if err != nil {
		return nil, err
	}
	raw := &Raw{Browser: conn.Browser, TargetID: tab.TargetID, Title: tab.Title, URL: tab.URL, Main: main.Doc, Client: c}

	// The same-origin iframes the Tab's tree left out, read through the
	// Tab's session with their frameIds, all at once.
	inTree := map[string]bool{}
	for _, n := range main.Nodes {
		if n.FrameID != "" {
			inTree[n.FrameID] = true
		}
	}
	var frameIDs []string
	for _, cf := range main.tree.ChildFrames {
		frameIDs = append(frameIDs, cf.Frame.ID)
	}
	var extra []string
	for id := range children {
		extra = append(extra, id)
	}
	sort.Strings(extra)
	frameIDs = append(frameIDs, extra...)
	reads := map[string]chan frameRead{}
	for _, id := range frameIDs {
		if _, ok := own[id]; ok || inTree[id] || reads[id] != nil {
			continue
		}
		ch := make(chan frameRead, 1)
		reads[id] = ch
		go func(id string) {
			doc, owner, err := r.sameProcessFrame(rctx, c, ts.id, id, main)
			ch <- frameRead{owner: owner, doc: doc, err: err}
		}(id)
	}
	done := map[string]bool{}
	for _, id := range frameIDs {
		if done[id] {
			continue
		}
		done[id] = true
		ch := own[id]
		if ch == nil {
			ch = reads[id]
		}
		if ch == nil {
			continue // in the Tab's tree already
		}
		f := <-ch
		if f.err != nil || f.owner == 0 {
			continue // the iframe is left out; the rest of the Page stands
		}
		raw.Frames = append(raw.Frames, Frame{Owner: f.owner, Doc: f.doc})
	}
	return raw, nil
}

// docResult is one session's document read: its Doc, its frame tree, and
// the snapshot its marks came from (nil when they came from the pass).
type docResult struct {
	Doc
	tree frameTree
	snap *domSnapshot
}

// domains are turned on once per session (enableAgain).
var domains = []string{"DOM.enable", "Accessibility.enable"}

// enableAgain is how long after a session's last read its domains are
// turned on again. The extension detaches a Tab nobody drove for 5 minutes
// and attaches it again on the next command without telling winuse
// (background.js IDLE_DETACH_MS), and a new attachment has every domain
// off; every read is such a command, so within this a session is still the
// one turned on.
const enableAgain = 4 * time.Minute

// readDoc reads one session's document in one round trip: its domains on
// (the first read of the session, and after enableAgain), the frame tree's
// loader ids, the accessibility tree, and with snap
// DOMSnapshot.captureSnapshot for the marks. The commands go out together,
// in that order, which the browser keeps. Without a usable snapshot the
// marks come from the clickables.js pass, round trips of its own.
func (r *Reader) readDoc(ctx context.Context, c *cdp.Client, ts *tabSession, session string, snap bool) (docResult, error) {
	var ons []*cdp.Pending
	if ts.turnOn(session) {
		for _, m := range domains {
			ons = append(ons, c.Send(session, m, nil))
		}
	}
	tree := c.Send(session, "Page.getFrameTree", nil)
	ax := c.Send(session, "Accessibility.getFullAXTree", nil)
	var shot *cdp.Pending
	if snap {
		shot = c.Send(session, "DOMSnapshot.captureSnapshot", snapshotParams)
	}
	all := append(ons, tree, ax, shot)
	fail := func(err error) (docResult, error) {
		ts.forget(session)
		giveUp(all...)
		return docResult{}, err
	}
	for _, p := range ons {
		if _, err := p.Wait(ctx); err != nil && isGone(err) {
			return fail(err)
		}
	}
	res, err := tree.Wait(ctx)
	if err != nil {
		return fail(err)
	}
	var f struct {
		FrameTree frameTree `json:"frameTree"`
	}
	if err := json.Unmarshal(res, &f); err != nil {
		return fail(err)
	}
	d := docResult{Doc: Doc{SessionID: session, FrameID: f.FrameTree.Frame.ID, Loaders: map[string]string{}}, tree: f.FrameTree}
	f.FrameTree.loaders(d.Loaders, -1)
	if res, err = ax.Wait(ctx); err != nil {
		return fail(err)
	}
	var axTree struct {
		Nodes []AXNode `json:"nodes"`
	}
	if err := json.Unmarshal(res, &axTree); err != nil {
		return fail(err)
	}
	d.Nodes = axTree.Nodes
	if shot != nil {
		res, err := shot.Wait(ctx)
		if err == nil {
			d.snap, err = decodeSnapshot(res)
		}
		if doc := d.snap.doc(d.FrameID); doc != nil {
			d.Marks = marksOf(d.snap, doc)
		}
		if d.Marks != nil {
			d.Snapshot = true
		} else {
			d.snap = nil
			r.refuse(c, err)
		}
	}
	if d.Marks == nil {
		d.Marks = readMarks(ctx, c, session, 0)
	}
	return d, nil
}

// sameProcessFrame reads a same-origin iframe the main tree left out: the
// iframe element that holds it, and its accessibility tree by frameId; its
// marks are its document in the Tab's snapshot, else the clickables.js
// pass in an isolated world of that frame.
func (r *Reader) sameProcessFrame(ctx context.Context, c *cdp.Client, session, frameID string, main docResult) (Doc, int64, error) {
	owner := c.Send(session, "DOM.getFrameOwner", map[string]any{"frameId": frameID})
	ax := c.Send(session, "Accessibility.getFullAXTree", map[string]any{"frameId": frameID})
	o, err := ownerOf(owner.Wait(ctx))
	if err != nil || o == 0 {
		giveUp(ax)
		return Doc{}, 0, err
	}
	res, err := ax.Wait(ctx)
	if err != nil {
		return Doc{}, 0, err
	}
	var tree struct {
		Nodes []AXNode `json:"nodes"`
	}
	if err := json.Unmarshal(res, &tree); err != nil {
		return Doc{}, 0, err
	}
	doc := Doc{SessionID: session, FrameID: frameID, Loaders: map[string]string{}, Nodes: tree.Nodes}
	for k, v := range main.Loaders {
		doc.Loaders[k] = v
	}
	if sd := main.snap.doc(frameID); sd != nil {
		doc.Marks = marksOf(main.snap, sd)
		doc.Snapshot = doc.Marks != nil
	}
	if doc.Marks != nil {
		return doc, o, nil
	}
	if res, err := c.CallSession(ctx, session, "Page.createIsolatedWorld", map[string]any{"frameId": frameID, "worldName": "winuse"}); err == nil {
		var w struct {
			ExecutionContextID int `json:"executionContextId"`
		}
		json.Unmarshal(res, &w)
		if w.ExecutionContextID != 0 {
			doc.Marks = readMarks(ctx, c, session, w.ExecutionContextID)
		}
	}
	return doc, o, nil
}

// giveUp stops waiting for answers a failed read no longer needs.
func giveUp(ps ...*cdp.Pending) {
	done, cancel := context.WithCancel(context.Background())
	cancel()
	for _, p := range ps {
		if p != nil {
			p.Wait(done)
		}
	}
}

// turnOn reports whether a read over session must turn its domains on
// (its first, or enableAgain after the last), and records the read.
func (ts *tabSession) turnOn(session string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	last, ok := ts.read[session]
	ts.read[session] = time.Now()
	return !ok || time.Since(last) > enableAgain
}

// forget makes the next read over session turn its domains on again.
func (ts *tabSession) forget(session string) {
	ts.mu.Lock()
	delete(ts.read, session)
	ts.mu.Unlock()
}

// refused reports a connection that refused DOMSnapshot.captureSnapshot.
func (r *Reader) refused(c *cdp.Client) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.noSnapshot[c]
}

// refuse remembers a connection whose browser does not offer
// DOMSnapshot.captureSnapshot (no such method, or the extension refused
// it), so its later reads use the clickables.js pass from the start. Any
// other failure (a navigation under way, a timeout) is this read's alone.
func (r *Reader) refuse(c *cdp.Client, err error) {
	var ce *cdp.Error
	if !errors.As(err, &ce) {
		return
	}
	msg := strings.ToLower(ce.Message)
	if ce.Code != -32601 && ce.Code != bridge.CodeRefused && !strings.Contains(msg, "wasn't found") && !strings.Contains(msg, "not allowed") {
		return
	}
	r.mu.Lock()
	if r.noSnapshot == nil {
		r.noSnapshot = map[*cdp.Client]bool{}
	}
	r.noSnapshot[c] = true
	r.mu.Unlock()
}

// ownerOf reads DOM.getFrameOwner's answer.
func ownerOf(res json.RawMessage, err error) (int64, error) {
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
	// Bounded: a Tab whose renderer does not answer (hung, or held by a
	// dialog) can keep Target.attachToTarget waiting for minutes (a
	// headless Chrome tab did in the speed issue 07 live check).
	actx, cancel := context.WithTimeout(ctx, attachTimeout)
	id, err := conn.AttachPage(actx, targetID)
	cancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, &NotAnsweringError{After: attachTimeout}
		}
		return nil, err
	}
	ts = &tabSession{id: id, children: map[string]string{}, read: map[string]time.Time{}}
	Watch(ctx, c, id) // dialogs and file choosers (watch.go, ticket 06)
	attached := c.Subscribe(id, "Target.attachedToTarget")
	detached := c.Subscribe(id, "Target.detachedFromTarget")
	ts.subs = []*cdp.Subscription{attached, detached}
	go ts.track(c, attached, detached)
	sctx, scancel := context.WithTimeout(ctx, setupTimeout)
	_, err = c.CallSession(sctx, id, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": false, "flatten": true})
	scancel()
	if err == nil {
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
