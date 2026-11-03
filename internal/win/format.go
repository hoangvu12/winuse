package win

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatWindows renders the `list-windows` output: one line per Window,
//
//	<Window ID>\t<App>\t<title>
//
// in the given order (z-order, topmost first). Tab-separated so a harness
// can split lines unambiguously (split on "\t" with limit 3: the title may
// contain anything). Empty output means no visible Window. The untitled
// taskbar of `list-windows --shell` is titled "Taskbar" (ListedTitle).
func FormatWindows(ws []WindowInfo) string {
	var b strings.Builder
	for _, w := range ws {
		b.WriteString(strconv.FormatUint(w.ID, 10))
		b.WriteByte('\t')
		b.WriteString(w.App)
		b.WriteByte('\t')
		b.WriteString(ListedTitle(w))
		b.WriteByte('\n')
	}
	return b.String()
}

// WindowDetails are the facts `list-windows --details` adds to a
// Window's line (desktop-parity 22) that WindowInfo does not carry; the
// frame and pid come from WindowInfo itself.
type WindowDetails struct {
	Focused   bool // the foreground Window
	Maximized bool // IsZoomed
	Admin     bool // a higher-integrity (administrator) process: winuse cannot send it input
}

// FormatWindowsDetailed renders `list-windows --details`: one line per
// Window,
//
//	<Window ID>\t<App>\t<x>,<y> <W>x<H> pid <pid>[ focused][ minimized][ maximized][ admin]\t<title>
//
// The details column sits before the title so the title stays last
// (split on "\t" with limit 4). The frame is the visible frame (DWM
// extended frame bounds), the numbers move-window and resize-window take;
// the flags appear only when true, in that order. d[i] belongs to ws[i].
func FormatWindowsDetailed(ws []WindowInfo, d []WindowDetails) string {
	var b strings.Builder
	for i, w := range ws {
		b.WriteString(strconv.FormatUint(w.ID, 10))
		b.WriteByte('\t')
		b.WriteString(w.App)
		b.WriteByte('\t')
		fmt.Fprintf(&b, "%d,%d %dx%d pid %d", w.Bounds.Left, w.Bounds.Top, w.Bounds.Width(), w.Bounds.Height(), w.PID)
		var det WindowDetails
		if i < len(d) {
			det = d[i]
		}
		if det.Focused {
			b.WriteString(" focused")
		}
		if w.Minimized {
			b.WriteString(" minimized")
		}
		if det.Maximized {
			b.WriteString(" maximized")
		}
		if det.Admin {
			b.WriteString(" admin")
		}
		b.WriteByte('\t')
		b.WriteString(ListedTitle(w))
		b.WriteByte('\n')
	}
	return b.String()
}
