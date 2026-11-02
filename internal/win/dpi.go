package win

import (
	"errors"

	"golang.org/x/sys/windows"
)

// dpiAwarenessContextPerMonitorAwareV2 is DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
// ((HANDLE)-4). Under it, GetWindowRect, GetCursorPos/SetCursorPos, UI
// Automation bounds and screen DCs all work in physical pixels, which is what
// keeps screenshot and click coordinates identical at any display scaling
// (Codex's 175%-offset bug).
var (
	procSetProcessDpiAwarenessContext    = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiAwarenessContextForProcess = user32.NewProc("GetDpiAwarenessContextForProcess")
	procGetThreadDpiAwarenessContext     = user32.NewProc("GetThreadDpiAwarenessContext")
	procAreDpiAwarenessContextsEqual     = user32.NewProc("AreDpiAwarenessContextsEqual")
	dpiPerMonitorV2Context               = ^uintptr(3) // (HANDLE)-4
)

// dpiAwarenessNames are the DPI_AWARENESS_CONTEXT pseudo-handles winuse
// status names, the one winuse sets first.
var dpiAwarenessNames = []struct {
	context uintptr
	name    string
}{
	{dpiPerMonitorV2Context, "per-monitor v2"},
	{^uintptr(2), "per-monitor"},          // (HANDLE)-3
	{^uintptr(1), "system"},               // (HANDLE)-2
	{^uintptr(0), "unaware"},              // (HANDLE)-1
	{^uintptr(4), "unaware (GDI scaled)"}, // (HANDLE)-5
}

// DPIAwareness names the calling process's DPI awareness for winuse
// status: "per-monitor v2" when winuse's own setting holds. It reads the
// awareness only (GetDpiAwarenessContextForProcess, or the thread's on
// Windows before 1803) and returns "unknown" when it cannot.
func DPIAwareness() string {
	if procAreDpiAwarenessContextsEqual.Find() != nil {
		return "unknown"
	}
	var ctx uintptr
	if procGetDpiAwarenessContextForProcess.Find() == nil {
		ctx, _, _ = procGetDpiAwarenessContextForProcess.Call(0)
	} else if procGetThreadDpiAwarenessContext.Find() == nil {
		ctx, _, _ = procGetThreadDpiAwarenessContext.Call()
	}
	if ctx == 0 {
		return "unknown"
	}
	for _, a := range dpiAwarenessNames {
		if eq, _, _ := procAreDpiAwarenessContextsEqual.Call(ctx, a.context); eq != 0 {
			return a.name
		}
	}
	return "unknown"
}

// SetPerMonitorV2DPIAwareness marks the process per-monitor DPI aware (v2).
// It must be called before any windowing API.
//
// The manifest embedded in the winuse binary (cmd/winuse/rsrc_*.syso) already
// declares this awareness; the runtime call exists so processes running the
// same code without the linked binary (go run, tests) behave identically.
// Failing because awareness is already set by the manifest is success, and so
// is a missing API (pre-1703 Windows, where the manifest still applies).
func SetPerMonitorV2DPIAwareness() error {
	if err := procSetProcessDpiAwarenessContext.Find(); err != nil {
		return nil // API missing: manifest still applies
	}
	r, _, err := procSetProcessDpiAwarenessContext.Call(dpiPerMonitorV2Context)
	if r != 0 {
		return nil
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		// Awareness already set (manifest) or fixed by the OS: fine.
		return nil
	}
	return err
}
