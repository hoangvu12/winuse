package page

// Tab watches (browser merge ticket 06, spec "Routing and page Actions"):
// what a Tab's Page is waiting on, kept from its events for as long as the
// helper holds the Tab's session.
//
//   - A JavaScript dialog (Page.javascriptDialogOpening and Closed). While
//     one is open the Page runs no script and takes no input, so every
//     other page Action fails at once (DialogOpenError) instead of waiting
//     for an answer that never comes. Alerts are not answered for the
//     agent: winuse dialog <window> accept|dismiss does it. Ready is the
//     check every page Action makes first; it also catches, within
//     AnswerTimeout, a dialog the watch missed (NotAnsweringError). Nothing
//     on the way to it (finding the Tab, attaching, the session check)
//     waits for the Page's main thread.
//   - A file chooser. Page.setInterceptFileChooserDialog is on only while
//     winuse acts on the Tab (Intercept, held by each page Action that
//     sends input), so a click of winuse's that would open the native
//     Windows file dialog opens nothing on the screen: the browser reports
//     Page.fileChooserOpened instead. Outside winuse's Actions it is off,
//     so a file input the user clicks in their own browser works as usual
//     (and reports nothing). The chooser is kept after interception is off:
//     an intercepted chooser leaves nothing pending in the browser, and
//     winuse upload answers it later with DOM.setFileInputFiles on the
//     input's backendNodeId, which needs no interception.
//
// One watch per (connection, session). The Reader starts one for each Tab
// session it attaches; Input and the Window State read look it up.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
)

// Dialog is a JavaScript dialog a Page shows.
type Dialog struct {
	Type          string // alert, confirm, prompt or beforeunload
	Message       string
	DefaultPrompt string // a prompt's default text
	URL           string // the page that opened it
}

// Describe is the dialog in one line: `a confirm dialog: "Leave this
// page?"`, the message collapsed and cut at 200 characters.
func (d Dialog) Describe() string {
	typ := d.Type
	if typ == "" {
		typ = "alert"
	}
	msg := Collapse(d.Message)
	if r := []rune(msg); len(r) > 200 {
		msg = string(r[:200]) + "..."
	}
	if msg == "" {
		return fmt.Sprintf("%s %s dialog", article(typ), typ)
	}
	return fmt.Sprintf("%s %s dialog: %q", article(typ), typ, msg)
}

// DialogOpenError refuses an Action on a Page that shows a JavaScript
// dialog: it runs no script and takes no input until the dialog is
// answered. Nothing was sent.
type DialogOpenError struct{ Dialog Dialog }

func (e *DialogOpenError) Error() string {
	return "the Page shows " + e.Dialog.Describe() + ", and takes no input until it is answered"
}

// NotAnsweringError refuses an Action on a Page that did not answer a
// harmless probe within AnswerTimeout while the watch knows of no dialog:
// a JavaScript dialog the watch did not see open (it opened before winuse
// attached) holds the Page this way, as does a script that keeps it busy.
// Nothing was sent.
type NotAnsweringError struct{ After time.Duration }

func (e *NotAnsweringError) Error() string {
	return fmt.Sprintf("the Page did not answer within %s: it most likely shows a JavaScript dialog (alert, confirm or prompt) that winuse did not see open, or a script keeps it busy", e.After)
}

// AnswerTimeout bounds Ready's probe. A Page answers it in milliseconds
// unless a JavaScript dialog or a long script holds its main thread.
// Tests shrink it.
var AnswerTimeout = 3 * time.Second

// setupTimeout bounds the setup calls of a session (Page.enable) whose
// answer winuse does not need: the browser queues them on the session, in
// order, and a Page a dialog holds answers them only after it closes.
const setupTimeout = time.Second

// readyProbe is the expression Ready evaluates: nothing, in the Page's
// main thread.
const readyProbe = "/* winuse:ready */ 0"

// Ready says whether the Page of a Tab session takes Actions now. A dialog
// the watch saw fails at once (*DialogOpenError). Otherwise a harmless
// probe that needs the Page's main thread, which a JavaScript dialog holds,
// must answer within AnswerTimeout, else *NotAnsweringError (or
// *DialogOpenError when the watch learned of a dialog meanwhile). Any other
// failure of the probe passes (nil): the Action's own calls report it.
// Nothing reaches the Page's input.
func Ready(ctx context.Context, c *cdp.Client, session string) error {
	if d := OpenDialog(c, session); d != nil {
		return &DialogOpenError{Dialog: *d}
	}
	if c == nil {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, AnswerTimeout)
	defer cancel()
	_, err := c.CallSession(pctx, session, "Runtime.evaluate", map[string]any{"expression": readyProbe, "returnByValue": true, "silent": true})
	if err == nil || ctx.Err() != nil || pctx.Err() == nil {
		return nil
	}
	if d := OpenDialog(c, session); d != nil {
		return &DialogOpenError{Dialog: *d}
	}
	return &NotAnsweringError{After: AnswerTimeout}
}

