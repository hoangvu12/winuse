package protocol

// The page-only Actions (browser merge ticket 06): navigate, back,
// forward, reload, upload, dialog, wait and eval act on the active Tab of a
// browser Window with a Browser Connection. Their names, usage lines and
// arguments live here, where the CLI and the helper both read them.

// PageCommands are the page-only Actions' command names, also their Batch
// step kinds.
var PageCommands = []string{"navigate", "back", "forward", "reload", "upload", "dialog", "wait", "eval"}

// PageUsage is a page-only Action's usage line: what the CLI prints on a
// usage error, and the helper's hint for bad arguments.
func PageUsage(cmd string) string {
	switch cmd {
	case "navigate":
		return "usage: winuse navigate <window> <url> [--wait load|domcontentloaded|none]"
	case "upload":
		return "usage: winuse upload <window> [<element index | name: | id:>] <file>...   (without an element: the file chooser a click opened)"
	case "dialog":
		return "usage: winuse dialog <window> accept|dismiss [text]   (text answers a prompt)"
	case "wait":
		return "usage: winuse wait <window> selector <css> | text <text> | url <url or pattern with *> | load [domcontentloaded] | download   [--timeout <seconds>]\n" +
			"       winuse wait <window> <element index | name: | id:> enabled | disabled | gone | value <value> | checked | unchecked | expanded | collapsed   [--timeout <seconds>]\n" +
			"       winuse wait window <app or title> [--gone] [--timeout <seconds>]\n" +
			"       winuse wait notification [--app <name>] [--text <text>] [--timeout <seconds>]\n" +
			"       winuse wait <milliseconds>"
	case "eval":
		return "usage: winuse eval <window> <expression>"
	}
	return "usage: winuse " + cmd + " <window> [--wait load|domcontentloaded|none]" // back, forward, reload
}

// NavigateArgs are the arguments of navigate (with URL), back, forward and
// reload (without): Wait is the load state to wait for, load (the
// default), domcontentloaded or none.
type NavigateArgs struct {
	Window string `json:"window"`
	URL    string `json:"url,omitempty"`
	Wait   string `json:"wait,omitempty"`
}

// UploadArgs are upload's arguments: the file input (an Element Index or a
// name:/id: reference; neither answers the file chooser a click opened)
// and the files, as absolute paths (the CLI resolves them).
type UploadArgs struct {
	Window       string   `json:"window"`
	ElementIndex *int     `json:"element_index,omitempty"`
	Ref          string   `json:"ref,omitempty"`
	Files        []string `json:"files"`
}

// DialogArgs are dialog's arguments: accept or dismiss the JavaScript
// dialog the Tab shows, with a prompt's text.
type DialogArgs struct {
	Window string  `json:"window"`
	Action string  `json:"action"`         // accept or dismiss
	Text   *string `json:"text,omitempty"` // a prompt's answer (accept only)
}

// WaitArgs are wait's arguments: what to wait for (selector, text, url,
// load or download), its value, and a timeout in milliseconds (0: the
// default; capped by the request budget, which the CLI widens to the
// timeout plus WaitMargin through Request.TimeoutMS).
//
// The desktop waits (desktop-parity ticket 03) add three kinds. window
// waits for a Window whose App or title is Value to appear, or with Gone
// for none to be left, and names no Window (in a Batch the Batch's Window
// is set and ignored). element waits for the Element (ElementIndex or Ref)
// of the Window's latest Window State to reach State, with Value for
// State value. ms pauses MS milliseconds. text on a Window that is no
// browser Window waits in its UI Automation tree. notification
// (desktop-parity ticket 18) waits for a toast banner to show a
// notification whose sender holds App and whose title or text holds
// Value, and names no Window.
type WaitArgs struct {
	Window    string `json:"window"`
	For       string `json:"for"`
	Value     string `json:"value,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`

	Gone         bool   `json:"gone,omitempty"`          // window: wait until no such Window is left
	ElementIndex *int   `json:"element_index,omitempty"` // element: the Element, from the Window's latest Window State
	Ref          string `json:"ref,omitempty"`           // element: name:<text> or id:<AutomationId>
	State        string `json:"state,omitempty"`         // element: one of WaitStates
	MS           int    `json:"ms,omitempty"`            // ms: the pause, up to MaxWaitTimeout
	App          string `json:"app,omitempty"`           // notification: the sender's name (any part); Value is the title or text
}

// The desktop wait kinds (desktop-parity ticket 03), beside page.go's
// Page kinds.
const (
	WaitWindow  = "window"
	WaitElement = "element"
	WaitMS      = "ms"
	// WaitNotification (desktop-parity ticket 18) waits for a toast banner
	// whose notification matches App and Value, and names no Window.
	WaitNotification = "notification"
)

// WaitStates are the states an element wait waits for; value takes the
// value to wait for.
var WaitStates = []string{"enabled", "disabled", "gone", "value", "checked", "unchecked", "expanded", "collapsed"}

// EvalArgs are eval's arguments: one JavaScript expression run in the
// Tab's page.
type EvalArgs struct {
	Window     string `json:"window"`
	Expression string `json:"expression"`
}
