// Package fixture is the winuse fixture app: a purpose-built Windows app
// whose Elements are known in advance, so end-to-end tests can assert on
// printed winuse output and on the app's end state.
//
// # Where the Element contract lives
//
// THIS FILE (elements.go) is the single source of truth for every Element
// the fixture exposes: its Name, AutomationId, role and control patterns.
// Later tickets' e2e tests (click, type, set-value, secondary actions,
// coordinate clicks, scroll/drag/hover, name:/id: references, Batches, Esc,
// overlay) must target these exact values and must not invent new ones —
// extend the table here instead, then document the change.
//
// The fixture is one Go binary (cmd/winusefixture) built with no cgo: it
// creates its windows through raw Win32 calls and exposes its Elements
// through a hand-written server-side UI Automation provider (uia.go), which
// is the only way a pure-Win32 app can expose stable, non-empty
// AutomationIds (the MSAA proxies used by standard controls leave
// AutomationId empty; the MSAA→UIA mapping has no AutomationId entry).
//
// The app reports its end state as JSON to the file given by -state; see
// state.go for that contract.
package fixture

// WindowTitle is the main window's title, also its UIA Name. It is fixed;
// e2e tests find the window by it (and by the exact Window ID reported in
// the state file).
const WindowTitle = "winuse fixture app"

// DialogTitle is the "Save As"-style dialog's title. The dialog is a real
// owned top-level window: while open it appears in `winuse list-windows` as
// its own Window of the same App (ticket 01's dialog contract).
const DialogTitle = "Save As"

// Window class names (also the ClassName the provider reports for the
// window roots).
const (
	MainWindowClass   = "WinuseFixtureWindow"
	DialogWindowClass = "WinuseFixtureDialog"
)

// Elem is an Element's AutomationId. It doubles as the Go-side element
// handle. Values are stable across runs and releases: never rename one, or
// later tickets' e2e tests and their recorded expectations break.
type Elem string

// Elements of the main window.
const (
	ElemWindow Elem = "window"

	ElemMenuBar           Elem = "menu-bar"
	ElemMenuFile          Elem = "menu-file"
	ElemMenuFileNew       Elem = "menu-file-new"
	ElemMenuFileOpen      Elem = "menu-file-open"
	ElemMenuFileExit      Elem = "menu-file-exit"
	ElemMenuEdit          Elem = "menu-edit"
	ElemMenuEditSelectAll Elem = "menu-edit-selectall"
	ElemMenuEditClear     Elem = "menu-edit-clear"
	ElemMenuHelp          Elem = "menu-help"
	ElemMenuHelpAbout     Elem = "menu-help-about"

	ElemHoverToolbar     Elem = "hover-toolbar"
	ElemToolbarBold      Elem = "toolbar-bold"
	ElemToolbarItalic    Elem = "toolbar-italic"
	ElemToolbarUnderline Elem = "toolbar-underline"

	ElemTextField Elem = "text-field"

	ElemButtonClick    Elem = "button-click"
	ElemButtonSaveAs   Elem = "button-save-as"
	ElemButtonDisabled Elem = "button-disabled"

	ElemTwinA   Elem = "twin-a"
	ElemTwinB   Elem = "twin-b"
	ElemSoloOne Elem = "solo-one"
	ElemSoloTwo Elem = "solo-two"

	ElemCheckboxNotify Elem = "checkbox-notify"

	ElemItemList    Elem = "item-list"
	ElemItemAlpha   Elem = "item-alpha"
	ElemItemBravo   Elem = "item-bravo"
	ElemItemCharlie Elem = "item-charlie"
	ElemItemDelta   Elem = "item-delta"
	ElemItemEcho    Elem = "item-echo"
	ElemItemFoxtrot Elem = "item-foxtrot"
	ElemItemGolf    Elem = "item-golf"
	ElemItemHotel   Elem = "item-hotel"

	ElemDetailsExpander Elem = "details-expander"
	ElemDetailsText     Elem = "details-text"

	ElemCanvas Elem = "canvas"
	ElemStatus Elem = "status"
)

