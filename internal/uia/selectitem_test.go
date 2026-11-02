package uia

// Select by name's offline tests (desktop-parity 12): selectByName over a
// fake combo or list, which records the live operations it is driven
// through. The Expand, Select, Collapse, FindItemByProperty and Realize
// calls themselves run on real Windows only; the opt-in e2e suite covers
// them on the fixture's list and the Classic Dialog's combo.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeList is a combo or list for selectByName: its items, its
// ExpandCollapse state (-1: none) and what its live calls return.
type fakeList struct {
	names       []string
	state       int32
	hidden      map[string]int // ItemContainer: names past the bounded search, and how many carry each
	expandErr   error
	collapseErr error
	itemsErr    error
	pickErr     error
	staysOpen   bool // a Select does not close the drop-down
	ignores     bool // the Select is delivered but the selection does not move
	selectedNow string
	calls       []string
}

func (f *fakeList) steps() selectSteps {
	return selectSteps{
		expandState: func() int32 { return f.state },
		expand: func() error {
			f.calls = append(f.calls, "expand")
			if f.expandErr != nil {
				return f.expandErr
			}
			f.state = expandExpanded
			return nil
		},
		collapse: func() error {
			f.calls = append(f.calls, "collapse")
			if f.collapseErr != nil {
				return f.collapseErr
			}
			f.state = expandCollapsed
			return nil
		},
		items: func() ([]string, error) {
			f.calls = append(f.calls, "items")
			return f.names, f.itemsErr
		},
		findHidden: func(label string) (int, string, error) {
			f.calls = append(f.calls, "find")
			n := f.hidden[label]
			if n == 1 {
				return 1, label, nil
			}
			return n, "", nil
		},
		pick: func(i int) error {
			f.calls = append(f.calls, fmt.Sprintf("pick %d", i))
			if f.pickErr != nil {
				return f.pickErr
			}
			if !f.ignores {
				if i >= 0 {
					f.selectedNow = f.names[i]
				} else {
					for name := range f.hidden {
						f.selectedNow = name
					}
				}
			}
			if f.state == expandExpanded && !f.staysOpen {
				f.state = expandCollapsed // a combo closes its drop-down on a pick
			}
			return nil
		},
		selected: func(name string) (bool, string) {
			f.calls = append(f.calls, "read "+name)
			if f.selectedNow == "" {
				return false, "nothing"
			}
			return f.selectedNow == name, fmt.Sprintf("%q", f.selectedNow)
		},
	}
}

