package win

// Polite real input (multi-agent ticket 08, ADR 0014 Decision 8's last
// step): the Win32 side of the Headed pipeline's manners. It reads how long
// the user's input has been quiet (GetLastInputInfo), syncs with a
// Window's thread (a WM_NULL round trip, so the activation landed before
// input goes in and the input was handled before anything is restored),
// reads and puts back the cursor, and keeps a ledger of the keys and mouse
// buttons winuse holds down, so every exit path can release them.
//
// The ledger is fed by sendInput, the one SendInput gate: a key or button
// down is held until its up goes through. Unicode text events come in
// down/up pairs and are never held.

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount     = kernel32.NewProc("GetTickCount")
)

// syncTimeout bounds SyncWindow's round trip.
const syncTimeout = 500 * time.Millisecond

// InputIdle is how long the user's input (any process's, injected input
// included) has been quiet: GetLastInputInfo against GetTickCount.
func InputIdle() time.Duration {
	if LiveDesktopGuard() != nil {
		return time.Hour // offline tests: the user's real input never paces them
	}
	var lii struct {
		Size uint32
		Time uint32
	}
	lii.Size = uint32(unsafe.Sizeof(lii))
	if r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii))); r == 0 {
		return time.Hour // unreadable: treat the input as quiet
	}
	now, _, _ := procGetTickCount.Call()
	return time.Duration(uint32(now)-lii.Time) * time.Millisecond
}

// SyncWindow sends WM_NULL to hwnd and waits for its thread to answer
// (SMTO_ABORTIFHUNG, 500 ms): every message the thread had queued before,
// the activation's and the injected input's included, was handled.
func SyncWindow(hwnd uint64) error {
	if LiveDesktopGuard() != nil {
		return nil // offline tests fake their Windows
	}
	if _, err := sendMessageTimeout(windows.HWND(hwnd), wmNull, 0, 0, syncTimeout); err != nil {
		return fmt.Errorf("window %d did not answer: %v", hwnd, err)
	}
	return nil
}

// CursorPos reads the real cursor's position; ok is false when it cannot.
func CursorPos() (x, y int32, ok bool) {
	if LiveDesktopGuard() != nil {
		return 0, 0, false // offline tests never put the real cursor back
	}
	var p point
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p))); r == 0 {
		return 0, 0, false
	}
	return p.X, p.Y, true
}

// PutCursor puts the real cursor back at x, y (SetCursorPos).
func PutCursor(x, y int32) error {
	return moveCursor(x, y)
}

// inputLedger is what winuse holds down: keys by virtual key (with their
// extended flag) and mouse buttons by their up flag.
type inputLedger struct {
	mu      sync.Mutex
	keys    map[uint16]bool   // vk → extended
	buttons map[uint32]uint32 // down flag → up flag
}

var held = &inputLedger{keys: map[uint16]bool{}, buttons: map[uint32]uint32{}}

// buttonUps pairs each mouse button's down flag with its up flag.
var buttonUps = map[uint32]uint32{
	mouseEventfLeftDown:  mouseEventfLeftUp,
	mouseEventfRightDown: mouseEventfRightUp,
	mouseEventfMidDown:   mouseEventfMidUp,
}

// note records the events that went through.
func (l *inputLedger) note(events []input) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range events {
		in := &events[i]
		switch in.Type {
		case inputTypeKeyboard:
			ki := (*keybdInput)(unsafe.Pointer(&in.U))
			if ki.Flags&keyEventfUnicode != 0 {
				continue
			}
			if ki.Flags&keyEventfKeyUp != 0 {
				delete(l.keys, ki.Vk)
			} else {
				l.keys[ki.Vk] = ki.Flags&keyEventfExtendedKey != 0
			}
		case inputTypeMouse:
			mi := (*mouseInput)(unsafe.Pointer(&in.U))
			for down, up := range buttonUps {
				if mi.Flags&down != 0 {
					l.buttons[down] = up
				}
				if mi.Flags&up != 0 {
					delete(l.buttons, down)
				}
			}
		}
	}
}

// releases are the up events for everything still held.
func (l *inputLedger) releases(scan func(uint16) uint16) []input {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []input
	for vk, ext := range l.keys {
		out = append(out, keyEvent(vk, scan(vk), ext, true))
	}
	for _, up := range l.buttons {
		out = append(out, mouseEvent(up))
	}
	return out
}

// ReleaseHeld releases every key and mouse button winuse still holds (a
// step that failed or was stopped between its downs and its ups) and
// returns how many it released. The Headed pipeline calls it on every
// exit path.
func ReleaseHeld() (int, error) {
	ups := held.releases(mapVirtualKeyScan)
	if len(ups) == 0 {
		return 0, nil
	}
	return len(ups), sendInput(ups...)
}
