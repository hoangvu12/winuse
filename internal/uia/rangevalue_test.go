package uia

// Offline tests for set-value on a RangeValue Element (recap-trial ticket
// 02): the checks made before the write (read-only, outside Minimum to
// Maximum) and the reading of a write the Element refused.

import (
	"errors"
	"math"
	"testing"
)

func TestCheckRangeWrite(t *testing.T) {
	slider := rangeFacts{min: 0, max: 100, boundsKnown: true}
	for _, tc := range []struct {
		name    string
		value   float64
		facts   rangeFacts
		want    error
		wantMsg string
	}{
		{name: "inside the range", value: 50, facts: slider},
		{name: "on the bounds", value: 100, facts: slider},
		{name: "above the maximum", value: 150, facts: slider, want: ErrRangeBounds,
			wantMsg: "the value is outside the Element's range: 150 is not between 0 and 100"},
		{name: "below the minimum", value: -0.5, facts: rangeFacts{min: 0, max: 3716.25, boundsKnown: true}, want: ErrRangeBounds,
			wantMsg: "the value is outside the Element's range: -0.5 is not between 0 and 3716.25"},
		{name: "unknown bounds are not judged", value: 1e9, facts: rangeFacts{}},
		{name: "read-only", value: 50, facts: rangeFacts{readOnly: true, min: 0, max: 100, boundsKnown: true}, want: ErrRangeReadOnly,
			wantMsg: "the Element's range value is read-only, so set-value cannot set it"},
		{name: "read-only wins over the bounds", value: 500, facts: rangeFacts{readOnly: true, min: 0, max: 100, boundsKnown: true},
			want: ErrRangeReadOnly},
	} {
		err := checkRangeWrite(tc.value, tc.facts)
		if tc.want == nil {
			if err != nil {
				t.Errorf("%s: error = %v, want nil", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", tc.name, err, tc.want)
			continue
		}
		if tc.wantMsg != "" && err.Error() != tc.wantMsg {
			t.Errorf("%s: message = %q, want %q", tc.name, err.Error(), tc.wantMsg)
		}
	}
}

func TestRangeBoundsFacts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		min, max float64
		ok       bool
		want     bool
	}{
		{"a real range", 0, 10, true, true},
		{"unreadable", 0, 10, false, false},
		{"no width: a provider that leaves both at zero", 0, 0, true, false},
		{"reversed", 10, 0, true, false},
		{"NaN", math.NaN(), 10, true, false},
	} {
		if got := rangeBoundsKnown(tc.min, tc.max, tc.ok); got != tc.want {
			t.Errorf("%s: rangeBoundsKnown = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRangeWriteFailure(t *testing.T) {
	call := &WriteError{What: "setting the Element's value", HR: -0x7feceaf7} // 0x80131509, UIA_E_INVALIDOPERATION
	t.Run("an unchanged value says nothing was changed", func(t *testing.T) {
		err := rangeWriteFailure(call, 0, 0, true)
		if !errors.Is(err, ErrRangeRefused) {
			t.Fatalf("error = %v, want ErrRangeRefused", err)
		}
		var write *WriteError
		if errors.As(err, &write) {
			t.Errorf("an unchanged value must not read as a write that may have taken effect: %v", err)
		}
		want := "the Element refused the value: setting the Element's value failed (HRESULT 0x80131509); it still reads 0, so nothing was changed"
		if err.Error() != want {
			t.Errorf("message = %q, want %q", err.Error(), want)
		}
	})
	t.Run("a changed value may have taken effect", func(t *testing.T) {
		if err := rangeWriteFailure(call, 0, 12, true); err != call {
			t.Errorf("error = %v, want the call's WriteError", err)
		}
	})
	t.Run("an unreadable value may have taken effect", func(t *testing.T) {
		if err := rangeWriteFailure(call, 0, 0, false); err != call {
			t.Errorf("error = %v, want the call's WriteError", err)
		}
	})
	t.Run("a refusal that is not a write keeps its own error", func(t *testing.T) {
		if err := rangeWriteFailure(ErrDisabled, 0, 0, true); err != ErrDisabled {
			t.Errorf("error = %v, want ErrDisabled", err)
		}
	})
}
