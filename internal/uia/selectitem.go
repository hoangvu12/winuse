package uia

// Select by name (desktop-parity ticket 12): `set-value <combo or list>
// <label>` on an Element with no writable Value pattern picks the item
// with that name, the way set-value already picks a Page <select> option
// by its label. agent-desktop's select does the same bounded descendant
// search (desktop-parity research, section 1).
//
// The route: Expand when the Element reads collapsed (ExpandCollapse);
// list its items in a bounded search of its control-view descendants;
// match the label exactly, then through ItemContainer.FindItemByProperty
// (which reaches the items a virtualized list has not built), then
// ignoring case and spacing; realize a virtualized item
// (VirtualizedItem.Realize), select it (SelectionItem.Select), collapse
// what was expanded, and read the selection back. Several matches at one
// rule, or none, refuse; an Expand made for the search is collapsed again
// first. The driver (selectByName) runs over selectSteps, so the offline
// tests drive it with fakes; the COM bindings below only run live.

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Pattern ids, IIDs, slots and properties the select route adds (SDK
// UIAutomationClient.h 10.0.26100.0).
const (
	patternSelection       = 10001
	patternItemContainer   = 10019
	patternVirtualizedItem = 10020

	elFindAll              = 6 // IUIAutomationElement::FindAll(scope, cond, **arr)
	selGetCurrentSelection = 3 // IUIAutomationSelectionPattern::GetCurrentSelection(**arr)
	icFindItemByProperty   = 3 // IUIAutomationItemContainerPattern::FindItemByProperty(startAfter, propertyId, VARIANT, **found)
	viRealize              = 3 // IUIAutomationVirtualizedItemPattern::Realize()

	treeScopeChildren = 0x2

	propIsSelectionAvailable       = 30037
	propIsItemContainerAvailable   = 30108
	propIsVirtualizedItemAvailable = 30109
)

var (
	iidSelectionPattern = windows.GUID{
		Data1: 0x5ed5202e, Data2: 0xb2ac, Data3: 0x47a6,
		Data4: [8]byte{0xb6, 0x38, 0x4b, 0x0b, 0xf1, 0x40, 0xd7, 0x8e},
	}
	iidItemContainerPattern = windows.GUID{
		Data1: 0xc690fdb2, Data2: 0x27a8, Data3: 0x423c,
		Data4: [8]byte{0x81, 0x2d, 0x42, 0x97, 0x73, 0xc9, 0x08, 0x4e},
	}
	iidVirtualizedItemPattern = windows.GUID{
		Data1: 0x6ba3d7a6, Data2: 0x04cf, Data3: 0x4f11,
		Data4: [8]byte{0x87, 0x93, 0xa8, 0xd1, 0xcd, 0xe9, 0x96, 0x9f},
	}
)

// The bounded search: a combo's items sit at most a few levels down (a
// Win32 combo's list, then its items); a list with more items than
// maxSelectItems is searched through ItemContainer instead.
const (
	maxSelectDepth = 4
	maxSelectNodes = 2000
	maxSelectItems = 1000
)

// ErrNoSuchItem and ErrAmbiguousItem classify a SelectItemError.
var (
	ErrNoSuchItem    = errors.New("no item has that name")
	ErrAmbiguousItem = errors.New("several items have that name")
)

// SelectItemError is a select by name refused before any item was
// selected: no item, or several, match the label, or the search itself
// failed (Err). Opened says the list was expanded for the search and
// collapsed again, so something was sent; otherwise nothing was.
type SelectItemError struct {
	Label       string
	Matches     []string // the matching items' names (ambiguous)
	Items       []string // the items the search found (no match)
	Opened      bool     // expanded for the search, then collapsed
	CloseFailed bool     // Opened, and the Collapse failed
	Err         error    // the search's own failure, when it failed
}

func (e *SelectItemError) Error() string {
	var msg string
	switch {
	case e.Err != nil:
		msg = e.Err.Error()
	case len(e.Matches) > 0:
		msg = fmt.Sprintf("%d items match %q: %s", len(e.Matches), e.Label, quoteNames(e.Matches))
	case len(e.Items) == 0:
		msg = fmt.Sprintf("no item named %q, and the list shows no items", e.Label)
	default:
		msg = fmt.Sprintf("no item named %q; its items are %s", e.Label, quoteNames(e.Items))
	}
	switch {
	case e.Opened && e.CloseFailed:
		return "the list was opened and could not be closed again; " + msg
	case e.Opened:
		return "the list was opened and closed; " + msg
	}
	return msg
}

func (e *SelectItemError) Unwrap() error {
	switch {
	case e.Err != nil:
		return e.Err
	case len(e.Matches) > 0:
		return ErrAmbiguousItem
	}
	return ErrNoSuchItem
}

// quoteNames renders item names for a message: quoted, at most 12, with a
// count of the rest (page.OptionError's shape).
func quoteNames(names []string) string {
	shown, more := names, ""
	if len(shown) > 12 {
		more = fmt.Sprintf(", and %d more", len(shown)-12)
		shown = shown[:12]
	}
	quoted := make([]string, len(shown))
	for i, n := range shown {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ") + more
}

// foldName is the case-insensitive rule's key: whitespace collapsed,
// case folded by the caller (strings.EqualFold).
func foldName(s string) string { return strings.Join(strings.Fields(s), " ") }

// matchItems returns the indexes of names the label matches: exactly, or
// ignoring case and spacing when fold is set.
func matchItems(names []string, label string, fold bool) []int {
	var hits []int
	for i, n := range names {
		if n == label || fold && strings.EqualFold(foldName(n), foldName(label)) {
			hits = append(hits, i)
		}
	}
	return hits
}

// selectSteps are the live operations selectByName drives: the COM code
// binds them to an Element, the offline tests to a fake list.
type selectSteps struct {
	expandState func() int32             // the ExpandCollapse state now; -1 without the pattern or unreadable
	expand      func() error             // one Expand call, then the wait for the list to open
	collapse    func() error             // one Collapse call
	items       func() ([]string, error) // the bounded search: the items' names in tree order
	// findHidden looks the label up through ItemContainer, past the
	// bounded search (a virtualized list's unbuilt items): how many items
	// carry the name (0, 1, or 2 for several) and the first one's name. 0
	// without the pattern.
	findHidden func(label string) (int, string, error)
	// pick realizes item i when it is virtualized, then selects it; i is -1
	// for findHidden's item.
	pick func(i int) error
	// selected reads the selection back, polling until name shows or the
	// read-back wait passes; observed is what it reads ("" when it cannot
	// read the selection, which then counts as shown).
	selected func(name string) (ok bool, observed string)
}

// selectByName picks the item named label (set-value on a combo or list).
// Matching runs exact first, then ItemContainer's exact lookup, then
// ignoring case and spacing; several matches at one rule are an error,
// never a guess. A refusal after an Expand collapses the list again and
// says so (SelectItemError.Opened); one before it sent nothing.
func selectByName(label string, s selectSteps) error {
	opened := false
	if s.expandState() == expandCollapsed {
		if err := s.expand(); err != nil {
			return err
		}
		opened = true
	}
	closeAgain := func() bool {
		return !opened || s.collapse() == nil
	}
	refuse := func(e *SelectItemError) error {
		e.Label, e.Opened = label, opened
		e.CloseFailed = !closeAgain()
		return e
	}

	names, err := s.items()
	if err != nil {
		return refuse(&SelectItemError{Err: err})
	}
	pick, name := -2, ""
	if hits := matchItems(names, label, false); len(hits) == 1 {
		pick, name = hits[0], names[hits[0]]
	} else if len(hits) > 1 {
		return refuse(&SelectItemError{Matches: pickNames(names, hits)})
	}
	if pick == -2 {
		n, hidden, err := s.findHidden(label)
		switch {
		case err != nil:
			return refuse(&SelectItemError{Err: err})
		case n == 1:
			pick, name = -1, hidden
		case n > 1:
			return refuse(&SelectItemError{Matches: []string{label, label}})
		}
	}
	if pick == -2 {
		hits := matchItems(names, label, true)
		switch len(hits) {
		case 0:
			return refuse(&SelectItemError{Items: names})
		case 1:
			pick, name = hits[0], names[hits[0]]
		default:
			return refuse(&SelectItemError{Matches: pickNames(names, hits)})
		}
	}

	if err := s.pick(pick); err != nil {
		var write *WriteError
		if errors.As(err, &write) {
			closeAgain()
			return err // the Select may have taken effect
		}
		return refuse(&SelectItemError{Err: err})
	}
	if opened {
		if st := s.expandState(); st == expandExpanded || st == expandPartiallyExpanded {
			s.collapse() // the item is selected; a list left open is cosmetic
		}
	}
	if ok, observed := s.selected(name); !ok {
		msg := fmt.Sprintf("set-value selected %q but the list does not show it selected afterwards", name)
		if observed != "" {
			msg = fmt.Sprintf("set-value selected %q but the list shows %s selected afterwards", name, observed)
		}
		return &VerifyError{Action: "set-value", Msg: msg}
	}
	return nil
}

// pickNames returns the names at hits.
func pickNames(names []string, hits []int) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = names[h]
	}
	return out
}

