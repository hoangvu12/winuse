package win

import "testing"

// shellKind recognises the shell surfaces by class, and the CoreWindow
// flyouts by their host process (desktop-parity 09), the toast banner by
// its caption too (desktop-parity 18).
func TestShellKind(t *testing.T) {
	cases := []struct {
		class, exe, title, want string
	}{
		{"Shell_TrayWnd", `C:\Windows\explorer.exe`, "", ShellTaskbar},
		{"Shell_SecondaryTrayWnd", `C:\Windows\explorer.exe`, "", ShellTaskbar},
		{"TopLevelWindowForOverflowXamlIsland", `C:\Windows\explorer.exe`, "", ShellTrayOverflow},
		{"NotifyIconOverflowWindow", `C:\Windows\explorer.exe`, "", ShellTrayOverflow},
		{"Windows.UI.Core.CoreWindow", `C:\Windows\SystemApps\Microsoft.Windows.StartMenuExperienceHost_cw5n1h2txyewy\StartMenuExperienceHost.exe`, "", ShellStart},
		{"Windows.UI.Core.CoreWindow", `C:\Windows\SystemApps\ShellExperienceHost_cw5n1h2txyewy\ShellExperienceHost.exe`, "", ShellActionCenter},
		{"Windows.UI.Core.CoreWindow", `C:\Windows\SystemApps\ShellExperienceHost_cw5n1h2txyewy\ShellExperienceHost.exe`, "Notification Center", ShellActionCenter},
		// The toast banner (desktop-parity 18): ShellExperienceHost's
		// reused CoreWindow, told from the panel by its caption alone.
		{"Windows.UI.Core.CoreWindow", `C:\Windows\SystemApps\ShellExperienceHost_cw5n1h2txyewy\ShellExperienceHost.exe`, "New notification", ShellNotification},
		{"Windows.UI.Core.CoreWindow", `C:\Windows\SystemApps\MicrosoftWindows.Client.CBS_cw5n1h2txyewy\ShellHost.exe`, "New notification", ShellActionCenter},
		{"Windows.UI.Core.CoreWindow", `C:\Windows\SystemApps\MicrosoftWindows.Client.CBS_cw5n1h2txyewy\ShellHost.exe`, "", ShellActionCenter},
		{"Windows.UI.Core.CoreWindow", `C:\Windows\SystemApps\MicrosoftWindows.Client.CBS_cw5n1h2txyewy\TextInputHost.exe`, "", ""},
		{"Progman", `C:\Windows\explorer.exe`, "", ""}, // the desktop stays out
		{"CabinetWClass", `C:\Windows\explorer.exe`, "", ""},
		{"Notepad", `C:\Windows\notepad.exe`, "", ""},
	}
	for _, c := range cases {
		if got := shellKind(c.class, c.exe, c.title); got != c.want {
			t.Errorf("shellKind(%q, %q, %q) = %q, want %q", c.class, c.exe, c.title, got, c.want)
		}
	}
}

func TestShellSurface(t *testing.T) {
	b := Rect{Left: 0, Top: 1368, Right: 2560, Bottom: 1440}
	cases := []struct {
		name string
		w    WindowInfo
		want bool
	}{
		{"taskbar", WindowInfo{Shell: ShellTaskbar, ShellWindow: true, ExStyle: wsExToolWindow, Visible: true, Bounds: b}, true},
		{"open overflow", WindowInfo{Shell: ShellTrayOverflow, ExStyle: wsExToolWindow, Visible: true, Bounds: b}, true},
		{"closed overflow", WindowInfo{Shell: ShellTrayOverflow, Bounds: b}, false},
		{"closed Start (cloaked)", WindowInfo{Shell: ShellStart, Visible: true, Cloaked: true, Bounds: b}, false},
		{"zero area", WindowInfo{Shell: ShellActionCenter, Visible: true}, false},
		{"any other window", WindowInfo{Title: "x", Visible: true, Bounds: b}, false},
	}
	for _, c := range cases {
		if got := ShellSurface(c.w); got != c.want {
			t.Errorf("%s: ShellSurface = %v, want %v", c.name, got, c.want)
		}
	}
}

