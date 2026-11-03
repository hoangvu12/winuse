package win

// Posted Clicks and posted wheel scrolls (headless-max ticket 12): the
// message sequences per button and count, the coordinate packing, the
// wheel's signs and chunks, and the class allow-list. Nothing is posted.

import (
	"errors"
	"slices"
	"testing"
)

func msgIDs(msgs []postedMessage) []uint32 {
	out := make([]uint32, len(msgs))
	for i, m := range msgs {
		out[i] = m.msg
	}
	return out
}

func TestClickMessagesPerButtonAndCount(t *testing.T) {
	cases := []struct {
		name   string
		button MouseButton
		count  int
		want   []uint32
		mk     uintptr
	}{
		{"left single", MouseLeft, 1, []uint32{wmMouseMove, wmLButtonDown, wmLButtonUp, wmCancelMode}, mkLButton},
		{"left double", MouseLeft, 2, []uint32{wmMouseMove, wmLButtonDown, wmLButtonUp, wmLButtonDbl, wmLButtonUp, wmCancelMode}, mkLButton},
		{"left triple", MouseLeft, 3, []uint32{wmMouseMove, wmLButtonDown, wmLButtonUp, wmLButtonDbl, wmLButtonUp, wmLButtonDown, wmLButtonUp, wmCancelMode}, mkLButton},
		// No WM_CANCELMODE after a right click: it would close the context
		// menu the up opened.
		{"right", MouseRight, 1, []uint32{wmMouseMove, wmRButtonDown, wmRButtonUp}, mkRButton},
		{"right double", MouseRight, 2, []uint32{wmMouseMove, wmRButtonDown, wmRButtonUp, wmRButtonDbl, wmRButtonUp}, mkRButton},
		{"middle", MouseMiddle, 1, []uint32{wmMouseMove, wmMButtonDown, wmMButtonUp, wmCancelMode}, mkMButton},
	}
	for _, tc := range cases {
		msgs := clickMessages(tc.button, tc.count, 30, 40)
		if got := msgIDs(msgs); !slices.Equal(got, tc.want) {
			t.Errorf("%s: messages %#x, want %#x", tc.name, got, tc.want)
		}
		for _, m := range msgs {
			switch m.msg {
			case wmLButtonDown, wmLButtonDbl, wmRButtonDown, wmRButtonDbl, wmMButtonDown:
				if m.wparam != tc.mk {
					t.Errorf("%s: %#x carries wParam %#x, want the button's MK flag %#x", tc.name, m.msg, m.wparam, tc.mk)
				}
			case wmLButtonUp, wmRButtonUp, wmMButtonUp, wmMouseMove:
				if m.wparam != 0 {
					t.Errorf("%s: %#x carries wParam %#x, want 0 (no button held)", tc.name, m.msg, m.wparam)
				}
			}
			if m.msg != wmCancelMode && m.lparam != mouseLParam(30, 40) {
				t.Errorf("%s: %#x lParam %#x, want the client point", tc.name, m.msg, m.lparam)
			}
		}
	}
}

func TestMouseLParamPacksSignedWords(t *testing.T) {
	if got := mouseLParam(30, 40); got != 40<<16|30 {
		t.Errorf("mouseLParam(30, 40) = %#x", got)
	}
	// A point left of or above the client area is negative in each word.
	if got := mouseLParam(-1, -2); got != 0xFFFEFFFF {
		t.Errorf("mouseLParam(-1, -2) = %#x, want 0xfffeffff", got)
	}
}

func TestWheelMessagesSignsAndChunks(t *testing.T) {
	delta := func(m postedMessage) int16 { return int16(uint16(m.wparam >> 16)) }

	msgs := wheelMessages(500, 600, 0, 120) // down one notch
	if len(msgs) != 1 || msgs[0].msg != wmMouseWheel || delta(msgs[0]) != -120 || msgs[0].lparam != mouseLParam(500, 600) {
		t.Fatalf("down one notch = %+v", msgs)
	}
	msgs = wheelMessages(0, 0, 240, -120) // up one notch, right two
	if len(msgs) != 2 || msgs[0].msg != wmMouseWheel || delta(msgs[0]) != 120 || msgs[1].msg != wmMouseHWheel || delta(msgs[1]) != 240 {
		t.Fatalf("up and right = %+v", msgs)
	}
	// A huge amount splits into chunks within the 16-bit field.
	msgs = wheelMessages(0, 0, 0, 100000)
	sum := 0
	for _, m := range msgs {
		d := int(delta(m))
		if d < -maxWheelChunk || d > maxWheelChunk {
			t.Errorf("chunk %d is outside ±%d", d, maxWheelChunk)
		}
		sum += d
	}
	if sum != -100000 || len(msgs) < 2 {
		t.Errorf("100000 down = %d messages summing to %d, want several summing to -100000", len(msgs), sum)
	}
}

func TestPostableClass(t *testing.T) {
	for _, c := range []string{"Edit", "EDIT", "RichEdit20W", "RICHEDIT50W", "SysListView32", "SysTreeView32", "Button", "ComboBox", "ComboLBox", "ListBox", "WindowsForms10.EDIT.app.0.141b42a_r9_ad1", "WindowsForms10.BUTTON.app.0.2bf8098_r6_ad1"} {
		if !PostableClass(c) {
			t.Errorf("PostableClass(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"", "#32770", "Chrome_RenderWidgetHostHWND", "Windows.UI.Core.CoreWindow", "Afx:00400000:8", "WindowsForms10.Window.8.app.0.1", "DirectUIHWND", "Static"} {
		if PostableClass(c) {
			t.Errorf("PostableClass(%q) = true, want false", c)
		}
	}
}

func TestPostClickAndWheelRefuseInsideTests(t *testing.T) {
	t.Setenv("WINUSE_E2E", "")
	if _, err := PostClick(1, 0, 0, MouseLeft, 1, Modifiers{}); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("PostClick in a test binary = %v", err)
	}
	if _, err := PostWheel(1, 0, 0, 0, 120, Modifiers{}); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("PostWheel in a test binary = %v", err)
	}
}
