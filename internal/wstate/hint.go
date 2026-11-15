package wstate

// Hints on unnamed or duplicate controls (learn-from 04, agent-browser's
// hint="..."). Agents pick the wrong one when several controls print the
// same line ("button Delete" in every row) or when a control has no name
// (an icon button). A view then adds " Hint: ..." to such a control's line:
// what the reader knew of it besides its line (Element.Hint, a Page
// Element's icon, image, test id or field name), and the text of the row,
// list item or card it sits in (Element.RowText from the DOM, or the
// nearest container above it in the read that holds none of its twins).
// It is computed from the read alone: no reader call per Element.
//
// A hint is not a name: name: references and find's filters never match
// it. Render (--full, Codex's text) never prints one.

import (
	"sort"
	"strings"
)

// hintRoles are the roles of the controls an agent picks between. A row
// itself (list item, data item, tree item) is what a hint names, not what
// gets one, and a table's cells repeat their text by nature.
var hintRoles = map[string]bool{
	"button": true, "split button": true, "link": true, "check box": true, "radio button": true,
	"menu item": true, "edit": true, "combo box": true, "slider": true, "spinner": true,
	"clickable": true,
}

// The caps on what a hint prints: each part, and the row text.
const (
	hintPartLimit = 40
	hintRowLimit  = 40
)

// Hint is what tells one control from others like it.
type Hint struct {
	Own string // Element.Hint: the reader's own parts, "icon trash" ...
	In  string // the text of the row, list item or card the control sits in
	Row int    // the Element In was read from; -1 when In came from the reader (RowText) or is empty
}

// IsZero reports a Hint with nothing to say.
func (h Hint) IsZero() bool { return h.Own == "" && h.In == "" }

// Text is the hint as a line prints it, after "Hint: "; with in false the
// row part is left out.
func (h Hint) Text(in bool) string {
	var parts []string
	if h.Own != "" {
		parts = append(parts, h.Own)
	}
	if in && h.In != "" {
		parts = append(parts, "in "+h.In)
	}
	return strings.Join(parts, ", ")
}

// Hints is, for each of els (one read, depth-first), the Hint of a control
// that is unnamed or whose role and name another control of els shares,
// and the zero Hint for every other Element. It returns nil when no
// Element gets one.
func Hints(els []Element) []Hint {
	groups := map[string][]int{} // the controls that print the same line's start, in order
	for i := range els {
		if hintRoles[els[i].Role] {
			k := hintKey(&els[i])
			groups[k] = append(groups[k], i)
		}
	}
	var out []Hint
	var t *treeShape
	for _, twins := range groups {
		if Collapse(els[twins[0]].Name) != "" && len(twins) < 2 {
			continue
		}
		near := make([][]place, len(twins)) // each twin's containers, nearest first, when the reader gave no row
		for k, i := range twins {
			if Collapse(els[i].RowText) == "" {
				if t == nil {
					t = shapeOf(els)
				}
				near[k] = t.containers(els, i, twins)
			}
		}
		in := pickContainers(near)
		hs := make([]Hint, len(twins))
		own, rows := map[string]int{}, map[string]int{}
		for k, i := range twins {
			h := Hint{Own: clip(Collapse(els[i].Hint), 3*hintPartLimit), In: clip(Collapse(els[i].RowText), hintRowLimit), Row: -1}
			if h.In == "" && in[k].text != "" {
				h.In, h.Row = in[k].text, in[k].index
			}
			own[h.Own]++
			rows[h.In]++
			hs[k] = h
		}
		named := Collapse(els[twins[0]].Name) != ""
		for k, i := range twins {
			// A part another twin shares tells them apart no better than the
			// line: left out, but for what an unnamed control is (its icon).
			h := hs[k]
			if named && own[h.Own] > 1 {
				h.Own = ""
			}
			if rows[h.In] > 1 {
				h.In, h.Row = "", -1
			}
			if h.IsZero() {
				continue
			}
			if out == nil {
				out = make([]Hint, len(els))
			}
			out[i] = h
		}
	}
	return out
}

// hintAt is hs[i], or the zero Hint.
func hintAt(hs []Hint, i int) Hint {
	if i < len(hs) {
		return hs[i]
	}
	return Hint{}
}

// hintKey is what two controls must share to print the same line's start.
func hintKey(e *Element) string { return e.Role + "\x00" + Collapse(e.Name) }

// treeShape is a depth-first list's parents and subtree ends.
type treeShape struct {
	parent []int // -1 for a root
	end    []int // one past the Element's last descendant
}

