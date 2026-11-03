package page

// Page input (browser merge ticket 05, spec "Routing and page Actions"):
// what a Page Action sends over the Browser Connection, and the checks it
// makes before sending anything.
//
//   - Aim scrolls a node into view (DOM.scrollIntoViewIfNeeded) and reads
//     where it is, in CSS px of the Tab's viewport, with the
//     actionability checks a click needs: enabled, visible with a size,
//     inside the viewport, and not covered at its centre (a hit test in
//     the node's own frame and in every frame above it). A failed check
//     is a *NotActionableError naming the reason, and the blocker when
//     something covers the node. Nothing is sent to the Page's input.
//   - Input sends trusted input (Input.dispatchMouseEvent,
//     Input.dispatchKeyEvent, Input.insertText) into the Tab's own
//     session, and races every event against Page.javascriptDialogOpening:
//     an event whose handler opens an alert, confirm, prompt or
//     beforeunload dialog never answers until the dialog closes, so the
//     dialog is reported (*DialogError) instead of waiting.
//   - KeyEvents maps winuse's key names (internal/keys) to CDP key events
//     with modifiers.
//   - ToScreen and ToCSS are the Agent Cursor transform (probe-results.md
//     section 3): screen px = the UIA document Element's top-left + CSS px
//     x devicePixelRatio, and its inverse.
//
// The JS in this file is winuse's own; nothing was copied from
// agent-browser or use-browser.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/linebreak"
)

// Point is a point in CSS px of the Tab's viewport.
type Point struct{ X, Y float64 }

// Aim is where a Page Action lands, read after the node was scrolled into
// view: its box and centre in CSS px of the Tab's viewport (iframe offsets
// added), the page's devicePixelRatio (in Rect.DPR), and a short name for
// the Agent Cursor's label (never a field's value).
type Aim struct {
	Rect  Rect
	Point Point
	Name  string
}

// AimOptions are the checks Aim makes. Every check runs before anything
// reaches the Page's input.
type AimOptions struct {
	Scroll  bool // DOM.scrollIntoViewIfNeeded first
	Enabled bool // refuse a disabled node (:disabled, aria-disabled, inert)
	Visible bool // refuse a hidden or empty node, and a centre outside the viewport
	HitTest bool // refuse a node covered at its centre, naming what covers it
}

// The reasons a node is not actionable.
const (
	WhyDisabled = "disabled"
	WhyHidden   = "hidden"
	WhyOutside  = "outside"
	WhyCovered  = "covered"
)

// NotActionableError says why a node cannot take the Action. It is raised
// before any input is sent.
type NotActionableError struct {
	Why     string // WhyDisabled, WhyHidden, WhyOutside or WhyCovered
	Blocker string // for WhyCovered: the covering element, like <div class="cookie-banner">
}

func (e *NotActionableError) Error() string {
	switch e.Why {
	case WhyDisabled:
		return "it is disabled"
	case WhyHidden:
		return "it is hidden or has no size"
	case WhyOutside:
		return "its centre is outside the visible part of the Page, even after scrolling it into view"
	case WhyCovered:
		return "it is covered by " + e.Blocker + " at its centre"
	}
	return "it cannot be acted on"
}

// aimJS reads a node's box, its centre and the actionability checks in
// its own frame, then walks up the same-origin frames above it, adding
// each iframe's content-box offset and hit-testing the iframe element at
// the point. With px and py set it checks that point (in the node's frame)
// instead of the centre: the iframe element of a cross-origin iframe is
// checked that way, at the point inside it.
const aimJS = `function(needEnabled, hitTest, px, py) {
  /* winuse:aim */
  const el = this.nodeType === 1 ? this : this.parentElement;
  if (!el || !el.isConnected) return {state: 'gone'};
  const describe = n => {
    if (!n || !n.tagName) return 'something';
    let s = '<' + n.tagName.toLowerCase();
    if (n.id) s += ' id="' + String(n.id).slice(0, 40) + '"';
    else if (typeof n.className === 'string' && n.className.trim()) s += ' class="' + n.className.trim().split(/\s+/).slice(0, 3).join(' ').slice(0, 60) + '"';
    return s + '>';
  };
  const label = () => {
    let t = el.getAttribute('aria-label') || el.getAttribute('title') || el.getAttribute('placeholder') || '';
    if (!t && el.labels && el.labels.length) t = el.labels[0].innerText || '';
    if (!t && el.tagName !== 'INPUT' && el.tagName !== 'TEXTAREA' && !el.isContentEditable) t = el.innerText || '';
    return String(t).replace(/\s+/g, ' ').trim().slice(0, 60);
  };
  const r = el.getBoundingClientRect();
  const out = {l: r.left, t: r.top, w: r.width, h: r.height, ox: 0, oy: 0, chain: true, dpr: window.devicePixelRatio, name: label()};
  out.x = px == null ? r.left + r.width / 2 : px;
  out.y = py == null ? r.top + r.height / 2 : py;
  if (needEnabled && (el.matches(':disabled') || el.getAttribute('aria-disabled') === 'true' || el.closest('[inert]'))) return Object.assign(out, {state: 'disabled'});
  const cs = getComputedStyle(el);
  if (cs.visibility === 'hidden' || cs.display === 'none' || r.width <= 0 || r.height <= 0) return Object.assign(out, {state: 'hidden'});
  const de = document.documentElement;
  const vw = de && de.clientWidth > 0 ? Math.min(de.clientWidth, innerWidth) : innerWidth;
  const vh = de && de.clientHeight > 0 ? Math.min(de.clientHeight, innerHeight) : innerHeight;
  if (out.x < 0 || out.y < 0 || out.x >= vw || out.y >= vh) return Object.assign(out, {state: 'outside'});
  if (hitTest) {
    let hit = el.ownerDocument.elementFromPoint(out.x, out.y);
    while (hit && hit.shadowRoot) {
      const h = hit.shadowRoot.elementFromPoint(out.x, out.y);
      if (!h || h === hit) break;
      hit = h;
    }
    if (!hit) return Object.assign(out, {state: 'outside'});
    const within = n => { for (let c = n; c; c = c.parentNode || c.host) if (c === el) return true; return false; };
    const lab = hit.closest && hit.closest('label');
    if (!within(hit) && !(lab && lab.control === el)) return Object.assign(out, {state: 'covered', by: describe(hit)});
  }
  let w = window, cx = out.x, cy = out.y;
  while (w !== w.parent) {
    let fe = null;
    try { fe = w.frameElement; } catch (e) {}
    if (!fe) { out.chain = false; break; }
    const fr = fe.getBoundingClientRect(), fs = fe.ownerDocument.defaultView.getComputedStyle(fe);
    const dx = fr.left + fe.clientLeft + parseFloat(fs.paddingLeft || 0), dy = fr.top + fe.clientTop + parseFloat(fs.paddingTop || 0);
    cx += dx; cy += dy; out.ox += dx; out.oy += dy;
    if (hitTest) {
      const hit = fe.ownerDocument.elementFromPoint(cx, cy);
      if (!hit) return Object.assign(out, {state: 'outside'});
      if (hit !== fe) return Object.assign(out, {state: 'covered', by: describe(hit)});
    }
    w = fe.ownerDocument.defaultView;
  }
  return Object.assign(out, {state: 'ok'});
}`

