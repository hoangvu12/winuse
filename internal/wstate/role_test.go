package wstate

import "testing"

// TestRoleName spot-checks the ControlType→role table (the full map is the
// researched proposal in uia-go.md §3; this pins the entries the fixture
// app and real Windows apps actually exercise) and the unknown-id fallback.
func TestRoleName(t *testing.T) {
	cases := []struct {
		id   int32
		want string
	}{
		{ControlTypeButton, "button"},
		{ControlTypeCheckBox, "check box"},
		{ControlTypeEdit, "edit"},
		{ControlTypeListItem, "list item"},
		{ControlTypeList, "list"},
		{ControlTypeMenuBar, "menu bar"},
		{ControlTypeMenuItem, "menu item"},
		{ControlTypeToolBar, "toolbar"},
		{ControlTypeText, "text"},
		{ControlTypeGroup, "group"},
		{ControlTypeCustom, "custom"},
		{ControlTypeWindow, "window"},
		{ControlTypeDocument, "document"},
		{ControlTypeSplitButton, "split button"},
		{ControlTypeTitleBar, "title bar"},
		{ControlTypeTab, "tab list"},
		{ControlTypeTabItem, "tab"},
	}
	for _, c := range cases {
		if got := RoleName(c.id); got != c.want {
			t.Errorf("RoleName(%d) = %q, want %q", c.id, got, c.want)
		}
	}
	if got := RoleName(60000); got != "control(60000)" {
		t.Errorf("RoleName(60000) = %q, want control(60000)", got)
	}
}

func TestCollapse(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"plain", "plain"},
		{"  spaces  ", "spaces"},
		{"tabs\tand\nnewlines\r\nmixed", "tabs and newlines mixed"},
		{"a\t\t \n b", "a b"},
	}
	for _, c := range cases {
		if got := Collapse(c.in); got != c.want {
			t.Errorf("Collapse(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
