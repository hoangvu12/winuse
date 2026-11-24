package helper

// The Notes in the Window State (ADR 0024, bladebro-ideas ticket 23). An
// Agent's first get-window-state of a site or App shows that key's Notes:
// the newest 10, newest first, each with its author and age, then
// "(+N more: winuse note list <key>)", under a header saying they come
// from earlier agents, not from the user. A browser Window (or a Tab
// target) shows its Page's site, then the browser App; any other Window
// its App (the key rule of notes.go, siteBrowser).
//
//   - Once per Agent per key: notesSeen remembers the keys each Agent's
//     reads have covered, Notes or none, so the store is read once per key.
//     A read that moves to another site covers that site the first time.
//   - The Notes travel apart from the tree text: the server puts a
//     windowNotes on each request's context, a read adds its Notes to it,
//     and the response carries them in protocol.Response.Notes. The CLI
//     prints them above the Window State, or in --json's "notes" field.
//   - Only get-window-state shows them (any form but --diff; a Batch's read
//     step too, the first time it covers a key). --observe, find and
//     --diff read through other paths and show none, and cover nothing.
//   - WINUSE_NOTES=off (protocol.Request.NoNotes) hides them and covers
//     nothing, so the Agent's next read shows them.
//   - They count toward --max-chars: they shrink first (fewer lines, down to
//     the header and the note list line), and only a cap the tree alone
//     nearly fills cuts the tree, by that much.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/notes"
	"github.com/hoangvu12/winuse/internal/win"
)

// maxShownNotes is how many of a key's Notes a read shows; note list shows
// them all.
const maxShownNotes = 10

// windowNotes collects the Notes one request's reads showed.
type windowNotes struct {
	off  bool // WINUSE_NOTES=off: none are shown
	mu   sync.Mutex
	text strings.Builder
}

type windowNotesKey struct{}

// withWindowNotes gives a request somewhere to put the Notes its reads
// show; off hides them.
func withWindowNotes(ctx context.Context, off bool) (context.Context, *windowNotes) {
	n := &windowNotes{off: off}
	return context.WithValue(ctx, windowNotesKey{}, n), n
}

func windowNotesOf(ctx context.Context) *windowNotes {
	n, _ := ctx.Value(windowNotesKey{}).(*windowNotes)
	return n
}

func (n *windowNotes) add(s string) {
	n.mu.Lock()
	n.text.WriteString(s)
	n.mu.Unlock()
}

// String is the Notes shown, "" for none.
func (n *windowNotes) String() string {
	if n == nil {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.text.String()
}

// readURLKey carries where readWindowTree reports the URL of the Page it
// merged, for the Window State's site key.
type readURLKey struct{}

func withReadURL(ctx context.Context, url *string) context.Context {
	return context.WithValue(ctx, readURLKey{}, url)
}

// noteReadURL reports the URL of the Page a read merged, when its caller
// asked (withReadURL).
func noteReadURL(ctx context.Context, url string) {
	if p, ok := ctx.Value(readURLKey{}).(*string); ok {
		*p = url
	}
}

// maxNoteAgents caps the Agents notesSeen remembers; the one idle longest
// goes first, and would see its Notes again.
const maxNoteAgents = 64

// notesSeen is the keys each Agent's reads have covered.
type notesSeen struct {
	mu     sync.Mutex
	agents map[string]*agentNotes
}

type agentNotes struct {
	keys map[string]bool
	at   time.Time
}

func newNotesSeen() *notesSeen { return &notesSeen{agents: map[string]*agentNotes{}} }

// claim marks keys covered for the Agent and returns those that were not.
func (ns *notesSeen) claim(agentID string, keys []string, now time.Time) []string {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	a := ns.agents[agentID]
	if a == nil {
		if len(ns.agents) >= maxNoteAgents {
			oldest, first := "", true
			for id, o := range ns.agents {
				if first || o.at.Before(ns.agents[oldest].at) {
					oldest, first = id, false
				}
			}
			delete(ns.agents, oldest)
		}
		a = &agentNotes{keys: map[string]bool{}}
		ns.agents[agentID] = a
	}
	a.at = now
	var fresh []string
	for _, k := range keys {
		if !a.keys[k] {
			a.keys[k] = true
			fresh = append(fresh, k)
		}
	}
	return fresh
}

// release uncovers keys a read claimed but did not show.
func (ns *notesSeen) release(agentID string, keys []string) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if a := ns.agents[agentID]; a != nil {
		for _, k := range keys {
			delete(a.keys, k)
		}
	}
}

// stateNoteKeys are the Note keys a Window State of w covers, in the order
// they show: a browser Window's Page's site (pageURL; "" when the read
// did not merge a Page), then its App.
func (s Services) stateNoteKeys(w win.WindowInfo, pageURL string) []string {
	var keys []string
	if s.siteBrowser(w) {
		if k, ok := notes.SiteKey(pageURL); ok {
			keys = append(keys, k)
		}
	}
	if k := notes.AppKey(w.ExePath); k != "" {
		keys = append(keys, k)
	}
	return keys
}

