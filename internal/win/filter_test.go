package win

import "testing"

// The filter table encodes the ticket's acceptance rules: hidden, cloaked
// (DWM) and tool windows are excluded; owned dialogs are kept.
func TestKeepWindow(t *testing.T) {
	normalBounds := Rect{Left: 0, Top: 0, Right: 800, Bottom: 600}

	cases := []struct {
		name string
		w    WindowInfo
		keep bool
	}{
		{
			name: "plain visible window",
			w:    WindowInfo{ID: 101, Title: "Untitled - Notepad", Class: "Notepad", App: "Notepad", Visible: true, Bounds: normalBounds},
			keep: true,
		},
		{
			name: "hidden window",
			w:    WindowInfo{ID: 102, Title: "x", Visible: false, Bounds: normalBounds},
			keep: false,
		},
		{
			name: "cloaked UWP on another virtual desktop",
			w:    WindowInfo{ID: 103, Title: "Calculator", Visible: true, Cloaked: true, Bounds: normalBounds},
			keep: false,
		},
		{
			name: "tool window",
			w:    WindowInfo{ID: 104, Title: "palette", Visible: true, ExStyle: wsExToolWindow, Bounds: normalBounds},
			keep: false,
		},
		{
			name: "tool window with APPWINDOW override",
			w:    WindowInfo{ID: 105, Title: "forced", Visible: true, ExStyle: wsExToolWindow | wsExAppWindow, Bounds: normalBounds},
			keep: true,
		},
		{
			name: "no-activate window (tooltip/splash)",
			w:    WindowInfo{ID: 106, Title: "tip", Visible: true, ExStyle: wsExNoActivate, Bounds: normalBounds},
			keep: false,
		},
		{
			name: "no-activate with APPWINDOW override",
			w:    WindowInfo{ID: 107, Title: "forced", Visible: true, ExStyle: wsExNoActivate | wsExAppWindow, Bounds: normalBounds},
			keep: true,
		},
		{
			name: "zero-area window",
			w:    WindowInfo{ID: 108, Title: "ghost", Visible: true, Bounds: Rect{0, 0, 0, 0}},
			keep: false,
		},
		{
			name: "minimized window keeps its slot",
			w:    WindowInfo{ID: 109, Title: "Minimized", Visible: true, Minimized: true, Bounds: Rect{}},
			keep: true,
		},
		{
			name: "shell desktop (Progman)",
			w:    WindowInfo{ID: 110, Title: "Program Manager", Class: "Progman", Visible: true, ShellWindow: true, Bounds: normalBounds},
			keep: false,
		},
		{
			name: "taskbar",
			w:    WindowInfo{ID: 111, Class: "Shell_TrayWnd", Visible: true, ShellWindow: true, Bounds: normalBounds},
			keep: false,
		},
		{
			name: "winuse overlay (own process)",
			w:    WindowInfo{ID: 112, Title: "winuse", Visible: true, OwnProcess: true, Bounds: normalBounds},
			keep: false,
		},
		{
			name: "untitled window",
			w:    WindowInfo{ID: 113, Visible: true, Bounds: normalBounds},
			keep: false,
		},
		{
			name: "untitled standard dialog (#32770)",
			w:    WindowInfo{ID: 114, Class: dialogClass, Visible: true, Bounds: normalBounds},
			keep: true,
		},
		{
			name: "owned dialog with title (Save As)",
			w:    WindowInfo{ID: 115, Title: "Save As", Class: dialogClass, Owner: 101, Visible: true, Bounds: normalBounds},
			keep: true,
		},
		{
			name: "owned untitled non-dialog helper",
			w:    WindowInfo{ID: 116, Owner: 101, Visible: true, Bounds: normalBounds},
			keep: false,
		},
	}

	for _, tc := range cases {
		if got := KeepWindow(tc.w); got != tc.keep {
			t.Errorf("%s: KeepWindow = %v, want %v", tc.name, got, tc.keep)
		}
	}
}

func TestVisibleWindowsPreservesZOrder(t *testing.T) {
	bounds := Rect{0, 0, 100, 100}
	all := []WindowInfo{
		{ID: 3, Title: "top", Visible: true, Bounds: bounds},
		{ID: 1, Title: "hidden", Visible: false, Bounds: bounds},
		{ID: 2, Title: "bottom", Visible: true, Bounds: bounds},
	}
	got := VisibleWindows(all)
	want := []uint64{3, 2}
	if len(got) != len(want) {
		t.Fatalf("got %d windows, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d: got window %d, want %d", i, got[i].ID, id)
		}
	}
}

func TestVisibleWindowsEmptyInput(t *testing.T) {
	if got := VisibleWindows(nil); len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

// KeepWindow's semantics for minimized-but-cloaked windows: cloaked wins
// (a minimized window is not cloaked; a suspended UWP is).
func TestCloakedBeatsMinimized(t *testing.T) {
	w := WindowInfo{ID: 9, Title: "suspended", Visible: true, Minimized: true, Cloaked: true}
	if KeepWindow(w) {
		t.Fatal("cloaked windows must be dropped even when minimized")
	}
}
