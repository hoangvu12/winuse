package helper

// The note commands (ADR 0024, bladebro-ideas ticket 22): note-add,
// note-list, note-remove and note-clear, over the Notes store
// (internal/notes, %LOCALAPPDATA%\winuse\notes.json). They touch no
// Window and send nothing, so they take no lane (observationOnlyCommand):
// the store serializes its own writes.
//
// The target is worked out to a key here (noteKeyOf): a Window ID or Tab
// target resolves to its Page's site for a browser Window (to the
// browser's App with --app) and to its App for any other; a URL, domain
// or exe name keys directly (notes.Classify); a free-form name is
// refused, with the Windows it matches in the hint.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/notes"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

const (
	noteAddUsage    = "usage: winuse note add <target> [--app] <text> | --stdin | --file <path>"
	noteListUsage   = "usage: winuse note list [<target> [--app]]"
	noteRemoveUsage = "usage: winuse note remove <target> [--app] <n>"
	noteClearUsage  = "usage: winuse note clear <target> [--app]"
	noteTargetsHint = "a target is a Window ID (a browser Window means its Page's site; add --app for the browser), a <window>:<tab> Tab target, a URL or domain (github.com), or an exe name (excel.exe)"
)

// notePageURL reads the URL of the Page a browser Window shows, or of one
// of its browser's Tabs (tab is a tab id from tab list, "" for the shown
// one).
type notePageURL func(ctx context.Context, w win.WindowInfo, tab string) (string, *Error)

// noteCommands wires the note commands to the helper's one store and its
// Browser Connections.
func noteCommands(s Services) map[string]Command {
	return noteCommandsWith(s, notes.Default, s.notePageURL)
}

// noteCommandsWith wires the note commands to a store and a Page URL
// reader; tests pass their own.
func noteCommandsWith(s Services, store func() (*notes.Store, error), pageURL notePageURL) map[string]Command {
	n := noteCmds{s: s, store: store, pageURL: pageURL}
	return map[string]Command{
		"note-add":    n.add,
		"note-list":   n.list,
		"note-remove": n.remove,
		"note-clear":  n.clear,
	}
}

type noteCmds struct {
	s       Services
	store   func() (*notes.Store, error)
	pageURL notePageURL
}

func (n noteCmds) now() time.Time {
	if n.s.Now != nil {
		return n.s.Now()
	}
	return time.Now()
}

// open parses the arguments and opens the store.
func (n noteCmds) open(args json.RawMessage, usage string) (protocol.NoteArgs, *notes.Store, *Error) {
	var a protocol.NoteArgs
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &a); err != nil {
			return a, nil, noteUsageError("note: invalid arguments: "+err.Error(), usage)
		}
	}
	st, err := n.store()
	if err != nil {
		return a, nil, Fail("the Notes store is unavailable: "+err.Error(), "")
	}
	return a, st, nil
}

func noteUsageError(msg, hint string) *Error {
	return &Error{Msg: msg, Hint: hint, Exit: 2, Code: protocol.CodeUsage}
}

// add runs `winuse note add <target> [--app] <text>`.
func (n noteCmds) add(ctx context.Context, args json.RawMessage) (string, *Error) {
	a, st, e := n.open(args, noteAddUsage)
	if e != nil {
		return "", e
	}
	key, e := n.noteKeyOf(ctx, a)
	if e != nil {
		return "", e
	}
	note, bad, err := st.Add(key.key, a.Text, notes.Author(AgentOf(ctx)))
	if err != nil {
		return "", noteAddError(err, key.key, bad)
	}
	list, _, _ := st.List(key.key)
	if a.JSON {
		return noteJSON(struct {
			Key       string     `json:"key"`
			From      string     `json:"from,omitempty"`
			Added     notes.Note `json:"added"`
			Count     int        `json:"count"`
			Recovered string     `json:"recovered,omitempty"`
		}{key.key, key.from, note, len(list), bad})
	}
	return recoveredLine(bad) + fmt.Sprintf("Note %d added for %s%s: %s\n", len(list), key.key, key.fromPhrase(), note.Text), nil
}