// aimState is aimJS's answer.
type aimState struct {
	State string  `json:"state"`
	By    string  `json:"by"`
	L     float64 `json:"l"`
	T     float64 `json:"t"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	OX    float64 `json:"ox"`
	OY    float64 `json:"oy"`
	Chain bool    `json:"chain"`
	DPR   float64 `json:"dpr"`
	Name  string  `json:"name"`
}

// refusal turns a check that failed into its error, for the checks opt
// asks for; nil when the state passes them.
func (st aimState) refusal(opt AimOptions) error {
	switch st.State {
	case "gone":
		return fmt.Errorf("%w: the node left the document", ErrStale)
	case "disabled":
		if opt.Enabled {
			return &NotActionableError{Why: WhyDisabled}
		}
	case "hidden":
		if opt.Visible {
			return &NotActionableError{Why: WhyHidden}
		}
	case "outside":
		if opt.Visible {
			return &NotActionableError{Why: WhyOutside}
		}
	case "covered":
		if opt.HitTest {
			return &NotActionableError{Why: WhyCovered, Blocker: st.By}
		}
	}
	return nil
}

// Aim scrolls the node into view (opt.Scroll) and reads where it is, with
// the checks opt asks for. A node in a cross-origin iframe is checked in
// its own session, then its iframe element in the Tab's session at the
// same point.
func (o Origin) Aim(ctx context.Context, opt AimOptions) (Aim, error) {
	c, err := o.caller()
	if err != nil {
		return Aim{}, err
	}
	return aim(ctx, c, o, opt)
}

func aim(ctx context.Context, c Caller, o Origin, opt AimOptions) (Aim, error) {
	if opt.Scroll {
		if _, err := c.CallSession(ctx, o.SessionID, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": o.BackendNodeID}); err != nil {
			if err := staleOr(err); errors.Is(err, ErrStale) {
				return Aim{}, err
			}
			// A node with no layout object cannot be scrolled to: it is
			// hidden (display: none, or detached from the rendering).
			var ce *cdp.Error
			if errors.As(err, &ce) && opt.Visible {
				return Aim{}, &NotActionableError{Why: WhyHidden}
			}
			if !errors.As(err, &ce) {
				return Aim{}, err
			}
		}
	}
	var st aimState
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, aimJS, []any{opt.Enabled, opt.HitTest, nil, nil}, &st); err != nil {
		return Aim{}, staleOr(err)
	}
	if err := st.refusal(opt); err != nil {
		return Aim{}, err
	}
	a := Aim{Name: st.Name}
	offX, offY, dpr := st.OX, st.OY, st.DPR
	switch {
	case o.Owner != nil:
		// A cross-origin iframe's node: its frame chain stops at the
		// iframe's own document; the iframe element sits in the Tab's
		// session. Check it at the node's point, and add its offset.
		var off [3]float64
		if err := callOn(ctx, c, o.Owner.SessionID, o.Owner.BackendNodeID, contentJS, &off); err != nil {
			return Aim{}, staleOr(err)
		}
		var ost aimState
		px, py := off[0]+st.X+st.OX, off[1]+st.Y+st.OY
		if err := callWith(ctx, c, o.Owner.SessionID, o.Owner.BackendNodeID, aimJS, []any{false, opt.HitTest, px, py}, &ost); err != nil {
			return Aim{}, staleOr(err)
		}
		if err := ost.refusal(AimOptions{Visible: opt.Visible, HitTest: opt.HitTest}); err != nil {
			return Aim{}, err
		}
		offX = st.OX + off[0] + ost.OX
		offY = st.OY + off[1] + ost.OY
		dpr = ost.DPR
	case !st.Chain:
		// A same-process iframe the page cannot see into (another origin
		// of the same site): its offset comes from the node's content
		// quads, which are in the Tab's viewport.
		qx, qy, err := quadTopLeft(ctx, c, o.SessionID, o.BackendNodeID)
		if err != nil {
			return Aim{}, staleOr(err)
		}
		offX, offY = qx-st.L, qy-st.T
	}
	a.Rect = Rect{Left: st.L + offX, Top: st.T + offY, Width: st.W, Height: st.H, DPR: dpr}
	a.Point = Point{X: st.X + offX, Y: st.Y + offY}
	return a, nil
}

// quadTopLeft is the top-left of a node's content quads' bounding box.
func quadTopLeft(ctx context.Context, c Caller, session string, backendNodeID int64) (float64, float64, error) {
	res, err := c.CallSession(ctx, session, "DOM.getContentQuads", map[string]any{"backendNodeId": backendNodeID})
	if err != nil {
		return 0, 0, err
	}
	var q struct {
		Quads [][]float64 `json:"quads"`
	}
	if err := json.Unmarshal(res, &q); err != nil {
		return 0, 0, err
	}
	x, y := math.Inf(1), math.Inf(1)
	for _, quad := range q.Quads {
		for i := 0; i+1 < len(quad); i += 2 {
			x, y = math.Min(x, quad[i]), math.Min(y, quad[i+1])
		}
	}
	if math.IsInf(x, 1) {
		return 0, 0, &NotActionableError{Why: WhyHidden}
	}
	return x, y, nil
}

// callWith is callOn with arguments: it resolves a backendNodeId in its
// session, calls fn on it with args (JSON values; nil is undefined), and
// decodes the returned value. The object is released.
func callWith(ctx context.Context, c Caller, session string, backendNodeID int64, fn string, args []any, out any) error {
	res, err := c.CallSession(ctx, session, "DOM.resolveNode", map[string]any{"backendNodeId": backendNodeID, "objectGroup": objectGroup})
	if err != nil {
		return err
	}
	var rn struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if json.Unmarshal(res, &rn) != nil || rn.Object.ObjectID == "" {
		return fmt.Errorf("%w: the node no longer resolves", ErrStale)
	}
	defer c.CallSession(context.WithoutCancel(ctx), session, "Runtime.releaseObject", map[string]any{"objectId": rn.Object.ObjectID})
	callArgs := make([]map[string]any, len(args))
	for i, a := range args {
		if a == nil {
			callArgs[i] = map[string]any{}
			continue
		}
		callArgs[i] = map[string]any{"value": a}
	}
	res, err = c.CallSession(ctx, session, "Runtime.callFunctionOn", map[string]any{
		"objectId": rn.Object.ObjectID, "functionDeclaration": fn, "arguments": callArgs, "returnByValue": true, "silent": true,
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
		return fmt.Errorf("reading the node failed: %s", cr.ExceptionDetails.Text)
	}
	if out == nil || len(cr.Result.Value) == 0 {
		return nil
	}
	return json.Unmarshal(cr.Result.Value, out)
}

// TopSession is the Tab's own session: Input events go there, in CSS px
// of the Tab's viewport, whatever frame the node is in.
func (o Origin) TopSession() string {
	if o.Owner != nil {
		return o.Owner.SessionID
	}
	return o.SessionID
}

// Input errors.
var (
	// ErrStopped reports that the Esc stop fired between two input
	// events; Input.Sent says whether anything went out before it.
	ErrStopped = errors.New("stopped by the Esc key")
)

// DialogError reports that an input event opened a JavaScript dialog: the
// Page waits for an answer and takes no other input until then.
type DialogError struct {
	Type    string // alert, confirm, prompt or beforeunload
	Message string
}

func (e *DialogError) Error() string {
	msg := strings.Join(strings.Fields(e.Message), " ")
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	if msg == "" {
		return fmt.Sprintf("the Page opened %s %s dialog", article(e.Type), e.Type)
	}
	return fmt.Sprintf("the Page opened %s %s dialog: %q", article(e.Type), e.Type, msg)
}

func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}

// SendError is an input event the Page did not take: the browser refused
// it, the connection failed, or the Page did not answer in time. Sent is
// how many events had gone out before (0: nothing reached the Page).
type SendError struct {
	Sent    int
	NoReply bool // the Page did not answer within the event timeout
	Err     error
}

func (e *SendError) Error() string {
	if e.NoReply {
		return "the Page did not answer the input in time; it may be busy, or a dialog may be open"
	}
	return "sending input to the Page failed: " + e.Err.Error()
}

func (e *SendError) Unwrap() error { return e.Err }

// VerifyError is a read-back that does not match: the input was delivered,
// so repeating it could apply it twice.
type VerifyError struct{ Msg string }

func (e *VerifyError) Error() string { return e.Msg }

// EventTimeout bounds how long one input event may wait for the Page's
// answer. An event whose handler opens a dialog is reported by the dialog
// race long before; this is for a Page that hangs, or a dialog that was
// already open before the Action.
const EventTimeout = 5 * time.Second

// Input sends trusted input into one Tab, racing each event against the
// Page's JavaScript dialogs. Close it when done.
type Input struct {
	c       *cdp.Client
	session string
	stopped func() bool
	timeout time.Duration
	subs    []*cdp.Subscription
	dialogs chan cdp.Event
	done    chan struct{}
	once    sync.Once

	// Sent counts the events that reached the Page (or may have).
	Sent int
}

// NewInput starts input into the Tab session, watching for dialogs there
// and in the other sessions given (a cross-origin iframe's). Page.enable
// runs on each so the dialog events flow; it sends nothing to the Page's
// input. stopped is the Esc checkpoint polled before each event.
func NewInput(ctx context.Context, c *cdp.Client, session string, stopped func() bool, also ...string) (*Input, error) {
	if c == nil || !c.Alive() {
		return nil, fmt.Errorf("%w: the Browser Connection ended", ErrStale)
	}
	if d := OpenDialog(c, session); d != nil {
		return nil, &DialogOpenError{Dialog: *d} // watch.go: fail at once, nothing sent
	}
	in := &Input{c: c, session: session, stopped: stopped, timeout: EventTimeout, dialogs: make(chan cdp.Event, 4), done: make(chan struct{})}
	sessions := []string{session}
	for _, s := range also {
		if s != "" && s != session {
			sessions = append(sessions, s)
		}
	}
	for _, s := range sessions {
		sub := c.Subscribe(s, "Page.javascriptDialogOpening")
		in.subs = append(in.subs, sub)
		go func() {
			for ev := range sub.Events() {
				select {
				case in.dialogs <- ev:
				case <-in.done:
					return
				}
			}
		}()
	}
	for _, s := range sessions {
		if _, err := c.CallSession(ctx, s, "Page.enable", nil); err != nil {
			if err := staleOr(err); errors.Is(err, ErrStale) {
				in.Close()
				return nil, err
			}
		}
	}
	return in, nil
}

// Close stops watching for dialogs.
func (in *Input) Close() {
	in.once.Do(func() {
		close(in.done)
		for _, s := range in.subs {
			s.Close()
		}
	})
}

// send sends one input event and waits for the Page's answer, a dialog,
// the event timeout or ctx.
func (in *Input) send(ctx context.Context, method string, params map[string]any) error {
	if in.stopped != nil && in.stopped() {
		return ErrStopped
	}
	select {
	case ev := <-in.dialogs:
		return dialogFrom(ev)
	default:
	}
	cctx, cancel := context.WithTimeout(ctx, in.timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := in.c.CallSession(cctx, in.session, method, params)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			in.Sent++
			return nil
		}
		var ce *cdp.Error
		if errors.As(err, &ce) {
			// The browser refused the event: it never reached the Page.
			return &SendError{Sent: in.Sent, Err: err}
		}
		sent := in.Sent
		in.Sent++ // written, but unanswered: it may have reached the Page
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return &SendError{Sent: sent, NoReply: true, Err: err}
		}
		if err := staleOr(err); errors.Is(err, ErrStale) {
			return err
		}
		return &SendError{Sent: sent, Err: err}
	case ev := <-in.dialogs:
		// The event's own handler opened the dialog: it was delivered, and
		// its answer waits for the dialog to close.
		in.Sent++
		return dialogFrom(ev)
	}
}

func dialogFrom(ev cdp.Event) error {
	var d struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	json.Unmarshal(ev.Params, &d)
	if d.Type == "" {
		d.Type = "alert"
	}
	return &DialogError{Type: d.Type, Message: d.Message}
}

// MouseButton names a CDP mouse button.
type MouseButton string

const (
	ButtonLeft   MouseButton = "left"
	ButtonRight  MouseButton = "right"
	ButtonMiddle MouseButton = "middle"
)

// buttonsMask is the `buttons` bit of a pressed button.
func buttonsMask(b MouseButton) int {
	switch b {
	case ButtonRight:
		return 2
	case ButtonMiddle:
		return 4
	}
	return 1
}

// ClickEvents is the event sequence of a click at p: a move there, then a
// press and a release per click, with clickCount counting up (so a double
// click fires click, click and dblclick, as a person's would).
func ClickEvents(p Point, button MouseButton, count int) []map[string]any {
	return ClickEventsWith(p, button, count, 0)
}

// ClickEventsWith is ClickEvents with modifiers held (CDP's bitmask: Alt
// 1, Control 2, Shift 8; desktop-parity ticket 06) on every event.
func ClickEventsWith(p Point, button MouseButton, count, modifiers int) []map[string]any {
	evs := []map[string]any{{"type": "mouseMoved", "x": p.X, "y": p.Y, "button": "none", "buttons": 0}}
	for i := 1; i <= count; i++ {
		evs = append(evs,
			map[string]any{"type": "mousePressed", "x": p.X, "y": p.Y, "button": string(button), "buttons": buttonsMask(button), "clickCount": i},
			map[string]any{"type": "mouseReleased", "x": p.X, "y": p.Y, "button": string(button), "buttons": 0, "clickCount": i})
	}
	if modifiers != 0 {
		for _, ev := range evs {
			ev["modifiers"] = modifiers
		}
	}
	return evs
}

// Click sends ClickEvents, each raced against the dialogs and preceded by
// the Esc checkpoint.
func (in *Input) Click(ctx context.Context, p Point, button MouseButton, count int) error {
	return in.ClickWith(ctx, p, button, count, 0)
}

// ClickWith is Click with modifiers held (ClickEventsWith).
func (in *Input) ClickWith(ctx context.Context, p Point, button MouseButton, count, modifiers int) error {
	for _, ev := range ClickEventsWith(p, button, count, modifiers) {
		if err := in.send(ctx, "Input.dispatchMouseEvent", ev); err != nil {
			return err
		}
	}
	return nil
}

// WheelPxPerStep is one scroll step in CSS px: Chrome's own wheel notch on
// Windows (three lines of 100/3 px).
const WheelPxPerStep = 100

// WheelEvent is a mouse wheel at p, in CSS px (positive deltaY scrolls
// down, positive deltaX right).
func WheelEvent(p Point, dx, dy float64) map[string]any {
	return map[string]any{"type": "mouseWheel", "x": p.X, "y": p.Y, "deltaX": dx, "deltaY": dy}
}

// Wheel sends one mouse wheel event at p.
func (in *Input) Wheel(ctx context.Context, p Point, dx, dy float64) error {
	return in.WheelWith(ctx, p, dx, dy, 0)
}

// WheelWith is Wheel with modifiers held (CDP's bitmask; Control zooms in
// most pages; desktop-parity ticket 06).
func (in *Input) WheelWith(ctx context.Context, p Point, dx, dy float64, modifiers int) error {
	ev := WheelEvent(p, dx, dy)
	if modifiers != 0 {
		ev["modifiers"] = modifiers
	}
	return in.send(ctx, "Input.dispatchMouseEvent", ev)
}

// InsertText inserts text into the Page's focused element, as an IME
// commit would (Input.insertText): one input event, no key events.
func (in *Input) InsertText(ctx context.Context, text string) error {
	return in.send(ctx, "Input.insertText", map[string]any{"text": text})
}

// PressKeys sends a chord: every key down in order, then up in reverse.
func (in *Input) PressKeys(ctx context.Context, chord keys.Chord) error {
	for _, ev := range KeyEvents(chord) {
		if err := in.send(ctx, "Input.dispatchKeyEvent", ev); err != nil {
			return err
		}
	}
	return nil
}

// TypeText types text the way winuse's Headed type-text does: runs of
// text are inserted (Input.insertText), a newline (\n, \r or \r\n) is a
// real Return and a tab a real Tab, so a form submits or focus moves
// exactly as a person's typing would.
func (in *Input) TypeText(ctx context.Context, text string) error {
	for _, part := range TextParts(text) {
		var err error
		if part.Key != "" {
			chord, perr := keys.Parse(part.Key)
			if perr != nil {
				return perr
			}
			err = in.PressKeys(ctx, chord)
		} else {
			err = in.InsertText(ctx, part.Text)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// TextPart is one piece of typed text: a run to insert, or a key to press.
type TextPart struct {
	Text string
	Key  string // "Return" or "Tab"
}

// TextParts splits text into runs and the keys a newline or a tab stands
// for.
func TextParts(text string) []TextPart {
	var parts []TextPart
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			parts = append(parts, TextPart{Text: run.String()})
			run.Reset()
		}
	}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '\r':
			if i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			fallthrough
		case '\n':
			flush()
			parts = append(parts, TextPart{Key: "Return"})
		case '\t':
			flush()
			parts = append(parts, TextPart{Key: "Tab"})
		default:
			run.WriteRune(rs[i])
		}
	}
	flush()
	return parts
}

// CDP modifier bits.
const (
	modAlt   = 1
	modCtrl  = 2
	modMeta  = 4
	modShift = 8
)

// cdpKey is one key's DOM description.
type cdpKey struct {
	key, code string
	text      string // the text it types; "" for none
	location  int    // 1 left, 2 right, 3 numpad
	modifier  int    // its CDP modifier bit, for a modifier key
}

// namedKeys maps winuse's canonical key names (keys.Key.Name) that are
// not plain characters to their DOM key and code.
var namedKeys = map[string]cdpKey{
	"Return":       {key: "Enter", code: "Enter", text: "\r"},
	"KP_Enter":     {key: "Enter", code: "NumpadEnter", text: "\r", location: 3},
	"Tab":          {key: "Tab", code: "Tab"},
	"Clear":        {key: "Clear", code: "NumpadEqual", location: 3},
	"space":        {key: " ", code: "Space", text: " "},
	"Escape":       {key: "Escape", code: "Escape"},
	"BackSpace":    {key: "Backspace", code: "Backspace"},
	"Delete":       {key: "Delete", code: "Delete"},
	"Insert":       {key: "Insert", code: "Insert"},
	"Home":         {key: "Home", code: "Home"},
	"End":          {key: "End", code: "End"},
	"Prior":        {key: "PageUp", code: "PageUp"},
	"Next":         {key: "PageDown", code: "PageDown"},
	"Up":           {key: "ArrowUp", code: "ArrowUp"},
	"Down":         {key: "ArrowDown", code: "ArrowDown"},
	"Left":         {key: "ArrowLeft", code: "ArrowLeft"},
	"Right":        {key: "ArrowRight", code: "ArrowRight"},
	"Menu":         {key: "ContextMenu", code: "ContextMenu"},
	"Help":         {key: "Help", code: "Help"},
	"Select":       {key: "Select", code: "Select"},
	"Print":        {key: "Print", code: "PrintScreen"},
	"Sys_Req":      {key: "PrintScreen", code: "PrintScreen"},
	"Execute":      {key: "Execute", code: "Execute"},
	"Cancel":       {key: "Cancel", code: "Abort"},
	"Mode_switch":  {key: "ModeChange", code: "Lang1"},
	"Pause":        {key: "Pause", code: "Pause"},
	"Scroll_Lock":  {key: "ScrollLock", code: "ScrollLock"},
	"Num_Lock":     {key: "NumLock", code: "NumLock"},
	"Caps_Lock":    {key: "CapsLock", code: "CapsLock"},
	"Control_L":    {key: "Control", code: "ControlLeft", location: 1, modifier: modCtrl},
	"Control_R":    {key: "Control", code: "ControlRight", location: 2, modifier: modCtrl},
	"Shift_L":      {key: "Shift", code: "ShiftLeft", location: 1, modifier: modShift},
	"Shift_R":      {key: "Shift", code: "ShiftRight", location: 2, modifier: modShift},
	"Alt_L":        {key: "Alt", code: "AltLeft", location: 1, modifier: modAlt},
	"Alt_R":        {key: "Alt", code: "AltRight", location: 2, modifier: modAlt},
	"KP_Multiply":  {key: "*", code: "NumpadMultiply", text: "*", location: 3},
	"KP_Add":       {key: "+", code: "NumpadAdd", text: "+", location: 3},
	"KP_Subtract":  {key: "-", code: "NumpadSubtract", text: "-", location: 3},
	"KP_Decimal":   {key: ".", code: "NumpadDecimal", text: ".", location: 3},
	"KP_Divide":    {key: "/", code: "NumpadDivide", text: "/", location: 3},
	"KP_Separator": {key: ",", code: "NumpadComma", text: ",", location: 3},
	"KP_Equal":     {key: "=", code: "NumpadEqual", text: "=", location: 3},
	"KP_Delete":    {key: "Delete", code: "NumpadDecimal", location: 3},
}

// oemKeys maps the punctuation virtual keys to their DOM code and the
// characters without and with Shift (US layout, like winuse's key names).
var oemKeys = map[uint16][3]string{
	0xBA: {"Semicolon", ";", ":"},
	0xBB: {"Equal", "=", "+"},
	0xBC: {"Comma", ",", "<"},
	0xBD: {"Minus", "-", "_"},
	0xBE: {"Period", ".", ">"},
	0xBF: {"Slash", "/", "?"},
	0xC0: {"Backquote", "`", "~"},
	0xDB: {"BracketLeft", "[", "{"},
	0xDC: {"Backslash", "\\", "|"},
	0xDD: {"BracketRight", "]", "}"},
	0xDE: {"Quote", "'", "\""},
}

