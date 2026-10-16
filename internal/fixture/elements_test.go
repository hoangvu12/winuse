package fixture

import (
	"encoding/json"
	"testing"
)

// These tests pin the Element contract offline: they run as part of the
// default `go test ./...`, never touch the desktop (no window is created;
// Main is never called) and are safe to run any time. What they check is
// exactly what later tickets' e2e tests rely on: names, AutomationIds and
// the dynamic tree stay as documented in elements.go.

func TestAutomationIDsStable(t *testing.T) {
	for _, s := range append(append([]Spec{}, Specs...), DialogSpecs...) {
		if AutomationID(s.Elem) == "" {
			t.Errorf("%s: empty AutomationId", s.Elem)
		}
		if s.Name == "" {
			t.Errorf("%s: empty Name", s.Elem)
		}
		if s.Role == 0 {
			t.Errorf("%s: zero role", s.Elem)
		}
	}
	// AutomationIds are unique except the deliberately ambiguous solo pair.
	seen := map[string]string{}
	for _, s := range append(append([]Spec{}, Specs...), DialogSpecs...) {
		id := AutomationID(s.Elem)
		if prev, ok := seen[id]; ok {
			if id != "solo" {
				t.Errorf("AutomationId %q shared by %s and %s", id, prev, s.Elem)
			}
		} else {
			seen[id] = string(s.Elem)
		}
	}
	if AutomationID(ElemSoloOne) != "solo" || AutomationID(ElemSoloTwo) != "solo" {
		t.Error("the solo pair must share AutomationId \"solo\"")
	}
}

func TestNamesStable(t *testing.T) {
	// Names are unique except the deliberately ambiguous twin pair.
	seen := map[string]string{}
	for _, s := range append(append([]Spec{}, Specs...), DialogSpecs...) {
		if prev, ok := seen[s.Name]; ok && s.Name != "Twin" && s.Name != "dynamic: StatusNamePrefix + status text" {
			t.Errorf("Name %q shared by %s and %s", s.Name, prev, s.Elem)
		}
		seen[s.Name] = string(s.Elem)
	}
	if Name(ElemTwinA, "") != "Twin" || Name(ElemTwinB, "") != "Twin" {
		t.Error("the twin pair must share the Name \"Twin\"")
	}
	if Name(ElemStatus, "ready") != "Status: ready" {
		t.Errorf("status Name = %q, want %q", Name(ElemStatus, "ready"), "Status: ready")
	}
}

func TestElemIDsUniquePerWindow(t *testing.T) {
	seenMain := map[int32]string{}
	for i, s := range Specs {
		if id := int32(i + 1); elemID[s.Elem] != id {
			t.Errorf("%s: elemID = %d, want %d", s.Elem, elemID[s.Elem], id)
		} else if prev, ok := seenMain[id]; ok {
			t.Errorf("elemID %d reused by %s and %s", id, prev, s.Elem)
		} else {
			seenMain[id] = string(s.Elem)
		}
	}
	seenDialog := map[int32]string{}
	for i, s := range DialogSpecs {
		id := elemID[s.Elem]
		if id != int32(1000+i+1) {
			t.Errorf("%s: elemID = %d", s.Elem, id)
		}
		if prev, ok := seenDialog[id]; ok {
			t.Errorf("dialog elemID %d reused by %s and %s", id, prev, s.Elem)
		}
		seenDialog[id] = string(s.Elem)
	}
}

func TestBuildTreeInitial(t *testing.T) {
	s := initial()
	nodes := BuildTree(s)
	m := NewTreeMap(nodes)
	if !m.Contains(ElemWindow) || !m.Contains(ElemMenuBar) || !m.Contains(ElemTextField) {
		t.Fatal("initial tree misses core elements")
	}
	for _, hidden := range []Elem{
		ElemMenuFileNew, ElemMenuFileOpen, ElemMenuFileExit,
		ElemMenuEditSelectAll, ElemMenuEditClear, ElemMenuHelpAbout,
		ElemHoverToolbar, ElemToolbarBold, ElemToolbarItalic, ElemToolbarUnderline,
		ElemDetailsText,
	} {
		if m.Contains(hidden) {
			t.Errorf("initial tree must not expose %s", hidden)
		}
	}
	for _, item := range ListItems {
		if !m.Contains(item) {
			t.Errorf("initial tree must expose list item %s", item)
		}
	}
	if len(m.Children(ElemWindow)) == 0 {
		t.Error("window root has no children")
	}
}

