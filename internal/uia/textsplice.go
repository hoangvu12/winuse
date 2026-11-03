package uia

// Caret-aware typing into Text-pattern fields (headless-max ticket 13,
// headless-100.md G5): `type-text --into <element>` on a field with no
// classic edit control behind it (WPF, WinUI) puts the text at the
// field's caret without keys: the selection is read through the Text
// pattern, the Value is written as prefix + text + suffix, read back, and
// the caret is placed after the text. Measured Headless on WPF and WinUI
// 3 (probes round 5, section 1).
//
// It is not keystroke-faithful: no key events fire, WinUI keeps no undo
// step (WPF keeps one), and web validators may not run. The helper says so
// in its output.
//
// Offsets are UTF-16 code units, read as the length of the document's text
// up to an endpoint (the document range cloned, its end moved to the
// endpoint, GetText). A Text pattern's Character unit is the provider's
// own (WPF counts an emoji as one character, WinUI counts UTF-16 units),
// so the caret is placed, read back, and placed again by the other count
// when the first lands elsewhere.

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf16"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/linebreak"
)

// Splice refusals, raised before anything was sent. ErrNoTextPattern and
// ErrNotSettable are shared with select-text and set-value.
var (
	// ErrSplicePassword is a password field: its text is never read.
	ErrSplicePassword = errors.New("the Element is a password field; winuse neither reads nor rewrites its text")

	// ErrSpliceNoValue is a Text pattern with no Value pattern to write.
	ErrSpliceNoValue = errors.New("the Element offers no Value pattern, so its text cannot be rewritten at the caret")

	// ErrSpliceRichText is a document control: rewriting its whole value
	// would drop its formatting.
	ErrSpliceRichText = errors.New("the Element is a rich text document; rewriting its value would drop its formatting")

	// ErrSpliceMismatch is a field whose Text pattern text and Value
	// differ, so a caret offset in one is no offset in the other.
	ErrSpliceMismatch = errors.New("the Element's text and its value differ, so the caret cannot be placed in the value")

	// ErrSpliceNoCaret is a Text pattern that reports no selection.
	ErrSpliceNoCaret = errors.New("the Element reports no caret or selection")
)

const (
	rangeGetText            = 12    // IUIAutomationTextRange::GetText(maxLength, *BSTR)
	rangeMove               = 13    // Move(unit, count, *moved)
	controlTypeDocument     = 50030 // UIA_DocumentControlTypeId
	spliceCaretPlaceRetries = 2     // by UTF-16 units, then by runes
)

// SpliceResult is what a splice did besides the write: whether the caret
// ended right after the text.
type SpliceResult struct {
	CaretPlaced bool
}

// Splice16 is the splice in UTF-16 units: value with its units start to
// end replaced by text. Offsets outside the value are an error.
func Splice16(value string, start, end int, text string) (string, error) {
	u := utf16.Encode([]rune(value))
	if start < 0 || end < start || end > len(u) {
		return "", fmt.Errorf("the selection %d to %d is outside the field's %d characters", start, end, len(u))
	}
	out := make([]uint16, 0, len(u)-(end-start)+len(utf16.Encode([]rune(text))))
	out = append(out, u[:start]...)
	out = append(out, utf16.Encode([]rune(text))...)
	out = append(out, u[end:]...)
	return string(utf16.Decode(out)), nil
}

// units is s's length in UTF-16 code units.
func units(s string) int { return len(utf16.Encode([]rune(s))) }

// SpliceText puts text at the caret of the Element behind index (see the
// file comment) and reads the value back.
func (c *Client) SpliceText(ctx context.Context, hwnd uint64, index int, text string) (SpliceResult, error) {
	var res SpliceResult
	err := c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		r, err := spliceText(el, text)
		res = r
		return err
	})
	return res, err
}

// spliceText is SpliceText on a live Element.
func spliceText(el comPtr, text string) (SpliceResult, error) {
	pat, hr := currentPattern(el, patternText, &iidTextPattern)
	if hrStale(hr) {
		return SpliceResult{}, staleErr(hr)
	}
	if pat == nil {
		return SpliceResult{}, ErrNoTextPattern
	}
	defer release(pat)
	if currentBool(el, propIsPassword) {
		return SpliceResult{}, ErrSplicePassword
	}
	if currentState(el, propControlType) == controlTypeDocument {
		return SpliceResult{}, ErrSpliceRichText
	}
	val, hr := currentPattern(el, patternValue, &iidValuePattern)
	if hrStale(hr) {
		return SpliceResult{}, staleErr(hr)
	}
	if val == nil {
		return SpliceResult{}, ErrSpliceNoValue
	}
	defer release(val)
	var ro int32
	call(val, valueCurrentRO, uintptr(unsafe.Pointer(&ro)))
	if ro != 0 {
		return SpliceResult{}, ErrNotSettable
	}

	doc, err := documentRange(pat)
	if err != nil {
		return SpliceResult{}, err
	}
	defer release(doc)
	docText, ok := rangeText(doc)
	if !ok {
		return SpliceResult{}, ErrTextUnreadable
	}
	value := currentString(el, propValueValue)
	if value != docText {
		return SpliceResult{}, ErrSpliceMismatch
	}
	start, end, err := selectionOffsets(pat, doc)
	if err != nil {
		return SpliceResult{}, err
	}
	next, err := Splice16(value, start, end, text)
	if err != nil {
		return SpliceResult{}, fmt.Errorf("%w: %v", ErrTextUnreadable, err)
	}

	bstr := sysAllocString(next)
	defer freeBSTR(bstr)
	if hr := call(val, valueSetValue, uintptr(unsafe.Pointer(bstr))); hr.Failed() {
		return SpliceResult{}, &WriteError{What: "setting the field's value", HR: hr}
	}
	if err := pollVerify("type-text", "the text at the caret", func() (bool, string) {
		return linebreak.Same(currentString(el, propValueValue), next), "another value"
	}); err != nil {
		return SpliceResult{}, err
	}
	// SetValue moves the caret to the start (probes round 5): place it
	// after the text, best effort.
	return SpliceResult{CaretPlaced: placeCaret(pat, start+units(text), runesBefore(next, start+units(text)))}, nil
}