func TestSelectByName(t *testing.T) {
	colors := []string{"Red", "Green", "Blue"}
	write := &WriteError{What: "selecting the item", HR: -0x7fffbffb}
	cases := []struct {
		name      string
		label     string
		list      fakeList
		wantErr   string // "" for success; otherwise the whole message
		wantCalls string
	}{
		{
			name: "a collapsed combo opens, picks, and reads back", label: "Green",
			list:      fakeList{names: colors, state: expandCollapsed},
			wantCalls: "expand items pick 1 read Green",
		},
		{
			name: "a combo that stays open after the pick is collapsed", label: "Green",
			list:      fakeList{names: colors, state: expandCollapsed, staysOpen: true},
			wantCalls: "expand items pick 1 collapse read Green",
		},
		{
			name: "an open combo is not expanded or collapsed", label: "Blue",
			list:      fakeList{names: colors, state: expandExpanded, staysOpen: true},
			wantCalls: "items pick 2 read Blue",
		},
		{
			name: "a list has no ExpandCollapse", label: "Red",
			list:      fakeList{names: colors, state: -1},
			wantCalls: "items pick 0 read Red",
		},
		{
			name: "case and spacing are ignored when nothing matches exactly", label: "  green ",
			list:      fakeList{names: colors, state: -1},
			wantCalls: "items find pick 1 read Green",
		},
		{
			name: "an exact name wins over one that differs in case", label: "blue",
			list:      fakeList{names: []string{"Blue", "blue"}, state: -1},
			wantCalls: "items pick 1 read blue",
		},
		{
			name: "a virtualized list's unbuilt item is found through ItemContainer", label: "Item 900",
			list:      fakeList{names: []string{"Item 1", "Item 2"}, state: -1, hidden: map[string]int{"Item 900": 1}},
			wantCalls: "items find pick -1 read Item 900",
		},
		{
			name: "ItemContainer's exact match wins over a case-insensitive one", label: "item 2",
			list:      fakeList{names: []string{"Item 1", "Item 2"}, state: -1, hidden: map[string]int{"item 2": 1}},
			wantCalls: "items find pick -1 read item 2",
		},
		{
			name: "two exact matches are ambiguous; nothing was opened", label: "Blue",
			list:      fakeList{names: []string{"Blue", "Red", "Blue"}, state: -1},
			wantErr:   `2 items match "Blue": "Blue", "Blue"`,
			wantCalls: "items",
		},
		{
			name: "two case-insensitive matches are ambiguous, and the combo closes again", label: "BLUE",
			list:      fakeList{names: []string{"Blue", "blue"}, state: expandCollapsed},
			wantErr:   `the list was opened and closed; 2 items match "BLUE": "Blue", "blue"`,
			wantCalls: "expand items find collapse",
		},
		{
			name: "several hidden items carry the name", label: "Item 900",
			list:      fakeList{names: []string{"Item 1"}, state: -1, hidden: map[string]int{"Item 900": 2}},
			wantErr:   `2 items match "Item 900": "Item 900", "Item 900"`,
			wantCalls: "items find",
		},
		{
			name: "no match in a list sends nothing", label: "Purple",
			list:      fakeList{names: colors, state: -1},
			wantErr:   `no item named "Purple"; its items are "Red", "Green", "Blue"`,
			wantCalls: "items find",
		},
		{
			name: "no match in a combo opens and closes it", label: "Purple",
			list:      fakeList{names: colors, state: expandCollapsed},
			wantErr:   `the list was opened and closed; no item named "Purple"; its items are "Red", "Green", "Blue"`,
			wantCalls: "expand items find collapse",
		},
		{
			name: "a combo that will not close again says so", label: "Purple",
			list:      fakeList{names: colors, state: expandCollapsed, collapseErr: errors.New("no")},
			wantErr:   `the list was opened and could not be closed again; no item named "Purple"; its items are "Red", "Green", "Blue"`,
			wantCalls: "expand items find collapse",
		},
		{
			name: "an empty list", label: "Red",
			list:      fakeList{state: -1},
			wantErr:   `no item named "Red", and the list shows no items`,
			wantCalls: "items find",
		},
		{
			name: "a failed Expand stops before the search", label: "Red",
			list:      fakeList{names: colors, state: expandCollapsed, expandErr: ErrDisabled},
			wantErr:   "the Element is disabled",
			wantCalls: "expand",
		},
		{
			name: "a failed search after the Expand closes the combo again", label: "Red",
			list:      fakeList{names: colors, state: expandCollapsed, itemsErr: fmt.Errorf("%w: gone", ErrStale)},
			wantErr:   "the list was opened and closed; " + ErrStale.Error() + ": gone",
			wantCalls: "expand items collapse",
		},
		{
			name: "a failed Select may have taken effect", label: "Red",
			list:      fakeList{names: colors, state: expandCollapsed, staysOpen: true, pickErr: write},
			wantErr:   write.Error(),
			wantCalls: "expand items pick 0 collapse",
		},
		{
			name: "a Select the list ignores fails the read-back", label: "Red",
			list:      fakeList{names: colors, state: -1, ignores: true, selectedNow: "Blue"},
			wantErr:   `set-value selected "Red" but the list shows "Blue" selected afterwards`,
			wantCalls: "items pick 0 read Red",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list := tc.list
			err := selectByName(tc.label, list.steps())
			if got := strings.Join(list.calls, " "); got != tc.wantCalls {
				t.Errorf("calls = %q, want %q", got, tc.wantCalls)
			}
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("error = %v, want success", err)
			case tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr):
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestSelectItemErrorClassifies(t *testing.T) {
	var sel *SelectItemError

	err := selectByName("Purple", (&fakeList{names: []string{"Red"}, state: -1}).steps())
	if !errors.Is(err, ErrNoSuchItem) || !errors.As(err, &sel) || sel.Opened {
		t.Errorf("no match = %#v, want ErrNoSuchItem, not opened", err)
	}

	err = selectByName("red", (&fakeList{names: []string{"Red", "RED"}, state: expandCollapsed}).steps())
	if !errors.Is(err, ErrAmbiguousItem) || !errors.As(err, &sel) || !sel.Opened {
		t.Errorf("ambiguous = %#v, want ErrAmbiguousItem, opened", err)
	}

	// The search's own failure stays recognizable through the wrapper.
	err = selectByName("Red", (&fakeList{state: expandCollapsed, itemsErr: fmt.Errorf("%w: gone", ErrStale)}).steps())
	if !errors.Is(err, ErrStale) || !errors.As(err, &sel) || !sel.Opened {
		t.Errorf("stale search = %#v, want ErrStale, opened", err)
	}

	// The read-back failure is a VerifyError: the Select was delivered.
	err = selectByName("Red", (&fakeList{names: []string{"Red"}, state: -1, ignores: true}).steps())
	var verify *VerifyError
	if !errors.As(err, &verify) || err.Error() != `set-value selected "Red" but the list shows nothing selected afterwards` {
		t.Errorf("ignored select = %v, want a VerifyError", err)
	}
}

func TestSelectItemErrorListsAtMostTwelveItems(t *testing.T) {
	names := make([]string, 15)
	for i := range names {
		names[i] = fmt.Sprintf("Item %d", i+1)
	}
	err := &SelectItemError{Label: "x", Items: names}
	if !strings.HasSuffix(err.Error(), `"Item 12", and 3 more`) {
		t.Errorf("message = %q", err.Error())
	}
}
