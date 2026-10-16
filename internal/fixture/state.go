package fixture

import "encoding/json"

// State is the fixture app's end state, written as JSON to the file given
// by the -state flag. End-to-end tests assert on these fields after driving
// winuse; winuse itself never reads it. The file is rewritten atomically
// (write + rename) on every change, so a reader never sees a partial file;
// Generation increments on every write so tests can wait for quiescence.
type State struct {
	App   string `json:"app"`   // "winuse-fixture"
	Title string `json:"title"` // WindowTitle

	// WindowID is the main window's Window ID: the HWND as an unsigned
	// decimal, the same number `winuse list-windows` prints. Zero until the
	// window exists.
	WindowID uint64 `json:"windowId"`

	// The Save As dialog. While DialogOpen is true the dialog is a visible
	// owned top-level window and the main window is disabled (modal
	// behaviour); DialogWindowID is its Window ID, DialogResult the last
	// way it closed ("save" or "cancel", "" while open or never opened),
	// DialogFileName the file name edit's content.
	DialogOpen     bool   `json:"dialogOpen"`
	DialogWindowID uint64 `json:"dialogWindowId"`
	DialogResult   string `json:"dialogResult"`
	DialogFileName string `json:"dialogFileName"`

	// Text is the main window's Text Field content; Typed is the raw
	// WM_CHAR stream (capped, last 200 runes) so press-key/type tests can
	// tell typed characters apart from focus side effects.
	Text  string `json:"text"`
	Typed string `json:"typed"`

	// Checkbox is the check box's toggle state.
	Checkbox bool `json:"checkbox"`

	// Last* record the last invocation of each kind, as the element's
	// Name ("" before the first one): LastButton (buttons + toolbar
	// buttons), LastMenu (menu item AutomationIds), LastKey (key names,
	// e.g. "Return", "Escape").
	LastButton  string `json:"lastButton"`
	LastToolbar string `json:"lastToolbar"`
	LastMenu    string `json:"lastMenu"`
	LastKey     string `json:"lastKey"`

	// List: SelectedItem is the selected item's Name ("" if none);
	// ListFirstVisible is the index of the first visible item (scroll
	// position; 0 initially).
	SelectedItem     string `json:"selectedItem"`
	ListFirstVisible int    `json:"listFirstVisible"`

	// DetailsExpanded: whether the Details expander is expanded.
	DetailsExpanded bool `json:"detailsExpanded"`

	// ToolbarVisible: whether the hover-only toolbar is currently revealed.
	ToolbarVisible bool `json:"toolbarVisible"`

	// OpenMenu: the AutomationId of the menu currently open ("" if none).
	OpenMenu string `json:"openMenu"`

	// Canvas input, in canvas-local coordinates (the canvas rect's
	// top-left is 0,0). CanvasClicks records every left click (single and
	// double both append; CanvasDoubleClicks marks the doubles),
	// CanvasRightClicks right clicks, CanvasDrags from/to pairs of left
	// drags, CanvasScrolls the accumulated wheel deltas per event
	// (positive x scrolls right, positive y scrolls up — SendInput wheel
	// semantics). CanvasRect is the canvas region in SCREEN coordinates
	// (left, top, right, bottom; physical pixels, per-monitor-v2 aware) so
	// tests can convert between canvas and screen coordinates; it updates
	// when the window moves.
	CanvasClicks       []Point  `json:"canvasClicks"`
	CanvasDoubleClicks int      `json:"canvasDoubleClicks"`
	CanvasRightClicks  []Point  `json:"canvasRightClicks"`
	CanvasDrags        []Drag   `json:"canvasDrags"`
	CanvasScrolls      []Point  `json:"canvasScrolls"`
	CanvasRect         [4]int32 `json:"canvasRect"`

	// Generation increments on every state write.
	Generation uint64 `json:"generation"`
}

// Point is a canvas-local point.
type Point struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// Drag is a canvas-local left-drag.
type Drag struct {
	From Point `json:"from"`
	To   Point `json:"to"`
}

// jsonMarshal is split out so the offline unit tests can pin the JSON field
// names (e2e tests read them by string key).
func jsonMarshal(s *State) ([]byte, error) {
	return json.Marshal(s)
}

// initial returns the state a freshly started fixture reports.
func initial() State {
	return State{App: "winuse-fixture", Title: WindowTitle}
}
