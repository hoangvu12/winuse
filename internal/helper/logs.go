package helper

// Failed requests and CSP errors (.scratch/bladebro-ideas ticket 29, after
// tickets 11 and 19). Every Page session winuse works in has the browser's
// Log domain on (page/logs.go): never Runtime, and never Network except on
// a Tab where --block is on (assets.go), both of which a Page can detect.
// Each Tab keeps a ring of its last page.LogRingSize entries.
//
// After a Page Action (runPage, runPageInput, and the page-only Actions
// of runPageWindow), new error entries the Page logged while it ran, or
// while its reports settled, add a note; the Action stays `ok`:
//
//	Note: 2 failed requests: 404 /api/save, 500 /api/user
//	Note: 1 security error: Refused to load the script 'https://...' because ...
//
// A same-site request prints as its path, another site's as host and path;
// at most 3 requests are named, and the rest are counted with the command
// that lists them. A Batch notes once, after its steps.
//
// `winuse logs <window>[:<tab>] [--capture] [--clear] [--max-chars N |
// --out <file>]` prints the ring, newest last; --clear empties it after
// printing (and Log.clear empties the browser's store that Log.enable
// replays from); the global --json answers with a JSON object. It only
// reads: no lane, no Agent Cursor, nothing sent to the Page's input.
//
// JavaScript exceptions (ticket 30, page/exceptions.go) join the same ring
// in a Tab that captures them: by default a Tab of a browser winuse
// launched or cloned and one tab new opened; the user's own Tabs after
// `winuse logs <window> --capture`, for the Tab's life. The listener goes
// in before each Page Action (markPageLogs), so a page the Action loads
// has it, and its records are read before the notes and before logs
// prints:
//
//	Note: the Page threw: TypeError: x is undefined (app.js:120)
//
// It covers pages loaded after it went in: the first --capture says so,
// as logs does for a page loaded before, and both offer reload.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// logNoteNamed is how many failed requests a note names.
const logNoteNamed = 3

// logNoteText caps the text of another error a note quotes.
const logNoteText = 160

// logNoteURL caps a failed request's URL in a note.
const logNoteURL = 80

// logMark is where a Tab's log stood when a Page Action began.
type logMark struct {
	log      *page.TabLog
	mark     page.LogMark
	c        *cdp.Client
	session  string // the Tab's session, to read the Page's URL; "" before it was attached
	windowID uint64
}

// logMarks are the marks of one reported command (a Batch's steps add
// theirs to its record).
type logMarks struct {
	mu    sync.Mutex
	marks []logMark
}

type logMarksKey struct{}

// markPageLogs marks the Tab's log before a Page Action's input, when the
// command reports its logs (reportLogs); a Tab marked already keeps its
// first mark. It turns Log on in the session if it is not yet.
func markPageLogs(ctx context.Context, c *cdp.Client, session, targetID string, windowID uint64) {
	rec, ok := ctx.Value(logMarksKey{}).(*logMarks)
	if !ok {
		return
	}
	l := page.WatchLog(ctx, c, session, targetID)
	if l == nil {
		return
	}
	if session != "" && l.Capturing() {
		// The exception listener (page/exceptions.go), before the Action:
		// a page it loads has it.
		l.Capture(ctx, session)
	}
	rec.mu.Lock()
	marked := rec.fill(l, session)
	rec.mu.Unlock()
	if marked {
		return
	}
	if session != "" && l.Capturing() {
		// What the Page threw before the Action joins the ring before the
		// mark, so the notes count only what it throws from here.
		pctx, cancel := context.WithTimeout(ctx, pageProbeBudget)
		l.PullExceptions(pctx, session)
		cancel()
	}
	// Mark waits for a Log.enable sent just now: outside the lock.
	mark := l.Mark(ctx)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !rec.fill(l, session) {
		rec.marks = append(rec.marks, logMark{log: l, mark: mark, c: c, session: session, windowID: windowID})
	}
}

// fill reports whether the record marked l already, giving its mark the
// session if it had none. The caller holds rec.mu.
func (rec *logMarks) fill(l *page.TabLog, session string) bool {
	for i, m := range rec.marks {
		if m.log == l {
			if m.session == "" {
				rec.marks[i].session = session
			}
			return true
		}
	}
	return false
}