// shiftedDigits are the US layout's digits with Shift, 0 to 9.
const shiftedDigits = ")!@#$%^&*("

// describeKey is a key's DOM key, code and text, given the modifiers held.
func describeKey(k keys.Key, mods int) (key, code, text string, location int) {
	shift := mods&modShift != 0
	if d, ok := namedKeys[k.Name]; ok {
		return d.key, d.code, d.text, d.location
	}
	vk := k.VK
	switch {
	case vk >= 'A' && vk <= 'Z':
		ch := string(rune(vk - 'A' + 'a'))
		if shift {
			ch = strings.ToUpper(ch)
		}
		return ch, "Key" + string(rune(vk)), ch, 0
	case vk >= '0' && vk <= '9':
		ch := string(rune(vk))
		if shift {
			ch = string(shiftedDigits[vk-'0'])
		}
		return ch, "Digit" + string(rune(vk)), ch, 0
	case vk >= 0x60 && vk <= 0x69: // the numpad digits
		ch := string(rune('0' + vk - 0x60))
		return ch, "Numpad" + ch, ch, 3
	case vk >= 0x70 && vk <= 0x83: // F1 to F20
		f := fmt.Sprintf("F%d", vk-0x70+1)
		return f, f, "", 0
	}
	if o, ok := oemKeys[vk]; ok {
		ch := o[1]
		if shift {
			ch = o[2]
		}
		return ch, o[0], ch, 0
	}
	return k.Name, "", "", 0
}

