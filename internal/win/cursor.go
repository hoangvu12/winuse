package win

// The Agent Cursor (headless spec, ADR 0010, ticket 03): a
// presentation-only second cursor that glides to the Element a Headless
// Action targets, says what is happening in a label pill, and ripples on
// the click — so headless work is never invisible, without taking the
// user's pointer, keyboard or foreground.
//
// The API takes screen rectangles (physical pixels) and label text only,
// never UI Automation types, so any driver (the CDP browser tool winuse
// will merge with) can show the same cursor. Everything is best effort:
// no method returns an error, none panics, and none blocks its caller
// beyond a bound when the UI thread is stuck.
//
// Threads. The windows, the timer and all state live on the shared UI
// thread (ADR 0006), reached through the do function the helper passes
// (Overlay.Do). Calls are funnelled through one dispatcher goroutine so
// they reach the UI thread in call order without the caller ever waiting
// on do itself: Travel waits for its own completion signal (bounded),
// HideWhile for its hide (bounded), Click and the restore not at all.
//
// The pieces: cursor_motion.go (path, easing, timeline, idle — pure),
// cursor_layout.go (shape and placement — pure), cursor_raster.go
// (pixels — pure), cursor_view.go (the Win32 windows); this file holds
// the API and the UI-thread controller that runs the model against the
// clock and hands frames to the view.

