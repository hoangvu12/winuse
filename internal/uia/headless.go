package uia

// Headless Element Actions (headless spec, ADR 0010): click, set-value and
// Secondary Actions performed through UI Automation patterns alone — no
// SetFocus, no foreground, no injected input — on a Window that may sit
// behind the user's own work. Each one reads the Element's state back
// where the pattern has a readable outcome, so a pattern call the app
// ignored is reported instead of passing as `ok`.
//
// The click route follows a person's click on each control kind, first
// offered pattern wins: Invoke (buttons, links, menu items), Toggle (check
// boxes), SelectionItem (list items, tabs, radio buttons), ExpandCollapse
// (combo boxes, tree nodes, submenus), then LegacyIAccessible's default
// action when it names one — the only affordance of many legacy Win32
// controls (agent-desktop findings A19-6, A15-5). Toggle and
// Expand/Collapse fall back to one Invoke call when the pattern call
// leaves the state unchanged (invokeFallback).

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrNoClickRoute is a Headless click on an Element that offers none of
// the click route's patterns. Nothing was sent.
var ErrNoClickRoute = errors.New("the Element offers no Invoke, Toggle, Select, Expand/Collapse or default action, so it cannot be clicked headless")

// WriteError is a pattern write that failed with an HRESULT that does not
// prove the write was refused (unlike a stale or disabled Element): the
// provider may have acted before failing, so the Action may have taken
// effect. Err, when set, is a window message's failure instead of an
// HRESULT (a timed-out message may still be processed).
type WriteError struct {
	What string
	HR   HRESULT
	Err  error
}

func (e *WriteError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s failed: %v", e.What, e.Err)
	}
	return fmt.Sprintf("%s failed (HRESULT %s)", e.What, e.HR)
}

// VerifyError is a delivered Headless Action whose read-back does not show
// the expected state: the pattern call succeeded, the Element did not
// follow. Observed is the state read back ("" when withheld, e.g. a
// password field). Msg, when set, is the whole message instead (a
// read-back with its own wording, such as a scroll that did not move).
type VerifyError struct {
	Action   string
	Expected string
	Observed string
	Msg      string
}

func (e *VerifyError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Observed == "" {
		return fmt.Sprintf("%s was delivered but the Element does not read %s afterwards", e.Action, e.Expected)
	}
	return fmt.Sprintf("%s was delivered but the Element reads %s afterwards, not %s", e.Action, e.Observed, e.Expected)
}

// Pattern ids, IIDs and slots the Headless click route adds (SDK
// UIAutomationClient.h 10.0.26100.0).
const (
	patternInvoke            = 10000
	patternLegacyIAccessible = 10018

	invokeInvoke         = 3  // Invoke()
	legacyDoDefault      = 4  // DoDefaultAction()
	elGetCurrentProperty = 10 // IUIAutomationElement::GetCurrentPropertyValue(propertyId, *VARIANT)

	propIsPassword          = 30019
	propLegacyDefaultAction = 30100
)

var (
	iidInvokePattern = windows.GUID{
		Data1: 0xfb377fbe, Data2: 0x8ea6, Data3: 0x46d5,
		Data4: [8]byte{0x9c, 0x73, 0x64, 0x99, 0x64, 0x2d, 0x30, 0x59},
	}
	iidLegacyIAccessiblePattern = windows.GUID{
		Data1: 0x828055ad, Data2: 0x355b, Data3: 0x4435,
		Data4: [8]byte{0x86, 0xd5, 0x3b, 0x51, 0xc1, 0x4a, 0x9b, 0x1b},
	}
)

// verifyWait bounds a read-back: an app that applies a pattern call
// asynchronously (web content, Electron) settles within it.
const (
	verifyWait     = time.Second
	verifyInterval = 50 * time.Millisecond
)

// HeadlessClick clicks the Element behind index of hwnd's latest Window
// State through the click route and returns the route taken ("Invoke",
// "Toggle", "Select", "Expand", "Collapse" or "default action").
func (c *Client) HeadlessClick(ctx context.Context, hwnd uint64, index int) (string, error) {
	var route string
	err := c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		var err error
		route, err = th.headlessClick(el)
		return err
	})
	return route, err
}

