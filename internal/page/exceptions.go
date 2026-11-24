package page

// A Tab's JavaScript exceptions (.scratch/bladebro-ideas ticket 30, after
// tickets 11 and 19), without Runtime.enable, which a Page can detect.
//
//   - The listener. exceptions.js runs in the Page's main world, installed
//     with Page.addScriptToEvaluateOnNewDocument (no worldName) so it runs
//     before the Page's own scripts. It catches error and
//     unhandledrejection with message, file, line and stack, using only the
//     built-ins it saved, and hands each record to winuse's isolated world
//     as a CustomEvent named at random per Tab. It sets no global and no
//     window.onerror, and never wraps console.
//   - The buffer. exceptions_world.js runs in the isolated world
//     exceptionWorld (its own addScriptToEvaluateOnNewDocument, run in the
//     loaded document too) and keeps the last LogRingSize records.
//     PullExceptions reads and empties it with Runtime.evaluate in that
//     world (Page.createIsolatedWorld's context id, no Runtime.enable) and
//     adds the records to the Tab's ring as source exception, level error.
//   - When. Capture turns it on for the Tab's life: by default in a
//     browser winuse launched or cloned (Reader.session) and in a Tab tab
//     new opened; in the user's own Tabs only after winuse logs --capture.
//     It turns the Page domain on (without it Chrome runs neither script
//     in a new document) and installs in a session once, and again after
//     the extension's
//     silent idle detach (logEnableAgain), removing the old scripts first.
//     The event name stays the Tab's, so a Page loaded under an earlier
//     session still reaches the new buffer (exceptions_world.js asks).
//   - Coverage. Only documents created after the install have the
//     listener; PullExceptions says whether the current one does.

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
)

//go:embed exceptions.js
var exceptionsJS string

//go:embed exceptions_world.js
var exceptionsWorldJS string

// exceptionWorld names the isolated world that keeps a Tab's records.
const exceptionWorld = "winuse-exceptions"

// exceptionKey is the isolated world's global that returns the records.
const exceptionKey = "__winuseExceptions"

// captureWait bounds the wait for the scripts' identifiers before a read.
var captureWait = 2 * time.Second

// captureSession is the listener's install in one session of the Tab.
type captureSession struct {
	used    time.Time     // the last Capture of the session
	scripts []string      // the scripts' identifiers once answered; none: not installed
	done    chan struct{} // closed once the install answered or gave up
}

// Capture turns exception capture on for the Tab's life and installs the
// listener in session if it is not there yet (or was not used for
// logEnableAgain, as the extension's idle detach drops it). The scripts go
// out without waiting for their answers; the session's later commands come
// after them. session "" (a Tab not attached) only turns capture on. It
// says whether capture was off before.
func (l *TabLog) Capture(ctx context.Context, session string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	first := !l.capture
	l.capture = true
	if l.event == "" {
		l.event = newEventName()
	}
	if session == "" {
		l.mu.Unlock()
		return first
	}
	if l.captures == nil {
		l.captures = map[string]*captureSession{}
	}
	cs := l.captures[session]
	now := time.Now()
	if cs != nil && now.Sub(cs.used) <= logEnableAgain {
		cs.used = now
		l.mu.Unlock()
		return first
	}
	prev := cs
	cs = &captureSession{used: now, done: make(chan struct{})}
	l.captures[session] = cs
	event := l.event
	l.mu.Unlock()

	// Chrome runs the scripts in a new document only while the session has
	// the Page domain on (the e2e test found the listener silent without
	// it; the watch turns it on in the Reader's sessions, but not again
	// after the extension's idle detach). A Page cannot detect it.
	l.c.Send(session, "Page.enable", nil)
	if prev != nil {
		// The old scripts, gone with an idle detach or not: removing one
		// that is gone fails harmlessly, and one left would run twice.
		select {
		case <-prev.done:
		case <-time.After(captureWait):
		}
		l.mu.Lock()
		old := prev.scripts
		l.mu.Unlock()
		for _, id := range old {
			l.c.Send(session, "Page.removeScriptToEvaluateOnNewDocument", map[string]any{"identifier": id})
		}
	}
	world := l.c.Send(session, "Page.addScriptToEvaluateOnNewDocument", map[string]any{
		"source": exceptionScript(exceptionsWorldJS, event), "worldName": exceptionWorld, "runImmediately": true})
	main := l.c.Send(session, "Page.addScriptToEvaluateOnNewDocument", map[string]any{
		"source": exceptionScript(exceptionsJS, event)})
	go func() {
		defer close(cs.done)
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), logEnableWait)
		defer cancel()
		var ids []string
		for _, p := range []*cdp.Pending{world, main} {
			res, err := p.Wait(wctx)
			var r struct {
				Identifier string `json:"identifier"`
			}
			if err != nil || json.Unmarshal(res, &r) != nil || r.Identifier == "" {
				continue
			}
			ids = append(ids, r.Identifier)
		}
		if len(ids) < 2 {
			// Half an install catches nothing; take back what is there.
			for _, id := range ids {
				l.c.Send(session, "Page.removeScriptToEvaluateOnNewDocument", map[string]any{"identifier": id})
			}
			ids = nil
		}
		l.mu.Lock()
		cs.scripts = ids
		l.mu.Unlock()
	}()
	return first
}

