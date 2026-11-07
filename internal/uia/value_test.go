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
			name: "a window root with everything, in the fixed order (ContextMenu is never listed)",
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
		"Expand", "Collapse", "Toggle", "Select", "ContextMenu",
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
		"Raise, Scroll Up, Scroll Down, Scroll Left, Scroll Right, Expand, Collapse, Toggle, Select, or ContextMenu"; got != want {
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
		"contextmenu":   "ContextMenu",
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

// TestWalkWithholdsPasswordValues checks IsPassword on the walk
// (reliability-council 07): true withholds the Value, false keeps it, and
// an unreadable answer withholds on an Edit only. Role, name and index
// stay; the line says "password"; the secret reaches neither the stored
// Element, the rendered Window State nor the diff.
func TestWalkWithholdsPasswordValues(t *testing.T) {
	src := newFakeSource(&fakeNode{data: NodeData{
		ControlType: wstate.ControlTypeWindow, Name: "Sign in", Enabled: true,
	}, kids: []*fakeNode{
		{data: NodeData{ControlType: wstate.ControlTypeEdit, Name: "Password", Enabled: true,
			IsPassword: true, HasValue: true, ValueValue: "hunter2"}},
		{data: NodeData{ControlType: wstate.ControlTypeEdit, Name: "User", Enabled: true,
			HasValue: true, ValueValue: "alice"}},
		{data: NodeData{ControlType: wstate.ControlTypeEdit, Name: "PIN", Enabled: true,
			PasswordUnread: true, HasValue: true, ValueValue: "4242"}},
		{data: NodeData{ControlType: wstate.ControlTypeComboBox, Name: "Domain", Enabled: true,
			PasswordUnread: true, HasValue: true, ValueReadOnly: true, ValueValue: "corp"}},
	}})
	res, err := Walk[*fakeNode](src, fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	tree := &wstate.Tree{Title: "Sign in", App: "a.exe", Elements: res.Elements, FocusedIndex: -1}
	want := "Window: \"Sign in\", App: a.exe.\n" +
		"\t0 window Sign in\n" +
		"\t\t1 edit (settable, string, password) Password\n" +
		"\t\t2 edit (settable, string) User Value: alice\n" +
		"\t\t3 edit (settable, string, password) PIN\n" +
		"\t\t4 combo box Domain Value: corp\n"
	if got := wstate.Render(tree); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
	for _, i := range []int{1, 3} {
		if e := res.Elements[i]; e.Value != "" || !e.Password || e.Role != "edit" || e.Name == "" {
			t.Errorf("element %d = %+v, want a named edit with Password and no Value", i, e)
		}
	}
	if e := res.Elements[2]; e.Password || e.Value != "alice" {
		t.Errorf("element 2 = %+v, want its Value and no Password", e)
	}

	// The secret changes between reads: the diff sees no change.
	src2 := newFakeSource(&fakeNode{data: NodeData{
		ControlType: wstate.ControlTypeWindow, Name: "Sign in", Enabled: true,
	}, kids: []*fakeNode{
		{data: NodeData{ControlType: wstate.ControlTypeEdit, Name: "Password", Enabled: true,
			IsPassword: true, HasValue: true, ValueValue: "hunter3"}},
	}})
	after, err := Walk[*fakeNode](src2, fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if changes := wstate.Diff(res.Elements[:2], after.Elements); len(changes) != 0 {
		t.Errorf("diff = %v, want none", changes)
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
	c.store.install(defaultAgent, 101, c.store.beginRead(), []comPtr{comPtr(&a), comPtr(&b)}, nil)
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
	c.store.install(defaultAgent, 101, c.store.beginRead(), []comPtr{comPtr(&a)}, nil)
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

func TestParseToggleValue(t *testing.T) {
	for value, want := range map[string]int32{
		"on": toggleOn, "true": toggleOn, "checked": toggleOn,
		"off": toggleOff, "false": toggleOff, "unchecked": toggleOff,
		"ON": toggleOn, "Checked": toggleOn, " off ": toggleOff, "FALSE": toggleOff,
	} {
		got, err := parseToggleValue(value)
		if err != nil || got != want {
			t.Errorf("parseToggleValue(%q) = %d, %v; want %d, nil", value, got, err, want)
		}
	}
	for _, value := range []string{"", "yes", "no", "1", "0", "indeterminate", "toggle", "on!"} {
		_, err := parseToggleValue(value)
		if !errors.Is(err, ErrToggleValue) {
			t.Errorf("parseToggleValue(%q) error = %v, want ErrToggleValue", value, err)
			continue
		}
		// The refusal names every accepted word.
		for _, word := range []string{"on", "off", "true", "false", "checked", "unchecked"} {
			if !strings.Contains(err.Error(), word) {
				t.Errorf("parseToggleValue(%q) error = %q, want it to name %q", value, err, word)
			}
		}
	}
}

// fakeBox is a toggle Element offline: each Toggle call moves it one step
// along cycle, unless it ignores Toggle; settle reads the state back.
type fakeBox struct {
	state   int32
	cycle   []int32 // the states Toggle steps through, in order
	ignore  bool    // the app ignores the Toggle call
	failAt  int     // the 1-based Toggle call that fails (0: none)
	readsAs int32   // when not 0, settle reads this instead (an unreadable state: -1)
	sent    int
}

var errToggleCall = errors.New("toggle call failed")

func (b *fakeBox) steps() toggleSteps {
	return toggleSteps{
		toggle: func(sent int) error {
			if sent != b.sent {
				panic(fmt.Sprintf("toggle told %d were sent, the box counted %d", sent, b.sent))
			}
			b.sent++
			if b.sent == b.failAt {
				return errToggleCall
			}
			if b.ignore {
				return nil
			}
			for i, s := range b.cycle {
				if s == b.state {
					b.state = b.cycle[(i+1)%len(b.cycle)]
					break
				}
			}
			return nil
		},
		settle: func(prev int32) int32 {
			if b.readsAs != 0 {
				return b.readsAs
			}
			return b.state
		},
	}
}

func TestToggleTo(t *testing.T) {
	twoState := []int32{toggleOff, toggleOn}
	// Win32 BS_AUTO3STATE and WPF's three-state CheckBox: off, on,
	// indeterminate. UIA's documentation names on, off, indeterminate.
	win32 := []int32{toggleOff, toggleOn, toggleIndeterminate}
	documented := []int32{toggleOn, toggleOff, toggleIndeterminate}
	cases := []struct {
		name     string
		box      fakeBox
		target   int32
		wantErr  error // nil: success; otherwise errors.Is, or any *VerifyError
		wantSent int
		observed string // the VerifyError's Observed
	}{
		{name: "already on: nothing is sent", box: fakeBox{state: toggleOn, cycle: twoState}, target: toggleOn},
		{name: "already off: nothing is sent", box: fakeBox{state: toggleOff, cycle: win32}, target: toggleOff},
		{name: "two-state off to on", box: fakeBox{state: toggleOff, cycle: twoState}, target: toggleOn, wantSent: 1},
		{name: "two-state on to off", box: fakeBox{state: toggleOn, cycle: twoState}, target: toggleOff, wantSent: 1},
		{name: "three-state on to off passes through indeterminate", box: fakeBox{state: toggleOn, cycle: win32}, target: toggleOff, wantSent: 2},
		{name: "three-state indeterminate to on", box: fakeBox{state: toggleIndeterminate, cycle: win32}, target: toggleOn, wantSent: 2},
		{name: "three-state indeterminate to off", box: fakeBox{state: toggleIndeterminate, cycle: win32}, target: toggleOff, wantSent: 1},
		{name: "documented cycle off to on", box: fakeBox{state: toggleOff, cycle: documented}, target: toggleOn, wantSent: 2},
		{name: "documented cycle indeterminate to off", box: fakeBox{state: toggleIndeterminate, cycle: documented}, target: toggleOff, wantSent: 2},
		{
			name: "an ignored toggle is not sent again", box: fakeBox{state: toggleOff, cycle: twoState, ignore: true},
			target: toggleOn, wantErr: &VerifyError{}, wantSent: 1, observed: "off",
		},
		{
			name: "an unreadable read-back fails verification", box: fakeBox{state: toggleOff, cycle: twoState, readsAs: -1},
			target: toggleOn, wantErr: &VerifyError{}, wantSent: 1, observed: "unreadable",
		},
		{
			name: "an unreadable state before refuses", box: fakeBox{state: -1, cycle: twoState},
			target: toggleOn, wantErr: ErrToggleUnreadable,
		},
		{
			name: "a failed first toggle is returned", box: fakeBox{state: toggleOff, cycle: twoState, failAt: 1},
			target: toggleOn, wantErr: errToggleCall, wantSent: 1,
		},
		{
			name: "a failed second toggle is returned", box: fakeBox{state: toggleOn, cycle: win32, failAt: 2},
			target: toggleOff, wantErr: errToggleCall, wantSent: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			box := tc.box
			err := toggleTo(box.state, tc.target, box.steps())
			switch want := tc.wantErr.(type) {
			case nil:
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				if box.state != tc.target {
					t.Errorf("box state = %s, want %s", toggleWord(box.state), toggleWord(tc.target))
				}
			case *VerifyError:
				var verify *VerifyError
				if !errors.As(err, &verify) {
					t.Fatalf("error = %v, want a VerifyError", err)
				}
				if verify.Action != "set-value" || verify.Expected != toggleWord(tc.target) || verify.Observed != tc.observed {
					t.Errorf("VerifyError = %+v, want set-value expecting %s, observing %s", verify, toggleWord(tc.target), tc.observed)
				}
			default:
				if !errors.Is(err, want) {
					t.Fatalf("error = %v, want %v", err, want)
				}
			}
			if box.sent != tc.wantSent {
				t.Errorf("toggles sent = %d, want %d", box.sent, tc.wantSent)
			}
		})
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
		{fmt.Errorf("%w: maybe", ErrToggleValue), "toggle value must be on, off, true, false, checked or unchecked: maybe"},
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
