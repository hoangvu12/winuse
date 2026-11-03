package win

import "testing"

func TestMenuFlagsDropASubmenuItemCount(t *testing.T) {
	cases := []struct {
		name  string
		state uint32
		want  uint32
	}{
		{"a command", 0, 0},
		{"a grey command", MenuGrayed, MenuGrayed},
		{"a separator", MenuSeparator, MenuSeparator},
		{"an owner-drawn command", MenuOwnerDraw | MenuDisabled, MenuOwnerDraw | MenuDisabled},
		// A submenu of 9 items: the count 9 sits in the high byte, and
		// would read as MF_OWNERDRAW and MF_SEPARATOR if it were kept.
		{"a submenu", 9<<8 | MenuPopup, MenuPopup},
		{"a grey submenu", 9<<8 | MenuPopup | MenuGrayed, MenuPopup | MenuGrayed},
	}
	for _, c := range cases {
		if got := menuFlags(c.state); got != c.want {
			t.Errorf("%s: menuFlags(%#x) = %#x, want %#x", c.name, c.state, got, c.want)
		}
	}
}

func TestMenuItemKinds(t *testing.T) {
	sep := MenuItem{Flags: MenuSeparator}
	if !sep.Separator() || sep.Popup() || sep.Unavailable() {
		t.Errorf("separator: %+v reads Separator %v, Popup %v, Unavailable %v", sep, sep.Separator(), sep.Popup(), sep.Unavailable())
	}
	sub := MenuItem{Submenu: 0x1234}
	if !sub.Popup() {
		t.Error("an item with a submenu must read as Popup")
	}
	for _, f := range []uint32{MenuGrayed, MenuDisabled, MenuGrayed | MenuDisabled} {
		if !(MenuItem{Flags: f}).Unavailable() {
			t.Errorf("flags %#x must read as Unavailable", f)
		}
	}
	if (MenuItem{ID: 7}).Unavailable() {
		t.Error("a plain command must be available")
	}
}

// A WinForms ContextMenuStrip opens in a drop-down whose class style has
// CS_SAVEBITS (hm-09b measured "WindowsForms10.Window.20808"); a Form or
// a control does not.
func TestWinFormsDropDown(t *testing.T) {
	cases := []struct {
		class string
		want  bool
	}{
		{"WindowsForms10.Window.20808.app.0.34f5582_r6_ad1", true},
		{"WindowsForms10.Window.808.app.0.34f5582_r6_ad1", true},
		{"WindowsForms10.Window.8.app.0.34f5582_r6_ad1", false}, // a Form
		{"WindowsForms10.EDIT.app.0.34f5582_r6_ad1", false},
		{"WindowsForms10.Window.zz.app.0", false},
		{"WindowsForms10.Window.20808", false},
		{MenuClass, false},
		{"", false},
	}
	for _, c := range cases {
		if got := WinFormsDropDown(c.class); got != c.want {
			t.Errorf("WinFormsDropDown(%q) = %v, want %v", c.class, got, c.want)
		}
	}
}
