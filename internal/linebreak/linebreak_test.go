package linebreak

import "testing"

func TestSameIgnoresLineBreakForms(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"one\r\ntwo\r\n", "one\rtwo\r", true},
		{"one\r\ntwo", "one\ntwo", true},
		{"one\rtwo", "one\ntwo", true},
		{"one\r\n\r\ntwo", "one\r\rtwo", true},
		{"one\r\ntwo", "one two", false},
		{"one\n\ntwo", "one\ntwo", false}, // a break is still a break: two are not one
		{"plain", "plain", true},
		{"", "", true},
	} {
		if got := Same(c.a, c.b); got != c.same {
			t.Errorf("Same(%q, %q) = %v, want %v", c.a, c.b, got, c.same)
		}
	}
}
