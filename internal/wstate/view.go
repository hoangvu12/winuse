package wstate

// Output budgets for a full read (speed 05): views that print less
// of a Window State without changing it. The Window State the helper
// stores is the whole read either way, so every Element Index a view
// prints is the full read's, and one it leaves out still works.
//
// Every view but Full leaves out the IDs two or more Elements of the read
// share (speed 13: the default), which an id: reference refuses as
// ambiguous anyway; a unique ID always prints. Every view but Full adds a
// hint to an unnamed or duplicate control (hint.go, learn-from 04).

import (
	"fmt"
	"sort"
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

	// Visible prints only the Elements on screen: it leaves out those a
	// Page read found scrolled out of the viewport (Element.Offscreen).
	// Elements whose place is unknown (a desktop Window's, an iframe's)
	// print.
	Visible bool

	// MarkOffscreen prints every Element, an off-screen one with
	// OffscreenMark after its index: for winuse run, which sends a Jev
	// server what is on screen and the off-screen Elements its goal names
	// (internal/jev). No CLI flag sets it.
	MarkOffscreen bool

	// Depth prints only the Elements at most Depth levels below the
	// Window (0: every level); one with children below the cut says how
	// many, as "(+N children)".
	Depth int

	// Compact folds every Element whose line would say nothing but its
	// index and role (an unnamed, valueless, stateless, non-interactive
	// container, its shared ID left out) into its parent.
	Compact bool

	// URLs prints a Page link's URL after its line, as " URL: <url>"
	// (--urls, bladebro-ideas 01). Off by default: on a page of links most
	// lines would carry one.
	URLs bool

	// MaxChars stops printing Element lines before the output passes this
	// many characters, with a note naming the Elements not printed (0: no
	// cap). The header and the tail (the focused Element and the notes)
	// are always printed.
	MaxChars int

	// Ref is the Window as the notes' --root commands name it ("1234");
	// empty, the notes leave --root out (a Tab target has no --root).
	Ref string

	// Before is the Agent's earlier Window State of the Window (nil: none).
	// A landmark holding an Element whose line changed since then is never
	// folded (landmarkFolds).
	Before []Element

	// folded says RenderView turned Compact on itself, because the read
	// did not fit MaxChars (fold before the cut).
	folded bool

	// landmarks says RenderView folded the Page's chrome landmarks, because
	// the read did not fit MaxChars even with Compact (bladebro-ideas 08).
	landmarks bool
}

// Page landmarks (internal/page/roles.go). A read past its cap folds the
// chrome ones of a Page that has main content (bladebro-ideas 08).
var (
	chromeRoles  = map[string]bool{"navigation": true, "banner": true, "footer": true, "complementary": true}
	contentRoles = map[string]bool{"main": true, "dialog": true}
)

// minFold is the fewest Elements a landmark must hold to be folded: a
// smaller one costs about what its fold line does.
const minFold = 3

// OffscreenMark follows the index of an off-screen Element's line under
// View.MarkOffscreen.
const OffscreenMark = "[off screen]"

// flags names the view's shaping flags as the CLI spells them.
func (v View) flags() string {
	var f []string
	if v.Interactive {
		f = append(f, "--interactive")
	}
	if v.Visible {
		f = append(f, "--visible")
	}
	if v.Depth > 0 {
		f = append(f, "--depth "+strconv.Itoa(v.Depth))
	}
	fit := " to fit --max-chars " + strconv.Itoa(v.MaxChars)
	switch {
	case v.folded && v.landmarks:
		f = append(f, "folding unnamed containers (as --compact does) and Page landmarks"+fit)
	case v.folded:
		f = append(f, "folding unnamed containers"+fit+" (as --compact does)")
	case v.Compact && v.landmarks:
		f = append(f, "--compact and folding Page landmarks"+fit)
	case v.Compact:
		f = append(f, "--compact")
	}
	return strings.Join(f, " ")
}

// RenderView prints a Window State through a view: Render's format, with
// each printed Element indented one level below its nearest printed
// ancestor, and a note saying what the view left out and how to see it.
//
// A read that MaxChars would cut folds its bare containers first, as
// Compact does, and cuts only what still does not fit (reliability-council
// 12): on a Chromium or Electron Window a third of the Elements are
// unnamed wrappers, and folding them and their indent fits about 1.5
// times as many useful Elements under the default cap (Discord,
// measured). One still too long folds a Page's chrome landmarks next
// (landmarkFolds, bladebro-ideas 08), so the cut drops chrome before
// content. A read that fits prints as before.
func RenderView(t *Tree, v View) string {
	if v.Full {
		return Render(t)
	}
	shared := SharedIDs(t.Elements)
	out, cut := renderView(t, v, shared)
	if cut && !v.Compact {
		v.Compact, v.folded = true, true
		out, cut = renderView(t, v, shared)
	}
	if cut && len(v.landmarkFolds(t)) > 0 {
		v.landmarks = true
		out, _ = renderView(t, v, shared)
	}
	return out
}