// The shell surfaces join the visible Windows in z-order; list-windows'
// own filter keeps dropping them.
func TestWithShellSurfacesKeepsZOrder(t *testing.T) {
	b := Rect{0, 0, 100, 100}
	all := []WindowInfo{
		{ID: 1, Class: "Shell_TrayWnd", Shell: ShellTaskbar, ShellWindow: true, ExStyle: wsExToolWindow, Visible: true, Bounds: b},
		{ID: 2, Title: "top", Visible: true, Bounds: b},
		{ID: 3, Title: "Start", Shell: ShellStart, Visible: true, Cloaked: true, Bounds: b},
		{ID: 4, Class: "Progman", Title: "Program Manager", ShellWindow: true, Visible: true, Bounds: b},
		{ID: 5, Title: "bottom", Visible: true, Bounds: b},
	}
	got := WithShellSurfaces(all)
	want := []uint64{1, 2, 5}
	if len(got) != len(want) {
		t.Fatalf("got %d windows, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d: got window %d, want %d", i, got[i].ID, id)
		}
	}
	if vis := VisibleWindows(all); len(vis) != 2 {
		t.Errorf("VisibleWindows kept %d windows, want the 2 that are not shell surfaces", len(vis))
	}
}

func TestFormatWindowsNamesTheTaskbar(t *testing.T) {
	ws := []WindowInfo{
		{ID: 900, App: "Windows Explorer", Shell: ShellTaskbar},
		{ID: 950, App: "Windows Explorer", Title: "System tray overflow window.", Shell: ShellTrayOverflow},
	}
	want := "900\tWindows Explorer\tTaskbar\n950\tWindows Explorer\tSystem tray overflow window.\n"
	if got := FormatWindows(ws); got != want {
		t.Errorf("FormatWindows:\n got %q\nwant %q", got, want)
	}
}

// The overflow's Escape is one press: a key down, then a key up with the
// key-up bits.
func TestOverflowEscapeMessages(t *testing.T) {
	msgs := overflowEscapeMessages()
	if len(msgs) != 2 || msgs[0].msg != wmKeyDown || msgs[1].msg != wmKeyUp {
		t.Fatalf("messages = %+v, want WM_KEYDOWN then WM_KEYUP", msgs)
	}
	for _, m := range msgs {
		if m.wparam != vkEscape {
			t.Errorf("wParam = %#x, want VK_ESCAPE", m.wparam)
		}
	}
	if msgs[0].lparam&(1<<31) != 0 || msgs[1].lparam&(1<<31) == 0 {
		t.Errorf("lParams = %#x, %#x: the transition bit belongs on the key up", msgs[0].lparam, msgs[1].lparam)
	}
}

func TestShellKindOfRefusesInATestBinary(t *testing.T) {
	if got := ShellKindOf(1); got != "" {
		t.Errorf("ShellKindOf read the desktop inside a test: %q", got)
	}
	if _, err := PostOverflowEscape(1); err == nil {
		t.Error("PostOverflowEscape posted inside a test")
	}
}

// The shell's immersive CoreWindows, which EnumWindows leaves out, join the
// enumeration before it; other Apps' CoreWindows and the ones it already
// has do not (desktop-parity 18).
func TestWithShellCoreWindows(t *testing.T) {
	shellHost := func(h uintptr) bool { return h == 10 || h == 11 || h == 3 }
	got := withShellCoreWindows([]uintptr{1, 2, 3}, []uintptr{10, 20, 3, 11}, shellHost)
	want := []uintptr{10, 11, 1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if got := withShellCoreWindows([]uintptr{1}, []uintptr{20}, shellHost); len(got) != 1 {
		t.Errorf("an App's CoreWindow joined: %v", got)
	}
}

// An open Start or toast banner stays out of the plain listing.
func TestKeepWindowDropsShellSurfaces(t *testing.T) {
	b := Rect{0, 0, 100, 100}
	for _, kind := range []string{ShellStart, ShellActionCenter, ShellNotification, ShellTrayOverflow} {
		if KeepWindow(WindowInfo{Title: "x", Shell: kind, Visible: true, Bounds: b}) {
			t.Errorf("KeepWindow kept an open %s", kind)
		}
	}
}