// SetValueHeadless writes value like SetValue but without focusing the
// Element first (UIA SetFocus moves the desktop foreground), then reads a
// Value-pattern write back (setToggle verifies a toggle write itself, and
// selectItem the item it picks in a combo or list).
func (c *Client) SetValueHeadless(ctx context.Context, hwnd uint64, index int, value string) error {
	return c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		selected, err := th.writeValue(el, value, false)
		if err != nil || selected {
			return err
		}
		return verifyValue(el, value)
	})
}

// ErrRaiseHeadless refuses Raise on the Headless path: Raise calls
// SetFocus, which moves the desktop foreground (agent-desktop A3-4). The
// helper routes Raise Headed; this guard keeps a routing slip from ever
// taking the user's foreground silently. Nothing was sent.
var ErrRaiseHeadless = errors.New("Raise moves the foreground and cannot run headless")

// PerformSecondaryActionHeadless performs a Secondary Action like
// PerformSecondaryAction and reads Toggle, Select, Expand and Collapse
// back; Toggle, Expand and Collapse fall back to one Invoke call when the
// pattern call left the state as it was (actWithInvokeFallback). action
// must be canonical; Raise (a foreground operation by definition; the
// helper keeps it Headed) is refused with ErrRaiseHeadless before the
// Element is touched.
func (c *Client) PerformSecondaryActionHeadless(ctx context.Context, hwnd uint64, index int, action string) error {
	if action == "Raise" {
		return ErrRaiseHeadless
	}
	return c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		before := currentState(el, statePropFor(action))
		return actWithInvokeFallback(el, action, before, func() error {
			return th.performSecondary(el, index, action)
		})
	})
}

// ElementBounds reads the live bounds and name of the Element behind
// index (the Agent Cursor's destination and label). Bounds are physical
// screen pixels.
func (c *Client) ElementBounds(ctx context.Context, hwnd uint64, index int) (left, top, right, bottom int32, name string, err error) {
	var r rect
	err = c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		if hr := call(el, elCurrentBoundingRect, uintptr(unsafe.Pointer(&r))); hr.Failed() {
			if hrStale(hr) {
				return staleErr(hr)
			}
			return fmt.Errorf("reading the Element's bounds failed (HRESULT %s)", hr)
		}
		v := currentProp(el, propName)
		defer clearVariant(&v)
		if v.has(vtBSTR) {
			name = bstrToString(v.bstr())
		}
		return nil
	})
	return r.Left, r.Top, r.Right, r.Bottom, name, err
}

// propName is UIA_NamePropertyId.
const propName = 30005

// onElement runs act on the live Element behind index of hwnd's latest
// Window State, on the UIA worker (the shared shape of every element job).
func (c *Client) onElement(ctx context.Context, hwnd uint64, index int, act func(th *thread, el comPtr) error) error {
	var jobErr error
	err := c.RunFor(ctx, hwnd, func(w *worker, _ *run) {
		el, lerr := c.lookup(ctx, hwnd, index)
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
		jobErr = act(w.thread, el)
	})
	if err != nil {
		return err
	}
	return jobErr
}

// headlessClick walks the click route on the worker thread.
func (th *thread) headlessClick(el comPtr) (string, error) {
	if pat, hr := currentPattern(el, patternInvoke, &iidInvokePattern); hrStale(hr) {
		return "", staleErr(hr)
	} else if pat != nil {
		defer release(pat)
		if hr := call(pat, invokeInvoke); hr.Failed() {
			return "", callErr(hr, "invoking the Element")
		}
		return "Invoke", nil
	}

	if pat, hr := currentPattern(el, patternToggle, &iidTogglePattern); hrStale(hr) {
		return "", staleErr(hr)
	} else if pat != nil {
		defer release(pat)
		before := currentState(el, propToggleState)
		return "Toggle", actWithInvokeFallback(el, "Toggle", before, func() error {
			if hr := call(pat, toggleToggle); hr.Failed() {
				return callErr(hr, "toggling the Element")
			}
			return nil
		})
	}

	if pat, hr := currentPattern(el, patternSelectionItem, &iidSelectionItemPattern); hrStale(hr) {
		return "", staleErr(hr)
	} else if pat != nil {
		defer release(pat)
		if hr := call(pat, siSelect); hr.Failed() {
			return "", callErr(hr, "selecting the Element")
		}
		return "Select", verifySecondary(el, "Select", -1)
	}

	if pat, hr := currentPattern(el, patternExpandCollapse, &iidExpandCollapsePattern); hrStale(hr) {
		return "", staleErr(hr)
	} else if pat != nil {
		defer release(pat)
		state := patternState(pat, ecCurrentState)
		if state != expandLeafNode {
			action, slot := "Expand", uintptr(ecExpand)
			if state == expandExpanded {
				action, slot = "Collapse", ecCollapse
			}
			return action, actWithInvokeFallback(el, action, state, func() error {
				if hr := call(pat, slot); hr.Failed() {
					return callErr(hr, "performing "+action)
				}
				return nil
			})
		}
	}

	if def := currentString(el, propLegacyDefaultAction); def != "" {
		if pat, hr := currentPattern(el, patternLegacyIAccessible, &iidLegacyIAccessiblePattern); hrStale(hr) {
			return "", staleErr(hr)
		} else if pat != nil {
			defer release(pat)
			if hr := call(pat, legacyDoDefault); hr.Failed() {
				return "", callErr(hr, "performing the Element's default action")
			}
			return "default action", nil
		}
	}
	return "", ErrNoClickRoute
}