// landmarkFolds are the chrome landmarks (navigation, banner, footer,
// complementary) v may fold, each with the number of Elements it holds:
// on a Page with main content (a main or dialog Element), each that holds
// minFold or more Elements, but none holding the focused Element, main
// content, a walk's cut mark, or an Element whose line changed since
// v.Before (unless most
// of the read changed: another page). Of two nested ones, the outer folds
// when it can.
func (v View) landmarkFolds(t *Tree) map[int]int {
	els := t.Elements
	content := false
	for i := range els {
		content = content || contentRoles[els[i].Role]
	}
	if !content {
		return nil
	}
	changed := map[int]bool{}
	// Past DiffThreshold of the read, the earlier read was another page (or
	// the indexes all moved): nothing in particular changed.
	if changes := Diff(v.Before, els); v.Before != nil && float64(len(changes)) <= DiffThreshold*float64(len(els)) {
		for _, c := range changes {
			changed[c.Index] = c.Kind != Gone
		}
	}
	focused := t.Focused()
	folds := map[int]int{}
	for i := 0; i < len(els); i++ {
		if !chromeRoles[els[i].Role] {
			continue
		}
		end := i + 1
		for end < len(els) && els[end].Depth > els[i].Depth {
			end++
		}
		open := end-i-1 < minFold || focused >= i && focused < end
		for j := i; j < end && !open; j++ {
			open = changed[j] || contentRoles[els[j].Role] || els[j].Truncated != nil // a walk's cut mark always prints
		}
		if !open {
			folds[i] = end - i - 1
			i = end - 1
		}
	}
	return folds
}

// renderView is RenderView's one pass through v; cut reports that
// MaxChars stopped the Element lines.
func renderView(t *Tree, v View, shared map[string]bool) (string, bool) {
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
		if (v.Depth > 0 || v.landmarks) && v.Ref != "" {
			tail.WriteString("; read a part whole with winuse get-window-state " + v.Ref + " --root <element>")
		}
		tail.WriteString("\n")
	}

	hint := "Narrow the read with --interactive, --compact or --depth, or write it all with --out <file>"
	if v.Ref != "" {
		hint = "See them with winuse get-window-state " + v.Ref + " --root <element>, narrow the read with --interactive, --compact or --depth, or write it all with --out <file>"
	}
	return cutLines(head.String(), lines, tail.String(), v.MaxChars, hint)
}

// PrintedLine is one Element line of a printed Window State or find
// answer: its Element Index, its text (indent and newline included) and
// its Element's role, which the cut note counts.
type PrintedLine struct {
	Index int
	Text  string
	Role  string
}

// Cut joins a header, Element lines and a tail (after a blank line, when
// there is one). With maxChars above 0 it stops the lines before the
// output passes that many characters and ends the tail with a note naming
// the lines not printed and their roles, then hint, the way to see them.
// The header and the tail are never cut, so a tiny cap can still be
// passed.
func Cut(head string, lines []PrintedLine, tail string, maxChars int, hint string) string {
	out, _ := cutLines(head, lines, tail, maxChars, hint)
	return out
}

// cutRoles is how many roles the cut note names; past it, the last is
// "other" (bladebro-ideas 08).
const cutRoles = 5

// roleCounts says what lines hold by role, as the cut note does: ": 30
// link, 8 button, 3 edit", the most first and then by role, so one page
// always gives the same text; past cutRoles roles, the rest count as
// "other". "" when no line has a role.
func roleCounts(lines []PrintedLine) string {
	counts := map[string]int{}
	for _, l := range lines {
		counts[l.Role]++
	}
	other := counts[""]
	delete(counts, "")
	if len(counts) == 0 {
		return ""
	}
	roles := make([]string, 0, len(counts))
	for r := range counts {
		roles = append(roles, r)
	}
	sort.Slice(roles, func(i, j int) bool {
		if counts[roles[i]] != counts[roles[j]] {
			return counts[roles[i]] > counts[roles[j]]
		}
		return roles[i] < roles[j]
	})
	named := len(roles)
	if named > cutRoles || named == cutRoles && other > 0 {
		named = cutRoles - 1
	}
	parts := make([]string, 0, cutRoles)
	for _, r := range roles[:named] {
		parts = append(parts, strconv.Itoa(counts[r])+" "+Collapse(r))
	}
	for _, r := range roles[named:] {
		other += counts[r]
	}
	if other > 0 {
		parts = append(parts, strconv.Itoa(other)+" other")
	}
	return ": " + strings.Join(parts, ", ")
}

