package win

// Offline tests for the keyboard input layer's pure half (event building,
// flags, batching, ABI sizes), with a fake scan-code table so no Win32
// call is needed. The injection itself (MapVirtualKey, SendInput) drives
// the live desktop and is covered only by the opt-in e2e suite.

import (
	"errors"
	"testing"
	"unicode/utf16"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/keys"
)

// fakeScans is a small stand-in for MapVirtualKey: distinct, non-zero scan
// codes (0x100 + VK) so tests can tell "filled from the table" from
// "left zero".
func fakeScans(vk uint16) uint16 { return 0x100 + vk }

func TestKeyboardABI(t *testing.T) {
	if unsafe.Sizeof(keybdInput{}) != 24 {
		t.Errorf("sizeof(KEYBDINPUT) = %d, want 24", unsafe.Sizeof(keybdInput{}))
	}
	if unsafe.Sizeof(keybdInput{}) > 32 {
		t.Errorf("KEYBDINPUT (%d bytes) must fit INPUT's 32-byte union", unsafe.Sizeof(keybdInput{}))
	}
}

func keybdOf(t *testing.T, in input) keybdInput {
	t.Helper()
	if in.Type != inputTypeKeyboard {
		t.Fatalf("event type = %d, want INPUT_KEYBOARD", in.Type)
	}
	return *(*keybdInput)(unsafe.Pointer(&in.U))
}

func TestChordEventsDownsInOrderUpsInReverse(t *testing.T) {
	// Codex's chord delivery: all key-downs in the chord's order, then
	// all key-ups in reverse, as one batch.
	chord, err := keys.Parse("Control_L+Shift_L+a")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	events := chordEvents(chord, fakeScans)
	if len(events) != 6 {
		t.Fatalf("%d events, want 6 (3 downs + 3 ups)", len(events))
	}
	wantVKs := []uint16{0xA2, 0xA0, 0x41, 0x41, 0xA0, 0xA2}
	wantUps := []bool{false, false, false, true, true, true}
	for i, ev := range events {
		ki := keybdOf(t, ev)
		if ki.Vk != wantVKs[i] {
			t.Errorf("event %d: VK = 0x%02X, want 0x%02X", i, ki.Vk, wantVKs[i])
		}
		up := ki.Flags&keyEventfKeyUp != 0
		if up != wantUps[i] {
			t.Errorf("event %d: KEYUP = %v, want %v", i, up, wantUps[i])
		}
		if ki.Scan != fakeScans(wantVKs[i]) {
			// The scan code must be filled (Codex's Flutter chord bug was
			// zero scancodes).
			t.Errorf("event %d: scan = 0x%02X, want 0x%02X", i, ki.Scan, fakeScans(wantVKs[i]))
		}
		if ki.ExtraInfo != injectedTag {
			t.Errorf("event %d: ExtraInfo = 0x%x, want the winuse tag", i, ki.ExtraInfo)
		}
	}
}

func TestChordEventsExtendedKeys(t *testing.T) {
	// Arrows and friends need KEYEVENTF_EXTENDEDKEY, or they arrive as
	// numpad keys.
	for name, wantVK := range map[string]uint16{"Right": 0x27, "KP_Divide": 0x6F, "Alt_R": 0xA5, "KP_Enter": 0x0D} {
		chord, err := keys.Parse(name)
		if err != nil {
			t.Fatalf("parse %q: %v", name, err)
		}
		for _, ev := range chordEvents(chord, fakeScans) {
			ki := keybdOf(t, ev)
			if ki.Vk != wantVK {
				t.Fatalf("%q: VK = 0x%02X, want 0x%02X", name, ki.Vk, wantVK)
			}
			if ki.Flags&keyEventfExtendedKey == 0 {
				t.Errorf("%q: KEYEVENTF_EXTENDEDKEY not set (flags 0x%x)", name, ki.Flags)
			}
		}
	}
	// Plain keys must not carry the extended flag.
	for _, name := range []string{"a", "Return", "Shift_L", "KP_5"} {
		chord, _ := keys.Parse(name)
		for _, ev := range chordEvents(chord, fakeScans) {
			if ki := keybdOf(t, ev); ki.Flags&keyEventfExtendedKey != 0 {
				t.Errorf("%q: unexpected KEYEVENTF_EXTENDEDKEY (flags 0x%x)", name, ki.Flags)
			}
		}
	}
}

func TestTextEventsUnicode(t *testing.T) {
	events := textEvents([]rune("aé"), fakeScans)
	if len(events) != 4 {
		t.Fatalf("%d events, want 4 (one down+up per rune)", len(events))
	}
	wantScans := []uint16{'a', 'a', 'é', 'é'}
	for i, ev := range events {
		ki := keybdOf(t, ev)
		if ki.Vk != 0 {
			t.Errorf("event %d: Vk = 0x%02X, want 0 (KEYEVENTF_UNICODE)", i, ki.Vk)
		}
		if ki.Scan != wantScans[i] {
			t.Errorf("event %d: scan = 0x%04X, want the UTF-16 unit 0x%04X", i, ki.Scan, wantScans[i])
		}
		if ki.Flags&keyEventfUnicode == 0 {
			t.Errorf("event %d: KEYEVENTF_UNICODE not set (flags 0x%x)", i, ki.Flags)
		}
	}
	// Down first, then up.
	if keybdOf(t, events[0]).Flags&keyEventfKeyUp != 0 || keybdOf(t, events[1]).Flags&keyEventfKeyUp == 0 {
		t.Error("each rune is a down followed by an up")
	}
}