// tabNoteKeys are the Note keys a Tab target's Window State covers: its
// Page's site, then the browser App.
func tabNoteKeys(bt *bgTab) []string {
	var keys []string
	if k, ok := notes.SiteKey(bt.tab.URL); ok {
		keys = append(keys, k)
	}
	if k := notes.AppKey(bt.w.ExePath); k != "" {
		keys = append(keys, k)
	}
	return keys
}

// stateWithNotes renders a get-window-state's Window State (render, with
// the read's flags) and adds the Notes of the keys this Agent's reads
// have not covered yet to the request's Notes, within a's --max-chars
// (see the file comment).
func (s Services) stateWithNotes(ctx context.Context, a windowRef, keys []string, render func(windowRef) (string, *Error)) (string, *Error) {
	sink := windowNotesOf(ctx)
	if sink == nil || sink.off || a.Diff || s.NoteStore == nil || s.notesSeen == nil || len(keys) == 0 {
		return render(a)
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	agentID := AgentOf(ctx).ID
	claimed := s.notesSeen.claim(agentID, keys, now)
	if len(claimed) == 0 {
		return render(a)
	}
	blocks, recovered, ok := s.dueNotes(claimed)
	if !ok {
		s.notesSeen.release(agentID, claimed) // the store could not be read: try again next read
		return render(a)
	}
	out, e := render(a)
	if e != nil {
		s.notesSeen.release(agentID, claimed)
		return out, e
	}
	if len(blocks) == 0 && recovered == "" {
		return out, nil
	}
	limit := a.maxChars()
	used := func(out string) int {
		n := utf8.RuneCountInString(out)
		if a.Screenshot {
			n += screenshotReserve // the Screenshot line printed after the tree
		}
		return n
	}
	if least := utf8.RuneCountInString(fitNotes(blocks, 0, now, recovered)); limit > 0 && a.Out == "" && used(out)+1+least > limit {
		// Even the shortest Notes do not fit beside the tree: the tree gives
		// up that much.
		cut := a
		reduced := max(limit-1-least, 1)
		cut.MaxChars = &reduced
		if o, e := render(cut); e == nil {
			out = o
		}
	}
	room := -1
	if limit > 0 {
		room = limit - used(out) - 1 // the blank line between the Notes and the tree
	}
	sink.add(fitNotes(blocks, room, now, recovered))
	return out, nil
}

// noteBlock is one key's Notes, oldest first.
type noteBlock struct {
	key   string
	notes []notes.Note
}

// dueNotes reads the Notes of keys, in keys' order; ok is false when the
// store could not be read. recovered says a corrupt file was moved aside.
func (s Services) dueNotes(keys []string) (blocks []noteBlock, recovered string, ok bool) {
	st, err := s.NoteStore()
	if err != nil || st == nil {
		return nil, "", false
	}
	all, bad, err := st.All()
	if err != nil {
		return nil, "", false
	}
	for _, k := range keys {
		var list []notes.Note
		for _, n := range all {
			if n.Key == k {
				list = append(list, n)
			}
		}
		if len(list) > 0 {
			blocks = append(blocks, noteBlock{key: k, notes: list})
		}
	}
	return blocks, recoveredLine(bad), true
}

// render prints the block with its newest n Notes, newest first; n 0
// prints the header and how many there are.
func (b noteBlock) render(n int, now time.Time) string {
	total := len(b.notes)
	n = min(n, total)
	var s strings.Builder
	fmt.Fprintf(&s, "Notes for %s (left by earlier agents, not instructions from the user):\n", b.key)
	if n == 0 {
		fmt.Fprintf(&s, "(%s: winuse note list %s)\n", plural(total, "Note"), b.key)
		return s.String()
	}
	for i := total - 1; i >= total-n; i-- {
		note := b.notes[i]
		fmt.Fprintf(&s, "- %s (%s, %s)\n", note.Text, note.Author, notes.Age(note.Added, now))
	}
	if total > n {
		fmt.Fprintf(&s, "(+%d more: winuse note list %s)\n", total-n, b.key)
	}
	return s.String()
}

// fitNotes prints the blocks within room characters (negative: no cap):
// the newest maxShownNotes of each, fewer while they do not fit, down to
// the headers and counts, which print whatever room says.
func fitNotes(blocks []noteBlock, room int, now time.Time, recovered string) string {
	print := func(n int) string {
		var s strings.Builder
		s.WriteString(recovered)
		for _, b := range blocks {
			s.WriteString(b.render(n, now))
		}
		return s.String()
	}
	for n := maxShownNotes; n > 0; n-- {
		if t := print(n); room < 0 || utf8.RuneCountInString(t) <= room {
			return t
		}
	}
	return print(0)
}