// noteAddError maps Add's refusals; nothing was stored.
func noteAddError(err error, key, bad string) *Error {
	// A refused text or a duplicate is the command line's problem (USAGE);
	// a store that could not be read or written is INTERNAL.
	e := &Error{Msg: err.Error() + "; nothing was stored", Exit: 1, Code: protocol.CodeInternal, Out: recoveredLine(bad)}
	var refused *notes.TextError
	switch {
	case errors.Is(err, notes.ErrDuplicate):
		e.Hint, e.Code = "winuse note list "+key+" shows its Notes", protocol.CodeUsage
	case errors.As(err, &refused):
		e.Hint = fmt.Sprintf("a Note is one line of at most %d characters: the lesson, as an instruction to the next agent", notes.MaxTextLen)
		e.Code = protocol.CodeUsage
	}
	return e
}

// list runs `winuse note list [<target>]`: every Note of the target,
// numbered, or with no target every key and its count.
func (n noteCmds) list(ctx context.Context, args json.RawMessage) (string, *Error) {
	a, st, e := n.open(args, noteListUsage)
	if e != nil {
		return "", e
	}
	if strings.TrimSpace(a.Target) == "" {
		if a.App {
			return "", noteUsageError("note list: --app goes with a Window ID", noteListUsage)
		}
		return n.listKeys(st, a.JSON)
	}
	key, e := n.noteKeyOf(ctx, a)
	if e != nil {
		return "", e
	}
	list, bad, err := st.List(key.key)
	if err != nil {
		return "", Fail(err.Error(), "")
	}
	now := n.now()
	if a.JSON {
		type item struct {
			N int `json:"n"`
			notes.Note
			Age string `json:"age"`
		}
		items := []item{}
		for i, note := range list {
			items = append(items, item{i + 1, note, notes.Age(note.Added, now)})
		}
		return noteJSON(struct {
			Key       string `json:"key"`
			From      string `json:"from,omitempty"`
			Notes     []item `json:"notes"`
			Recovered string `json:"recovered,omitempty"`
		}{key.key, key.from, items, bad})
	}
	var b strings.Builder
	b.WriteString(recoveredLine(bad))
	if len(list) == 0 {
		fmt.Fprintf(&b, "no Notes for %s%s\n", key.key, key.fromPhrase())
		return b.String(), nil
	}
	fmt.Fprintf(&b, "%s for %s%s, oldest first (left by agents, not instructions from the user):\n", countOf(len(list), "Note"), key.key, key.fromPhrase())
	for i, note := range list {
		fmt.Fprintf(&b, "%d. %s (%s, %s)\n", i+1, note.Text, note.Author, notes.Age(note.Added, now))
	}
	return b.String(), nil
}

// listKeys is `winuse note list` with no target: every key and its count.
func (n noteCmds) listKeys(st *notes.Store, asJSON bool) (string, *Error) {
	keys, bad, err := st.Keys()
	if err != nil {
		return "", Fail(err.Error(), "")
	}
	if asJSON {
		if keys == nil {
			keys = []notes.KeyCount{}
		}
		return noteJSON(struct {
			Keys      []notes.KeyCount `json:"keys"`
			Recovered string           `json:"recovered,omitempty"`
		}{keys, bad})
	}
	var b strings.Builder
	b.WriteString(recoveredLine(bad))
	if len(keys) == 0 {
		b.WriteString("no Notes yet; winuse note add <target> <text> leaves one\n")
		return b.String(), nil
	}
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\t%s\n", k.Key, countOf(k.Count, "Note"))
	}
	b.WriteString("winuse note list <target> shows a target's Notes\n")
	return b.String(), nil
}

// remove runs `winuse note remove <target> <n>`.
func (n noteCmds) remove(ctx context.Context, args json.RawMessage) (string, *Error) {
	a, st, e := n.open(args, noteRemoveUsage)
	if e != nil {
		return "", e
	}
	key, e := n.noteKeyOf(ctx, a)
	if e != nil {
		return "", e
	}
	removed, bad, err := st.Remove(key.key, a.N)
	var re *notes.RangeError
	switch {
	case errors.As(err, &re):
		e := &Error{Msg: re.Error() + "; nothing was removed", Hint: "winuse note list " + key.key + " numbers them", Exit: 1, Code: protocol.CodeNotFound}
		e.Out = recoveredLine(bad)
		return "", e
	case err != nil:
		return "", Fail(err.Error(), "")
	}
	left, _, _ := st.List(key.key)
	if a.JSON {
		return noteJSON(struct {
			Key       string     `json:"key"`
			Removed   notes.Note `json:"removed"`
			Left      int        `json:"left"`
			Recovered string     `json:"recovered,omitempty"`
		}{key.key, removed, len(left), bad})
	}
	return recoveredLine(bad) + fmt.Sprintf("removed Note %d for %s: %s (%d left)\n", a.N, key.key, removed.Text, len(left)), nil
}