// roleCountsBound is the longest roleCounts could print for some of lines.
func roleCountsBound(lines []PrintedLine) string {
	longest := ""
	for _, l := range lines {
		if r := Collapse(l.Role); utf8.RuneCountInString(r) > utf8.RuneCountInString(longest) {
			longest = r
		}
	}
	if longest == "" {
		return ""
	}
	parts := make([]string, 0, cutRoles)
	for range cutRoles - 1 {
		parts = append(parts, "9999999 "+longest)
	}
	return ": " + strings.Join(append(parts, "9999999 other"), ", ")
}

// cutLines is Cut, and whether it stopped the lines.
func cutLines(head string, lines []PrintedLine, tail string, maxChars int, hint string) (string, bool) {
	const format = "Note: --max-chars %d stopped the output after %s: %d more Element lines (from Element %d on%s) are not printed; their Element Indexes still work. %s\n"
	printed := len(lines)
	if maxChars > 0 {
		reserve := utf8.RuneCountInString(fmt.Sprintf(format, maxChars, "Element 9999999", 9999999, 9999999, roleCountsBound(lines), hint))
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
		tail += fmt.Sprintf(format, maxChars, last, len(lines)-printed, lines[printed].Index, roleCounts(lines[printed:]), hint)
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
	return b.String(), printed < len(lines)
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
// left out and its controls' hints added, and counts the Elements it
// leaves out (a --max-chars cut aside).
func viewLines(t *Tree, v View, shared map[string]bool) ([]PrintedLine, int) {
	type shown struct{ depth, level int }
	var stack []shown // the printed ancestors of the current Element
	var out []PrintedLine
	hidden := 0
	hints := Hints(t.Elements)
	var folds map[int]int
	if v.landmarks {
		folds = v.landmarkFolds(t)
	}
	for i := 0; i < len(t.Elements); i++ {
		e := t.Elements[i].WithoutSharedID(shared)
		for len(stack) > 0 && stack[len(stack)-1].depth >= e.Depth {
			stack = stack[:len(stack)-1]
		}
		if v.Depth > 0 && e.Depth == v.Depth {
			e.MoreChildren = max(e.MoreChildren, directChildren(t.Elements, i))
		}
		folded, fold := folds[i]
		fold = fold && (v.Depth == 0 || e.Depth <= v.Depth)
		hint := v.viewHint(t, hints, i)
		extra := hint != "" || v.URLs && Collapse(e.URL) != ""
		if !fold && !v.prints(i, &e) && !(extra && v.unfolded().prints(i, &e)) { // --compact keeps a hinted line, and one with a URL it prints
			hidden++
			continue
		}
		if fold {
			// One line for the landmark and all it holds (bladebro-ideas 08).
			e.MoreChildren = 0
			level := 0
			if len(stack) > 0 {
				level = stack[len(stack)-1].level + 1
			}
			var b strings.Builder
			b.WriteString(strings.Repeat("\t", level+1))
			writeElement(&b, i, &e, false)
			fmt.Fprintf(&b, " (+%d Elements folded", folded)
			if v.Ref != "" {
				fmt.Fprintf(&b, "; --root %d shows them", i)
			}
			b.WriteString(")\n")
			out = append(out, PrintedLine{Index: i, Text: b.String(), Role: e.Role})
			hidden += folded
			i += folded
			continue
		}
		level := 0
		if len(stack) > 0 {
			level = stack[len(stack)-1].level + 1
		}
		stack = append(stack, shown{e.Depth, level})
		var b strings.Builder
		b.WriteString(strings.Repeat("\t", level+1))
		var line strings.Builder
		writeElement(&line, i, &e, false)
		text := line.String()
		if v.MarkOffscreen && e.Offscreen {
			idx := strconv.Itoa(i) + " "
			text = idx + OffscreenMark + " " + strings.TrimPrefix(text, idx)
		}
		b.WriteString(text)
		if v.URLs {
			writeURL(&b, &e)
		}
		writeHint(&b, hint)
		b.WriteByte('\n')
		out = append(out, PrintedLine{Index: i, Text: b.String(), Role: e.Role})
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
	if v.Visible && e.Offscreen {
		return false
	}
	if v.Compact && e.bare() {
		return false
	}
	return true
}

// unfolded is v without --compact.
func (v View) unfolded() View {
	v.Compact = false
	return v
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
