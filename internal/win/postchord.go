package win

// Posted chords through attached key state (headless-max ticket 07; ADR
// 0013 rule 2, opt-in through press-key --post). A posted message does
// not change the keyboard state an app reads with GetKeyState, so a
// posted Control_L+s arrives as a plain s. This route makes the state
// true for the target's thread instead, without touching the user's:
//
//  1. AttachThreadInput(a disposable thread of winuse's, the target
//     Window's thread), which shares one input state between the two;
//  2. SetKeyboardState with the chord's modifiers down (VK_CONTROL and
//     VK_LCONTROL, and so on);
//  3. for WPF, Chromium and Alt chords, the activation pair (WM_NCACTIVATE,
//     WM_ACTIVATE, WM_SETFOCUS sent to the top-level Window): without it
//     WPF's KeyBindings do not fire and an Alt menu does not open;
//  4. WM_KEYDOWN (WM_SYSKEYDOWN with the Alt context bit for an Alt chord)
//     posted to the focused control, held 150 ms, then the key up;
//  5. the activation undone (WM_KILLFOCUS, WM_ACTIVATE inactive,
//     WM_NCACTIVATE 0), the saved state restored, and the thread detached
//     on every exit path.
//
// Measured on the laptop (probes round 4, section 5): a Win32
// TranslateAccelerator loop, WinForms ShortcutKeys, WPF KeyBindings (with
// the pair) and a Win32 Alt+F menu (with the pair) all fired; the user's
// async key state stayed 0 and the foreground and cursor were unchanged.
// Chromium and Electron chords (Ctrl+A, Shift+Tab) through the same state
// and pair were measured in round 3. MFC is unmeasured and treated as
// Win32. Ctrl+Alt chords are unmeasured and refused.
//
// The thread owning the foreground is refused before anything is sent:
// attaching to it would share the user's own keyboard state. The work
// runs on a disposable OS-locked thread with a bound, so a hung target
// cannot stall the helper. Everything refuses inside a go test binary
// (LiveDesktopGuard); the plan and the messages are pure and tested
// offline.

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/keys"
)

var (
	procSetKeyboardState = user32.NewProc("SetKeyboardState")
	procGetKeyboardState = user32.NewProc("GetKeyboardState")
)

const (
	wmNcActivate  = 0x0086
	wmActivate    = 0x0006
	wmSetFocus    = 0x0007
	wmKillFocus   = 0x0008
	waActive      = 1
	altContextBit = 1 << 29 // lParam bit 29: Alt is down

	vkShift, vkLShift  = 0x10, 0xA0
	vkControl, vkLCtrl = 0x11, 0xA2
	vkMenu, vkLMenu    = 0x12, 0xA4
	vkRShift, vkRCtrl  = 0xA1, 0xA3
	vkRMenu            = 0xA5
	keyStateDown       = 0x80

	// chordHold is how long the modifiers stay down after the key down is
	// posted, so the app reads them when it handles it (probes round 4).
	chordHold = 150 * time.Millisecond
	// chordBound bounds the whole chord on its disposable thread.
	chordBound = 3 * time.Second
	// chordActivateTimeout bounds each activation message.
	chordActivateTimeout = 500 * time.Millisecond
)

// ErrChordForeground refuses a chord into the thread that owns the
// foreground: attaching would share the user's keyboard state.
var ErrChordForeground = errors.New("the Window's thread owns the foreground, and winuse does not share the user's keyboard state")

// ChordPlan is how a chord is posted: the modifiers to hold, the key, and
// whether it is an Alt (system) chord and needs the activation pair.
type ChordPlan struct {
	Mods     []uint16 // the virtual keys to set down: each generic and left key of the chord's modifiers
	Key      keys.Key // the one key that is posted
	Sys      bool     // an Alt chord: WM_SYSKEYDOWN with the Alt context bit
	Activate bool     // send the activation pair around it
}

// PlanPostedChord decides how chord is posted to a top-level Window of
// class topClass. It fails for a chord it cannot post: no key besides
// modifiers, more than one such key, or Control and Alt together
// (unmeasured).
func PlanPostedChord(chord keys.Chord, topClass string) (ChordPlan, error) {
	var p ChordPlan
	var ctrl, alt, shift, found bool
	for _, k := range chord {
		switch k.VK {
		case vkControl, vkLCtrl, vkRCtrl:
			ctrl = true
		case vkMenu, vkLMenu, vkRMenu:
			alt = true
		case vkShift, vkLShift, vkRShift:
			shift = true
		default:
			if found {
				return ChordPlan{}, fmt.Errorf("%s holds more than one key besides its modifiers", chord)
			}
			p.Key, found = k, true
		}
	}
	switch {
	case !found:
		return ChordPlan{}, fmt.Errorf("%s is only modifiers; a chord needs a key besides them", chord)
	case ctrl && alt:
		return ChordPlan{}, fmt.Errorf("%s holds Control and Alt together, which winuse has not measured posting", chord)
	}
	if ctrl {
		p.Mods = append(p.Mods, vkControl, vkLCtrl)
	}
	if shift {
		p.Mods = append(p.Mods, vkShift, vkLShift)
	}
	if alt {
		p.Mods = append(p.Mods, vkMenu, vkLMenu)
		p.Sys = true
	}
	p.Activate = alt || chordNeedsActivation(topClass)
	return p, nil
}

