package apps

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/win"

	"golang.org/x/sys/windows"
)

// StartApp starts the App described by spec and returns the new process
// id (0 when the shell handed the start to a broker or an already running
// instance). It never waits for the App: recognizing the Window the App
// opens is WaitForWindow's job. Real Win32 process creation.
func StartApp(spec Spec) (uint32, error) {
	switch spec.Kind {
	case IDExe:
		return startExe(spec.ID)
	case IDShortcut:
		return shellExec(spec.ID, filepath.Dir(spec.ID))
	case IDAppUserModelID:
		return shellExec("shell:AppsFolder\\"+spec.ID, "")
	default:
		return 0, fmt.Errorf("unknown app kind %d", spec.Kind)
	}
}

// Process creation flags: a new process group so the App does not share
// the helper's Ctrl-C group, and breakaway from job objects so the App
// survives a harness job being closed (retried without breakaway when the
// job forbids it). Unlike the helper's own spawn, no DETACHED_PROCESS and
// no hidden window: a launched App is supposed to appear.
const (
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
)

// startExe starts an executable directly.
func startExe(path string) (uint32, error) {
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	var lastErr error
	for _, flags := range []uint32{createNewProcessGroup | createBreakawayFromJob, createNewProcessGroup} {
		cmd := exec.Command(path)
		cmd.Dir = filepath.Dir(path)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			lastErr = err
			continue
		}
		pid := uint32(cmd.Process.Pid)
		_ = cmd.Process.Release()
		return pid, nil
	}
	return 0, lastErr
}

// SHELLEXECUTEINFOW, hand-written (no cgo); 112 bytes on 64-bit.
type shellExecuteInfoW struct {
	cbSize, fMask  uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	_              uint32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	_              uint32
	hIconOrMonitor uintptr
	hProcess       uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
	swShowNormal          = 1
)

var (
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

// shellExec opens file through the shell: resolves App Paths, PATH, .lnk
// files, URLs and shell: names — the way the Start menu launches Apps.
// ShellExecuteExW is documented as possibly needing COM, so it runs on a
// fresh locked OS thread with apartment-threaded COM initialised around it
// (Go goroutines migrate between OS threads; COM initialization is per
// thread).
func shellExec(file, dir string) (pid uint32, err error) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
			defer windows.CoUninitialize()
		}
		pid, err = shellExecute(file, dir)
	}()
	<-done
	return pid, err
}

// shellExecute performs one ShellExecuteExW call. hProcess may stay 0
// (DDE, broker or Store activation) — the pid is then unknown.
func shellExecute(file, dir string) (uint32, error) {
	var info shellExecuteInfoW
	info.cbSize = uint32(unsafe.Sizeof(info))
	info.fMask = seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI
	info.lpFile = utf16Ptr(file)
	if dir != "" {
		info.lpDirectory = utf16Ptr(dir)
	}
	info.nShow = swShowNormal
	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, callErr
	}
	defer windows.CloseHandle(windows.Handle(info.hProcess)) // no-op for 0
	if info.hProcess == 0 {
		return 0, nil
	}
	pid, err := windows.GetProcessId(windows.Handle(info.hProcess))
	if err != nil {
		return 0, nil
	}
	return pid, nil
}

func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

// NoWindowError reports an App that started but exposed no targetable
// Window within the launch timeout (Codex's message).
type NoWindowError struct {
	App string
}

func (e *NoWindowError) Error() string {
	return "launched app did not expose a targetable window: " + e.App
}

// ErrStopped reports a Window wait aborted by the user's Esc press
// (ticket 12): the App that was started keeps running; the wait — and
// with it the launch-app command — stops. Wrapped by WaitForWindow so
// callers distinguish it from a timeout through errors.Is.
var ErrStopped = errors.New("the wait for the launched app's window was stopped")

// pollInterval is how often the launched App's Window is looked for.
const pollInterval = 250 * time.Millisecond

