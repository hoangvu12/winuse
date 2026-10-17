package uia

// The element-side half of ticket 07: the pattern constants and the
// Secondary-Actions policy shared by the tree walk (cached pattern
// properties → the Window State's rich state and Secondary Actions lines)
// and the Action commands (live pattern probes → set-value and
// perform-secondary-action).
//
// The Secondary Actions are Codex's set — Raise (WindowPattern), Scroll
// Up/Down/Left/Right (ScrollPattern), Expand/Collapse
// (ExpandCollapsePattern, offered per the current state) — plus winuse's
// extensions Toggle (TogglePattern) and Select (SelectionItemPattern),
// in that fixed order (orchestrator decision). One canonical table below
// (finding 18) holds the names, the gating and the pattern calls: the
// walk's listing, perform-secondary-action's dispatch, the expected-set
// error and the CLI's usage text all read it, so the three sites cannot
// drift apart. The gating follows the
// Codex-parity reference renderer (research notes, codex-surface.md §2):
//   - Raise when WindowPattern is available;
//   - scroll actions when ScrollPattern is available: Scroll Up/Down when
//     the Element scrolls vertically (or reports no scrollability), Scroll
//     Left/Right when it scrolls horizontally;
//   - Expand when the expand state is collapsed, partially expanded or
//     unreadable; Collapse when expanded, partially expanded or
//     unreadable; a leaf node offers neither;
//   - Toggle and Select whenever their patterns are available.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// secondaryAction is one row of the canonical Secondary Actions table
// (finding 18): every site that names, lists, validates or performs a
// Secondary Action reads the table — the walk's listing through
// patternFacts, perform-secondary-action's dispatch, the expected-set
// error, the CLI's usage text — so the names, their gating and their
// pattern calls can never drift apart. The table's order is the fixed
// order the Window State prints them.
type secondaryAction struct {
	name    string                                   // the canonical name
	offered func(f patternFacts) bool                // when an Element offers it
	perform func(el comPtr, p *probedPatterns) error // how it is performed
}

// scrollOfferedV gates the vertical scroll pair: an Element that scrolls
// vertically, or reports a ScrollPattern with no horizontal scrollability.
func scrollOfferedV(f patternFacts) bool {
	return f.ScrollV || (f.HasScroll && !f.ScrollH)
}

// secondaryActionTable is the one canonical list. The first seven entries
// are Codex's set (Raise, the four scrolls, Expand, Collapse); Toggle and
// Select are winuse extensions.
var secondaryActionTable = []secondaryAction{
	{
		name:    "Raise",
		offered: func(f patternFacts) bool { return f.HasWindow },
		perform: func(el comPtr, p *probedPatterns) error {
			// Codex's Raise: bring the Element (a window) to the foreground
			// through UIA's SetFocus.
			if hr := call(el, elSetFocus); hr.Failed() {
				return callErr(hr, "raising the Element")
			}
			return nil
		},
	},
	{
		name:    "Scroll Up",
		offered: scrollOfferedV,
		perform: func(el comPtr, p *probedPatterns) error {
			return scrollBy(p.scroll, scrollNoAmount, scrollLargeDecrement, "Scroll Up")
		},
	},
	{
		name:    "Scroll Down",
		offered: scrollOfferedV,
		perform: func(el comPtr, p *probedPatterns) error {
			return scrollBy(p.scroll, scrollNoAmount, scrollLargeIncrement, "Scroll Down")
		},
	},
	{
		name:    "Scroll Left",
		offered: func(f patternFacts) bool { return f.ScrollH },
		perform: func(el comPtr, p *probedPatterns) error {
			return scrollBy(p.scroll, scrollLargeDecrement, scrollNoAmount, "Scroll Left")
		},
	},
	{
		name:    "Scroll Right",
		offered: func(f patternFacts) bool { return f.ScrollH },
		perform: func(el comPtr, p *probedPatterns) error {
			return scrollBy(p.scroll, scrollLargeIncrement, scrollNoAmount, "Scroll Right")
		},
	},
	{
		// Expand while collapsed (or unreadable or partial); a leaf node
		// offers neither Expand nor Collapse.
		name: "Expand",
		offered: func(f patternFacts) bool {
			return f.HasExpand && f.ExpandState != expandLeafNode &&
				(f.ExpandState < 0 || f.ExpandState == expandCollapsed || f.ExpandState == expandPartiallyExpanded)
		},
		perform: func(el comPtr, p *probedPatterns) error {
			if hr := call(p.expand, ecExpand); hr.Failed() {
				return callErr(hr, "expanding the Element")
			}
			return nil
		},
	},
	{
		// Collapse while expanded (or unreadable or partial).
		name: "Collapse",
		offered: func(f patternFacts) bool {
			return f.HasExpand && f.ExpandState != expandLeafNode &&
				(f.ExpandState < 0 || f.ExpandState == expandExpanded || f.ExpandState == expandPartiallyExpanded)
		},
		perform: func(el comPtr, p *probedPatterns) error {
			if hr := call(p.expand, ecCollapse); hr.Failed() {
				return callErr(hr, "collapsing the Element")
			}
			return nil
		},
	},
	{
		name:    "Toggle",
		offered: func(f patternFacts) bool { return f.HasToggle },
		perform: func(el comPtr, p *probedPatterns) error {
			if hr := call(p.toggle, toggleToggle); hr.Failed() {
				return callErr(hr, "toggling the Element")
			}
			return nil
		},
	},
	{
		name:    "Select",
		offered: func(f patternFacts) bool { return f.HasSelection },
		perform: func(el comPtr, p *probedPatterns) error {
			if hr := call(p.selection, siSelect); hr.Failed() {
				return callErr(hr, "selecting the Element")
			}
			return nil
		},
	},
}

