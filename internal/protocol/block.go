package protocol

// Asset blocking (bladebro-ideas ticket 28): `tab new <window> [url]
// --block images,fonts,media|none` and `navigate <window>:<tab> <url>
// --block ...` on a Tab winuse opened (or any Tab of a browser winuse
// launched or cloned). The value travels in canonical form, the classes in
// BlockClasses order joined by commas, or "none"; reads, --observe and
// screenshots of a blocking Tab end with BlockNote.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// BlockClasses are the asset classes --block takes, in the order the
// canonical value and the note name them.
var BlockClasses = []string{"images", "fonts", "media"}

// BlockNone is --block none: blocking off.
const BlockNone = "none"

// BlockUsage is the option's usage, for errors.
const BlockUsage = "--block takes images, fonts and media, comma-separated, or none"

// ParseBlock parses --block's value into its canonical form: the classes
// in BlockClasses order, each once, joined by commas; or "none".
func ParseBlock(v string) (string, error) {
	on := map[string]bool{}
	none := false
	for _, part := range strings.Split(v, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		switch {
		case p == "":
			continue
		case p == BlockNone:
			none = true
		case slices.Contains(BlockClasses, p):
			on[p] = true
		default:
			return "", fmt.Errorf("--block has no class %q: %s", strings.TrimSpace(part), BlockUsage)
		}
	}
	switch {
	case none && len(on) > 0:
		return "", errors.New("--block none turns blocking off, so it goes alone")
	case none:
		return BlockNone, nil
	case len(on) == 0:
		return "", errors.New(BlockUsage)
	}
	var out []string
	for _, c := range BlockClasses {
		if on[c] {
			out = append(out, c)
		}
	}
	return strings.Join(out, ","), nil
}

// BlockList is a canonical --block value's classes; nil for none or "".
func BlockList(v string) []string {
	if v == "" || v == BlockNone {
		return nil
	}
	return strings.Split(v, ",")
}

// BlockNote is the line that ends a read of a Tab blocking these classes:
// "Note: images, fonts and media are blocked in this Tab". "" for none.
func BlockNote(classes []string) string {
	var names string
	switch n := len(classes); n {
	case 0:
		return ""
	case 1:
		names = classes[0]
	default:
		names = strings.Join(classes[:n-1], ", ") + " and " + classes[n-1]
	}
	return "Note: " + names + " are blocked in this Tab"
}