func TestTextEventsSurrogatePair(t *testing.T) {
	// An emoji is one rune = two UTF-16 units = 4 events, in unit order
	// with down/up per unit; the system composes the pair.
	events := textEvents([]rune("🎉"), fakeScans)
	if len(events) != 4 {
		t.Fatalf("%d events, want 4 (two units, down+up each)", len(events))
	}
	hi, lo := utf16.Encode([]rune("🎉"))[0], utf16.Encode([]rune("🎉"))[1]
	wantScans := []uint16{hi, hi, lo, lo}
	wantUps := []bool{false, true, false, true}
	for i, ev := range events {
		ki := keybdOf(t, ev)
		if ki.Scan != wantScans[i] {
			t.Errorf("event %d: scan = 0x%04X, want 0x%04X", i, ki.Scan, wantScans[i])
		}
		if up := ki.Flags&keyEventfKeyUp != 0; up != wantUps[i] {
			t.Errorf("event %d: KEYUP = %v, want %v", i, up, wantUps[i])
		}
	}
}

func TestTextEventsNewlineAndTabAreRealKeys(t *testing.T) {
	// \n and \t must be VK_RETURN / VK_TAB key events, not Unicode events:
	// many controls ignore Unicode CR.
	events := textEvents([]rune{'a', '\n', '\t'}, fakeScans)
	if len(events) != 6 {
		t.Fatalf("%d events, want 6", len(events))
	}
	ki := keybdOf(t, events[2])
	if ki.Vk != vkReturnKey || ki.Flags&keyEventfUnicode != 0 {
		t.Errorf("\\n = VK 0x%02X flags 0x%x, want VK_RETURN as a key event", ki.Vk, ki.Flags)
	}
	if ki.Scan != fakeScans(vkReturnKey) {
		t.Errorf("\\n scan = 0x%02X, want the filled scan code", ki.Scan)
	}
	ki = keybdOf(t, events[4])
	if ki.Vk != vkTabKey || ki.Flags&keyEventfUnicode != 0 {
		t.Errorf("\\t = VK 0x%02X flags 0x%x, want VK_TAB as a key event", ki.Vk, ki.Flags)
	}
	// \r is a Return too.
	for _, r := range []rune{'\r', '\n'} {
		if ev := textEvents([]rune{r}, fakeScans); keybdOf(t, ev[0]).Vk != vkReturnKey {
			t.Errorf("%q must be a Return press", string(r))
		}
	}
}

func TestTextBatches(t *testing.T) {
	cases := []struct {
		runes []rune
		size  int
		want  []int // batch lengths, in order
	}{
		{[]rune("hello"), 16, []int{5}},
		{[]rune("0123456789abcdefghij"), 16, []int{16, 4}},
		{[]rune(""), 16, nil},
		{[]rune("abc"), 2, []int{2, 1}},
	}
	for _, tc := range cases {
		batches := textBatches(tc.runes, tc.size)
		if len(batches) != len(tc.want) {
			t.Fatalf("runes %q size %d: %d batches (%v), want %d", string(tc.runes), tc.size, len(batches), batches, len(tc.want))
		}
		var flat []rune
		for i, b := range batches {
			if len(b) != tc.want[i] {
				t.Errorf("runes %q size %d: batch %d has %d runes, want %d", string(tc.runes), tc.size, i, len(b), tc.want[i])
			}
			if len(b) > tc.size {
				t.Errorf("runes %q size %d: batch %d of %d exceeds the size", string(tc.runes), tc.size, i, len(b))
			}
			flat = append(flat, b...)
		}
		if string(flat) != string(tc.runes) {
			t.Errorf("runes %q size %d: batches flatten to %q, want the original order preserved", string(tc.runes), tc.size, string(flat))
		}
	}
}

func TestTextBatchNeverSplitsASurrogatePair(t *testing.T) {
	// A rune's events stay within one batch: a batch boundary can never
	// fall inside a surrogate pair.
	text := []rune("é🎉ab")
	batches := textBatches(text, 2)
	for _, b := range batches {
		for _, r := range b {
			if r >= 0xD800 && r <= 0xDFFF {
				t.Errorf("a lone surrogate half %04X ended up in a batch", r)
			}
		}
	}
}

func TestTypeTextStopsBeforeInjecting(t *testing.T) {
	// The Esc-stop checkpoint runs between batches — including before the
	// first: a stop already latched when typing starts aborts with
	// ErrEscStopped before a single event is injected (and so never
	// reaches the live-desktop guard). A stop mid-typing is exercised by
	// the user-run e2e suite; here the immediate abort pins the contract.
	err := TypeText("hello", func() bool { return true })
	if !errors.Is(err, ErrEscStopped) {
		t.Fatalf("TypeText with a latched stop: error = %v, want ErrEscStopped", err)
	}
}
