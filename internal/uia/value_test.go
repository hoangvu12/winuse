package uia

// The ticket-07 offline tests: the Secondary Actions policy (Codex's set
// plus the Toggle/Select extensions, gated like the parity-verified
// reference renderer), the action-name canonicalization, the walk's rich
// state mapping (NodeData → wstate.Element), and the client's Element
// Index lookups for set-value and perform-secondary-action. The COM paths
// (Value.SetValue, the pattern calls) run on real Windows only; the e2e
// suite covers them against the fixture app.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

func TestSecondaryActionsPolicy(t *testing.T) {
	cases := []struct {
		name string
		f    patternFacts
		want []string
	}{
		{
			name: "no patterns, no actions",
			f:    patternFacts{},
			want: nil,
		},
		{
			name: "window pattern offers Raise",
			f:    patternFacts{HasWindow: true},
			want: []string{"Raise"},
		},
		{
			name: "vertical scroll offers up and down",
			f:    patternFacts{HasScroll: true, ScrollV: true},
			want: []string{"Scroll Up", "Scroll Down"},
		},
		{
			name: "horizontal scroll offers left and right only",
			f:    patternFacts{HasScroll: true, ScrollH: true},
			want: []string{"Scroll Left", "Scroll Right"},
		},
		{
			name: "both axes offer all four scroll actions",
			f:    patternFacts{HasScroll: true, ScrollH: true, ScrollV: true},
			want: []string{"Scroll Up", "Scroll Down", "Scroll Left", "Scroll Right"},
		},
		{
			name: "scroll pattern with no scrollability still offers up and down",
			f:    patternFacts{HasScroll: true},
			want: []string{"Scroll Up", "Scroll Down"},
		},
		{
			name: "collapsed offers Expand",
			f:    patternFacts{HasExpand: true, ExpandState: expandCollapsed},
			want: []string{"Expand"},
		},
		{
			name: "expanded offers Collapse",
			f:    patternFacts{HasExpand: true, ExpandState: expandExpanded},
			want: []string{"Collapse"},
		},
		{
			name: "partially expanded offers both",
			f:    patternFacts{HasExpand: true, ExpandState: expandPartiallyExpanded},
			want: []string{"Expand", "Collapse"},
		},
		{
			name: "unreadable expand state offers both",
			f:    patternFacts{HasExpand: true, ExpandState: -1},
			want: []string{"Expand", "Collapse"},
		},
		{
			name: "a leaf node offers neither",
			f:    patternFacts{HasExpand: true, ExpandState: expandLeafNode},
			want: nil,
		},
		{
			name: "toggle and select extensions",
			f:    patternFacts{HasToggle: true, HasSelection: true},
			want: []string{"Toggle", "Select"},
		},
		{
			name: "a window root with everything, in the fixed order",
			f: patternFacts{
				HasWindow: true, HasScroll: true, ScrollH: true, ScrollV: true,
				HasExpand: true, ExpandState: expandPartiallyExpanded,
				HasToggle: true, HasSelection: true,
			},
			want: []string{
				"Raise", "Scroll Up", "Scroll Down", "Scroll Left", "Scroll Right",
				"Expand", "Collapse", "Toggle", "Select",
			},
		},
		{
			name: "expand state without the pattern is ignored (zero-value NodeData)",
			f:    patternFacts{ExpandState: expandCollapsed},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.f.secondaryActions()
			if len(got) != len(tc.want) {
				t.Fatalf("actions = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("actions = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestSecondaryActionNamesCoverTheSurface(t *testing.T) {
	// The wire surface: every action the commands accept must be listed,
	// in the fixed order errors print them.
	want := []string{
		"Raise", "Scroll Up", "Scroll Down", "Scroll Left", "Scroll Right",
		"Expand", "Collapse", "Toggle", "Select",
	}
	if len(SecondaryActionNames) != len(want) {
		t.Fatalf("SecondaryActionNames = %v, want %v", SecondaryActionNames, want)
	}
	for i, name := range want {
		if SecondaryActionNames[i] != name {
			t.Errorf("SecondaryActionNames[%d] = %q, want %q", i, SecondaryActionNames[i], name)
		}
	}
	if got, want := ExpectedSecondaryActions(),
		"Raise, Scroll Up, Scroll Down, Scroll Left, Scroll Right, Expand, Collapse, Toggle, or Select"; got != want {
		t.Errorf("ExpectedSecondaryActions() = %q, want %q", got, want)
	}
}

func TestCanonicalSecondaryAction(t *testing.T) {
	for name, want := range map[string]string{
		"Raise":         "Raise",
		"raise":         "Raise",
		"SCROLL DOWN":   "Scroll Down",
		"scroll  down":  "Scroll Down",
		" scroll down ": "Scroll Down",
		"Expand":        "Expand",
		"toggle":        "Toggle",
		"select":        "Select",
	} {
		got, ok := CanonicalSecondaryAction(name)
		if !ok || got != want {
			t.Errorf("CanonicalSecondaryAction(%q) = %q, %v; want %q, true", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "Fly", "Invoke", "scroll", "Set Value", "expandcollapse"} {
		if got, ok := CanonicalSecondaryAction(name); ok {
			t.Errorf("CanonicalSecondaryAction(%q) = %q, want a refusal", name, got)
		}
	}
}

// TestWalkRichStateAndActions checks the walk's NodeData → Element
// mapping: the rich state tokens and Secondary Actions a live read
// renders, in the renderer's grammar positions.
func TestWalkRichStateAndActions(t *testing.T) {
	src := newFakeSource(&fakeNode{data: NodeData{
		ControlType: wstate.ControlTypeWindow,
		Name:        "Window",
		Enabled:     true,
		HasWindow:   true,
	}, kids: []*fakeNode{
		{data: NodeData{ControlType: wstate.ControlTypeEdit, Name: "Search", Enabled: true,
			HasValue: true, ValueReadOnly: false, ValueValue: "hello"}},
		{data: NodeData{ControlType: wstate.ControlTypeEdit, Name: "Locked", Enabled: true,
			HasValue: true, ValueReadOnly: true, ValueValue: "fixed"}},
		{data: NodeData{ControlType: wstate.ControlTypeCheckBox, Name: "Notify", Enabled: true,
			HasToggle: true, ToggleState: toggleOn}},
		{data: NodeData{ControlType: wstate.ControlTypeListItem, Name: "Bravo", Enabled: true,
			HasSelectionItem: true, Selected: true}},
		{data: NodeData{ControlType: wstate.ControlTypeTreeItem, Name: "Favorites", Enabled: true,
			HasExpand: true, ExpandState: expandExpanded}},
		{data: NodeData{ControlType: wstate.ControlTypeTreeItem, Name: "Leaf", Enabled: true,
			HasExpand: true, ExpandState: expandLeafNode}},
		{data: NodeData{ControlType: wstate.ControlTypeList, Name: "Items", Enabled: true,
			HasScroll: true, ScrollV: true, HasRangeValue: true}},
	}})
	res, err := Walk[*fakeNode](src, fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	want := []string{
		"window Window Secondary Actions: Raise",
		"edit (settable, string) Search Value: hello",
		"edit Locked Value: fixed",
		"check box Notify on Secondary Actions: Toggle",
		"list item (selectable, selected) Bravo Secondary Actions: Select",
		"tree item (expanded) Favorites Secondary Actions: Collapse",
		"tree item Leaf",
		"list (settable, float) Items Secondary Actions: Scroll Up, Scroll Down",
	}
	if len(res.Elements) != len(want) {
		t.Fatalf("got %d elements, want %d", len(res.Elements), len(want))
	}
	for i, w := range want {
		e := res.Elements[i]
		// The renderer owns the exact line; reconstruct the relevant
		// pieces to keep this test about the walk's mapping, not the
		// renderer's spacing.
		var parts []string
		parts = append(parts, e.Role)
		if states := statesOf(e); states != "" {
			parts = append(parts, "("+states+")")
		}
		parts = append(parts, e.Name)
		if e.Value != "" {
			parts = append(parts, "Value: "+e.Value)
		}
		if e.Toggle != "" {
			parts = append(parts, e.Toggle)
		}
		if len(e.SecondaryActions) > 0 {
			parts = append(parts, "Secondary Actions: "+strings.Join(e.SecondaryActions, ", "))
		}
		if got := strings.Join(parts, " "); got != w {
			t.Errorf("element %d: got %q, want %q", i, got, w)
		}
	}
}

// statesOf mirrors wstate's parenthesised state list for the walk test.
func statesOf(e wstate.Element) string {
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

func TestSetValueLookupPaths(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	// No cached state for the Window: ErrNoState.
	if err := c.SetValue(context.Background(), 101, 0, "x"); !errors.Is(err, ErrNoState) {
		t.Fatalf("error = %v, want ErrNoState", err)
	}
	// Fake element pointers seeded through the real beginRead/install path.
	a, b := 1, 2
	c.store.install(101, c.store.beginRead(), []comPtr{comPtr(&a), comPtr(&b)}, nil)
	for _, index := range []int{2, 100, -1} {
		if err := c.SetValue(context.Background(), 101, index, "x"); !errors.Is(err, ErrStale) {
			t.Fatalf("index %d: error = %v, want ErrStale", index, err)
		}
	}
	// In-range indexes resolve without error (fake workers carry no COM
	// thread, so no value is written).
	if err := c.SetValue(context.Background(), 101, 1, "hello"); err != nil {
		t.Fatalf("index 1: error = %v, want nil (the fake worker skips COM)", err)
	}
	// A different Window's state does not resolve either.
	if err := c.SetValue(context.Background(), 202, 0, "x"); !errors.Is(err, ErrNoState) {
		t.Fatalf("window 202: error = %v, want ErrNoState", err)
	}
}

func TestPerformSecondaryActionLookupPaths(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	if err := c.PerformSecondaryAction(context.Background(), 101, 0, "Expand"); !errors.Is(err, ErrNoState) {
		t.Fatalf("error = %v, want ErrNoState", err)
	}
	a := 1
	c.store.install(101, c.store.beginRead(), []comPtr{comPtr(&a)}, nil)
	if err := c.PerformSecondaryAction(context.Background(), 101, 9, "Expand"); !errors.Is(err, ErrStale) {
		t.Fatalf("error = %v, want ErrStale", err)
	}
	if err := c.PerformSecondaryAction(context.Background(), 101, 0, "Raise"); err != nil {
		t.Fatalf("error = %v, want nil (the fake worker skips COM)", err)
	}
}

func TestPerformSecondaryActionSurfacesContextCancellation(t *testing.T) {
	c := testClient(func() (*worker, error) { return hangWorker(), nil })
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := c.PerformSecondaryAction(ctx, 101, 0, "Expand"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the context error", err)
	}
}

func TestUnavailableActionErrorFormat(t *testing.T) {
	e := &UnavailableActionError{Action: "Expand", Index: 6, Offered: []string{"Toggle"}}
	want := "secondary action 'Expand' is not available for element 6; available actions: Toggle"
	if e.Error() != want {
		t.Errorf("message = %q, want %q", e.Error(), want)
	}
	// An Element offering nothing says so explicitly, not with an empty list.
	e = &UnavailableActionError{Action: "Toggle", Index: 2}
	want = "secondary action 'Toggle' is not available for element 2; available actions: none"
	if e.Error() != want {
		t.Errorf("message = %q, want %q", e.Error(), want)
	}
}

// TestActionErrorSentinels pins the messages the helper maps hints onto:
// they are Codex's texts and stay stable for the output contract.
func TestActionErrorSentinels(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrNotSettable, "Cannot set a value for an element that is not settable"},
		{ErrFocusBeforeValue, "focus element before setting value"},
		{fmt.Errorf("%w: 1.2.3", ErrRangeValue), "range value must be a number: 1.2.3"},
		{ErrDisabled, "the Element is disabled"},
	}
	for _, tc := range cases {
		if tc.err.Error() != tc.want {
			t.Errorf("message = %q, want %q", tc.err.Error(), tc.want)
		}
	}
	// Wrapped sentinels stay recognizable through the helper's mapping.
	if !errors.Is(fmt.Errorf("wrap: %w", ErrNotSettable), ErrNotSettable) {
		t.Error("wrapped ErrNotSettable must stay recognizable")
	}
}
