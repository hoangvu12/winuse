package win

// Typing, keys and clicks into a Chromium or Electron Window without a
// Browser Connection and without the foreground (headless-max ticket 20,
// ADR 0013 rule 7, after agent-desktop PR #400). Window messages are sent
// (SendMessageTimeout, abort if hung) to the top-level Chrome_WidgetWin_1,
// which hands them to the page as trusted input. Measured on Chrome, Edge
// and Electron 45.2.1 (.scratch/headless-gaps/probes-round3.md, round 4
// section 3):
//
//   - Keys need the activation pair around them: WM_NCACTIVATE(1),
//     WM_ACTIVATE(WA_ACTIVE) and WM_SETFOCUS before, WM_KILLFOCUS,
//     WM_ACTIVATE(WA_INACTIVE) and WM_NCACTIVATE(0) after. Without it a
//     WM_CHAR does nothing. The page sees a focus and blur around it.
//   - Text is one WM_KEYDOWN(VK_PACKET), WM_CHAR, WM_KEYUP per UTF-16 unit
//     (surrogate pairs included); a newline is Return (scan 0x1C) with a CR
//     character, a tab is Tab (scan 0x0F) with a tab character.
//   - Clicks, double and right clicks are mouse messages in client
//     coordinates and need no activation pair, so the page sees no focus
//     change.
//   - On a minimized Window the activation makes Chromium restore itself
//     and take the foreground: the helper refuses minimized Windows first.
//
// Keys and text refuse when the Window's thread owns the foreground (the
// user is in that browser; faking its activation would fight them).
// Chords go through ticket 07's attached key state (PostChord). Every
// sending call refuses inside a go test binary; the sequences are pure.

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/keys"
)

// ChromiumClass is the top-level class of Chromium browsers and Electron
// Apps. WebView2 hosts are child windows of another class: not covered.
const ChromiumClass = "Chrome_WidgetWin_1"

// The keys the Chromium route sends besides VK_PACKET, with their scan codes.
const (
	vkPacket    = 0xE7
	vkReturn    = 0x0D
	vkTab       = 0x09
	vkBack      = 0x08
	vkSpace     = 0x20
	scanReturn  = 0x1C
	scanTab     = 0x0F
	waInactive  = 0
	chromiumMsg = 500 * time.Millisecond // each sent message's bound
)

// sentMessage is one message the Chromium route sends.
type sentMessage = postedMessage

// activationOn and activationOff are the activation pair, in order.
var (
	activationOn  = []sentMessage{{wmNcActivate, 1, 0}, {wmActivate, waActive, 0}, {wmSetFocus, 0, 0}}
	activationOff = []sentMessage{{wmKillFocus, 0, 0}, {wmActivate, waInactive, 0}, {wmNcActivate, 0, 0}}
)

// chromiumUnitMessages is the sequence for one UTF-16 unit of text.
func chromiumUnitMessages(u uint16) []sentMessage {
	switch u {
	case '\r', '\n':
		return keyWithChar(vkReturn, scanReturn, '\r')
	case '\t':
		return keyWithChar(vkTab, scanTab, '\t')
	}
	return []sentMessage{
		{wmKeyDown, vkPacket, keyLParam(0, false, false)},
		{wmChar, uintptr(u), keyLParam(0, false, false)},
		{wmKeyUp, vkPacket, keyLParam(0, false, true)},
	}
}

// keyWithChar is a key down, the character it types, and the key up.
func keyWithChar(vk, scan uint16, ch uint16) []sentMessage {
	return []sentMessage{
		{wmKeyDown, uintptr(vk), keyLParam(scan, false, false)},
		{wmChar, uintptr(ch), keyLParam(scan, false, false)},
		{wmKeyUp, uintptr(vk), keyLParam(scan, false, true)},
	}
}

// chromiumTextUnits are the UTF-16 units of text as the route types them:
// CR LF becomes one CR, so a Windows newline is one Return.
func chromiumTextUnits(text string) []uint16 {
	units := utf16.Encode([]rune(text))
	out := make([]uint16, 0, len(units))
	for i, u := range units {
		if u == '\n' && i > 0 && units[i-1] == '\r' {
			continue
		}
		out = append(out, u)
	}
	return out
}

// keyChar is the character a key types when a real keyboard presses it
// with no modifier (what TranslateMessage would add), or 0 for a key that
// types none (arrows, Delete, F keys).
func keyChar(k keys.Key) uint16 {
	switch {
	case k.VK == vkReturn:
		return '\r'
	case k.VK == vkTab:
		return '\t'
	case k.VK == vkBack:
		return '\b'
	case k.VK == vkEscape:
		return 0x1B
	case k.VK == vkSpace:
		return ' '
	case k.VK >= 'A' && k.VK <= 'Z':
		return k.VK + ('a' - 'A')
	case k.VK >= '0' && k.VK <= '9':
		return k.VK
	case k.VK >= 0x60 && k.VK <= 0x69: // the numpad digits
		return '0' + (k.VK - 0x60)
	}
	return 0
}

