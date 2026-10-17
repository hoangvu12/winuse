package fixture

// Offline tests for the fixture's keyboard plumbing that later tickets'
// e2e type-text/press-key tests rely on: the WM_CHAR surrogate-pair
// recombination (Unicode key injection delivers one UTF-16 unit per
// event, so an emoji arrives as two WM_CHAR messages). Pure logic — no
// window is created, no desktop is touched.

import (
	"testing"
	"unicode/utf16"
)

func TestCharStreamRecombinesSurrogatePairs(t *testing.T) {
	// An emoji typed as Unicode key events: two WM_CHAR messages, one per
	// UTF-16 unit, in order; the stream returns nothing for the high half
	// and the combined rune for the low half.
	for _, emoji := range []rune{'🎉', '😀', '𝔘', 0x10FFFF} {
		units := utf16.Encode([]rune{emoji})
		if len(units) != 2 {
			t.Fatalf("%U must encode as a surrogate pair", emoji)
		}
		var cs charStream
		if got := cs.char(rune(units[0])); len(got) != 0 {
			t.Errorf("%U: the high half produced %q, want nothing yet", emoji, string(got))
		}
		got := cs.char(rune(units[1]))
		if len(got) != 1 || got[0] != emoji {
			t.Errorf("%U: the low half produced %q, want the combined rune", emoji, string(got))
		}
	}
}

func TestCharStreamPassesPlainRunesThrough(t *testing.T) {
	var cs charStream
	for _, r := range "aé漢 space" {
		got := cs.char(r)
		if len(got) != 1 || got[0] != r {
			t.Errorf("char(%q) = %q, want the rune itself", string(r), string(got))
		}
	}
}

func TestCharStreamMixesPairsAndPlainRunes(t *testing.T) {
	// The typed stream "hi🎉!" must come out exactly as it went in, one
	// rune at a time.
	units := utf16.Encode([]rune("hi🎉!"))
	var cs charStream
	var out []rune
	for _, u := range units {
		out = append(out, cs.char(rune(u))...)
	}
	if string(out) != "hi🎉!" {
		t.Errorf("stream output = %q, want %q", string(out), "hi🎉!")
	}
}

func TestCharStreamDropsStrayHighHalf(t *testing.T) {
	// A lone high surrogate followed by a normal character: the stray
	// half is dropped (a real app never stores unpaired halves).
	var cs charStream
	cs.char(0xD83C) // high half, never completed
	got := cs.char('x')
	if len(got) != 1 || got[0] != 'x' {
		t.Errorf("char('x') after a stray half = %q, want just x", string(got))
	}
}

func TestCharStreamTwoPairsInARow(t *testing.T) {
	// Two emoji back to back: the pending half must not leak across the
	// boundary (the first pair's low half must not be mistaken for the
	// second pair's high half).
	units := utf16.Encode([]rune("🎉🎉"))
	var cs charStream
	var out []rune
	for _, u := range units {
		out = append(out, cs.char(rune(u))...)
	}
	if string(out) != "🎉🎉" {
		t.Errorf("stream output = %q, want two emoji", string(out))
	}
}
