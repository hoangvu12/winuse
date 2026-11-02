// Package page reads the Page of a Chromium browser Window through its
// Browser Connection and merges it into the Window State (browser merge
// ticket 04, ADR 0012).
//
// A browser Window has one Window State: the frame (tab strip, address
// bar, menus, dialogs, infobars) is read through UI Automation as before,
// and the descendants of the UIA document Element of the active Tab are
// replaced by the Page read over CDP. The pieces:
//
//   - match.go: which Tab a Window shows (bounds plus title, the rule the
//     ticket 03 probe settled), pure over faked bounds and titles.
//   - reader.go: the live glue. It finds the Tab over a Browser
//     Connection, attaches a flattened session (cached per Tab), turns on
//     auto-attach for cross-origin iframes, and records a Raw read:
//     Accessibility.getFullAXTree, the frame tree's loader ids, and the
//     cursor-pointer pass (clickables.js).
//   - build.go and roles.go: Raw to Window State Elements, pure, with the
//     same caps and truncation notes as the UI Automation walk (ADR 0004).
//   - merge.go: the Elements spliced in at the document Element.
//   - origin.go: what an Element Index of the Page points at, its
//     staleness checks, and its rectangle (for ticket 05's Actions).
//
// A Raw read is also the fixture format: testdata holds hand-authored
// getFullAXTree answers that the offline tests render to golden text.
package page

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// Raw is one read of a Tab's Page: what the browser answered, before any
// rendering. It is also the fixture format of the offline tests.
type Raw struct {
	Source   string      `json:"source,omitempty"`  // provenance of a fixture file
	Browser  string      `json:"browser,omitempty"` // the browser's short name (chrome, edge, ...)
	TargetID string      `json:"targetId"`          // the active Tab's page target
	Title    string      `json:"title"`             // the Tab's title (the UIA document is named after it)
	URL      string      `json:"url,omitempty"`
	Others   []string    `json:"others,omitempty"` // the other Tabs' URLs: a UIA document showing one is a stale Tab's (DocRule)
	Main     Doc         `json:"main"`             // the Tab's own document
	Frames   []Frame     `json:"frames,omitempty"` // iframes read on their own, one level deep
	Client   *cdp.Client `json:"-"`                // the connection the read went over; Origins carry it
}

// Doc is one document's accessibility tree and the session it was read
// on.
type Doc struct {
	SessionID string            `json:"sessionId"`
	FrameID   string            `json:"frameId"`           // the document's frame
	Loaders   map[string]string `json:"loaders,omitempty"` // frame id to loader id, for every frame of this session
	Nodes     []AXNode          `json:"nodes"`             // Accessibility.getFullAXTree, root first
	// Marks is the cursor-pointer pass. Nil means the pass failed: no
	// role-less clickables are added, and no edit field shows its value,
	// since password fields could not be told apart.
	Marks *Marks `json:"marks,omitempty"`
}

// Frame is an iframe whose document is not in its parent's tree: a
// cross-origin iframe read over its own auto-attached session, or a
// same-origin one the main tree left out. It is inlined under the iframe
// Element (Owner, in the parent session).
type Frame struct {
	Owner int64 `json:"owner"` // the iframe element's backendNodeId in the Tab's session
	Doc
}

// Marks is what the cursor-pointer pass found in one document.
type Marks struct {
	Clickables []Clickable `json:"clickables,omitempty"`
	Passwords  []int64     `json:"passwords,omitempty"` // backendNodeIds of password inputs
}

// Clickable is an element with no interactive role that still takes
// clicks: cursor:pointer, an onclick attribute or a tabindex.
type Clickable struct {
	BackendNodeID int64  `json:"backendNodeId"`
	Text          string `json:"text,omitempty"` // its visible text, at most 80 characters
}

// AXNode is one node of Accessibility.getFullAXTree.
type AXNode struct {
	NodeID           string       `json:"nodeId"`
	Ignored          bool         `json:"ignored,omitempty"`
	Role             *AXValue     `json:"role,omitempty"`
	Name             *AXValue     `json:"name,omitempty"`
	Description      *AXValue     `json:"description,omitempty"`
	Value            *AXValue     `json:"value,omitempty"`
	Properties       []AXProperty `json:"properties,omitempty"`
	ParentID         string       `json:"parentId,omitempty"`
	ChildIDs         []string     `json:"childIds,omitempty"`
	BackendDOMNodeID int64        `json:"backendDOMNodeId,omitempty"`
	FrameID          string       `json:"frameId,omitempty"`
}

// AXValue is CDP's AXValue: a typed value. Only the value itself is read.
type AXValue struct {
	Type  string          `json:"type,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

// String renders the value as text: a string as is, a number or a
// boolean in its JSON form, anything else as "".
func (v *AXValue) String() string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(v.Value, &s) == nil {
		return s
	}
	var f float64
	if json.Unmarshal(v.Value, &f) == nil {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	var b bool
	if json.Unmarshal(v.Value, &b) == nil {
		return strconv.FormatBool(b)
	}
	return ""
}

// AXProperty is one named property of an AXNode (focused, checked, ...).
type AXProperty struct {
	Name  string   `json:"name"`
	Value *AXValue `json:"value"`
}

// Unavailable is why a browser Window's Page could not be read through its
// Browser Connection. Note is the reason, worded to follow "the Page is
// read through UI Automation because": the Window State then falls back to
// the UI Automation read with that one note.
type Unavailable struct {
	Reason string
}

func (u *Unavailable) Error() string { return u.Reason }

// InternalPageError says the Tab a Window shows is a browser page
// (chrome://newtab/, edge://settings/), which winuse never attaches to or
// reads: the extension cannot, and its content is the browser's, not a
// site's. navigate leaves it.
type InternalPageError struct {
	URL string
}

func (e *InternalPageError) Error() string {
	return "the active Tab shows a browser page (" + e.URL + "), which winuse does not read or act in"
}

// FallbackNote is the one note line a browser Window's State carries when
// its Page falls back to UI Automation.
func FallbackNote(reason string) string {
	return "the Page is read through UI Automation because " + reason + "."
}

// ErrStale reports that a Page Element Index no longer points at a live
// node: the page navigated (its document changed), the node is gone, or
// the session or the Browser Connection ended. The recovery is the same as
// for a UIA Element: read the Window State again.
var ErrStale = errors.New("the page Element is stale")
