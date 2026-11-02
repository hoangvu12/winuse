package win

import "testing"

// TestSelectionNotice pins which windows the select route notifies about
// and what it sends: a classic ComboBox, and a ListBox only with
// LBS_NOTIFY, get MAKEWPARAM(id, SELCHANGE); a UIA provider's list (no
// window of its own, or another class) gets nothing.
func TestSelectionNotice(t *testing.T) {
	cases := []struct {
		name  string
		c     Control
		want  uintptr
		sends bool
	}{
		{name: "a classic combo", c: Control{HWND: 7, ID: 1003, Class: "ComboBox"}, want: 0x000103EB, sends: true},
		{name: "class names match in any case", c: Control{HWND: 7, ID: 5, Class: "COMBOBOX"}, want: 0x00010005, sends: true},
		{name: "a ListBox with LBS_NOTIFY", c: Control{HWND: 7, ID: 1004, Class: "ListBox", Style: lbsNotify}, want: 0x000103EC, sends: true},
		{name: "a ListBox without LBS_NOTIFY tells nobody", c: Control{HWND: 7, ID: 1004, Class: "ListBox"}},
		{name: "an Element drawn inside another window", c: Control{Class: "ComboBox"}},
		{name: "a combo's drop-down list", c: Control{HWND: 7, Class: "ComboLBox", Style: lbsNotify}},
		{name: "a list view", c: Control{HWND: 7, Class: "SysListView32"}},
		{name: "a provider-drawn window", c: Control{HWND: 7, Class: "WinuseFixture"}},
	}
	for _, tc := range cases {
		got, sends := selectionNotice(tc.c)
		if sends != tc.sends || got != tc.want {
			t.Errorf("%s: selectionNotice = %#x, %v; want %#x, %v", tc.name, got, sends, tc.want, tc.sends)
		}
	}
}

// TestNotifySelectionChangeLeavesNoWindowAlone: an Element with no window
// of its own (NativeWindowHandle 0) is notified about nothing, and no
// message is attempted.
func TestNotifySelectionChangeLeavesNoWindowAlone(t *testing.T) {
	if err := NotifySelectionChange(0); err != nil {
		t.Errorf("NotifySelectionChange(0) = %v, want nil", err)
	}
}

// TestDropDownList pins which combos set-value picks an item in by name
// although their Value reads writable: a classic ComboBox with
// CBS_DROPDOWNLIST only.
func TestDropDownList(t *testing.T) {
	cases := []struct {
		name string
		c    Control
		want bool
	}{
		{name: "CBS_DROPDOWNLIST", c: Control{HWND: 7, Class: "ComboBox", Style: 0x50010203}, want: true},
		{name: "CBS_DROPDOWN has an edit field", c: Control{HWND: 7, Class: "ComboBox", Style: 0x50010202}},
		{name: "CBS_SIMPLE has an edit field", c: Control{HWND: 7, Class: "ComboBox", Style: 0x50010201}},
		{name: "a ListBox", c: Control{HWND: 7, Class: "ListBox", Style: 0x0003}},
		{name: "no window of its own", c: Control{Class: "ComboBox", Style: 0x0003}},
	}
	for _, tc := range cases {
		if got := tc.c.DropDownList(); got != tc.want {
			t.Errorf("%s: DropDownList = %v, want %v", tc.name, got, tc.want)
		}
	}
}
