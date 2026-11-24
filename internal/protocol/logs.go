package protocol

// `winuse logs` (.scratch/bladebro-ideas tickets 29 and 30): a Tab's log,
// the failed requests and CSP or security errors its Page logged, and the
// JavaScript exceptions it threw where the Tab captures them.

// LogsUsage is logs' usage line: what the CLI prints on a usage error, and
// the helper's hint for bad arguments.
const LogsUsage = "usage: winuse logs <window>[:<tab>] [--capture] [--clear] [--max-chars N | --out <file>]"

// LogsArgs are logs' arguments. Capture turns on the capture of the Tab's
// JavaScript exceptions for the Tab's life (a Tab winuse opened has it
// already); Clear empties the Tab's log after printing it; MaxChars cuts
// the entries printed (nil: the default cap, 0: none), keeping the newest;
// Out writes them all to that file (absolute; the CLI resolves it); JSON
// answers with a JSON object (the global --json).
type LogsArgs struct {
	Window   string `json:"window"`
	Capture  bool   `json:"capture,omitempty"`
	Clear    bool   `json:"clear,omitempty"`
	MaxChars *int   `json:"max_chars,omitempty"`
	Out      string `json:"out,omitempty"`
	JSON     bool   `json:"json,omitempty"`
}
