package uia

// select-text (headless-max ticket 10, headless-100.md G8): text in a
// field selected without keys or the mouse. The helper takes the classic
// edit route itself (EM_SETSEL on the Element's own window, read back with
// EM_GETSEL); this file holds the span both routes share and the UI
// Automation Text pattern route for every other provider (WPF, WinUI,
// Chromium's frame, RichEdit's own provider).
//
// The Text pattern route: the DocumentRange, cloned and collapsed to its
// start, its end moved start+length characters and its start moved start
// characters, then ITextRangeProvider::Select, read back by comparing
// TextPattern.GetSelection's first range with the one selected. Moving a
// range is the provider's arithmetic, not an edit: a span past the end
// fails before Select, and nothing was sent. AutoSetFocus is off on the
// client (thread.go), so Select moves no focus from winuse's side. The
// route is unmeasured headless: a provider may still activate its Window
// on Select.
//
// Offsets are UTF-16 code units, as type-text's verification counts. A
// Text pattern's character unit is the provider's own: most count UTF-16
// units, and a provider that counts a surrogate pair or a CRLF as one
// character selects the span it counts.

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// MaxTextOffset bounds a span's end: no field winuse selects in is longer,
// and the bound keeps start+length inside an int32 for the UIA calls.
const MaxTextOffset = 100_000_000

// TextSpan is what select-text selects: Length UTF-16 units from Start,
// or, with All, the whole text (0 to its length).
type TextSpan struct {
	Start  int
	Length int
	All    bool
}

// ErrTextSpan reports a span that is not a selection: a negative start or
// length, or an end past MaxTextOffset.
var ErrTextSpan = fmt.Errorf("start and length must be 0 or more, and start+length at most %d", MaxTextOffset)

// CheckTextSpan refuses a span that is not a selection.
func CheckTextSpan(s TextSpan) error {
	if s.All {
		return nil
	}
	if s.Start < 0 || s.Length < 0 || s.Start > MaxTextOffset || s.Length > MaxTextOffset-s.Start {
		return fmt.Errorf("%w, got start %d and length %d", ErrTextSpan, s.Start, s.Length)
	}
	return nil
}

// String renders the span for messages: "all of the text", or "characters
// 2 to 7" (the end excluded, as EM_GETSEL counts it).
func (s TextSpan) String() string {
	if s.All {
		return "all of the text"
	}
	return fmt.Sprintf("characters %d to %d", s.Start, s.Start+s.Length)
}

// select-text refusals, raised before anything was sent.
var (
	// ErrNoTextPattern is an Element that is no classic edit control and
	// offers no Text pattern.
	ErrNoTextPattern = errors.New("the Element is no classic edit control and offers no Text pattern, so its text cannot be selected headless")

	// ErrNoTextSelection is a Text pattern whose provider says it supports
	// no selection (SupportedTextSelection_None).
	ErrNoTextSelection = errors.New("the Element's Text pattern supports no selection")

	// ErrTextUnreadable is a Text pattern whose document range could not be
	// read or measured.
	ErrTextUnreadable = errors.New("the Element's text could not be read through its Text pattern")

	// ErrTextRange is a span that ends past the end of the text.
	ErrTextRange = errors.New("the selection ends past the end of the text")
)

// Bounds is the span's start and end in a text of length units: All is 0
// to length; any other span must end at or before length (ErrTextRange).
func (s TextSpan) Bounds(length int) (start, end int, err error) {
	if s.All {
		return 0, length, nil
	}
	end = s.Start + s.Length
	if end > length {
		return 0, 0, fmt.Errorf("%w: it would end at %d, and the text holds %d characters", ErrTextRange, end, length)
	}
	return s.Start, end, nil
}

// spanMoved is the Text pattern route's range check: the end moved from
// the document's start by movedEnd characters out of start+length asked
// for, the start by movedStart out of start. A short move is a span past
// the end of the text; the text then holds movedEnd characters.
func spanMoved(s TextSpan, movedEnd, movedStart int) error {
	if movedEnd < s.Start+s.Length {
		_, _, err := s.Bounds(movedEnd)
		return err
	}
	if movedStart != s.Start {
		return fmt.Errorf("%w: the start moved %d characters, not %d", ErrTextUnreadable, movedStart, s.Start)
	}
	return nil
}

// ---- the COM side ----

// The pattern id, IID, slots and enums the Text pattern route uses
// (verified against the SDK 10.0.26100.0 UIAutomationClient.h and
// UIAutomationCore.h).
const (
	patternText = 10014 // UIA_TextPatternId

	textGetSelection          = 5 // IUIAutomationTextPattern::GetSelection(**ranges)
	textGetDocumentRange      = 7 // IUIAutomationTextPattern::get_DocumentRange(**range)
	textGetSupportedSelection = 8 // IUIAutomationTextPattern::get_SupportedTextSelection(*enum)

	rangeClone               = 3  // IUIAutomationTextRange::Clone(**range)
	rangeCompareEndpoints    = 5  // CompareEndpoints(srcEndPoint, range, targetEndPoint, *int)
	rangeMoveEndpointByUnit  = 14 // MoveEndpointByUnit(endpoint, unit, count, *moved)
	rangeMoveEndpointByRange = 15 // MoveEndpointByRange(srcEndPoint, range, targetEndPoint)
	rangeSelect              = 16 // Select()

	rangeArrayLength     = 3 // IUIAutomationTextRangeArray::get_Length(*int)
	rangeArrayGetElement = 4 // IUIAutomationTextRangeArray::GetElement(index, **range)

	endpointStart          = 0 // TextPatternRangeEndpoint_Start
	endpointEnd            = 1 // TextPatternRangeEndpoint_End
	textUnitCharacter      = 0 // TextUnit_Character
	supportedSelectionNone = 0 // SupportedTextSelection_None

	propNativeWindowHandle = 30020 // UIA_NativeWindowHandlePropertyId
)

