package protocol

// NoteCommands are the wire names of `winuse note add|list|remove|clear`
// (ADR 0024, bladebro-ideas ticket 22).
var NoteCommands = []string{"note-add", "note-list", "note-remove", "note-clear"}

// NoteArgs are the note commands' arguments. Target is a Window ID, a Tab
// target, a URL or domain, or an exe name; the helper works out its key.
// It is named target, not window, so the Window wrappers leave it alone.
type NoteArgs struct {
	Target string `json:"target,omitempty"`
	App    bool   `json:"app,omitempty"`  // a Window's App rather than its Page's site
	Text   string `json:"text,omitempty"` // note-add
	N      int    `json:"n,omitempty"`    // note-remove: the Note's number in note list
	JSON   bool   `json:"json,omitempty"` // answer with JSON (the global --json)
}
