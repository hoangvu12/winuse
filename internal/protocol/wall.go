package protocol

// The wall note (.scratch/bladebro-ideas ticket 05): after navigate, back,
// forward, reload and a Page Action that navigated, and in `winuse read`, a
// short Page that looks like a challenge or block page gets one note line
// naming the wall, and the command stays ok. Under --json its kind is the
// "wall" field (Response.Wall). The wording matches the skill's Safety
// rule: the agent stops and asks the user, and never clicks or solves
// anything.

// WallKinds are the walls winuse names (page/wall.js).
var WallKinds = []string{"Cloudflare", "DataDome", "PerimeterX", "reCAPTCHA", "hCaptcha", "Akamai", "rate-limit"}

const (
	wallNoteHead = "Note: the Page looks like a "
	wallNoteTail = " wall; ask the user to handle it"
)

// WallNote is the note line for a wall of kind (no line break).
func WallNote(kind string) string { return wallNoteHead + kind + wallNoteTail }