// SecondaryActionNames lists every Secondary Action winuse performs, in
// the fixed order the Window State prints them and errors list the
// expected set: the canonical table's names. The first seven are Codex's;
// Toggle and Select are winuse extensions.
var SecondaryActionNames = secondaryActionNames()

// secondaryActionNames derives the canonical name list from the table.
func secondaryActionNames() []string {
	names := make([]string, len(secondaryActionTable))
	for i, a := range secondaryActionTable {
		names[i] = a.name
	}
	return names
}

// secondaryActionRow finds the canonical row for action (already
// canonicalized); nil when the name is not in the table.
func secondaryActionRow(action string) *secondaryAction {
	for i := range secondaryActionTable {
		if secondaryActionTable[i].name == action {
			return &secondaryActionTable[i]
		}
	}
	return nil
}

// CanonicalSecondaryAction resolves an action name (case-insensitive,
// whitespace-collapsed) to its canonical spelling.
func CanonicalSecondaryAction(name string) (string, bool) {
	key := strings.ToLower(strings.Join(strings.Fields(name), " "))
	for _, a := range SecondaryActionNames {
		if strings.ToLower(a) == key {
			return a, true
		}
	}
	return "", false
}

// ExpectedSecondaryActions renders the expected-set phrase of the
// unsupported-action error, Codex-style: a comma list with "or" before
// the last entry.
func ExpectedSecondaryActions() string {
	names := SecondaryActionNames
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

// Action errors the helper maps to recovery hints. Message texts follow
// Codex's lowercase, specific style (codex-surface.md §2, §4).
var (
	// ErrNotSettable is Codex's set_value refusal for an Element with no
	// writable value pattern.
	ErrNotSettable = errors.New("Cannot set a value for an element that is not settable")

	// ErrFocusBeforeValue is Codex's set_value failure when the Element
	// cannot be focused before its value is written.
	ErrFocusBeforeValue = errors.New("focus element before setting value")

	// ErrRangeValue reports a value that a range Element cannot parse.
	ErrRangeValue = errors.New("range value must be a number")

	// ErrDisabled reports an Element that refused a pattern call because
	// it is disabled (UIA_E_ELEMENTNOTENABLED).
	ErrDisabled = errors.New("the Element is disabled")
)

// UnavailableActionError reports a Secondary Action the Element does not
// offer, listing the ones it does — the ticket's contract for "unsupported
// Secondary Actions fail listing the ones the Element offers".
type UnavailableActionError struct {
	Action  string
	Index   int
	Offered []string
}

func (e *UnavailableActionError) Error() string {
	offered := strings.Join(e.Offered, ", ")
	if offered == "" {
		offered = "none"
	}
	if e.Index < 0 {
		// The @x,y form: there is no Element Index to name.
		return fmt.Sprintf("secondary action '%s' is not available for the Element at the point; available actions: %s",
			e.Action, offered)
	}
	return fmt.Sprintf("secondary action '%s' is not available for element %d; available actions: %s",
		e.Action, e.Index, offered)
}

// Pattern ids used with GetCurrentPatternAs (SDK UIAutomationClient.h;
// uia-go notes §2).
const (
	patternValue          = 10002
	patternRangeValue     = 10003
	patternScroll         = 10004
	patternExpandCollapse = 10005
	patternWindow         = 10009
	patternSelectionItem  = 10010
	patternToggle         = 10015
)

// Client-side pattern interface IIDs (SDK UIAutomationClient.h; uia-go
// notes §2).
var (
	iidValuePattern = windows.GUID{
		Data1: 0xa94cd8b1, Data2: 0x0844, Data3: 0x4cd6,
		Data4: [8]byte{0x9d, 0x2d, 0x64, 0x05, 0x37, 0xab, 0x39, 0xe9},
	}
	iidRangeValuePattern = windows.GUID{
		Data1: 0x59213f4f, Data2: 0x7346, Data3: 0x49e5,
		Data4: [8]byte{0xb1, 0x20, 0x80, 0x55, 0x59, 0x87, 0xa1, 0x48},
	}
	iidScrollPattern = windows.GUID{
		Data1: 0x88f4d42a, Data2: 0xe881, Data3: 0x459d,
		Data4: [8]byte{0xa7, 0x7c, 0x73, 0xbb, 0xbb, 0x7e, 0x02, 0xdc},
	}
	iidExpandCollapsePattern = windows.GUID{
		Data1: 0x619be086, Data2: 0x1f4e, Data3: 0x4ee4,
		Data4: [8]byte{0xba, 0xfa, 0x21, 0x01, 0x28, 0x73, 0x87, 0x30},
	}
	iidWindowPattern = windows.GUID{
		Data1: 0x0faef453, Data2: 0x9208, Data3: 0x43ef,
		Data4: [8]byte{0xbb, 0xb2, 0x3b, 0x48, 0x51, 0x77, 0x86, 0x4f},
	}
	iidSelectionItemPattern = windows.GUID{
		Data1: 0xa8efa66a, Data2: 0x0fda, Data3: 0x421a,
		Data4: [8]byte{0x91, 0x94, 0x38, 0x02, 0x1f, 0x35, 0x78, 0xea},
	}
	iidTogglePattern = windows.GUID{
		Data1: 0x94cf8058, Data2: 0x9b8d, Data3: 0x4ab9,
		Data4: [8]byte{0x8b, 0xfd, 0x4c, 0xd0, 0xa3, 0x3c, 0x8c, 0x70},
	}
)

// Pattern method vtable slots (verified against SDK 10.0.26100.0; see
// uia-go notes §2). IUnknown occupies slots 0-2.
const (
	valueSetValue      = 3  // SetValue(BSTR)
	valueCurrentRO     = 5  // get_CurrentIsReadOnly(*BOOL)
	rangeValueSetValue = 3  // SetValue(double)
	scrollScroll       = 3  // Scroll(hAmount, vAmount)
	scrollCurrentH     = 9  // get_CurrentHorizontallyScrollable(*BOOL)
	scrollCurrentV     = 10 // get_CurrentVerticallyScrollable(*BOOL)
	ecExpand           = 3  // Expand()
	ecCollapse         = 4  // Collapse()
	ecCurrentState     = 5  // get_CurrentExpandCollapseState(*int32)
	siSelect           = 3  // Select()
	toggleToggle       = 3  // Toggle()
)

// UIA enums the policy and the actions use (SDK; uia-go notes §2).
const (
	toggleOff, toggleOn, toggleIndeterminate = 0, 1, 2

	expandCollapsed, expandExpanded, expandPartiallyExpanded, expandLeafNode = 0, 1, 2, 3

	scrollLargeDecrement, scrollSmallDecrement, scrollNoAmount, scrollLargeIncrement, scrollSmallIncrement = 0, 1, 2, 3, 4
)

// Cached pattern property ids (SDK UIAutomationClient.h; verified against
// the local SDK 10.0.26100.0 header). Pattern-availability properties let
// the walk list Secondary Actions with no per-element pattern calls.
const (
	propIsExpandCollapseAvailable = 30028
	propIsRangeValueAvailable     = 30033
	propIsScrollAvailable         = 30034
	propIsSelectionItemAvailable  = 30036
	propIsToggleAvailable         = 30041
	propIsValueAvailable          = 30043
	propIsWindowAvailable         = 30044
	propValueValue                = 30045
	propValueIsReadOnly           = 30046
	propScrollHScrollable         = 30057
	propScrollVScrollable         = 30058
	propExpandCollapseState       = 30070
	propSelectionItemIsSelected   = 30079
	propToggleState               = 30086
)

// patternFacts is the pattern-derived data Secondary Actions are listed
// from, however it was obtained: cached pattern properties during the
// walk, live pattern probes during an Action.
type patternFacts struct {
	HasWindow bool // WindowPattern available (Raise)
	HasScroll bool // ScrollPattern available
	ScrollH   bool // scrolls horizontally
	ScrollV   bool // scrolls vertically
	HasExpand bool // ExpandCollapsePattern available
	// ExpandState is the current ExpandCollapseState, -1 when unreadable.
	// Leaf nodes (3) support the pattern but offer no Expand/Collapse.
	ExpandState  int32
	HasToggle    bool // TogglePattern available
	HasSelection bool // SelectionItemPattern available
}

// patternFacts returns the NodeData's pattern facts.
func (d NodeData) patternFacts() patternFacts {
	return patternFacts{
		HasWindow:    d.HasWindow,
		HasScroll:    d.HasScroll,
		ScrollH:      d.ScrollH,
		ScrollV:      d.ScrollV,
		HasExpand:    d.HasExpand,
		ExpandState:  d.ExpandState,
		HasToggle:    d.HasToggle,
		HasSelection: d.HasSelectionItem,
	}
}

// secondaryActions lists the Secondary Actions the facts describe, in the
// fixed printed order: the canonical table's rows whose offered gate the
// facts satisfy.
func (f patternFacts) secondaryActions() []string {
	var out []string
	for _, a := range secondaryActionTable {
		if a.offered(f) {
			out = append(out, a.name)
		}
	}
	return out
}

// ---- cached pattern properties (the walk's rich state) ----

// VT_ types (oaidl.h).
const (
	vtEmpty = 0
	vtI4    = 3
	vtBSTR  = 8
	vtBool  = 11
)

// variant is a Win32 VARIANT (24 bytes: vt, 3 reserved words, a 16-byte
// union). Values arrive through GetCachedPropertyValue and are owned by
// us: every one is VariantClear'ed.
type variant struct {
	vt uint16
	_  [3]uint16
	u  [2]uintptr
}

func (v *variant) bstr() *uint16     { return *(**uint16)(unsafe.Pointer(&v.u[0])) }
func (v *variant) i4() int32         { return *(*int32)(unsafe.Pointer(&v.u[0])) }
func (v *variant) boolean() bool     { return *(*int16)(unsafe.Pointer(&v.u[0])) != 0 }
func (v *variant) has(t uint16) bool { return v.vt == t }

var procVariantClear = oleaut32.NewProc("VariantClear")

// cachedProp reads one cached property as a VARIANT. The VARIANT is owned
// by the caller (callers here always clear it immediately).
func cachedProp(el comPtr, prop uint32) variant {
	var v variant
	call(el, elGetCachedPropertyValue, uintptr(prop), uintptr(unsafe.Pointer(&v)))
	return v
}

// clearVariant frees a VARIANT owned by us (BSTRs, arrays, interfaces).
func clearVariant(v *variant) {
	procVariantClear.Call(uintptr(unsafe.Pointer(v)))
}

// propBool reads a VT_BOOL property; false when unsupported (VT_EMPTY).
func propBool(el comPtr, prop uint32) bool {
	v := cachedProp(el, prop)
	defer clearVariant(&v)
	return v.has(vtBool) && v.boolean()
}

// propState reads a pattern-state VT_I4 property; -1 (unknown) when the
// element does not support it. ExpandState and ToggleState use -1 for
// "pattern present but state unreadable" (both actions offered then).
func propState(el comPtr, prop uint32) int32 {
	v := cachedProp(el, prop)
	defer clearVariant(&v)
	if v.has(vtI4) {
		return v.i4()
	}
	return -1
}

// propString reads a VT_BSTR property (the Value pattern's value); ""
// when unsupported.
func propString(el comPtr, prop uint32) string {
	v := cachedProp(el, prop)
	defer clearVariant(&v)
	if !v.has(vtBSTR) {
		return ""
	}
	return bstrToString(v.bstr())
}

// ---- set-value and perform-secondary-action (the UIA worker's half) ----

// SetValue sets an Element's value directly (ticket 07). The Element Index
// is resolved against the Window's latest Window State; the Element is
// focused first (Codex's set_value does), then written through its Value
// pattern — or, for range Elements (sliders), through RangeValue with the
// value parsed as a number.
func (c *Client) SetValue(ctx context.Context, hwnd uint64, index int, value string) error {
	var jobErr error
	err := c.Run(ctx, func(w *worker, _ *run) {
		el, lerr := c.lookup(hwnd, index)
		if lerr != nil {
			jobErr = lerr
			return
		}
		// AddRef'd on hand-out (finding 4): release our reference after
		// the action.
		defer c.store.release(el)
		if w.thread == nil {
			return // fake workers carry no COM thread; tests exercise the miss paths
		}
		jobErr = w.thread.setValue(el, value)
	})
	if err != nil {
		return err
	}
	return jobErr
}

// SetValueAtPoint writes value to the Element at the screen point (an
// @x,y Element Reference, resolved through the Window's latest
// screenshot's Mapping): the worker resolves the point to the live
// Element under it and writes exactly like the Element Index form.
func (c *Client) SetValueAtPoint(ctx context.Context, x, y int32, value string) error {
	var jobErr error
	err := c.Run(ctx, func(w *worker, _ *run) {
		if w.thread == nil {
			return // fake workers carry no COM thread; tests exercise the miss paths
		}
		el, herr := w.thread.elementFromPoint(x, y)
		if herr != nil {
			jobErr = herr
			return
		}
		defer release(el)
		jobErr = w.thread.setValue(el, value)
	})
	if err != nil {
		return err
	}
	return jobErr
}

// PerformSecondaryActionAtPoint performs one Secondary Action on the
// Element at the screen point (an @x,y Element Reference, resolved
// through the Window's latest screenshot's Mapping). action must already
// be canonical (CanonicalSecondaryAction).
func (c *Client) PerformSecondaryActionAtPoint(ctx context.Context, x, y int32, action string) error {
	var jobErr error
	err := c.Run(ctx, func(w *worker, _ *run) {
		if w.thread == nil {
			return // fake workers carry no COM thread; tests exercise the miss paths
		}
		el, herr := w.thread.elementFromPoint(x, y)
		if herr != nil {
			jobErr = herr
			return
		}
		defer release(el)
		jobErr = w.thread.performSecondary(el, -1, action)
	})
	if err != nil {
		return err
	}
	return jobErr
}

// PerformSecondaryAction performs one of an Element's Secondary Actions
// (ticket 07): Raise, Scroll Up/Down/Left/Right, Expand, Collapse, plus
// winuse's Toggle and Select. The Element's offered actions are probed
// live on the worker thread, so an action the Element no longer exposes
// fails listing what it offers now. action must already be canonical
// (CanonicalSecondaryAction).
func (c *Client) PerformSecondaryAction(ctx context.Context, hwnd uint64, index int, action string) error {
	var jobErr error
	err := c.Run(ctx, func(w *worker, _ *run) {
		el, lerr := c.lookup(hwnd, index)
		if lerr != nil {
			jobErr = lerr
			return
		}
		// AddRef'd on hand-out (finding 4): release our reference after
		// the action.
		defer c.store.release(el)
		if w.thread == nil {
			return // fake workers carry no COM thread; tests exercise the miss paths
		}
		jobErr = w.thread.performSecondary(el, index, action)
	})
	if err != nil {
		return err
	}
	return jobErr
}

// setValue writes an Element's value on the worker thread: focus first,
// then Value.SetValue(BSTR), or RangeValue.SetValue(number) for range
// Elements, else Codex's not-settable refusal.
func (th *thread) setValue(el comPtr, value string) error {
	if hr := call(el, elSetFocus); hr.Failed() {
		// Codex's set_value focuses the Element first; a stale or disabled
		// Element maps to its sentinel, anything else to Codex's message.
		if hrStale(hr) {
			return staleErr(hr)
		}
		if hr == uiaErrElementDisabled {
			return ErrDisabled
		}
		return ErrFocusBeforeValue
	}
	// ValuePattern: a read-only value cannot be set (Codex's refusal).
	if pat, hr := currentPattern(el, patternValue, &iidValuePattern); hrStale(hr) {
		return staleErr(hr)
	} else if pat != nil {
		defer release(pat)
		var ro int32
		call(pat, valueCurrentRO, uintptr(unsafe.Pointer(&ro))) // unreadable → writable
		if ro != 0 {
			return ErrNotSettable
		}
		bstr := sysAllocString(value)
		defer freeBSTR(bstr)
		if hr := call(pat, valueSetValue, uintptr(unsafe.Pointer(bstr))); hr.Failed() {
			return callErr(hr, "setting the Element's value")
		}
		return nil
	}
	// RangeValuePattern: the value must parse as a number.
	if pat, hr := currentPattern(el, patternRangeValue, &iidRangeValuePattern); hrStale(hr) {
		return staleErr(hr)
	} else if pat != nil {
		defer release(pat)
		f, perr := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if perr != nil {
			return fmt.Errorf("%w: %s", ErrRangeValue, value)
		}
		if runtime.GOARCH != "amd64" {
			// RangeValue.SetValue takes a double by value; Go's windows/arm64
			// syscall trampoline cannot pass those (uia-go notes §2).
			return errors.New("setting a range value is not supported on this architecture")
		}
		if hr := call(pat, rangeValueSetValue, uintptr(math.Float64bits(f))); hr.Failed() {
			return callErr(hr, "setting the Element's value")
		}
		return nil
	}
	return ErrNotSettable
}

// probedPatterns holds the live pattern interfaces probed on an Element
// for one Secondary Action, each obtained once.
type probedPatterns struct {
	window    comPtr // WindowPattern (Raise availability)
	scroll    comPtr // ScrollPattern
	expand    comPtr // ExpandCollapsePattern
	toggle    comPtr // TogglePattern
	selection comPtr // SelectionItemPattern
}

func (p *probedPatterns) release() {
	release(p.window)
	release(p.scroll)
	release(p.expand)
	release(p.toggle)
	release(p.selection)
}

// performSecondary performs one Secondary Action on the worker thread. The
// Element's pattern facts are probed live (the Window State's cached line
// may already be stale), an action the Element does not offer fails
// listing the ones it does, and the call goes through the matching
// pattern method.
func (th *thread) performSecondary(el comPtr, index int, action string) error {
	f, p, err := probeFacts(el)
	if err != nil {
		return err
	}
	defer p.release()
	row := secondaryActionRow(action)
	if row == nil {
		return fmt.Errorf("unsupported secondary action: %s; expected %s", action, ExpectedSecondaryActions())
	}
	if !row.offered(f) {
		return &UnavailableActionError{Action: action, Index: index, Offered: f.secondaryActions()}
	}
	return row.perform(el, p)
}

// probeFacts reads an Element's pattern facts through live pattern
// probes (one GetCurrentPatternAs per pattern) plus the current scroll
// and expand state, returning the facts and the pattern interfaces for
// the caller to use and release.
func probeFacts(el comPtr) (patternFacts, *probedPatterns, error) {
	var f patternFacts
	var p probedPatterns

	win, hr := currentPattern(el, patternWindow, &iidWindowPattern)
	if hrStale(hr) {
		return f, nil, staleErr(hr)
	}
	p.window, f.HasWindow = win, win != nil

	sc, hr := currentPattern(el, patternScroll, &iidScrollPattern)
	if hrStale(hr) {
		p.release()
		return f, nil, staleErr(hr)
	}
	p.scroll, f.HasScroll = sc, sc != nil
	if sc != nil {
		f.ScrollH = patternBool(sc, scrollCurrentH)
		f.ScrollV = patternBool(sc, scrollCurrentV)
	}

	ec, hr := currentPattern(el, patternExpandCollapse, &iidExpandCollapsePattern)
	if hrStale(hr) {
		p.release()
		return f, nil, staleErr(hr)
	}
	p.expand, f.HasExpand = ec, ec != nil
	if ec != nil {
		f.ExpandState = patternState(ec, ecCurrentState)
	}

	tg, hr := currentPattern(el, patternToggle, &iidTogglePattern)
	if hrStale(hr) {
		p.release()
		return f, nil, staleErr(hr)
	}
	p.toggle, f.HasToggle = tg, tg != nil

	si, hr := currentPattern(el, patternSelectionItem, &iidSelectionItemPattern)
	if hrStale(hr) {
		p.release()
		return f, nil, staleErr(hr)
	}
	p.selection, f.HasSelection = si, si != nil
	return f, &p, nil
}

// currentPattern probes an element's live pattern: GetCurrentPatternAs
// hands back the pattern interface, or a nil out with S_OK when the
// element does not support it.
func currentPattern(el comPtr, patternId uintptr, iid *windows.GUID) (comPtr, HRESULT) {
	var pat comPtr
	hr := call(el, elGetCurrentPatternAs, patternId, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&pat)))
	if hr.Failed() {
		return nil, hr
	}
	return pat, hr
}

