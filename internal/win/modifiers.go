package win

// Modifiers on clicks and wheel scrolls (desktop-parity ticket 06):
// `click ... --modifiers ctrl,shift,alt` and `scroll @x,y ... --modifiers
// ctrl`. One set of modifiers is carried to every route: CDP's modifiers
// bitmask on a Page, the MK_* flags of posted mouse messages, and, under
// --headed, the real keys held with SendInput around the click or wheel
// and released on every exit path.

import (
	"fmt"
	"strings"
	"sync"
)

// Modifiers are the keys held during a click or a wheel scroll.
type Modifiers struct {
	Ctrl, Shift, Alt bool
}

// None reports no modifier.
func (m Modifiers) None() bool { return !m.Ctrl && !m.Shift && !m.Alt }

// String is the modifiers the way the agent names them: "ctrl,shift".
func (m Modifiers) String() string {
	var out []string
	if m.Ctrl {
		out = append(out, "ctrl")
	}
	if m.Shift {
		out = append(out, "shift")
	}
	if m.Alt {
		out = append(out, "alt")
	}
	return strings.Join(out, ",")
}

// ParseModifiers reads --modifiers' names (ctrl or control, shift, alt;
// any case). An unknown name is an error.
func ParseModifiers(names []string) (Modifiers, error) {
	var m Modifiers
	for _, n := range names {
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "ctrl", "control":
			m.Ctrl = true
		case "shift":
			m.Shift = true
		case "alt":
			m.Alt = true
		case "":
		default:
			return Modifiers{}, fmt.Errorf("unknown modifier %q: use ctrl, shift or alt", n)
		}
	}
	return m, nil
}

// The MK_* flags the modifiers set in a mouse message's key state.
const (
	mkShift   = 0x0004
	mkControl = 0x0008
)

// MK is the modifiers' MK_* key-state flags. Alt has none: a posted mouse
// message cannot carry it.
func (m Modifiers) MK() uintptr {
	var f uintptr
	if m.Ctrl {
		f |= mkControl
	}
	if m.Shift {
		f |= mkShift
	}
	return f
}

// CDP is the modifiers as Input.dispatchMouseEvent's bitmask (Alt 1,
// Control 2, Shift 8).
func (m Modifiers) CDP() int {
	f := 0
	if m.Alt {
		f |= 1
	}
	if m.Ctrl {
		f |= 2
	}
	if m.Shift {
		f |= 8
	}
	return f
}

// vks are the virtual keys the modifiers hold, in press order.
func (m Modifiers) vks() []uint16 {
	var out []uint16
	if m.Ctrl {
		out = append(out, vkControl)
	}
	if m.Shift {
		out = append(out, vkShift)
	}
	if m.Alt {
		out = append(out, vkMenu)
	}
	return out
}

// holdEvents are the key downs in order and the key ups in reverse.
func holdEvents(vks []uint16, scan func(uint16) uint16) (downs, ups []input) {
	for _, vk := range vks {
		downs = append(downs, keyEvent(vk, scan(vk), false, false))
	}
	for i := len(vks) - 1; i >= 0; i-- {
		ups = append(ups, keyEvent(vks[i], scan(vks[i]), false, true))
	}
	return downs, ups
}

// HoldModifiers presses the modifiers with SendInput and returns the
// release, which sends their key ups once however often it is called. The
// caller defers it, so the keys come up on every exit path, an Esc stop
// included. Headed only: it is real keyboard input.
func HoldModifiers(m Modifiers) (release func() error, err error) {
	if m.None() {
		return func() error { return nil }, nil
	}
	if err := LiveDesktopGuard(); err != nil {
		return nil, err
	}
	downs, ups := holdEvents(m.vks(), mapVirtualKeyScan)
	if err := sendInput(downs...); err != nil {
		// A partial press: bring every key up anyway.
		sendInput(ups...)
		return nil, fmt.Errorf("holding %s failed: %v", m, err)
	}
	var once sync.Once
	return func() error {
		var rerr error
		once.Do(func() {
			if err := sendInput(ups...); err != nil {
				rerr = fmt.Errorf("releasing %s failed: %v", m, err)
			}
		})
		return rerr
	}, nil
}
