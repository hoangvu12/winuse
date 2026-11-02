package win

// Posted Keys (ADR 0011, Decision 5; headless-parity spec, "Posted keys,
// opt-in"): keys and text posted as window messages to a Window's focused
// control, without the foreground and without SendInput. press-key --post
// posts WM_KEYDOWN and WM_KEYUP; type-text --post posts WM_CHAR, one per
// UTF-16 unit. Posting is best effort: the app may ignore posted keys,
// and posted messages do not change the keyboard state that apps read
// with GetKeyState, so the helper refuses chords with modifiers before
// anything is posted.
//
// Each posting call refuses inside a go test binary (LiveDesktopGuard);
// the message building is pure and tested offline.

import (
	"fmt"
	"unicode/utf16"

	"github.com/hoangvu12/winuse/internal/keys"
)

// wmChar is WM_CHAR. Posted Keys also send WM_KEYDOWN and WM_KEYUP
// (wmKeyDown and wmKeyUp, shared with the Esc hook).
const wmChar = 0x0102

// postedMessage is one keyboard message to post.
type postedMessage struct {
	msg    uint32
	wparam uintptr
	lparam uintptr
}

// PostKeys posts the chord to target (the focused control, or the Window
// itself): every key down in order, then every key up in reverse, the
// order a real chord arrives in. It returns how many messages were
// posted, so the caller can tell "nothing was sent" (0) from a partial
// post.
func PostKeys(target uint64, chord keys.Chord) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	return postAll(target, postedKeyMessages(chord, mapVirtualKeyScan))
}

// PostText posts text to target as WM_CHAR messages, one per UTF-16 unit
// (postedCharUnits). It returns how many were posted.
func PostText(target uint64, text string) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	units := postedCharUnits(text)
	msgs := make([]postedMessage, len(units))
	for i, u := range units {
		msgs[i] = postedMessage{wmChar, uintptr(u), 1} // repeat count 1
	}
	return postAll(target, msgs)
}

// postAll posts the messages in order and stops at the first failure.
func postAll(target uint64, msgs []postedMessage) (int, error) {
	for i, m := range msgs {
		if r, _, err := procPostMessageW.Call(uintptr(target), uintptr(m.msg), m.wparam, m.lparam); r == 0 {
			return i, fmt.Errorf("PostMessage(%#x) failed for window %d after %d of %d messages: %v", m.msg, target, i, len(msgs), err)
		}
	}
	return len(msgs), nil
}

// postedKeyMessages builds a posted chord: WM_KEYDOWN for each key in
// order, then WM_KEYUP for each in reverse, each with its lParam. scan
// resolves a virtual key's scan code (MapVirtualKey).
func postedKeyMessages(chord keys.Chord, scan func(uint16) uint16) []postedMessage {
	msgs := make([]postedMessage, 0, 2*len(chord))
	for _, k := range chord {
		msgs = append(msgs, postedMessage{wmKeyDown, uintptr(k.VK), keyLParam(scan(k.VK), k.Extended, false)})
	}
	for i := len(chord) - 1; i >= 0; i-- {
		k := chord[i]
		msgs = append(msgs, postedMessage{wmKeyUp, uintptr(k.VK), keyLParam(scan(k.VK), k.Extended, true)})
	}
	return msgs
}

// keyLParam builds WM_KEYDOWN's or WM_KEYUP's lParam: a repeat count of 1
// (bits 0-15), the scan code (bits 16-23), the extended-key flag (bit
// 24), the context code 0 (bit 29: no Alt), and for a key up the
// previous-state and transition bits (30 and 31), which a key down leaves
// clear.
func keyLParam(scan uint16, extended, up bool) uintptr {
	l := uint32(1) | uint32(scan&0xFF)<<16
	if extended {
		l |= 1 << 24
	}
	if up {
		l |= 1<<30 | 1<<31
	}
	return uintptr(l)
}

// postedCharUnits is posted text's WM_CHAR stream: its UTF-16 units, with
// a newline (\n, \r or a \r\n pair) as the single carriage return that
// Return types, and a tab as itself.
func postedCharUnits(text string) []uint16 {
	var runes []rune
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '\r':
			if i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			runes = append(runes, '\r')
		case '\n':
			runes = append(runes, '\r')
		default:
			runes = append(runes, rs[i])
		}
	}
	return utf16.Encode(runes)
}
