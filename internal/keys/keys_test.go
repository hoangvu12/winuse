package keys

// Offline tests for Codex's key-name parsing: every accepted name family,
// the alias rules (case-insensitive, `-` as `_`, single letters and
// digits), the chord grammar (whitespace around `+`, literal order) and
// the three failure kinds. Pure parsing — no desktop is touched.

import (
	"strings"
	"testing"
)

func TestParseSingleKeys(t *testing.T) {
	cases := []struct {
		name     string
		wantName string
		wantVK   uint16
		wantExt  bool
	}{
		// A single letter or digit maps to its own key.
		{"a", "a", 0x41, false},
		{"Z", "z", 0x5A, false},
		{"5", "5", 0x35, false},
		{"0", "0", 0x30, false},

		// Editing and navigation names, with aliases.
		{"Return", "Return", 0x0D, false},
		{"return", "Return", 0x0D, false},
		{"ENTER", "Return", 0x0D, false},
		{"Linefeed", "Return", 0x0D, false},
		{"KP_Enter", "KP_Enter", 0x0D, true},
		{"Numpad_Enter", "KP_Enter", 0x0D, true},
		{"Tab", "Tab", 0x09, false},
		{"space", "space", 0x20, false},
		{"Escape", "Escape", 0x1B, false},
		{"esc", "Escape", 0x1B, false},
		{"BackSpace", "BackSpace", 0x08, false},
		{"Delete", "Delete", 0x2E, true},
		{"del", "Delete", 0x2E, true},
		{"Insert", "Insert", 0x2D, true},
		{"Home", "Home", 0x24, true},
		{"Begin", "Home", 0x24, true},
		{"End", "End", 0x23, true},
		{"Prior", "Prior", 0x21, true},
		{"Page_Up", "Prior", 0x21, true},
		{"Next", "Next", 0x22, true},
		{"Pagedown", "Next", 0x22, true},
		{"Up", "Up", 0x26, true},
		{"Down", "Down", 0x28, true},
		{"Left", "Left", 0x25, true},
		{"Right", "Right", 0x27, true},
		{"Clear", "Clear", 0x0C, false},
		{"Pause", "Pause", 0x13, false},

		// Modifiers.
		{"Control_L", "Control_L", 0xA2, false},
		{"Control", "Control_L", 0xA2, false},
		{"ctrl", "Control_L", 0xA2, false},
		{"ctrl_l", "Control_L", 0xA2, false},
		{"Control_R", "Control_R", 0xA3, true},
		{"ctrl_r", "Control_R", 0xA3, true},
		{"Shift", "Shift_L", 0xA0, false},
		{"Shift_L", "Shift_L", 0xA0, false},
		{"Shift_R", "Shift_R", 0xA1, false},
		{"Alt", "Alt_L", 0xA4, false},
		{"Alt_L", "Alt_L", 0xA4, false},
		{"option", "Alt_L", 0xA4, false},
		{"Alt_R", "Alt_R", 0xA5, true},
		{"option_r", "Alt_R", 0xA5, true},

		// System keys.
		{"Menu", "Menu", 0x5D, true},
		{"Help", "Help", 0x2F, true},
		{"Select", "Select", 0x29, true},
		{"Print", "Print", 0x2A, true},
		{"Sys_Req", "Sys_Req", 0x2C, true},
		{"Execute", "Execute", 0x2B, true},
		{"Cancel", "Cancel", 0x03, false},
		{"Break", "Cancel", 0x03, false},
		{"Mode_switch", "Mode_switch", 0x1F, false},
		{"Script_Switch", "Mode_switch", 0x1F, false},
		{"Scroll_Lock", "Scroll_Lock", 0x91, false},
		{"Num_Lock", "Num_Lock", 0x90, true},
		{"Caps_Lock", "Caps_Lock", 0x14, false},
		{"Shift_Lock", "Caps_Lock", 0x14, false},

		// Punctuation: shifted names map to the unshifted key.
		{"period", "period", 0xBE, false},
		{"dot", "period", 0xBE, false},
		{"full_stop", "period", 0xBE, false},
		{"greater", "greater", 0xBE, false},
		{">", "greater", 0xBE, false},
		{"comma", "comma", 0xBC, false},
		{"less", "less", 0xBC, false},
		{"slash", "slash", 0xBF, false},
		{"question", "question", 0xBF, false},
		{"semicolon", "semicolon", 0xBA, false},
		{"colon", "colon", 0xBA, false},
		{"apostrophe", "apostrophe", 0xDE, false},
		{"quote", "apostrophe", 0xDE, false},
		{"double_quote", "quotedbl", 0xDE, false},
		{"grave", "grave", 0xC0, false},
		{"backtick", "grave", 0xC0, false},
		{"tilde", "asciitilde", 0xC0, false},
		{"bracketleft", "bracketleft", 0xDB, false},
		{"bracketright", "bracketright", 0xDD, false},
		{"braceleft", "braceleft", 0xDB, false},
		{"braceright", "braceright", 0xDD, false},
		{"backslash", "backslash", 0xDC, false},
		{"bar", "bar", 0xDC, false},
		{"pipe", "bar", 0xDC, false},
		{"plus", "plus", 0xBB, false},
		{"equal", "equal", 0xBB, false},
		{"equals", "equal", 0xBB, false},
		{"minus", "minus", 0xBD, false},
		{"hyphen", "minus", 0xBD, false},
		{"underscore", "underscore", 0xBD, false},

		// Raw punctuation characters.
		{"=", "equal", 0xBB, false},
		{"_", "underscore", 0xBD, false},
		{",", "comma", 0xBC, false},
		{"<", "less", 0xBC, false},
		{".", "period", 0xBE, false},
		{"/", "slash", 0xBF, false},
		{"?", "question", 0xBF, false},
		{";", "semicolon", 0xBA, false},
		{":", "colon", 0xBA, false},
		{"'", "apostrophe", 0xDE, false},
		{`"`, "quotedbl", 0xDE, false},
		{"`", "grave", 0xC0, false},
		{"~", "asciitilde", 0xC0, false},
		{"[", "bracketleft", 0xDB, false},
		{"]", "bracketright", 0xDD, false},
		{"{", "braceleft", 0xDB, false},
		{"}", "braceright", 0xDD, false},
		{`\`, "backslash", 0xDC, false},
		{"|", "bar", 0xDC, false},
		// Raw shifted digits map to their digit key, like the shifted
		// punctuation names map to unshifted keys.
		{"!", "exclam", 0x31, false},
		{"@", "at", 0x32, false},
		{"#", "numbersign", 0x33, false},
		{"$", "dollar", 0x34, false},
		{"%", "percent", 0x35, false},
		{"^", "asciicircum", 0x36, false},
		{"&", "ampersand", 0x37, false},
		{"*", "asterisk", 0x38, false},
		{"(", "parenleft", 0x39, false},
		{")", "parenright", 0x30, false},

		// Numpad keys, all three spellings.
		{"KP_0", "KP_0", 0x60, false},
		{"KP_9", "KP_9", 0x69, false},
		{"Numpad_5", "KP_5", 0x65, false},
		{"NUM_5", "KP_5", 0x65, false},
		{"Numpad_Add", "KP_Add", 0x6B, false},
		{"NUM_SUBTRACT", "KP_Subtract", 0x6D, false},
		{"kp_multiply", "KP_Multiply", 0x6A, false},
		{"Numpad_Decimal", "KP_Decimal", 0x6E, false},
		{"KP_Divide", "KP_Divide", 0x6F, true},
		{"NUM_SEPARATOR", "KP_Separator", 0x6C, false},
		{"Numpad_Equal", "KP_Equal", 0x92, false},
		{"KP_Delete", "KP_Delete", 0x2E, true},

		// Function keys F1..F20.
		{"F1", "F1", 0x70, false},
		{"f1", "F1", 0x70, false},
		{"F12", "F12", 0x7B, false},
		{"F20", "F20", 0x83, false},
	}
	for _, tc := range cases {
		chord, err := Parse(tc.name)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error %v", tc.name, err)
			continue
		}
		if len(chord) != 1 {
			t.Errorf("Parse(%q): %d keys, want 1", tc.name, len(chord))
			continue
		}
		k := chord[0]
		if k.Name != tc.wantName || k.VK != tc.wantVK || k.Extended != tc.wantExt {
			t.Errorf("Parse(%q) = {%s 0x%02X ext:%v}, want {%s 0x%02X ext:%v}",
				tc.name, k.Name, k.VK, k.Extended, tc.wantName, tc.wantVK, tc.wantExt)
		}
	}
}

func TestParseChords(t *testing.T) {
	// Chords keep literal press order; whitespace around "+" is ignored.
	chord, err := Parse("Control_L+a")
	if err != nil {
		t.Fatalf("Parse(Control_L+a): %v", err)
	}
	if len(chord) != 2 || chord[0].VK != 0xA2 || chord[1].VK != 0x41 {
		t.Fatalf("Control_L+a = %v, want Control_L(0xA2), a(0x41)", chord)
	}
	if want := "Control_L+a"; chord.String() != want {
		t.Errorf("chord.String() = %q, want %q", chord.String(), want)
	}

	chord, err = Parse("Control_L+Shift_L+period")
	if err != nil {
		t.Fatalf("Parse(Control_L+Shift_L+period): %v", err)
	}
	wantVKs := []uint16{0xA2, 0xA0, 0xBE}
	for i, want := range wantVKs {
		if chord[i].VK != want {
			t.Errorf("chord key %d = 0x%02X, want 0x%02X", i, chord[i].VK, want)
		}
	}

	chord, err = Parse("  Control_L +  a  ")
	if err != nil || len(chord) != 2 {
		t.Fatalf("whitespace around + must be ignored: %v, %v", chord, err)
	}

	// A chord's order is literal — even an odd order like a+Control_L
	// stays as written (Codex sends what the caller named, in order).
	chord, err = Parse("a+Control_L")
	if err != nil || chord[0].VK != 0x41 || chord[1].VK != 0xA2 {
		t.Fatalf("a+Control_L = %v, %v: order must be preserved", chord, err)
	}

	// The same key may repeat.
	if chord, err := Parse("Tab+Tab"); err != nil || len(chord) != 2 {
		t.Fatalf("Tab+Tab = %v, %v", chord, err)
	}

	// Mixed alias spellings and a hyphen for the underscore.
	if chord, err := Parse("Ctrl+page-up"); err != nil || len(chord) != 2 {
		t.Fatalf("Ctrl+page-up = %v, %v: '-' must act as '_'", chord, err)
	} else if chord[1].VK != 0x21 {
		t.Errorf("page-up VK = 0x%02X, want Prior 0x21", chord[1].VK)
	}

	// A lone "-" canonicalizes to "_" (underscore), the minus key.
	if chord, err := Parse("-"); err != nil || chord[0].VK != 0xBD {
		t.Fatalf("Parse(\"-\") = %v, %v: want the minus key", chord, err)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		label string
		input string
		key   string
		kind  Kind
		want  string
	}{
		// Empty keys: Codex's "key is required".
		{"empty", "", "", ErrEmpty, "key is required"},
		{"spaces only", "   ", "", ErrEmpty, "key is required"},
		{"dangling separator", "Control_L+", "", ErrEmpty, "key is required"},
		{"leading separator", "+a", "", ErrEmpty, "key is required"},
		{"lone plus", "+", "", ErrEmpty, "key is required"},
		{"empty part between separators", "Control_L++a", "", ErrEmpty, "key is required"},

		// Unknown names: Codex's "unsupported key: <name>".
		{"unknown word", "NotAKey", "NotAKey", ErrUnknown, "unsupported key: NotAKey"},
		{"unknown word in chord", "Control_L+NotAKey", "NotAKey", ErrUnknown, "unsupported key: NotAKey"},
		{"too-high function key", "F21", "F21", ErrUnknown, "unsupported key: F21"},
		{"non-ASCII letter", "é", "é", ErrUnknown, "unsupported key: é"},
		{"name with a space", "a b", "a b", ErrUnknown, "unsupported key: a b"},
		{"printscreen is not an alias", "PrintScreen", "PrintScreen", ErrUnknown, "unsupported key: PrintScreen"},

		// The Windows-key family: recognized, always refused.
		{"Win", "Win", "Win", ErrWindowsKey, "unsupported key: Win (the Windows key is never sent)"},
		{"Windows", "Windows", "Windows", ErrWindowsKey, "unsupported key: Windows (the Windows key is never sent)"},
		{"Meta", "Meta", "Meta", ErrWindowsKey, "unsupported key: Meta (the Windows key is never sent)"},
		{"Meta_L", "Meta_L", "Meta_L", ErrWindowsKey, "unsupported key: Meta_L (the Windows key is never sent)"},
		{"Command", "Command", "Command", ErrWindowsKey, "unsupported key: Command (the Windows key is never sent)"},
		{"cmd_r", "cmd_r", "cmd_r", ErrWindowsKey, "unsupported key: cmd_r (the Windows key is never sent)"},
		{"Super_L", "Super_L", "Super_L", ErrWindowsKey, "unsupported key: Super_L (the Windows key is never sent)"},
		{"OS", "OS", "OS", ErrWindowsKey, "unsupported key: OS (the Windows key is never sent)"},
		{"in a chord", "Win+e", "Win", ErrWindowsKey, "unsupported key: Win (the Windows key is never sent)"},
	}
	for _, tc := range cases {
		chord, err := Parse(tc.input)
		if err == nil {
			t.Errorf("Parse(%q): want error, got chord %v", tc.input, chord)
			continue
		}
		ke, ok := err.(*Error)
		if !ok {
			t.Errorf("Parse(%q): error %T, want *keys.Error", tc.input, err)
			continue
		}
		if ke.Kind != tc.kind {
			t.Errorf("Parse(%q): kind = %d, want %d", tc.input, ke.Kind, tc.kind)
		}
		if ke.Key != tc.key {
			t.Errorf("Parse(%q): key = %q, want %q", tc.input, ke.Key, tc.key)
		}
		if err.Error() != tc.want {
			t.Errorf("Parse(%q): message = %q, want %q", tc.input, err.Error(), tc.want)
		}
	}
}

func TestNumpadDigitsAreDistinctFromMainRow(t *testing.T) {
	// The point of the KP_ names: apps can tell the numpad keys from the
	// main row (Codex's guidance: prefer KP_0-KP_9 when an app
	// distinguishes numpad keys).
	kp, err := Parse("KP_1")
	if err != nil {
		t.Fatalf("Parse(KP_1): %v", err)
	}
	main, err := Parse("1")
	if err != nil {
		t.Fatalf("Parse(1): %v", err)
	}
	if kp[0].VK == main[0].VK {
		t.Errorf("KP_1 and 1 share VK 0x%02X; they must differ", kp[0].VK)
	}
}

func TestNoAliasCollidesWithTheWindowsKeyFamily(t *testing.T) {
	// If a future alias ever collides with the forbidden family, the
	// refusal would win silently — so the two sets are checked disjoint.
	for name := range aliases {
		if windowsKeyNames[name] {
			t.Errorf("name %q is both an accepted key and a forbidden Windows-key name", name)
		}
	}
}

func TestCanonicalNamesRoundTrip(t *testing.T) {
	// The canonical names of single keys parse back to themselves, so
	// error messages and Chord.String() always name pressable keys.
	for name := range aliases {
		chord, err := Parse(name)
		if err != nil {
			t.Errorf("alias %q must parse", name)
			continue
		}
		if got := chord.String(); got != name && got != strings.ToLower(name) {
			// Canonical names differ from alias spellings by design
			// (CTRL -> Control_L); only verify the round trip of the
			// canonical name itself.
			if _, err := Parse(chord.String()); err != nil {
				t.Errorf("canonical name %q of alias %q does not parse: %v", chord.String(), name, err)
			}
		}
	}
}
