package page

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Mapping a Window to its Tab (spec "Mapping a Window to its Tab"; the
// ticket 03 probe, probe-results.md section 1). Neither signal is enough
// alone: windows opened with newWindow:true share the same default
// bounds, and a background Tab can repeat a title. Both together matched
// every Window at 100%, 150% and 175%.

// Geometry is what the matching needs to know about a browser Window.
type Geometry struct {
	Title     string
	Rect      Box    // GetWindowRect, physical pixels
	DPI       uint32 // GetDpiForWindow; 0 counts as 96
	Minimized bool
	Maximized bool

	// HWND and PID name the Window (its handle and its browser process) in
	// Memory, which settles an ambiguous match by the browser window the
	// Window last mapped to (WindowMemory). Nil Memory or a zero HWND
	// remembers nothing.
	HWND   uint64
	PID    uint32
	Memory *WindowMemory
}

// Box is a rectangle as left, top, right, bottom.
type Box struct{ Left, Top, Right, Bottom int32 }

// Tab is one page target with what the matching reads.
type Tab struct {
	TargetID string
	Title    string
	URL      string
	// Bounds are the Tab's browser window bounds in DIPs, from
	// Browser.getWindowForTarget (or chrome.windows on the extension),
	// with that window's state (normal, minimized, maximized, fullscreen).
	// HasBounds is false when they could not be read.
	Bounds    DIPBounds
	HasBounds bool
	WindowID  int
	// Active is known on the extension only (chrome.tabs active); a known
	// background Tab is never a candidate.
	Active *bool
	// Internal marks a Tab that shows a browser page (chrome://newtab/,
	// edge://settings/): winuse maps, lists, switches, closes and navigates
	// it, but never reads or acts inside it (IsTabTarget).
	Internal bool
}

// DIPBounds are a browser window's bounds in device independent pixels.
type DIPBounds struct {
	Left, Top, Width, Height int
	State                    string
}

// boundsTolerance is "within a few px": the probe matched at 0 px off, and
// allowed 8.
const boundsTolerance = 8

// NoTabError says no Tab matched the Window. Titles lists the open Tabs'
// titles that the Window title did not show, for the note.
type NoTabError struct{ Titles []string }

func (e *NoTabError) Error() string {
	return "no Tab of the Browser Connection matches this Window's title and bounds"
}

// AmbiguousTabError says several Tabs matched; never a guess.
type AmbiguousTabError struct{ Candidates []Tab }

func (e *AmbiguousTabError) Error() string {
	var names []string
	for _, t := range e.Candidates {
		names = append(names, fmt.Sprintf("%q (target %s)", Collapse(t.Title), shortID(t.TargetID)))
	}
	return "several Tabs match this Window's title and bounds: " + strings.Join(names, ", ")
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// MatchTab picks the active Tab of a browser Window among the browser's
// Tabs: the Tabs whose window bounds, scaled by the Window's DPI, equal the
// Window's GetWindowRect within a few px, and whose title the Window title
// shows (TitleMatches). A minimized Window matches the minimized browser
// windows and a maximized one the maximized windows on its screen area,
// since their bounds do not line up with GetWindowRect. Exactly one
// candidate is the answer; none is a *NoTabError and several an
// *AmbiguousTabError naming them.
func MatchTab(g Geometry, browserName string, tabs []Tab) (Tab, error) {
	var found []Tab
	for _, t := range tabs {
		if t.Active != nil && !*t.Active {
			continue
		}
		if !TitleMatches(g.Title, t.Title, browserName) || !t.HasBounds || !boundsMatch(g, t.Bounds) {
			continue
		}
		found = append(found, t)
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		var titles []string
		for _, t := range tabs {
			titles = append(titles, Collapse(t.Title))
		}
		sort.Strings(titles)
		return Tab{}, &NoTabError{Titles: titles}
	}
	return Tab{}, &AmbiguousTabError{Candidates: found}
}

// MatchAppTab picks the page of an App's Window (headless-max 15): an
// Electron or WebView2 App winuse launched with --cdp has no tab strip, and
// its DevTools may not answer Browser.getWindowForTarget. The App's one
// page is the answer when it has one; otherwise the pages whose title the
// Window title shows (TitleMatches, any suffix of one or two parts). One
// candidate is the answer; none is a *NoTabError and several an
// *AmbiguousTabError, as in MatchTab.
func MatchAppTab(g Geometry, tabs []Tab) (Tab, error) {
	var pages []Tab
	for _, t := range tabs {
		if !t.Internal {
			pages = append(pages, t)
		}
	}
	if len(pages) == 1 {
		return pages[0], nil
	}
	var found []Tab
	for _, t := range pages {
		if TitleMatches(g.Title, t.Title, "") {
			found = append(found, t)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		var titles []string
		for _, t := range pages {
			titles = append(titles, Collapse(t.Title))
		}
		sort.Strings(titles)
		return Tab{}, &NoTabError{Titles: titles}
	}
	return Tab{}, &AmbiguousTabError{Candidates: found}
}

// boundsMatch compares a browser window's DIP bounds with the Window.
func boundsMatch(g Geometry, b DIPBounds) bool {
	switch {
	case g.Minimized:
		return b.State == "minimized"
	case b.State == "minimized":
		return false
	case g.Maximized || b.State == "maximized" || b.State == "fullscreen":
		// A maximized window's rect overhangs the screen by its invisible
		// borders; the state and the screen area decide instead.
		if !(g.Maximized && (b.State == "maximized" || b.State == "fullscreen")) {
			return false
		}
		cx, cy := scaled(g, float64(b.Left)+float64(b.Width)/2), scaled(g, float64(b.Top)+float64(b.Height)/2)
		return cx >= float64(g.Rect.Left) && cx <= float64(g.Rect.Right) && cy >= float64(g.Rect.Top) && cy <= float64(g.Rect.Bottom)
	}
	edges := [4]float64{
		scaled(g, float64(b.Left)), scaled(g, float64(b.Top)),
		scaled(g, float64(b.Left+b.Width)), scaled(g, float64(b.Top+b.Height)),
	}
	want := [4]float64{float64(g.Rect.Left), float64(g.Rect.Top), float64(g.Rect.Right), float64(g.Rect.Bottom)}
	for i := range edges {
		if math.Abs(edges[i]-want[i]) > boundsTolerance {
			return false
		}
	}
	return true
}

// scaled turns DIPs into the Window's physical pixels.
func scaled(g Geometry, v float64) float64 {
	dpi := g.DPI
	if dpi == 0 {
		dpi = 96
	}
	return v * float64(dpi) / 96
}

// brandNames are the names each browser puts at the end of its Window
// titles ("<tab title> - Google Chrome").
var brandNames = map[string][]string{
	"chrome":   {"Google Chrome", "Google Chrome for Testing", "Chrome"},
	"chromium": {"Chromium"},
	"edge":     {"Microsoft Edge", "Edge"},
	"brave":    {"Brave"},
	"vivaldi":  {"Vivaldi"},
	"opera":    {"Opera"},
}

// morePages is Edge's "and N more pages" between the tab title and the
// rest of the Window title.
var morePages = regexp.MustCompile(`^ and \d+ more pages?`)

// TitleMatches reports whether a browser Window title shows a Tab title.
// Chromium titles a Window "<tab title> - <browser>"; Edge adds its
// profile ("<tab title> - Personal - Microsoft Edge") and, with several
// tabs, "and N more pages". The rest after the Tab title must be exactly
// such a suffix: " - <brand>", with at most one more part (a profile)
// before or after the brand. With an unknown browser any one or two parts
// pass. Zero-width characters and runs of spaces are ignored.
func TitleMatches(windowTitle, tabTitle, browserName string) bool {
	w, t := Collapse(windowTitle), Collapse(tabTitle)
	if t == "" {
		return false
	}
	if w == t {
		return true
	}
	rest, ok := strings.CutPrefix(w, t)
	if !ok {
		return false
	}
	rest = morePages.ReplaceAllString(rest, "")
	rest, ok = strings.CutPrefix(rest, " - ")
	if !ok {
		return false
	}
	parts := strings.Split(rest, " - ")
	if len(parts) > 2 {
		return false
	}
	brands := brandNames[browserName]
	if len(brands) == 0 {
		return true
	}
	for _, p := range parts {
		for _, b := range brands {
			if strings.EqualFold(p, b) {
				return true
			}
		}
	}
	return false
}