func TestBuildTreeDynamicElements(t *testing.T) {
	s := initial()
	s.OpenMenu = string(ElemMenuFile)
	s.ToolbarVisible = true
	s.DetailsExpanded = true
	m := NewTreeMap(BuildTree(s))
	for _, present := range []Elem{
		ElemMenuFileNew, ElemMenuFileOpen, ElemMenuFileExit,
		ElemHoverToolbar, ElemToolbarBold, ElemToolbarItalic, ElemToolbarUnderline,
		ElemDetailsText,
	} {
		if !m.Contains(present) {
			t.Errorf("tree with everything open must expose %s", present)
		}
	}
	// One menu open at a time: Edit's items stay hidden.
	if m.Contains(ElemMenuEditSelectAll) {
		t.Error("Edit menu items exposed while File is the open menu")
	}
}

func TestParentsInvertChildren(t *testing.T) {
	states := []State{
		initial(),
		func() State { s := initial(); s.OpenMenu = string(ElemMenuEdit); return s }(),
		func() State { s := initial(); s.OpenMenu = string(ElemMenuHelp); s.ToolbarVisible = true; return s }(),
		func() State { s := initial(); s.DetailsExpanded = true; return s }(),
	}
	for _, s := range states {
		for _, dialog := range []bool{false, true} {
			var nodes []Node
			if dialog {
				nodes = DialogTree(s)
			} else {
				nodes = BuildTree(s)
			}
			m := NewTreeMap(nodes)
			for _, n := range nodes {
				p := Parent(n.Elem, s, dialog)
				if p == "" && n.Elem != providerRootElem(dialog) {
					t.Errorf("%s: no parent", n.Elem)
					continue
				}
				if p == "" {
					continue
				}
				found := false
				for _, c := range m.Children(p) {
					if c == n.Elem {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s's parent %s does not list it as a child (state %+v, dialog=%v)", n.Elem, p, s, dialog)
				}
			}
		}
	}
}

func TestMenuChildren(t *testing.T) {
	if got := menuItems(ElemMenuFile); len(got) != 3 || got[0] != ElemMenuFileNew {
		t.Errorf("File menu items = %v", got)
	}
	if got := menuItems(ElemMenuEdit); len(got) != 2 {
		t.Errorf("Edit menu items = %v", got)
	}
	if got := menuItems(ElemMenuHelp); len(got) != 1 {
		t.Errorf("Help menu items = %v", got)
	}
	if got := menuItems(ElemTextField); got != nil {
		t.Errorf("non-menu element has menu items: %v", got)
	}
}

func TestStateJSONFieldNames(t *testing.T) {
	// The e2e runner (and later tickets' tests) read these fields by name;
	// changing one is a contract break, so pin them.
	s := initial()
	s.WindowID = 42
	s.Text = "hello"
	s.Checkbox = true
	s.CanvasClicks = []Point{{X: 3, Y: 4}}
	b, err := jsonMarshal(&s)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"app", "title", "windowId", "dialogOpen", "dialogWindowId", "dialogResult",
		"dialogFileName", "text", "typed", "checkbox", "lastButton", "lastToolbar",
		"lastMenu", "lastKey", "selectedItem", "listFirstVisible", "detailsExpanded",
		"toolbarVisible", "openMenu", "canvasClicks", "canvasDoubleClicks",
		"canvasRightClicks", "canvasDrags", "canvasScrolls", "canvasRect", "generation",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("state JSON is missing key %q", key)
		}
	}
	var c map[string]any
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if len(c) == 0 {
		t.Error("state JSON is empty")
	}
}

func TestKeyNames(t *testing.T) {
	cases := map[uint32]string{
		0x08: "BackSpace", 0x09: "Tab", 0x0D: "Return", 0x1B: "Escape",
		0x20: "space", 0x25: "Left", 0x26: "Up", 0x27: "Right", 0x28: "Down",
		0x2E: "Delete", 0x41: "a", 0x5A: "z", 0x30: "0", 0x70: "F1", 0x7B: "F12",
	}
	for vk, want := range cases {
		if got := keyName(vk); got != want {
			t.Errorf("keyName(0x%02X) = %q, want %q", vk, got, want)
		}
	}
}

func TestInitialAppState(t *testing.T) {
	s := initial()
	if s.App != "winuse-fixture" || s.Title != WindowTitle {
		t.Errorf("initial state = %+v", s)
	}
	if s.DialogOpen || s.ToolbarVisible || s.OpenMenu != "" {
		t.Error("initial state must be fully closed")
	}
	if s.ListFirstVisible != 0 {
		t.Error("list starts unscrolled")
	}
}

func TestInDialog(t *testing.T) {
	if !InDialog(ElemDialogButtonSave) || InDialog(ElemButtonClick) {
		t.Error("InDialog misclassifies elements")
	}
}

func TestSpecForCoversTree(t *testing.T) {
	// Every element any tree can expose has a Spec.
	s := initial()
	s.OpenMenu = string(ElemMenuFile)
	s.ToolbarVisible = true
	s.DetailsExpanded = true
	for _, n := range append(BuildTree(s), DialogTree(s)...) {
		if _, ok := SpecFor(n.Elem); !ok {
			t.Errorf("element %s in tree has no Spec", n.Elem)
		}
	}
}
