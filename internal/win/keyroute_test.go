package win

import "testing"

// TestControlClassification pins the pure style reads the keyboard
// routing decides on (ADR 0011, Decision 4): which focused controls are
// classic edit controls, which of them take typed text, which want Return,
// and which controls are push buttons.
func TestControlClassification(t *testing.T) {
	cases := []struct {
		name                                     string
		c                                        Control
		edit, writable, wantsReturn, pw, pushBtn bool
	}{
		{name: "single-line Edit", c: Control{Class: "Edit", Enabled: true},
			edit: true, writable: true},
		{name: "class names match in any case", c: Control{Class: "EDIT", Enabled: true},
			edit: true, writable: true},
		{name: "RichEdit20W", c: Control{Class: "RichEdit20W", Enabled: true}, edit: true, writable: true},
		{name: "RichEdit20A", c: Control{Class: "RichEdit20A", Enabled: true}, edit: true, writable: true},
		{name: "RICHEDIT50W", c: Control{Class: "RICHEDIT50W", Enabled: true}, edit: true, writable: true},
		{name: "RichEditD2DPT (Notepad)", c: Control{Class: "RichEditD2DPT", Enabled: true}, edit: true, writable: true},
		{name: "read-only Edit", c: Control{Class: "Edit", Style: ESReadOnly, Enabled: true}, edit: true},
		{name: "disabled Edit", c: Control{Class: "Edit"}, edit: true},
		{name: "password Edit", c: Control{Class: "Edit", Style: ESPassword, Enabled: true},
			edit: true, writable: true, pw: true},
		{name: "multi-line Edit without ES_WANTRETURN", c: Control{Class: "Edit", Style: ESMultiline, Enabled: true},
			edit: true, writable: true},
		{name: "multi-line Edit with ES_WANTRETURN", c: Control{Class: "Edit", Style: ESMultiline | ESWantReturn, Enabled: true},
			edit: true, writable: true, wantsReturn: true},
		{name: "ES_WANTRETURN alone is not multi-line", c: Control{Class: "Edit", Style: ESWantReturn, Enabled: true},
			edit: true, writable: true},
		{name: "a button's style bits are not edit styles", c: Control{Class: "Button", Style: ESMultiline | ESWantReturn, Enabled: true}},
		{name: "push button", c: Control{Class: "Button", Style: 0x0, Enabled: true}, pushBtn: true},
		{name: "default push button", c: Control{Class: "Button", Style: 0x1, Enabled: true}, pushBtn: true},
		{name: "check box is no push button", c: Control{Class: "Button", Style: 0x3, Enabled: true}},
		{name: "a WPF or Chromium host is no edit", c: Control{Class: "Chrome_RenderWidgetHostHWND", Enabled: true}},
		{name: "no control", c: Control{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.c
			if c.Class != "" {
				c.HWND = 7
			}
			if got := c.IsEdit(); got != tc.edit {
				t.Errorf("IsEdit = %v, want %v", got, tc.edit)
			}
			if got := c.Writable(); got != tc.writable {
				t.Errorf("Writable = %v, want %v", got, tc.writable)
			}
			if got := c.WantsReturn(); got != tc.wantsReturn {
				t.Errorf("WantsReturn = %v, want %v", got, tc.wantsReturn)
			}
			if got := c.Password(); got != tc.pw {
				t.Errorf("Password = %v, want %v", got, tc.pw)
			}
			if got := c.PushButton(); got != tc.pushBtn {
				t.Errorf("PushButton = %v, want %v", got, tc.pushBtn)
			}
		})
	}
}

// TestDialogCommandWParam pins MAKEWPARAM(id, BN_CLICKED): the control ID
// in the low word, the notification (BN_CLICKED is 0) in the high word.
func TestDialogCommandWParam(t *testing.T) {
	if got := dialogCommandWParam(IDCancel); got != 2 {
		t.Errorf("wParam(IDCANCEL) = %#x, want 0x2", got)
	}
	if got := dialogCommandWParam(0xFFFF); got != 0xFFFF {
		t.Errorf("wParam(0xFFFF) = %#x, want 0xFFFF (BN_CLICKED keeps the high word 0)", got)
	}
}

// TestDefaultIDOf pins the DM_GETDEFID reply's decoding: the ID in the
// low word, DC_HASDEFID in the high word, or no default button.
func TestDefaultIDOf(t *testing.T) {
	cases := []struct {
		reply uintptr
		id    uint16
		ok    bool
	}{
		{0x534B0001, 1, true},
		{0x534B03E9, 1001, true},
		{0x00000001, 0, false}, // no DC_HASDEFID
		{0, 0, false},
	}
	for _, tc := range cases {
		id, ok := defaultIDOf(tc.reply)
		if id != tc.id || ok != tc.ok {
			t.Errorf("defaultIDOf(%#x) = %d, %v; want %d, %v", tc.reply, id, ok, tc.id, tc.ok)
		}
	}
}

// TestEditSelectionOf pins EM_GETSEL's reply decoding: the start in the
// low word, the end in the high word, and -1 (either end past 65,535)
// as unknown.
func TestEditSelectionOf(t *testing.T) {
	cases := []struct {
		reply      uintptr
		start, end int
		ok         bool
	}{
		{0, 0, 0, true},
		{0x00050005, 5, 5, true},     // a caret, no selection
		{0x000A0002, 2, 10, true},    // characters 2..10 selected
		{0xFFFF0000, 0, 65535, true}, // everything up to 65,535
		{^uintptr(0), 0, 0, false},   // -1: past 65,535
		{0xFFFFFFFF, 0, 0, false},    // -1 as a 32-bit LRESULT
	}
	for _, tc := range cases {
		start, end, ok := editSelectionOf(tc.reply)
		if start != tc.start || end != tc.end || ok != tc.ok {
			t.Errorf("editSelectionOf(%#x) = %d, %d, %v; want %d, %d, %v", tc.reply, start, end, ok, tc.start, tc.end, tc.ok)
		}
	}
}