// documentRange reads the Text pattern's document range; the caller
// releases it.
func documentRange(pat comPtr) (comPtr, error) {
	var doc comPtr
	if hr := call(pat, textGetDocumentRange, uintptr(unsafe.Pointer(&doc))); hr.Failed() || doc == nil {
		if hrStale(hr) {
			return nil, staleErr(hr)
		}
		return nil, fmt.Errorf("%w (HRESULT %s)", ErrTextUnreadable, hr)
	}
	return doc, nil
}

// rangeText reads a range's whole text.
func rangeText(r comPtr) (string, bool) {
	var b *uint16
	if hr := call(r, rangeGetText, ^uintptr(0), uintptr(unsafe.Pointer(&b))); hr.Failed() {
		return "", false
	}
	defer freeBSTR(b)
	return bstrToString(b), true
}

// offsetOf is an endpoint's offset in UTF-16 units: the length of the
// document's text up to it (the document cloned, its end moved to the
// endpoint).
func offsetOf(doc, r comPtr, endpoint uintptr) (int, bool) {
	var c comPtr
	if hr := call(doc, rangeClone, uintptr(unsafe.Pointer(&c))); hr.Failed() || c == nil {
		return 0, false
	}
	defer release(c)
	if hr := call(c, rangeMoveEndpointByRange, endpointEnd, uintptr(r), endpoint); hr.Failed() {
		return 0, false
	}
	s, ok := rangeText(c)
	return units(s), ok
}

// firstSelection is the Text pattern's first selection range; the caller
// releases it.
func firstSelection(pat comPtr) (comPtr, bool) {
	var arr comPtr
	if hr := call(pat, textGetSelection, uintptr(unsafe.Pointer(&arr))); hr.Failed() || arr == nil {
		return nil, false
	}
	defer release(arr)
	var n int32
	if hr := call(arr, rangeArrayLength, uintptr(unsafe.Pointer(&n))); hr.Failed() || n < 1 {
		return nil, false
	}
	var sel comPtr
	if hr := call(arr, rangeArrayGetElement, 0, uintptr(unsafe.Pointer(&sel))); hr.Failed() || sel == nil {
		return nil, false
	}
	return sel, true
}

// selectionOffsets reads the selection's start and end in UTF-16 units.
func selectionOffsets(pat, doc comPtr) (start, end int, err error) {
	sel, ok := firstSelection(pat)
	if !ok {
		return 0, 0, ErrSpliceNoCaret
	}
	defer release(sel)
	start, ok1 := offsetOf(doc, sel, endpointStart)
	end, ok2 := offsetOf(doc, sel, endpointEnd)
	if !ok1 || !ok2 || end < start {
		return 0, 0, ErrTextUnreadable
	}
	return start, end, nil
}

// placeCaret puts the caret at want UTF-16 units and reads it back. The
// provider's Character unit decides how far a move goes, so the move is
// tried by UTF-16 units, then by runes (runes is the rune count up to
// want). It reports whether the caret ended at want.
func placeCaret(pat comPtr, want, runes int) bool {
	doc, err := documentRange(pat)
	if err != nil {
		return false
	}
	defer release(doc)
	counts := []int{want}
	if runes != want {
		counts = append(counts, runes)
	}
	for _, n := range counts[:min(len(counts), spliceCaretPlaceRetries)] {
		var r comPtr
		if hr := call(doc, rangeClone, uintptr(unsafe.Pointer(&r))); hr.Failed() || r == nil {
			return false
		}
		// Collapse to the start, move n characters, collapse again (without
		// the second collapse WPF selects one character), then Select.
		call(r, rangeMoveEndpointByRange, endpointEnd, uintptr(r), endpointStart)
		var moved int32
		call(r, rangeMove, textUnitCharacter, uintptr(n), uintptr(unsafe.Pointer(&moved)))
		call(r, rangeMoveEndpointByRange, endpointEnd, uintptr(r), endpointStart)
		hr := call(r, rangeSelect)
		release(r)
		if hr.Failed() {
			return false
		}
		if s, e, err := selectionOffsets(pat, doc); err == nil && s == want && e == want {
			return true
		}
	}
	return false
}

// runesBefore is the number of runes in the first n UTF-16 units of s.
func runesBefore(s string, n int) int {
	count, seen := 0, 0
	for _, r := range s {
		if seen >= n {
			break
		}
		seen += len(utf16.Encode([]rune{r}))
		count++
	}
	return count
}