// KeyEvents maps a chord to CDP key events: every key down in order, then
// up in reverse, with the modifiers held so far (Alt 1, Control 2, Shift
// 8). A key that types a character gets a keyDown with its text (which
// Chrome turns into keydown, keypress and input, as for a person); one
// that types nothing, or is pressed with Control or Alt held, gets a
// rawKeyDown, so Control+a selects instead of typing "a". Shift picks the
// shifted character: Shift_L+a types "A".
func KeyEvents(chord keys.Chord) []map[string]any {
	mods := 0
	var evs []map[string]any
	base := func(k keys.Key) map[string]any {
		key, code, text, location := describeKey(k, mods|modBit(k))
		ev := map[string]any{"key": key, "windowsVirtualKeyCode": int(k.VK), "nativeVirtualKeyCode": int(k.VK)}
		if code != "" {
			ev["code"] = code
		}
		if location != 0 {
			ev["location"] = location
		}
		if location == 3 {
			ev["isKeypad"] = true
		}
		if text != "" {
			ev["text"] = text
			ev["unmodifiedText"] = text
		}
		return ev
	}
	for _, k := range chord {
		mods |= modBit(k)
		ev := base(k)
		ev["modifiers"] = mods
		if _, hasText := ev["text"]; hasText && mods&(modCtrl|modAlt|modMeta) == 0 {
			ev["type"] = "keyDown"
		} else {
			delete(ev, "text")
			delete(ev, "unmodifiedText")
			ev["type"] = "rawKeyDown"
		}
		evs = append(evs, ev)
	}
	for i := len(chord) - 1; i >= 0; i-- {
		k := chord[i]
		mods &^= modBit(k)
		ev := base(k)
		delete(ev, "text")
		delete(ev, "unmodifiedText")
		ev["type"] = "keyUp"
		ev["modifiers"] = mods
		evs = append(evs, ev)
	}
	return evs
}

