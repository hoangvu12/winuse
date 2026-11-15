package wstate

import (
	"errors"
	"strings"
	"testing"
)

// hintTree is a Window with two list rows that each hold a "Delete" and an
// unnamed button, a data item whose own text names it, a Page-like control
// that carries its reader's hint and row text, and controls that are fine
// as they are.
func hintTree() *Tree {
	return &Tree{
		Title: "Cart", App: "app.exe", FocusedIndex: -1,
		Elements: []Element{
			{Depth: 0, Role: "window", Name: "Cart"},                                                         // 0
			{Depth: 1, Role: "list", Name: "Items"},                                                          // 1
			{Depth: 2, Role: "list item", Name: "Blue mug"},                                                  // 2
			{Depth: 3, Role: "button", Name: "Delete"},                                                       // 3: duplicate
			{Depth: 3, Role: "button"},                                                                       // 4: unnamed
			{Depth: 2, Role: "list item"},                                                                    // 5: unnamed row
			{Depth: 3, Role: "text", Name: "Red mug"},                                                        // 6
			{Depth: 3, Role: "button", Name: "Delete"},                                                       // 7: duplicate
			{Depth: 1, Role: "button", Name: "Checkout"},                                                     // 8: unique
			{Depth: 1, Role: "button", Hint: "icon trash, testid clear-cart"},                                // 9: the reader's hint
			{Depth: 1, Role: "group"},                                                                        // 10
			{Depth: 2, Role: "link", Name: "Edit", RowText: "Green mug 12 EUR"},                              // 11: the reader's row
			{Depth: 2, Role: "link", Name: "Edit", RowText: "Yellow mug with a very long name that goes on"}, // 12
			{Depth: 1, Role: "button"},                                                                       // 13: unnamed, nothing known
		},
	}
}

func TestHintsPickUnnamedAndDuplicateControls(t *testing.T) {
	hs := Hints(hintTree().Elements)
	want := map[int]Hint{
		3:  {In: "Blue mug", Row: 2},
		4:  {In: "Blue mug", Row: 2},
		7:  {In: "Red mug", Row: 5},
		9:  {Own: "icon trash, testid clear-cart", Row: -1},
		11: {In: "Green mug 12 EUR", Row: -1},
		12: {In: "Yellow mug with a very long name that g…", Row: -1},
	}
	for i := range hintTree().Elements {
		if got := hintAt(hs, i); got != want[i] {
			t.Errorf("Element %d: hint %+v, want %+v", i, got, want[i])
		}
	}
	if Hints([]Element{{Role: "window"}, {Depth: 1, Role: "button", Name: "OK"}, {Depth: 1, Role: "text"}}) != nil {
		t.Error("a Window with nothing to tell apart has no hints")
	}
}

// TestHintsLeaveOutWhatTwinsShare: a part two twins share does not tell
// them apart, so it is left out (an unnamed control keeps its icon, which
// says what it is); a container only reaches up to the next twin.
func TestHintsLeaveOutWhatTwinsShare(t *testing.T) {
	els := []Element{
		{Depth: 0, Role: "window", Name: "Shop"},
		{Depth: 1, Role: "group", Name: "Card"}, // 1: holds both "Save"s: no container
		{Depth: 2, Role: "button", Name: "Save", Hint: "testid save"},
		{Depth: 2, Role: "button", Name: "Save", Hint: "testid save"},
		{Depth: 1, Role: "group", Name: "Row A"},
		{Depth: 2, Role: "button", Hint: "icon star", RowText: "Same row"},
		{Depth: 1, Role: "group", Name: "Row B"},
		{Depth: 2, Role: "button", Hint: "icon star", RowText: "Same row"},
	}
	hs := Hints(els)
	for i, want := range map[int]Hint{2: {}, 3: {}, 5: {Own: "icon star", Row: -1}, 7: {Own: "icon star", Row: -1}} {
		if got := hintAt(hs, i); got != want {
			t.Errorf("Element %d: %+v, want %+v", i, got, want)
		}
	}
}

// TestRenderViewPrintsHintsWhereTheyDisambiguate: the default view prints
// the reader's own parts and leaves the row to the lines above; the
// --interactive view adds the row a hidden or unnamed row would have
// shown, and leaves it out under a named row it prints; --full prints
// Codex's text with no hint at all.
func TestRenderViewPrintsHintsWhereTheyDisambiguate(t *testing.T) {
	tree := hintTree()
	def := lineIndexes(t, RenderView(tree, View{}))
	if def[3] != "button Delete" || def[9] != "button Hint: icon trash, testid clear-cart" || def[11] != "link Edit" {
		t.Errorf("default view:\n%s", RenderView(tree, View{}))
	}
	in := lineIndexes(t, RenderView(tree, View{Interactive: true}))
	for i, want := range map[int]string{
		3:  "button Delete",                       // under the printed list item "Blue mug"
		7:  "button Delete Hint: in Red mug",      // its row is unnamed
		11: "link Edit Hint: in Green mug 12 EUR", // the reader's row
		8:  "button Checkout",
		13: "button",
	} {
		if in[i] != want {
			t.Errorf("--interactive line %d = %q, want %q", i, in[i], want)
		}
	}
	if d := lineIndexes(t, RenderView(tree, View{Depth: 2})); d[3] != "" || d[2] != "list item Blue mug (+2 children)" {
		t.Errorf("--depth 2: %v", d)
	}
	if full := RenderView(tree, View{Full: true}); strings.Contains(full, "Hint:") || full != Render(tree) {
		t.Errorf("--full must print no hint:\n%s", full)
	}
}

// TestCompactKeepsAHintedLine: --compact folds a line with nothing but its
// index and role, unless a hint tells it from the others.
func TestCompactKeepsAHintedLine(t *testing.T) {
	got := lineIndexes(t, RenderView(hintTree(), View{Compact: true}))
	if got[9] == "" || got[13] != "" || got[10] != "" {
		t.Errorf("--compact: %v", got)
	}
}

// TestDiffAndCandidatesCarryTheWholeHint: a diff line and an ambiguous
// reference's candidates have no rows around them, so the row part prints.
func TestDiffAndCandidatesCarryTheWholeHint(t *testing.T) {
	before := hintTree()
	after := hintTree()
	for i := 0; i < 30; i++ { // enough unchanged lines for a diff to be shorter than the whole
		filler := Element{Depth: 1, Role: "text", Name: "Unchanged line of the Window"}
		before.Elements = append(before.Elements, filler)
		after.Elements = append(after.Elements, filler)
	}
	after.Elements[7].Disabled = true
	got := RenderChanges(before.Elements, after, View{}, "window 1")
	if !strings.Contains(got.Text, "\t\t\t\t~7 button (disabled) Delete Hint: in Red mug (was: button Delete)\n") {
		t.Errorf("diff:\n%s", got.Text)
	}
	if strings.Contains(RenderChanges(before.Elements, after, View{Full: true}, "window 1").Text, "Hint:") {
		t.Error("--full diff printed a hint")
	}
	_, err := ResolveElementRef(before.Elements, "name:Delete")
	var several *AmbiguousRefError
	if !errors.As(err, &several) || strings.Join(several.Candidates, "|") != "3 button Delete Hint: in Blue mug|7 button Delete Hint: in Red mug" {
		t.Errorf("candidates: %v", err)
	}
	// A hint is not a name.
	if _, err := ResolveElementRef(before.Elements, "name:icon trash"); err == nil {
		t.Error("name: matched the text only a hint carries")
	}
}