// WaitForWindow polls the visible Windows until the launched App exposes
// one, or budget passes (the caller keeps it just under the request
// timeout so this specific error, not the generic one, is reported).
// enum enumerates the visible Windows (z-order); fg returns the
// foreground Window's ID; sleep waits between polls (tests stub it);
// stopped reports whether the user pressed Esc (nil: never).
//
// A Window counts as the one the launch opened when it is new since the
// before snapshot and belongs to the App's process (exe path, or the
// started pid); a single-instance App that instead reuses an existing
// Window is recognized by that Window coming to the foreground. Without a
// known target exe any new visible Window counts.
func WaitForWindow(ctx context.Context, spec Spec, pid uint32, before []win.WindowInfo, fgBefore uint64, budget time.Duration,
	enum func() ([]win.WindowInfo, error), fg func() uint64, sleep func(time.Duration), stopped func() bool) (win.WindowInfo, error) {
	deadline := time.Now().Add(budget)
	for {
		// The Esc stop outranks both the timeout and a lucky late match:
		// the user's signal is what the caller must act on (exit 130).
		if stopped != nil && stopped() {
			return win.WindowInfo{}, fmt.Errorf("%w: %s", ErrStopped, spec.App)
		}
		if err := ctx.Err(); err != nil {
			return win.WindowInfo{}, &NoWindowError{App: spec.App}
		}
		after, err := enum()
		if err == nil {
			if w, ok := MatchNewWindow(before, after, fg(), fgBefore, spec, pid); ok {
				return w, nil
			}
		}
		if !time.Now().Before(deadline) {
			return win.WindowInfo{}, &NoWindowError{App: spec.App}
		}
		if sleep != nil {
			sleep(pollInterval)
		}
	}
}

// MatchNewWindow picks the Window a launch opened (pure): a new visible
// Window of the App's process, else the Window the App brought to the
// foreground (single-instance Apps), else any new visible Window when the
// App's exe is unknown. after is z-ordered, topmost first.
func MatchNewWindow(before, after []win.WindowInfo, fgID, fgBefore uint64, spec Spec, pid uint32) (win.WindowInfo, bool) {
	seen := make(map[uint64]bool, len(before))
	for _, w := range before {
		seen[w.ID] = true
	}
	targetExe := strings.ToLower(spec.TargetExe)
	for _, w := range after {
		if seen[w.ID] {
			continue
		}
		if targetExe == "" {
			return w, true // unknown exe: any new Window is the one it opened
		}
		if belongsToApp(w, targetExe, pid) {
			return w, true
		}
	}
	if targetExe == "" || fgID == 0 || fgID == fgBefore {
		return win.WindowInfo{}, false
	}
	for _, w := range after {
		if w.ID == fgID && belongsToApp(w, targetExe, pid) {
			return w, true // the App came to the foreground without a new Window
		}
	}
	return win.WindowInfo{}, false
}

// belongsToApp reports whether a Window belongs to the launched App's
// process: matching exe path (case-insensitive) or the started pid (for
// processes whose path cannot be queried).
func belongsToApp(w win.WindowInfo, targetExe string, pid uint32) bool {
	if targetExe != "" && strings.ToLower(w.ExePath) == targetExe {
		return true
	}
	return pid != 0 && w.PID == pid
}

// RecentApps turns UserAssist records into catalog entries (pure): names
// come from nameFor (the App name of the executable) or the shortcut file
// name, shortcut targets from lnkTarget.
func RecentApps(recs []RecentRecord, nameFor func(exe string) string, lnkTarget func(lnk string) string) []App {
	var out []App
	for _, rec := range recs {
		switch {
		case rec.AUMID != "":
			out = append(out, App{ID: rec.AUMID, Kind: IDAppUserModelID, LastUsed: rec.LastUsed, UseCount: rec.UseCount})
		case hasSuffixFold(rec.Path, ".lnk"):
			out = append(out, App{
				Name:      trimExt(filepath.Base(rec.Path)),
				ID:        rec.Path,
				Kind:      IDShortcut,
				TargetExe: lnkTarget(rec.Path),
				LastUsed:  rec.LastUsed,
				UseCount:  rec.UseCount,
			})
		default:
			out = append(out, App{Name: nameFor(rec.Path), ID: rec.Path, Kind: IDExe, LastUsed: rec.LastUsed, UseCount: rec.UseCount})
		}
	}
	return out
}