// modBit is a modifier key's CDP modifier bit, 0 for any other key.
func modBit(k keys.Key) int {
	if d, ok := namedKeys[k.Name]; ok {
		return d.modifier
	}
	return 0
}

// Field is a node set-value can write: what kind of text it takes.
type Field struct {
	Kind     string `json:"kind"` // "text" (input or textarea), "editable" (contenteditable) or "" for none
	Type     string `json:"type"` // the input's type
	Disabled bool   `json:"disabled"`
	ReadOnly bool   `json:"readonly"`
	Password bool   `json:"password"`
}

const fieldJS = `function() {
  /* winuse:field */
  const el = this.nodeType === 1 ? this : this.parentElement;
  if (!el || !el.isConnected) return {kind: 'gone'};
  const t = (el.getAttribute('type') || '').toLowerCase();
  const out = {kind: '', type: t, disabled: el.matches(':disabled') || el.getAttribute('aria-disabled') === 'true', readonly: !!el.readOnly, password: t === 'password'};
  if (el.tagName === 'TEXTAREA') out.kind = 'text';
  else if (el.tagName === 'INPUT') {
    if (!/^(button|submit|reset|checkbox|radio|file|image|range|color|hidden|date|datetime-local|month|week|time)$/.test(t)) out.kind = 'text';
  } else if (el.isContentEditable) out.kind = 'editable';
  return out;
}`

