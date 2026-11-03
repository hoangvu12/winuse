package win

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Rect is a window rectangle in physical pixels (int32 quadruple,
// layout-identical to the Win32 RECT: under per-monitor-v2 awareness all
// coordinates are physical pixels).
type Rect struct {
	Left, Top, Right, Bottom int32
}

// Width returns the horizontal extent (may be negative for empty rects).
func (r Rect) Width() int32 { return r.Right - r.Left }

// Height returns the vertical extent (may be negative for empty rects).
func (r Rect) Height() int32 { return r.Bottom - r.Top }

// WindowInfo is one top-level window with everything the visibility filter
// needs. Collect fills it from Win32; filtering and formatting are pure
// functions on this struct so they are testable offline.
type WindowInfo struct {
	ID      uint64 // HWND as an unsigned decimal — the Window ID
	Title   string // window caption (GetWindowTextW, or InternalGetWindowText for this process's windows: never blocks)
	Class   string // window class name
	PID     uint32 // owning process (the real app process for UWP windows)
	ExePath string // full path of the owning process, "" if unresolvable
	App     string // App display name (FileDescription or exe base name)
	Owner   uint64 // owning window ID (dialogs own back to their main window), 0 if unowned

	Style   uint32
	ExStyle uint32

	Bounds    Rect // DWM extended frame bounds: visible extent without the shadow
	Visible   bool // IsWindowVisible
	Cloaked   bool // DWM cloaked (suspended UWP, other virtual desktop, …)
	Minimized bool // IsIconic

	ShellWindow bool   // desktop / taskbar surface
	Shell       string // the shell surface `list-windows --shell` lists (ShellTaskbar, ...; shell.go), "" for any other window
	OwnProcess  bool   // a window of a winuse process (the overlay; fixtures stay listed)
}

// WindowStyle and extended-style flags used by the filter.
const (
	wsExToolWindow = 0x00000080
	wsExAppWindow  = 0x00040000
	wsExNoActivate = 0x08000000
)

// Window-long offsets and window relationships.
const (
	gwlStyle   = -16
	gwlExStyle = -20
	gwOwner    = 4
)

// shellClasses are window classes of shell surfaces that are "visible" but
// are not Windows a person works in. list-windows drops them;
// `list-windows --shell` lists the taskbars again, with the other shell
// surfaces (shell.go, ADR 0015).
var shellClasses = map[string]bool{
	"Progman":                true, // desktop
	"WorkerW":                true, // desktop behind wallpaper
	"Shell_TrayWnd":          true, // taskbar
	"Shell_SecondaryTrayWnd": true, // taskbar on other monitors
}

// uwpFrameClass is the top-level class of UWP app hosts; their real app
// process is found through the child CoreWindow.
const uwpFrameClass = "ApplicationFrameWindow"

// coreWindowClass is the UWP content window class.
var coreWindowClass, _ = windows.UTF16PtrFromString("Windows.UI.Core.CoreWindow")

var (
	procGetWindowLongPtrW     = user32.NewProc("GetWindowLongPtrW")
	procGetWindow             = user32.NewProc("GetWindow")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procInternalGetWindowText = user32.NewProc("InternalGetWindowText")
	procGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	procIsIconic              = user32.NewProc("IsIconic")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procFindWindowExW         = user32.NewProc("FindWindowExW")
)

// enumTarget is the slice the enumeration callback appends to. A package
// variable instead of the classic lparam unsafe.Pointer dance (vet-clean),
// so enumMu is held for the whole EnumWindows call: concurrent
// enumerations (requests in different lanes, multi-agent 06) take turns.
// Holding it only around the target's set and clear let one call's
// callback append through a target another call had cleared: it panicked
// holding the lock and every later Collect blocked (e2e live run,
// 2026-11-03).
var (
	enumMu     sync.Mutex
	enumTarget *[]uintptr
)

// enumWindowsCallback appends every enumerated HWND. Created once:
// callbacks are never freed.
// The callback runs inside enumAll's EnumWindows call, under enumMu.
var enumWindowsCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
	*enumTarget = append(*enumTarget, hwnd)
	return 1 // continue
})

// Collect enumerates top-level windows in z-order (topmost first) together
// with the data the visibility filter needs. Windows that are not
// IsWindowVisible are skipped early as a cheap prefilter; the full predicate
// lives in the pure VisibleWindows filter. UWP windows are attributed to
// their real app process.
//
// Collect reads the live desktop (titles, class names, process paths); tests
// never call it. It performs no input and activates nothing.
func Collect() ([]WindowInfo, error) {
	hwnds, err := enumAll()
	if err != nil {
		return nil, fmt.Errorf("EnumWindows: %w", err)
	}
	// The shell's own CoreWindows (Start, the Action Center, the toast
	// banner) are immersive: EnumWindows leaves them out (shell.go).
	hwnds = withShellCoreWindows(hwnds, topLevelCoreWindows(), isShellFlyoutHost)
	shell := windows.GetShellWindow()
	selfExe, _ := os.Executable()
	apps := newAppNames()

	out := make([]WindowInfo, 0, len(hwnds))
	for _, h := range hwnds {
		hwnd := windows.HWND(h)
		if !windows.IsWindowVisible(hwnd) {
			continue
		}
		out = append(out, collectOne(hwnd, shell, selfExe, apps))
	}
	return out, nil
}

