package wstate

// What --observe=diff prints (speed observe, ADR 0021): the Elements of a
// new read whose line differs from the line the same Element Index had in
// the Agent's earlier Window State of the Window. The comparison is by
// Element Index, never by a guessed identity, so the rule an agent relies
// on holds by construction: an Element Index the diff does not list names
// an Element whose line is exactly what the agent saw before. An Element
// inserted in the middle of the tree moves every index after it, and they
// all show as changed; past DiffThreshold the whole Window State prints
// instead, as cua and dev-browser do.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DiffThreshold is the share of the whole Window State (as the view prints
// it) a diff may reach; a longer one prints the whole Window State.
const DiffThreshold = 0.6

// Change is one Element Index whose line differs between two Window
// States of one Window.
type Change struct {
	Index int
	Kind  ChangeKind
}

// ChangeKind is how an Element Index changed.
type ChangeKind byte

const (
	Changed ChangeKind = '~' // the index names an Element with another line
	New     ChangeKind = '*' // the index is new: the earlier read ended before it
	Gone    ChangeKind = '-' // the index is gone: the new read ends before it
)

// Diff compares two Window States' Elements by Element Index: an index
// both have is Changed when its line (depth included) differs, an index
// only after has is New, and one only before has is Gone.
func Diff(before, after []Element) []Change {
	var out []Change
	n := min(len(before), len(after))
	for i := 0; i < n; i++ {
		if before[i].Depth != after[i].Depth || Line(i, &before[i]) != Line(i, &after[i]) {
			out = append(out, Change{i, Changed})
		}
	}
	for i := n; i < len(after); i++ {
		out = append(out, Change{i, New})
	}
	for i := n; i < len(before); i++ {
		out = append(out, Change{i, Gone})
	}
	return out
}

// Observed is what RenderChanges printed.
type Observed struct {
	Text  string
	Whole bool // the whole Window State printed, not a diff
	// Changes counts the Element Indexes that changed, appeared or went;
	// -1 when there was no earlier Window State to compare with.
	Changes int
}

// wasLimit bounds the earlier line a Changed index quotes.
const wasLimit = 100

// RenderChanges prints what changed in t since before, the Elements of the
// Agent's earlier Window State of the same Window (nil: none), through the
// view v: the header, one line per changed, new or gone Element Index the
// view prints, and a tail with the focused Element when it changed and
// t's notes. what names the Window in the text ("window 1234"). With no
// earlier Window State, or a diff longer than DiffThreshold of the whole,
// the whole Window State prints through v, with a note saying why.
func RenderChanges(before []Element, t *Tree, v View, what string) Observed {
	if before == nil {
		return Observed{Text: renderWhole(t, v, fmt.Sprintf("the whole Window State: this agent had no earlier read of %s to diff against", what)), Whole: true, Changes: -1}
	}
	changes := Diff(before, t.Elements)
	head, lines, tail, hidden := diffParts(before, t, v, changes, what)

	full := v
	full.MaxChars = 0
	whole := RenderView(t, full)
	diff := Cut(head, lines, tail, 0, "")
	if float64(utf8.RuneCountInString(diff)) > DiffThreshold*float64(utf8.RuneCountInString(whole)) {
		return Observed{Text: renderWhole(t, v, fmt.Sprintf("the whole Window State: %d Element Indexes changed, so a diff would not be shorter", len(changes))),
			Whole: true, Changes: len(changes)}
	}
	if hidden > 0 {
		tail += fmt.Sprintf("Note: %s left out %d of the %d changes\n", v.flags(), hidden, len(changes))
	}
	hint := "Read the Window whole with winuse get-window-state"
	if v.Ref != "" {
		hint += " " + v.Ref
	}
	return Observed{Text: Cut(head, lines, tail, v.MaxChars, hint+", or narrow it with --interactive, --compact or --depth"), Changes: len(changes)}
}

// renderWhole is RenderView with one more note.
func renderWhole(t *Tree, v View, note string) string {
	c := *t
	c.Notes = append(append([]string(nil), t.Notes...), note)
	return RenderView(&c, v)
}

// diffParts builds the diff's header, its lines and its tail, and counts
// the changes the view left out.
func diffParts(before []Element, t *Tree, v View, changes []Change, what string) (head string, lines []PrintedLine, tail string, hidden int) {
	var h strings.Builder
	writeHeader(&h, t)
	if len(changes) == 0 {
		fmt.Fprintf(&h, "No change since this agent's last read of %s.\n", what)
	} else {
		fmt.Fprintf(&h, "Changed since this agent's last read of %s (~ changed, * new, - gone); Element Indexes not listed are unchanged:\n", what)
	}
	sharedBefore, sharedAfter := sharedOf(before, v), sharedOf(t.Elements, v)
	for _, c := range changes {
		var e Element
		if c.Kind == Gone {
			e = before[c.Index].WithoutSharedID(sharedBefore)
		} else {
			e = t.Elements[c.Index].WithoutSharedID(sharedAfter)
		}
		if !v.prints(c.Index, &e) {
			hidden++
			continue
		}
		var b strings.Builder
		b.WriteString(strings.Repeat("\t", e.Depth+1))
		b.WriteByte(byte(c.Kind))
		writeElement(&b, c.Index, &e, false)
		if c.Kind == Changed {
			old := before[c.Index].WithoutSharedID(sharedBefore)
			b.WriteString(" (was: " + clip(lineBody(c.Index, &old), wasLimit) + ")")
		}
		b.WriteByte('\n')
		lines = append(lines, PrintedLine{c.Index, b.String()})
	}

	var tl strings.Builder
	if focused := t.Focused(); focused >= 0 && focusMoved(before, t, focused) {
		f := t.Elements[focused].WithoutSharedID(sharedAfter)
		tl.WriteString("The focused UI element is ")
		writeElement(&tl, focused, &f, false)
		tl.WriteString(".\n")
	}
	for _, n := range t.Notes {
		tl.WriteString("Note: " + n + "\n")
	}
	return h.String(), lines, tl.String(), hidden
}

// focusMoved reports whether the focused Element's line is news: the
// focused index changed, or so did its line. The earlier focus is not
// stored, so a focused index whose line is the same is taken as unchanged.
func focusMoved(before []Element, t *Tree, focused int) bool {
	return focused >= len(before) || Line(focused, &before[focused]) != Line(focused, &t.Elements[focused])
}

// sharedOf is the IDs a view leaves out of els: those two or more of them
// share (SharedIDs), and none under Full.
func sharedOf(els []Element, v View) map[string]bool {
	if v.Full {
		return nil
	}
	return SharedIDs(els)
}

// lineBody is an Element's line without its index.
func lineBody(index int, e *Element) string {
	return strings.TrimPrefix(Line(index, e), strconv.Itoa(index)+" ")
}

// clip cuts s to at most n runes, marking the cut.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
