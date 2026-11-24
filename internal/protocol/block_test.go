package protocol

import "testing"

// --block's value (bladebro-ideas ticket 28): any of images, fonts and
// media, comma-separated, in the canonical order, or none.
func TestParseBlock(t *testing.T) {
	for in, want := range map[string]string{
		"images":              "images",
		"media,images":        "images,media",
		"images,fonts,media":  "images,fonts,media",
		" Fonts , IMAGES ":    "images,fonts",
		"images,images":       "images",
		"none":                "none",
		"NONE":                "none",
		"fonts,media,images,": "images,fonts,media",
	} {
		if got, err := ParseBlock(in); err != nil || got != want {
			t.Errorf("ParseBlock(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "  ", "trackers", "images,ads", "none,images", ","} {
		if got, err := ParseBlock(bad); err == nil {
			t.Errorf("ParseBlock(%q) = %q, want an error", bad, got)
		}
	}
}

func TestBlockNote(t *testing.T) {
	for in, want := range map[string]string{
		"images,fonts,media": "Note: images, fonts and media are blocked in this Tab",
		"images,fonts":       "Note: images and fonts are blocked in this Tab",
		"media":              "Note: media are blocked in this Tab",
		"fonts,media":        "Note: fonts and media are blocked in this Tab",
		"none":               "",
		"":                   "",
	} {
		if got := BlockNote(BlockList(in)); got != want {
			t.Errorf("BlockNote(%q) = %q, want %q", in, got, want)
		}
	}
}
