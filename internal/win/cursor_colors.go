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
// order, after the default Agent's blue (cursorAccent): mid-tones like
// Mural's collaborator colours (cursor-look issue 11), so the name tag's
// white text reads at 4.5:1 or better on all but the yellow, which takes
// the near-black (pillTextColor). winuse hands them out in this order
// (ADR 0014), so the first six take six different tab group colours, none
// the default Agent's blue, and the two that share one with an earlier
// colour — the indigo the default's own blue — come last.
var CursorPalette = []color.RGBA{
	{R: 0x6B, G: 0x58, B: 0xCE, A: 0xFF}, // violet
	{R: 0xC4, G: 0x52, B: 0x24, A: 0xFF}, // orange
	{R: 0x1F, G: 0x85, B: 0x3E, A: 0xFF}, // green
	{R: 0xC7, G: 0x37, B: 0x82, A: 0xFF}, // pink
	{R: 0x0E, G: 0x7C, B: 0x86, A: 0xFF}, // teal
	{R: 0xD0, G: 0x32, B: 0x45, A: 0xFF}, // red
	{R: 0x45, G: 0x50, B: 0xC8, A: 0xFF}, // indigo
	{R: 0xF5, G: 0xC2, B: 0x3C, A: 0xFF}, // yellow
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

// DefaultCursorColor is the default Agent's accent.
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
