package protocol

import "testing"

func TestWallNoteWording(t *testing.T) {
	if got := WallNote("Cloudflare"); got != "Note: the Page looks like a Cloudflare wall; ask the user to handle it" {
		t.Errorf("WallNote: %q", got)
	}
}

func TestWallOfReadsTheNoteBack(t *testing.T) {
	for _, tc := range []struct{ out, want string }{
		{"ok https://shop.test/\n" + WallNote("Cloudflare") + "\n", "Cloudflare"},
		{"ok https://api.test/\n" + WallNote("rate-limit") + "\n", "rate-limit"},
		{"ok https://shop.test/\n", ""},
		{"", ""},
		// Page text that only quotes the wording is no Note line.
		{"Page: x\n\n> Note: the Page looks like a Cloudflare wall; ask the user to handle it\n", ""},
		// A Batch indents a step's notes; its last navigation decides.
		{"Action 1 (navigate): ok a\n  " + WallNote("DataDome") + "\nAction 2 (navigate): ok b\n  " + WallNote("Akamai") + "\n", "Akamai"},
		// A kind winuse never names is no Note of its own.
		{"ok a\nNote: the Page looks like a friendly wall; ask the user to handle it\n", ""},
	} {
		if got := WallOf(tc.out); got != tc.want {
			t.Errorf("WallOf(%q) = %q, want %q", tc.out, got, tc.want)
		}
	}
}
