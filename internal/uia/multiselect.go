package uia

// Multi-select with modifiers (desktop-parity ticket 06): Ctrl+click and
// Shift+click on a list item, Headless, through SelectionItem.
// AddToSelection when the item's container allows several selected items
// (Selection.CanSelectMultiple). Measured on WPF ListBox (Extended and
// Multiple), Win32 SysListView32 and WinUI 3 ListView, covered
// (.scratch/headless-gaps/probes-round4.md section 6): AddToSelection read
// back correctly with the foreground unchanged.
//
//   - Ctrl toggles the item: AddToSelection when it is not selected,
//     RemoveFromSelection when it is, as Ctrl+click does.
//   - Shift adds the range from the container's last selected item to
//     this one, in the container's item order; with nothing selected, just
//     this item.
//
// The selection is read back: every item the route meant to change shows
// its new state.

import (
	"context"
	"errors"
	"fmt"
	"unsafe"
)

// The SelectionItem and Selection pattern slots and the IUIAutomation slot
// this route adds (SDK UIAutomationClient.h 10.0.26100.0).
const (
	siAddToSelection      = 4 // IUIAutomationSelectionItemPattern::AddToSelection()
	siRemoveFromSelection = 5 // RemoveFromSelection()
	siSelectionContainer  = 7 // get_CurrentSelectionContainer(**el)
	selCanSelectMultiple  = 4 // IUIAutomationSelectionPattern::get_CurrentCanSelectMultiple(*BOOL)
	uiaCompareElements    = 3 // IUIAutomation::CompareElements(el1, el2, *BOOL)
)

// ErrNotMultiSelect refuses a modifier click on an Element that is not an
// item of a list that allows several selected items. Nothing was sent.
var ErrNotMultiSelect = errors.New("the Element is not an item of a list that allows several selected items")

// MultiMode is the modifier a multi-select click carries.
type MultiMode int

const (
	MultiToggle MultiMode = iota // Ctrl+click: toggle the item
	MultiRange                   // Shift+click: add the range from the last selected item
)

// rangeIndices is the item positions a Shift+click selects (pure): from
// the anchor to the target, inclusive, in order; just the target with no
// anchor (-1).
func rangeIndices(anchor, target int) []int {
	if anchor < 0 {
		return []int{target}
	}
	lo, hi := min(anchor, target), max(anchor, target)
	out := make([]int, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		out = append(out, i)
	}
	return out
}

// MultiSelect performs a Ctrl+click (MultiToggle) or Shift+click
// (MultiRange) on the item behind index of hwnd's latest Window State
// through the SelectionItem pattern, and reads the selection back. It
// returns how many items it changed.
func (c *Client) MultiSelect(ctx context.Context, hwnd uint64, index int, mode MultiMode) (int, error) {
	changed := 0
	err := c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		var err error
		changed, err = th.multiSelect(el, mode)
		return err
	})
	return changed, err
}

func (th *thread) multiSelect(el comPtr, mode MultiMode) (int, error) {
	si, hr := currentPattern(el, patternSelectionItem, &iidSelectionItemPattern)
	if hrStale(hr) {
		return 0, staleErr(hr)
	}
	if si == nil {
		return 0, ErrNotMultiSelect
	}
	defer release(si)
	var container comPtr
	if hr := call(si, siSelectionContainer, uintptr(unsafe.Pointer(&container))); hr.Failed() || container == nil {
		return 0, ErrNotMultiSelect
	}
	defer release(container)
	sel, _ := currentPattern(container, patternSelection, &iidSelectionPattern)
	if sel == nil {
		return 0, ErrNotMultiSelect
	}
	defer release(sel)
	var multi int32
	if hr := call(sel, selCanSelectMultiple, uintptr(unsafe.Pointer(&multi))); hr.Failed() || multi == 0 {
		return 0, ErrNotMultiSelect
	}

	if mode == MultiToggle {
		was := currentBool(el, propSelectionItemIsSelected)
		slot, what := uintptr(siAddToSelection), "adding the item to the selection"
		if was {
			slot, what = siRemoveFromSelection, "removing the item from the selection"
		}
		if hr := call(si, slot); hr.Failed() {
			return 0, callErr(hr, what)
		}
		return 1, verifySelected([]comPtr{el}, !was)
	}

	items, err := th.selectableDescendants(container, 0)
	if err != nil {
		return 0, err
	}
	defer releaseAll(items)
	target := th.indexOf(items, el)
	if target < 0 {
		return 0, ErrNotMultiSelect
	}
	anchor := -1
	var arr comPtr
	if hr := call(sel, selGetCurrentSelection, uintptr(unsafe.Pointer(&arr))); !hr.Failed() && arr != nil {
		current := arrayElements(arr)
		release(arr)
		if len(current) > 0 {
			anchor = th.indexOf(items, current[len(current)-1])
		}
		releaseAll(current)
	}
	var picked []comPtr
	for n, i := range rangeIndices(anchor, target) {
		pat, _ := currentPattern(items[i], patternSelectionItem, &iidSelectionItemPattern)
		if pat == nil {
			continue
		}
		hr := call(pat, siAddToSelection)
		release(pat)
		if hr.Failed() {
			if n == 0 {
				return 0, callErr(hr, "adding the item to the selection")
			}
			return n, &WriteError{What: fmt.Sprintf("adding item %d of the range to the selection", n+1), HR: hr}
		}
		picked = append(picked, items[i])
	}
	return len(picked), verifySelected(picked, true)
}

// indexOf is el's position in items (CompareElements), or -1.
func (th *thread) indexOf(items []comPtr, el comPtr) int {
	for i, it := range items {
		var same int32
		if hr := call(th.auto, uiaCompareElements, uintptr(it), uintptr(el), uintptr(unsafe.Pointer(&same))); !hr.Failed() && same != 0 {
			return i
		}
	}
	return -1
}

// verifySelected polls until every item's IsSelected reads want.
func verifySelected(items []comPtr, want bool) error {
	expected := "selected"
	if !want {
		expected = "not selected"
	}
	return pollVerify("multi-select", expected, func() (bool, string) {
		for _, it := range items {
			if currentBool(it, propSelectionItemIsSelected) != want {
				if want {
					return false, "an item still not selected"
				}
				return false, "still selected"
			}
		}
		return true, ""
	})
}
