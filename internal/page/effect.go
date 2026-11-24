package page

// What a Page click or key did (.scratch/bladebro-ideas ticket 06, after
// bladebro's observer armed before the click, bb/src/action/perform.rs:61).
// ArmEffect runs before the input: in an isolated world of the Tab's top
// frame it starts MutationObservers on the document, its open shadow roots
// and its same-origin frames, and notes the state a mutation does not show
// (a control's checked state or value, the focus, the scroll offsets). The
// Tab's own Page events tell a navigation: a committed load or a
// same-document one of the top frame, or a load it started. Read, after
// the input, waits for the DOM to stay quiet for EffectQuiet, up to
// EffectBound (and to the bound while a load is pending), then says what
// changed. It never sends input, and nothing in the Page's own world sees
// it: no Runtime.enable, no binding, no global of the Page's.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// The effects a Page Action can have.
const (
	EffectNavigated = "navigated" // the top frame loaded another document or changed its URL
	EffectChanged   = "changed"   // elements, attributes, a control's state, the focus or a scroll changed
	EffectText      = "text"      // only text changed
	EffectNone      = "none"      // nothing changed that the watch can see
	EffectUnknown   = "unknown"   // the watch could not be armed or read
)

// Effect is what an Action did to its Page.
type Effect struct {
	Kind string
	// URL is the top frame's URL after a navigation (or the URL a pending
	// load asked for, "" when the browser did not say).
	URL string
	// Pending marks a load the top frame started but did not commit by the
	// bound.
	Pending bool
	// Added and Removed count the elements a change added and removed.
	Added, Removed int
	// For EffectNone: what sits at the click point now, when that is no
	// longer the clicked element (described as aimJS describes a blocker),
	// and whether the clicked element left the document.
	Blocker string
	Gone    bool
}

// The watch's timing; tests shrink it.
var (
	// EffectQuiet is how long the DOM must stay unchanged before the read.
	EffectQuiet = 150 * time.Millisecond
	// EffectBound caps the wait, whatever the Page does.
	EffectBound = time.Second
	// effectPoll is the pause between two looks at the DOM's quiet.
	effectPoll = 40 * time.Millisecond
)

// effectWorld names the isolated world the watch runs in.
const effectWorld = "winuse-effect"

