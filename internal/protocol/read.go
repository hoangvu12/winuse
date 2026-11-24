package protocol

// `winuse read` (.scratch/bladebro-ideas ticket 25): a Page's main
// content, or one Page Element's subtree, as Markdown.

// ReadUsage is read's usage line: what the CLI prints on a usage error,
// and the helper's hint for bad arguments.
const ReadUsage = "usage: winuse read <window>[:<tab>] [--root <element>] [--urls full] [--max-chars N | --out <file>]"

// ReadArgs are read's arguments. Root is an Element Reference (an Element
// Index, name: or id:) of the Window's latest Window State whose Page
// subtree is read instead of the main content; URLs "full" prints links
// whole; MaxChars cuts the Markdown (nil: the default cap, 0: none); Out
// writes it whole to that file (absolute; the CLI resolves it).
type ReadArgs struct {
	Window   string `json:"window"`
	Root     string `json:"root,omitempty"`
	URLs     string `json:"urls,omitempty"`
	MaxChars *int   `json:"max_chars,omitempty"`
	Out      string `json:"out,omitempty"`
}
