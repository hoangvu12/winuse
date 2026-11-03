package win

// Shell surfaces (desktop-parity 09, ADR 0015): the taskbar, the tray
// overflow, Start, the Action Center and a showing notification banner
// (desktop-parity 18). list-windows drops them
// (KeepWindow: they are not Windows a person works in), and
// `list-windows --shell` adds them back, so the agent can read them as
// Window States and act on their Elements by Window ID.
//
// A surface is recognised by its window class, and the flyouts that share
// the UWP CoreWindow class by their host process: class names and
// executables do not change with the display language, titles do. The
// one exception is the notification banner: ShellExperienceHost shows its
// toasts in a CoreWindow of its own, reused for every toast, and only the
// caption tells it from the panel (probes round 5, section 7).

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The shell surface kinds (WindowInfo.Shell).
const (
	ShellTaskbar      = "taskbar"             // Shell_TrayWnd, and Shell_SecondaryTrayWnd on other monitors
	ShellTrayOverflow = "tray overflow"       // the hidden icons, shown by the taskbar's chevron
	ShellStart        = "Start"               // the Start menu, while open
	ShellActionCenter = "Action Center"       // the notification, calendar and quick settings panels, while open
	ShellNotification = "notification banner" // a toast showing on its own, outside the panel (desktop-parity 18)
)

// notificationBannerTitle is the caption of ShellExperienceHost's toast
// banner CoreWindow, as measured on an English Windows 11 (probes round
// 5, section 7). Under another display language the caption differs and
// the banner is taken for the Action Center, as before ticket 18.
const notificationBannerTitle = "New notification"

// shellSurfaceClasses are the surfaces known by their window class.
var shellSurfaceClasses = map[string]string{
	"Shell_TrayWnd":                       ShellTaskbar,
	"Shell_SecondaryTrayWnd":              ShellTaskbar,
	"TopLevelWindowForOverflowXamlIsland": ShellTrayOverflow, // Windows 11
	"NotifyIconOverflowWindow":            ShellTrayOverflow, // Windows 10
}

// shellFlyoutHosts are the processes whose CoreWindows are shell flyouts:
// Start, and the panels of the Action Center (ShellExperienceHost's
// notification and calendar panel; quick settings, which recent builds
// host in ShellHost). Their CoreWindows stay hidden or cloaked while
// closed.
var shellFlyoutHosts = map[string]string{
	"startmenuexperiencehost.exe": ShellStart,
	"shellexperiencehost.exe":     ShellActionCenter,
	"shellhost.exe":               ShellActionCenter,
}

// topLevelCoreWindows lists every top-level UWP CoreWindow in z-order,
// topmost first, through FindWindowEx: unlike EnumWindows, which on
// Windows 8 and later enumerates the top-level windows of desktop apps
// only, it reaches the shell's immersive CoreWindows (desktop-parity 18:
// the toast banner, Start and the Action Center were never listed).
func topLevelCoreWindows() []uintptr {
	var out []uintptr
	var prev uintptr
	for len(out) < 1024 {
		h, _, _ := procFindWindowExW.Call(0, prev, uintptr(unsafe.Pointer(coreWindowClass)), 0)
		if h == 0 {
			break
		}
		out = append(out, h)
		prev = h
	}
	return out
}

// isShellFlyoutHost reports whether a window belongs to one of the
// shellFlyoutHosts processes.
func isShellFlyoutHost(hwnd uintptr) bool {
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid); err != nil || pid == 0 {
		return false
	}
	path, _ := exePath(pid)
	_, ok := shellFlyoutHosts[strings.ToLower(filepath.Base(path))]
	return ok
}

// withShellCoreWindows adds to an EnumWindows enumeration the CoreWindows
// of the shell flyout hosts it left out, before it: they sit in the
// immersive bands above the desktop's windows, in their own z-order. Pure
// given shellHost: offline-testable.
func withShellCoreWindows(enumerated, coreWindows []uintptr, shellHost func(uintptr) bool) []uintptr {
	known := make(map[uintptr]bool, len(enumerated))
	for _, h := range enumerated {
		known[h] = true
	}
	var extra []uintptr
	for _, h := range coreWindows {
		if !known[h] && shellHost(h) {
			extra = append(extra, h)
		}
	}
	if len(extra) == 0 {
		return enumerated
	}
	return append(extra, enumerated...)
}

