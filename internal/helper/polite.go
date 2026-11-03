package helper

// Polite real input (multi-agent ticket 08, ADR 0014 Decision 8's last
// step, after cua and Raymond Chen's practice): a Headed Action, which
// takes the user's foreground, pointer and keyboard, disturbs the user as
// little as it can.
//
//   - It waits while the user is typing or using the mouse: input quiet
//     for less than politeQuiet delays the start, up to politeWait, then
//     the Action is refused with nothing sent. winuse's own last input
//     does not count.
//   - It saves the foreground Window and the cursor position first, and
//     afterwards gives them back: the cursor when it is within
//     politeCursorSlack of where winuse left it; the foreground when the
//     target Window still holds it (the user, a flyout or Start did not
//     move it meanwhile) and the Action left nothing open that the
//     foreground keeps alive: no new Window of the target's App, a popup
//     menu (#32768) included. Taking the foreground away would close such
//     a menu, dropdown or dialog. A shell surface (the taskbar) keeps it
//     too. A Chromium Window re-activates itself after a hand-back, so the
//     foreground is checked again for a moment. activate-window and
//     maximize-window keep the foreground, and hover keeps the pointer:
//     that is their point. A Batch saves once before its first step and
//     gives back once after its last, from any Window a Headed step acted
//     in (its steps can name several, desktop-parity 24). When winuse keeps the foreground for
//     a menu it opened, or the foreground moved elsewhere, a Note says so.
//   - After the activation, a WM_NULL round trip to the Window confirms
//     its thread took it before any input goes in (else nothing is sent).
//   - After the Action, and after any release of held keys, the injected
//     input is flushed before anything is given back: a sentinel key the
//     Esc hook swallows (win.EscWatcher.FlushInput) proves every earlier
//     event reached the target's queue, so moving the foreground cannot
//     carry the target's keys into the user's Window (measured live: the
//     tail of a long Headed type-text went astray without it). Then a
//     second WM_NULL round trip.
//   - Every exit path releases the keys and mouse buttons winuse still
//     holds (win.ReleaseHeld).
//   - Real user input during the Action stops it like Esc (the hook's
//     yield, win/eschook.go); the stop then says the user took over.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

const (
	politeQuiet        = 500 * time.Millisecond // the user's input must be this quiet before winuse takes over
	politeWait         = 3 * time.Second        // how long winuse waits for that, at most
	politePoll         = 50 * time.Millisecond
	politeOwnSlack     = 100 * time.Millisecond // input this close to winuse's own last input is winuse's
	politeCursorSlack  = 2                      // pixels: the cursor is still where winuse left it
	politeRecheck      = 600 * time.Millisecond // how long a Chromium hand-back is watched
	politeRecheckEvery = 100 * time.Millisecond
	politeFlush        = 2 * time.Second        // how long the injected input may take to come through the hooks
	politeFlushMiss    = 200 * time.Millisecond // the pause instead, when the flush cannot run
)

// politeState is what the Headed pipeline remembers across Actions: when
// winuse's own input last ended.
type politeState struct {
	lastOwn atomic.Int64 // unix nanoseconds; 0 never
}

// ownInputEnded records that winuse's own input ended now.
func (p *politeState) ownInputEnded(now time.Time) { p.lastOwn.Store(now.UnixNano()) }

// lastInputWasOwn reports whether the last input, idle ago, was winuse's.
func (p *politeState) lastInputWasOwn(now time.Time, idle time.Duration) bool {
	n := p.lastOwn.Load()
	if n == 0 {
		return false
	}
	return now.Sub(time.Unix(0, n)) <= idle+politeOwnSlack
}

// savedDesk is what a Headed Action gives back.
type savedDesk struct {
	windows      map[uint64]bool // the Windows open before (AllWindows); nil: unknown
	foreground   uint64
	cursorX      int32
	cursorY      int32
	cursorOK     bool
	leftX, leftY int32 // where winuse left the cursor
	leftOK       bool
}

// politeRun is one Headed run's (an Action's, or a whole Batch's) record:
// what was saved, the Window it acted on, and what it keeps.
type politeRun struct {
	saved          savedDesk
	target         win.WindowInfo
	headed         bool
	keepForeground bool
	keepPointer    bool
	visited        []win.WindowInfo // a Batch's: every Headed step's Window, its steps in several (desktop-parity 24)
}

