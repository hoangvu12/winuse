package fixture

// This file builds the element tree for the two windows from a State: a
// pure, deterministic function, unit-tested offline (tree_test.go). The UIA
// provider (uia.go) and the hit-testing/painting (app.go) both consume it,
// which is what keeps what winuse sees and what the app does in lockstep.
//
// Rules encoded here (each is a documented property of the fixture):
//   - the menu bar is always present; a menu's items appear only while that
//     menu is open (one menu open at a time);
//   - the hover toolbar and its buttons appear only while revealed;
//   - the details text appears only while the expander is expanded;
//   - the list always exposes all 8 items; scrolled-out items stay in the
//     tree with IsOffscreen=true (offscreen handling is uia.go's);
//   - the dialog window's tree exists only while the dialog is open.

// Node is one element in a window's current tree.
type Node struct {
	Elem     Elem
	Children []Elem
}

// DialogRootElem is the root element of the dialog window's tree.
const DialogRootElem = ElemDialog

// BuildTree returns the main window's current tree, root first, in
// control-view order (parent before children; siblings in Specs order).
// The menu bar always exposes its three menu items; a menu's dropdown
// items appear only while that menu is open (one at a time).
func BuildTree(s State) []Node {
	nodes := []Node{
		{Elem: ElemWindow, Children: firstChildren(s)},
		{Elem: ElemMenuBar, Children: []Elem{ElemMenuFile, ElemMenuEdit, ElemMenuHelp}},
	}
	for _, menu := range []Elem{ElemMenuFile, ElemMenuEdit, ElemMenuHelp} {
		var children []Elem
		if s.OpenMenu == string(menu) {
			children = menuItems(menu)
			nodes = append(nodes, leaves(children)...)
		}
		nodes = append(nodes, Node{Elem: menu, Children: children})
	}
	if s.ToolbarVisible {
		nodes = append(nodes,
			Node{Elem: ElemHoverToolbar, Children: []Elem{ElemToolbarBold, ElemToolbarItalic, ElemToolbarUnderline}},
			Node{Elem: ElemToolbarBold}, Node{Elem: ElemToolbarItalic}, Node{Elem: ElemToolbarUnderline},
		)
	}
	nodes = append(nodes,
		Node{Elem: ElemTextField},
		Node{Elem: ElemButtonClick},
		Node{Elem: ElemButtonSaveAs},
		Node{Elem: ElemButtonDisabled},
		Node{Elem: ElemTwinA},
		Node{Elem: ElemTwinB},
		Node{Elem: ElemSoloOne},
		Node{Elem: ElemSoloTwo},
		Node{Elem: ElemCheckboxNotify},
		Node{Elem: ElemItemList, Children: ListItems},
	)
	nodes = append(nodes, leaves(ListItems)...)
	detailsChildren := []Elem{}
	if s.DetailsExpanded {
		detailsChildren = []Elem{ElemDetailsText}
	}
	nodes = append(nodes,
		Node{Elem: ElemDetailsExpander, Children: detailsChildren},
	)
	if s.DetailsExpanded {
		nodes = append(nodes, Node{Elem: ElemDetailsText})
	}
	nodes = append(nodes,
		Node{Elem: ElemCanvas},
		Node{Elem: ElemStatus},
	)
	return nodes
}

// DialogTree returns the dialog window's tree. It only exists while the
// dialog is open; the caller (the provider) must not use it otherwise.
func DialogTree(s State) []Node {
	return []Node{
		{Elem: DialogRootElem, Children: []Elem{ElemDialogLabelFileName, ElemDialogEditFileName, ElemDialogButtonSave, ElemDialogButtonCancel}},
		{Elem: ElemDialogLabelFileName},
		{Elem: ElemDialogEditFileName},
		{Elem: ElemDialogButtonSave},
		{Elem: ElemDialogButtonCancel},
	}
}

