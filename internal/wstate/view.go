package wstate

// Output budgets for a full read (speed 05): views that print less
// of a Window State without changing it. The Window State the helper
// stores is the whole read either way, so every Element Index a view
// prints is the full read's, and one it leaves out still works.
//
// Every view but Full leaves out the IDs two or more Elements of the read
// share (speed 13: the default), which an id: reference refuses as
// ambiguous anyway; a unique ID always prints.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// View shapes how RenderView prints a Window State. The zero View prints
// every Element and leaves out only the shared IDs; Full prints exactly
// what Render does.
type View struct {
	// Full prints the Window State as Render does, byte for byte: Codex's
	// text, shared IDs included, never cut. The other fields are ignored.
	Full bool

	// Interactive prints only the Elements an agent acts on (ActedOn),
	// disabled ones included, plus the root and the Elements that carry a
	// walk's cut marks.
	Interactive bool

	// Depth prints only the Elements at most Depth levels below the
	// Window (0: every level); one with children below the cut says how
	// many, as "(+N children)".
	Depth int

	// Compact folds every Element whose line would say nothing but its
	// index and role (an unnamed, valueless, stateless, non-interactive
	// container, its shared ID left out) into its parent.
	Compact bool

	// MaxChars stops printing Element lines before the output passes this
	// many characters, with a note naming the Elements not printed (0: no
	// cap). The header and the tail (the focused Element and the notes)
	// are always printed.
	MaxChars int

	// Ref is the Window as the notes' --root commands name it ("1234");
	// empty, the notes leave --root out (a Tab target has no --root).
	Ref string
}

// flags names the view's shaping flags as the CLI spells them.
func (v View) flags() string {
	var f []string
	if v.Interactive {
		f = append(f, "--interactive")
	}
	if v.Depth > 0 {
		f = append(f, "--depth "+strconv.Itoa(v.Depth))
	}
	if v.Compact {
		f = append(f, "--compact")
	}
	return strings.Join(f, " ")
}

// RenderView prints a Window State through a view: Render's format, with
// each printed Element indented one level below its nearest printed
// ancestor, and a note saying what the view left out and how to see it.
func RenderView(t *Tree, v View) string {
	if v.Full {
		return Render(t)
	}
	shared := SharedIDs(t.Elements)
	lines, hidden := viewLines(t, v, shared)

	var head strings.Builder
	writeHeader(&head, t)
	var tail strings.Builder
	if focused := t.Focused(); focused >= 0 {
		e := t.Elements[focused].WithoutSharedID(shared)
		tail.WriteString("The focused UI element is ")
		writeElement(&tail, focused, &e, false)
		tail.WriteString(".\n")
	}
	for _, n := range t.Notes {
		tail.WriteString("Note: " + n + "\n")
	}
	if hidden > 0 {
		fmt.Fprintf(&tail, "Note: %s left out %d of the %d Elements; their Element Indexes still work", v.flags(), hidden, len(t.Elements))
		if v.Depth > 0 && v.Ref != "" {
			tail.WriteString("; read a part whole with winuse get-window-state " + v.Ref + " --root <element>")
		}
		tail.WriteString("\n")
	}

	hint := "Narrow the read with --interactive, --compact or --depth, or write it all with --out <file>"
	if v.Ref != "" {
		hint = "See them with winuse get-window-state " + v.Ref + " --root <element>, narrow the read with --interactive, --compact or --depth, or write it all with --out <file>"
	}
	return Cut(head.String(), lines, tail.String(), v.MaxChars, hint)
}

// PrintedLine is one Element line of a printed Window State or find
// answer: its Element Index and its text, indent and newline included.
type PrintedLine struct {
	Index int
	Text  string
}

