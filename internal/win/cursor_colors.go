package win

// The Agent Cursors' colours (multi-agent ticket 04): the default Agent
// keeps the accent of before, and every other Agent gets its own, from
// WINUSE_CURSOR_COLOR or the palette.

import (
	"image/color"
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
