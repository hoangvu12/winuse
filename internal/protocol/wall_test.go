package protocol

import "testing"

func TestWallNoteWording(t *testing.T) {
	if got := WallNote("Cloudflare"); got != "Note: the Page looks like a Cloudflare wall; ask the user to handle it" {
		t.Errorf("WallNote: %q", got)
	}
}