// selectJS selects a field's whole content, so the insert replaces it.
const selectJS = `function() {
  /* winuse:select */
  const el = this.nodeType === 1 ? this : this.parentElement;
  if (typeof el.select === 'function') { el.select(); return true; }
  const doc = el.ownerDocument, sel = doc.getSelection(), rg = doc.createRange();
  rg.selectNodeContents(el); sel.removeAllRanges(); sel.addRange(rg);
  return true;
}`

// readJS reads a field's value back: value for inputs and textareas, the
// text for contenteditable.
const readJS = `function() {
  /* winuse:read */
  const el = this.nodeType === 1 ? this : this.parentElement;
  return el.value !== undefined && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') ? String(el.value) : String(el.innerText || '');
}`

// NotSettableError says a node takes no typed text, or refuses it.
type NotSettableError struct{ Why string }

func (e *NotSettableError) Error() string { return e.Why }

// ReadField reads what kind of field a node is.
func (o Origin) ReadField(ctx context.Context) (Field, error) {
	c, err := o.caller()
	if err != nil {
		return Field{}, err
	}
	var f Field
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, fieldJS, nil, &f); err != nil {
		return Field{}, staleOr(err)
	}
	if f.Kind == "gone" {
		return Field{}, fmt.Errorf("%w: the node left the document", ErrStale)
	}
	return f, nil
}

