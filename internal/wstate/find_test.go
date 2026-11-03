package wstate

// find's matching over the recorded fixture tree (desktop-parity 04).

import (
	"slices"
	"strings"
	"testing"
)

func fixtureTree(t *testing.T) *Tree {
	t.Helper()
	tree, err := LoadFixture("testdata/fixture-app.json")
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func matching(tree *Tree, q Query) []int {
	var out []int
	for i, e := range tree.Elements {
		if q.Match(e) {
			out = append(out, i)
		}
	}
	return out
}

func TestQueryMatch(t *testing.T) {
	tree := fixtureTree(t)
	for _, c := range []struct {
		q    Query
		want []int
	}{
		{Query{Role: "button"}, []int{6, 7, 8, 9, 10, 11, 12}},
		{Query{Role: "Menu  Item"}, []int{2, 3, 4}},
		{Query{Name: "solo"}, []int{11, 12}},
		{Query{Role: "button", Name: "save"}, []int{7}},
		{Query{Text: "ready"}, []int{25}},
		{Query{State: "disabled"}, []int{8}},
		{Query{Role: "button", State: "enabled"}, []int{6, 7, 9, 10, 11, 12}},
		{Query{State: "collapsed"}, []int{2, 3, 4, 23}},
		{Query{State: "settable"}, []int{5}},
		{Query{State: "off"}, []int{13}},
		{Query{Role: "list item", State: "selectable", Name: "o"}, []int{16, 19, 20, 21, 22}},
		{Query{Role: "slider"}, nil},
	} {
		if got := matching(tree, c.q); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.q, got, c.want)
		}
	}
	if (Query{}).String() != "no filter" || (Query{Role: "button", Name: "Save"}).String() != `role "button", name "Save"` {
		t.Errorf("String: %q", Query{Role: "button", Name: "Save"})
	}
}

func TestInteractive(t *testing.T) {
	tree := fixtureTree(t)
	got := matching(tree, Query{State: "interactive"})
	want := []int{2, 3, 4, 5, 6, 7, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23}
	if !slices.Equal(got, want) {
		t.Errorf("interactive: %v, want %v (not the window with only Raise, the disabled button, the canvas or the status text)", got, want)
	}
	if !ValidState("Interactive") || ValidState("pressed") {
		t.Error("ValidState")
	}
}

func TestRolesPresent(t *testing.T) {
	got := RolesPresent(fixtureTree(t).Elements)
	want := []string{"button", "check box", "custom", "edit", "group", "list", "list item", "menu bar", "menu item", "text", "window"}
	if !slices.Equal(got, want) {
		t.Errorf("roles = %q, want %q", got, want)
	}
}

func TestRenderMoreChildren(t *testing.T) {
	e := Element{Role: "pane", Name: "Body", MoreChildren: 12}
	if got := Line(4, &e); got != "4 pane Body (+12 children)" {
		t.Errorf("line %q", got)
	}
	tree := &Tree{Title: "T", App: "a.exe", Elements: []Element{{Role: "window", Name: "T"}, {Depth: 1, Role: "pane", MoreChildren: 3}}, FocusedIndex: -1}
	if !strings.Contains(Render(tree), "\t\t1 pane (+3 children)\n") {
		t.Errorf("render:\n%s", Render(tree))
	}
}
