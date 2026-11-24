package cli

// The Notes in the Window State (ADR 0024, bladebro-ideas ticket 23): the
// helper answers an Agent's first get-window-state of a site or App with
// its Notes in protocol.Response.Notes. finish prints them above the
// Window State, then a blank line; --json puts them in the line's own
// "notes" field (json.go). WINUSE_NOTES=off asks the helper to hide them
// (protocol.Request.NoNotes), and so do pick and run, whose reads never
// reach the agent: hidden Notes still show on the agent's next read.

import (
	"os"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// notesOff reports whether WINUSE_NOTES=off hides the Notes.
func notesOff() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("WINUSE_NOTES")), "off")
}

// hideNotes is a sendRequest edit for a read whose output never reaches
// the agent.
func hideNotes(req *protocol.Request) { req.NoNotes = true }

// printNotes prints a response's Notes above its output, unless --json
// keeps them in their own field.
func printNotes(resp *protocol.Response) {
	if resp.Notes == "" || jsonActive {
		return
	}
	os.Stdout.WriteString(resp.Notes + "\n")
}
