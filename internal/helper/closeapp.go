package helper

// close-app (desktop-parity ticket 10, agent-desktop's close-app): every
// Window of an App is asked to close and winuse waits for the App's
// processes to exit. Headless like close-window (ADR 0011): a posted
// WM_CLOSE per Window, so winuse never activates a Window, moves the
// pointer, sends keys, raises the banner or shows the Agent Cursor. An App
// is not an Action's target (CONTEXT.md), so close-app is no Batch step.
//
// An App still running after the wait is not a failure: the close was
// requested, and the App may be asking to save. With --force (the user's
// decision, 2026-11-03), winuse then ends the App's own processes with
// TerminateProcess, as agent-desktop does. It is the one command that ends
// a process.
//
// Every refusal raised before the first WM_CLOSE says "nothing was sent":
// a locked desktop, an administrator Window, and agent-desktop's protected
// processes, which close-app refuses with or without --force.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

const (
	// closeAppWait bounds the wait for the App's processes to exit after
	// the posted closes: a few seconds, longer than close-window's, since
	// an App may save state and close several Windows.
	closeAppWait = 5 * time.Second
	// terminateWait bounds the wait for a process --force ended.
	terminateWait = 2 * time.Second

	closeAppUsage = "usage: winuse close-app <app> [--force]"
)

// protectedProcesses are agent-desktop's session-critical images
// (ad:crates/windows/src/system/app_ops.rs:7-17), by executable name
// without .exe, lower case. close-app refuses them: ending one logs the
// user off, crashes the session or takes the shell down.
var protectedProcesses = map[string]bool{
	"csrss": true, "wininit": true, "winlogon": true, "services": true, "lsass": true,
	"smss": true, "lsaiso": true, "dwm": true, "explorer": true,
}

// sharedHost is the process that hosts the frames of every packaged App
// whose own process it could not attribute: it is no one App's own, so
// --force never ends it.
const sharedHost = "applicationframehost"

// AppProcess is one process close-app waits on and, with --force, ends.
// The real one is *win.AppProcess, a process handle; tests fake it.
type AppProcess interface {
	Exited() bool
	Terminate() error
	Close() error
}

// openAppProcess is the real Services.OpenProcess.
func openAppProcess(pid uint32, terminate bool) (AppProcess, error) {
	p, err := win.OpenAppProcess(pid, terminate)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// closeApp runs `winuse close-app <app> [--force]`: the App's Windows are
// resolved by App name (every one, unlike a Window reference), the
// refusals checked, its processes opened, then WM_CLOSE posted to each
// Window and the processes waited for, up to closeAppWait. Packaged Apps
// close the same way, per Window: the frame's process is the App's own
// (win.Collect attributes it through the CoreWindow).
func closeApp(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.CloseAppArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("close-app: "+err.Error(), closeAppUsage)
		}
		ref := strings.TrimSpace(a.App)
		if ref == "" {
			return "", Fail("close-app: an App name is required", closeAppUsage)
		}
		all, err := s.Windows()
		if err != nil {
			return "", Fail("enumerating windows: "+err.Error(), "")
		}
		ws := win.WindowsOfApp(all, ref)
		if len(ws) == 0 {
			return "", nothingSent(Fail(fmt.Sprintf("app %q has no open Windows", ref),
				"name the App as winuse list-windows prints it, or by its executable name"))
		}
		name := ws[0].App
		if name == "" {
			name = ref
		}
		if locked, desktop := s.Locked(); locked {
			return "", nothingSent(lockedDesktopFailure(desktop))
		}
		for _, w := range ws {
			if rerr := s.closeAppRefusal(w, a.Force); rerr != nil {
				return "", rerr
			}
		}
		procs, rerr := s.openAppProcesses(ws, a.Force)
		if rerr != nil {
			return "", rerr
		}
		defer func() {
			for _, p := range procs {
				p.Close()
			}
		}()

		defer armPassive(s.Esc)()
		var (
			posted   int
			firstErr error
		)
		for _, w := range ws {
			if err := s.CloseWindow(w.ID); err != nil {
				// A dialog closes with its owner: only a Window still open failed.
				if firstErr == nil && s.WindowOpen(w.ID) {
					firstErr = err
				}
				continue
			}
			posted++
		}
		if posted == 0 && firstErr != nil {
			return "", windowCallError(firstErr)
		}
		exited := s.pollWindow(ctx, closeAppWait, func() bool { return allExited(procs) && !s.anyOpen(ws) })
		if s.Esc.Stopped() {
			return "", escStopError()
		}
		if exited {
			return okOutput, nil
		}
		if !a.Force {
			return fmt.Sprintf("ok (the close was requested; %s is still running, it may be asking to save: read winuse list-windows --app %s or the Window State next)\n",
				name, quoteArg(ref)), nil
		}
		return s.forceEnd(ctx, name, procs)
	}
}

