package apps

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/win"

	"golang.org/x/sys/windows"
)

// StartApp starts the App described by spec and returns the new process
// id (0 when the shell handed the start to a broker or an already running
// instance). It never waits for the App: recognizing the Window the App
// opens is WaitForWindow's job. Real Win32 process creation, always with a
// non-activating show state (headless-max ticket 01): an executable through
// win.StartProcess, a shortcut or packaged App through ShellExecuteEx with
// SW_SHOWNOACTIVATE (SW_SHOWMINNOACTIVE when minimized). A packaged App's
// activation has no such option and takes the foreground anyway (measured
// on Calculator, .scratch/headless-gaps/probes-round5.md section 5).
//
// Arguments, a working directory and environment overrides need an
// executable (ExeOnlyOptions): the caller refuses them for the other kinds
// before anything starts, and StartApp refuses them again.
func StartApp(spec Spec, opts win.LaunchOptions) (uint32, error) {
	if spec.Kind != IDExe && ExeOnlyOptions(opts) {
		return 0, errors.New("--arg, --cwd and --env need an executable")
	}
	switch spec.Kind {
	case IDExe:
		return win.StartProcess(spec.ID, opts)
	case IDShortcut:
		return shellExec(spec.ID, filepath.Dir(spec.ID), opts.Minimized)
	case IDAppUserModelID:
		return shellExec("shell:AppsFolder\\"+spec.ID, "", opts.Minimized)
	default:
		return 0, fmt.Errorf("unknown app kind %d", spec.Kind)
	}
}

// ExeOnlyOptions reports whether opts carry anything only an executable
// launch can apply: arguments, a working directory or environment entries.
// The shell starts a shortcut with the shortcut's own command line and
// directory, and a packaged App through its activation, so neither has a
// place for them.
func ExeOnlyOptions(opts win.LaunchOptions) bool {
	return len(opts.Args) > 0 || opts.Dir != "" || len(opts.Env) > 0
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
func shellExec(file, dir string, minimized bool) (pid uint32, err error) {
	if err := win.LiveDesktopGuard(); err != nil {
		return 0, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
			defer windows.CoUninitialize()
		}
		pid, err = shellExecute(file, dir, minimized)
	}()
	<-done
	return pid, err
}

// shellInfo builds the SHELLEXECUTEINFOW for one launch: no UI on failure,
// the process handle kept for its pid, and a non-activating show state.
func shellInfo(file, dir string, minimized bool) shellExecuteInfoW {
	var info shellExecuteInfoW
	info.cbSize = uint32(unsafe.Sizeof(info))
	info.fMask = seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI
	info.lpFile = utf16Ptr(file)
	if dir != "" {
		info.lpDirectory = utf16Ptr(dir)
	}
	info.nShow = int32(win.LaunchShowState(minimized))
	return info
}

// shellExecute performs one ShellExecuteExW call. hProcess may stay 0
// (DDE, broker or Store activation) — the pid is then unknown.
func shellExecute(file, dir string, minimized bool) (uint32, error) {
	info := shellInfo(file, dir, minimized)
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
