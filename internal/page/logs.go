package page

// A Tab's log (.scratch/bladebro-ideas tickets 11, 19 and 29): what the
// browser's Log domain reports about a Tab's Page, kept in a ring of the
// last LogRingSize entries per Tab for `winuse logs` and the notes after
// Page Actions.
//
// Log.enable is turned on in every Page session winuse works in (the
// Reader's Tab sessions, so Window Pages and Tab targets alike). Ticket 11
// found that a Page cannot detect it, that it replays the entries stored
// before it was turned on with their original times, and that without
// Runtime it reports failed requests (source network, level error) and CSP
// and security errors only. Runtime.enable is never sent, and
// Network.enable never except on a Tab where --block is on (assets.go,
// ticket 28): a Page can detect both.
//
// The ring is the Tab's (its connection and target id), not a session's:
// the extension detaches a Tab nobody drove for 5 minutes, and the session
// that attaches it again replays what the ring already holds. An entry the
// ring holds, or one older than a full ring's oldest, is dropped; so is
// anything stamped at or before the newest entry the last Clear emptied.
// The Page's JavaScript exceptions join the same ring with Add, where the
// Tab captures them (exceptions.go, ticket 30).
//
// Entries join the ring on the CDP reader goroutine (cdp.Client.Listen), in
// wire order: whatever the browser logged before it answered a command is
// in the ring when that command returns. So a Log.enable that answered has
// its replay in the ring, and Mark, which waits for that, counts only what
// joins later. Since compares the ring's own order, never the browser's
// times with winuse's clock.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// LogRingSize is how many entries winuse keeps per Tab.
const LogRingSize = 50

// The caps on what one entry keeps.
const (
	logTextCap  = 1000
	logURLCap   = 500
	logStackCap = 4000
)

// The timing of the Log domain's setup.
const (
	// logEnableWait bounds the wait for Log.enable's answer, which carries
	// no data (the replay comes before it, as events).
	logEnableWait = 5 * time.Second
	// logReplayMax bounds how long Mark and WaitReplay wait for a Log.enable
	// sent just now to answer.
	logReplayMax = 750 * time.Millisecond
)

// LogEntry is one entry of a Tab's log.
type LogEntry struct {
	Time time.Time // when the browser logged it
	// Source is the Log domain's: network (a failed request), security (a
	// CSP or mixed-content error), and so on; exception for a JavaScript
	// exception (exceptions.go).
	Source string
	Level  string // verbose, info, warning or error
	Text   string
	URL    string // the request's or the resource's, when the entry has one
	Line   int    // 1-based line in URL, 0 when unknown
	Stack  string // the call frames, one per line ("" for most Log entries)
	seq    uint64 // the order it joined the ring
}

func (e LogEntry) same(o LogEntry) bool {
	return e.Time.Equal(o.Time) && e.Source == o.Source && e.Level == o.Level && e.Text == o.Text && e.URL == o.URL
}

// LogMark is a point in a Tab's log: what Since counts from.
type LogMark struct {
	seq uint64
}

// TabLog is one Tab's log.
type TabLog struct {
	c        *cdp.Client
	targetID string

	mu       sync.Mutex
	entries  []LogEntry // by time, oldest first
	seq      uint64
	cleared  time.Time // the newest entry's time at the last Clear, browser's clock
	sessions map[string]*logSession

	// Exception capture (exceptions.go): on for the Tab's life once on,
	// the Tab's random event name, and the listener's install per session.
	capture  bool
	event    string
	captures map[string]*captureSession
}

// logSession is one session of the Tab with Log on.
type logSession struct {
	stop    func()        // ends the session's listener
	used    time.Time     // the last WatchLog of the session
	enabled chan struct{} // closed once Log.enable answered or gave up
}

type logKey struct {
	c        *cdp.Client
	targetID string
}

var tabLogs = struct {
	mu sync.Mutex
	m  map[logKey]*TabLog
}{m: map[logKey]*TabLog{}}