// Elements of the "Save As" dialog.
const (
	ElemDialog              Elem = "dialog"
	ElemDialogLabelFileName Elem = "dialog-label-file-name"
	ElemDialogEditFileName  Elem = "dialog-edit-file-name"
	ElemDialogButtonSave    Elem = "dialog-button-save"
	ElemDialogButtonCancel  Elem = "dialog-button-cancel"
)

// Role is a UIA ControlType. The numeric IDs are the UIA ControlType ids
// (uia-go.md §3); winuse maps them to role strings, so the fixture uses the
// same numbers to make role rendering deterministic.
type Role int

// ControlType ids used by the fixture.
const (
	RoleButton   Role = 50000 // button
	RoleCheckBox Role = 50002 // check box
	RoleEdit     Role = 50004 // edit
	RoleListItem Role = 50007 // list item
	RoleList     Role = 50008 // list
	RoleMenuBar  Role = 50010 // menu bar
	RoleMenuItem Role = 50011 // menu item
	RoleToolBar  Role = 50021 // toolbar
	RoleText     Role = 50020 // text
	RoleGroup    Role = 50026 // group
	RoleCustom   Role = 50025 // custom
	RoleWindow   Role = 50032 // window
)

// Pattern is a control pattern the fixture implements in its UIA provider.
type Pattern int

// Patterns implemented by the fixture's provider.
const (
	PatternInvoke         Pattern = 10000 // Invoke
	PatternSelection      Pattern = 10001 // Selection (the list container)
	PatternValue          Pattern = 10002 // Value
	PatternScroll         Pattern = 10004 // Scroll
	PatternExpandCollapse Pattern = 10005 // ExpandCollapse
	PatternSelectionItem  Pattern = 10010 // SelectionItem (list items)
	PatternToggle         Pattern = 10015 // Toggle
	PatternScrollItem     Pattern = 10017 // ScrollItem (list items)
)

// Spec is one Element's contract: everything winuse and the e2e tests may
// rely on. Visible fields are the stable surface; behaviour notes describe
// what interacting with the Element does to the state file.
type Spec struct {
	Elem     Elem
	Name     string // UIA Name; "Dynamic" entries are documented below
	Role     Role
	Patterns []Pattern
	// Note documents what the Element does and any dynamic behaviour, in
	// one short line for later tickets' authors.
	Note string
}

// StatusNamePrefix is the fixed prefix of the status Element's Name. The
// Name is dynamic: StatusNamePrefix + the current status text. name:
// references to the whole name are therefore unstable by design; use
// id:status. This is the fixture's only dynamic name (documented so).
const StatusNamePrefix = "Status: "

