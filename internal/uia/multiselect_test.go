package uia

// desktop-parity ticket 06: the range a Shift+click selects.

import (
	"slices"
	"testing"
)

func TestRangeIndices(t *testing.T) {
	cases := []struct {
		anchor, target int
		want           []int
	}{
		{-1, 3, []int{3}},
		{1, 3, []int{1, 2, 3}},
		{4, 2, []int{2, 3, 4}},
		{2, 2, []int{2}},
	}
	for _, tc := range cases {
		if got := rangeIndices(tc.anchor, tc.target); !slices.Equal(got, tc.want) {
			t.Errorf("rangeIndices(%d, %d) = %v, want %v", tc.anchor, tc.target, got, tc.want)
		}
	}
}
