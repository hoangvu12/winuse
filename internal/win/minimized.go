package win

// Which minimized Windows still take UI Automation pattern calls
// (headless-max ticket 14). Measured with the Window iconic, the
// foreground and the cursor unchanged throughout, and the tree as large as
// when restored:
//
//   - classic Win32 (probes round 3): Invoke, Value.SetValue,
//     EM_REPLACESEL and SelectionItem.Select all took effect;
//   - WPF and WinUI 3 Desktop (round 4): Invoke, Toggle, SetValue and
//     SelectionItem.Select all took effect.
//
// The exceptions, kept refused:
//
//   - a packaged Store app (ApplicationFrameWindow, round 5): minimized,
//     its UI Automation tree is empty (the app is most likely suspended),
//     and restoring it takes the foreground;
//   - Chromium and Electron (Chrome_WidgetWin_*): the message routes'
//     activation restores a minimized one and takes the foreground (round
//     3), and its frame through UI Automation is unmeasured;
//   - the other frameworks that draw without per-control windows, none of
//     them measured minimized: Firefox, Qt and Java.
//
// Any other class is treated as classic Win32, which includes MFC,
// WinForms and Delphi Windows (real Win32 controls behind the same
// proxies) and WinUI 3's own desktop window class.

import "strings"

// storeAppFrameClass is the frame Windows draws around a packaged Store
// app (owned by ApplicationFrameHost, not by the app's process).
const storeAppFrameClass = "ApplicationFrameWindow"

// StoreAppFrame reports whether class is a packaged Store app's frame.
func StoreAppFrame(class string) bool {
	return class == storeAppFrameClass
}

// MinimizedPatterns reports whether UI Automation pattern Actions reach a
// minimized top-level Window of this class (see the file comment).
func MinimizedPatterns(class string) bool {
	switch {
	case StoreAppFrame(class), class == "Windows.UI.Core.CoreWindow":
		return false
	case strings.HasPrefix(class, "Chrome_WidgetWin_"):
		return false
	case class == "MozillaWindowClass":
		return false
	case strings.HasPrefix(class, "Qt") && strings.Contains(class, "QWindow"):
		return false
	case strings.HasPrefix(class, "SunAwt"):
		return false
	}
	return true
}