type politeKey struct{}

// politeRunOf is the run on ctx, or nil.
func politeRunOf(ctx context.Context) *politeRun {
	r, _ := ctx.Value(politeKey{}).(*politeRun)
	return r
}

// keepForegroundHere marks the running Headed Action as one whose point is
// the foreground (activate-window, maximize-window): it is not given back.
func keepForegroundHere(ctx context.Context) {
	if r := politeRunOf(ctx); r != nil {
		r.keepForeground = true
	}
}

// keepPointerHere marks the running Headed Action as one whose point is
// where it leaves the pointer (hover): the cursor is not put back.
func keepPointerHere(ctx context.Context) {
	if r := politeRunOf(ctx); r != nil {
		r.keepPointer = true
	}
}

// waitForQuietInput waits until the user's input has been quiet for
// politeQuiet, or the last input was winuse's own; past politeWait (or
// the request's deadline) it refuses with nothing sent.
func (s Services) waitForQuietInput(ctx context.Context) *Error {
	if s.InputIdle == nil {
		return nil
	}
	deadline := s.Now().Add(politeWait)
	for {
		idle := s.InputIdle()
		if idle >= politeQuiet || s.polite.lastInputWasOwn(s.Now(), idle) {
			return nil
		}
		if s.Now().After(deadline) || ctx.Err() != nil {
			return nothingSent(Fail("the user is typing or using the mouse right now, so winuse did not take the foreground",
				"wait until the user pauses, or ask them, then retry; a Headless route, --into or --post leaves their input alone"))
		}
		s.sleep(politePoll)
	}
}

// saveDesk reads the foreground Window and the cursor before a Headed run.
func (s Services) saveDesk() savedDesk {
	var d savedDesk
	if s.Foreground != nil {
		d.foreground = s.Foreground()
	}
	if s.CursorPos != nil {
		d.cursorX, d.cursorY, d.cursorOK = s.CursorPos()
	}
	if s.AllWindows != nil {
		if ws, err := s.AllWindows(); err == nil {
			d.windows = make(map[uint64]bool, len(ws))
			for _, w := range ws {
				d.windows[w.ID] = true
			}
		}
	}
	return d
}

// leftOpen reports whether the run left a new Window of target's App open
// (a popup menu, a dropdown, a dialog): the foreground keeps it alive.
func (s Services) leftOpen(d savedDesk, target win.WindowInfo) bool {
	if d.windows == nil || s.AllWindows == nil {
		return false
	}
	ws, err := s.AllWindows()
	if err != nil {
		return true // unknown: keep the foreground rather than close something
	}
	for _, w := range ws {
		if !d.windows[w.ID] && w.PID == target.PID && w.PID != 0 {
			return true
		}
	}
	return false
}

// noteLeft records where winuse left the cursor after its input.
func (s Services) noteLeft(r *politeRun) {
	if s.CursorPos != nil {
		r.saved.leftX, r.saved.leftY, r.saved.leftOK = s.CursorPos()
	}
}

