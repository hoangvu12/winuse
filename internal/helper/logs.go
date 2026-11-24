package helper

// Failed requests and CSP errors (.scratch/bladebro-ideas ticket 29, after
// tickets 11 and 19). Every Page session winuse works in has the browser's
// Log domain on (page/logs.go): never Runtime or Network, which a Page can
// detect. Each Tab keeps a ring of its last page.LogRingSize entries.
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
// `winuse logs <window>[:<tab>] [--clear] [--max-chars N | --out <file>]`
// prints the ring, newest last; --clear empties it after printing (and
// Log.clear empties the browser's store that Log.enable replays from);
// the global --json answers with a JSON object. It only reads: no lane, no
// Agent Cursor, nothing sent to the Page's input. Ticket 30 adds the
// Page's JavaScript exceptions to the same ring and its notes.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/notes"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// logNoteNamed is how many failed requests a note names.
const logNoteNamed = 3

// logNoteText caps the text of another error a note quotes.
const logNoteText = 160

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
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i, m := range rec.marks {
		if m.log == l {
			if m.session == "" {
				rec.marks[i].session = session
			}
			return
		}
	}
	rec.marks = append(rec.marks, logMark{log: l, mark: l.Mark(), c: c, session: session, windowID: windowID})
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
// requests, at most logNoteNamed named, then the other errors, the first
// one quoted. pageURL decides which requests are the Page's own site.
func logNoteLines(errs []page.LogEntry, pageURL string, windowID uint64) []string {
	var reqs, others []page.LogEntry
	for _, e := range errs {
		if e.Source == "network" {
			reqs = append(reqs, e)
		} else {
			others = append(others, e)
		}
	}
	var lines []string
	if len(reqs) > 0 {
		var named []string
		for _, e := range reqs[:min(len(reqs), logNoteNamed)] {
			named = append(named, strings.TrimSpace(requestFailure(e.Text)+" "+shortRequest(e.URL, pageURL)))
		}
		line := plural(len(reqs), "failed request") + ": " + strings.Join(named, ", ")
		if len(reqs) > logNoteNamed {
			line += fmt.Sprintf(", and %d more: winuse logs %d", len(reqs)-logNoteNamed, windowID)
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
		line := plural(len(others), noun) + ": " + capText(page.Collapse(others[0].Text), logNoteText)
		if len(others) > 1 {
			line += fmt.Sprintf("; %d more: winuse logs %d", len(others)-1, windowID)
		}
		lines = append(lines, line)
	}
	return lines
}

// entries is "entry" or "entries" after n.
func entries(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}

// capText cuts s at n characters, marking the cut.
func capText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n])) + "..."
	}
	return s
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

// shortRequest is a request's URL in a note: its path (and query) on the
// Page's own site, else its host and path, cut at 80 characters.
func shortRequest(raw, pageURL string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return capText(raw, 80)
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	site, ok := notes.SiteKey(raw)
	if own, pok := notes.SiteKey(pageURL); ok && pok && site == own {
		return capText(path, 80)
	}
	return capText(u.Host+path, 80)
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
				l.Settle(ctx) // the replay of a session turned on just now
				list := l.Entries()
				if a.Clear {
					l.Clear(ctx)
				}
				out, rerr := renderLogs(pw.tab.URL, list, a)
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

// renderLogs prints the entries under a header naming the Page: the newest
// within the cap (--max-chars, else defaultMaxChars) after a line counting
// the older ones left out, or every one written to --out; JSON under
// --json.
func renderLogs(pageURL string, list []page.LogEntry, a protocol.LogsArgs) (string, *Error) {
	if a.JSON {
		return renderLogsJSON(pageURL, list, a)
	}
	var b strings.Builder
	if len(list) == 0 {
		fmt.Fprintf(&b, "Page: %s - no log entries (failed requests and CSP or security errors show here)\n", pageURL)
	} else {
		fmt.Fprintf(&b, "Page: %s - %d log %s, newest last (failed requests and CSP or security errors; winuse keeps the last %d per Tab)\n",
			pageURL, len(list), entries(len(list)), page.LogRingSize)
	}
	lines := make([]string, len(list))
	for i, e := range list {
		lines[i] = logLine(e)
	}
	if a.Out != "" {
		if err := writeFileAtomic(a.Out, []byte(b.String()+strings.Join(lines, ""))); err != nil {
			return "", Fail("writing the logs to "+a.Out+" failed: "+err.Error(), "pass --out a file in a folder you can write to")
		}
		fmt.Fprintf(&b, "Logs: %s (%d %s)\n", a.Out, len(list), entries(len(list)))
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
				first, entries(first), limit)
		}
		b.WriteString(strings.Join(lines[first:], ""))
	}
	if a.Clear {
		b.WriteString("(cleared: the next winuse logs lists only later entries)\n")
	}
	return b.String(), nil
}

// renderLogsJSON is logs' --json answer: {"url","entries","cleared"}, or
// with --out the same object in the file and {"url","file","count",
// "cleared"} printed.
func renderLogsJSON(pageURL string, list []page.LogEntry, a protocol.LogsArgs) (string, *Error) {
	items := make([]logJSON, 0, len(list))
	for _, e := range list {
		items = append(items, logJSON{Time: e.Time, Source: e.Source, Level: e.Level, Text: e.Text, URL: e.URL, Line: e.Line, Stack: e.Stack})
	}
	var v any = struct {
		URL     string    `json:"url"`
		Entries []logJSON `json:"entries"`
		Cleared bool      `json:"cleared"`
	}{pageURL, items, a.Clear}
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
