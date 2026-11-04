package win

import (
	"image/color"
	"testing"
)

// TestTabGroupColor (browser-ux 10, cursor-look 02): each Agent Cursor
// colour maps to the Chrome tab group colour nearest in hue. cua's nine
// pastels and the default take only seven of Chrome's colours, so the
// palette's order keeps the first six Agents apart from each other and
// from the default Agent there.
func TestTabGroupColor(t *testing.T) {
	want := []string{"purple", "pink", "green", "yellow", "red", "blue", "purple", "yellow", "cyan"}
	if len(CursorPalette) != len(want) {
		t.Fatalf("the palette has %d colours, want cua's 9", len(CursorPalette))
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
	if got := TabGroupColor(DefaultCursorColor()); got != "cyan" || first["cyan"] {
		t.Errorf("the default Agent is %s, want cyan, which none of the first six takes", got)
	}
	if DefaultCursorColor() != (color.RGBA{R: 0x5E, G: 0xC0, B: 0xE8, A: 0xFF}) {
		t.Errorf("the default Agent's colour is %v, want cua's #5EC0E8", DefaultCursorColor())
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