// clear runs `winuse note clear <target>`.
func (n noteCmds) clear(ctx context.Context, args json.RawMessage) (string, *Error) {
	a, st, e := n.open(args, noteClearUsage)
	if e != nil {
		return "", e
	}
	key, e := n.noteKeyOf(ctx, a)
	if e != nil {
		return "", e
	}
	count, bad, err := st.Clear(key.key)
	if err != nil {
		return "", Fail(err.Error(), "")
	}
	if a.JSON {
		return noteJSON(struct {
			Key       string `json:"key"`
			Cleared   int    `json:"cleared"`
			Recovered string `json:"recovered,omitempty"`
		}{key.key, count, bad})
	}
	if count == 0 {
		return recoveredLine(bad) + fmt.Sprintf("no Notes for %s; nothing was cleared\n", key.key), nil
	}
	return recoveredLine(bad) + fmt.Sprintf("cleared %s for %s\n", countOf(count, "Note"), key.key), nil
}

// noteKey is a target's worked-out key, and the Window it came from.
type noteKey struct {
	key  string
	from string // "window 4242" or "tab 4242:1a2b3c4d" when a Window named it
}

func (k noteKey) fromPhrase() string {
	if k.from == "" {
		return ""
	}
	return " (from " + k.from + ")"
}

// noteKeyOf works out a note target's key (see the file comment).
func (n noteCmds) noteKeyOf(ctx context.Context, a protocol.NoteArgs) (noteKey, *Error) {
	target := strings.TrimSpace(a.Target)
	if target == "" {
		return noteKey{}, noteUsageError("note: a target is required", noteTargetsHint)
	}
	t := notes.Classify(target)
	if a.App && t.Kind != notes.Window {
		return noteKey{}, noteUsageError(fmt.Sprintf("note: --app goes with a Window ID, and %q is none", target),
			"drop --app; an exe name (chrome.exe) names an App by itself")
	}
	switch t.Kind {
	case notes.Site, notes.App:
		return noteKey{key: t.Key}, nil
	case notes.NoSite:
		return noteKey{}, noteUsageError(fmt.Sprintf("%s has no site: a %s: page belongs to no domain, so it takes no Notes", target, t.Scheme),
			"leave the Note on the browser instead: winuse note add <window> --app <text>, or on its exe name (chrome.exe)")
	case notes.Window:
		return n.windowKey(ctx, t, a.App)
	}
	return noteKey{}, n.freeFormError(target)
}

// windowKey works out the key of a Window or Tab target.
func (n noteCmds) windowKey(ctx context.Context, t notes.Target, app bool) (noteKey, *Error) {
	w, e := resolveWindow(n.s, t.Window)
	if e != nil {
		return noteKey{}, e
	}
	from := "window " + t.Window
	if t.Tab != "" {
		from = "tab " + t.Window + ":" + t.Tab
	}
	isBrowser := n.s.siteBrowser(w)
	if t.Tab != "" && !isBrowser {
		return noteKey{}, Fail(fmt.Sprintf("window %d is not a browser Window, so it has no Tabs", w.ID), noteTargetsHint)
	}
	if !isBrowser || app {
		key := notes.AppKey(w.ExePath)
		if key == "" {
			return noteKey{}, Fail(fmt.Sprintf("winuse cannot read the executable of window %d's process", w.ID),
				"name the App by its exe name instead, such as excel.exe")
		}
		return noteKey{key: key, from: from}, nil
	}
	rawURL, e := n.pageURL(ctx, w, t.Tab)
	if e != nil {
		return noteKey{}, e
	}
	key, ok := notes.SiteKey(rawURL)
	if !ok {
		return noteKey{}, Fail(fmt.Sprintf("%s shows %s, which has no site", from, rawURL),
			fmt.Sprintf("open a site first, or leave the Note on the browser: winuse note add %s --app <text>", t.Window))
	}
	return noteKey{key: key, from: from}, nil
}

