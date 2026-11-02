package page

import (
	"errors"
	"strings"
	"testing"
)

// probeShape is the ticket 03 probe's real shape (probe-results.md
// section 1): the first window plus two opened with newWindow:true, which
// share Chrome's default bounds, and a background Tab in the first window
// repeating window 2's title.
func probeShape() (wins []Geometry, tabs []Tab, want []string) {
	first := DIPBounds{Left: 100, Top: 60, Width: 1200, Height: 800, State: "normal"}
	dflt := DIPBounds{Left: 22, Top: 22, Width: 1050, Height: 700, State: "normal"}
	tabs = []Tab{
		{TargetID: "AAAA1111", Title: "winuse probe page", WindowID: 1, Bounds: first, HasBounds: true},
		{TargetID: "BBBB2222", Title: "winuse probe window 2", WindowID: 2, Bounds: dflt, HasBounds: true},
		{TargetID: "CCCC3333", Title: "winuse probe window 3", WindowID: 3, Bounds: dflt, HasBounds: true},
		{TargetID: "DDDD4444", Title: "winuse probe window 2", WindowID: 1, Bounds: first, HasBounds: true}, // background
	}
	wins = []Geometry{
		{Title: "winuse probe page - Google Chrome", Rect: box(first, 96)},
		{Title: "winuse probe window 2 - Google Chrome", Rect: box(dflt, 96)},
		{Title: "winuse probe window 3 - Google Chrome", Rect: box(dflt, 96)},
	}
	return wins, tabs, []string{"AAAA1111", "BBBB2222", "CCCC3333"}
}

// box is the GetWindowRect a DIP rectangle has at a DPI.
func box(b DIPBounds, dpi uint32) Box {
	s := float64(dpi) / 96
	return Box{Left: int32(float64(b.Left) * s), Top: int32(float64(b.Top) * s),
		Right: int32(float64(b.Left+b.Width) * s), Bottom: int32(float64(b.Top+b.Height) * s)}
}

func TestMatchTabProbeShapeAtEveryScale(t *testing.T) {
	for _, dpi := range []uint32{96, 144, 168} {
		wins, tabs, want := probeShape()
		matched := 0
		for i, g := range wins {
			g.DPI = dpi
			g.Rect = box(tabs[i].Bounds, dpi)
			// A px or two of rounding stays within the tolerance.
			g.Rect.Right += 2
			got, err := MatchTab(g, "chrome", tabs)
			if err != nil {
				t.Errorf("dpi %d, window %q: %v", dpi, g.Title, err)
				continue
			}
			if got.TargetID != want[i] {
				t.Errorf("dpi %d, window %q: matched %s, want %s", dpi, g.Title, got.TargetID, want[i])
				continue
			}
			matched++
		}
		if matched != 3 {
			t.Errorf("dpi %d: %d of 3 matched", dpi, matched)
		}
	}
}

func TestMatchTabNeedsBothSignals(t *testing.T) {
	wins, tabs, _ := probeShape()
	// Bounds alone: windows 2 and 3 share them. Title alone: window 2's
	// title repeats in window 1. Unscaled bounds at 150% match nothing.
	g := wins[1]
	g.DPI = 144
	if _, err := MatchTab(g, "chrome", tabs); err == nil {
		t.Error("150% bounds compared unscaled must not match")
	}
}

func TestMatchTabAmbiguousNamesTheCandidates(t *testing.T) {
	wins, tabs, _ := probeShape()
	// Two Tabs of window 1 with the same title, and nothing (CDP) says
	// which one is active.
	tabs[3].Title = "winuse probe page"
	_, err := MatchTab(wins[0], "chrome", tabs)
	var amb *AmbiguousTabError
	if !errors.As(err, &amb) || len(amb.Candidates) != 2 {
		t.Fatalf("err = %v", err)
	}
	want := `several Tabs match this Window's title and bounds: "winuse probe page" (target AAAA1111), "winuse probe page" (target DDDD4444)`
	if err.Error() != want {
		t.Errorf("error:\n got %s\nwant %s", err, want)
	}
	// Knowing the active Tab (the extension) settles it.
	no, yes := false, true
	tabs[0].Active, tabs[3].Active = &yes, &no
	if got, err := MatchTab(wins[0], "chrome", tabs); err != nil || got.TargetID != "AAAA1111" {
		t.Errorf("with active known: %v, %v", got.TargetID, err)
	}
}