// ---- the COM side ----

// selectsByName reports whether set-value picks an item on el instead of
// writing a value: it is a list or combo (Selection or ItemContainer
// pattern available).
func selectsByName(el comPtr) bool {
	return currentBool(el, propIsSelectionAvailable) || currentBool(el, propIsItemContainerAvailable)
}

// selectItem binds selectByName to the live Element el (the combo or
// list) on the worker thread. Every element it obtains is released before
// it returns.
func (th *thread) selectItem(el comPtr, label string) error {
	var ec comPtr
	if pat, hr := currentPattern(el, patternExpandCollapse, &iidExpandCollapsePattern); hrStale(hr) {
		return staleErr(hr)
	} else if pat != nil {
		ec = pat
		defer release(ec)
	}
	var found []comPtr // the bounded search's items
	var hidden comPtr  // findHidden's item
	var chosen comPtr  // the item pick selected (borrowed from the two above)
	defer func() {
		for _, it := range found {
			release(it)
		}
		release(hidden)
	}()

	return selectByName(label, selectSteps{
		expandState: func() int32 {
			if ec == nil {
				return -1
			}
			return patternState(ec, ecCurrentState)
		},
		expand: func() error {
			if hr := call(ec, ecExpand); hr.Failed() {
				return callErr(hr, "expanding the list")
			}
			// Wait for the list to open: some combos build their items
			// only once their drop-down shows.
			deadline := time.Now().Add(verifyWait)
			for patternState(ec, ecCurrentState) == expandCollapsed && time.Now().Before(deadline) {
				time.Sleep(verifyInterval)
			}
			return nil
		},
		collapse: func() error {
			if hr := call(ec, ecCollapse); hr.Failed() {
				return callErr(hr, "collapsing the list")
			}
			return nil
		},
		items: func() ([]string, error) {
			var err error
			found, err = th.selectableDescendants(el)
			if err != nil {
				return nil, err
			}
			names := make([]string, len(found))
			for i, it := range found {
				names[i] = currentString(it, propName)
			}
			return names, nil
		},
		findHidden: func(label string) (int, string, error) {
			first, err := findItemByName(el, nil, label)
			if err != nil || first == nil {
				return 0, "", err
			}
			hidden = first
			next, err := findItemByName(el, first, label)
			if err != nil {
				return 0, "", err
			}
			if next != nil {
				release(next)
				return 2, "", nil
			}
			if name := currentString(first, propName); name != "" {
				return 1, name, nil
			}
			return 1, label, nil
		},
		pick: func(i int) error {
			chosen = hidden
			if i >= 0 {
				chosen = found[i]
			}
			return realizeAndSelect(chosen)
		},
		selected: func(name string) (bool, string) {
			return readSelection(el, chosen, name)
		},
	})
}