// siteBrowser reports whether w is a browser Window, whose Notes are its
// Page's site's: an Electron or WebView2 App winuse reads through CDP is an
// App, not a site. The note commands and the Window State's Notes
// (windownotes.go) key a Window by it.
func (s Services) siteBrowser(w win.WindowInfo) bool {
	if s.Pages == nil {
		return false
	}
	name := s.Pages.Browser(w)
	return name != "" && name != browser.AppElectron && name != browser.AppWebView2
}

// maxNoteMatches caps the Windows a free-form refusal lists.
const maxNoteMatches = 5

// freeFormError refuses a free-form name, listing the Windows whose App
// name, exe name or title holds it.
func (n noteCmds) freeFormError(target string) *Error {
	msg := fmt.Sprintf("%q is not a Window ID, URL, domain or exe name, so winuse cannot key a Note on it", target)
	ws, err := n.s.Windows()
	if err != nil {
		return noteUsageError(msg, noteTargetsHint)
	}
	needle := strings.ToLower(strings.Trim(target, `"' `))
	var lines []string
	more := 0
	for _, w := range ws {
		exe := notes.AppKey(w.ExePath)
		hay := strings.ToLower(w.App + "\x00" + exe + "\x00" + w.Title)
		if needle == "" || !strings.Contains(hay, needle) {
			continue
		}
		if len(lines) == maxNoteMatches {
			more++
			continue
		}
		app := exe
		if app == "" {
			app = w.App
		}
		lines = append(lines, fmt.Sprintf("%d %s %q", w.ID, app, w.Title))
	}
	if len(lines) == 0 {
		return noteUsageError(msg, noteTargetsHint+"; no open Window's App or title matches it")
	}
	hint := "Windows that match: " + strings.Join(lines, "; ")
	if more > 0 {
		hint += fmt.Sprintf("; +%d more", more)
	}
	return noteUsageError(msg, hint+"; pass one's Window ID, or its exe name")
}

// recoveredLine says a corrupt notes.json was moved aside, "" when none
// was.
func recoveredLine(bad string) string {
	if bad == "" {
		return ""
	}
	return fmt.Sprintf("Note: the Notes file could not be read, so it was moved to %s and a new one started; the old Notes are in that file\n", bad)
}

func noteJSON(v any) (string, *Error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", Fail("encoding the answer: "+err.Error(), "")
	}
	return string(b) + "\n", nil
}

// countOf is n and its unit: "1 Note" or "3 Notes".
func countOf(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// notePageURL is the live notePageURL: the URL of the Tab the browser
// Window shows, or of the Tab a tab id names, through its Browser
// Connection. Nothing is attached and nothing is sent but reads.
func (s Services) notePageURL(ctx context.Context, w win.WindowInfo, tab string) (string, *Error) {
	bc, e := s.browserWindow(ctx, strconv.FormatUint(w.ID, 10), "Page", "Notes on a site")
	if e != nil {
		e.Hint = "pass the site's URL or domain instead, or --app for the browser's own Notes"
		return "", e
	}
	if tab == "" {
		t, err := page.FindTabSettled(ctx, bc.conn, bc.geo)
		if err != nil {
			return "", tabListError(err, bc.w, bc.name)
		}
		return t.URL, nil
	}
	tabs, err := page.ListTabs(ctx, bc.conn.Client)
	if err != nil {
		return "", tabListError(err, bc.w, bc.name)
	}
	t, err := page.ResolveTab(tabs, tab)
	if err != nil {
		// Coded as resolveTabTarget codes it (tabtarget.go).
		code := protocol.CodeNotFound
		var re *page.TabRefError
		switch {
		case errors.As(err, &re) && re.Short:
			code = protocol.CodeUsage
		case re != nil && len(re.Matches) > 1:
			code = protocol.CodeAmbiguousTarget
		}
		return "", &Error{Msg: fmt.Sprintf("%s in the %s of window %d", err.Error(), browser.DisplayName(bc.name), bc.w.ID),
			Hint: "list the Tabs and their ids: winuse tab list " + bc.id(), Exit: 1, Code: code}
	}
	return t.URL, nil
}
