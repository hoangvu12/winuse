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
// order: cua's nine pastels for named sessions (theme.rs, trycua/cua at
// ecaf10b, MIT; see third_party/cua/LICENSE), readable on light and dark
// Windows behind the arrow's white rim. cua picks one by the session's
// name; winuse hands them out in this order (ADR 0014), so the first six
// take six different tab group colours, none the default Agent's cyan,
// and the three that share a tab group colour with an earlier one — the
// cyan nearest the default's own blue among them — come last.
var CursorPalette = []color.RGBA{
	{R: 0xB2, G: 0x84, B: 0xFF, A: 0xFF}, // violet
	{R: 0xF7, G: 0x84, B: 0xAA, A: 0xFF}, // pink
	{R: 0x60, G: 0xDA, B: 0xAE, A: 0xFF}, // mint
	{R: 0xF4, G: 0xB2, B: 0x42, A: 0xFF}, // amber
	{R: 0xE8, G: 0x52, B: 0x62, A: 0xFF}, // red
	{R: 0x50, G: 0x7E, B: 0xEC, A: 0xFF}, // blue
	{R: 0xDD, G: 0x71, B: 0xEC, A: 0xFF}, // orchid
	{R: 0xB8, G: 0xDC, B: 0x36, A: 0xFF}, // lime
	{R: 0x4C, G: 0xCC, B: 0xE0, A: 0xFF}, // cyan
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
