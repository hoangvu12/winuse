package keys

// Shortcut labels (headless-max ticket 06): the shortcut an App shows for
// a command, read from UI Automation's AcceleratorKey ("Ctrl+S",
// "Ctrl+Shift+O"), from a classic menu item's name after its tab
// ("&Save\tCtrl+S"), or from the end of a Chromium menu item's name
// ("Select all Ctrl+A"). press-key compares them with its chord to find
// the command and invoke it instead of pressing keys (probes round 5,
// section 4).

import "strings"

// Shortcut is a chord reduced to what a label says: the modifiers held
// (left, right or generic alike) and the one key.
type Shortcut struct {
	Ctrl, Shift, Alt bool
	VK               uint16
}

// ShortcutOf reduces a chord to its Shortcut: false when the chord has no
// modifier (a lone key is no shortcut to look up) or not exactly one key
// besides its modifiers.
func ShortcutOf(c Chord) (Shortcut, bool) {
	var s Shortcut
	keys := 0
	for _, k := range c {
		switch k.VK {
		case 0x11, vkLcontrol, vkRcontrol:
			s.Ctrl = true
		case 0x10, vkLshift, vkRshift:
			s.Shift = true
		case 0x12, vkLmenu, vkRmenu:
			s.Alt = true
		default:
			s.VK = k.VK
			keys++
		}
	}
	return s, keys == 1 && (s.Ctrl || s.Shift || s.Alt)
}

// ParseShortcutLabel reads one shortcut label such as "Ctrl+Shift+S",
// "Alt+F4" or "Ctrl+PgUp". A label with no "+" is a mnemonic letter, not
// a shortcut: false.
func ParseShortcutLabel(label string) (Shortcut, bool) {
	label = strings.TrimSpace(label)
	if !strings.Contains(label, "+") {
		return Shortcut{}, false
	}
	parts := strings.Split(label, "+")
	for i, p := range parts {
		if alias, ok := labelAliases[strings.ToUpper(strings.TrimSpace(p))]; ok {
			parts[i] = alias
		}
	}
	c, err := Parse(strings.Join(parts, "+"))
	if err != nil {
		return Shortcut{}, false
	}
	return ShortcutOf(c)
}

// labelAliases are the label words Parse does not know by that name.
var labelAliases = map[string]string{
	"CTRL": "Control_L",
	"PGUP": "Prior",
	"PGDN": "Next",
	"INS":  "Insert",
	"BKSP": "BackSpace",
}

// LabelShortcuts are the shortcuts an Element's labels show: its
// AcceleratorKey when that is a shortcut (not a mnemonic letter), the
// text after the last tab of its name (classic menus), and, for a menu
// item, the last word of its name when that is a shortcut (Chromium).
func LabelShortcuts(acceleratorKey, name string, menuItem bool) []Shortcut {
	var out []Shortcut
	if s, ok := ParseShortcutLabel(acceleratorKey); ok {
		out = append(out, s)
	}
	if i := strings.LastIndex(name, "\t"); i >= 0 {
		if s, ok := ParseShortcutLabel(name[i+1:]); ok {
			out = append(out, s)
		}
	} else if menuItem {
		if f := strings.Fields(name); len(f) > 1 {
			if s, ok := ParseShortcutLabel(f[len(f)-1]); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// ShownBy reports whether any of an Element's labels show s.
func (s Shortcut) ShownBy(acceleratorKey, name string, menuItem bool) bool {
	for _, l := range LabelShortcuts(acceleratorKey, name, menuItem) {
		if l == s {
			return true
		}
	}
	return false
}