// closeAppRefusal refuses one of the App's Windows before anything is
// sent: an administrator Window (UIPI blocks the posted close, and the
// helper may not end the process), a protected process, and under --force
// a process whose image winuse cannot name or that hosts other Apps.
func (s Services) closeAppRefusal(w win.WindowInfo, force bool) *Error {
	if err := s.Integrity(w); err != nil {
		var admin *win.AdminWindowError
		if errors.As(err, &admin) {
			return nothingSent(Fail(err.Error(),
				"Windows blocks closing a higher-integrity App; start winuse from an elevated terminal to control administrator Windows, or ask the user to close it"))
		}
		return nothingSent(Fail(err.Error(), ""))
	}
	image := processImage(w.ExePath)
	if protectedProcesses[image] {
		return nothingSent(Fail("close-app refuses "+image+".exe: it is a protected Windows process",
			"close one of its Windows with winuse close-window "+strconv.FormatUint(w.ID, 10)+" instead"))
	}
	if !force {
		return nil
	}
	id := strconv.FormatUint(w.ID, 10)
	if image == "" {
		return nothingSent(Fail("close-app --force cannot read which program owns window "+id+", so it ends nothing",
			"run close-app without --force, or close the Window with winuse close-window "+id))
	}
	if image == sharedHost {
		return nothingSent(Fail("close-app --force refuses ApplicationFrameHost.exe: it hosts the Windows of every packaged App",
			"run close-app without --force, or close the Window with winuse close-window "+id))
	}
	return nil
}

// anyOpen reports whether any of the Windows is still open.
func (s Services) anyOpen(ws []win.WindowInfo) bool {
	for _, w := range ws {
		if s.WindowOpen(w.ID) {
			return true
		}
	}
	return false
}

// openAppProcesses opens each distinct process of the App's Windows
// before the first close, so the wait follows those processes and not a
// later one that reuses an id. A process that already exited is skipped.
func (s Services) openAppProcesses(ws []win.WindowInfo, force bool) ([]AppProcess, *Error) {
	var procs []AppProcess
	seen := map[uint32]bool{}
	for _, w := range ws {
		if w.PID == 0 || seen[w.PID] {
			continue
		}
		seen[w.PID] = true
		p, err := s.OpenProcess(w.PID, force)
		if errors.Is(err, win.ErrProcessGone) {
			continue
		}
		if err != nil {
			for _, open := range procs {
				open.Close()
			}
			return nil, nothingSent(Fail(err.Error(), "run winuse list-windows; the App may have closed"))
		}
		procs = append(procs, p)
	}
	return procs, nil
}

// forceEnd is --force after the wait: TerminateProcess on every process
// of the App still running, then a short wait for them to go.
func (s Services) forceEnd(ctx context.Context, name string, procs []AppProcess) (string, *Error) {
	ended := 0
	for _, p := range procs {
		if p.Exited() {
			continue
		}
		if err := p.Terminate(); err != nil {
			return "", Fail(err.Error(), fmt.Sprintf("the close was delivered and %s is still running; read winuse list-windows", name))
		}
		ended++
	}
	if !s.pollWindow(ctx, terminateWait, func() bool { return allExited(procs) }) {
		return "", Fail(fmt.Sprintf("%s is still running after --force ended it", name),
			"Windows may still be ending it; read winuse list-windows")
	}
	noun := "process"
	if ended != 1 {
		noun = "processes"
	}
	return fmt.Sprintf("ok (%s was still running after the close; --force ended %d %s)\n", name, ended, noun), nil
}

// allExited reports whether every process has ended.
func allExited(procs []AppProcess) bool {
	for _, p := range procs {
		if !p.Exited() {
			return false
		}
	}
	return true
}

// processImage is an executable path's base name without .exe, lower
// case ("" for an unknown path): the protected list's key.
func processImage(exePath string) string {
	if exePath == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(exePath))
	return strings.TrimSuffix(base, ".exe")
}

// quoteArg quotes a command-line argument that holds a space, for a
// command the output tells the agent to run.
func quoteArg(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}
