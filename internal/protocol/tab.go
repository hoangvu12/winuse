package protocol

// TabArgs are the arguments of the tab commands (browser merge ticket 07):
// tab-list, tab-new, tab-close and tab-switch, on a browser Window with a
// Browser Connection.
type TabArgs struct {
	Window string `json:"window"`           // the browser Window (Window ID, or an App name when that App has exactly one Window)
	Tab    string `json:"tab,omitempty"`    // tab-close, tab-switch: a tab id from tab list, at least 4 characters
	URL    string `json:"url,omitempty"`    // tab-new: the page to open; empty opens about:blank
	Headed bool   `json:"headed,omitempty"` // tab-switch: the agent's --headed, needed where the switch takes the foreground (headless-max ticket 21)
}
