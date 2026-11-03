package helper

// Lanes (multi-agent ticket 06, ADR 0014 Decisions 1, 6 and 7): the one
// action gate becomes a lane per UI thread, a lane per Page and one
// desktop lane, so agents working in Windows of different Apps no longer
// wait for each other.
//
//   - A Headless or Posted Action takes the lane of its target Window's UI
//     thread (GetWindowThreadProcessId): focus, attached key state and fake
//     activation are per thread, so all Windows of one Chrome, Explorer or
//     Windows 11 Notepad take turns.
//   - A Page Action through CDP touches no thread state: it takes the lane
//     of its browser Window's Page (the one active Tab that Window shows),
//     so Page Actions in two browser Windows run at once, even in one
//     browser.
//   - A Headed Action (real pointer or keys, an activation) takes the one
//     desktop lane and its target Window's thread lane. So does a command
//     that names no Window (the clipboard, a display screenshot), as
//     before.
//   - A Batch takes the lanes of its Window: the desktop lane too when any
//     step is Headed, the Page lane too when any step goes to the Page.
//     A step in its own Window adds that Window's lanes, and an `in new`
//     step its opener's (desktop-parity ticket 24, batchLanes).
//   - Reads take no lane, as before.
//
// Lanes are taken in one fixed order (the desktop, then threads by id,
// then Pages), so two Batches cannot deadlock. A request waits for its
// lanes up to laneWaitLimit, clamped by its own budget; past it, it is
// refused with nothing sent, naming the agent that holds the lane. The
// user's Esc stops a waiting request too (multi-agent ticket 05,
// gateWait).

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// laneWaitLimit is how long a request waits for its lanes (cua's
// input_busy wait), clamped by the request's budget.
const laneWaitLimit = 12 * time.Second

// laneDeadlineMargin is kept between a lane wait and the request's
// deadline, to answer with the refusal in time.
const laneDeadlineMargin = 100 * time.Millisecond

// desktopLane is the desktop lane's key; it sorts first.
const desktopLane = "0 desktop"

// threadLane and pageLane build the other keys; zero-padded so they sort
// by id after the desktop.
func threadLane(tid uint32) string { return fmt.Sprintf("1 thread %010d", tid) }
func pageLane(hwnd uint64) string  { return fmt.Sprintf("2 page %020d", hwnd) }
func windowLane(hwnd uint64) string {
	return fmt.Sprintf("1 window %020d", hwnd) // a Window whose thread cannot be read
}

// laneHolder is who holds a lane, for the refusal of a request that waited
// too long.
type laneHolder struct {
	agent  string // the agent's label
	window uint64 // the Window it acts on; 0 for none
}

// laneManager is the helper's lanes: one capacity-1 channel per key,
// made on first use, and who holds each.
type laneManager struct {
	mu      sync.Mutex
	lanes   map[string]chan struct{}
	holders map[string]laneHolder
	desktop int // how many times the desktop lane was taken (desktopUses)
}

func newLaneManager() *laneManager {
	return &laneManager{lanes: map[string]chan struct{}{}, holders: map[string]laneHolder{}}
}

// lane is the channel of a key.
func (m *laneManager) lane(key string) chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.lanes[key]
	if !ok {
		ch = make(chan struct{}, 1)
		m.lanes[key] = ch
	}
	return ch
}

func (m *laneManager) setHolder(key string, h laneHolder) {
	m.mu.Lock()
	m.holders[key] = h
	if key == desktopLane {
		m.desktop++
	}
	m.mu.Unlock()
}

// desktopUses counts the desktop lane's takings so far, and counts one
// more while it is held: a Headless Action compares it before and after
// to know that no Headed Action ran beside it (selfactivation.go).
func (s Services) desktopUses() int {
	if s.lanes == nil {
		return 0
	}
	s.lanes.mu.Lock()
	defer s.lanes.mu.Unlock()
	n := 2 * s.lanes.desktop
	if _, held := s.lanes.holders[desktopLane]; held {
		n++
	}
	return n
}

func (m *laneManager) holder(key string) (laneHolder, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.holders[key]
	return h, ok
}

func (m *laneManager) release(key string) {
	m.mu.Lock()
	delete(m.holders, key)
	ch := m.lanes[key]
	m.mu.Unlock()
	<-ch
}

// laneSet is the lanes one request takes, and its Window.
type laneSet struct {
	keys   []string // sorted: the acquisition order
	window uint64
}

func (l laneSet) has(key string) bool {
	for _, k := range l.keys {
		if k == key {
			return true
		}
	}
	return false
}