// scrollBy scrolls through the ScrollPattern with the direction's
// amounts (a large step per page, Codex's "Scroll Down" granularity).
func scrollBy(pat comPtr, h, v uintptr, action string) error {
	if pat == nil {
		// The element no longer exposes ScrollPattern (it may have offered
		// the action in the Window State that produced the index).
		return errors.New("element no longer exposes ScrollPattern")
	}
	if hr := call(pat, scrollScroll, h, v); hr.Failed() {
		return callErr(hr, "performing "+action)
	}
	return nil
}

// patternBool reads a get_Current* BOOL getter (BOOL is a 4-byte int).
func patternBool(pat comPtr, slot uintptr) bool {
	var v int32
	call(pat, slot, uintptr(unsafe.Pointer(&v)))
	return v != 0
}

// patternState reads a get_Current* pattern-state int32 getter; -1 on
// failure (unreadable).
func patternState(pat comPtr, slot uintptr) int32 {
	var v int32 = -1
	call(pat, slot, uintptr(unsafe.Pointer(&v)))
	return v
}

// callErr maps a failed COM call on a live element: stale elements fail
// as stale (the read-the-state-again contract), disabled elements say so,
// anything else names the operation and the HRESULT.
func callErr(hr HRESULT, what string) error {
	switch {
	case hrStale(hr):
		return staleErr(hr)
	case hr == uiaErrElementDisabled:
		return ErrDisabled
	}
	return fmt.Errorf("%s failed (HRESULT %s)", what, hr)
}

// staleErr wraps a stale HRESULT (the Element behind the index is gone).
func staleErr(hr HRESULT) error {
	return fmt.Errorf("%w: the Element is no longer available (HRESULT %s)", ErrStale, hr)
}

// sysAllocString allocates a BSTR the caller frees (freeBSTR). BSTRs are
// length-prefixed; a UTF16 NUL terminator is not part of the content.
func sysAllocString(s string) *uint16 {
	runes := []rune(s)
	if len(runes) == 0 {
		b, _, _ := procSysAllocStringLen.Call(0, 0)
		return u16ptr(b)
	}
	u16 := utf16.Encode(runes)
	b, _, _ := procSysAllocStringLen.Call(uintptr(unsafe.Pointer(&u16[0])), uintptr(len(u16)))
	return u16ptr(b)
}

// u16ptr converts the pointer-sized return value of SysAllocStringLen to
// the BSTR it is: C memory the GC ignores, valid until freeBSTR releases
// it. The indirection through the local's address keeps go vet's
// unsafeptr check quiet while preserving the exact same conversion.
func u16ptr(u uintptr) *uint16 {
	return *(**uint16)(unsafe.Pointer(&u))
}
