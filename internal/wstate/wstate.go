// Package wstate models the Window State: one Window's accessibility tree
// of Elements as observable data, plus its Codex-format text rendering.
//
// The model is deliberately decoupled from how a tree is obtained. The UI
// Automation walk (internal/uia) fills it from a live Window; the recorder
// writes it as a JSON fixture file; the seam-2 offline tests load recorded
// or hand-authored fixtures and render them (spec: Testing Decisions, seam
// 2). Later tickets (05 click, 07 set-value and Secondary Actions, 11
// name:/id: references) reuse this model as the Element Index space: the
// slice position of an Element *is* its Element Index, and it is stable for
// the Window State that produced it.
//
// # Rendering format
//
// The text format follows Codex Computer Use's Windows tree format
// (research notes, codex-surface.md §2):
//
//	Window: "<title>", App: <exe file name>.
//	→0 window <name> [Secondary Actions: Raise]
//	→→1 text <name> ID: <AutomationId>
//	...
//
//	Element lines are depth-first pre-order, numbered from 0 at the root
//	window, one TAB per depth level (the root itself gets one TAB), with the
//	grammar:
//
//	<index> <role>[ (<states>)][ <name>][ Description: <HelpText>][ Value: <value>][ on|off|indeterminate][ Secondary Actions: A, B][ ID: <AutomationId>][ (truncated: <limit>, omitted <n> children)]
//
//	Divergence from Codex (orchestrator decision): role strings are English
//	names derived from the numeric UIA ControlType, never
//	LocalizedControlType — locale-dependent roles are a documented Codex bug.
package wstate

import (
	"strconv"
	"strings"
	"time"
)

// FixtureFormat is the version of the recorded-tree fixture JSON. LoadFixture
// refuses other versions.
const FixtureFormat = 1

// Truncation marks an Element whose direct children were cut by a walk
// budget, rendered as the Codex truncation suffix
// "(truncated: <limit>, omitted <n> children)". Limit is the budget that was
// hit (its value, as a string), OmittedChildren the number of direct
// children not visited.
type Truncation struct {
	Limit           string `json:"limit"`
	OmittedChildren int    `json:"omittedChildren"`
}

// Element is one node of the tree. Its Element Index is its position in
// Tree.Elements (depth-first pre-order); the walk guarantees that the live
// element handles behind a Window State share that order, so an Element
// Index resolves to the same Element for the Window State's lifetime.
//
// Fields beyond role/name/AutomationId/basic state are part of the Codex
// line grammar; the tree walk populates them from later tickets on (07 adds
// Secondary Actions and the toggled/selected/expanded/value states), but the
// model and renderer support the full grammar from the start so recorded
// fixtures can pin the whole format.
type Element struct {
	Depth int    `json:"depth"` // 0 = the root window (rendered with one TAB)
	Role  string `json:"role"`  // English role from the numeric ControlType (RoleName)

	// Identification.
	Name         string `json:"name,omitempty"`         // UIA Name, whitespace-collapsed, omitted when empty
	AutomationId string `json:"automationId,omitempty"` // UIA AutomationId, rendered as "ID: <value>"
	Description  string `json:"description,omitempty"`  // UIA HelpText, rendered as "Description: <value>"

	// Basic state (ticket 03).
	Disabled bool `json:"disabled,omitempty"` // rendered as the "(disabled)" state

	// Rich state (populated by the walk from ticket 07; fixtures may set it).
	Selectable  bool   `json:"selectable,omitempty"` // SelectionItemPattern availability
	Selected    bool   `json:"selected,omitempty"`
	ExpandState string `json:"expandState,omitempty"` // "collapsed" | "expanded" | "partially expanded"
	Settable    string `json:"settable,omitempty"`    // "string" (ValuePattern) | "float" (RangeValue)
	Value       string `json:"value,omitempty"`
	Toggle      string `json:"toggle,omitempty"` // "on" | "off" | "indeterminate"

	// SecondaryActions are the Element's Secondary Actions (Raise, Scroll
	// Down, Expand, ...), rendered as "Secondary Actions: A, B".
	SecondaryActions []string `json:"secondaryActions,omitempty"`

	// Truncated is set when a walk budget cut this Element's children.
	Truncated *Truncation `json:"truncated,omitempty"`
}

// Tree is one Window State: the header data plus the depth-first Element
// list. It doubles as the recorder's fixture file format (JSON).
type Tree struct {
	Format       int        `json:"format,omitempty"`   // fixture format version (FixtureFormat)
	Source       string     `json:"source,omitempty"`   // provenance of a fixture file
	Captured     *time.Time `json:"captured,omitempty"` // when a fixture was recorded live
	Title        string     `json:"title"`              // Window title for the header line
	App          string     `json:"app"`                // App name for the header line (the exe file name)
	Elements     []Element  `json:"elements"`           // depth-first pre-order; position = Element Index
	FocusedIndex int        `json:"focusedIndex"`       // focused Element's index; -1 = none (the zero value claims the root, so always set it)
	Notes        []string   `json:"notes,omitempty"`    // partial-tree / sparse-tree notes, "Note: <line>" each
}

