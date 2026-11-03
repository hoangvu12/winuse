package win

import (
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/keys"
)

func mustChord(t *testing.T, s string) keys.Chord {
	t.Helper()
	c, err := keys.Parse(s)
	if err != nil {
		t.Fatalf("keys.Parse(%q): %v", s, err)
	}
	return c
}

func TestPlanPostedChord(t *testing.T) {
	cases := []struct {
		chord, class string
		mods         []uint16
		key          uint16
		sys, act     bool
	}{
		{"Control_L+s", "Notepad", []uint16{vkControl, vkLCtrl}, 'S', false, false},
		{"Control_R+Shift_L+s", "WindowsForms10.Window.8.app", []uint16{vkControl, vkLCtrl, vkShift, vkLShift}, 'S', false, false},
		{"Shift_L+Tab", "#32770", []uint16{vkShift, vkLShift}, 0x09, false, false},
		// WPF and Chromium need the activation pair.
		{"Control_L+s", "HwndWrapper[App;;abc]", []uint16{vkControl, vkLCtrl}, 'S', false, true},
		{"Control_L+a", "Chrome_WidgetWin_1", []uint16{vkControl, vkLCtrl}, 'A', false, true},
		// Alt is a system chord, always with the pair.
		{"Alt_L+f", "Notepad", []uint16{vkMenu, vkLMenu}, 'F', true, true},
		{"Shift_L+Alt_L+f", "Notepad", []uint16{vkShift, vkLShift, vkMenu, vkLMenu}, 'F', true, true},
	}
	for _, c := range cases {
		p, err := PlanPostedChord(mustChord(t, c.chord), c.class)
		if err != nil {
			t.Errorf("%s: %v", c.chord, err)
			continue
		}
		if p.Key.VK != c.key || p.Sys != c.sys || p.Activate != c.act || !equalVKs(p.Mods, c.mods) {
			t.Errorf("%s on %s = %+v, want mods %x key %x sys %v activate %v", c.chord, c.class, p, c.mods, c.key, c.sys, c.act)
		}
	}
	for chord, want := range map[string]string{
		"Control_L":              "only modifiers",
		"Control_L+Alt_L+Delete": "Control and Alt together",
		"Control_L+a+b":          "more than one key",
	} {
		ch, err := keys.Parse(chord)
		if err != nil {
			continue // a chord the parser itself refuses
		}
		if _, err := PlanPostedChord(ch, "Notepad"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", chord, err, want)
		}
	}
}

func equalVKs(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestChordMessages(t *testing.T) {
	scan := func(vk uint16) uint16 { return 0x1F } // S's scan code
	p, _ := PlanPostedChord(mustChord(t, "Control_L+s"), "Notepad")
	down, up := chordMessages(p, scan)
	if down.msg != wmKeyDown || up.msg != wmKeyUp || down.wparam != 'S' || up.wparam != 'S' {
		t.Errorf("Ctrl+S messages = %+v, %+v, want WM_KEYDOWN/WM_KEYUP of S", down, up)
	}
	if down.lparam&altContextBit != 0 || down.lparam != keyLParam(0x1F, false, false) || up.lparam != keyLParam(0x1F, false, true) {
		t.Errorf("Ctrl+S lParams = %#x, %#x", down.lparam, up.lparam)
	}

	p, _ = PlanPostedChord(mustChord(t, "Alt_L+f"), "Notepad")
	down, up = chordMessages(p, scan)
	if down.msg != wmSysKeyDown || up.msg != wmSysKeyUp {
		t.Errorf("Alt+F messages = %#x, %#x, want WM_SYSKEYDOWN/WM_SYSKEYUP", down.msg, up.msg)
	}
	if down.lparam&altContextBit == 0 || up.lparam&altContextBit == 0 {
		t.Errorf("Alt+F lParams %#x, %#x lack the Alt context bit", down.lparam, up.lparam)
	}
}

func TestHeldStateSetsOnlyTheModifiers(t *testing.T) {
	var saved [256]byte
	saved[0x41] = 0x01 // a toggle bit the user has
	held := heldState(saved, []uint16{vkControl, vkLCtrl})
	if held[vkControl] != keyStateDown || held[vkLCtrl] != keyStateDown {
		t.Errorf("Control = %#x, LControl = %#x, want down", held[vkControl], held[vkLCtrl])
	}
	if held[0x41] != 0x01 || held[vkShift] != 0 {
		t.Errorf("other keys changed: A %#x, Shift %#x", held[0x41], held[vkShift])
	}
	if saved[vkControl] != 0 {
		t.Error("heldState changed the saved state")
	}
}