// effectArmJS starts the watch in the isolated world: px, py is the click
// point in CSS px of the Tab's viewport, or null for a key. It keeps its
// state in the world's own global, which the Page cannot see.
const effectArmJS = `function(px, py) {
  /* winuse:effect-arm */
  const S = {n: 0, added: 0, removed: 0, text: 0, attrs: 0, last: performance.now(), observers: [], blind: false, budget: 20000};
  const opts = {subtree: true, childList: true, characterData: true, attributes: true};
  const seen = new WeakSet();
  const frameDoc = el => { if (el.tagName !== 'IFRAME' && el.tagName !== 'FRAME') return undefined; try { return el.contentDocument; } catch (e) { return null; } };
  const watch = root => {
    if (seen.has(root)) return;
    seen.add(root);
    try { const mo = new MutationObserver(count); mo.observe(root, opts); S.observers.push(mo); } catch (e) {}
  };
  const watchTree = node => {
    const stack = [node];
    while (stack.length && S.budget > 0) {
      const n = stack.pop();
      const els = n.querySelectorAll ? Array.from(n.querySelectorAll('*')) : [];
      if (n.nodeType === 1) els.unshift(n);
      for (const el of els) {
        if (--S.budget <= 0) break;
        if (el.shadowRoot) { watch(el.shadowRoot); stack.push(el.shadowRoot); }
        const d = frameDoc(el);
        if (d) { watch(d); stack.push(d); }
      }
    }
  };
  function count(recs) {
    for (const r of recs) {
      if (r.type === 'characterData') { S.text++; continue; }
      if (r.type === 'attributes') { S.attrs++; continue; }
      let el = false;
      for (const n of r.addedNodes) if (n.nodeType === 1) { S.added++; el = true; watchTree(n); }
      for (const n of r.removedNodes) if (n.nodeType === 1) { S.removed++; el = true; }
      if (!el && (r.addedNodes.length || r.removedNodes.length)) S.text++;
    }
    if (recs.length) { S.n += recs.length; S.last = performance.now(); }
  }
  const hitAt = (x, y) => {
    let doc = document, el = null;
    for (let i = 0; i < 20; i++) {
      el = doc.elementFromPoint(x, y);
      while (el && el.shadowRoot) { const h = el.shadowRoot.elementFromPoint(x, y); if (!h || h === el) break; el = h; }
      if (!el) return null;
      const d = frameDoc(el);
      if (d === undefined) return el;
      if (d === null) { S.blind = true; return el; }
      const r = el.getBoundingClientRect(), cs = getComputedStyle(el);
      x -= r.left + el.clientLeft + parseFloat(cs.paddingLeft || 0); y -= r.top + el.clientTop + parseFloat(cs.paddingTop || 0); doc = d;
    }
    return el;
  };
  const active = () => {
    let a = document.activeElement;
    for (let i = 0; a && i < 20; i++) {
      const d = frameDoc(a);
      if (a.shadowRoot && a.shadowRoot.activeElement) a = a.shadowRoot.activeElement;
      else if (d && d.activeElement) a = d.activeElement;
      else break;
    }
    return a;
  };
  const read = (n, k) => { try { const v = n[k]; return v === undefined ? null : v; } catch (e) { return null; } };
  const stateOf = el => {
    if (!el || !el.closest) return [];
    const lab = el.closest('label');
    const ctl = el.closest('input,select,textarea,details,dialog,option') || (lab && lab.control);
    const out = [];
    for (const n of [el, ctl]) if (n) for (const k of ['checked', 'indeterminate', 'value', 'selectedIndex', 'open', 'selectionStart', 'selectionEnd']) out.push(read(n, k));
    return out;
  };
  const scrolls = el => {
    const out = [scrollX, scrollY];
    for (let n = el, i = 0; n && i < 50; i++) { out.push(n.scrollTop, n.scrollLeft); n = n.parentElement || (n.getRootNode && n.getRootNode().host) || null; }
    return out;
  };
  const editable = n => !!n && (n.isContentEditable || n.tagName === 'TEXTAREA' || n.tagName === 'SELECT' ||
    (n.tagName === 'INPUT' && !/^(button|submit|reset|image|checkbox|radio|file|color|range|hidden)$/i.test(n.type)));
  const holdsHit = n => { for (let c = S.hit; c; c = c.parentNode || c.host) if (c === n) return true; return false; };
  S.count = count;
  S.hitAt = hitAt;
  S.snap = () => {
    const a = active();
    return {a, hit: JSON.stringify(stateOf(S.hit)), act: JSON.stringify(stateOf(a)), sc: JSON.stringify(scrolls(S.hit || S.a0 || a))};
  };
  // A state change a mutation does not show: the clicked control's state, a
  // scroll, the focused element's value or caret, or the focus moving. Focus
  // that moved to the clicked element itself is no change, unless it takes
  // text (clicking a field is how typing starts).
  S.changed = (b, n) => {
    if (n.hit !== b.hit || n.sc !== b.sc) return true;
    if (n.a === b.a) return n.act !== b.act;
    return !(S.hit && holdsHit(n.a) && !editable(n.a));
  };
  watch(document);
  watchTree(document);
  S.px = px; S.py = py;
  S.hit = px == null ? null : hitAt(px, py);
  S.a0 = active();
  S.before = S.snap();
  globalThis.__winuseEffect = S;
  return {ok: true};
}`

// effectPollJS reports how many mutations the watch counted and how long
// ago the last one (or the arm) was, in ms.
const effectPollJS = `function() {
  /* winuse:effect-poll */
  const S = globalThis.__winuseEffect;
  if (!S) return null;
  return {n: S.n, ago: performance.now() - S.last};
}`

// effectReadJS ends the watch and reports what changed.
const effectReadJS = `function() {
  /* winuse:effect-read */
  const S = globalThis.__winuseEffect;
  if (!S) return null;
  delete globalThis.__winuseEffect;
  for (const mo of S.observers) { S.count(mo.takeRecords()); mo.disconnect(); }
  const now = S.snap();
  const out = {added: S.added, removed: S.removed, text: S.text, attrs: S.attrs, state: S.changed(S.before, now), gone: false, by: '', blind: S.blind};
  if (S.hit) {
    out.gone = !S.hit.isConnected;
    const hit = out.gone ? null : S.hitAt(S.px, S.py);
    const within = n => { for (let c = n; c; c = c.parentNode || c.host) if (c === S.hit) return true; return false; };
    if (hit && !within(hit) && !(hit.contains && hit.contains(S.hit))) {
      let s = '<' + hit.tagName.toLowerCase();
      if (hit.id) s += ' id="' + String(hit.id).slice(0, 40) + '"';
      else if (typeof hit.className === 'string' && hit.className.trim()) s += ' class="' + hit.className.trim().split(/\s+/).slice(0, 3).join(' ').slice(0, 60) + '"';
      out.by = s + '>';
    }
    out.blind = S.blind;
  }
  return out;
}`

// effectRead is effectReadJS's answer.
type effectRead struct {
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Text    int    `json:"text"`
	Attrs   int    `json:"attrs"`
	State   bool   `json:"state"`
	Gone    bool   `json:"gone"`
	By      string `json:"by"`
	Blind   bool   `json:"blind"`
}