// WatchLog returns the Tab's log, turning Log on in session the first time
// (and again after enableAgain unused): the entries it replays and every
// later one join the Tab's ring. Log.enable goes out without waiting for its
// answer; Mark and WaitReplay wait for it. session "" (a Tab that shows a
// browser page, not attached) only finds the log. Nil for a nil client.
func WatchLog(ctx context.Context, c *cdp.Client, session, targetID string) *TabLog {
	if c == nil || targetID == "" {
		return nil
	}
	key := logKey{c, targetID}
	tabLogs.mu.Lock()
	for k, l := range tabLogs.m {
		if !k.c.Alive() {
			delete(tabLogs.m, k)
			l.close()
		}
	}
	l := tabLogs.m[key]
	if l == nil {
		l = &TabLog{c: c, targetID: targetID, sessions: map[string]*logSession{}}
		tabLogs.m[key] = l
	}
	tabLogs.mu.Unlock()
	if session == "" {
		return l
	}
	l.mu.Lock()
	s := l.sessions[session]
	now := time.Now()
	if s != nil && now.Sub(s.used) <= enableAgain {
		s.used = now
		l.mu.Unlock()
		return l
	}
	if s == nil {
		// Before Log.enable goes out, so its replay finds the listener.
		s = &logSession{stop: c.Listen(session, "Log.entryAdded", l.entryAdded)}
		l.sessions[session] = s
	}
	s.used = now
	enabled := make(chan struct{})
	s.enabled = enabled
	l.mu.Unlock()
	p := c.Send(session, "Log.enable", nil)
	go func() {
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), logEnableWait)
		defer cancel()
		p.Wait(wctx)
		close(enabled)
	}()
	return l
}

// entryAdded is the sessions' listener: it runs on the CDP reader, so it
// only parses and adds.
func (l *TabLog) entryAdded(ev cdp.Event) {
	if e, ok := parseLogEntry(ev.Params); ok {
		l.Add(e)
	}
}

// TabLogOf is the Tab's log, nil when winuse never watched it.
func TabLogOf(c *cdp.Client, targetID string) *TabLog {
	tabLogs.mu.Lock()
	defer tabLogs.mu.Unlock()
	return tabLogs.m[logKey{c, targetID}]
}

// UnwatchLog stops following a session (the Reader dropped it); the Tab's
// ring stays.
func UnwatchLog(c *cdp.Client, session string) {
	tabLogs.mu.Lock()
	var logs []*TabLog
	for k, l := range tabLogs.m {
		if k.c == c {
			logs = append(logs, l)
		}
	}
	tabLogs.mu.Unlock()
	for _, l := range logs {
		l.mu.Lock()
		s := l.sessions[session]
		delete(l.sessions, session)
		delete(l.captures, session)
		l.mu.Unlock()
		if s != nil {
			s.stop()
		}
	}
}

func (l *TabLog) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, s := range l.sessions {
		s.stop()
		delete(l.sessions, id)
	}
}

