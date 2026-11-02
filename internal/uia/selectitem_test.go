package uia

// Select by name's offline tests (desktop-parity 12): selectByName over a
// fake combo or list, which records the live operations it is driven
// through. A combo whose items show while collapsed is never expanded;
// one that builds them on Expand is expanded and collapsed again. The
// Expand, Select, Collapse, FindItemByProperty and Realize calls
// themselves run on real Windows only; the opt-in e2e suite covers them
// on the fixture's list. A classic Win32 combo runs the driver over a fake
// combo's messages (classicComboSteps; the e2e suite covers the Classic
// Dialog's combo). The notify step runs once, last, and only after the
// selection reads back: a refused or failed select notifies nothing
// (which windows are notified is win.selectionNotice's test).

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeList is a combo or list for selectByName: its items, its
// ExpandCollapse state (-1: none) and what its live calls return.
// buildsOnExpand hides the items (and the ItemContainer ones) while the
// combo is collapsed, as WPF and WinUI do; otherwise they show either way,
// as with the Win32 combo's proxy.
type fakeList struct {
	names          []string
	state          int32
	buildsOnExpand bool
	hidden         map[string]int // ItemContainer: names past the bounded search, and how many carry each
	expandErr      error
	collapseErr    error
	itemsErr       error
	openItemsErr   error // the search fails once expanded
	pickErr        error
	staysOpen      bool // a Select does not close the drop-down
	ignores        bool // the Select is delivered but the selection does not move
	notifyErr      error
	selectedNow    string
	calls          []string
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
			if f.state == expandExpanded && f.openItemsErr != nil {
				return nil, f.openItemsErr
			}
			if f.buildsOnExpand && f.state == expandCollapsed {
				return nil, nil
			}
			return f.names, f.itemsErr
		},
		findHidden: func(label string) (int, string, error) {
			f.calls = append(f.calls, "find")
			if f.buildsOnExpand && f.state == expandCollapsed {
				return 0, "", nil
			}
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
		notify: func() error {
			f.calls = append(f.calls, "notify")
			return f.notifyErr
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
			name: "a collapsed combo whose items show collapsed is never expanded", label: "Green",
			list:      fakeList{names: colors, state: expandCollapsed},
			wantCalls: "items pick 1 read Green notify",
		},
		{
			name: "a collapsed combo with no items collapsed is expanded, picked and collapsed", label: "Green",
			list:      fakeList{names: colors, state: expandCollapsed, buildsOnExpand: true, staysOpen: true},
			wantCalls: "items find expand items pick 1 collapse read Green notify",
		},
		{
			name: "a combo that closes on the pick is not collapsed again", label: "Green",
			list:      fakeList{names: colors, state: expandCollapsed, buildsOnExpand: true},
			wantCalls: "items find expand items pick 1 read Green notify",
		},
		{
			name: "an open combo is not expanded or collapsed", label: "Blue",
			list:      fakeList{names: colors, state: expandExpanded, staysOpen: true},
			wantCalls: "items pick 2 read Blue notify",
		},
		{
			name: "a list has no ExpandCollapse", label: "Red",
			list:      fakeList{names: colors, state: -1},
			wantCalls: "items pick 0 read Red notify",
		},
		{
			name: "case and spacing are ignored when nothing matches exactly", label: "  green ",
			list:      fakeList{names: colors, state: -1},
			wantCalls: "items find pick 1 read Green notify",
		},
		{
			name: "an exact name wins over one that differs in case", label: "blue",
			list:      fakeList{names: []string{"Blue", "blue"}, state: -1},
			wantCalls: "items pick 1 read blue notify",
		},
		{
			name: "a virtualized list's unbuilt item is found through ItemContainer", label: "Item 900",
			list:      fakeList{names: []string{"Item 1", "Item 2"}, state: -1, hidden: map[string]int{"Item 900": 1}},
			wantCalls: "items find pick -1 read Item 900 notify",
		},
		{
			name: "ItemContainer's match on a collapsed combo needs no Expand", label: "Item 900",
			list:      fakeList{state: expandCollapsed, hidden: map[string]int{"Item 900": 1}},
			wantCalls: "items find pick -1 read Item 900 notify",
		},
		{
			name: "ItemContainer's exact match wins over a case-insensitive one", label: "item 2",
			list:      fakeList{names: []string{"Item 1", "Item 2"}, state: -1, hidden: map[string]int{"item 2": 1}},
			wantCalls: "items find pick -1 read item 2 notify",
		},
		{
			name: "two exact matches are ambiguous; nothing was opened", label: "Blue",
			list:      fakeList{names: []string{"Blue", "Red", "Blue"}, state: -1},
			wantErr:   `2 items match "Blue": "Blue", "Blue"`,
			wantCalls: "items",
		},
		{
			name: "an ambiguous name in a combo whose items show collapsed is refused unopened", label: "BLUE",
			list:      fakeList{names: []string{"Blue", "blue"}, state: expandCollapsed},
			wantErr:   `2 items match "BLUE": "Blue", "blue"`,
			wantCalls: "items find",
		},
		{
			name: "an ambiguous name in a combo that builds on Expand opens and closes it", label: "BLUE",
			list:      fakeList{names: []string{"Blue", "blue"}, state: expandCollapsed, buildsOnExpand: true},
			wantErr:   `the list was opened and closed; 2 items match "BLUE": "Blue", "blue"`,
			wantCalls: "items find expand items find collapse",
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
			name: "no match in a combo whose items show collapsed is refused unopened", label: "Purple",
			list:      fakeList{names: colors, state: expandCollapsed},
			wantErr:   `no item named "Purple"; its items are "Red", "Green", "Blue"`,
			wantCalls: "items find",
		},
		{
			name: "no match in a combo that builds on Expand opens and closes it", label: "Purple",
			list:      fakeList{names: colors, state: expandCollapsed, buildsOnExpand: true},
			wantErr:   `the list was opened and closed; no item named "Purple"; its items are "Red", "Green", "Blue"`,
			wantCalls: "items find expand items find collapse",
		},
		{
			name: "a combo that will not close again says so", label: "Purple",
			list:      fakeList{names: colors, state: expandCollapsed, buildsOnExpand: true, collapseErr: errors.New("no")},
			wantErr:   `the list was opened and could not be closed again; no item named "Purple"; its items are "Red", "Green", "Blue"`,
			wantCalls: "items find expand items find collapse",
		},
		{
			name: "an empty list", label: "Red",
			list:      fakeList{state: -1},
			wantErr:   `no item named "Red", and the list shows no items`,
			wantCalls: "items find",
		},
		{
			name: "a combo empty even when expanded", label: "Red",
			list:      fakeList{state: expandCollapsed},
			wantErr:   `the list was opened and closed; no item named "Red", and the list shows no items`,
			wantCalls: "items find expand items find collapse",
		},
		{
			name: "a failed collapsed search sends nothing and never expands", label: "Red",
			list:      fakeList{names: colors, state: expandCollapsed, itemsErr: fmt.Errorf("%w: gone", ErrStale)},
			wantErr:   ErrStale.Error() + ": gone",
			wantCalls: "items",
		},
		{
			name: "a failed Expand stops before the second search", label: "Red",
			list:      fakeList{names: colors, state: expandCollapsed, buildsOnExpand: true, expandErr: ErrDisabled},
			wantErr:   "the Element is disabled",
			wantCalls: "items find expand",
		},
		{
			name: "a failed search after the Expand closes the combo again", label: "Red",
			list:      fakeList{names: colors, state: expandCollapsed, buildsOnExpand: true, openItemsErr: fmt.Errorf("%w: gone", ErrStale)},
			wantErr:   "the list was opened and closed; " + ErrStale.Error() + ": gone",
			wantCalls: "items find expand items collapse",
		},
		{
			name: "a failed Select may have taken effect", label: "Red",
			list:      fakeList{names: colors, state: expandCollapsed, pickErr: write},
			wantErr:   write.Error(),
			wantCalls: "items pick 0",
		},
		{
			name: "a failed Select after an Expand closes the combo again", label: "Red",
			list:      fakeList{names: colors, state: expandCollapsed, buildsOnExpand: true, staysOpen: true, pickErr: write},
			wantErr:   write.Error(),
			wantCalls: "items find expand items pick 0 collapse",
		},
		{
			name: "a Select the list ignores fails the read-back", label: "Red",
			list:      fakeList{names: colors, state: -1, ignores: true, selectedNow: "Blue"},
			wantErr:   `set-value selected "Red" but the list shows "Blue" selected afterwards`,
			wantCalls: "items pick 0 read Red",
		},
		{
			name: "a failed notification still reports the Select delivered", label: "Green",
			list:      fakeList{names: colors, state: expandCollapsed, notifyErr: errors.New("SendMessageTimeout(0x111) failed")},
			wantErr:   `set-value selected "Green", but telling the app about the change failed (SendMessageTimeout(0x111) failed); the app may not have reacted to it`,
			wantCalls: "items pick 1 read Green notify",
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
	if !errors.Is(err, ErrAmbiguousItem) || !errors.As(err, &sel) || sel.Opened {
		t.Errorf("ambiguous with the items shown collapsed = %#v, want ErrAmbiguousItem, not opened", err)
	}
	err = selectByName("red", (&fakeList{names: []string{"Red", "RED"}, state: expandCollapsed, buildsOnExpand: true}).steps())
	if !errors.Is(err, ErrAmbiguousItem) || !errors.As(err, &sel) || !sel.Opened {
		t.Errorf("ambiguous after an Expand = %#v, want ErrAmbiguousItem, opened", err)
	}

	// The search's own failure stays recognizable through the wrapper.
	err = selectByName("Red", (&fakeList{state: expandCollapsed, buildsOnExpand: true, openItemsErr: fmt.Errorf("%w: gone", ErrStale)}).steps())
	if !errors.Is(err, ErrStale) || !errors.As(err, &sel) || !sel.Opened {
		t.Errorf("stale search = %#v, want ErrStale, opened", err)
	}

	// The read-back failure is a VerifyError: the Select was delivered.
	err = selectByName("Red", (&fakeList{names: []string{"Red"}, state: -1, ignores: true}).steps())
	var verify *VerifyError
	if !errors.As(err, &verify) || err.Error() != `set-value selected "Red" but the list shows nothing selected afterwards` {
		t.Errorf("ignored select = %v, want a VerifyError", err)
	}

	// A failed notification is a VerifyError too: the item is selected.
	err = selectByName("Red", (&fakeList{names: []string{"Red"}, state: -1, notifyErr: errors.New("hung")}).steps())
	if !errors.As(err, &verify) {
		t.Errorf("failed notification = %v, want a VerifyError", err)
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

// fakeCombo is a classic Win32 ComboBox for classicComboSteps: its items,
// its selected index, what its messages return, and the messages sent.
type fakeCombo struct {
	names     []string
	sel       int
	setErr    error
	ignores   bool // CB_SETCURSEL answers but the selection does not move
	notifyErr error
	calls     []string
}

func (f *fakeCombo) comboCalls() comboCalls {
	return comboCalls{
		items: func(uint64) ([]string, error) {
			f.calls = append(f.calls, "items")
			return f.names, nil
		},
		setCurSel: func(_ uint64, i int) error {
			f.calls = append(f.calls, fmt.Sprintf("setcursel %d", i))
			if f.setErr != nil {
				return f.setErr
			}
			if !f.ignores {
				f.sel = i
			}
			return nil
		},
		curSel: func(uint64) (int, error) { return f.sel, nil },
		notify: func(uint64) error {
			f.calls = append(f.calls, "notify")
			return f.notifyErr
		},
	}
}

// TestClassicComboSteps: a classic Win32 combo is listed, selected and
// read back through its own messages, never opened, and its parent is
// notified exactly once, only after the selection reads back.
func TestClassicComboSteps(t *testing.T) {
	colors := []string{"Red", "Green", "Blue", "blue"}
	cases := []struct {
		name      string
		label     string
		combo     fakeCombo
		wantErr   string
		wantCalls string
	}{
		{
			name: "a pick is notified once", label: "Green",
			combo:     fakeCombo{names: colors, sel: -1},
			wantCalls: "items setcursel 1 notify",
		},
		{
			name: "an exact name wins over one that differs in case", label: "blue",
			combo:     fakeCombo{names: colors, sel: 0},
			wantCalls: "items setcursel 3 notify",
		},
		{
			name: "an ambiguous name sends nothing", label: "BLUE",
			combo:     fakeCombo{names: colors, sel: -1},
			wantErr:   `2 items match "BLUE": "Blue", "blue"`,
			wantCalls: "items",
		},
		{
			name: "no match sends nothing and never opens the combo", label: "Purple",
			combo:     fakeCombo{sel: -1},
			wantErr:   `no item named "Purple", and the list shows no items`,
			wantCalls: "items",
		},
		{
			name: "a failed CB_SETCURSEL may have taken effect and is not notified", label: "Red",
			combo:     fakeCombo{names: colors, sel: -1, setErr: errors.New("timed out")},
			wantErr:   "selecting the combo's item (CB_SETCURSEL) failed: timed out",
			wantCalls: "items setcursel 0",
		},
		{
			name: "a selection that does not read back is not notified", label: "Red",
			combo:     fakeCombo{names: colors, sel: 2, ignores: true},
			wantErr:   `set-value selected "Red" but the list shows "Blue" selected afterwards`,
			wantCalls: "items setcursel 0",
		},
		{
			name: "a failed notification still reports the Select delivered", label: "Red",
			combo:     fakeCombo{names: colors, sel: -1, notifyErr: errors.New("hung")},
			wantErr:   `set-value selected "Red", but telling the app about the change failed (hung); the app may not have reacted to it`,
			wantCalls: "items setcursel 0 notify",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			combo := tc.combo
			err := selectByName(tc.label, classicComboSteps(7, combo.comboCalls()))
			if got := strings.Join(combo.calls, " "); got != tc.wantCalls {
				t.Errorf("calls = %q, want %q", got, tc.wantCalls)
			}
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("error = %v, want success", err)
			case tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr):
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
			var write *WriteError
			if combo.setErr != nil && !errors.As(err, &write) {
				t.Errorf("a failed CB_SETCURSEL = %#v, want a WriteError (it may have taken effect)", err)
			}
		})
	}
}