// Specs is the complete Element table. Order matters: it is the control-view
// order the provider reports (siblings are returned in this order), and it
// doubles as the documentation of the tree. Elements with conditional
// visibility are marked in Note and enforced by tree.go.
var Specs = []Spec{
	{ElemWindow, WindowTitle, RoleWindow, nil, "main window root; the Window `winuse list-windows` shows"},

	{ElemMenuBar, "Menu Bar", RoleMenuBar, nil, "static menu bar"},
	{ElemMenuFile, "File", RoleMenuItem, []Pattern{PatternExpandCollapse}, "Expand opens the dropdown, exposing its three items"},
	{ElemMenuFileNew, "New", RoleMenuItem, []Pattern{PatternInvoke}, "visible only while File is expanded; clears the text field and the typed log"},
	{ElemMenuFileOpen, "Open", RoleMenuItem, []Pattern{PatternInvoke}, "visible only while File is expanded; opens the Save As dialog"},
	{ElemMenuFileExit, "Exit", RoleMenuItem, []Pattern{PatternInvoke}, "visible only while File is expanded; closes the app (WM_CLOSE)"},
	{ElemMenuEdit, "Edit", RoleMenuItem, []Pattern{PatternExpandCollapse}, "Expand opens the dropdown, exposing its two items"},
	{ElemMenuEditSelectAll, "Select All", RoleMenuItem, []Pattern{PatternInvoke}, "visible only while Edit is expanded; selects the text field's text"},
	{ElemMenuEditClear, "Clear", RoleMenuItem, []Pattern{PatternInvoke}, "visible only while Edit is expanded; clears the text field"},
	{ElemMenuHelp, "Help", RoleMenuItem, []Pattern{PatternExpandCollapse}, "Expand opens the dropdown, exposing its single item"},
	{ElemMenuHelpAbout, "About", RoleMenuItem, []Pattern{PatternInvoke}, "visible only while Help is expanded; sets the status text"},

	{ElemHoverToolbar, "Hover Toolbar", RoleToolBar, nil, "visible only while the mouse pointer is in the hover band under the menu bar (a hover-only toolbar)"},
	{ElemToolbarBold, "Bold", RoleButton, []Pattern{PatternInvoke}, "visible only while the hover toolbar is revealed; records lastToolbar"},
	{ElemToolbarItalic, "Italic", RoleButton, []Pattern{PatternInvoke}, "visible only while the hover toolbar is revealed; records lastToolbar"},
	{ElemToolbarUnderline, "Underline", RoleButton, []Pattern{PatternInvoke}, "visible only while the hover toolbar is revealed; records lastToolbar"},

	{ElemTextField, "Text Field", RoleEdit, []Pattern{PatternValue}, "single-line edit; typing lands here when focused; value is the state file's text"},

	{ElemButtonClick, "Click Me", RoleButton, []Pattern{PatternInvoke}, "records lastButton and sets the status text"},
	{ElemButtonSaveAs, "Save As...", RoleButton, []Pattern{PatternInvoke}, "opens the Save As dialog"},
	{ElemButtonDisabled, "Disabled Button", RoleButton, []Pattern{PatternInvoke}, "permanently disabled: IsEnabled=false, Invoke fails with UIA_E_ELEMENTNOTENABLED"},

	{ElemTwinA, "Twin", RoleButton, []Pattern{PatternInvoke}, "same Name as twin-b, different AutomationIds: a name: reference to \"Twin\" matches both, id:twin-a does not"},
	{ElemTwinB, "Twin", RoleButton, []Pattern{PatternInvoke}, "same Name as twin-a, different AutomationIds"},
	{ElemSoloOne, "Solo One", RoleButton, []Pattern{PatternInvoke}, "same AutomationId (\"solo\") as solo-two, different Names: an id: reference to \"solo\" matches both, name:Solo One does not"},
	{ElemSoloTwo, "Solo Two", RoleButton, []Pattern{PatternInvoke}, "same AutomationId (\"solo\") as solo-one, different Names"},

	{ElemCheckboxNotify, "Notify me", RoleCheckBox, []Pattern{PatternToggle}, "check box; toggle/click/space flips the state file's checkbox"},

	{ElemItemList, "Item List", RoleList, []Pattern{PatternSelection, PatternScroll}, "single-selection list with 8 items; 5 visible, items 6-8 are below the fold (IsOffscreen=true) until scrolled"},
	{ElemItemAlpha, "Alpha", RoleListItem, []Pattern{PatternSelectionItem, PatternScrollItem}, "list item; Select selects it (selection is exclusive)"},
	{ElemItemBravo, "Bravo", RoleListItem, []Pattern{PatternSelectionItem, PatternScrollItem}, "list item"},
	{ElemItemCharlie, "Charlie", RoleListItem, []Pattern{PatternSelectionItem, PatternScrollItem}, "list item"},
	{ElemItemDelta, "Delta", RoleListItem, []Pattern{PatternSelectionItem, PatternScrollItem}, "list item"},
	{ElemItemEcho, "Echo", RoleListItem, []Pattern{PatternSelectionItem, PatternScrollItem}, "list item"},
	{ElemItemFoxtrot, "Foxtrot", RoleListItem, []Pattern{PatternSelectionItem, PatternScrollItem}, "list item; below the fold initially"},
	{ElemItemGolf, "Golf", RoleListItem, []Pattern{PatternSelectionItem, PatternScrollItem}, "list item; below the fold initially"},
	{ElemItemHotel, "Hotel", RoleListItem, []Pattern{PatternSelectionItem, PatternScrollItem}, "list item; below the fold initially"},

	{ElemDetailsExpander, "Details", RoleGroup, []Pattern{PatternExpandCollapse}, "expander: Expand reveals details-text, Collapse hides it"},
	{ElemDetailsText, "Some details text.", RoleText, nil, "visible only while Details is expanded"},

	{ElemCanvas, "Canvas", RoleCustom, nil, "no patterns, no children: a sparse subtree; pointer input on it is recorded (clicks, drags, wheel) in canvas coordinates"},
	{ElemStatus, "dynamic: StatusNamePrefix + status text", RoleText, nil, "status line; the only dynamic Name — see StatusNamePrefix"},
}