// parseLogEntry reads Log.entryAdded's params.
func parseLogEntry(raw json.RawMessage) (LogEntry, bool) {
	var p struct {
		Entry struct {
			Source     string  `json:"source"`
			Level      string  `json:"level"`
			Text       string  `json:"text"`
			Timestamp  float64 `json:"timestamp"` // ms since the epoch
			URL        string  `json:"url"`
			LineNumber *int    `json:"lineNumber"` // 0-based
			StackTrace *struct {
				CallFrames []struct {
					FunctionName string `json:"functionName"`
					URL          string `json:"url"`
					LineNumber   int    `json:"lineNumber"`
					ColumnNumber int    `json:"columnNumber"`
				} `json:"callFrames"`
			} `json:"stackTrace"`
		} `json:"entry"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return LogEntry{}, false
	}
	in := p.Entry
	e := LogEntry{Source: in.Source, Level: in.Level, Text: in.Text, URL: in.URL}
	if in.Timestamp > 0 {
		// Whole milliseconds apart from the fraction: ms since the epoch
		// times 1e6 is past float64's exact integers. The fraction is
		// rounded to the microsecond, the browser's finest: a float64 this
		// size is off by up to about 120 ns, and a replay must get the same
		// time to be known as one.
		ms := math.Floor(in.Timestamp)
		e.Time = time.UnixMilli(int64(ms)).Add(time.Duration(math.Round((in.Timestamp-ms)*1000)) * time.Microsecond)
	}
	if in.LineNumber != nil {
		e.Line = *in.LineNumber + 1
	}
	if in.StackTrace != nil {
		var b strings.Builder
		for _, f := range in.StackTrace.CallFrames {
			name := f.FunctionName
			if name == "" {
				name = "(anonymous)"
			}
			fmt.Fprintf(&b, "at %s (%s:%d:%d)\n", name, f.URL, f.LineNumber+1, f.ColumnNumber+1)
		}
		e.Stack = strings.TrimSuffix(b.String(), "\n")
	}
	return e, true
}

// Add puts an entry in the Tab's ring, unless the ring holds it already, it
// is older than a full ring's oldest, or it is no newer than the newest
// entry the last Clear emptied. An entry with no time is stamped now.
// Exception records (exceptions.go) join through here too. The Log
// domain's entries come through the reader goroutine, so Add never calls
// the browser.
func (l *TabLog) Add(e LogEntry) {
	if l == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Text = CutRunes(e.Text, logTextCap)
	e.URL = CutRunes(e.URL, logURLCap)
	e.Stack = CutRunes(e.Stack, logStackCap)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.cleared.IsZero() && !e.Time.After(l.cleared) {
		return
	}
	for _, o := range l.entries {
		if o.same(e) {
			return
		}
	}
	if len(l.entries) >= LogRingSize && e.Time.Before(l.entries[0].Time) {
		return
	}
	l.seq++
	e.seq = l.seq
	i := len(l.entries)
	for i > 0 && l.entries[i-1].Time.After(e.Time) {
		i--
	}
	l.entries = append(l.entries, LogEntry{})
	copy(l.entries[i+1:], l.entries[i:])
	l.entries[i] = e
	if len(l.entries) > LogRingSize {
		l.entries = append([]LogEntry(nil), l.entries[len(l.entries)-LogRingSize:]...)
	}
}

// Entries is the ring, oldest first.
func (l *TabLog) Entries() []LogEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]LogEntry(nil), l.entries...)
}

// Mark is the log now, for Since. It first waits (WaitReplay) for a Log.enable
// sent just now, so the entries it replays, logged before the mark, are in
// the ring already and not counted after it.
func (l *TabLog) Mark(ctx context.Context) LogMark {
	if l == nil {
		return LogMark{}
	}
	l.WaitReplay(ctx)
	l.mu.Lock()
	defer l.mu.Unlock()
	return LogMark{seq: l.seq}
}

// Since is what joined the ring after the mark and is still in it: what
// the Page logged since, and what a session attached since replayed (a Tab
// that showed a browser page and loaded one).
func (l *TabLog) Since(m LogMark) []LogEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []LogEntry
	for _, e := range l.entries {
		if e.seq > m.seq {
			out = append(out, e)
		}
	}
	return out
}

// WaitReplay waits, within logReplayMax, for the Log.enable of the Tab's
// sessions to answer. The entries it replayed came before its answer, so
// they are in the ring then, and a read right after the first attach shows
// them. A session turned on long ago returns at once.
func (l *TabLog) WaitReplay(ctx context.Context) {
	if l == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, logReplayMax)
	defer cancel()
	l.mu.Lock()
	var waits []chan struct{}
	for _, s := range l.sessions {
		if s.enabled != nil {
			waits = append(waits, s.enabled)
		}
	}
	l.mu.Unlock()
	for _, w := range waits {
		select {
		case <-w:
		case <-ctx.Done():
			return
		}
	}
}

// Clear empties the ring and sends Log.clear on the Tab's sessions, which
// empties the browser's store that Log.enable replays from. An entry no
// newer than the newest it emptied never joins again, even when Log.clear
// failed. (The browser's times are compared only with each other.)
func (l *TabLog) Clear(ctx context.Context) {
	if l == nil {
		return
	}
	l.mu.Lock()
	if n := len(l.entries); n > 0 && l.entries[n-1].Time.After(l.cleared) {
		l.cleared = l.entries[n-1].Time
	}
	l.entries = nil
	var sessions []string
	for id := range l.sessions {
		sessions = append(sessions, id)
	}
	l.mu.Unlock()
	for _, id := range sessions {
		cctx, cancel := context.WithTimeout(ctx, setupTimeout)
		l.c.CallSession(cctx, id, "Log.clear", nil)
		cancel()
	}
}
