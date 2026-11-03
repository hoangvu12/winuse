package protocol

// Session traces (desktop-parity ticket 19): the CLI writes one JSON line
// per command to a trace file (internal/cli/trace.go). What only the
// helper knows of a run, the Window it resolved and the routes it took,
// comes back in Response.Trace when Request.Trace asks for it.

// The routes a trace names (ADR 0010, 0011, 0012 and 0013).
const (
	RouteHeadless = "Headless" // UI Automation patterns or sent messages with a readback
	RoutePosted   = "Posted"   // posted messages, best effort (--post)
	RouteHeaded   = "Headed"   // the foreground, the real pointer and keyboard
	RouteCDP      = "CDP"      // the Page through its Browser Connection
)

// Trace is the helper's part of a trace line: the Window the command
// resolved, and the routes its Actions took in order, each once (a Batch
// can take several). Either is empty when the command has none.
type Trace struct {
	Window *TraceWindow `json:"window,omitempty"`
	Routes []string     `json:"routes,omitempty"`
}

// TraceWindow names a Window in a trace: its Window ID, App and title.
type TraceWindow struct {
	ID    uint64 `json:"id"`
	App   string `json:"app"`
	Title string `json:"title"`
}