// statePropFor names the property a Secondary Action's read-back compares
// (0: the action has no readable outcome).
func statePropFor(action string) uint32 {
	switch action {
	case "Toggle":
		return propToggleState
	case "Expand", "Collapse":
		return propExpandCollapseState
	}
	return 0
}

// verifySecondary polls the Element until the action's expected state
// shows, or fails with what it reads. before is the pre-action state
// (Toggle must change it); -1 when unread or not needed.
func verifySecondary(el comPtr, action string, before int32) error {
	var expected string
	var ok func() (bool, string)
	switch action {
	case "Toggle":
		if before < 0 {
			return nil // unreadable before: nothing to compare against
		}
		expected = "a changed toggle state"
		ok = func() (bool, string) {
			now := currentState(el, propToggleState)
			return now >= 0 && now != before, toggleWord(now)
		}
	case "Select":
		expected = "selected"
		ok = func() (bool, string) {
			sel := currentBool(el, propSelectionItemIsSelected)
			if sel {
				return true, "selected"
			}
			return false, "not selected"
		}
	case "Expand":
		expected = "expanded"
		ok = func() (bool, string) {
			now := currentState(el, propExpandCollapseState)
			return now == expandExpanded || now == expandPartiallyExpanded, expandWord(now)
		}
	case "Collapse":
		expected = "collapsed"
		ok = func() (bool, string) {
			now := currentState(el, propExpandCollapseState)
			return now == expandCollapsed, expandWord(now)
		}
	default:
		return nil // Scroll, Invoke, default action: no readable outcome
	}
	return pollVerify(action, expected, ok)
}

// invokeFallback decides whether a Toggle, Expand or Collapse falls back
// to one Invoke call (headless-parity spec, "Invoke fallbacks"): some web
// widgets expose the pattern but act only on Invoke. It fires only from a
// known state before (any readable toggle state; collapsed for Expand,
// expanded for Collapse) and only when after still reads that state, after
// a pattern call that failed with a WriteError (a refusal such as a stale
// or disabled Element sent nothing to fall back from) or delivered
// without effect. A changed or unreadable state after never invokes: the
// first call may have acted, and Invoke could undo it.
func invokeFallback(action string, before, after int32, callErr error) bool {
	switch action {
	case "Toggle":
		if before != toggleOff && before != toggleOn && before != toggleIndeterminate {
			return false
		}
	case "Expand":
		if before != expandCollapsed {
			return false
		}
	case "Collapse":
		if before != expandExpanded {
			return false
		}
	default:
		return false
	}
	if callErr != nil {
		var write *WriteError
		if !errors.As(callErr, &write) {
			return false
		}
	}
	return after == before
}

// fallbackSteps are the live operations runWithInvokeFallback drives: the
// COM code binds them to an Element, the offline tests to fakes.
type fallbackSteps struct {
	perform func() error         // the pattern call
	verify  func() error         // the read-back (verifySecondary)
	state   func() int32         // the action's state now, -1 when unreadable
	invoke  func() (bool, error) // one Invoke call; false when the Element offers no Invoke (nothing sent)
}