// selectableDescendants is the bounded search: a breadth-first walk of
// el's control-view children, maxSelectDepth levels down and maxSelectNodes
// elements at most, keeping the ones that offer SelectionItem (an item's
// own children are not searched). The caller releases the items.
func (th *thread) selectableDescendants(el comPtr) ([]comPtr, error) {
	var cond comPtr
	if hr := call(th.auto, uiaGetControlViewCondition, uintptr(unsafe.Pointer(&cond))); hr.Failed() || cond == nil {
		return nil, fmt.Errorf("get_ControlViewCondition failed (HRESULT %s)", hr)
	}
	defer release(cond)

	var items []comPtr
	level := []comPtr{el} // el is borrowed; every other element is owned
	visited := 0
	for depth := 0; depth < maxSelectDepth && len(level) > 0 && visited < maxSelectNodes; depth++ {
		var next []comPtr
		for _, node := range level {
			kids, hr := findChildren(node, cond)
			if hrStale(hr) && node == el {
				releaseAll(next)
				releaseAll(items)
				return nil, staleErr(hr)
			}
			for _, k := range kids {
				visited++
				switch {
				case len(items) < maxSelectItems && visited <= maxSelectNodes && currentBool(k, propIsSelectionItemAvailable):
					items = append(items, k)
				case visited <= maxSelectNodes:
					next = append(next, k)
				default:
					release(k)
				}
			}
			if node != el {
				release(node)
			}
		}
		level = next
	}
	for _, node := range level {
		if node != el {
			release(node)
		}
	}
	return items, nil
}

// findChildren reads node's control-view children (FindAll with
// TreeScope_Children); the caller releases them.
func findChildren(node, cond comPtr) ([]comPtr, HRESULT) {
	var arr comPtr
	hr := call(node, elFindAll, treeScopeChildren, uintptr(cond), uintptr(unsafe.Pointer(&arr)))
	if hr.Failed() || arr == nil {
		return nil, hr
	}
	defer release(arr)
	return arrayElements(arr), hr
}

