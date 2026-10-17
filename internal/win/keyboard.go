package win

// Keyboard input injection for Actions (ticket 06): SendInput key events
// for press-key chords and Unicode text for type-text. Key events carry
// the virtual key *and* its scan code (MapVirtualKey) — some apps (games,
// RDP, some Electron paths, Flutter) read scancodes, and Codex's own
// Ctrl-chords-in-Flutter bug was zero scan codes. Extended keys get
// KEYEVENTF_EXTENDEDKEY so arrows arrive as arrows, not numpad keys. Text
// is injected as Unicode key events (KEYEVENTF_UNICODE, one event per
// UTF-16 code unit), so any script and emoji type correctly; \n and \t are
// injected as real Return/Tab keys, because many controls ignore Unicode
// CR. As with the mouse: no input is ever injected from the CLI process —
// only the helper acts.

import (
	"fmt"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/keys"
)

// Keyboard input flags (SendInput docs, win32.md §6).
const (
	keyEventfExtendedKey = 0x0001
	keyEventfKeyUp       = 0x0002
	keyEventfUnicode     = 0x0004
)

// inputTypeKeyboard is INPUT's type for KEYBDINPUT.
const inputTypeKeyboard = 1

// Virtual keys text typing needs: \n as a real Return and \t as a real Tab
// (many controls ignore Unicode CR).
const (
	vkReturnKey = 0x0D
	vkTabKey    = 0x09
)

// typeBatchRunes is how many runes one SendInput call carries (~16-32 per
// win32.md §6: larger single batches get dropped by some apps), and
// typeBatchPause is the small sleep between batches.
const (
	typeBatchRunes = 16
	typeBatchPause = 10 * time.Millisecond
	mapVKToVsc     = 0 // MAPVK_VK_TO_VSC: virtual key to scan code
)

var procMapVirtualKeyW = user32.NewProc("MapVirtualKeyW")

// keybdInput is the Win32 KEYBDINPUT struct (24 bytes on amd64 and arm64:
// two uint16s, two uint32s, alignment pad, one pointer), which fits
// INPUT's 32-byte union.
type keybdInput struct {
	Vk, Scan  uint16
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

var _ [24]byte = [unsafe.Sizeof(keybdInput{})]byte{} // fits INPUT's 32-byte union

// PressChord presses the chord exactly as Codex delivers chords: every key
// down in the chord's order, then every key up in reverse, as one SendInput
// batch so the app sees a single chord, not interleaved presses.
func PressChord(chord keys.Chord) error {
	if len(chord) == 0 {
		return nil // keys.Parse never returns an empty chord; empty is a no-op
	}
	events := chordEvents(chord, mapVirtualKeyScan)
	if err := sendInput(events...); err != nil {
		return fmt.Errorf("injecting the key press failed: %v", err)
	}
	return nil
}

// TypeText types text into whatever currently holds keyboard focus, as
// Unicode key events in small batches. \n (and \r) become real Return
// presses and \t a real Tab; every other rune is injected as its UTF-16
// code units, so non-Latin scripts and emoji (surrogate pairs) arrive
// intact.
//
// stopped is the Esc-stop checkpoint (ticket 12): it is polled between
// batches — the pause is the abort point ticket 06 designed in — and a
// stop returns ErrEscStopped with the batches already injected landed
// and nothing further sent. A nil stopped never aborts.
func TypeText(text string, stopped func() bool) error {
	runes := []rune(text)
	for _, batch := range textBatches(runes, typeBatchRunes) {
		if stopped != nil && stopped() {
			return ErrEscStopped
		}
		events := textEvents(batch, mapVirtualKeyScan)
		if err := sendInput(events...); err != nil {
			return fmt.Errorf("injecting the text failed: %v", err)
		}
		time.Sleep(typeBatchPause)
	}
	return nil
}

// textBatches splits runes into batches of at most size runes (a rune's
// events always stay in one batch, so a surrogate pair is never split).
func textBatches(runes []rune, size int) [][]rune {
	var batches [][]rune
	for start := 0; start < len(runes); start += size {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		batches = append(batches, runes[start:end])
	}
	return batches
}

// chordEvents builds the chord's events: all key-downs in order, then all
// key-ups in reverse. scan resolves each key's scan code.
func chordEvents(chord keys.Chord, scan func(uint16) uint16) []input {
	events := make([]input, 0, 2*len(chord))
	for _, k := range chord {
		events = append(events, keyEvent(k.VK, scan(k.VK), k.Extended, false))
	}
	for i := len(chord) - 1; i >= 0; i-- {
		k := chord[i]
		events = append(events, keyEvent(k.VK, scan(k.VK), k.Extended, true))
	}
	return events
}

// textEvents builds one batch's events. Each rune is down+up; a surrogate
// pair is two units, so an emoji sends 4 events (the system composes the
// pair into one character). Newline and tab are real keys, not Unicode
// events.
func textEvents(runes []rune, scan func(uint16) uint16) []input {
	events := make([]input, 0, len(runes)*2)
	for _, r := range runes {
		switch r {
		case '\n', '\r':
			events = append(events, keyEvent(vkReturnKey, scan(vkReturnKey), false, false))
			events = append(events, keyEvent(vkReturnKey, scan(vkReturnKey), false, true))
		case '\t':
			events = append(events, keyEvent(vkTabKey, scan(vkTabKey), false, false))
			events = append(events, keyEvent(vkTabKey, scan(vkTabKey), false, true))
		default:
			for _, unit := range utf16.Encode([]rune{r}) {
				events = append(events, unicodeEvent(unit, false))
				events = append(events, unicodeEvent(unit, true))
			}
		}
	}
	return events
}

// keyEvent builds one INPUT carrying a KEYBDINPUT for a virtual key.
func keyEvent(vk uint16, scan uint16, extended, up bool) input {
	var flags uint32
	if up {
		flags |= keyEventfKeyUp
	}
	if extended {
		flags |= keyEventfExtendedKey
	}
	ki := keybdInput{Vk: vk, Scan: scan, Flags: flags, ExtraInfo: injectedTag}
	var in input
	in.Type = inputTypeKeyboard
	*(*keybdInput)(unsafe.Pointer(&in.U)) = ki
	return in
}

// unicodeEvent builds one KEYEVENTF_UNICODE INPUT: Vk 0, the UTF-16 code
// unit in Scan. The system synthesizes VK_PACKET and delivers the
// character as WM_CHAR.
func unicodeEvent(unit uint16, up bool) input {
	var flags uint32 = keyEventfUnicode
	if up {
		flags |= keyEventfKeyUp
	}
	ki := keybdInput{Scan: unit, Flags: flags, ExtraInfo: injectedTag}
	var in input
	in.Type = inputTypeKeyboard
	*(*keybdInput)(unsafe.Pointer(&in.U)) = ki
	return in
}

// mapVirtualKeyScan resolves a virtual key's scan code (MAPVK_VK_TO_VSC).
// For extended keys the base scan code is returned; the E0 prefix is
// conveyed by KEYEVENTF_EXTENDEDKEY.
func mapVirtualKeyScan(vk uint16) uint16 {
	r, _, _ := procMapVirtualKeyW.Call(uintptr(vk), mapVKToVsc)
	return uint16(r)
}