import (
	"errors"
	"fmt"
	"image/color"
	"log"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// AgentCursorClassName is the class of the Agent Cursor's layered windows
// (the dart with its label pill, the ripple, the outline). The e2e tests find
// the cursor on the desktop by it.
const AgentCursorClassName = "winuse.cursor"

// cursorTravelSlack is added to Travel's bound (twice the estimated
// travel time) for the job's trip to the UI thread.
const cursorTravelSlack = 50 * time.Millisecond

// cursorQueueSize bounds the calls waiting for the UI thread. When it is
// full (a UI thread stuck for a long time) further calls are dropped:
// the cursor is cosmetic.
const cursorQueueSize = 256

// cursorLiveGuard refuses to create the cursor's windows inside an
// offline test binary (LiveDesktopGuard); the opt-in visual demo test
// replaces it.
var cursorLiveGuard = LiveDesktopGuard

// cursorExcludeFromCapture sets WDA_EXCLUDEFROMCAPTURE on the cursor's
// windows. Only the opt-in visual demo turns it off, to screenshot the
// cursor it draws.
var cursorExcludeFromCapture = true

// cursorTrailOn reads WINUSE_CURSOR_TRAIL from the helper's environment:
// "1" draws cua's comet trail behind a travelling cursor. Off by default,
// as in cua ("the default style is plain").
func cursorTrailOn() bool { return os.Getenv("WINUSE_CURSOR_TRAIL") == "1" }

// cursorFrameTraceOn reads WINUSE_CURSOR_TRACE (the helper's cursor
// trace, browser-ux 03): "1" also logs each animation's frame count, rate
// and the UI thread's time spent drawing it.
func cursorFrameTraceOn() bool { return os.Getenv("WINUSE_CURSOR_TRACE") == "1" }

// AgentCursor is one Agent Cursor. The helper creates one per Agent
// (multi-agent ticket 04) on the overlay's UI thread with
// NewAgentCursorIn, each in its own colour, and Closes it when the Agent
// is gone or the helper exits. All methods are safe on a nil cursor (no
// cursor).
type AgentCursor struct {
	do    func(func()) error
	ui    *cursorUI // touched only from jobs run through do
	queue chan func()
	quit  chan struct{}
	once  sync.Once
	snap  cursorSnapshot
}

// cursorSnapshot is the controller's last published position, read by
// Travel's caller to size its bound without a round trip.
type cursorSnapshot struct {
	mu      sync.Mutex
	visible bool
	tip     fpoint
}

func (s *cursorSnapshot) store(visible bool, tip fpoint) {
	s.mu.Lock()
	s.visible, s.tip = visible, tip
	s.mu.Unlock()
}

func (s *cursorSnapshot) load() (bool, fpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.visible, s.tip
}

// NewAgentCursor creates a cursor in the default accent (the default
// Agent's): see NewAgentCursorIn.
func NewAgentCursor(do func(func()) error) (*AgentCursor, error) {
	return NewAgentCursorIn(do, cursorAccent)
}

// NewAgentCursorIn creates the cursor's hidden windows, drawn in accent,
// on the UI thread do runs jobs on (the helper passes Overlay.Do, like
// NewEscWatcherOnUI). An error means no cursor; Headless Actions then run
// without one.
func NewAgentCursorIn(do func(func()) error, accent color.RGBA) (*AgentCursor, error) {
	if do == nil {
		return nil, errors.New("the Agent Cursor needs a UI thread")
	}
	if err := cursorLiveGuard(); err != nil {
		return nil, err
	}
	var (
		view      *cursorWin
		createErr error
		abandoned atomic.Bool
	)
	if err := do(func() {
		if abandoned.Load() {
			createErr = errors.New("abandoned")
			return
		}
		view, createErr = newCursorWin(accent)
	}); err != nil {
		abandoned.Store(true) // a late job must not create orphan windows
		return nil, fmt.Errorf("creating the Agent Cursor on the UI thread: %w", err)
	}
	if createErr != nil {
		return nil, fmt.Errorf("creating the Agent Cursor: %w", createErr)
	}
	c := newAgentCursor(do, view, time.Now)
	view.setTick(c.ui.tick)
	return c, nil
}

// newAgentCursor wires a cursor around a view (the Win32 windows, or a
// test fake) and starts its dispatcher.
func newAgentCursor(do func(func()) error, view cursorView, now func() time.Time) *AgentCursor {
	c := &AgentCursor{
		do:    do,
		queue: make(chan func(), cursorQueueSize),
		quit:  make(chan struct{}),
	}
	c.ui = &cursorUI{view: view, now: now, snap: &c.snap, trace: cursorFrameTraceOn()}
	c.ui.model.trail = cursorTrailOn()
	go c.dispatch()
	return c
}

// dispatch runs queued calls in order until Close.
func (c *AgentCursor) dispatch() {
	for {
		select {
		case <-c.quit:
			return
		case call := <-c.queue:
			call()
		}
	}
}

// enqueue queues a call for the dispatcher without blocking; false when
// the cursor is closed or the queue is full.
func (c *AgentCursor) enqueue(call func()) bool {
	select {
	case <-c.quit:
		return false
	default:
	}
	select {
	case c.queue <- call:
		return true
	default:
		return false
	}
}

// post runs job on the UI thread, in call order, without waiting.
func (c *AgentCursor) post(job func()) {
	c.enqueue(func() { _ = c.do(job) })
}

// postWait runs job on the UI thread, in call order, and waits until it
// ran, the UI thread refused it, or bound passed.
func (c *AgentCursor) postWait(job func(), bound time.Duration) {
	ran := make(chan struct{})
	failed := make(chan struct{})
	if !c.enqueue(func() {
		if err := c.do(func() { defer close(ran); job() }); err != nil {
			close(failed)
		}
	}) {
		return
	}
	t := time.NewTimer(bound)
	defer t.Stop()
	select {
	case <-ran:
	case <-failed:
	case <-t.C:
	case <-c.quit:
	}
}

// Travel is TravelAbove with no anchor: the cursor sits at the top of the
// non-topmost band.
func (c *AgentCursor) Travel(origin, target Rect, label string) {
	c.TravelAbove(0, origin, target, label)
}

// TravelAbove shows the cursor, at origin's centre when it is hidden or
// fading, else where it is, and glides it to target's centre with label
// in the pill beside it, its windows placed just above the Window anchor
// in the z-order (cursor_zorder.go). It returns when the travel arrived
// (the tip within a pixel of the target, its follow-through still
// settling while the Action runs), or after a bound of about twice the
// travel time: a stuck UI thread never holds the Action back longer than
// that.
func (c *AgentCursor) TravelAbove(anchor uint64, origin, target Rect, label string) {
	if c == nil {
		return
	}
	bound := c.travelBound(origin, target)
	done := make(chan struct{})
	failed := make(chan struct{})
	if !c.enqueue(func() {
		if err := c.do(func() { c.ui.travel(anchor, origin, target, label, done) }); err != nil {
			close(failed)
		}
	}) {
		return
	}
	t := time.NewTimer(bound)
	defer t.Stop()
	select {
	case <-done:
	case <-failed:
	case <-t.C:
	case <-c.quit:
	}
}

// travelBound is Travel's longest wait: twice the travel time estimated
// from the last published position, plus the trip to the UI thread. The
// estimate uses scale 1, an upper bound for any monitor at 96 DPI or
// more (the duration shrinks with the scale).
func (c *AgentCursor) travelBound(origin, target Rect) time.Duration {
	visible, tip := c.snap.load()
	from := rectCentre(origin)
	if visible {
		from = tip
	}
	return 2*cursorTravelDuration(from, rectCentre(target), targetSide(target), 1, false) + cursorTravelSlack
}

// Click is ClickAbove with no anchor.
func (c *AgentCursor) Click(target Rect) {
	c.ClickAbove(0, target)
}

// ClickAbove presses the arrow and briefly outlines target's bounds,
// the windows just above anchor. Non-blocking.
func (c *AgentCursor) ClickAbove(anchor uint64, target Rect) {
	if c == nil {
		return
	}
	c.post(func() { c.ui.click(anchor, target) })
}

// HideWhile runs f with the cursor hidden — the hide is applied (and
// composed off the screen) before f runs, within the UI thread's bound —
// and restores it afterwards, even when f panics. winuse's captures
// bracket themselves with it so the cursor never appears in its own
// screenshots.
func (c *AgentCursor) HideWhile(f func()) {
	if c == nil {
		f()
		return
	}
	c.postWait(func() { c.ui.pause() }, uiThreadTimeout)
	defer c.post(func() { c.ui.resume() })
	f()
}

// Close destroys the cursor's windows and stops its dispatcher.
// Idempotent and safe on a nil cursor.
func (c *AgentCursor) Close() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		c.postWait(func() { c.ui.close() }, uiThreadTimeout)
		close(c.quit)
	})
}

