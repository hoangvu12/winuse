package uia

// The renamed-item read-back's pure parts (headless-max 24): which
// read-back failures send it looking for the item under its new name, and
// what counts as named so. The sibling search itself runs on live UI
// Automation (the opt-in e2e suite).

import (
	"errors"
	"fmt"
	"testing"
)

func TestReadEmptyIsAnEmptySetValueRead(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&VerifyError{Action: "set-value", Expected: `"summary.txt"`, Observed: `""`}, true},
		{fmt.Errorf("wrapped: %w", &VerifyError{Action: "set-value", Observed: `""`}), true},
		{&VerifyError{Action: "set-value", Expected: `"x"`, Observed: `"y"`}, false},
		{&VerifyError{Action: "set-value", Expected: "the value that was set", Observed: ""}, false}, // a password field
		{&VerifyError{Action: "type-text", Observed: `""`}, false},
		{errors.New("set-value failed"), false},
		{nil, false},
	} {
		if got := readEmpty(c.err); got != c.want {
			t.Errorf("readEmpty(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestNamedAs(t *testing.T) {
	for _, c := range []struct {
		name, val, value string
		want             bool
	}{
		{"summary.txt", "summary.txt", "summary.txt", true},
		{"summary.txt", "", "summary.txt", true},
		{"", "summary.txt", "summary.txt", true},
		{"two\rlines", "", "two\r\nlines", true},
		{"draft-summary.txt", "draft-summary.txt", "summary.txt", false},
		{"", "", "", false}, // an empty value names nothing
	} {
		if got := namedAs(c.name, c.val, c.value); got != c.want {
			t.Errorf("namedAs(%q, %q, %q) = %v, want %v", c.name, c.val, c.value, got, c.want)
		}
	}
}

func TestRenamedInPlaceNeedsANewName(t *testing.T) {
	empty := &VerifyError{Action: "set-value", Expected: `"summary.txt"`, Observed: `""`}
	other := &VerifyError{Action: "set-value", Expected: `"summary.txt"`, Observed: `"draft.txt"`}
	asked := 0
	after := func(v bool) func() bool { return func() bool { asked++; return v } }
	for _, c := range []struct {
		name        string
		err         error
		before, now bool
		want        bool
	}{
		{"the item is found under its new name", empty, false, true, true},
		{"a sibling was named so already", empty, true, true, false},
		{"no sibling is named so", empty, false, false, false},
		{"the Element reads another value", other, false, true, false},
		{"the write read back", nil, false, true, false},
	} {
		if got := renamedInPlace(c.err, c.before, after(c.now)); got != c.want {
			t.Errorf("%s: renamedInPlace = %v, want %v", c.name, got, c.want)
		}
	}
	// The siblings are read again only when the read-back is "" and the
	// name was new: the first and third cases.
	if asked != 2 {
		t.Errorf("the siblings were read %d times after the write, want 2", asked)
	}
}