// Refusal is why set-value cannot write the field, nil when it can.
func (f Field) Refusal() error {
	switch {
	case f.Kind == "":
		return &NotSettableError{Why: "it takes no typed text (it is not a text field, a text area or editable text)"}
	case f.Disabled:
		return &NotActionableError{Why: WhyDisabled}
	case f.ReadOnly:
		return &NotSettableError{Why: "it is read-only"}
	}
	return nil
}

// SetValue writes value into a text field the way a person replaces its
// text, with no OS focus: DOM.focus, select all, then Input.insertText
// (an empty value deletes the selection with a Backspace), and reads the
// value back. It dispatches no synthetic input or change event: insertText
// already fires a trusted beforeinput and input, which frameworks listen
// to, and the field fires change itself when the focus leaves it. f is the
// field's ReadField answer; a failed read-back is a *VerifyError that never
// shows a password field's value.
func (o Origin) SetValue(ctx context.Context, in *Input, f Field, value string) error {
	c, err := o.caller()
	if err != nil {
		return err
	}
	if _, err := c.CallSession(ctx, o.SessionID, "DOM.focus", map[string]any{"backendNodeId": o.BackendNodeID}); err != nil {
		return staleOr(err)
	}
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, selectJS, nil, nil); err != nil {
		return staleOr(err)
	}
	if value == "" {
		chord, _ := keys.Parse("BackSpace")
		err = in.PressKeys(ctx, chord)
	} else {
		err = in.InsertText(ctx, value)
	}
	if err != nil {
		return err
	}
	var got string
	if err := callWith(ctx, c, o.SessionID, o.BackendNodeID, readJS, nil, &got); err != nil {
		return &VerifyError{Msg: "the value was delivered, but the field could not be read back"}
	}
	return ValueMatches(f, value, got)
}

// TypeInto types text into a field the way type-text types into the
// Page's focus, with no OS focus: DOM.focus, then TypeText (runs through
// Input.insertText, a newline as Return, a tab as Tab). The text lands at
// the field's own caret or replaces its selection, as typing does, so a
// select-text before it makes a replacement. There is no read-back: the
// text may be only part of the field's value.
func (o Origin) TypeInto(ctx context.Context, in *Input, text string) error {
	c, err := o.caller()
	if err != nil {
		return err
	}
	if in.stopped != nil && in.stopped() {
		return ErrStopped
	}
	if _, err := c.CallSession(ctx, o.SessionID, "DOM.focus", map[string]any{"backendNodeId": o.BackendNodeID}); err != nil {
		return staleOr(err)
	}
	return in.TypeText(ctx, text)
}

// ValueMatches compares a field's read-back with the value sent, line
// breaks aside (a text area stores CRLF as LF; linebreak): exactly
// otherwise for inputs and text areas, and for editable text with spaces
// and the trailing newline normalized too. A mismatch never shows a
// password field's value, only the lengths.
func ValueMatches(f Field, sent, got string) error {
	if f.Kind == "editable" {
		norm := func(s string) string {
			s = strings.ReplaceAll(s, " ", " ")
			return strings.TrimRight(linebreak.Normalize(s), "\n")
		}
		if norm(sent) == norm(got) {
			return nil
		}
	} else if linebreak.Same(sent, got) {
		return nil
	}
	if f.Password {
		return &VerifyError{Msg: fmt.Sprintf("the value was delivered, but the password field holds %d characters afterwards, not %d", len([]rune(got)), len([]rune(sent)))}
	}
	shown := got
	if r := []rune(shown); len(r) > 80 {
		shown = string(r[:80]) + "..."
	}
	return &VerifyError{Msg: fmt.Sprintf("the value was delivered, but the field holds %q afterwards (it may limit or reformat its text)", shown)}
}

