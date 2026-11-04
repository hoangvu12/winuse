package win

// The Agent Cursors' colours (multi-agent ticket 04): the default Agent
// keeps the accent of before, and every other Agent gets its own, from
// WINUSE_CURSOR_COLOR or the palette.

import (
	"image/color"
	"math"
	"strconv"
	"strings"
)

// CursorPalette are the colours handed to Agents that name none, in
// order: distinct from each other and from the default accent, and
// readable on light and dark Windows.
var CursorPalette = []color.RGBA{
	{R: 0xFF, G: 0x8A, B: 0x3D, A: 0xFF}, // orange
	{R: 0x2F, G: 0xBF, B: 0x71, A: 0xFF}, // green
	{R: 0xA0, G: 0x66, B: 0xFF, A: 0xFF}, // violet
	{R: 0xFF, G: 0x5C, B: 0x93, A: 0xFF}, // pink
	{R: 0x20, G: 0xB8, B: 0xC8, A: 0xFF}, // teal
	{R: 0xE8, G: 0xB5, B: 0x00, A: 0xFF}, // amber
	{R: 0xF0, G: 0x52, B: 0x4F, A: 0xFF}, // red
}

// tabGroupHues are Chrome's tab group colours (chrome.tabGroups.Color) by
// the hue of their light-theme swatch; "grey" is for the unsaturated.
var tabGroupHues = []struct {
	name string
	hue  float64
}{
	{"red", 5}, {"orange", 26}, {"yellow", 41}, {"green", 142},
	{"cyan", 184}, {"blue", 217}, {"purple", 276}, {"pink", 326},
}

// TabGroupColor is the chrome.tabGroups colour closest to an Agent
// Cursor's colour (browser-ux 10): the one nearest in hue, or grey for a
// colour with little saturation. Chrome offers only nine fixed colours.
func TabGroupColor(c color.RGBA) string {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	hi, lo := max(r, g, b), min(r, g, b)
	if hi == 0 || (hi-lo)/hi < 0.25 {
		return "grey"
	}
	d := hi - lo
	var hue float64
	switch hi {
	case r:
		hue = 60 * (g - b) / d
	case g:
		hue = 60 * ((b-r)/d + 2)
	default:
		hue = 60 * ((r-g)/d + 4)
	}
	if hue < 0 {
		hue += 360
	}
	best, bestDist := "grey", 361.0
	for _, t := range tabGroupHues {
		dist := math.Abs(hue - t.hue)
		dist = min(dist, 360-dist)
		if dist < bestDist {
			best, bestDist = t.name, dist
		}
	}
	return best
}

// DefaultCursorColor is the default Agent's accent, the cursor's colour
// before there were several Agents.
func DefaultCursorColor() color.RGBA { return cursorAccent }

// ParseCursorColor reads a WINUSE_CURSOR_COLOR value: "#RRGGBB" or
// "RRGGBB" in hex, any case. Anything else is no colour (false).
func ParseCursorColor(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}, true
}
