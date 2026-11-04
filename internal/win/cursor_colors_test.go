package win

import (
	"image/color"
	"testing"
)

// TestTabGroupColor (browser-ux 10): each Agent Cursor colour maps to the
// Chrome tab group colour that reads as the same, and the palette's
// colours stay apart there too.
func TestTabGroupColor(t *testing.T) {
	want := []string{"orange", "green", "purple", "pink", "cyan", "yellow", "red"}
	seen := map[string]bool{}
	for i, c := range CursorPalette {
		got := TabGroupColor(c)
		if got != want[i] {
			t.Errorf("palette %d %v = %s, want %s", i, c, got, want[i])
		}
		seen[got] = true
	}
	if got := TabGroupColor(DefaultCursorColor()); got != "blue" || seen["blue"] {
		t.Errorf("the default Agent is %s, want blue, which no palette colour takes", got)
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
