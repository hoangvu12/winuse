package win

import (
	"image/color"
	"testing"
)

// TestTabGroupColor (browser-ux 10, cursor-look 02, 11): each Agent Cursor
// colour maps to the Chrome tab group colour nearest in hue. The palette's
// order keeps the first six Agents apart from each other and from the
// default Agent's blue; the indigo and the yellow, which share a colour
// with an earlier one, come last.
func TestTabGroupColor(t *testing.T) {
	want := []string{"purple", "orange", "green", "pink", "cyan", "red", "blue", "yellow"}
	if len(CursorPalette) != len(want) {
		t.Fatalf("the palette has %d colours, want %d", len(CursorPalette), len(want))
	}
	first := map[string]bool{}
	for i, c := range CursorPalette {
		got := TabGroupColor(c)
		if got != want[i] {
			t.Errorf("palette %d %v = %s, want %s", i, c, got, want[i])
		}
		if i < 6 {
			if first[got] {
				t.Errorf("palette %d shares %s with an earlier one of the first six", i, got)
			}
			first[got] = true
		}
	}
	if got := TabGroupColor(DefaultCursorColor()); got != "blue" || first["blue"] {
		t.Errorf("the default Agent is %s, want blue, which none of the first six takes", got)
	}
	if DefaultCursorColor() != (color.RGBA{R: 0x2F, G: 0x6F, B: 0xD6, A: 0xFF}) {
		t.Errorf("the default Agent's colour is %v, want #2F6FD6", DefaultCursorColor())
	}
	for c, want := range map[color.RGBA]string{
		{R: 0x80, G: 0x80, B: 0x80, A: 0xFF}: "grey",
		{A: 0xFF}:                            "grey",
		{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}: "blue",
		{R: 0xFF, G: 0x00, B: 0xFF, A: 0xFF}: "purple", // magenta: hue 300, nearer purple's 276 than pink's 326
	} {
		if got := TabGroupColor(c); got != want {
			t.Errorf("%v = %s, want %s", c, got, want)
		}
	}
}
