package win

import (
	"strings"
	"testing"
)

func TestMinimizedPatternsByClass(t *testing.T) {
	cases := map[string]bool{
		// Measured working (headless-max ticket 14).
		"#32770":                                       true, // a classic dialog
		"WinuseProbeWindow":                            true, // any classic Win32 class an App registers
		"HwndWrapper[App;;0b7d6d3c]":                   true, // WPF
		"WinUIDesktopWin32WindowClass":                 true, // WinUI 3 Desktop
		"WindowsForms10.Window.8.app.0.141b42a_r6_ad1": true, // WinForms: real Win32 controls
		// Measured or kept refused.
		"ApplicationFrameWindow":     false, // a Store app: no tree while minimized
		"Windows.UI.Core.CoreWindow": false,
		"Chrome_WidgetWin_1":         false, // Chrome, Edge, Electron
		"Chrome_WidgetWin_0":         false,
		"MozillaWindowClass":         false,
		"Qt5152QWindowIcon":          false,
		"Qt6110QWindowIcon":          false,
		"SunAwtFrame":                false,
	}
	for class, want := range cases {
		if got := MinimizedPatterns(class); got != want {
			t.Errorf("MinimizedPatterns(%q) = %v, want %v", class, got, want)
		}
	}
}

func TestMinimizedErrorNamesAStoreApp(t *testing.T) {
	if got := (&MinimizedError{WindowID: 7, Class: "Notepad"}).Error(); got != "window 7 is minimized" {
		t.Errorf("classic: %q", got)
	}
	got := (&MinimizedError{WindowID: 7, Class: "ApplicationFrameWindow"}).Error()
	if !strings.Contains(got, "window 7 is minimized") || !strings.Contains(got, "a minimized Store app has no UI to act on") {
		t.Errorf("Store app: %q", got)
	}
}