// runWithInvokeFallback performs a Secondary Action and reads it back,
// then, when invokeFallback says so, calls Invoke once and reads the
// action's outcome back again. A missing or failed Invoke leaves the
// first outcome, which already says whether the Action was delivered.
func runWithInvokeFallback(action string, before int32, s fallbackSteps) error {
	callErr := s.perform()
	err := callErr
	if callErr == nil {
		err = s.verify()
		var verify *VerifyError
		if !errors.As(err, &verify) {
			return err
		}
	}
	if !invokeFallback(action, before, s.state(), callErr) {
		return err
	}
	if invoked, ierr := s.invoke(); !invoked || ierr != nil {
		return err
	}
	return s.verify()
}

// actWithInvokeFallback is runWithInvokeFallback on a live Element:
// perform is the pattern call, before the action's state read before it.
// The Headed v1 Secondary Actions do not use it (they read nothing back).
func actWithInvokeFallback(el comPtr, action string, before int32, perform func() error) error {
	return runWithInvokeFallback(action, before, fallbackSteps{
		perform: perform,
		verify:  func() error { return verifySecondary(el, action, before) },
		state:   func() int32 { return currentState(el, statePropFor(action)) },
		invoke: func() (bool, error) {
			pat, _ := currentPattern(el, patternInvoke, &iidInvokePattern)
			if pat == nil {
				return false, nil
			}
			defer release(pat)
			if hr := call(pat, invokeInvoke); hr.Failed() {
				return true, callErr(hr, "invoking the Element")
			}
			return true, nil
		},
	})
}

// verifyValue reads a Value-pattern write back. Password fields are
// compared but never echoed; range Elements are not compared (sliders
// snap to their steps).
func verifyValue(el comPtr, value string) error {
	if !currentHasValuePattern(el) {
		return nil // a RangeValue write
	}
	secret := currentBool(el, propIsPassword)
	expected := fmt.Sprintf("%q", value)
	if secret {
		// VerifyError prints Expected even when Observed is withheld: a
		// password must not reach the message through either side.
		expected = "the value that was set"
	}
	return pollVerify("set-value", expected, func() (bool, string) {
		now := currentString(el, propValueValue)
		if secret {
			return now == value, ""
		}
		return now == value, fmt.Sprintf("%q", now)
	})
}

// pollVerify polls ok until it holds or verifyWait passes.
func pollVerify(action, expected string, ok func() (bool, string)) error {
	deadline := time.Now().Add(verifyWait)
	for {
		good, observed := ok()
		if good {
			return nil
		}
		if time.Now().After(deadline) {
			return &VerifyError{Action: action, Expected: expected, Observed: observed}
		}
		time.Sleep(verifyInterval)
	}
}

// currentHasValuePattern reports whether the Element exposes the Value
// pattern now.
func currentHasValuePattern(el comPtr) bool {
	return currentBool(el, propIsValueAvailable)
}

// ---- live property reads (GetCurrentPropertyValue) ----

// currentProp reads one live property as a VARIANT the caller clears.
func currentProp(el comPtr, prop uint32) variant {
	var v variant
	call(el, elGetCurrentProperty, uintptr(prop), uintptr(unsafe.Pointer(&v)))
	return v
}

// currentBool reads a live VT_BOOL property; false when unsupported.
func currentBool(el comPtr, prop uint32) bool {
	v := currentProp(el, prop)
	defer clearVariant(&v)
	return v.has(vtBool) && v.boolean()
}

// currentState reads a live VT_I4 pattern state; -1 when unsupported or
// prop is 0.
func currentState(el comPtr, prop uint32) int32 {
	if prop == 0 {
		return -1
	}
	v := currentProp(el, prop)
	defer clearVariant(&v)
	if v.has(vtI4) {
		return v.i4()
	}
	return -1
}

// currentString reads a live VT_BSTR property; "" when unsupported.
func currentString(el comPtr, prop uint32) string {
	v := currentProp(el, prop)
	defer clearVariant(&v)
	if !v.has(vtBSTR) {
		return ""
	}
	return bstrToString(v.bstr())
}

// toggleWord and expandWord render a read-back state the way the Window
// State prints it.
func toggleWord(s int32) string {
	switch s {
	case toggleOff:
		return "off"
	case toggleOn:
		return "on"
	case toggleIndeterminate:
		return "indeterminate"
	}
	return "unreadable"
}

func expandWord(s int32) string {
	switch s {
	case expandCollapsed:
		return "collapsed"
	case expandExpanded:
		return "expanded"
	case expandPartiallyExpanded:
		return "partially expanded"
	case expandLeafNode:
		return "a leaf"
	}
	return "unreadable"
}