// Index returns the Element Index of the focused Element, or -1.
func (t *Tree) Focused() int {
	if t.FocusedIndex >= 0 && t.FocusedIndex < len(t.Elements) {
		return t.FocusedIndex
	}
	return -1
}

// Collapse whitespace-collapses s: runs of whitespace (spaces, tabs,
// newlines) become single spaces, with ends trimmed. Codex's tree format
// puts each Element on one line, so every rendered string is collapsed.
func Collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ControlType ids (UIA, contiguous 50000–50040; from the Windows SDK
// UIAutomationClient.h). Role strings follow the project's researched
// proposal (uia-go notes §3): English words keyed on the numeric id only.
const (
	ControlTypeButton       = 50000
	ControlTypeCalendar     = 50001
	ControlTypeCheckBox     = 50002
	ControlTypeComboBox     = 50003
	ControlTypeEdit         = 50004
	ControlTypeHyperlink    = 50005
	ControlTypeImage        = 50006
	ControlTypeListItem     = 50007
	ControlTypeList         = 50008
	ControlTypeMenu         = 50009
	ControlTypeMenuBar      = 50010
	ControlTypeMenuItem     = 50011
	ControlTypeProgressBar  = 50012
	ControlTypeRadioButton  = 50013
	ControlTypeScrollBar    = 50014
	ControlTypeSlider       = 50015
	ControlTypeSpinner      = 50016
	ControlTypeStatusBar    = 50017
	ControlTypeTab          = 50018
	ControlTypeTabItem      = 50019
	ControlTypeText         = 50020
	ControlTypeToolBar      = 50021
	ControlTypeToolTip      = 50022
	ControlTypeTree         = 50023
	ControlTypeTreeItem     = 50024
	ControlTypeCustom       = 50025
	ControlTypeGroup        = 50026
	ControlTypeThumb        = 50027
	ControlTypeDataGrid     = 50028
	ControlTypeDataItem     = 50029
	ControlTypeDocument     = 50030
	ControlTypeSplitButton  = 50031
	ControlTypeWindow       = 50032
	ControlTypePane         = 50033
	ControlTypeHeader       = 50034
	ControlTypeHeaderItem   = 50035
	ControlTypeTable        = 50036
	ControlTypeTitleBar     = 50037
	ControlTypeSeparator    = 50038
	ControlTypeSemanticZoom = 50039
	ControlTypeAppBar       = 50040
)

// roleNames maps the numeric ControlType to the English role string. The
// map is keyed on the number only: logic and display never depend on the
// locale (Codex's LocalizedControlType bug, codex-surface.md §6).
var roleNames = map[int32]string{
	ControlTypeButton:       "button",
	ControlTypeCalendar:     "calendar",
	ControlTypeCheckBox:     "check box",
	ControlTypeComboBox:     "combo box",
	ControlTypeEdit:         "edit",
	ControlTypeHyperlink:    "link",
	ControlTypeImage:        "image",
	ControlTypeListItem:     "list item",
	ControlTypeList:         "list",
	ControlTypeMenu:         "menu",
	ControlTypeMenuBar:      "menu bar",
	ControlTypeMenuItem:     "menu item",
	ControlTypeProgressBar:  "progress bar",
	ControlTypeRadioButton:  "radio button",
	ControlTypeScrollBar:    "scroll bar",
	ControlTypeSlider:       "slider",
	ControlTypeSpinner:      "spinner",
	ControlTypeStatusBar:    "status bar",
	ControlTypeTab:          "tab list",
	ControlTypeTabItem:      "tab",
	ControlTypeText:         "text",
	ControlTypeToolBar:      "toolbar",
	ControlTypeToolTip:      "tooltip",
	ControlTypeTree:         "tree",
	ControlTypeTreeItem:     "tree item",
	ControlTypeCustom:       "custom",
	ControlTypeGroup:        "group",
	ControlTypeThumb:        "thumb",
	ControlTypeDataGrid:     "data grid",
	ControlTypeDataItem:     "data item",
	ControlTypeDocument:     "document",
	ControlTypeSplitButton:  "split button",
	ControlTypeWindow:       "window",
	ControlTypePane:         "pane",
	ControlTypeHeader:       "header",
	ControlTypeHeaderItem:   "header item",
	ControlTypeTable:        "table",
	ControlTypeTitleBar:     "title bar",
	ControlTypeSeparator:    "separator",
	ControlTypeSemanticZoom: "semantic zoom",
	ControlTypeAppBar:       "app bar",
}

// RoleName returns the English role string for a UIA ControlType id. An
// unknown id maps to "control(<id>)"; the Element is never dropped.
func RoleName(controlType int32) string {
	if name, ok := roleNames[controlType]; ok {
		return name
	}
	return "control(" + strconv.FormatInt(int64(controlType), 10) + ")"
}