// shellKind names the shell surface a window is, from its class, the
// path of its process and, for ShellExperienceHost's toast banner, its
// caption: "" for any other window. Pure: offline-testable.
func shellKind(class, exePath, title string) string {
	if kind, ok := shellSurfaceClasses[class]; ok {
		return kind
	}
	if class == "Windows.UI.Core.CoreWindow" {
		host := strings.ToLower(filepath.Base(exePath))
		if host == "shellexperiencehost.exe" && title == notificationBannerTitle {
			return ShellNotification
		}
		return shellFlyoutHosts[host]
	}
	return ""
}

// ShellKindOf names the shell surface the window with this Window ID is,
// from its class and process alone (no enumeration): "" for any other
// window, and inside a go test binary.
func ShellKindOf(id uint64) string {
	if LiveDesktopGuard() != nil {
		return ""
	}
	hwnd := windows.HWND(id)
	class := className(hwnd)
	if class == "Windows.UI.Core.CoreWindow" {
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid == 0 {
			return ""
		}
		path, _ := exePath(pid)
		return shellKind(class, path, windowText(hwnd))
	}
	return shellKind(class, "", "")
}

// ShellSurface reports whether a collected window is a shell surface that
// `list-windows --shell` lists and a Window ID resolves to: visible, not
// cloaked, with an area. A closed Start or Action Center is cloaked or
// hidden, and the hidden tray overflow has no Elements, so none of them is
// listed until it opens.
func ShellSurface(w WindowInfo) bool {
	if w.Shell == "" || !w.Visible || w.Cloaked || w.OwnProcess {
		return false
	}
	return w.Bounds.Width() > 0 && w.Bounds.Height() > 0
}

// WithShellSurfaces filters collected windows down to the visible set
// plus the shell surfaces, in the given (z) order, topmost first:
// `list-windows --shell`.
func WithShellSurfaces(all []WindowInfo) []WindowInfo {
	out := make([]WindowInfo, 0, len(all))
	for _, w := range all {
		if KeepWindow(w) || ShellSurface(w) {
			out = append(out, w)
		}
	}
	return out
}

// ListedTitle is the title column of a Window's line: its title, or for an
// untitled shell surface (the taskbar has no caption) its kind.
func ListedTitle(w WindowInfo) string {
	if w.Title == "" && w.Shell == ShellTaskbar {
		return "Taskbar"
	}
	return w.Title
}

// The tray overflow's input windows (probes round 5, section 6): Escape
// posted to the overflow's top-level window does nothing; posted to the
// XAML island's bridge and its InputSite it closes the overflow.
var overflowInputClasses = map[string]bool{
	"Windows.UI.Composition.DesktopWindowContentBridge": true,
	"Windows.UI.Input.InputSite.WindowClass":            true,
}

var (
	procEnumChildWindows = user32.NewProc("EnumChildWindows")

	childMu     sync.Mutex
	childTarget *[]uintptr
)

// enumChildCallback appends every enumerated child HWND (enumTarget's
// pattern, for EnumChildWindows). Created once: callbacks are never freed.
var enumChildCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
	*childTarget = append(*childTarget, hwnd)
	return 1 // continue
})

// descendants lists every descendant window of hwnd.
func descendants(hwnd windows.HWND) []uintptr {
	childMu.Lock()
	defer childMu.Unlock()
	kids := make([]uintptr, 0, 8)
	childTarget = &kids
	procEnumChildWindows.Call(uintptr(hwnd), enumChildCallback, 0)
	childTarget = nil
	return kids
}

// PostOverflowEscape posts Escape (WM_KEYDOWN, then WM_KEYUP) to the tray
// overflow's island input windows, which closes it without the foreground
// and without real keys. It returns how many messages were posted: 0 means
// nothing was sent. The caller reads the close back (the overflow hides).
func PostOverflowEscape(hwnd uint64) (int, error) {
	if err := LiveDesktopGuard(); err != nil {
		return 0, err
	}
	sent := 0
	for _, child := range descendants(windows.HWND(hwnd)) {
		if !overflowInputClasses[className(windows.HWND(child))] {
			continue
		}
		n, err := postAll(uint64(child), overflowEscapeMessages())
		sent += n
		if err != nil {
			return sent, err
		}
	}
	if sent == 0 {
		return 0, fmt.Errorf("window %d has no XAML island input window to post Escape to", hwnd)
	}
	return sent, nil
}

// overflowEscapeMessages is one posted Escape press.
func overflowEscapeMessages() []postedMessage {
	scan := mapVirtualKeyScan(vkEscape)
	// lParam as Posted Keys build it: repeat 1, the scan code, the key-up
	// bits on the up.
	return []postedMessage{
		{wmKeyDown, vkEscape, keyLParam(scan, false, false)},
		{wmKeyUp, vkEscape, keyLParam(scan, false, true)},
	}
}
