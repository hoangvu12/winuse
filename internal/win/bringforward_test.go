package win

import (
	"slices"
	"testing"
)

// bringForward attaches to the foreground thread only after the plain
// call fails: an attach while that thread still has keyboard input to
// handle loses it (the tail of a Headed type-text, e2e live run
// 2026-11-03).
func TestBringForwardAttachesOnlyWhenRefused(t *testing.T) {
	cases := []struct {
		name     string
		accepted []bool // setForeground's answers, in order
		shows    []bool // verify's answers, in order
		want     bool
		steps    []string
	}{
		{"accepted and shown", []bool{true}, []bool{true}, true, []string{"set", "verify"}},
		{"refused, then accepted under the attach", []bool{false, true}, []bool{true}, true,
			[]string{"set", "attach", "set", "detach", "verify"}},
		{"accepted but not shown", []bool{true, true}, []bool{false, true}, true,
			[]string{"set", "verify", "attach", "set", "detach", "verify"}},
		{"refused twice", []bool{false, false}, []bool{false}, false,
			[]string{"set", "attach", "set", "detach", "verify"}},
	}
	for _, tc := range cases {
		var steps []string
		set, shown := 0, 0
		got := bringForward(
			func() bool { steps = append(steps, "set"); set++; return tc.accepted[set-1] },
			func() func() {
				steps = append(steps, "attach")
				return func() { steps = append(steps, "detach") }
			},
			func() bool { steps = append(steps, "verify"); shown++; return tc.shows[shown-1] },
		)
		if got != tc.want || !slices.Equal(steps, tc.steps) {
			t.Errorf("%s: %v with steps %v, want %v with %v", tc.name, got, steps, tc.want, tc.steps)
		}
	}
}
