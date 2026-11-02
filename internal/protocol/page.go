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
		return "usage: winuse wait <window> selector <css> | text <text> | url <url or pattern with *> | load [domcontentloaded] | download   [--timeout <seconds>]"
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
type WaitArgs struct {
	Window    string `json:"window"`
	For       string `json:"for"`
	Value     string `json:"value,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// EvalArgs are eval's arguments: one JavaScript expression run in the
// Tab's page.
type EvalArgs struct {
	Window     string `json:"window"`
	Expression string `json:"expression"`
}
