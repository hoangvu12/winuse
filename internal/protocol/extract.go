package protocol

// extract (bladebro ideas, tickets 26 and 27): the rows of a list, table
// or JSON-LD of a browser Page (ticket 26) or, later, a native list or
// grid (ticket 27), as one header line and a JSON array.

// ExtractUsage is extract's usage line: the CLI prints it on a usage
// error, and the helper hints it on bad arguments.
const ExtractUsage = "usage: winuse extract <window>[:<tab>] [--root <element>] [--as list|table|jsonld] [--limit N] [--max-chars N | --out <file>]"

// ExtractArgs are extract's arguments.
type ExtractArgs struct {
	Window string `json:"window"`
	// Root is the container, or one item of it, as an Element Index,
	// name: or id: reference of the Window's latest Window State; "" lets
	// extract pick, and its header names what it picked.
	Root string `json:"root,omitempty"`
	// As is list, table or jsonld; "" is auto.
	As string `json:"as,omitempty"`
	// Limit caps the rows printed (0: the backend's default, 50 on a
	// Page).
	Limit int `json:"limit,omitempty"`
	// MaxChars caps the printed text (nil: the read's default cap, 0:
	// none); Out (absolute) receives the JSON array instead.
	MaxChars *int   `json:"max_chars,omitempty"`
	Out      string `json:"out,omitempty"`
}
