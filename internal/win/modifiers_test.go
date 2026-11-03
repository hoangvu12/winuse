package win

// Modifiers on clicks and wheel scrolls (desktop-parity ticket 06): the
// names, the MK_* and CDP encodings, the hold's key order and the posted
// key state. Nothing is sent.

import (
	"errors"
	"slices"
	"testing"
)

func TestParseModifiers(t *testing.T) {
	m, err := ParseModifiers([]string{"Ctrl", " shift", "control"})
	if err != nil || !m.Ctrl || !m.Shift || m.Alt || m.String() != "ctrl,shift" {
		t.Errorf("ParseModifiers = %+v, %v", m, err)
	}
	if m, err := ParseModifiers(nil); err != nil || !m.None() {
		t.Errorf("no modifiers = %+v, %v", m, err)
	}
	if _, err := ParseModifiers([]string{"win"}); err == nil {
		t.Error("an unknown modifier must be refused")
	}
}

func TestModifierEncodings(t *testing.T) {
	m := Modifiers{Ctrl: true, Shift: true, Alt: true}
	if m.MK() != mkControl|mkShift {
		t.Errorf("MK = %#x, want MK_CONTROL|MK_SHIFT (Alt has no MK flag)", m.MK())
	}
	if m.CDP() != 1|2|8 {
		t.Errorf("CDP = %d, want 11", m.CDP())
	}
	if (Modifiers{Ctrl: true}).CDP() != 2 || (Modifiers{Shift: true}).CDP() != 8 {
		t.Error("CDP: Control is 2, Shift is 8")
	}
}

func TestHoldEventsPressInOrderReleaseInReverse(t *testing.T) {
	scan := func(vk uint16) uint16 { return vk }
	downs, ups := holdEvents(Modifiers{Ctrl: true, Shift: true}.vks(), scan)
	if len(downs) != 2 || len(ups) != 2 {
		t.Fatalf("downs %d, ups %d", len(downs), len(ups))
	}
	want := []input{keyEvent(vkControl, vkControl, false, false), keyEvent(vkShift, vkShift, false, false)}
	if !slices.Equal(downs, want) {
		t.Errorf("downs = %+v, want Control then Shift", downs)
	}
	wantUps := []input{keyEvent(vkShift, vkShift, false, true), keyEvent(vkControl, vkControl, false, true)}
	if !slices.Equal(ups, wantUps) {
		t.Errorf("ups = %+v, want Shift then Control", ups)
	}
}

func TestWithKeyStateSetsMKOnMouseMessages(t *testing.T) {
	msgs := withKeyState(clickMessages(MouseLeft, 1, 1, 2), mkControl)
	for _, m := range msgs {
		switch m.msg {
		case wmCancelMode:
			if m.wparam != 0 {
				t.Errorf("WM_CANCELMODE carries %#x", m.wparam)
			}
		case wmLButtonDown:
			if m.wparam != mkLButton|mkControl {
				t.Errorf("the down carries %#x, want MK_LBUTTON|MK_CONTROL", m.wparam)
			}
		default:
			if m.wparam&mkControl == 0 {
				t.Errorf("%#x lacks MK_CONTROL: %#x", m.msg, m.wparam)
			}
		}
	}
	wheel := withKeyState(wheelMessages(0, 0, 0, 120), mkControl)
	if len(wheel) != 1 || wheel[0].wparam&0xFFFF != mkControl || int16(uint16(wheel[0].wparam>>16)) != -120 {
		t.Errorf("Ctrl+wheel = %+v, want MK_CONTROL in the low word and -120 in the high", wheel)
	}
}

func TestHoldModifiersRefusesInsideTests(t *testing.T) {
	t.Setenv("WINUSE_E2E", "")
	if _, err := HoldModifiers(Modifiers{Ctrl: true}); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("HoldModifiers = %v", err)
	}
	release, err := HoldModifiers(Modifiers{})
	if err != nil || release() != nil {
		t.Errorf("no modifiers must hold nothing: %v", err)
	}
}