// cursorView is the controller's window side, called on the UI thread:
// the Win32 windows (cursor_view.go) or a test fake.
type cursorView interface {
	// monitorAt returns the work area and DPI of the monitor at p.
	monitorAt(p fpoint) (work Rect, dpi uint32)
	// measureLabel is text's width in pixels in the label font at scale.
	measureLabel(text string, scale float64) int32
	// showCursor shows the arrow at tip, turned by angle and scaled by
	// press about its tip, and the look's label pill at labelOp and scaled
	// by labelPop (its reveal and pop), the whole at opacity (the idle
	// fade).
	showCursor(tip fpoint, opacity, labelOp, labelPop, angle, press float64, look cursorLook)
	hideCursor()
	// showTrail shows the comet trail pts (screen pixels) at opacity.
	showTrail(pts []fpoint, opacity, scale float64)
	hideTrail()
	// reducedMotion reports whether Windows' animation effects are off.
	reducedMotion() bool
	// showOutline shows the highlight around target at opacity, scaled by
	// pop about the element's centre.
	showOutline(target Rect, opacity, pop, scale float64)
	hideOutline()
	// showCaret shows the insertion caret's bar (screen pixels) at
	// opacity; hideCaret hides it.
	showCaret(bar Rect, opacity float64)
	hideCaret()
	// composed waits until hidden windows are off the screen (DwmFlush).
	composed()
	// raise places the windows just above the Window anchor in the
	// z-order (0: the top of the non-topmost band).
	raise(anchor uint64)
	// schedule (re)arms the controller's tick after d; cancel disarms it.
	schedule(d time.Duration)
	cancel()
	destroy()
}

// cursorUI is the controller, owned by the UI thread: it runs the model
// against the clock, renders its frames into the view, completes
// Travel's waits and drives the timer.
type cursorUI struct {
	view   cursorView
	now    func() time.Time
	snap   *cursorSnapshot
	model  cursorModel
	look   cursorLook
	fx     float64       // the scale of the last click's outline
	paused int           // HideWhile depth
	done   chan struct{} // the in-flight travel's arrival; nil when none
	closed bool

	// The frame trace (WINUSE_CURSOR_TRACE=1): frames and drawing time of
	// the animation running since animStart.
	trace     bool
	animStart time.Time
	frames    int
	drawing   time.Duration
}

// travel starts a Travel (see AgentCursor.TravelAbove); done is closed
// when it completes, is superseded, or cannot run.
func (u *cursorUI) travel(anchor uint64, origin, target Rect, label string, done chan struct{}) {
	if u.closed {
		close(done)
		return
	}
	now := u.now()
	to := rectCentre(target)
	work, dpi := u.view.monitorAt(to)
	scale := cursorScale(dpi)
	look := u.lookFor(to, label, work, scale)
	// A new label waits for the cursor to land, then fades in; the same
	// label on a cursor already showing it travels with it (the rule of
	// agent-desktop's reveal.rs, cursor_motion.go).
	newLabel := look.Label != "" && (look.Label != u.look.Label || !u.model.visibleForTravel())
	u.look = look
	u.finishTravel() // a travel still in flight is superseded
	u.done = done
	u.model.startTravel(now, origin, target, scale, u.view.reducedMotion())
	if newLabel {
		u.model.revealLabel()
	}
	u.view.raise(anchor)
	u.render(now)
}