// Focus is what holds the keyboard focus in a Tab's Page.
type Focus struct {
	// Kind is "editable" (a text field or editable text), "other" (any
	// other focused element), "frame" (a cross-origin iframe holds it),
	// "body" (nothing in particular) or "none".
	Kind     string  `json:"kind"`
	Tag      string  `json:"tag"`
	Password bool    `json:"password"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
	DPR      float64 `json:"dpr"`
}

// Rect is the focused element's box in the Tab's viewport.
func (f Focus) Rect() Rect { return Rect{Left: f.X, Top: f.Y, Width: f.W, Height: f.H, DPR: f.DPR} }

// focusJS finds the focused element, through same-origin iframes and
// shadow roots.
const focusJS = `(() => {
  /* winuse:focus */
  let d = document, e = d.activeElement, ox = 0, oy = 0, framed = false;
  while (e && (e.tagName === 'IFRAME' || e.tagName === 'FRAME')) {
    let inner = null;
    try { inner = e.contentDocument; } catch (x) {}
    const r = e.getBoundingClientRect();
    if (!inner) return {kind: 'frame', tag: 'iframe', x: r.left, y: r.top, w: r.width, h: r.height, dpr: devicePixelRatio};
    ox += r.left + e.clientLeft; oy += r.top + e.clientTop; framed = true;
    d = inner; e = d.activeElement;
  }
  while (e && e.shadowRoot && e.shadowRoot.activeElement) e = e.shadowRoot.activeElement;
  if (!e) return {kind: 'none', dpr: devicePixelRatio};
  if (e === d.body || e === d.documentElement) return {kind: framed ? 'other' : 'body', tag: e.tagName.toLowerCase(), dpr: devicePixelRatio};
  const r = e.getBoundingClientRect(), t = (e.getAttribute('type') || '').toLowerCase();
  const editable = e.isContentEditable || e.tagName === 'TEXTAREA' ||
    (e.tagName === 'INPUT' && !/^(button|submit|reset|checkbox|radio|file|image|range|color|hidden)$/.test(t));
  return {kind: editable ? 'editable' : 'other', tag: e.tagName.toLowerCase(), password: t === 'password',
    x: r.left + ox, y: r.top + oy, w: r.width, h: r.height, dpr: devicePixelRatio};
})()`

// ReadFocus reads what holds the focus in the Tab behind session.
func ReadFocus(ctx context.Context, c *cdp.Client, session string) (Focus, error) {
	if c == nil || !c.Alive() {
		return Focus{}, fmt.Errorf("%w: the Browser Connection ended", ErrStale)
	}
	res, err := c.CallSession(ctx, session, "Runtime.evaluate", map[string]any{"expression": focusJS, "returnByValue": true, "silent": true})
	if err != nil {
		return Focus{}, staleOr(err)
	}
	var r struct {
		Result struct {
			Value Focus `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return Focus{}, err
	}
	if r.ExceptionDetails != nil {
		return Focus{}, fmt.Errorf("reading the Page's focus failed: %s", r.ExceptionDetails.Text)
	}
	return r.Result.Value, nil
}

// ToScreen is the Agent Cursor transform (probe-results.md section 3): a
// rectangle in CSS px of the Tab's viewport to physical screen px, from
// the UIA document Element's top-left and the page's devicePixelRatio,
// both read fresh for each Action. Never the monitor scale alone: it is
// wrong as soon as the page is zoomed.
func ToScreen(docLeft, docTop int32, r Rect) (left, top, right, bottom int32) {
	dpr := r.DPR
	if dpr <= 0 {
		dpr = 1
	}
	left = int32(math.Round(float64(docLeft) + r.Left*dpr))
	top = int32(math.Round(float64(docTop) + r.Top*dpr))
	right = int32(math.Round(float64(docLeft) + (r.Left+r.Width)*dpr))
	bottom = int32(math.Round(float64(docTop) + (r.Top+r.Height)*dpr))
	return
}

// PointToScreen is ToScreen for one point.
func PointToScreen(docLeft, docTop int32, dpr float64, p Point) (x, y int32) {
	if dpr <= 0 {
		dpr = 1
	}
	return int32(math.Round(float64(docLeft) + p.X*dpr)), int32(math.Round(float64(docTop) + p.Y*dpr))
}

// ToCSS is the inverse transform: a physical screen point to CSS px of the
// Tab's viewport.
func ToCSS(docLeft, docTop int32, dpr float64, x, y int32) Point {
	if dpr <= 0 {
		dpr = 1
	}
	return Point{X: float64(x-docLeft) / dpr, Y: float64(y-docTop) / dpr}
}

// dprJS reads the page's devicePixelRatio.
const dprJS = `window.devicePixelRatio`

// ReadDPR reads the devicePixelRatio of the Tab behind session.
func ReadDPR(ctx context.Context, c *cdp.Client, session string) (float64, error) {
	if c == nil || !c.Alive() {
		return 0, fmt.Errorf("%w: the Browser Connection ended", ErrStale)
	}
	res, err := c.CallSession(ctx, session, "Runtime.evaluate", map[string]any{"expression": dprJS, "returnByValue": true, "silent": true})
	if err != nil {
		return 0, staleOr(err)
	}
	var r struct {
		Result struct {
			Value float64 `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return 0, err
	}
	if r.Result.Value <= 0 {
		return 1, nil
	}
	return r.Result.Value, nil
}
