package win

import (
	"testing"

	"github.com/hoangvu12/winuse/internal/keys"
)

// TestKeyLParam pins the posted key messages' lParam (WM_KEYDOWN and
// WM_KEYUP docs): repeat count 1 in bits 0-15, the scan code in bits
// 16-23, the extended-key flag in bit 24, the context code (bit 29) 0,
// and for a key up the previous-state (bit 30) and transition (bit 31)
// bits set.
func TestKeyLParam(t *testing.T) {
	cases := []struct {
		name     string
		scan     uint16
		extended bool
		up       bool
		want     uint32
	}{
		{"Return down", 0x1C, false, false, 0x001C0001},
		{"Return up", 0x1C, false, true, 0xC01C0001},
		{"KP_Enter down (extended)", 0x1C, true, false, 0x011C0001},
		{"Right arrow up (extended)", 0x4D, true, true, 0xC14D0001},
		{"a scan code wider than 8 bits keeps its low byte", 0x1E01, false, false, 0x00010001},
	}
	for _, tc := range cases {
		if got := keyLParam(tc.scan, tc.extended, tc.up); got != uintptr(tc.want) {
			t.Errorf("%s: keyLParam = %#x, want %#x", tc.name, got, tc.want)
		}
	}
}

// TestPostedKeyMessages pins the order of a posted chord: every key down
// in order, then every key up in reverse, each with its scan code.
func TestPostedKeyMessages(t *testing.T) {
	chord, err := keys.Parse("a+Right")
	if err != nil {
		t.Fatal(err)
	}
	got := postedKeyMessages(chord, fakeScans)
	want := []postedMessage{
		{wmKeyDown, 0x41, keyLParam(0x141, false, false)},
		{wmKeyDown, 0x27, keyLParam(0x127, true, false)},
		{wmKeyUp, 0x27, keyLParam(0x127, true, true)},
		{wmKeyUp, 0x41, keyLParam(0x141, false, true)},
	}
	if len(got) != len(want) {
		t.Fatalf("messages = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestPostedCharUnits pins the WM_CHAR stream of posted text: one message
// per UTF-16 unit (an emoji is a surrogate pair), a newline as the
// carriage return Return types (a CRLF pair as one), and a tab as itself.
func TestPostedCharUnits(t *testing.T) {
	cases := []struct {
		text string
		want []uint16
	}{
		{"ab", []uint16{'a', 'b'}},
		{"é世", []uint16{0xE9, 0x4E16}},
		{"🎉", []uint16{0xD83C, 0xDF89}},
		{"a\nb", []uint16{'a', '\r', 'b'}},
		{"a\r\nb", []uint16{'a', '\r', 'b'}},
		{"a\rb", []uint16{'a', '\r', 'b'}},
		{"a\tb", []uint16{'a', '\t', 'b'}},
		{"", nil},
	}
	for _, tc := range cases {
		got := postedCharUnits(tc.text)
		if len(got) != len(tc.want) {
			t.Errorf("%q: units = %#x, want %#x", tc.text, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q: units = %#x, want %#x", tc.text, got, tc.want)
				break
			}
		}
	}
}