// newLaneSet sorts and de-duplicates keys.
func newLaneSet(window uint64, keys ...string) laneSet {
	sort.Strings(keys)
	out := keys[:0]
	for i, k := range keys {
		if i == 0 || k != keys[i-1] {
			out = append(out, k)
		}
	}
	return laneSet{keys: out, window: window}
}

// lanesFor is the lanes a command with these arguments takes, from the
// routing predicates the commands themselves route on (stepHeadless,
// pageStep). It reads the Window but sends nothing.
func (s Services) lanesFor(ctx context.Context, name string, args json.RawMessage) laneSet {
	var a struct {
		Window string `json:"window"`
	}
	if json.Unmarshal(args, &a) != nil || strings.TrimSpace(a.Window) == "" {
		return newLaneSet(0, desktopLane) // no Window: the clipboard, a display screenshot
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return newLaneSet(0, desktopLane) // it fails on the Window anyway
	}
	if name == "batch" {
		return s.batchLanes(ctx, args, w)
	}
	headed, page := s.stepClass(ctx, name, args, w)
	return newLaneSet(w.ID, s.classKeys(w.ID, headed, page)...)
}

// classKeys is the lanes of one Action on Window hwnd: Headed takes the
// desktop lane and the thread lane, a Page Action the Page lane, any
// other the thread lane.
func (s Services) classKeys(hwnd uint64, headed, page bool) []string {
	thread := windowLane(hwnd)
	if s.WindowThread != nil {
		if tid := s.WindowThread(hwnd); tid != 0 {
			thread = threadLane(tid)
		}
	}
	switch {
	case headed && page:
		return []string{desktopLane, thread, pageLane(hwnd)}
	case headed:
		return []string{desktopLane, thread}
	case page:
		return []string{pageLane(hwnd)}
	}
	return []string{thread}
}

// batchLanes is the lanes of a Batch: every step's, on the step's own
// Window (desktop-parity ticket 24) or the Batch's, and the desktop lane
// when the Batch is sent with headed. They are taken up front. An `in new`
// step's Window does not exist yet: it takes the lanes of the Window of
// the step before it, whose App opens it, and at its turn the Batch checks
// that the new Window needs no other lane (lanesHeld); a dialog runs on its
// owner's UI thread. A step's Window that does not resolve takes the
// desktop lane alone: the Batch fails on it before any step.
func (s Services) batchLanes(ctx context.Context, args json.RawMessage, w win.WindowInfo) laneSet {
	var b protocol.BatchArgs
	if json.Unmarshal(args, &b) != nil {
		return newLaneSet(w.ID, s.classKeys(w.ID, true, false)...)
	}
	var keys []string
	if b.Headed {
		keys = s.classKeys(w.ID, true, false)
	}
	prev := w
	for _, st := range b.Steps {
		sw := w
		if st.Window != "" {
			r, rerr := resolveWindow(s, st.Window)
			if rerr != nil {
				return newLaneSet(0, desktopLane)
			}
			sw = r
		}
		if st.InNew {
			sw = prev
		}
		step, err := withWindow(strconv.FormatUint(sw.ID, 10), st.Args)
		if err == nil && b.Headed && takesHeaded(st.Kind) {
			step, err = withHeaded(step) // as the Batch runs it
		}
		if err != nil {
			return newLaneSet(w.ID, s.classKeys(w.ID, true, false)...)
		}
		headed, page := inNewHeaded(st.Kind, step), false
		if !st.InNew {
			headed, page = s.stepClass(ctx, st.Kind, step, sw)
		}
		keys = append(keys, s.classKeys(sw.ID, headed, page)...)
		prev = sw
	}
	return newLaneSet(w.ID, keys...)
}

// heldLanesKey carries the lanes a request holds (onLanes) to its command.
type heldLanesKey struct{}

// lanesHeld reports whether the request under ctx holds every lane the
// command with these arguments takes: a Batch's `in new` step checks its
// new Window this way before it runs. Outside onLanes (no lanes) it
// holds them all, and so does a Window that does not resolve: the command
// fails on it. The desktop lane is left out: an `in new` step that asks
// for Headed took it up front (inNewHeaded), and one with no Headless
// route that does not ask is refused by its own command.
func (s Services) lanesHeld(ctx context.Context, name string, args json.RawMessage) bool {
	held, ok := ctx.Value(heldLanesKey{}).(laneSet)
	if !ok {
		return true
	}
	need := s.lanesFor(ctx, name, args)
	if need.window == 0 {
		return true
	}
	for _, k := range need.keys {
		if k != desktopLane && !held.has(k) {
			return false
		}
	}
	return true
}