// reportLogs wraps an Action command: the error entries its Page Actions'
// Tabs logged since their marks become notes after its output, as the
// Windows it opened do (opened.go). A failure keeps its error alone, but
// for the steps' output a failed Batch carries (ranOn).
func (s Services) reportLogs(cmd Command) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		rec := &logMarks{}
		out, err := cmd(context.WithValue(ctx, logMarksKey{}, rec), args)
		if err != nil && !ranOn(err) {
			return out, err
		}
		lines := rec.notes(ctx)
		if lines == "" {
			return out, err
		}
		if err != nil {
			err.Out = withNotes(err.Out, lines)
			return out, err
		}
		return withNotes(out, lines), nil
	}
}

// notes renders the note lines of every marked Tab.
func (rec *logMarks) notes(ctx context.Context) string {
	rec.mu.Lock()
	marks := append([]logMark(nil), rec.marks...)
	rec.mu.Unlock()
	var b strings.Builder
	for _, m := range marks {
		if m.session != "" && m.log.Capturing() && ctx.Err() == nil {
			// The exceptions the isolated world holds join the ring first.
			pctx, cancel := context.WithTimeout(ctx, pageProbeBudget)
			m.log.PullExceptions(pctx, m.session)
			cancel()
		}
		var errs []page.LogEntry
		for _, e := range m.log.Since(m.mark) {
			if e.Level == "error" {
				errs = append(errs, e)
			}
		}
		if len(errs) == 0 {
			continue
		}
		pageURL := ""
		if m.session != "" && ctx.Err() == nil {
			pctx, cancel := context.WithTimeout(ctx, pageProbeBudget)
			_, pageURL, _ = page.MainFrame(pctx, m.c, m.session)
			cancel()
		}
		for _, line := range logNoteLines(errs, pageURL, m.windowID) {
			b.WriteString(notePrefix + line + "\n")
		}
	}
	return b.String()
}

// logNoteLines are the notes for a Tab's new error entries: the failed
// requests, at most logNoteNamed named, then the exceptions the Page threw
// and the other errors, the first of each quoted. pageURL decides which
// requests are the Page's own site.
func logNoteLines(errs []page.LogEntry, pageURL string, windowID uint64) []string {
	var reqs, thrown, others []page.LogEntry
	for _, e := range errs {
		switch e.Source {
		case "network":
			reqs = append(reqs, e)
		case "exception":
			thrown = append(thrown, e)
		default:
			others = append(others, e)
		}
	}
	var lines []string
	if len(reqs) > 0 {
		var named []string
		for _, e := range reqs[:min(len(reqs), logNoteNamed)] {
			named = append(named, strings.TrimSpace(requestFailure(e.Text)+" "+shortRequest(e.URL, pageURL)))
		}
		line := countOf(len(reqs), "failed request") + ": " + strings.Join(named, ", ")
		if len(reqs) > logNoteNamed {
			line += fmt.Sprintf(", and %d more: winuse logs %d", len(reqs)-logNoteNamed, windowID)
		}
		lines = append(lines, line)
	}
	if len(thrown) > 0 {
		line := "the Page threw"
		if len(thrown) > 1 {
			line += fmt.Sprintf(" %d times", len(thrown))
		}
		line += ": " + thrownText(thrown[0])
		if len(thrown) > 1 {
			line += fmt.Sprintf("; %d more: winuse logs %d", len(thrown)-1, windowID)
		}
		lines = append(lines, line)
	}
	if len(others) > 0 {
		noun := "security error"
		for _, e := range others {
			if e.Source != "security" {
				noun = "error"
			}
		}
		line := countOf(len(others), noun) + ": " + page.CutRunes(page.Collapse(others[0].Text), logNoteText)
		if len(others) > 1 {
			line += fmt.Sprintf("; %d more: winuse logs %d", len(others)-1, windowID)
		}
		lines = append(lines, line)
	}
	return lines
}

// thrownText is an exception as a note quotes it: its text without
// Chrome's "Uncaught " and with where it was thrown ("app.js:120").
func thrownText(e page.LogEntry) string {
	text := page.CutRunes(page.Collapse(strings.TrimPrefix(e.Text, "Uncaught ")), logNoteText)
	if place := page.ScriptPlace(e.URL, e.Line); place != "" {
		text += " (" + place + ")"
	}
	return text
}

// entryNoun is "entry" or "entries", the noun after n.
func entryNoun(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}

// requestFailure is how a failed request ended, from Chrome's text: the
// HTTP status ("404"), or the network error ("net::ERR_CONNECTION_REFUSED");
// "failed" when the text says neither.
func requestFailure(text string) string {
	const statusOf = "status of "
	if i := strings.Index(text, statusOf); i >= 0 {
		rest := text[i+len(statusOf):]
		n := 0
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if n > 0 {
			return rest[:n]
		}
	}
	if i := strings.Index(text, "net::"); i >= 0 {
		return strings.Fields(text[i:])[0]
	}
	return "failed"
}