// Chooser is a file chooser the browser held back (intercepted): the file
// input that asked for it.
type Chooser struct {
	BackendNodeID int64
	FrameID       string
	Multiple      bool // the input takes several files
}

// TabWatch follows one Tab session's dialog and file chooser events.
type TabWatch struct {
	c       *cdp.Client
	session string
	subs    []*cdp.Subscription

	mu           sync.Mutex
	dialog       *Dialog
	chooser      *Chooser
	choosers     int  // file choosers seen, for "did this click open one"
	intercepting bool // the last Page.setInterceptFileChooserDialog {enabled:true} was accepted
	changed      chan struct{}
	stopped      bool

	// ic serializes turning interception on and off; holds counts the
	// Actions that hold it (Intercept), on says the browser has it on, and
	// unsupported says the browser refused it (not asked again).
	ic          sync.Mutex
	holds       int
	on          bool
	unsupported bool
}

type watchKey struct {
	c       *cdp.Client
	session string
}

var watches = struct {
	mu sync.Mutex
	m  map[watchKey]*TabWatch
}{m: map[watchKey]*TabWatch{}}

// Watch returns the session's watch, starting it the first time:
// subscribe to the events, then Page.enable, which sends nothing to the
// Page's input and changes nothing the user sees (a dialog still shows and
// the user can still answer it). File chooser interception is not turned
// on here: Intercept does it around each Action.
func Watch(ctx context.Context, c *cdp.Client, session string) *TabWatch {
	key := watchKey{c, session}
	watches.mu.Lock()
	for k, w := range watches.m {
		if !k.c.Alive() {
			delete(watches.m, k)
			w.close()
		}
	}
	if w := watches.m[key]; w != nil {
		watches.mu.Unlock()
		return w
	}
	w := &TabWatch{c: c, session: session, changed: make(chan struct{})}
	// One subscription for every event of the session, so a dialog's
	// closing is never applied before its opening; apply ignores the rest.
	w.subs = append(w.subs, c.Subscribe(session, ""))
	watches.m[key] = w
	watches.mu.Unlock()
	for _, s := range w.subs {
		go w.follow(s)
	}
	// Bounded: a Page that already shows a dialog answers only once it
	// closes, and the events need no answer (setupTimeout).
	ectx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	c.CallSession(ectx, session, "Page.enable", nil)
	return w
}

// interceptOffTimeout bounds the call that turns interception off, which
// runs after the Action even when its context was cancelled (Esc, the
// deadline).
const interceptOffTimeout = 2 * time.Second

// Intercept holds file chooser interception on for the Tab while an Action
// that may open a chooser runs (a click, keys, a drag), and returns the
// release, which the caller defers. Holds nest: the first one sends
// Page.setInterceptFileChooserDialog {enabled:true}, the last release
// sends {enabled:false}, and the others send nothing. A browser that
// refuses interception (the extension or an old browser may) is not asked
// again; the Action runs without it, as before. A release that fails to
// turn interception off returns the error for the caller to log: the
// Action's own outcome stands, and the next release tries again. A nil
// watch holds nothing.
func (w *TabWatch) Intercept(ctx context.Context) (release func() error) {
	if w == nil {
		return func() error { return nil }
	}
	w.ic.Lock()
	w.holds++
	if !w.on && !w.unsupported {
		_, err := w.c.CallSession(ctx, w.session, "Page.setInterceptFileChooserDialog", map[string]any{"enabled": true})
		var pe *cdp.Error
		switch {
		case err == nil:
			w.on = true
		case errors.As(err, &pe):
			w.unsupported = true // the browser answered no: it lacks the command
		}
		w.mu.Lock()
		w.intercepting = err == nil
		w.mu.Unlock()
	}
	w.ic.Unlock()
	var once sync.Once
	return func() error {
		var err error
		once.Do(func() {
			w.ic.Lock()
			defer w.ic.Unlock()
			w.holds--
			if w.holds > 0 || !w.on {
				return
			}
			octx, cancel := context.WithTimeout(context.WithoutCancel(ctx), interceptOffTimeout)
			defer cancel()
			if _, err = w.c.CallSession(octx, w.session, "Page.setInterceptFileChooserDialog", map[string]any{"enabled": false}); err == nil {
				w.on = false
			}
		})
		return err
	}
}

// InterceptingNow reports whether the browser holds file choosers back at
// this moment (an Action holds Intercept, or a release failed to turn it
// off).
func (w *TabWatch) InterceptingNow() bool {
	if w == nil {
		return false
	}
	w.ic.Lock()
	defer w.ic.Unlock()
	return w.on
}