// lookFor computes the cursor window's look for a travel to tip: the
// label sanitized, ellipsized to the pill's width (and the work area's),
// and the pill placed beside the tip.
func (u *cursorUI) lookFor(tip fpoint, label string, work Rect, scale float64) cursorLook {
	look := cursorLook{Scale: scale}
	text := sanitizeLabel(label)
	if text == "" {
		return look
	}
	measure := func(s string) int32 { return u.view.measureLabel(s, scale) }
	maxW := pillTextMax(scale)
	if wa := work.Width() - 2*scaled(cursorPillPadX, scale); wa > 0 {
		maxW = min(maxW, wa)
	}
	text = ellipsize(text, maxW, measure)
	if text == "" {
		return look
	}
	w, h := pillSize(measure(text)+1, scale)
	pill := placePill(tip, w, h, work, scale)
	tx, ty := roundPoint(tip)
	look.Label = text
	look.Pill = Rect{Left: pill.Left - tx, Top: pill.Top - ty, Right: pill.Right - tx, Bottom: pill.Bottom - ty}
	return look
}

// click starts a Click's press and outline (see AgentCursor.ClickAbove).
func (u *cursorUI) click(anchor uint64, target Rect) {
	if u.closed {
		return
	}
	now := u.now()
	_, dpi := u.view.monitorAt(rectCentre(target))
	u.fx = cursorScale(dpi)
	if !u.model.visibleForTravel() {
		u.look = cursorLook{Scale: u.fx} // appearing without a travel: no label
	}
	u.model.startClick(now, target, u.fx, u.view.reducedMotion())
	u.view.raise(anchor)
	u.render(now)
}

// tick is the timer's frame (or idle) callback.
func (u *cursorUI) tick() {
	if !u.closed {
		u.render(u.now())
	}
}

// render advances the model to now, shows its frame (unless paused),
// releases an arrived travel's waiter and re-arms the timer.
func (u *cursorUI) render(now time.Time) {
	animating := u.model.animating()
	f := u.model.advance(now)
	u.snap.store(u.model.visibleForTravel(), u.model.tip)
	if !f.Travelling {
		u.finishTravel()
	}
	if u.paused == 0 {
		var began time.Time
		if u.trace {
			began = time.Now()
		}
		u.show(f)
		if u.trace {
			u.traceFrame(now, began, animating)
		}
	}
	if d, ok := u.model.nextWake(now); ok {
		u.view.schedule(d)
	} else {
		u.view.cancel()
	}
}

// traceFrame counts one frame of an animation and, when the animation
// ends, logs its frame rate and the UI thread's time spent drawing it.
func (u *cursorUI) traceFrame(now, began time.Time, animating bool) {
	if !animating && u.frames == 0 {
		u.animStart = now
	}
	u.frames++
	u.drawing += time.Since(began)
	if u.model.animating() {
		return
	}
	if el := now.Sub(u.animStart); u.frames > 1 && el > 0 {
		log.Printf("cursor trace: %d frames in %dms (%.0f per second), drawing %.2fms per frame",
			u.frames, el.Milliseconds(), float64(u.frames-1)/el.Seconds(),
			float64(u.drawing.Microseconds())/1000/float64(u.frames))
	}
	u.frames, u.drawing = 0, 0
}

// show hands one frame to the view.
func (u *cursorUI) show(f cursorFrame) {
	if f.Cursor {
		u.view.showCursor(f.Tip, f.Opacity, f.LabelOp, f.LabelPop, f.Angle, f.Press, u.look)
	} else {
		u.view.hideCursor()
	}
	if f.Cursor && len(f.Trail) >= 2 {
		u.view.showTrail(f.Trail, f.Opacity, u.look.Scale)
	} else {
		u.view.hideTrail()
	}
	if f.Outline {
		u.view.showOutline(f.OutlineRect, f.OutlineOp, f.OutlinePop, u.fx)
	} else {
		u.view.hideOutline()
	}
	if f.Caret {
		u.view.showCaret(f.CaretBar, f.CaretOp)
	} else {
		u.view.hideCaret()
	}
}

// finishTravel releases the waiting Travel, if any.
func (u *cursorUI) finishTravel() {
	if u.done != nil {
		close(u.done)
		u.done = nil
	}
}

// pause hides everything for a capture (HideWhile); nested pauses count.
func (u *cursorUI) pause() {
	u.paused++
	if u.paused == 1 && !u.closed {
		u.view.hideCursor()
		u.view.hideTrail()
		u.view.hideOutline()
		u.view.hideCaret()
		u.view.composed()
	}
}

// resume ends a pause; the outermost one shows the current frame again.
func (u *cursorUI) resume() {
	if u.paused == 0 {
		return
	}
	u.paused--
	if u.paused == 0 && !u.closed {
		u.render(u.now())
	}
}

// close destroys the view and releases a waiting Travel.
func (u *cursorUI) close() {
	if u.closed {
		return
	}
	u.closed = true
	u.finishTravel()
	u.view.cancel()
	u.view.destroy()
}

// roundPoint rounds p to whole pixels.
func roundPoint(p fpoint) (int32, int32) {
	return int32(math.Round(p.X)), int32(math.Round(p.Y))
}
