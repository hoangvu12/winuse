package wstate

// Offline tests for name:/id: Element Reference resolution (ticket 11,
// seam 2): parsing the reference forms and resolving them against a
// Window State's Elements — unique, ambiguous and missing matches, the
// candidate lines ambiguity reports, and the exact-match semantics (case,
// whitespace collapsing, Name vs AutomationId columns).

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// refElements is a small tree with the shapes reference resolution cares
// about: two Elements sharing a Name, two sharing an AutomationId, and
// unique names and IDs elsewhere.
func refElements() []Element {
	return []Element{
		{Depth: 0, Role: "window", Name: "Example", AutomationId: "w"},
		{Depth: 1, Role: "edit", Name: "Search", AutomationId: "searchBox"},
		{Depth: 1, Role: "button", Name: "Twin", AutomationId: "twin-a"},
		{Depth: 1, Role: "button", Name: "Twin", AutomationId: "twin-b"},
		{Depth: 1, Role: "button", Name: "Solo One", AutomationId: "solo"},
		{Depth: 1, Role: "button", Name: "Solo Two", AutomationId: "solo"},
		{Depth: 1, Role: "text", Name: "", AutomationId: "unnamed"},
		{Depth: 1, Role: "check box", Name: "Notify me", AutomationId: "notify", Toggle: "off",
			SecondaryActions: []string{"Toggle"}},
	}
}