// firstChildren returns the window root's direct children in order.
func firstChildren(s State) []Elem {
	children := []Elem{ElemMenuBar}
	if s.ToolbarVisible {
		children = append(children, ElemHoverToolbar)
	}
	children = append(children,
		ElemTextField,
		ElemButtonClick, ElemButtonSaveAs, ElemButtonDisabled,
		ElemTwinA, ElemTwinB, ElemSoloOne, ElemSoloTwo,
		ElemCheckboxNotify,
		ElemItemList,
		ElemDetailsExpander,
		ElemCanvas,
		ElemStatus,
	)
	return children
}

// menuItems returns a menu's dropdown items (exposed only while open).
func menuItems(menu Elem) []Elem {
	switch menu {
	case ElemMenuFile:
		return []Elem{ElemMenuFileNew, ElemMenuFileOpen, ElemMenuFileExit}
	case ElemMenuEdit:
		return []Elem{ElemMenuEditSelectAll, ElemMenuEditClear}
	case ElemMenuHelp:
		return []Elem{ElemMenuHelpAbout}
	}
	return nil
}

func leaves(elems []Elem) []Node {
	nodes := make([]Node, len(elems))
	for i, e := range elems {
		nodes[i] = Node{Elem: e}
	}
	return nodes
}

// Parent returns the element's current parent ("" for a window root).
// Parents are the inverse of the children lists above; the unit test pins
// the two together.
func Parent(e Elem, s State, dialog bool) Elem {
	if dialog {
		return dialogParent(e)
	}
	switch e {
	case ElemWindow:
		return ""
	case ElemMenuBar, ElemTextField, ElemButtonClick, ElemButtonSaveAs, ElemButtonDisabled,
		ElemTwinA, ElemTwinB, ElemSoloOne, ElemSoloTwo, ElemCheckboxNotify, ElemItemList,
		ElemDetailsExpander, ElemCanvas, ElemStatus:
		return ElemWindow
	case ElemHoverToolbar:
		return ElemWindow
	case ElemToolbarBold, ElemToolbarItalic, ElemToolbarUnderline:
		return ElemHoverToolbar
	case ElemMenuFile, ElemMenuEdit, ElemMenuHelp:
		return ElemMenuBar
	case ElemMenuFileNew, ElemMenuFileOpen, ElemMenuFileExit:
		return ElemMenuFile
	case ElemMenuEditSelectAll, ElemMenuEditClear:
		return ElemMenuEdit
	case ElemMenuHelpAbout:
		return ElemMenuHelp
	}
	if isListItem(e) {
		return ElemItemList
	}
	if e == ElemDetailsText {
		return ElemDetailsExpander
	}
	return ""
}

func dialogParent(e Elem) Elem {
	switch e {
	case DialogRootElem:
		return ""
	case ElemDialogLabelFileName, ElemDialogEditFileName, ElemDialogButtonSave, ElemDialogButtonCancel:
		return DialogRootElem
	}
	return ""
}

func isListItem(e Elem) bool {
	for _, item := range ListItems {
		if e == item {
			return true
		}
	}
	return false
}

// InDialog reports whether the element belongs to the Save As dialog.
func InDialog(e Elem) bool { return dialogParent(e) != "" }

// TreeMap indexes a built tree for navigation: element → node, plus the
// sibling lookup the provider needs for Next/Previous.
type TreeMap struct {
	byElem map[Elem]*Node
}

// NewTreeMap indexes nodes.
func NewTreeMap(nodes []Node) *TreeMap {
	m := &TreeMap{byElem: make(map[Elem]*Node, len(nodes))}
	for i := range nodes {
		m.byElem[nodes[i].Elem] = &nodes[i]
	}
	return m
}

// Children returns the element's current children (nil for leaves and
// unknown elements).
func (m *TreeMap) Children(e Elem) []Elem {
	if n, ok := m.byElem[e]; ok {
		return n.Children
	}
	return nil
}

// Contains reports whether the element is in the tree right now.
func (m *TreeMap) Contains(e Elem) bool {
	_, ok := m.byElem[e]
	return ok
}
