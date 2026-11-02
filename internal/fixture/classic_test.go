package fixture

// Offline tests for the classic Win32 windows' pure parts: the in-memory
// dialog template the Classic Dialog is built from, the result and focus
// names the state file records, and the two buttons that open the windows.
// No window is created and the desktop is never touched.

import (
	"testing"
	"unicode/utf16"

	"github.com/hoangvu12/winuse/internal/win"
)

// parsedItem is one DLGITEMTEMPLATE read back from a template.
type parsedItem struct {
	offset int // in UTF-16 words from the template start
	style  uint32
	id     uint16
	class  uint16
	text   string
}

// parseTemplate reads a DLGTEMPLATE back the way the dialog manager does:
// the header, then each item from the next DWORD boundary.
func parseTemplate(t *testing.T, b []uint16) (style uint32, count int, title, font string, items []parsedItem) {
	t.Helper()
	i := 0
	word := func() uint16 {
		if i >= len(b) {
			t.Fatalf("the template ends early at word %d", i)
		}
		w := b[i]
		i++
		return w
	}
	dword := func() uint32 { lo := word(); return uint32(lo) | uint32(word())<<16 }
	str := func() string {
		var s []uint16
		for w := word(); w != 0; w = word() {
			s = append(s, w)
		}
		return string(utf16.Decode(s))
	}
	style = dword()
	dword() // extended style
	count = int(word())
	word() // x
	word() // y
	word() // cx
	word() // cy
	if m := word(); m != 0 {
		t.Fatalf("menu word = %#x, want 0 (no menu)", m)
	}
	if c := word(); c != 0 {
		t.Fatalf("class word = %#x, want 0 (the predefined dialog class)", c)
	}
	title = str()
	if style&dsSetFont != 0 {
		word() // point size
		font = str()
	}
	for n := 0; n < count; n++ {
		if i%2 != 0 {
			i++
		}
		it := parsedItem{offset: i}
		it.style = dword()
		dword() // extended style
		word()  // x
		word()  // y
		word()  // cx
		word()  // cy
		it.id = word()
		if m := word(); m != 0xFFFF {
			t.Fatalf("item %d: class marker = %#x, want 0xFFFF (an ordinal)", n, m)
		}
		it.class = word()
		it.text = str()
		if extra := word(); extra != 0 {
			t.Fatalf("item %d: creation data size = %d, want 0", n, extra)
		}
		items = append(items, it)
	}
	if i != len(b) {
		t.Errorf("the template has %d trailing words", len(b)-i)
	}
	return style, count, title, font, items
}

func TestClassicTemplateLayout(t *testing.T) {
	style, count, title, font, items := parseTemplate(t, classicTemplate())
	if style&dsSetFont == 0 || style&wsCaption != wsCaption || style&wsPopup == 0 {
		t.Errorf("dialog style %#x lacks DS_SETFONT, WS_CAPTION or WS_POPUP", style)
	}
	if style&wsVisible != 0 {
		t.Errorf("dialog style %#x is visible: the fixture places it before showing it", style)
	}
	if title != ClassicDialogTitle || font != "Segoe UI" {
		t.Errorf("title %q, font %q", title, font)
	}
	if count != len(classicItems) || len(items) != len(classicItems) {
		t.Fatalf("%d items (count word %d), want %d", len(items), count, len(classicItems))
	}
	for n, it := range items {
		want := classicItems[n]
		if it.offset%2 != 0 {
			t.Errorf("item %d starts at word %d: not DWORD-aligned", n, it.offset)
		}
		if it.id != want.id || it.class != want.class || it.text != want.text {
			t.Errorf("item %d = id %d class %#x text %q, want id %d class %#x text %q",
				n, it.id, it.class, it.text, want.id, want.class, want.text)
		}
		if it.style&(wsChild|wsVisible) != wsChild|wsVisible {
			t.Errorf("item %d style %#x is not a visible child", n, it.style)
		}
	}
}

