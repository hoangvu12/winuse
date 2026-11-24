package uia

// set-value on a RangeValue Element (recap-trial ticket 02). Before the
// write, a read-only range or a value outside Minimum..Maximum is refused
// with nothing sent: a provider would refuse it anyway, with an HRESULT
// (UIA_E_INVALIDOPERATION, ArgumentOutOfRange) that says nothing useful. A
// write the Element refused is read back: a value that still reads as it
// did says nothing was changed, instead of "may have taken effect".

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"unsafe"
)

var (
	// ErrRangeReadOnly is set-value on a range Element whose RangeValue is
	// read-only (a scroll bar or progress bar that only reports its
	// position): nothing was sent.
	ErrRangeReadOnly = errors.New("the Element's range value is read-only, so set-value cannot set it")

	// ErrRangeBounds is set-value on a range Element with a value outside its
	// Minimum..Maximum: nothing was sent.
	ErrRangeBounds = errors.New("the value is outside the Element's range")

	// ErrRangeRefused is a RangeValue write that failed while the value read
	// the same before and after: nothing was changed.
	ErrRangeRefused = errors.New("the Element refused the value")
)

// rangeFacts is what a RangeValue Element says about the values it takes.
type rangeFacts struct {
	readOnly    bool
	min, max    float64
	boundsKnown bool // min and max read and make a range (rangeBoundsKnown)
}

// rangeBoundsKnown reports whether a Minimum and Maximum read (ok) describe a
// range to check a value against: finite, with max above min. Providers
// that do not set them leave both at zero.
func rangeBoundsKnown(min, max float64, ok bool) bool {
	return ok && !math.IsNaN(min) && !math.IsNaN(max) && !math.IsInf(min, 0) && !math.IsInf(max, 0) && max > min
}

// num formats a range number the shortest way it reads back.
func num(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// checkRangeWrite refuses a write the Element cannot take: a read-only
// range first, then a value outside known bounds.
func checkRangeWrite(value float64, f rangeFacts) error {
	if f.readOnly {
		return ErrRangeReadOnly
	}
	if f.boundsKnown && (value < f.min || value > f.max) {
		return fmt.Errorf("%w: %s is not between %s and %s", ErrRangeBounds, num(value), num(f.min), num(f.max))
	}
	return nil
}

// rangeWriteFailure reads a failed RangeValue write: a WriteError whose
// value read the same before and after (both readable) becomes
// ErrRangeRefused, with nothing changed; anything else stays as it is.
func rangeWriteFailure(err error, before, after float64, readable bool) error {
	var write *WriteError
	if !errors.As(err, &write) || !readable || before != after {
		return err
	}
	return fmt.Errorf("%w: %s; it still reads %s, so nothing was changed", ErrRangeRefused, write.Error(), num(after))
}

// ---- the COM side ----

// rangeDouble reads one double property of a RangeValue pattern.
func rangeDouble(pat comPtr, slot uintptr) (float64, bool) {
	var f float64
	if hr := call(pat, slot, uintptr(unsafe.Pointer(&f))); hr.Failed() {
		return 0, false
	}
	return f, true
}

// readRangeFacts reads a RangeValue pattern's IsReadOnly, Minimum and
// Maximum. An unreadable IsReadOnly reads writable.
func readRangeFacts(pat comPtr) rangeFacts {
	var ro int32
	call(pat, rangeValueCurrentReadOnly, uintptr(unsafe.Pointer(&ro)))
	lo, okLo := rangeDouble(pat, rangeValueCurrentMinimum)
	hi, okHi := rangeDouble(pat, rangeValueCurrentMaximum)
	return rangeFacts{readOnly: ro != 0, min: lo, max: hi, boundsKnown: rangeBoundsKnown(lo, hi, okLo && okHi)}
}