// effect classifies what the read saw. A watch that could not see the
// click point (a cross-origin frame) and saw nothing does not know.
func (r effectRead) effect() Effect {
	switch {
	case r.Added+r.Removed+r.Attrs > 0 || r.State:
		return Effect{Kind: EffectChanged, Added: r.Added, Removed: r.Removed}
	case r.Text > 0:
		return Effect{Kind: EffectText}
	case r.Blind:
		return Effect{Kind: EffectUnknown}
	}
	return Effect{Kind: EffectNone, Blocker: r.By, Gone: r.Gone}
}

// EffectWatch is one armed watch. Read it once, after the input; Close it
// when the input failed.
type EffectWatch struct {
	c       *cdp.Client
	session string
	frameID string
	loader  string
	url     string
	world   int
	armed   bool
	ended   bool // the read script ended the observers
	sub     *cdp.Subscription
	once    sync.Once

	mu        sync.Mutex
	committed bool   // the top frame navigated
	pending   bool   // the top frame started a load
	navURL    string // where it went, or asked to go
}

// ArmEffect arms a watch on the Tab session's top frame before an Action's
// input; at is the click point in CSS px of the Tab's viewport, nil for a
// key. It never fails: a watch it could not arm reads as EffectUnknown.
// The session's Page events must be on (NewInput turns them on).
func ArmEffect(ctx context.Context, c *cdp.Client, session string, at *Point) *EffectWatch {
	w := &EffectWatch{c: c, session: session}
	if c == nil || !c.Alive() {
		return w
	}
	w.sub = c.Subscribe(session, "")
	go w.listen(w.sub)
	res, err := c.CallSession(ctx, session, "Page.getFrameTree", nil)
	if err != nil {
		return w
	}
	var ft struct {
		FrameTree frameTree `json:"frameTree"`
	}
	if json.Unmarshal(res, &ft) != nil || ft.FrameTree.Frame.ID == "" {
		return w
	}
	w.mu.Lock()
	w.frameID = ft.FrameTree.Frame.ID
	w.mu.Unlock()
	w.loader, w.url = ft.FrameTree.Frame.LoaderID, ft.FrameTree.Frame.URL
	res, err = c.CallSession(ctx, session, "Page.createIsolatedWorld", map[string]any{"frameId": ft.FrameTree.Frame.ID, "worldName": effectWorld})
	if err != nil {
		return w
	}
	var iw struct {
		ExecutionContextID int `json:"executionContextId"`
	}
	if json.Unmarshal(res, &iw) != nil || iw.ExecutionContextID == 0 {
		return w
	}
	w.world = iw.ExecutionContextID
	var args []any
	if at != nil {
		args = []any{at.X, at.Y}
	}
	var ok struct {
		OK bool `json:"ok"`
	}
	if err := w.call(ctx, effectArmJS, args, &ok); err != nil || !ok.OK {
		return w
	}
	w.armed = true
	return w
}

// listen notes the top frame's navigations from the session's events.
func (w *EffectWatch) listen(sub *cdp.Subscription) {
	for ev := range sub.Events() {
		var p struct {
			FrameID     string `json:"frameId"`
			URL         string `json:"url"`
			Disposition string `json:"disposition"`
			Frame       struct {
				ID       string `json:"id"`
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
		}
		switch ev.Method {
		case "Page.frameNavigated", "Page.navigatedWithinDocument", "Page.frameRequestedNavigation", "Page.frameStartedLoading", "Page.frameStoppedLoading":
		default:
			continue
		}
		json.Unmarshal(ev.Params, &p)
		w.mu.Lock()
		top := w.frameID
		switch ev.Method {
		case "Page.frameNavigated":
			if p.Frame.ParentID == "" && (top == "" || p.Frame.ID == top) {
				w.committed, w.navURL = true, p.Frame.URL
			}
		case "Page.navigatedWithinDocument":
			if top == "" || p.FrameID == top {
				w.committed, w.navURL = true, p.URL
			}
		case "Page.frameRequestedNavigation":
			// A link that opens a new Tab or a download asks too, with
			// another disposition: the Tab's own document stays.
			if (top == "" || p.FrameID == top) && (p.Disposition == "" || p.Disposition == "currentTab") {
				w.pending = true
				if !w.committed {
					w.navURL = p.URL
				}
			}
		case "Page.frameStartedLoading":
			if top == "" || p.FrameID == top {
				w.pending = true
			}
		case "Page.frameStoppedLoading":
			// A load that stopped with no commit was no navigation: a
			// download, or an answer with no content (204).
			if (top == "" || p.FrameID == top) && !w.committed {
				w.pending, w.navURL = false, ""
			}
		}
		w.mu.Unlock()
	}
}

// navigation is what the events said so far.
func (w *EffectWatch) navigation() (e Effect, committed, pending bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Effect{Kind: EffectNavigated, URL: w.navURL, Pending: !w.committed}, w.committed, w.pending
}

// frameMoved reads the top frame again: another loader or URL than the
// arm saw is a navigation the events did not show.
func (w *EffectWatch) frameMoved(ctx context.Context) (Effect, bool) {
	res, err := w.c.CallSession(ctx, w.session, "Page.getFrameTree", nil)
	if err != nil {
		return Effect{}, false
	}
	var ft struct {
		FrameTree frameTree `json:"frameTree"`
	}
	if json.Unmarshal(res, &ft) != nil || ft.FrameTree.Frame.ID == "" {
		return Effect{}, false
	}
	f := ft.FrameTree.Frame
	if f.LoaderID != w.loader || f.URL != w.url {
		return Effect{Kind: EffectNavigated, URL: f.URL}, true
	}
	return Effect{}, false
}

// call runs fn in the watch's isolated world with args and decodes its
// value.
func (w *EffectWatch) call(ctx context.Context, fn string, args []any, out any) error {
	callArgs := make([]map[string]any, len(args))
	for i, a := range args {
		callArgs[i] = map[string]any{"value": a}
	}
	res, err := w.c.CallSession(ctx, w.session, "Runtime.callFunctionOn", map[string]any{
		"functionDeclaration": fn, "executionContextId": w.world, "arguments": callArgs, "returnByValue": true, "silent": true,
	})
	if err != nil {
		return err
	}
	var cr struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &cr); err != nil {
		return err
	}
	if cr.ExceptionDetails != nil {
		return errors.New("the effect watch failed: " + cr.ExceptionDetails.Text)
	}
	if len(cr.Result.Value) == 0 || string(cr.Result.Value) == "null" {
		return errors.New("the effect watch is gone")
	}
	return json.Unmarshal(cr.Result.Value, out)
}

