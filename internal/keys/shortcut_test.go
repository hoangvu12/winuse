package keys

import "testing"

func TestParseShortcutLabel(t *testing.T) {
	ctrlS := Shortcut{Ctrl: true, VK: 'S'}
	cases := []struct {
		label string
		want  Shortcut
		ok    bool
	}{
		{"Ctrl+S", ctrlS, true},
		{"ctrl+s", ctrlS, true},
		{"Ctrl+Shift+O", Shortcut{Ctrl: true, Shift: true, VK: 'O'}, true},
		{"Shift+Ctrl+O", Shortcut{Ctrl: true, Shift: true, VK: 'O'}, true},
		{"Alt+F4", Shortcut{Alt: true, VK: 0x73}, true},
		{"Ctrl+PgUp", Shortcut{Ctrl: true, VK: 0x21}, true},
		{"Ctrl+Z", Shortcut{Ctrl: true, VK: 'Z'}, true},
		{"S", Shortcut{}, false},     // a mnemonic letter
		{"F5", Shortcut{}, false},    // no modifier: no shortcut to look up
		{"Ctrl+", Shortcut{}, false}, // malformed
		{"Ctrl+A+B", Shortcut{}, false},
		{"", Shortcut{}, false},
	}
	for _, c := range cases {
		got, ok := ParseShortcutLabel(c.label)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseShortcutLabel(%q) = %+v, %v, want %+v, %v", c.label, got, ok, c.want, c.ok)
		}
	}
}

func TestShortcutOfMatchesLabels(t *testing.T) {
	for _, chord := range []string{"Control_L+s", "Control_R+s", "s+Control_L"} {
		c, err := Parse(chord)
		if err != nil {
			t.Fatal(err)
		}
		s, ok := ShortcutOf(c)
		if !ok || s != (Shortcut{Ctrl: true, VK: 'S'}) {
			t.Errorf("ShortcutOf(%s) = %+v, %v", chord, s, ok)
		}
	}
	c, _ := Parse("Return")
	if _, ok := ShortcutOf(c); ok {
		t.Error("a lone key is no shortcut")
	}
}

func TestLabelShortcutsReadEveryPlace(t *testing.T) {
	ctrlS := Shortcut{Ctrl: true, VK: 'S'}
	ctrlA := Shortcut{Ctrl: true, VK: 'A'}
	cases := []struct {
		accel, name string
		menuItem    bool
		want        Shortcut
		shown       bool
	}{
		{"Ctrl+S", "Save", true, ctrlS, true},           // WinForms, WinUI, Paint
		{"", "&Save\tCtrl+S", true, ctrlS, true},        // a classic menu item's name
		{"a", "Select all Ctrl+A", true, ctrlA, true},   // Chromium: the mnemonic in AcceleratorKey, the shortcut in the name
		{"a", "Select all Ctrl+A", false, ctrlA, false}, // the name's last word counts on menu items only
		{"S", "Save", true, ctrlS, false},               // a mnemonic is no shortcut
		{"Ctrl+Shift+S", "Save As", true, ctrlS, false}, // another shortcut
	}
	for _, c := range cases {
		if got := c.want.ShownBy(c.accel, c.name, c.menuItem); got != c.shown {
			t.Errorf("%+v.ShownBy(%q, %q, %v) = %v, want %v", c.want, c.accel, c.name, c.menuItem, got, c.shown)
		}
	}
}
