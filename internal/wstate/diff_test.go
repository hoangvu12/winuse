package wstate

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// fixtureApp is the recorded fixture App's Window State (26 Elements).
func fixtureApp(t *testing.T) *Tree {
	t.Helper()
	tree, err := LoadFixture(filepath.Join("testdata", "fixture-app.json"))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// cloneTree copies a tree deeply enough to change its Elements.
func cloneTree(t *Tree) *Tree {
	c := *t
	c.Elements = append([]Element(nil), t.Elements...)
	c.Notes = append([]string(nil), t.Notes...)
	return &c
}

func TestDiffByIndex(t *testing.T) {
	before := []Element{{Role: "window", Name: "W"}, {Depth: 1, Role: "edit", Value: "a"}, {Depth: 1, Role: "text", Name: "x"}}
	after := []Element{{Role: "window", Name: "W"}, {Depth: 1, Role: "edit", Value: "ab"}, {Depth: 2, Role: "text", Name: "x"}, {Depth: 1, Role: "button", Name: "OK"}}
	got := Diff(before, after)
	want := []Change{{1, Changed}, {2, Changed}, {3, New}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d: got %v, want %v (a depth change counts too)", i, got[i], want[i])
		}
	}
	if gone := Diff(after, before); gone[len(gone)-1] != (Change{3, Gone}) {
		t.Errorf("an index the new read ends before is gone: %v", gone)
	}
}

func TestRenderChangesValueAndTail(t *testing.T) {
	before := fixtureApp(t)
	after := cloneTree(before)
	after.Elements[5].Value = "hello"
	after.Elements[25].Name = "Status: saved"
	after.Elements = append(after.Elements, Element{Depth: 1, Role: "button", Name: "Undo", AutomationId: "undo"})

	got := RenderChanges(before.Elements, after, View{Ref: "1234"}, "window 1234")
	want := `Window: "winuse fixture app", App: winusefixture.exe.
Changed since this agent's last read of window 1234 (~ changed, * new, - gone); Element Indexes not listed are unchanged:
		~5 edit (settable, string) Text Field Value: hello ID: text-field (was: edit (settable, string) Text Field ID: text-field)
		~25 text Status: saved ID: status (was: text Status: ready ID: status)
		*26 button Undo ID: undo

The focused UI element is 5 edit (settable, string) Text Field Value: hello ID: text-field.
`
	if got.Text != want || got.Whole || got.Changes != 3 {
		t.Errorf("got (whole %v, %d changes):\n%s\nwant:\n%s", got.Whole, got.Changes, got.Text, want)
	}
	if n, full := utf8.RuneCountInString(got.Text), utf8.RuneCountInString(Render(after)); n*2 > full {
		t.Errorf("the diff (%d characters) should be well under the whole state (%d)", n, full)
	}
}

func TestRenderChangesNoChange(t *testing.T) {
	tree := fixtureApp(t)
	got := RenderChanges(tree.Elements, tree, View{Ref: "1234"}, "window 1234")
	want := "Window: \"winuse fixture app\", App: winusefixture.exe.\nNo change since this agent's last read of window 1234.\n"
	if got.Text != want || got.Changes != 0 {
		t.Errorf("got:\n%q\nwant:\n%q", got.Text, want)
	}
}

func TestRenderChangesGoneIndexes(t *testing.T) {
	before := fixtureApp(t)
	after := cloneTree(before)
	after.Elements = after.Elements[:24] // the canvas and the status text went
	got := RenderChanges(before.Elements, after, View{}, "window 1234")
	for _, line := range []string{"\t\t-24 custom Canvas ID: canvas\n", "\t\t-25 text Status: ready ID: status\n"} {
		if !strings.Contains(got.Text, line) {
			t.Errorf("missing %q in:\n%s", line, got.Text)
		}
	}
}

