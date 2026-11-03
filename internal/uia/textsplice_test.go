package uia

import "testing"

func TestSplice16CountsUTF16Units(t *testing.T) {
	cases := []struct {
		value      string
		start, end int
		text, want string
	}{
		{"hello world", 5, 5, "xy", "helloxy world"},   // at the caret
		{"hello world", 6, 11, "there", "hello there"}, // over a selection
		{"", 0, 0, "first", "first"},                   // an empty field
		{"a😀b", 3, 3, "c", "a😀cb"},                     // after a surrogate pair: 1+2 units
		{"a😀b", 1, 3, "x", "axb"},                      // the pair itself selected
		{"hello world", 0, 11, "😀", "😀"},               // all of it
		{"héllo", 2, 2, "-", "hé-llo"},                 // é is one unit
	}
	for _, c := range cases {
		got, err := Splice16(c.value, c.start, c.end, c.text)
		if err != nil || got != c.want {
			t.Errorf("Splice16(%q, %d, %d, %q) = %q, %v, want %q", c.value, c.start, c.end, c.text, got, err, c.want)
		}
	}
	for _, bad := range [][2]int{{-1, 0}, {3, 2}, {0, 12}} {
		if _, err := Splice16("hello world", bad[0], bad[1], "x"); err == nil {
			t.Errorf("Splice16 with %v must fail", bad)
		}
	}
}

func TestRunesBeforeCountsTheProvidersCharacters(t *testing.T) {
	// WPF moves by whole emoji, WinUI by UTF-16 units: placeCaret tries the
	// units first, then this rune count.
	cases := []struct {
		s    string
		n    int
		want int
	}{
		{"hello", 3, 3},
		{"a😀b", 3, 2},
		{"😀😀x", 4, 2},
		{"😀😀x", 5, 3},
		{"", 0, 0},
	}
	for _, c := range cases {
		if got := runesBefore(c.s, c.n); got != c.want {
			t.Errorf("runesBefore(%q, %d) = %d, want %d", c.s, c.n, got, c.want)
		}
	}
	if units("a😀b") != 4 {
		t.Errorf("units(a😀b) = %d, want 4", units("a😀b"))
	}
}