// chordNeedsActivation reports a top-level class whose keys reach their
// handlers only while the Window believes it is active: WPF (HwndWrapper)
// and Chromium (Chrome_WidgetWin_*).
func chordNeedsActivation(class string) bool {
	return strings.HasPrefix(class, "HwndWrapper[") || strings.HasPrefix(class, "Chrome_WidgetWin_")
}

// chordMessages is the key down and key up the plan posts.
func chordMessages(p ChordPlan, scan func(uint16) uint16) (down, up postedMessage) {
	sc := scan(p.Key.VK)
	dl, ul := keyLParam(sc, p.Key.Extended, false), keyLParam(sc, p.Key.Extended, true)
	dm, um := uint32(wmKeyDown), uint32(wmKeyUp)
	if p.Sys {
		dl |= altContextBit
		ul |= altContextBit
		dm, um = wmSysKeyDown, wmSysKeyUp
	}
	return postedMessage{dm, uintptr(p.Key.VK), dl}, postedMessage{um, uintptr(p.Key.VK), ul}
}

// heldState is saved with the plan's modifiers set down.
func heldState(saved [256]byte, mods []uint16) [256]byte {
	held := saved
	for _, vk := range mods {
		held[vk&0xFF] |= keyStateDown
	}
	return held
}

// PostChord posts a chord planned by PlanPostedChord: top is the
// top-level Window (the activation pair's target and the thread to
// attach to), target its focused control or itself. It returns how many
// key messages were posted (0: nothing was sent).
func PostChord(top, target uint64, p ChordPlan) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread() // the attach binds this OS thread; it is never unlocked, so it exits with the goroutine
		n, err := postChordOnThread(top, target, p)
		done <- result{n, err}
	}()
	select {
	case r := <-done:
		return r.n, r.err
	case <-time.After(chordBound):
		return 1, fmt.Errorf("the chord into window %d did not finish within %s; it may have been posted", top, chordBound)
	}
}

// postChordOnThread runs the sequence of the file comment on the calling,
// OS-locked thread.
func postChordOnThread(top, target uint64, p ChordPlan) (int, error) {
	tid, err := windows.GetWindowThreadProcessId(windows.HWND(top), nil)
	if err != nil || tid == 0 {
		return 0, fmt.Errorf("window %d is gone", top)
	}
	if fg := windows.GetForegroundWindow(); fg != 0 {
		if ftid, _ := windows.GetWindowThreadProcessId(fg, nil); ftid == tid {
			return 0, ErrChordForeground
		}
	}
	me := windows.GetCurrentThreadId()
	if r, _, err := procAttachThreadInput.Call(uintptr(me), uintptr(tid), 1); r == 0 {
		return 0, fmt.Errorf("attaching to the thread of window %d failed: %v", top, err)
	}
	defer procAttachThreadInput.Call(uintptr(me), uintptr(tid), 0)

	var saved [256]byte
	if r, _, err := procGetKeyboardState.Call(uintptr(unsafe.Pointer(&saved))); r == 0 {
		return 0, fmt.Errorf("reading the keyboard state of window %d's thread failed: %v", top, err)
	}
	held := heldState(saved, p.Mods)
	if r, _, err := procSetKeyboardState.Call(uintptr(unsafe.Pointer(&held))); r == 0 {
		return 0, fmt.Errorf("setting the keyboard state of window %d's thread failed: %v", top, err)
	}
	defer procSetKeyboardState.Call(uintptr(unsafe.Pointer(&saved)))

	h := windows.HWND(top)
	if p.Activate {
		sendMessageTimeout(h, wmNcActivate, 1, 0, chordActivateTimeout)
		sendMessageTimeout(h, wmActivate, waActive, 0, chordActivateTimeout)
		sendMessageTimeout(h, wmSetFocus, 0, 0, chordActivateTimeout)
		defer func() {
			sendMessageTimeout(h, wmKillFocus, 0, 0, chordActivateTimeout)
			sendMessageTimeout(h, wmActivate, 0, 0, chordActivateTimeout)
			sendMessageTimeout(h, wmNcActivate, 0, 0, chordActivateTimeout)
		}()
	}
	down, up := chordMessages(p, mapVirtualKeyScan)
	if n, err := postAll(target, []postedMessage{down}); n == 0 {
		return 0, err
	}
	time.Sleep(chordHold)
	if _, err := postAll(target, []postedMessage{up}); err != nil {
		return 1, err
	}
	time.Sleep(chordHold / 3) // the key up handled before the state is restored
	return 2, nil
}