// arrayElements reads an IUIAutomationElementArray's elements; the caller
// releases them (and the array).
func arrayElements(arr comPtr) []comPtr {
	var n int32
	if hr := call(arr, arrGetLength, uintptr(unsafe.Pointer(&n))); hr.Failed() || n <= 0 {
		return nil
	}
	out := make([]comPtr, 0, n)
	for i := int32(0); i < n; i++ {
		var el comPtr
		if hr := call(arr, arrGetElement, uintptr(i), uintptr(unsafe.Pointer(&el))); hr.Failed() || el == nil {
			continue
		}
		out = append(out, el)
	}
	return out
}

// releaseAll releases every element.
func releaseAll(els []comPtr) {
	for _, el := range els {
		release(el)
	}
}

// findItemByName is ItemContainer.FindItemByProperty(startAfter, Name,
// label): the next item after startAfter (nil: the first) whose Name is
// label, including items a virtualized list has not built; nil when none
// or when el offers no ItemContainer. The VARIANT argument is passed by
// value in the C signature; both the x64 and the arm64 ABIs pass a
// 24-byte struct as a pointer to a caller-owned copy, which is what this
// call hands over.
func findItemByName(el, startAfter comPtr, label string) (comPtr, error) {
	pat, hr := currentPattern(el, patternItemContainer, &iidItemContainerPattern)
	if hrStale(hr) {
		return nil, staleErr(hr)
	}
	if pat == nil {
		return nil, nil
	}
	defer release(pat)
	bstr := sysAllocString(label)
	defer freeBSTR(bstr)
	v := variant{vt: vtBSTR}
	*(**uint16)(unsafe.Pointer(&v.u[0])) = bstr
	var found comPtr
	if hr := call(pat, icFindItemByProperty, uintptr(startAfter), propName, uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&found))); hr.Failed() {
		if hrStale(hr) {
			return nil, staleErr(hr)
		}
		return nil, nil // the provider cannot search by Name: nothing found
	}
	return found, nil
}

// realizeAndSelect realizes a virtualized item (VirtualizedItem.Realize)
// and selects it (SelectionItem.Select).
func realizeAndSelect(item comPtr) error {
	if currentBool(item, propIsVirtualizedItemAvailable) {
		if pat, hr := currentPattern(item, patternVirtualizedItem, &iidVirtualizedItemPattern); hrStale(hr) {
			return staleErr(hr)
		} else if pat != nil {
			hr := call(pat, viRealize)
			release(pat)
			if hr.Failed() {
				return callErr(hr, "realizing the item")
			}
		}
	}
	pat, hr := currentPattern(item, patternSelectionItem, &iidSelectionItemPattern)
	if hrStale(hr) {
		return staleErr(hr)
	}
	if pat == nil {
		return errors.New("the item offers no Select")
	}
	defer release(pat)
	if hr := call(pat, siSelect); hr.Failed() {
		return callErr(hr, "selecting the item")
	}
	return nil
}

// readSelection polls the selection until name shows or verifyWait
// passes: the container's Selection pattern when it has one, else its
// Value (a combo shows the chosen item's text), else the item's own
// IsSelected. Nothing readable counts as shown.
func readSelection(el, item comPtr, name string) (bool, string) {
	read := func() (bool, string) {
		if pat, _ := currentPattern(el, patternSelection, &iidSelectionPattern); pat != nil {
			defer release(pat)
			var arr comPtr
			if hr := call(pat, selGetCurrentSelection, uintptr(unsafe.Pointer(&arr))); !hr.Failed() && arr != nil {
				defer release(arr)
				sel := arrayElements(arr)
				defer releaseAll(sel)
				var names []string
				for _, s := range sel {
					n := currentString(s, propName)
					if n == name {
						return true, ""
					}
					names = append(names, n)
				}
				if len(names) == 0 {
					return false, "nothing"
				}
				return false, quoteNames(names)
			}
		}
		if currentHasValuePattern(el) {
			now := currentString(el, propValueValue)
			return now == name, fmt.Sprintf("%q", now)
		}
		if item != nil && currentBool(item, propIsSelectionItemAvailable) {
			return currentBool(item, propSelectionItemIsSelected), ""
		}
		return true, ""
	}
	deadline := time.Now().Add(verifyWait)
	for {
		ok, observed := read()
		if ok || time.Now().After(deadline) {
			return ok, observed
		}
		time.Sleep(verifyInterval)
	}
}
