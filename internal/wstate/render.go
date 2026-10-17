package wstate

import (
	"strconv"
	"strings"
)

// Render prints a Window State as Codex's depth-first, indented, numbered
// tree (see the package comment for the grammar). Every rendered string is
// whitespace-collapsed so each Element is exactly one line.
//
// The tail (after a blank line): the focused Element's sentence when one is
// focused, then one "Note: ..." line per note (partial-tree and sparse-tree
// notes from the walk).
func Render(t *Tree) string {
	var b strings.Builder
	writeHeader(&b, t)
	for i := range t.Elements {
		writeElement(&b, i, &t.Elements[i], true)
	}
	if focused, note := t.Focused(), len(t.Notes) > 0; focused >= 0 || note {
		b.WriteByte('\n')
		if focused >= 0 {
			b.WriteString("The focused UI element is ")
			writeElement(&b, focused, &t.Elements[focused], false)
			b.WriteString(".\n")
		}
		for _, n := range t.Notes {
			b.WriteString("Note: ")
			b.WriteString(n)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// writeHeader prints the Window State's header line:
// Window: "<title>", App: <app>.
func writeHeader(b *strings.Builder, t *Tree) {
	b.WriteString(`Window: "`)
	b.WriteString(Collapse(t.Title))
	b.WriteString(`", App: `)
	if app := Collapse(t.App); app != "" {
		b.WriteString(app)
	} else {
		b.WriteString("unknown")
	}
	b.WriteString(".\n")
}

// writeElement prints one Element line. With indent (inside the tree) it is
// TAB-indented by depth+1; without (the focused-Element sentence) the same
// line content follows the sentence's "is ".
func writeElement(b *strings.Builder, index int, e *Element, indent bool) {
	if indent {
		b.WriteString(strings.Repeat("\t", e.Depth+1))
	}
	b.WriteString(strconv.Itoa(index))
	b.WriteByte(' ')
	b.WriteString(Collapse(e.Role))
	if states := e.states(); states != "" {
		b.WriteString(" (")
		b.WriteString(states)
		b.WriteByte(')')
	}
	if name := Collapse(e.Name); name != "" {
		b.WriteByte(' ')
		b.WriteString(name)
	}
	if desc := Collapse(e.Description); desc != "" {
		b.WriteString(" Description: ")
		b.WriteString(desc)
	}
	if value := Collapse(e.Value); value != "" {
		b.WriteString(" Value: ")
		b.WriteString(value)
	}
	if toggle := Collapse(e.Toggle); toggle != "" {
		b.WriteByte(' ')
		b.WriteString(toggle)
	}
	if len(e.SecondaryActions) > 0 {
		actions := make([]string, 0, len(e.SecondaryActions))
		for _, a := range e.SecondaryActions {
			if a = Collapse(a); a != "" {
				actions = append(actions, a)
			}
		}
		if len(actions) > 0 {
			b.WriteString(" Secondary Actions: ")
			b.WriteString(strings.Join(actions, ", "))
		}
	}
	if id := Collapse(e.AutomationId); id != "" {
		b.WriteString(" ID: ")
		b.WriteString(id)
	}
	if e.Truncated != nil {
		b.WriteString(" (truncated: ")
		b.WriteString(Collapse(e.Truncated.Limit))
		b.WriteString(", omitted ")
		b.WriteString(strconv.Itoa(e.Truncated.OmittedChildren))
		b.WriteString(" children)")
	}
	if indent {
		b.WriteByte('\n')
	}
}

// states renders the parenthesised state list, comma-separated in Codex's
// order: selectable, selected, disabled, collapsed/expanded/partially
// expanded, then "settable, string" (writable ValuePattern) or
// "settable, float" (RangeValue).
func (e *Element) states() string {
	var parts []string
	if e.Selectable {
		parts = append(parts, "selectable")
	}
	if e.Selected {
		parts = append(parts, "selected")
	}
	if e.Disabled {
		parts = append(parts, "disabled")
	}
	if e.ExpandState != "" {
		parts = append(parts, e.ExpandState)
	}
	if e.Settable != "" {
		parts = append(parts, "settable, "+e.Settable)
	}
	return strings.Join(parts, ", ")
}