// DialogSpecs is the Element table of the Save As dialog. The dialog is its
// own top-level window, so its root is a separate Window with its own tree.
var DialogSpecs = []Spec{
	{ElemDialog, DialogTitle, RoleWindow, nil, "dialog window root; IsDialog=true; disables the main window while open (modal Save As behaviour)"},
	{ElemDialogLabelFileName, "File name:", RoleText, nil, "static label"},
	{ElemDialogEditFileName, "File name", RoleEdit, []Pattern{PatternValue}, "single-line edit for the file name; value is dialogFileName in the state file"},
	{ElemDialogButtonSave, "Save", RoleButton, []Pattern{PatternInvoke}, "closes the dialog; records dialogResult=save"},
	{ElemDialogButtonCancel, "Cancel", RoleButton, []Pattern{PatternInvoke}, "closes the dialog; records dialogResult=cancel"},
}

// specIndex maps AutomationId to its Spec (built once; Specs is read-only).
var specIndex = func() map[Elem]Spec {
	m := make(map[Elem]Spec, len(Specs)+len(DialogSpecs))
	for _, s := range Specs {
		m[s.Elem] = s
	}
	for _, s := range DialogSpecs {
		m[s.Elem] = s
	}
	return m
}()

// AutomationID returns the AutomationId the provider exposes for an
// element. It equals the element's handle for every element except the
// deliberately ambiguous solo pair, which shares one AutomationId so id:
// references can be tested for ambiguity errors.
func AutomationID(e Elem) string {
	if e == ElemSoloOne || e == ElemSoloTwo {
		return "solo"
	}
	return string(e)
}

// SpecFor returns the Element contract for an AutomationId.
func SpecFor(e Elem) (Spec, bool) {
	s, ok := specIndex[e]
	return s, ok
}

// Name returns the element's UIA Name. Only the status Element is dynamic
// (its Name is StatusNamePrefix + status, the argument here).
func Name(e Elem, status string) string {
	s, ok := specIndex[e]
	if !ok {
		return ""
	}
	if e == ElemStatus {
		return StatusNamePrefix + status
	}
	return s.Name
}

// HasPattern reports whether the element offers the control pattern.
func HasPattern(e Elem, p Pattern) bool {
	s, ok := specIndex[e]
	if !ok {
		return false
	}
	for _, q := range s.Patterns {
		if q == p {
			return true
		}
	}
	return false
}

// ListItems are the list's items in order.
var ListItems = []Elem{
	ElemItemAlpha, ElemItemBravo, ElemItemCharlie, ElemItemDelta,
	ElemItemEcho, ElemItemFoxtrot, ElemItemGolf, ElemItemHotel,
}

// ListVisibleItems is how many items fit in the list's viewport. The rest
// are offscreen until the list is scrolled (listFirstVisible in the state).
const ListVisibleItems = 5

// ElemID assigns each element a fixed integer: the second runtime-id part
// after UiaAppendRuntimeId (per window). Ids are stable and unique within a
// window.
var elemID = func() map[Elem]int32 {
	m := make(map[Elem]int32, len(Specs)+len(DialogSpecs))
	for i, s := range Specs {
		m[s.Elem] = int32(i + 1)
	}
	// Dialog elements restart at 1000: unique within their own window and
	// unmistakably distinct from main-window ids.
	for i, s := range DialogSpecs {
		m[s.Elem] = int32(1000 + i + 1)
	}
	return m
}()