// Cut joins a header, Element lines and a tail (after a blank line, when
// there is one). With maxChars above 0 it stops the lines before the
// output passes that many characters and ends the tail with a note naming
// the lines not printed, then hint, the way to see them. The header and
// the tail are never cut, so a tiny cap can still be passed.
func Cut(head string, lines []PrintedLine, tail string, maxChars int, hint string) string {
	const format = "Note: --max-chars %d stopped the output after %s: %d more Element lines (from Element %d on) are not printed; their Element Indexes still work. %s\n"
	printed := len(lines)
	if maxChars > 0 {
		reserve := utf8.RuneCountInString(fmt.Sprintf(format, maxChars, "Element 9999999", 9999999, 9999999, hint))
		budget := maxChars - utf8.RuneCountInString(head) - utf8.RuneCountInString(tail) - 1 - reserve
		used := 0
		for i, l := range lines {
			if used += utf8.RuneCountInString(l.Text); used > budget {
				printed = i
				break
			}
		}
	}
	if printed < len(lines) {
		last := "the header"
		if printed > 0 {
			last = "Element " + strconv.Itoa(lines[printed-1].Index)
		}
		tail += fmt.Sprintf(format, maxChars, last, len(lines)-printed, lines[printed].Index, hint)
	}
	var b strings.Builder
	b.WriteString(head)
	for _, l := range lines[:printed] {
		b.WriteString(l.Text)
	}
	if tail != "" {
		b.WriteByte('\n')
		b.WriteString(tail)
	}
	return b.String()
}

// SharedIDs are the IDs (collapsed, as printed) two or more of the
// Elements share: an id: reference naming one is refused as ambiguous.
func SharedIDs(els []Element) map[string]bool {
	seen := make(map[string]bool, len(els))
	shared := map[string]bool{}
	for i := range els {
		if id := Collapse(els[i].AutomationId); id != "" {
			shared[id] = shared[id] || seen[id]
			seen[id] = true
		}
	}
	return shared
}

// WithoutSharedID is e with its ID left out when the ID is one of shared.
func (e Element) WithoutSharedID(shared map[string]bool) Element {
	if shared[Collapse(e.AutomationId)] {
		e.AutomationId = ""
	}
	return e
}

// viewLines picks and indents the Element lines v prints, its shared IDs
// left out, and counts the Elements it leaves out (a --max-chars cut
// aside).
func viewLines(t *Tree, v View, shared map[string]bool) ([]PrintedLine, int) {
	type shown struct{ depth, level int }
	var stack []shown // the printed ancestors of the current Element
	var out []PrintedLine
	hidden := 0
	for i := range t.Elements {
		e := t.Elements[i].WithoutSharedID(shared)
		for len(stack) > 0 && stack[len(stack)-1].depth >= e.Depth {
			stack = stack[:len(stack)-1]
		}
		if v.Depth > 0 && e.Depth == v.Depth {
			e.MoreChildren = max(e.MoreChildren, directChildren(t.Elements, i))
		}
		if !v.prints(i, &e) {
			hidden++
			continue
		}
		level := 0
		if len(stack) > 0 {
			level = stack[len(stack)-1].level + 1
		}
		stack = append(stack, shown{e.Depth, level})
		var b strings.Builder
		b.WriteString(strings.Repeat("\t", level+1))
		writeElement(&b, i, &e, false)
		b.WriteByte('\n')
		out = append(out, PrintedLine{i, b.String()})
	}
	return out, hidden
}

// prints reports whether v prints Element i (e is its line as the view
// shows it). The root always prints, and so does an Element that carries
// a walk's cut marks: the agent needs to know a part was not read.
func (v View) prints(i int, e *Element) bool {
	if v.Depth > 0 && e.Depth > v.Depth {
		return false
	}
	if i == 0 || e.Truncated != nil || e.MoreChildren > 0 {
		return true
	}
	if v.Interactive && !e.ActedOn() {
		return false
	}
	if v.Compact && e.bare() {
		return false
	}
	return true
}

// bare reports whether an Element's line would say nothing but its index
// and role: no name, state, description, value, toggle, Secondary Action
// or ID.
func (e *Element) bare() bool {
	if Collapse(e.Name) != "" || e.states() != "" || Collapse(e.Description) != "" || Collapse(e.Value) != "" ||
		Collapse(e.Toggle) != "" || Collapse(e.AutomationId) != "" {
		return false
	}
	for _, a := range e.SecondaryActions {
		if Collapse(a) != "" {
			return false
		}
	}
	return true
}

// directChildren counts Element i's direct children in a depth-first
// pre-order list.
func directChildren(els []Element, i int) int {
	n := 0
	for j := i + 1; j < len(els) && els[j].Depth > els[i].Depth; j++ {
		if els[j].Depth == els[i].Depth+1 {
			n++
		}
	}
	return n
}