func TestClassicDialogControls(t *testing.T) {
	// What the e2e tests and winuse's keyboard routes rely on: the name
	// Edit is the first tab stop (it takes the focus when the dialog
	// opens), the password Edit is the next one and has ES_PASSWORD, OK is
	// the default button, and IDCANCEL exists. No Edit is multi-line, so
	// Return reaches the default button.
	var stops []uint16
	byID := map[uint16]dlgItem{}
	for _, it := range classicItems {
		byID[it.id] = it
		if it.style&wsTabStop != 0 {
			stops = append(stops, it.id)
		}
	}
	if len(stops) < 2 || stops[0] != ClassicEditID || stops[1] != ClassicPasswordID {
		t.Errorf("tab stops %v, want the name Edit then the password Edit first", stops)
	}
	if e := byID[ClassicEditID]; e.class != classEdit || e.style&esPassword != 0 {
		t.Errorf("the name Edit = %+v", e)
	}
	if e := byID[ClassicPasswordID]; e.class != classEdit || e.style&esPassword == 0 {
		t.Errorf("the password Edit = %+v, want ES_PASSWORD", e)
	}
	for _, e := range classicItems {
		if e.class == classEdit && e.style&win.ESMultiline != 0 {
			t.Errorf("Edit %d is multi-line", e.id)
		}
	}
	if b := byID[idOK]; b.class != classButton || b.style&0xF != bsDefPushButton {
		t.Errorf("OK = %+v, want the default push button", b)
	}
	if b := byID[idCancel]; b.class != classButton || b.text != "Cancel" {
		t.Errorf("Cancel = %+v", b)
	}
	// The combo is a drop-down list (no edit part: its Value is read-only,
	// so set-value picks an item instead of writing text).
	if c := byID[ClassicComboID]; c.class != classCombo || c.style&0x3 != cbsDropDownList || c.style&wsTabStop == 0 {
		t.Errorf("the combo = %+v, want a CBS_DROPDOWNLIST tab stop", c)
	}
}

func TestClassicColors(t *testing.T) {
	// The e2e test relies on two items that differ only in case.
	seen := map[string]bool{}
	for _, c := range ClassicColors {
		if seen[c] {
			t.Errorf("%q appears twice: an exact name must stay unique", c)
		}
		seen[c] = true
	}
	if !seen["Blue"] || !seen["blue"] {
		t.Errorf("ClassicColors = %v, want both Blue and blue", ClassicColors)
	}
	for sel, want := range map[int32]string{-1: "", 0: "Red", 3: "blue", 4: ""} {
		if got := classicColor(sel); got != want {
			t.Errorf("classicColor(%d) = %q, want %q", sel, got, want)
		}
	}
}

func TestCommandResultAndFocusNames(t *testing.T) {
	for id, want := range map[uintptr]string{idOK: "ok", idCancel: "cancel", 0: "failed", 7: "other"} {
		if got := commandResult(id); got != want {
			t.Errorf("commandResult(%d) = %q, want %q", id, got, want)
		}
	}
	for id, want := range map[uint16]string{ClassicEditID: "name", ClassicPasswordID: "password", idOK: ""} {
		if got := classicFocusName(id); got != want {
			t.Errorf("classicFocusName(%d) = %q, want %q", id, got, want)
		}
	}
}

func TestClassicButtonsInTheMainWindow(t *testing.T) {
	m := NewTreeMap(BuildTree(initial()))
	for _, e := range []Elem{ElemButtonClassic, ElemButtonMessageBox} {
		if !m.Contains(e) {
			t.Errorf("the initial tree lacks %s", e)
		}
		if !HasPattern(e, PatternInvoke) {
			t.Errorf("%s offers no Invoke", e)
		}
		if Parent(e, initial(), false) != ElemWindow {
			t.Errorf("%s's parent is not the window root", e)
		}
		r, ok := Rect(e, 0)
		if !ok {
			t.Fatalf("%s has no rectangle", e)
		}
		// Its rectangle overlaps no other static Element: hit-testing and
		// painting stay unambiguous.
		for _, s := range Specs {
			if s.Elem == e || s.Elem == ElemWindow || s.Elem == ElemMenuBar || s.Elem == ElemHoverToolbar {
				continue
			}
			o, ok := Rect(s.Elem, 0)
			if !ok {
				continue
			}
			if r.Left < o.Right && o.Left < r.Right && r.Top < o.Bottom && o.Top < r.Bottom {
				t.Errorf("%s %+v overlaps %s %+v", e, r, s.Elem, o)
			}
		}
	}
}
