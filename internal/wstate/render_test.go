package wstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMinimalTree(t *testing.T) {
	tree := &Tree{
		Title: "Untitled - Notepad",
		App:   "Notepad.exe",
		Elements: []Element{
			{Depth: 0, Role: "window", Name: "Untitled - Notepad", AutomationId: "Window"},
			{Depth: 1, Role: "edit", Name: "Text Editor", AutomationId: "15"},
		},
		FocusedIndex: -1,
	}
	want := "Window: \"Untitled - Notepad\", App: Notepad.exe.\n" +
		"\t0 window Untitled - Notepad ID: Window\n" +
		"\t\t1 edit Text Editor ID: 15\n"
	if got := Render(tree); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderOmitsEmptyParts(t *testing.T) {
	// A nameless, id-less element renders as just the index and role; no
	// double spaces, no trailing tokens.
	tree := &Tree{
		Title: "T",
		App:   "a.exe",
		Elements: []Element{
			{Depth: 0, Role: "pane"},
			{Depth: 1, Role: "text"},
		},
		FocusedIndex: -1,
	}
	want := "Window: \"T\", App: a.exe.\n\t0 pane\n\t\t1 text\n"
	if got := Render(tree); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderStatesOrderAndToggleToken(t *testing.T) {
	tree := &Tree{
		Title: "T",
		App:   "a.exe",
		Elements: []Element{
			{Depth: 0, Role: "list item", Selectable: true, Selected: true, Disabled: true,
				ExpandState: "partially expanded", Settable: "float", Toggle: "off", AutomationId: "x"},
			{Depth: 0, Role: "check box", Name: "Opt", Toggle: "indeterminate"},
			{Depth: 0, Role: "check box", Name: "On", Toggle: "on"},
		},
		FocusedIndex: -1,
	}
	want := "Window: \"T\", App: a.exe.\n" +
		"\t0 list item (selectable, selected, disabled, partially expanded, settable, float) off ID: x\n" +
		"\t1 check box Opt indeterminate\n" +
		"\t2 check box On on\n"
	if got := Render(tree); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderFocusedTailAndNotes(t *testing.T) {
	tree := &Tree{
		Title: "T",
		App:   "a.exe",
		Elements: []Element{
			{Depth: 0, Role: "window", Name: "T"},
			{Depth: 1, Role: "edit", Name: "Search", AutomationId: "search"},
		},
		FocusedIndex: 1,
		Notes:        []string{"first note", "second note"},
	}
	want := "Window: \"T\", App: a.exe.\n" +
		"\t0 window T\n\t\t1 edit Search ID: search\n" +
		"\n" +
		"The focused UI element is 1 edit Search ID: search.\n" +
		"Note: first note\n" +
		"Note: second note\n"
	if got := Render(tree); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderNotesWithoutFocusedStillBlankLine(t *testing.T) {
	tree := &Tree{
		Title: "T", App: "a.exe",
		Elements:     []Element{{Depth: 0, Role: "window"}},
		FocusedIndex: -1,
		Notes:        []string{"a note"},
	}
	want := "Window: \"T\", App: a.exe.\n\t0 window\n\nNote: a note\n"
	if got := Render(tree); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderOutOfRangeFocusedIndexIsIgnored(t *testing.T) {
	tree := &Tree{Title: "T", App: "a", Elements: []Element{{Depth: 0, Role: "window"}}, FocusedIndex: 7}
	want := "Window: \"T\", App: a.\n\t0 window\n"
	if got := Render(tree); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderCollapsesWhitespace(t *testing.T) {
	tree := &Tree{
		Title:        "Multi   line\n title ",
		App:          " a.exe ",
		FocusedIndex: -1,
		Elements: []Element{
			{Depth: 0, Role: "text", Name: "hello\tworld\nagain", AutomationId: " id 1 ",
				Description: " help text ", Value: "a\nb", Toggle: "on"},
		},
	}
	want := "Window: \"Multi line title\", App: a.exe.\n" +
		"\t0 text hello world again Description: help text Value: a b on ID: id 1\n"
	if got := Render(tree); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderHeaderFallsBackWhenAppMissing(t *testing.T) {
	tree := &Tree{Title: "T", Elements: []Element{{Depth: 0, Role: "window"}}, FocusedIndex: -1}
	want := "Window: \"T\", App: unknown.\n\t0 window\n"
	if got := Render(tree); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

// TestRenderFixtures is the seam-2 rendering test: recorded (or
// hand-authored, marked as such in each fixture's "source") trees render to
// the expected text. The .json files are the recorder's file format; the
// .txt files pin the exact bytes.
func TestRenderFixtures(t *testing.T) {
	for _, name := range []string{"fixture-app", "word-style"} {
		t.Run(name, func(t *testing.T) {
			jsonPath := filepath.Join("testdata", name+".json")
			txtPath := filepath.Join("testdata", name+".txt")
			tree, err := LoadFixture(jsonPath)
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			if tree.Source == "" {
				t.Errorf("fixture %s must declare its provenance in \"source\"", name)
			}
			want, err := os.ReadFile(txtPath)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if got := Render(tree); got != string(want) {
				t.Errorf("render of %s:\n got:\n%s\nwant:\n%s", jsonPath, got, want)
			}
		})
	}
}

func TestLoadFixtureRejectsWrongFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"format": 99, "title": "x", "app": "y", "elements": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFixture(path); err == nil || !strings.Contains(err.Error(), "format") {
		t.Errorf("want a format error, got %v", err)
	}
}

// TestFixtureRoundTrip pins the recorder's file format: a Tree marshals to
// JSON that loads back and renders identically.
func TestFixtureRoundTrip(t *testing.T) {
	tree := &Tree{
		Format: FixtureFormat,
		Title:  "Round trip",
		App:    "app.exe",
		Elements: []Element{
			{Depth: 0, Role: "window", Name: "Round trip", SecondaryActions: []string{"Raise"}},
			{Depth: 1, Role: "button", Name: "OK", AutomationId: "ok", Disabled: true,
				Truncated: &Truncation{Limit: "7", OmittedChildren: 2}},
		},
		FocusedIndex: 1,
		Notes:        []string{"a note"},
	}
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "tree.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	back, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if Render(back) != Render(tree) {
		t.Errorf("round-trip render differs:\n%q\n%q", Render(back), Render(tree))
	}
}
