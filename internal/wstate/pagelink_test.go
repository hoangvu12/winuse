package wstate

import (
	"strings"
	"testing"
)

// linkTree is a small Page-like Window State: a heading with its level and
// two links with their URLs.
func linkTree() *Tree {
	return &Tree{Title: "Docs", App: "chrome.exe", FocusedIndex: -1, Elements: []Element{
		{Depth: 0, Role: "window", Name: "Docs"},
		{Depth: 1, Role: "heading", Name: "Install", Level: 2},
		{Depth: 1, Role: "link", Name: "Download", URL: "/download"},
		{Depth: 1, Role: "link", Name: "Source", URL: "github.com/x/y"},
		{Depth: 1, Role: "button", Name: "Search"},
	}}
}

// TestHeadingLevelIsAState (bladebro-ideas 01): a heading's level is one
// of its states, on every line that prints the heading, except --full's:
// --full is Codex's text byte for byte, and Codex prints no level.
func TestHeadingLevelIsAState(t *testing.T) {
	tree := linkTree()
	if out := RenderView(tree, View{}); !strings.Contains(out, "\t\t1 heading (level 2) Install\n") {
		t.Errorf("default:\n%s", out)
	}
	if out := RenderView(tree, View{Full: true}); !strings.Contains(out, "\t\t1 heading Install\n") || strings.Contains(out, "level") {
		t.Errorf("--full printed a level:\n%s", out)
	}
	tree.FocusedIndex = 1
	if out := RenderView(tree, View{Full: true}); !strings.Contains(out, "The focused UI element is 1 heading Install.") {
		t.Errorf("--full's focus sentence printed a level:\n%s", out)
	}
	tree.FocusedIndex = -1
	if got := Line(1, &tree.Elements[1]); got != "1 heading (level 2) Install" {
		t.Errorf("Line = %q", got)
	}
	e := Element{Role: "heading", Name: "Off", Level: 3, Disabled: true}
	if got := Line(0, &e); got != "0 heading (disabled, level 3) Off" {
		t.Errorf("the level comes after Codex's states: %q", got)
	}
}

// TestLinkURLPrintsWhereAsked (bladebro-ideas 01): a link's URL prints
// after its line, as " URL: <url>", only where asked: with --urls
// (View.URLs) on a read, and on the lines printed without the tree around
// them (an ambiguous reference's candidates). --full prints Codex's text,
// with no URL, as before.
func TestLinkURLPrintsWhereAsked(t *testing.T) {
	tree := linkTree()
	plain := RenderView(tree, View{})
	if strings.Contains(plain, "URL:") || !strings.Contains(plain, "\t\t2 link Download\n") {
		t.Errorf("the default read prints no URL:\n%s", plain)
	}
	if full := Render(tree); strings.Contains(full, "URL:") {
		t.Errorf("--full printed a URL:\n%s", full)
	}
	if full := RenderView(tree, View{Full: true, URLs: true}); full != RenderCodex(tree) {
		t.Errorf("--full is RenderCodex's text whatever else is set:\n%s", full)
	}
	urls := RenderView(tree, View{URLs: true})
	for _, want := range []string{"\t\t2 link Download URL: /download\n", "\t\t3 link Source URL: github.com/x/y\n", "\t\t4 button Search\n", "\t\t1 heading (level 2) Install\n"} {
		if !strings.Contains(urls, want) {
			t.Errorf("--urls: no %q in\n%s", want, urls)
		}
	}
	if got := HintedLine(2, &tree.Elements[2], Hint{}); got != "2 link Download URL: /download" {
		t.Errorf("HintedLine = %q", got)
	}
	if got := HintedLine(2, &tree.Elements[2], Hint{Own: "icon download", Row: -1}); got != "2 link Download URL: /download Hint: icon download" {
		t.Errorf("the URL goes before the hint: %q", got)
	}
}

// TestDiffSeesAChangedURL (bladebro-ideas 01): --diff and --observe count
// a link whose URL changed as a changed line, and the line shows both.
func TestDiffSeesAChangedURL(t *testing.T) {
	before := fixtureApp(t)
	before.Elements = append(before.Elements, Element{Depth: 1, Role: "link", Name: "Download", URL: "/download"})
	after := cloneTree(before)
	last := len(after.Elements) - 1
	after.Elements[last].URL = "/get"

	changes := Diff(before.Elements, after.Elements)
	if len(changes) != 1 || changes[0] != (Change{last, Changed}) {
		t.Fatalf("changes %v, want only %d changed", changes, last)
	}
	got := RenderChanges(before.Elements, after, View{Ref: "1234"}, "window 1234")
	if want := "\t\t~26 link Download URL: /get (was: link Download URL: /download)\n"; got.Whole || !strings.Contains(got.Text, want) {
		t.Errorf("the diff:\n%s\nwant the line %q", got.Text, want)
	}
	if same := Diff(before.Elements, before.Elements); len(same) != 0 {
		t.Errorf("an unchanged read differs: %v", same)
	}
}
