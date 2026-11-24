package protocol

// `winuse logs` (.scratch/bladebro-ideas ticket 29): a Tab's log, the
// failed requests and CSP or security errors its Page logged.

// LogsUsage is logs' usage line: what the CLI prints on a usage error, and
// the helper's hint for bad arguments.
const LogsUsage = "usage: winuse logs <window>[:<tab>] [--clear] [--max-chars N | --out <file>]"

// LogsArgs are logs' arguments. Clear empties the Tab's log after printing
// it; MaxChars cuts the entries printed (nil: the default cap, 0: none),
// keeping the newest; Out writes them all to that file (absolute; the CLI
// resolves it); JSON answers with a JSON object (the global --json).
type LogsArgs struct {
	Window   string `json:"window"`
	Clear    bool   `json:"clear,omitempty"`
	MaxChars *int   `json:"max_chars,omitempty"`
	Out      string `json:"out,omitempty"`
	JSON     bool   `json:"json,omitempty"`
}