// shortRequest is a request's URL in a note: page.SiteURL's rule, the one
// `read` prints links by (its path on the Page's own site, else its host
// and path), cut at logNoteURL characters.
func shortRequest(raw, pageURL string) string {
	return page.CutRunes(page.SiteURL(raw, pageURL), logNoteURL)
}

// logsCommand runs `winuse logs`.
func logsCommand(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.LogsArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("logs: "+err.Error(), protocol.LogsUsage)
		}
		if strings.TrimSpace(a.Window) == "" {
			return "", Fail("logs: a window reference is required", protocol.LogsUsage)
		}
		switch {
		case a.MaxChars != nil && *a.MaxChars < 0:
			return "", Fail("logs: --max-chars takes a number of characters (0: no cap)", protocol.LogsUsage)
		case a.Out != "" && a.MaxChars != nil && *a.MaxChars != 0:
			return "", Fail("logs: pass --out or --max-chars, not both: --out writes every entry to the file", protocol.LogsUsage)
		}
		if tabTargetOf(ctx) == nil {
			w, rerr := resolveWindow(s, a.Window)
			if rerr != nil {
				return "", rerr
			}
			if s.Pages == nil || s.Pages.Browser(w) == "" {
				id := strconv.FormatUint(w.ID, 10)
				return "", Fail(fmt.Sprintf("window %d is not a browser Window: logs lists a web Page's failed requests and CSP errors", w.ID),
					"read this Window with winuse get-window-state "+id)
			}
		}
		return s.runPageWindow(ctx, a.Window, pageWindowRun{
			// A dialog holds the Page's script, not its log; a browser page
			// (not attached) has an empty one.
			dialogOK:    true,
			browserPage: func(pageWin) *Error { return nil },
			act: func(ctx context.Context, pw pageWin, stopped func() bool) (string, bool, *Error) {
				l := page.WatchLog(ctx, pw.conn.Client, pw.session, pw.tab.TargetID)
				view := logsView{ref: a.Window}
				if a.Capture || l.Capturing() {
					// Exceptions (page/exceptions.go): --capture turns them on
					// for the Tab's life; a Tab that captures reads them.
					view.first = l.Capture(ctx, pw.session) && a.Capture
					pctx, cancel := context.WithTimeout(ctx, pageProbeBudget)
					covered, ok := l.PullExceptions(pctx, pw.session)
					cancel()
					view.capturing, view.uncovered = true, ok && !covered
				}
				l.WaitReplay(ctx) // the replay of a session turned on just now
				list := l.Entries()
				if a.Clear {
					l.Clear(ctx)
				}
				out, rerr := renderLogs(pw.tab.URL, list, a, view)
				return out, false, rerr
			},
		})
	}
}

// logLine is one entry as logs prints it: the time, source, level and
// text, the URL when the text does not hold it, and the stack indented.
func logLine(e page.LogEntry) string {
	var b strings.Builder
	b.WriteString(e.Time.Local().Format("15:04:05.000") + " " + e.Source + " " + e.Level + " " + page.Collapse(e.Text))
	if e.URL != "" && !strings.Contains(e.Text, e.URL) {
		b.WriteString(" " + e.URL)
		if e.Line > 0 {
			b.WriteString(":" + strconv.Itoa(e.Line))
		}
	}
	b.WriteString("\n")
	for _, f := range strings.Split(e.Stack, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			b.WriteString("    " + f + "\n")
		}
	}
	return b.String()
}

// logJSON is one entry in logs' --json answer.
type logJSON struct {
	Time   time.Time `json:"time"`
	Source string    `json:"source"`
	Level  string    `json:"level"`
	Text   string    `json:"text"`
	URL    string    `json:"url,omitempty"`
	Line   int       `json:"line,omitempty"`
	Stack  string    `json:"stack,omitempty"`
}

// logsView is what logs says about the Tab's exception capture.
type logsView struct {
	ref       string // the Window (or Tab target) as given, for the reload hint
	capturing bool   // the Tab captures its exceptions
	first     bool   // this --capture turned it on
	uncovered bool   // the Page loaded before the listener was installed
}