// captureByDefault turns capture on, with nothing sent, in a Tab of a
// browser winuse launched or cloned: its Tabs are winuse's, never the
// user's. The listener installs at the Tab's next Page Action or logs.
func captureByDefault(conn *browser.Connection, l *TabLog) {
	if conn.Mode == browser.ModeLaunch || conn.Mode == browser.ModeClone {
		l.Capture(context.Background(), "")
	}
}

// Capturing says whether the Tab captures its exceptions.
func (l *TabLog) Capturing() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.capture
}

func (l *TabLog) eventName() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.event
}

// newEventName is a Tab's event name: random, so the Page cannot listen
// for it.
func newEventName() string {
	b := make([]byte, 10)
	rand.Read(b)
	return "wu" + hex.EncodeToString(b)
}

// exceptionScript fills a script's names in and leaves out its whole-line
// comments.
func exceptionScript(js, event string) string {
	var b strings.Builder
	for _, line := range strings.Split(js, "\n") {
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		b.WriteString(line + "\n")
	}
	return strings.NewReplacer("__NAME__", event, "__KEY__", exceptionKey, "__MAX__", strconv.Itoa(LogRingSize)).Replace(b.String())
}

// pullExpression returns the isolated world's records and empties it.
var pullExpression = "/* winuse:exceptions */ (() => { const f = globalThis." + exceptionKey + "; return f ? f() : ''; })()"

// exceptionRecord is one record of the buffer.
type exceptionRecord struct {
	T float64 `json:"t"` // ms since the epoch
	M string  `json:"m"`
	U string  `json:"u"`
	L int     `json:"l"`
	S string  `json:"s"`
}

// PullExceptions reads the records the listener handed to session's
// isolated world since the last read and adds them to the Tab's ring.
// covered says the Page's current document has the listener; ok is false
// when nothing could be read (the listener is not installed in session, a
// dialog holds the Page, or the read failed).
func (l *TabLog) PullExceptions(ctx context.Context, session string) (covered, ok bool) {
	if l == nil || session == "" {
		return false, false
	}
	l.mu.Lock()
	cs := l.captures[session]
	l.mu.Unlock()
	if cs == nil || OpenDialog(l.c, session) != nil {
		return false, false // a dialog holds the Page's main thread
	}
	wctx, cancel := context.WithTimeout(ctx, captureWait)
	select {
	case <-cs.done:
	case <-wctx.Done():
	}
	cancel()
	l.mu.Lock()
	installed := len(cs.scripts) > 0
	l.mu.Unlock()
	if !installed {
		return false, false
	}
	ctxID, err := isolatedWorld(ctx, l.c, session, "", exceptionWorld)
	if err != nil {
		return false, false
	}
	res, err := l.c.CallSession(ctx, session, "Runtime.evaluate", map[string]any{
		"expression": pullExpression, "contextId": ctxID, "returnByValue": true, "silent": true})
	if err != nil {
		return false, false
	}
	var ev struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(res, &ev) != nil || ev.Result.Value == "" {
		return false, false
	}
	var buf struct {
		Covered bool              `json:"c"`
		Records []exceptionRecord `json:"r"`
	}
	if json.Unmarshal([]byte(ev.Result.Value), &buf) != nil {
		return false, false
	}
	for _, r := range buf.Records {
		l.Add(exceptionEntry(r))
	}
	return buf.Covered, true
}

// exceptionEntry is a record as a log entry: the stack's frames only (its
// first line repeats the message), and for a rejection, which has no file
// or line, its first frame's.
func exceptionEntry(r exceptionRecord) LogEntry {
	e := LogEntry{Source: "exception", Level: "error", Text: r.M, URL: r.U, Line: r.L}
	if r.T > 0 {
		// Whole milliseconds apart from the fraction, rounded to the
		// microsecond, as parseLogEntry does.
		ms := math.Floor(r.T)
		e.Time = time.UnixMilli(int64(ms)).Add(time.Duration(math.Round((r.T-ms)*1000)) * time.Microsecond)
	}
	var frames []string
	for _, line := range strings.Split(r.S, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "at ") {
			frames = append(frames, t)
		}
	}
	e.Stack = strings.Join(frames, "\n")
	if e.URL == "" && len(frames) > 0 {
		e.URL, e.Line = framePlace(frames[0])
	}
	return e
}

// frameRE is a stack frame's place: "url:line:col", in parentheses or not.
var frameRE = regexp.MustCompile(`\(?([^\s()]+):(\d+):\d+\)?$`)

// framePlace is a frame's URL and line, "" when it names none.
func framePlace(frame string) (string, int) {
	m := frameRE.FindStringSubmatch(frame)
	if m == nil {
		return "", 0
	}
	n, _ := strconv.Atoi(m[2])
	return m[1], n
}

// ScriptPlace is where an exception was thrown as a note names it: the
// script's file name and line ("app.js:120"), its host for a page's root.
func ScriptPlace(raw string, line int) string {
	if raw == "" {
		return ""
	}
	place := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		place = u.Host
		if segs := strings.Split(strings.Trim(u.Path, "/"), "/"); segs[len(segs)-1] != "" {
			place = segs[len(segs)-1]
		}
	}
	if line > 0 {
		place += ":" + strconv.Itoa(line)
	}
	return place
}