// chromiumKeyMessages is the sequence for one key with no modifier: the
// key down, its character when it types one, and the key up.
func chromiumKeyMessages(k keys.Key, scan func(uint16) uint16) []sentMessage {
	sc := scan(k.VK)
	msgs := []sentMessage{{wmKeyDown, uintptr(k.VK), keyLParam(sc, k.Extended, false)}}
	if ch := keyChar(k); ch != 0 {
		msgs = append(msgs, sentMessage{wmChar, uintptr(ch), keyLParam(sc, k.Extended, false)})
	}
	return append(msgs, sentMessage{wmKeyUp, uintptr(k.VK), keyLParam(sc, k.Extended, true)})
}

// sendAll sends the messages in order, each bounded, and stops at the
// first failure; it returns how many were sent.
func sendAll(top uint64, msgs []sentMessage) (int, error) {
	for i, m := range msgs {
		if _, err := sendMessageTimeout(windows.HWND(top), m.msg, m.wparam, m.lparam, chromiumMsg); err != nil {
			return i, err
		}
	}
	return len(msgs), nil
}

// foregroundThread refuses a Window whose thread owns the foreground.
func foregroundThread(top uint64) error {
	tid, err := windows.GetWindowThreadProcessId(windows.HWND(top), nil)
	if err != nil || tid == 0 {
		return fmt.Errorf("window %d is gone", top)
	}
	if fg := windows.GetForegroundWindow(); fg != 0 {
		if ftid, _ := windows.GetWindowThreadProcessId(fg, nil); ftid == tid {
			return ErrChordForeground
		}
	}
	return nil
}

// ErrAfterActivation marks a failure after an activation message reached
// the Window (afterActivation): the page saw a focus change, so the
// Action may have taken effect even when no key message was sent (a
// Chromium menu dismissed on the activation, e2e live run 2026-11-03).
var ErrAfterActivation = errors.New("an activation message had reached the Window")

// afterActivationError is a failure after the activation pair went out.
// Its message is the failure's own.
type afterActivationError struct{ err error }

func (e *afterActivationError) Error() string   { return e.err.Error() }
func (e *afterActivationError) Unwrap() []error { return []error{e.err, ErrAfterActivation} }

// afterActivation marks err as a failure after the activation pair.
func afterActivation(err error) error {
	if err == nil {
		return nil
	}
	return &afterActivationError{err}
}

// withActivation sends the activation pair, runs keys, and always undoes
// the pair. It returns how many key messages keys sent; a failure once an
// activation message went out is marked ErrAfterActivation.
func withActivation(top uint64, keys func() (int, error)) (int, error) {
	if n, err := sendAll(top, activationOn); err != nil {
		sendAll(top, activationOff)
		if n > 0 {
			return 0, afterActivation(err)
		}
		return 0, err
	}
	defer sendAll(top, activationOff)
	n, err := keys()
	return n, afterActivation(err)
}

// SendChromiumText types text into the Chromium Window top: the activation
// pair around one VK_PACKET sequence per UTF-16 unit. stopped is polled
// between units (the Esc stop, the request's deadline). It returns how
// many key messages were sent (0: nothing was typed).
func SendChromiumText(top uint64, text string, stopped func() bool) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	if err := foregroundThread(top); err != nil {
		return 0, err
	}
	return withActivation(top, func() (int, error) {
		sent := 0
		for _, u := range chromiumTextUnits(text) {
			if stopped != nil && stopped() {
				return sent, ErrEscStopped
			}
			n, err := sendAll(top, chromiumUnitMessages(u))
			sent += n
			if err != nil {
				return sent, err
			}
		}
		return sent, nil
	})
}

// SendChromiumKey presses one key with no modifier in the Chromium Window
// top, inside the activation pair. Chords go through PostChord.
func SendChromiumKey(top uint64, k keys.Key) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	if err := foregroundThread(top); err != nil {
		return 0, err
	}
	return withActivation(top, func() (int, error) {
		return sendAll(top, chromiumKeyMessages(k, mapVirtualKeyScan))
	})
}

// SendChromiumClick clicks the Chromium Window top at a screen point: the
// mouse messages of a click (clickMessages) in its client coordinates,
// with no activation pair.
func SendChromiumClick(top uint64, sx, sy int32, button MouseButton, count int) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	if count < 1 || count > 3 {
		return 0, fmt.Errorf("click count %d is not 1, 2 or 3", count)
	}
	p := struct{ X, Y int32 }{sx, sy}
	procScreenToClient.Call(uintptr(top), uintptr(unsafe.Pointer(&p)))
	return sendAll(top, clickMessages(button, count, p.X, p.Y))
}