// Read waits for the Page to settle after the input and says what changed;
// it closes the watch. A committed navigation answers at once. Otherwise it
// reads once the DOM was quiet for EffectQuiet, at EffectBound at the
// latest, or at the bound while a load the top frame started is pending.
func (w *EffectWatch) Read(ctx context.Context) Effect {
	defer w.Close()
	if !w.armed {
		return Effect{Kind: EffectUnknown}
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, EffectBound+time.Second)
	defer cancel()
	quietMS := float64(EffectQuiet) / float64(time.Millisecond)
	for {
		if e, committed, _ := w.navigation(); committed {
			return e
		}
		var p struct {
			N   int     `json:"n"`
			Ago float64 `json:"ago"`
		}
		if err := w.call(ctx, effectPollJS, nil, &p); err != nil {
			return w.lost(ctx)
		}
		_, _, pending := w.navigation()
		since := time.Since(start)
		if (!pending && since >= EffectQuiet && p.Ago >= quietMS) || since >= EffectBound {
			break
		}
		select {
		case <-ctx.Done():
			return Effect{Kind: EffectUnknown}
		case <-time.After(effectPoll):
		}
	}
	if e, committed, pending := w.navigation(); committed || pending {
		return e
	}
	if e, ok := w.frameMoved(ctx); ok {
		return e
	}
	var r effectRead
	if err := w.call(ctx, effectReadJS, nil, &r); err != nil {
		return w.lost(ctx)
	}
	w.ended = true
	return r.effect()
}

// lost answers a watch whose world went away: a navigation if one shows,
// else unknown.
func (w *EffectWatch) lost(ctx context.Context) Effect {
	if e, committed, pending := w.navigation(); committed || pending {
		return e
	}
	if e, ok := w.frameMoved(ctx); ok {
		return e
	}
	// The commit's event may trail the world's end by a moment.
	select {
	case <-ctx.Done():
	case <-time.After(2 * effectPoll):
	}
	if e, committed, _ := w.navigation(); committed {
		return e
	}
	return Effect{Kind: EffectUnknown}
}

// Close ends the watch: its event subscription, and its observers when
// they are still on (a failed input that never read).
func (w *EffectWatch) Close() {
	w.once.Do(func() {
		if w.sub != nil {
			w.sub.Close()
		}
		if w.armed && !w.ended && w.c.Alive() {
			// In the background: a Page a dialog holds answers only once the
			// dialog is answered, and the Action does not wait for that.
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				w.c.CallSession(ctx, w.session, "Runtime.callFunctionOn", map[string]any{
					"functionDeclaration": effectEndJS, "executionContextId": w.world, "returnByValue": true, "silent": true,
				})
			}()
		}
	})
}

// effectEndJS stops the watch's observers, if Read did not.
const effectEndJS = `function() {
  /* winuse:effect-end */
  const S = globalThis.__winuseEffect;
  if (!S) return null;
  delete globalThis.__winuseEffect;
  for (const mo of S.observers) mo.disconnect();
  return true;
}`