// WatchOf is the session's watch, or nil when none was started.
func WatchOf(c *cdp.Client, session string) *TabWatch {
	if c == nil {
		return nil
	}
	watches.mu.Lock()
	defer watches.mu.Unlock()
	return watches.m[watchKey{c, session}]
}

// Unwatch stops and forgets a session's watch.
func Unwatch(c *cdp.Client, session string) {
	watches.mu.Lock()
	w := watches.m[watchKey{c, session}]
	delete(watches.m, watchKey{c, session})
	watches.mu.Unlock()
	if w != nil {
		w.close()
	}
}

// OpenDialog is the dialog a watched session's Page shows, nil for none
// (or no watch).
func OpenDialog(c *cdp.Client, session string) *Dialog {
	if w := WatchOf(c, session); w != nil {
		return w.Dialog()
	}
	return nil
}

func (w *TabWatch) close() {
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
	for _, s := range w.subs {
		s.Close()
	}
}

// follow applies one subscription's events.
func (w *TabWatch) follow(s *cdp.Subscription) {
	for ev := range s.Events() {
		w.apply(ev)
	}
}

// apply records one event and wakes whoever waits on Changed.
func (w *TabWatch) apply(ev cdp.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch ev.Method {
	case "Page.javascriptDialogOpening":
		var p struct {
			URL           string `json:"url"`
			Message       string `json:"message"`
			Type          string `json:"type"`
			DefaultPrompt string `json:"defaultPrompt"`
		}
		json.Unmarshal(ev.Params, &p)
		w.dialog = &Dialog{Type: p.Type, Message: p.Message, DefaultPrompt: p.DefaultPrompt, URL: p.URL}
	case "Page.javascriptDialogClosed":
		w.dialog = nil
	case "Page.fileChooserOpened":
		var p struct {
			FrameID       string `json:"frameId"`
			Mode          string `json:"mode"`
			BackendNodeID int64  `json:"backendNodeId"`
		}
		json.Unmarshal(ev.Params, &p)
		w.chooser = &Chooser{BackendNodeID: p.BackendNodeID, FrameID: p.FrameID, Multiple: p.Mode == "selectMultiple"}
		w.choosers++
	case "Page.frameNavigated":
		var p struct {
			Frame struct {
				ParentID string `json:"parentId"`
			} `json:"frame"`
		}
		json.Unmarshal(ev.Params, &p)
		if p.Frame.ParentID != "" {
			return
		}
		w.chooser = nil // the input that asked is gone with its document
	default:
		return
	}
	close(w.changed)
	w.changed = make(chan struct{})
}

// Dialog is the open dialog, nil for none.
func (w *TabWatch) Dialog() *Dialog {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dialog == nil {
		return nil
	}
	d := *w.dialog
	return &d
}

// Chooser is the file chooser waiting for an answer (nil for none) and how
// many choosers the Tab opened so far.
func (w *TabWatch) Chooser() (*Chooser, int) {
	if w == nil {
		return nil, 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.chooser == nil {
		return nil, w.choosers
	}
	c := *w.chooser
	return &c, w.choosers
}

// TakeChooser forgets the waiting chooser once it was answered.
func (w *TabWatch) TakeChooser() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.chooser = nil
	w.mu.Unlock()
}

// Intercepting reports whether the browser accepted the last request to
// hold file choosers back (Intercept); false before the first one.
func (w *TabWatch) Intercepting() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.intercepting
}

// Changed is closed at the next dialog or chooser event.
func (w *TabWatch) Changed() <-chan struct{} {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.changed
}

// TabSession is the Tab's cached session for a window-level page Action
// (navigate, dialog, wait, eval, upload), attaching it the first time with
// its watch. A session the browser detached is attached again, once. It
// sends nothing that needs the Page's main thread, so it answers at once
// while a JavaScript dialog holds the Page: callers check Ready (all but
// dialog itself).
func (r *Reader) TabSession(ctx context.Context, conn *browser.Connection, targetID string) (string, error) {
	ts, err := r.session(ctx, conn, targetID)
	if err != nil {
		return "", err
	}
	// The browser answers Target.getTargetInfo on a session itself (its
	// Page is never asked, so a dialog does not hold it), and fails it for
	// a detached session. Any other answer, a refusal included, means the
	// session is there; so does no answer in time.
	pctx, cancel := context.WithTimeout(ctx, setupTimeout)
	_, err = conn.Client.CallSession(pctx, ts.id, "Target.getTargetInfo", nil)
	cancel()
	if err != nil && isGone(err) && ctx.Err() == nil {
		r.drop(tabKey{conn.Client, targetID})
		if ts, err = r.session(ctx, conn, targetID); err != nil {
			return "", err
		}
	}
	Watch(ctx, conn.Client, ts.id)
	return ts.id, nil
}