func enumAll() ([]uintptr, error) {
	enumMu.Lock()
	defer enumMu.Unlock()
	hwnds := make([]uintptr, 0, 256)
	enumTarget = &hwnds
	defer func() { enumTarget = nil }()
	if err := windows.EnumWindows(enumWindowsCallback, nil); err != nil {
		return nil, err
	}
	return hwnds, nil
}

func collectOne(hwnd windows.HWND, shell windows.HWND, selfExe string, apps *appNames) WindowInfo {
	w := WindowInfo{
		ID:      uint64(hwnd),
		Visible: true,
	}

	// Cloaked (DWMWA_CLOAKED): suspended UWP apps, windows on other virtual
	// desktops, hidden shell surfaces. A missing DWM answer counts as not
	// cloaked.
	var cloaked uint32
	if err := windows.DwmGetWindowAttribute(hwnd, windows.DWMWA_CLOAKED, unsafe.Pointer(&cloaked), uint32(unsafe.Sizeof(cloaked))); err == nil {
		w.Cloaked = cloaked != 0
	}

	w.Style = windowLong(hwnd, gwlStyle)
	w.ExStyle = windowLong(hwnd, gwlExStyle)
	w.Class = className(hwnd)
	w.Minimized = isIconic(hwnd)
	w.Bounds = windowBounds(hwnd)
	w.Owner = ownerOf(hwnd)
	w.ShellWindow = hwnd == shell || shellClasses[w.Class]

	// Process attribution. For UWP frames, the host window belongs to
	// ApplicationFrameHost.exe; the child CoreWindow belongs to the real app.
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err == nil && pid != 0 {
		if w.Class == uwpFrameClass {
			if real := uwpContentPID(hwnd); real != 0 {
				pid = real
			}
		}
		w.PID = pid
		w.ExePath, _ = exePath(pid)
		w.App = apps.Name(w.ExePath)
	}
	// The caption is read once the process is known: a window of this
	// process takes the no-message read (windowTitle).
	w.Title = windowTitle(hwnd, w.PID)
	w.Shell = shellKind(w.Class, w.ExePath, w.Title)
	w.OwnProcess = selfExe != "" && w.ExePath == selfExe
	return w
}

// ForegroundWindow returns the ID of the Window currently in the
// foreground (0 when there is none). Read-only: no activation, no input.
func ForegroundWindow() uint64 {
	return uint64(windows.GetForegroundWindow())
}

// WindowPID returns the process id that owns the Window with the given
// Window ID (0 when it cannot be read).
func WindowPID(id uint64) uint32 {
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(id), &pid); err != nil {
		return 0
	}
	return pid
}

// uwpContentPID returns the pid of the CoreWindow child of a UWP app frame,
// or 0 for non-frame (or frame without content) windows.
func uwpContentPID(hwnd windows.HWND) uint32 {
	if className(hwnd) != uwpFrameClass {
		return 0
	}
	child, _, _ := procFindWindowExW.Call(uintptr(hwnd), 0, uintptr(unsafe.Pointer(coreWindowClass)), 0)
	if child == 0 {
		return 0
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(child), &pid); err != nil {
		return 0
	}
	return pid
}

func windowLong(hwnd windows.HWND, index int32) uint32 {
	r, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index))
	return uint32(r)
}

func ownerOf(hwnd windows.HWND) uint64 {
	r, _, _ := procGetWindow.Call(uintptr(hwnd), gwOwner)
	return uint64(r)
}

func isIconic(hwnd windows.HWND) bool {
	r, _, _ := procIsIconic.Call(uintptr(hwnd))
	return r != 0
}

// windowTitle reads a window's caption without sending it a message.
// GetWindowTextW reads the cached caption of another process's window, but
// for a window of this process it sends WM_GETTEXT and waits for the
// owning thread. The helper's own overlay and Agent Cursor windows live on
// its UI thread, so a Window list read (a desktop wait polls one every
// 100 ms) blocked while that thread was busy, and with it the request.
// InternalGetWindowText reads the cached caption and sends nothing; these
// windows are filtered out of the listings anyway (OwnProcess).
func windowTitle(hwnd windows.HWND, pid uint32) string {
	if pid != 0 && pid == windows.GetCurrentProcessId() {
		return internalWindowText(hwnd)
	}
	return windowText(hwnd)
}

// internalWindowText reads a window's cached caption with
// InternalGetWindowText, which never sends WM_GETTEXT (titles past 511
// characters are cut).
func internalWindowText(hwnd windows.HWND) string {
	buf := make([]uint16, 512)
	r, _, _ := procInternalGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:r])
}

// windowText reads a window's caption. GetWindowTextW reads the cached
// caption for windows of other processes (no WM_GETTEXT is sent), so a hung
// app cannot block enumeration; a window of this process gets the message
// (windowTitle avoids that).
func windowText(hwnd windows.HWND) string {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	r, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:r])
}

func className(hwnd windows.HWND) string {
	buf := make([]uint16, 256)
	n, err := windows.GetClassName(hwnd, &buf[0], int32(len(buf)))
	if err != nil || n <= 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// windowBounds returns the DWM extended frame bounds (the window's visible
// extent without the drop shadow) in physical pixels, falling back to
// GetWindowRect when DWM does not answer (e.g. no composition).
func windowBounds(hwnd windows.HWND) Rect {
	var r Rect
	if err := windows.DwmGetWindowAttribute(hwnd, windows.DWMWA_EXTENDED_FRAME_BOUNDS, unsafe.Pointer(&r), uint32(unsafe.Sizeof(r))); err == nil {
		return r
	}
	var wr Rect
	if r1, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&wr))); r1 == 0 {
		return Rect{}
	}
	return wr
}
