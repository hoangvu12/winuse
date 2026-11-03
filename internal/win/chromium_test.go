package win

// The Chromium message route's sequences (headless-max ticket 20): per
// UTF-16 unit, Return and Tab, surrogate pairs, plain keys, the activation
// pair, and the live guard. Nothing is sent.

import (
	"errors"
	"slices"
	"testing"

	"github.com/hoangvu12/winuse/internal/keys"
)

func TestChromiumUnitMessages(t *testing.T) {
	got := chromiumUnitMessages('é')
	want := []sentMessage{
		{wmKeyDown, vkPacket, keyLParam(0, false, false)},
		{wmChar, 'é', keyLParam(0, false, false)},
		{wmKeyUp, vkPacket, keyLParam(0, false, true)},
	}
	if !slices.Equal(got, want) {
		t.Errorf("é = %+v, want %+v", got, want)
	}
	ret := chromiumUnitMessages('\n')
	if len(ret) != 3 || ret[0].wparam != vkReturn || ret[1].wparam != '\r' || ret[0].lparam != keyLParam(scanReturn, false, false) {
		t.Errorf("a newline = %+v, want Return (scan 0x1C) with a CR", ret)
	}
	tab := chromiumUnitMessages('\t')
	if len(tab) != 3 || tab[0].wparam != vkTab || tab[1].wparam != '\t' || tab[2].lparam != keyLParam(scanTab, false, true) {
		t.Errorf("a tab = %+v, want Tab (scan 0x0F) with a tab", tab)
	}
}

func TestChromiumTextUnits(t *testing.T) {
	// An emoji is two UTF-16 units, each its own VK_PACKET sequence; CR LF
	// is one Return.
	got := chromiumTextUnits("a😀\r\nb\n")
	want := []uint16{'a', 0xD83D, 0xDE00, '\r', 'b', '\n'}
	if !slices.Equal(got, want) {
		t.Errorf("units = %#x, want %#x", got, want)
	}
}

func TestChromiumKeyMessages(t *testing.T) {
	scan := func(vk uint16) uint16 { return vk + 0x100 }
	parse := func(name string) keys.Key {
		c, err := keys.Parse(name)
		if err != nil || len(c) != 1 {
			t.Fatalf("keys.Parse(%q) = %v, %v", name, c, err)
		}
		return c[0]
	}
	cases := []struct {
		key  string
		char uintptr // 0: no WM_CHAR
	}{
		{"Return", '\r'}, {"Tab", '\t'}, {"BackSpace", '\b'}, {"Escape", 0x1B},
		{"space", ' '}, {"a", 'a'}, {"7", '7'}, {"Numpad_3", '3'},
		{"Down", 0}, {"Delete", 0}, {"F5", 0},
	}
	for _, tc := range cases {
		k := parse(tc.key)
		msgs := chromiumKeyMessages(k, scan)
		ids := []uint32{wmKeyDown, wmKeyUp}
		if tc.char != 0 {
			ids = []uint32{wmKeyDown, wmChar, wmKeyUp}
		}
		got := make([]uint32, len(msgs))
		for i, m := range msgs {
			got[i] = m.msg
		}
		if !slices.Equal(got, ids) {
			t.Errorf("%s: messages %#x, want %#x", tc.key, got, ids)
			continue
		}
		if tc.char != 0 && msgs[1].wparam != tc.char {
			t.Errorf("%s: WM_CHAR %q, want %q", tc.key, rune(msgs[1].wparam), rune(tc.char))
		}
		if msgs[0].wparam != uintptr(k.VK) || msgs[0].lparam != keyLParam(k.VK+0x100, k.Extended, false) {
			t.Errorf("%s: key down %+v", tc.key, msgs[0])
		}
	}
}

func TestActivationPairOrder(t *testing.T) {
	on := []uint32{activationOn[0].msg, activationOn[1].msg, activationOn[2].msg}
	off := []uint32{activationOff[0].msg, activationOff[1].msg, activationOff[2].msg}
	if !slices.Equal(on, []uint32{wmNcActivate, wmActivate, wmSetFocus}) || activationOn[0].wparam != 1 || activationOn[1].wparam != waActive {
		t.Errorf("activation on = %+v", activationOn)
	}
	if !slices.Equal(off, []uint32{wmKillFocus, wmActivate, wmNcActivate}) || activationOff[1].wparam != waInactive || activationOff[2].wparam != 0 {
		t.Errorf("activation off = %+v", activationOff)
	}
}

func TestChromiumSendsRefuseInsideTests(t *testing.T) {
	t.Setenv("WINUSE_E2E", "")
	if _, err := SendChromiumText(1, "a", nil); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("SendChromiumText = %v", err)
	}
	if _, err := SendChromiumKey(1, keys.Key{Name: "a", VK: 'A'}); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("SendChromiumKey = %v", err)
	}
	if _, err := SendChromiumClick(1, 0, 0, MouseLeft, 2); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("SendChromiumClick = %v", err)
	}
}
