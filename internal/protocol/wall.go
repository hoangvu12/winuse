package protocol

// The wall Note (.scratch/bladebro-ideas ticket 05): after navigate, back,
// forward and reload, and in `winuse read`, a short Page that looks like a
// challenge or block page gets one Note line naming the wall, and the
// command stays ok. Under --json the line's kind is the "wall" field
// (WallOf). The wording matches the skill's Safety rule: the agent stops
// and asks the user, and never clicks or solves anything.

import (
	"slices"
	"strings"
)

// WallKinds are the walls winuse names (page/block.js).
var WallKinds = []string{"Cloudflare", "DataDome", "PerimeterX", "reCAPTCHA", "hCaptcha", "Akamai", "rate-limit"}

const (
	wallNoteHead = "Note: the Page looks like a "
	wallNoteTail = " wall; ask the user to handle it"
)

// WallNote is the Note line for a wall of kind (no line break).
func WallNote(kind string) string { return wallNoteHead + kind + wallNoteTail }

// WallOf is the wall the last wall Note in a command's output names, ""
// for none. A Note line stands at the left margin, or two spaces in under
// a Batch step; its kind is one of WallKinds.
func WallOf(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSuffix(lines[i], "\r")
		line = strings.TrimPrefix(line, "  ")
		kind, ok := strings.CutPrefix(line, wallNoteHead)
		if !ok {
			continue
		}
		if kind, ok = strings.CutSuffix(kind, wallNoteTail); ok && slices.Contains(WallKinds, kind) {
			return kind
		}
	}
	return ""
}
