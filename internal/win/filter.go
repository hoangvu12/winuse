package win

// KeepWindow reports whether a collected Window is one a person could see:
// the Alt-Tab-like set, plus owned dialogs.
//
// Dropped:
//   - hidden windows (IsWindowVisible false; Collect prefilters these too);
//   - DWM-cloaked windows: suspended UWP apps, windows on other virtual
//     desktops, hidden shell surfaces;
//   - tool windows (WS_EX_TOOLWINDOW without WS_EX_APPWINDOW): tray icons,
//     tool palettes, our own overlay;
//   - no-activate windows without WS_EX_APPWINDOW (tooltips, splash popups);
//   - zero-area windows (minimized windows keep their slot: they are real
//     Windows a person restores);
//   - shell surfaces (desktop, taskbar);
//   - winuse's own windows (the overlay; a fixture app stays listed).
//
// Kept: owned windows (dialogs such as "Save As") that pass the rest, with
// the owner's App; an owned Window needs a title or the standard dialog
// class (#32770) so helper windows owned by apps stay hidden.
func KeepWindow(w WindowInfo) bool {
	if !w.Visible || w.Cloaked {
		return false
	}
	if w.ExStyle&wsExToolWindow != 0 && w.ExStyle&wsExAppWindow == 0 {
		return false
	}
	if w.ExStyle&wsExNoActivate != 0 && w.ExStyle&wsExAppWindow == 0 {
		return false
	}
	if w.ShellWindow || w.OwnProcess {
		return false
	}
	if !w.Minimized && (w.Bounds.Width() <= 0 || w.Bounds.Height() <= 0) {
		return false
	}
	if w.Title == "" && w.Class != dialogClass {
		return false
	}
	return true
}

// tooltipClasses are window classes that only ever show a tooltip or a
// shadow: never a Window an agent reads next.
var tooltipClasses = map[string]bool{
	"tooltips_class32": true, // common-controls tooltips (Win32, WinForms, Chromium's native tooltips)
	"SysShadow":        true, // the shadow behind a classic menu
}

// OpenedSurface reports whether a collected window is one an Action can
// open and the agent can read next (desktop-parity 02, agent-desktop's
// `data.surfaces`): a Window list-windows keeps, or a popup it drops
// because it is a tool window or has no title, such as a classic menu
// (`#32768`), a WinUI 3 menu (`Microsoft.UI.Content.PopupWindowSiteBridge`
// "PopupHost") or a Chromium menu popup.
//
// Dropped: hidden, cloaked, minimized and zero-area windows, shell
// surfaces, winuse's own windows, tooltips and click-through windows
// (WS_EX_TRANSPARENT: they take no input, so they are no menu or dialog).
func OpenedSurface(w WindowInfo) bool {
	if !w.Visible || w.Cloaked || w.Minimized || w.ShellWindow || w.OwnProcess {
		return false
	}
	if w.Bounds.Width() <= 0 || w.Bounds.Height() <= 0 {
		return false
	}
	if tooltipClasses[w.Class] || w.ExStyle&wsExTransparent != 0 {
		return false
	}
	return true
}

// VisibleWindows filters collected windows down to the visible set, in the
// given (z) order, topmost first.
func VisibleWindows(all []WindowInfo) []WindowInfo {
	out := make([]WindowInfo, 0, len(all))
	for _, w := range all {
		if KeepWindow(w) {
			out = append(out, w)
		}
	}
	return out
}