// An Element inserted near the top moves every index after it: past the
// threshold the whole Window State prints, with this read's indexes.
func TestRenderChangesFallsBackToWhole(t *testing.T) {
	before := fixtureApp(t)
	after := cloneTree(before)
	ins := Element{Depth: 1, Role: "text", Name: "Banner"}
	after.Elements = append(append(append([]Element(nil), after.Elements[:1]...), ins), after.Elements[1:]...)
	got := RenderChanges(before.Elements, after, View{Ref: "1234"}, "window 1234")
	if !got.Whole {
		t.Fatalf("an insertion at the top should print the whole state:\n%s", got.Text)
	}
	whole := RenderView(after, View{Ref: "1234"})
	if !strings.HasPrefix(got.Text, whole[:strings.Index(whole, "\n\n")]) {
		t.Errorf("the whole state should be the view's:\n%s", got.Text)
	}
	if full := RenderChanges(before.Elements, after, View{Full: true}, "window 1234"); !strings.HasPrefix(full.Text, Render(after)[:strings.Index(Render(after), "\n\n")]) {
		t.Errorf("under --full the whole state is Render's, shared IDs included:\n%s", full.Text)
	}
	if !strings.Contains(got.Text, "Note: the whole Window State: 26 Element Indexes changed, so a diff would not be shorter\n") {
		t.Errorf("the note should say why:\n%s", got.Text)
	}
}

func TestRenderChangesNoBaseline(t *testing.T) {
	tree := fixtureApp(t)
	got := RenderChanges(nil, tree, View{}, "window 1234")
	if !got.Whole || got.Changes != -1 || !strings.Contains(got.Text, "Note: the whole Window State: this agent had no earlier read of window 1234 to diff against\n") {
		t.Errorf("got (whole %v):\n%s", got.Whole, got.Text)
	}
	if len(tree.Notes) != 0 {
		t.Errorf("the note must not be added to the caller's tree: %v", tree.Notes)
	}
}

// The views shape a diff as they shape a read: --interactive leaves out a
// text's change and says so; --max-chars cuts whole lines.
func TestRenderChangesViews(t *testing.T) {
	before := fixtureApp(t)
	after := cloneTree(before)
	after.Elements[5].Value = "hello"
	after.Elements[25].Name = "Status: saved"
	got := RenderChanges(before.Elements, after, View{Interactive: true, Ref: "1234"}, "window 1234")
	if strings.Contains(got.Text, "~25") || !strings.Contains(got.Text, "~5 edit") {
		t.Errorf("--interactive prints the edit's change only:\n%s", got.Text)
	}
	if !strings.Contains(got.Text, "Note: --interactive left out 1 of the 2 changes\n") {
		t.Errorf("missing the left-out note:\n%s", got.Text)
	}

	for i := 15; i < 18; i++ {
		after.Elements[i].Selected = true
	}
	cut := RenderChanges(before.Elements, after, View{MaxChars: 700, Ref: "1234"}, "window 1234")
	if n := utf8.RuneCountInString(cut.Text); n > 700 || cut.Whole {
		t.Errorf("--max-chars 700 printed %d characters (whole %v):\n%s", n, cut.Whole, cut.Text)
	}
	if !strings.Contains(cut.Text, "Note: --max-chars 700 stopped the output") {
		t.Errorf("missing the cut note:\n%s", cut.Text)
	}
}

// A diff leaves out the IDs several Elements share, as a read does by
// default (speed 13); --full keeps them, as Codex's text does.
func TestRenderChangesSharedIDs(t *testing.T) {
	before := fixtureApp(t)
	after := cloneTree(before)
	after.Elements[12].Disabled = true // "Solo Two", whose ID 11 shares
	got := RenderChanges(before.Elements, after, View{}, "window 1234")
	if !strings.Contains(got.Text, "~12 button (disabled) Solo Two (was: button Solo Two)\n") {
		t.Errorf("the default drops the shared ID on both sides:\n%s", got.Text)
	}
	full := RenderChanges(before.Elements, after, View{Full: true}, "window 1234")
	if !strings.Contains(full.Text, "~12 button (disabled) Solo Two ID: solo (was: button Solo Two ID: solo)\n") {
		t.Errorf("--full keeps it:\n%s", full.Text)
	}
}