func TestParseElementRef(t *testing.T) {
	cases := []struct {
		ref      string
		wantKind RefKind
		wantText string
		wantErr  bool
	}{
		{ref: "name:OK", wantKind: RefName, wantText: "OK"},
		{ref: "name:Save As", wantKind: RefName, wantText: "Save As"},
		{ref: "name:name:OK", wantKind: RefName, wantText: "name:OK"}, // only the first prefix splits
		{ref: "id:searchBox", wantKind: RefID, wantText: "searchBox"},
		{ref: "id:14681076", wantKind: RefID, wantText: "14681076"},
		{ref: "name:", wantErr: true},
		{ref: "id:", wantErr: true},
		{ref: "name:   ", wantErr: true}, // empty after collapsing
		{ref: "5", wantErr: true},        // the Element Index form, not a name:/id: reference
		{ref: "@10,20", wantErr: true},   // the coordinate form, click's own
		{ref: "Name:OK", wantErr: true},  // prefixes are case-sensitive
		{ref: "ID:x", wantErr: true},
		{ref: "", wantErr: true},
	}
	for _, tc := range cases {
		kind, text, err := ParseElementRef(tc.ref)
		if tc.wantErr {
			var bad *BadRefError
			if !errors.As(err, &bad) {
				t.Errorf("ParseElementRef(%q) error = %v, want a BadRefError", tc.ref, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseElementRef(%q) unexpected error: %v", tc.ref, err)
			continue
		}
		if kind != tc.wantKind || text != tc.wantText {
			t.Errorf("ParseElementRef(%q) = %d, %q, want %d, %q", tc.ref, kind, text, tc.wantKind, tc.wantText)
		}
	}
}

func TestResolveElementRefUniqueMatches(t *testing.T) {
	els := refElements()
	cases := []struct {
		ref       string
		wantIndex int
	}{
		{"name:Example", 0},
		{"name:Search", 1},
		{"name:Solo One", 4},
		{"name:Notify me", 7},
		{"id:w", 0},
		{"id:searchBox", 1},
		{"id:twin-a", 2},
		{"id:unnamed", 6},
	}
	for _, tc := range cases {
		index, err := ResolveElementRef(els, tc.ref)
		if err != nil {
			t.Errorf("ResolveElementRef(%q): %v", tc.ref, err)
			continue
		}
		if index != tc.wantIndex {
			t.Errorf("ResolveElementRef(%q) = %d, want %d", tc.ref, index, tc.wantIndex)
		}
	}
}

func TestResolveElementRefWhitespaceCollapsesBothSides(t *testing.T) {
	// The tree prints collapsed names, and an agent may copy either the
	// collapsed form or the raw one with odd spacing: both collapse to the
	// same text, so both resolve.
	els := []Element{{Depth: 0, Role: "button", Name: "Save   As…", AutomationId: "b"}}
	for _, ref := range []string{"name:Save   As…", "name: Save As… ", "name:Save As…"} {
		if index, err := ResolveElementRef(els, ref); err != nil || index != 0 {
			t.Errorf("ResolveElementRef(%q) = %d, %v, want 0, nil", ref, index, err)
		}
	}
}

func TestResolveElementRefAmbiguousListsCandidates(t *testing.T) {
	els := refElements()
	index, err := ResolveElementRef(els, "name:Twin")
	if err == nil {
		t.Fatalf("an ambiguous name resolved to %d; it must never guess", index)
	}
	var amb *AmbiguousRefError
	if !errors.As(err, &amb) {
		t.Fatalf("error = %v (%T), want an AmbiguousRefError", err, err)
	}
	if amb.Error() != `name "Twin" matches 2 elements` {
		t.Errorf("message = %q, want `name \"Twin\" matches 2 elements`", amb.Error())
	}
	if len(amb.Matches) != 2 || amb.Matches[0] != 2 || amb.Matches[1] != 3 {
		t.Errorf("matches = %v, want [2 3]", amb.Matches)
	}
	want := []string{
		"2 button Twin ID: twin-a",
		"3 button Twin ID: twin-b",
	}
	if len(amb.Candidates) != len(want) {
		t.Fatalf("candidates = %q, want %q", amb.Candidates, want)
	}
	for i, line := range want {
		if amb.Candidates[i] != line {
			t.Errorf("candidate %d = %q, want %q", i, amb.Candidates[i], line)
		}
	}

	// The shared-AutomationId pair is ambiguous through id:, with each
	// candidate's own name shown.
	index, err = ResolveElementRef(els, "id:solo")
	if index != 0 || err == nil {
		t.Fatalf("an ambiguous id resolved to %d; it must never guess", index)
	}
	if !errors.As(err, &amb) {
		t.Fatalf("error = %v (%T), want an AmbiguousRefError", err, err)
	}
	if amb.Error() != `ID "solo" matches 2 elements` {
		t.Errorf("message = %q", amb.Error())
	}
	if len(amb.Candidates) != 2 ||
		amb.Candidates[0] != "4 button Solo One ID: solo" ||
		amb.Candidates[1] != "5 button Solo Two ID: solo" {
		t.Errorf("candidates = %q", amb.Candidates)
	}
}

// labelledControls is a desktop-shaped tree whose controls carry their
// own label as a child, as many Apps' accessibility trees do.
func labelledControls() []Element {
	return []Element{
		{Depth: 0, Role: "window", Name: "Example"},
		{Depth: 1, Role: "button", Name: "Open Item"},
		{Depth: 2, Role: "text", Name: "Open Item"},
		{Depth: 1, Role: "tab", Name: "Notes", Selectable: true},
		{Depth: 2, Role: "group"},
		{Depth: 3, Role: "text", Name: "Notes"},
		{Depth: 1, Role: "button", Name: "Next", Disabled: true},
		{Depth: 2, Role: "image", Name: "Next"},
	}
}

// TestNameResolvesAControlOverItsOwnLabel: a name: reference matching an
// Element an agent acts on and one non-interactive Element inside it with
// the same name names one control, its label included, so it resolves to
// the control.
func TestNameResolvesAControlOverItsOwnLabel(t *testing.T) {
	els := labelledControls()
	for _, tc := range []struct {
		ref  string
		want int
	}{
		{"name:Open Item", 1}, // the label is the control's child
		{"name:Notes", 3},     // or deeper inside it
		{"name:Next", 6},      // a disabled control is still the one acted on
	} {
		if i, err := ResolveElementRef(els, tc.ref); err != nil || i != tc.want {
			t.Errorf("ResolveElementRef(%q) = %d, %v, want %d, nil", tc.ref, i, err, tc.want)
		}
	}
}

// TestNameResolvesAPageControlOverItsOwnLabel: the same rule on a Page's
// Elements, where a control's text sits under a wrapper the Page reader
// keeps (its own direct text it already leaves out).
func TestNameResolvesAPageControlOverItsOwnLabel(t *testing.T) {
	els := []Element{
		{Depth: 0, Role: "document", Name: "Shop"},
		{Depth: 1, Role: "link", Name: "Pricing", URL: "/pricing"},
		{Depth: 2, Role: "group"},
		{Depth: 3, Role: "text", Name: "Pricing"},
		{Depth: 1, Role: "list item", Name: "Cart"},
		{Depth: 2, Role: "image", Name: "Cart"},
		{Depth: 1, Role: "heading", Name: "Pricing plans", Level: 2},
		{Depth: 1, Role: "text", Name: "Pricing plans"},
	}
	for _, tc := range []struct {
		ref  string
		want int
	}{
		{"name:Pricing", 1},
		{"name:Cart", 4},
	} {
		if i, err := ResolveElementRef(els, tc.ref); err != nil || i != tc.want {
			t.Errorf("ResolveElementRef(%q) = %d, %v, want %d, nil", tc.ref, i, err, tc.want)
		}
	}
	// A heading is not acted on, and the text beside it is not inside it.
	var amb *AmbiguousRefError
	if _, err := ResolveElementRef(els, "name:Pricing plans"); !errors.As(err, &amb) || !slices.Equal(amb.Matches, []int{6, 7}) {
		t.Errorf("name:Pricing plans: %v, want ambiguous among 6 and 7", err)
	}
}

// TestNameResolvesAPageClickableOverItsOwnLabel: a Page Element marked
// clickable (a div with a click handler) is the control a name: reference
// means when its own text sits inside it, as a button would be
// (recap-trial 03). Two clickables, or a clickable and a control inside
// it, stay ambiguous.
func TestNameResolvesAPageClickableOverItsOwnLabel(t *testing.T) {
	els := []Element{
		{Depth: 0, Role: "document", Name: "Shop"},
		{Depth: 1, Role: "clickable", Name: "Add to basket"},
		{Depth: 2, Role: "group"},
		{Depth: 3, Role: "text", Name: "Add to basket"},
		{Depth: 1, Role: "clickable", Name: "Gift wrap"},
		{Depth: 2, Role: "image", Name: "Gift wrap"},
		{Depth: 1, Role: "clickable", Name: "Size"},
		{Depth: 2, Role: "clickable", Name: "Size"},
		{Depth: 1, Role: "clickable", Name: "Colour"},
		{Depth: 1, Role: "clickable", Name: "Colour"},
		{Depth: 1, Role: "clickable", Name: "Help"},
		{Depth: 2, Role: "link", Name: "Help", URL: "/help"},
		{Depth: 1, Role: "clickable", Name: "Reviews"},
		{Depth: 1, Role: "text", Name: "Reviews"},
	}
	for _, tc := range []struct {
		ref  string
		want int
	}{
		{"name:Add to basket", 1},
		{"name:Gift wrap", 4},
	} {
		if i, err := ResolveElementRef(els, tc.ref); err != nil || i != tc.want {
			t.Errorf("ResolveElementRef(%q) = %d, %v, want %d, nil", tc.ref, i, err, tc.want)
		}
	}
	for _, tc := range []struct {
		ref  string
		want []int
	}{
		{"name:Size", []int{6, 7}},      // a clickable inside a clickable
		{"name:Colour", []int{8, 9}},    // two clickables
		{"name:Help", []int{10, 11}},    // a link inside the clickable
		{"name:Reviews", []int{12, 13}}, // a text beside it
	} {
		var amb *AmbiguousRefError
		if i, err := ResolveElementRef(els, tc.ref); !errors.As(err, &amb) || !slices.Equal(amb.Matches, tc.want) {
			t.Errorf("ResolveElementRef(%q) = %d, %v, want ambiguous among %v", tc.ref, i, err, tc.want)
		}
	}
	// The rule is name: resolution's alone: a clickable is still not
	// ActedOn, so the --interactive views are unchanged.
	if els[1].ActedOn() || els[1].Interactive() {
		t.Errorf("a clickable reads as ActedOn/Interactive; the rule must stay inside name: resolution")
	}
}

// TestNameStaysAmbiguousUnlessTheLabelIsTheControlsOwn: exact, never a
// guess. Only a control and a non-interactive Element inside it collapse;
// every other pair, and three matches, fail with the candidates.
func TestNameStaysAmbiguousUnlessTheLabelIsTheControlsOwn(t *testing.T) {
	cases := []struct {
		name string
		els  []Element
		ref  string
		want []int
	}{
		{"a text beside the control", []Element{
			{Depth: 0, Role: "window", Name: "w"},
			{Depth: 1, Role: "button", Name: "Save"},
			{Depth: 1, Role: "text", Name: "Save"},
		}, "name:Save", []int{1, 2}},
		{"a text after the control's subtree", []Element{
			{Depth: 0, Role: "window", Name: "w"},
			{Depth: 1, Role: "group"},
			{Depth: 2, Role: "button", Name: "Save"},
			{Depth: 3, Role: "image"},
			{Depth: 2, Role: "text", Name: "Save"},
		}, "name:Save", []int{2, 4}},
		{"two buttons, one inside the other", []Element{
			{Depth: 0, Role: "window", Name: "w"},
			{Depth: 1, Role: "button", Name: "Save"},
			{Depth: 2, Role: "button", Name: "Save"},
		}, "name:Save", []int{1, 2}},
		{"two sibling buttons", []Element{
			{Depth: 0, Role: "window", Name: "w"},
			{Depth: 1, Role: "button", Name: "Save"},
			{Depth: 1, Role: "button", Name: "Save"},
		}, "name:Save", []int{1, 2}},
		{"a link inside the control", []Element{
			{Depth: 0, Role: "window", Name: "w"},
			{Depth: 1, Role: "list item", Name: "Docs"},
			{Depth: 2, Role: "link", Name: "Docs"},
		}, "name:Docs", []int{1, 2}},
		{"a text with a Secondary Action inside the control", []Element{
			{Depth: 0, Role: "window", Name: "w"},
			{Depth: 1, Role: "button", Name: "More"},
			{Depth: 2, Role: "text", Name: "More", SecondaryActions: []string{"Expand"}},
		}, "name:More", []int{1, 2}},
		{"a label inside a non-interactive Element", []Element{
			{Depth: 0, Role: "window", Name: "w"},
			{Depth: 1, Role: "group", Name: "Totals"},
			{Depth: 2, Role: "text", Name: "Totals"},
		}, "name:Totals", []int{1, 2}},
		{"the control and two labels", []Element{
			{Depth: 0, Role: "window", Name: "w"},
			{Depth: 1, Role: "button", Name: "Go"},
			{Depth: 2, Role: "text", Name: "Go"},
			{Depth: 2, Role: "text", Name: "Go"},
		}, "name:Go", []int{1, 2, 3}},
		{"id: is never collapsed", []Element{
			{Depth: 0, Role: "window", Name: "w"},
			{Depth: 1, Role: "button", Name: "Go", AutomationId: "go"},
			{Depth: 2, Role: "text", Name: "Go", AutomationId: "go"},
		}, "id:go", []int{1, 2}},
	}
	for _, tc := range cases {
		i, err := ResolveElementRef(tc.els, tc.ref)
		var amb *AmbiguousRefError
		if !errors.As(err, &amb) || !slices.Equal(amb.Matches, tc.want) || len(amb.Candidates) != len(tc.want) {
			t.Errorf("%s: ResolveElementRef(%q) = %d, %v, want ambiguous among %v", tc.name, tc.ref, i, err, tc.want)
		}
	}
}

func TestResolveElementRefCandidateLinesUseTheTreeGrammar(t *testing.T) {
	// Candidate lines are the Elements' tree lines (states, value, toggle,
	// Secondary Actions, ID:), so the agent sees exactly what the Window
	// State printed — just without the indentation.
	els := []Element{
		{Depth: 0, Role: "window", Name: "w", AutomationId: "w"},
		{Depth: 1, Role: "edit", Name: "Same", AutomationId: "a", Settable: "string", Value: "typed"},
		{Depth: 2, Role: "check box", Name: "Same", AutomationId: "b", Toggle: "on",
			SecondaryActions: []string{"Toggle"}, Disabled: true},
	}
	_, err := ResolveElementRef(els, "name:Same")
	var amb *AmbiguousRefError
	if !errors.As(err, &amb) {
		t.Fatalf("error = %v, want an AmbiguousRefError", err)
	}
	want := []string{
		"1 edit (settable, string) Same Value: typed ID: a",
		"2 check box (disabled) Same on Secondary Actions: Toggle ID: b",
	}
	for i, line := range want {
		if amb.Candidates[i] != line {
			t.Errorf("candidate %d = %q, want %q", i, amb.Candidates[i], line)
		}
	}
}

func TestResolveElementRefMissingFailsWithTheColumn(t *testing.T) {
	els := refElements()
	cases := []struct {
		ref      string
		wantText string
	}{
		{"name:OK", `no element named "OK"`},
		{"id:nope", `no element with ID "nope"`},
	}
	for _, tc := range cases {
		index, err := ResolveElementRef(els, tc.ref)
		if index != 0 || err == nil {
			t.Fatalf("ResolveElementRef(%q) = %d, %v, want a failure", tc.ref, index, err)
		}
		var miss *NoMatchRefError
		if !errors.As(err, &miss) {
			t.Fatalf("error = %v (%T), want a NoMatchRefError", err, err)
		}
		if miss.Error() != tc.wantText {
			t.Errorf("message = %q, want %q", miss.Error(), tc.wantText)
		}
	}
}

func TestResolveElementRefExactMatchSemantics(t *testing.T) {
	els := refElements()
	// Matching is case-sensitive and exact after collapsing: a reference
	// that differs only in case or detail misses rather than guessing.
	for _, ref := range []string{"name:search", "name:SEARCH", "id:SearchBox", "name:Twins", "id:twin"} {
		if _, err := ResolveElementRef(els, ref); err == nil {
			t.Errorf("ResolveElementRef(%q) succeeded; only exact matches resolve", ref)
		}
	}
	// name: matches only the Name column, id: only the AutomationId
	// column — a Name never satisfies id: and an AutomationId never
	// satisfies name:.
	for _, ref := range []string{"name:searchBox", "id:Search"} {
		if _, err := ResolveElementRef(els, ref); err == nil {
			t.Errorf("ResolveElementRef(%q) succeeded; the forms match their own column only", ref)
		}
	}
	// An Element with an empty Name is not reachable through name: (the
	// empty reference is refused), but its AutomationId still resolves.
	if index, err := ResolveElementRef(els, "id:unnamed"); err != nil || index != 6 {
		t.Errorf("ResolveElementRef(id:unnamed) = %d, %v, want 6, nil", index, err)
	}
}

func TestResolveElementRefEmptyStateMisses(t *testing.T) {
	index, err := ResolveElementRef(nil, "name:OK")
	if index != 0 || err == nil {
		t.Fatalf("resolving against no Elements = %d, %v, want a miss", index, err)
	}
	var miss *NoMatchRefError
	if !errors.As(err, &miss) {
		t.Fatalf("error = %v, want a NoMatchRefError", err)
	}
}

func TestLineMatchesTheTreesOwnRendering(t *testing.T) {
	// Line is the tree's own element line without the TABs, so candidate
	// lists never diverge from what the Window State printed.
	tree := &Tree{
		Title: "t",
		App:   "a.exe",
		Elements: []Element{
			{Depth: 0, Role: "window", Name: "t", AutomationId: "w"},
			{Depth: 1, Role: "button", Name: "OK", AutomationId: "ok"},
		},
		FocusedIndex: -1,
	}
	rendered := Render(tree)
	if !strings.Contains(rendered, "\t\t1 button OK ID: ok") {
		t.Fatalf("tree rendering missing the element line:\n%s", rendered)
	}
	if got, want := Line(1, &tree.Elements[1]), "1 button OK ID: ok"; got != want {
		t.Errorf("Line(1) = %q, want %q", got, want)
	}
}

// TestInPartialRead: in a partial Window State (reliability council 03)
// one match is refused with its line, none is incomplete, several stay
// ambiguous.
func TestInPartialRead(t *testing.T) {
	els := refElements()
	i, err := ResolveElementRef(els, "name:Example")
	_, err = InPartialRead(els, "name:Example", i, err)
	var one *PartialRefError
	if !errors.As(err, &one) || one.Match != 0 || one.Candidate != Line(0, &els[0]) ||
		err.Error() != `name "Example": 1 match in the part read; the read was partial` {
		t.Errorf("one match: %v %+v", err, one)
	}
	i, err = ResolveElementRef(els, "id:nope")
	_, err = InPartialRead(els, "id:nope", i, err)
	var none *NoMatchRefError
	if !errors.As(err, &none) || !none.Partial || err.Error() != `no element with ID "nope" in the part read; the read was partial` {
		t.Errorf("no match: %v", err)
	}
	i, err = ResolveElementRef(els, "name:Twin")
	_, err = InPartialRead(els, "name:Twin", i, err)
	var several *AmbiguousRefError
	if !errors.As(err, &several) {
		t.Errorf("several: %v", err)
	}
}
