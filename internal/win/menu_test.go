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