func shapeOf(els []Element) *treeShape {
	t := &treeShape{parent: make([]int, len(els)), end: make([]int, len(els))}
	var stack []int
	for i := range els {
		for len(stack) > 0 && els[stack[len(stack)-1]].Depth >= els[i].Depth {
			t.end[stack[len(stack)-1]] = i
			stack = stack[:len(stack)-1]
		}
		t.parent[i] = -1
		if len(stack) > 0 {
			t.parent[i] = stack[len(stack)-1]
		}
		stack = append(stack, i)
	}
	for _, j := range stack {
		t.end[j] = len(els)
	}
	return t
}

// The bounds of the row context: the ancestors climbed, and the size of an
// unnamed container whose Elements' names are its text.
const (
	hintClimb   = 6
	hintRowSize = 16
)

// place is a container's text and its Element Index.
type place struct {
	text  string
	index int
}

// containers are the texts of the containers Element i sits in, nearest
// first: its ancestors up to hintClimb levels (the first Element, the
// Window or the read's root, aside), until one holds another of twins. A
// container's text is its name, or for a small unnamed one the names under
// it outside i's subtree. The container of a "Delete" in each row is its
// row, whatever role the row has.
func (t *treeShape) containers(els []Element, i int, twins []int) []place {
	own := Collapse(els[i].Name)
	var out []place
	for j, climbed := t.parent[i], 0; j > 0 && climbed < hintClimb; j, climbed = t.parent[j], climbed+1 {
		if holdsAnother(twins, i, j, t.end[j]) {
			break
		}
		if name := Collapse(els[j].Name); name != "" && name != own {
			out = append(out, place{clip(name, hintRowLimit), j})
			continue
		}
		if t.end[j]-j > hintRowSize {
			continue
		}
		var parts []string
		for k := j + 1; k < t.end[j]; k++ {
			if k == i {
				k = t.end[i] - 1 // the control's own subtree
				continue
			}
			if name := Collapse(els[k].Name); name != "" && name != own && (len(parts) == 0 || parts[len(parts)-1] != name) {
				parts = append(parts, name)
			}
		}
		if len(parts) > 0 {
			out = append(out, place{clip(strings.Join(parts, " "), hintRowLimit), j})
		}
	}
	return out
}

// pickContainers picks each twin's container: its nearest one whose text
// no other twin's containers have, or none. A control with no twin (an
// unnamed one alone of its role) takes its nearest.
func pickContainers(near [][]place) []place {
	out := make([]place, len(near))
	if len(near) == 1 {
		if len(near[0]) > 0 {
			out[0] = near[0][0]
		}
		return out
	}
	holders := map[string]int{} // a text to the number of twins with a container of that text
	for _, ps := range near {
		seen := map[string]bool{}
		for _, p := range ps {
			if !seen[p.text] {
				seen[p.text] = true
				holders[p.text]++
			}
		}
	}
	for k, ps := range near {
		for _, p := range ps {
			if holders[p.text] == 1 {
				out[k] = p
				break
			}
		}
	}
	return out
}

// holdsAnother reports whether a twin other than i is in [from, to).
func holdsAnother(twins []int, i, from, to int) bool {
	k := sort.SearchInts(twins, from)
	for ; k < len(twins) && twins[k] < to; k++ {
		if twins[k] != i {
			return true
		}
	}
	return false
}

// viewHint is the hint a view prints on Element i, or "": the reader's own
// parts always, and the row part only where the view does not show that
// text around the line. A view with neither --interactive nor --depth
// prints every Element, so the row's text is a few lines up; otherwise the
// row part is left out only under a named row the view prints.
func (v View) viewHint(t *Tree, hs []Hint, i int) string {
	h := hintAt(hs, i)
	if h.IsZero() {
		return ""
	}
	in := v.Interactive || v.Depth > 0
	if in && h.Row >= 0 && Collapse(t.Elements[h.Row].Name) != "" {
		row := t.Elements[h.Row]
		in = !v.prints(h.Row, &row)
	}
	return h.Text(in)
}

// HintedLine is Line with the hint after it, as find, a diff and an
// ambiguous reference print a control without the rows around it.
func HintedLine(index int, e *Element, h Hint) string {
	var b strings.Builder
	writeElement(&b, index, e, false)
	writeHint(&b, h.Text(true))
	return b.String()
}

// writeHint adds " Hint: <text>" to a line, when there is one.
func writeHint(b *strings.Builder, text string) {
	if text != "" {
		b.WriteString(" Hint: ")
		b.WriteString(text)
	}
}