// renderLogs prints the entries under a header naming the Page and what
// its log holds: the newest within the cap (--max-chars, else
// defaultMaxChars) after a line counting the older ones left out, or every
// one written to --out; JSON under --json. A capture just turned on, or a
// Page loaded before it, says that reload covers the Page.
func renderLogs(pageURL string, list []page.LogEntry, a protocol.LogsArgs, v logsView) (string, *Error) {
	if a.JSON {
		return renderLogsJSON(pageURL, list, a, v)
	}
	var b strings.Builder
	what := "failed requests and CSP or security errors"
	if v.capturing {
		what = "failed requests, CSP or security errors and JavaScript exceptions"
	}
	switch {
	case len(list) == 0 && v.capturing:
		fmt.Fprintf(&b, "Page: %s - no log entries (%s show here)\n", pageURL, what)
	case len(list) == 0:
		fmt.Fprintf(&b, "Page: %s - no log entries (%s show here; --capture adds JavaScript exceptions)\n", pageURL, what)
	case v.capturing:
		fmt.Fprintf(&b, "Page: %s - %d log %s, newest last (%s; winuse keeps the last %d per Tab)\n",
			pageURL, len(list), entryNoun(len(list)), what, page.LogRingSize)
	default:
		fmt.Fprintf(&b, "Page: %s - %d log %s, newest last (%s; --capture adds JavaScript exceptions; winuse keeps the last %d per Tab)\n",
			pageURL, len(list), entryNoun(len(list)), what, page.LogRingSize)
	}
	switch {
	case v.first:
		fmt.Fprintf(&b, "(capturing JavaScript exceptions in this Tab from now on, while it is open: they show for pages it loads from now on; winuse reload %s covers this one)\n", v.ref)
	case v.uncovered:
		fmt.Fprintf(&b, "(this page loaded before winuse began capturing its exceptions: winuse reload %s covers it)\n", v.ref)
	}
	lines := make([]string, len(list))
	for i, e := range list {
		lines[i] = logLine(e)
	}
	if a.Out != "" {
		if err := writeFileAtomic(a.Out, []byte(b.String()+strings.Join(lines, ""))); err != nil {
			return "", Fail("writing the logs to "+a.Out+" failed: "+err.Error(), "pass --out a file in a folder you can write to")
		}
		fmt.Fprintf(&b, "Logs: %s (%d %s)\n", a.Out, len(list), entryNoun(len(list)))
	} else {
		limit := defaultMaxChars
		if a.MaxChars != nil {
			limit = *a.MaxChars
		}
		first, used := 0, 0
		for i := len(lines) - 1; i >= 0; i-- {
			n := utf8.RuneCountInString(lines[i])
			if limit > 0 && used+n > limit {
				first = i + 1
				break
			}
			used += n
		}
		if first > 0 {
			fmt.Fprintf(&b, "(%d older %s left out to stay within %d characters; --max-chars raises the cut, 0 lifts it, --out <file> writes them all)\n",
				first, entryNoun(first), limit)
		}
		b.WriteString(strings.Join(lines[first:], ""))
	}
	if a.Clear {
		b.WriteString("(cleared: the next winuse logs lists only later entries)\n")
	}
	return b.String(), nil
}

// renderLogsJSON is logs' --json answer: {"url","capturing","entries",
// "cleared"}, or with --out the same object in the file and {"url","file",
// "count","cleared"} printed. capturing says the Tab captures its
// JavaScript exceptions.
func renderLogsJSON(pageURL string, list []page.LogEntry, a protocol.LogsArgs, view logsView) (string, *Error) {
	items := make([]logJSON, 0, len(list))
	for _, e := range list {
		items = append(items, logJSON{Time: e.Time, Source: e.Source, Level: e.Level, Text: e.Text, URL: e.URL, Line: e.Line, Stack: e.Stack})
	}
	var v any = struct {
		URL       string    `json:"url"`
		Capturing bool      `json:"capturing"`
		Entries   []logJSON `json:"entries"`
		Cleared   bool      `json:"cleared"`
	}{pageURL, view.capturing, items, a.Clear}
	if a.Out != "" {
		data, err := json.Marshal(v)
		if err != nil {
			return "", Fail("encoding the logs: "+err.Error(), "")
		}
		if err := writeFileAtomic(a.Out, append(data, '\n')); err != nil {
			return "", Fail("writing the logs to "+a.Out+" failed: "+err.Error(), "pass --out a file in a folder you can write to")
		}
		v = struct {
			URL     string `json:"url"`
			File    string `json:"file"`
			Count   int    `json:"count"`
			Cleared bool   `json:"cleared"`
		}{pageURL, a.Out, len(list), a.Clear}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", Fail("encoding the logs: "+err.Error(), "")
	}
	return string(data) + "\n", nil
}