func TestMatchTabNone(t *testing.T) {
	wins, tabs, _ := probeShape()
	g := wins[0]
	g.Title = "New Tab - Google Chrome"
	_, err := MatchTab(g, "chrome", tabs)
	var none *NoTabError
	if !errors.As(err, &none) || err.Error() != "no Tab of the Browser Connection matches this Window's title and bounds" {
		t.Fatalf("err = %v", err)
	}
	// The title matches but the bounds are another window's.
	g = wins[0]
	g.Rect = Box{Left: 900, Top: 900, Right: 1300, Bottom: 1300}
	if _, err := MatchTab(g, "chrome", tabs); !errors.As(err, &none) {
		t.Errorf("err = %v", err)
	}
	// Bounds that could not be read never match.
	tabs[0].HasBounds = false
	if _, err := MatchTab(wins[0], "chrome", tabs); !errors.As(err, &none) {
		t.Errorf("err = %v", err)
	}
}

func TestMatchTabMinimizedAndMaximized(t *testing.T) {
	_, tabs, _ := probeShape()
	tabs[1].Bounds.State = "minimized"
	g := Geometry{Title: "winuse probe window 2 - Google Chrome", Rect: Box{Left: -32000, Top: -32000, Right: -31840, Bottom: -31972}, Minimized: true, DPI: 144}
	if got, err := MatchTab(g, "chrome", tabs); err != nil || got.TargetID != "BBBB2222" {
		t.Errorf("minimized: %s, %v", got.TargetID, err)
	}
	tabs[1].Bounds = DIPBounds{Left: 0, Top: 0, Width: 1280, Height: 680, State: "maximized"}
	g = Geometry{Title: "winuse probe window 2 - Google Chrome", Rect: Box{Left: -8, Top: -8, Right: 1928, Bottom: 1040}, Maximized: true, DPI: 144}
	if got, err := MatchTab(g, "chrome", tabs); err != nil || got.TargetID != "BBBB2222" {
		t.Errorf("maximized: %s, %v", got.TargetID, err)
	}
	g.Maximized = false
	if _, err := MatchTab(g, "chrome", tabs); err == nil {
		t.Error("a normal Window never matches a maximized browser window")
	}
}

func TestTitleMatchesBrowserSuffixes(t *testing.T) {
	cases := []struct {
		window, tab, browser string
		want                 bool
	}{
		{"Inbox - Google Chrome", "Inbox", "chrome", true},
		{"Inbox - Google Chrome - Work", "Inbox", "chrome", true},
		{"Inbox", "Inbox", "chrome", true},
		{"Inbox - Personal - Microsoft​ Edge", "Inbox", "edge", true},
		{"Inbox and 2 more pages - Personal - Microsoft Edge", "Inbox", "edge", true},
		{"Inbox and 1 more page - Microsoft​ Edge", "Inbox", "edge", true},
		{"Inbox - Brave", "Inbox", "brave", true},
		{"Inbox - Chromium", "Inbox", "chromium", true},
		{"Inbox - Vivaldi", "Inbox", "vivaldi", true},
		{"Inbox  -  Google  Chrome", "Inbox", "chrome", true},
		{"Inboxes - Google Chrome", "Inbox", "chrome", false},
		{"Inbox (3) - Google Chrome", "Inbox", "chrome", false},
		{"Inbox - Brave", "Inbox", "chrome", false},
		{"Inbox - Microsoft Edge", "Inbox", "chrome", false},
		{"Inbox - a - b - Google Chrome", "Inbox", "chrome", false},
		{"Google Chrome", "", "chrome", false},
		{"Inbox - Something", "Inbox", "unknown", true},
	}
	for _, c := range cases {
		if got := TitleMatches(c.window, c.tab, c.browser); got != c.want {
			t.Errorf("TitleMatches(%q, %q, %s) = %v, want %v", c.window, c.tab, c.browser, got, c.want)
		}
	}
}

func TestReasonWording(t *testing.T) {
	for _, err := range []error{
		&Unavailable{Reason: "x"},
		&NoTabError{},
		&AmbiguousTabError{Candidates: []Tab{{TargetID: "T", Title: "A"}}},
		errors.New("boom"),
	} {
		note := FallbackNote(Reason(err))
		if !strings.HasPrefix(note, "the Page is read through UI Automation because ") || !strings.HasSuffix(note, ".") {
			t.Errorf("note %q", note)
		}
		if strings.ContainsAny(note, "–—") {
			t.Errorf("note %q has a dash", note)
		}
	}
}
