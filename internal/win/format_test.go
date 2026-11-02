package win

import (
	"strings"
	"testing"
)

func TestFormatWindows(t *testing.T) {
	ws := []WindowInfo{
		{ID: 131072, App: "Notepad", Title: "Untitled - Notepad"},
		{ID: 197504, App: "Notepad", Title: "Save As"},
		{ID: 262256, App: "Microsoft Edge", Title: "Tabs and spaces - Docs"},
	}
	want := "131072\tNotepad\tUntitled - Notepad\n" +
		"197504\tNotepad\tSave As\n" +
		"262256\tMicrosoft Edge\tTabs and spaces - Docs\n"
	if got := FormatWindows(ws); got != want {
		t.Fatalf("FormatWindows:\n got %q\nwant %q", got, want)
	}
}

// The output must be machine-parseable: split on \t with limit 3 so titles
// containing tabs stay in the title field.
func TestFormatWindowsParsesBack(t *testing.T) {
	ws := []WindowInfo{
		{ID: 1, App: "App", Title: "title with\ttab and  double  spaces"},
		{ID: 2, App: "", Title: "no app"},
	}
	lines := strings.Split(strings.TrimSuffix(FormatWindows(ws), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	parts := strings.SplitN(lines[0], "\t", 3)
	if parts[0] != "1" || parts[1] != "App" || parts[2] != "title with\ttab and  double  spaces" {
		t.Fatalf("parse: %q", parts)
	}
	parts = strings.SplitN(lines[1], "\t", 3)
	if parts[0] != "2" || parts[1] != "" || parts[2] != "no app" {
		t.Fatalf("parse: %q", parts)
	}
}

func TestFormatWindowsEmpty(t *testing.T) {
	if got := FormatWindows(nil); got != "" {
		t.Fatalf("want empty output, got %q", got)
	}
}

func TestFormatWindowsIsListableLine(t *testing.T) {
	// The skill will tell agents to parse lines with a regexp; keep the line
	// shape stable: digits, tab, app, tab, title.
	ws := []WindowInfo{{ID: 1234567890123, App: "a", Title: "b"}}
	for _, line := range strings.Split(FormatWindows(ws), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "1234567890123\t") {
			t.Fatalf("line must start with the Window ID: %q", line)
		}
	}
}

func TestFormatWindowsDetailed(t *testing.T) {
	ws := []WindowInfo{
		{ID: 131072, App: "Notepad", Title: "Untitled - Notepad", PID: 4312,
			Bounds: Rect{Left: 100, Top: 50, Right: 1124, Bottom: 818}},
		{ID: 197504, App: "Notepad", Title: "title with\ttab", PID: 4312, Minimized: true,
			Bounds: Rect{Left: -32000, Top: -32000, Right: -31840, Bottom: -31972}},
		{ID: 262256, App: "", Title: "", PID: 77,
			Bounds: Rect{Left: -1928, Top: -8, Right: 8, Bottom: 1048}},
	}
	d := []WindowDetails{{Focused: true}, {Admin: true}, {Focused: true, Maximized: true, Admin: true}}
	want := "131072\tNotepad\t100,50 1024x768 pid 4312 focused\tUntitled - Notepad\n" +
		"197504\tNotepad\t-32000,-32000 160x28 pid 4312 minimized admin\ttitle with\ttab\n" +
		"262256\t\t-1928,-8 1936x1056 pid 77 focused maximized admin\t\n"
	got := FormatWindowsDetailed(ws, d)
	if got != want {
		t.Fatalf("FormatWindowsDetailed:\n got %q\nwant %q", got, want)
	}
	// The title stays last: split on \t with limit 4.
	line := strings.Split(got, "\n")[1]
	if parts := strings.SplitN(line, "\t", 4); parts[3] != "title with\ttab" {
		t.Errorf("parse: %q", parts)
	}
	// Missing details read as all false.
	if got := FormatWindowsDetailed(ws[:1], nil); got != "131072\tNotepad\t100,50 1024x768 pid 4312\tUntitled - Notepad\n" {
		t.Errorf("no details: %q", got)
	}
}