var iidTextPattern = windows.GUID{
	Data1: 0x32eba289, Data2: 0x3583, Data3: 0x42c9,
	Data4: [8]byte{0x9c, 0x59, 0x3b, 0x6d, 0x9a, 0x1e, 0x9b, 0x6a},
}

// ElementWindow reads the native window handle of the Element behind
// index (UIA_NativeWindowHandlePropertyId): the Element's own HWND for a
// classic control, 0 for an Element drawn inside another window.
func (c *Client) ElementWindow(ctx context.Context, hwnd uint64, index int) (uint64, error) {
	var h uint64
	err := c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		v := currentProp(el, propNativeWindowHandle)
		defer clearVariant(&v)
		if v.has(vtI4) {
			h = uint64(uint32(v.i4()))
		}
		return nil
	})
	return h, err
}

// SelectText selects the span of the Element behind index through its
// Text pattern and reads the selection back.
func (c *Client) SelectText(ctx context.Context, hwnd uint64, index int, span TextSpan) error {
	if err := CheckTextSpan(span); err != nil {
		return err
	}
	return c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		return selectTextRange(el, span)
	})
}

// selectTextRange is the Text pattern route on a live Element.
func selectTextRange(el comPtr, span TextSpan) error {
	pat, hr := currentPattern(el, patternText, &iidTextPattern)
	if hrStale(hr) {
		return staleErr(hr)
	}
	if pat == nil {
		return ErrNoTextPattern
	}
	defer release(pat)
	if patternState(pat, textGetSupportedSelection) == supportedSelectionNone {
		return ErrNoTextSelection
	}
	var doc comPtr
	if hr := call(pat, textGetDocumentRange, uintptr(unsafe.Pointer(&doc))); hr.Failed() || doc == nil {
		if hrStale(hr) {
			return staleErr(hr)
		}
		return fmt.Errorf("%w (HRESULT %s)", ErrTextUnreadable, hr)
	}
	defer release(doc)

	target := doc
	if !span.All {
		r, err := spanRange(doc, span)
		if err != nil {
			return err
		}
		defer release(r)
		target = r
	}
	if hr := call(target, rangeSelect); hr.Failed() {
		return callErr(hr, "selecting the text")
	}
	return pollVerify("select-text", span.String(), func() (bool, string) {
		return selectionIs(pat, target)
	})
}

// spanRange builds the span's range from the document range: a clone
// collapsed to the document's start, its end moved start+length
// characters, then its start moved start characters. The caller releases
// it. Nothing is selected here.
func spanRange(doc comPtr, span TextSpan) (comPtr, error) {
	var r comPtr
	if hr := call(doc, rangeClone, uintptr(unsafe.Pointer(&r))); hr.Failed() || r == nil {
		return nil, fmt.Errorf("%w (HRESULT %s)", ErrTextUnreadable, hr)
	}
	moved := func(endpoint uintptr, count int) (int, HRESULT) {
		var n int32
		hr := call(r, rangeMoveEndpointByUnit, endpoint, textUnitCharacter, uintptr(count), uintptr(unsafe.Pointer(&n)))
		return int(n), hr
	}
	if hr := call(r, rangeMoveEndpointByRange, endpointEnd, uintptr(r), endpointStart); hr.Failed() {
		release(r)
		return nil, fmt.Errorf("%w (HRESULT %s)", ErrTextUnreadable, hr)
	}
	movedEnd, hr := moved(endpointEnd, span.Start+span.Length)
	if hr.Failed() {
		release(r)
		return nil, fmt.Errorf("%w (HRESULT %s)", ErrTextUnreadable, hr)
	}
	movedStart, hr := moved(endpointStart, span.Start)
	if hr.Failed() {
		release(r)
		return nil, fmt.Errorf("%w (HRESULT %s)", ErrTextUnreadable, hr)
	}
	if err := spanMoved(span, movedEnd, movedStart); err != nil {
		release(r)
		return nil, err
	}
	return r, nil
}

// selectionIs reads the Text pattern's selection and reports whether its
// first range has the same two endpoints as want, with what it read for
// the message.
func selectionIs(pat, want comPtr) (bool, string) {
	var arr comPtr
	if hr := call(pat, textGetSelection, uintptr(unsafe.Pointer(&arr))); hr.Failed() || arr == nil {
		return false, "no readable selection"
	}
	defer release(arr)
	var n int32
	if hr := call(arr, rangeArrayLength, uintptr(unsafe.Pointer(&n))); hr.Failed() || n < 1 {
		return false, "no selection"
	}
	var got comPtr
	if hr := call(arr, rangeArrayGetElement, 0, uintptr(unsafe.Pointer(&got))); hr.Failed() || got == nil {
		return false, "no readable selection"
	}
	defer release(got)
	same := func(endpoint uintptr) bool {
		var cmp int32 = 1
		hr := call(got, rangeCompareEndpoints, endpoint, uintptr(want), endpoint, uintptr(unsafe.Pointer(&cmp)))
		return !hr.Failed() && cmp == 0
	}
	if same(endpointStart) && same(endpointEnd) {
		return true, ""
	}
	return false, "another selection"
}