// giveBack gives the user's foreground and cursor back after a Headed
// run (see the file comment) and returns the Note line when it kept the
// foreground for a reason the agent should know. Best effort: nothing here
// fails the Action.
func (s Services) giveBack(r *politeRun) string {
	if !r.headed {
		return ""
	}
	d := r.saved
	if !r.keepPointer && d.cursorOK && d.leftOK && s.CursorPos != nil && s.PutCursor != nil &&
		(d.leftX != d.cursorX || d.leftY != d.cursorY) {
		if x, y, ok := s.CursorPos(); ok && near(x, d.leftX) && near(y, d.leftY) {
			if err := s.PutCursor(d.cursorX, d.cursorY); err != nil {
				log.Printf("putting the cursor back: %v", err)
			}
		}
	}
	w := r.target
	if r.keepForeground || w.ShellWindow || d.foreground == 0 || d.foreground == w.ID || s.Foreground == nil || s.Activate == nil {
		return ""
	}
	fg := s.Foreground()
	for _, v := range r.visited {
		if fg != w.ID && fg == v.ID {
			w = v // an earlier Headed step's Window of the Batch (desktop-parity 24): a dialog its last step closed
		}
	}
	if fg != w.ID {
		// The user, a flyout or Start moved the foreground meanwhile.
		if fg == 0 || fg == d.foreground {
			return ""
		}
		return fmt.Sprintf("%sthe foreground was not given back: it moved to window %d during the Action\n", notePrefix, fg)
	}
	if s.leftOpen(d, w) {
		// A menu or Window the Action opened lives on the foreground.
		return fmt.Sprintf("%sthe foreground stays on window %d: the Action left a menu or Window of its App open\n", notePrefix, w.ID)
	}
	if err := s.Activate(d.foreground); err != nil {
		log.Printf("giving the foreground back to window %d: %v", d.foreground, err)
		return ""
	}
	if !strings.HasPrefix(w.Class, "Chrome_WidgetWin_") {
		return ""
	}
	// Chromium re-activates itself after a hand-back: watch for a moment
	// and give it back again, unless the user moved it elsewhere.
	for waited := time.Duration(0); waited < politeRecheck; waited += politeRecheckEvery {
		s.sleep(politeRecheckEvery)
		switch fg := s.Foreground(); fg {
		case w.ID:
			_ = s.Activate(d.foreground)
		case d.foreground:
		default:
			return ""
		}
	}
	return ""
}

func near(a, b int32) bool { return a-b <= politeCursorSlack && b-a <= politeCursorSlack }

// beginPolite opens a Headed Action's politeness: its own run, saved now,
// unless a Batch's run is already on ctx (the Batch saves and gives back
// once). It returns the ctx to run with, the run, and whether it is the
// Action's own.
func (s Services) beginPolite(ctx context.Context, w win.WindowInfo) (context.Context, *politeRun, bool) {
	if r := politeRunOf(ctx); r != nil {
		r.target, r.headed = w, true
		r.visited = append(r.visited, w)
		return ctx, r, false
	}
	r := &politeRun{saved: s.saveDesk(), target: w, headed: true}
	return context.WithValue(ctx, politeKey{}, r), r, true
}

// beginBatchPolite opens a Batch's politeness: saved before its first
// step; its Headed steps record their Window into it.
func (s Services) beginBatchPolite(ctx context.Context) (context.Context, *politeRun) {
	r := &politeRun{saved: s.saveDesk()}
	return context.WithValue(ctx, politeKey{}, r), r
}

// releaseHeld releases what winuse still holds down, records the end of
// its own input, and returns how many keys and buttons it released.
func (s Services) releaseHeld() int {
	n := 0
	if s.ReleaseHeld != nil {
		var err error
		if n, err = s.ReleaseHeld(); err != nil {
			log.Printf("releasing %d held keys and buttons: %v", n, err)
		}
	}
	s.polite.ownInputEnded(s.Now())
	return n
}

// inputFlusher is an Esc service that can flush the injected input
// through its hook (*win.EscWatcher).
type inputFlusher interface {
	FlushInput(timeout time.Duration) error
}

// flushInput waits until the input winuse injected has reached its
// target's queue (see the file comment). Without a hook to flush through
// it pauses politeFlushMiss instead; the offline fakes have neither.
func (s Services) flushInput() {
	f, ok := s.Esc.(inputFlusher)
	if !ok {
		return
	}
	if err := f.FlushInput(politeFlush); err != nil {
		log.Printf("flushing the injected input: %v", err)
		s.sleep(politeFlushMiss)
	}
}

// yieldStopMessage is the stop of a Headed Action the user took over by
// typing or clicking (the hook's yield).
const yieldStopMessage = "winuse stopped: the user used the keyboard or mouse during a Headed Action"

// yieldReporter is an Esc service that tells a yield from Esc
// (*win.EscWatcher).
type yieldReporter interface{ Yielded() bool }

// stopErrorFor is the stop of a Headed scope: the user's Esc, or their own
// input when the hook yielded to it. Both exit 130.
func stopErrorFor(esc EscWatch) *Error {
	if y, ok := esc.(yieldReporter); ok && y.Yielded() {
		return &Error{Msg: yieldStopMessage, Hint: escStopHint, Exit: protocol.ExitEsc}
	}
	return escStopError()
}