// stepClass is laneClass for one command or Batch step.
func (s Services) stepClass(ctx context.Context, kind string, args json.RawMessage, w win.WindowInfo) (headed, page bool) {
	switch kind {
	case "restore-window", "minimize-window", "move-window", "resize-window", "close-window":
		return false, false // Window Actions are Headless (ADR 0011)
	case "activate-window":
		return true, false
	}
	if s.pageStep(ctx, kind, args, w) {
		return false, true
	}
	if kind == "press-key" || kind == "type-text" {
		// Since headless-max 21 a key or text goes Headed only with
		// --headed (anything else has a background route or is refused), so
		// the flag decides without probing the Window's focus twice.
		var f struct {
			Headed bool `json:"headed"`
			Post   bool `json:"post"`
		}
		json.Unmarshal(args, &f)
		return f.Headed && !f.Post, false
	}
	return !s.stepHeadless(ctx, kind, args, &w), false
}

// laned runs a command on its lanes (the registry's wrapper for every
// command that is not a read).
func (s Services) laned(name string, cmd Command) Command {
	if s.lanes == nil {
		return cmd // a zero Services (no defaultServices): tests wrap nothing
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		set := s.lanesFor(ctx, name, args)
		return s.onLanes(withLaneSet(ctx, set), set, cmd, args)
	}
}

// serialized runs a command on the desktop lane alone: the screenshot and
// Page-wait paths that call it hide the indicator and the cursors or hold
// the browser, as when there was one gate.
func (s Services) serialized(cmd Command) Command {
	if s.lanes == nil {
		return cmd
	}
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		return s.onLanes(ctx, newLaneSet(0, desktopLane), cmd, args)
	}
}

// onLanes takes set's lanes in order, runs cmd and releases them. The
// wait ends at the user's Esc (nothing was sent), at laneWaitLimit or the
// request's deadline (nothing was sent, naming the holder).
func (s Services) onLanes(ctx context.Context, set laneSet, cmd Command, args json.RawMessage) (string, *Error) {
	who := agent.FromContext(ctx)
	limit := s.LaneWait
	if limit <= 0 {
		limit = laneWaitLimit
	}
	if dl, ok := ctx.Deadline(); ok {
		// Stop waiting just before the request's own deadline, so the
		// refusal names the holder instead of the bare request timeout.
		if left := time.Until(dl) - laneDeadlineMargin; left < limit {
			limit = left
		}
	}
	timer := time.NewTimer(max(limit, 0))
	defer timer.Stop()
	wait := s.beginWait()
	var held []string
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			s.lanes.release(held[i])
		}
	}
	for _, key := range set.keys {
		ok, timedOut := wait.acquireOr(ctx, s.lanes.lane(key), timer.C)
		if !ok {
			wait.end()
			release()
			if wait.stopped() {
				return "", escStopBeforeActError()
			}
			if timedOut {
				return "", s.laneBusyError(key)
			}
			return "", queuedOutError() // the request was cancelled while it waited
		}
		s.lanes.setHolder(key, laneHolder{agent: who.Label, window: set.window})
		held = append(held, key)
	}
	wait.end()
	defer release()
	if wait.stopped() {
		return "", escStopBeforeActError() // the press landed as the last lane opened
	}
	if err := ctx.Err(); err != nil {
		return "", queuedOutError() // the budget expired as the last lane opened
	}
	if set.has(desktopLane) {
		s.setOverlayAgent(who) // the banner names the agent holding the desktop
	}
	return cmd(context.WithValue(ctx, heldLanesKey{}, set), args)
}

// laneBusyError is the refusal of a request that waited for a lane past
// its limit or budget.
func (s Services) laneBusyError(key string) *Error {
	what := "the desktop"
	h, ok := s.lanes.holder(key)
	if key != desktopLane && ok && h.window != 0 {
		what = "window " + strconv.FormatUint(h.window, 10)
	} else if key != desktopLane {
		what = "a Window this Action needs"
	}
	by := "another agent"
	if ok && h.agent != "" {
		by = "agent " + h.agent
	}
	return Fail(fmt.Sprintf("%s is using %s; nothing was sent", by, what),
		"retry once it is done, or work in a Window of another App meanwhile")
}

// queuedOutError is the failure a command reports when its request's
// budget expired while it waited for its lanes.
func queuedOutError() *Error {
	return Fail("another winuse Action was still running when this request's budget expired",
		"retry once the other Action finishes")
}
