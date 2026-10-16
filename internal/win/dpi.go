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
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	dpiPerMonitorV2Context            = ^uintptr(3) // (HANDLE)-4
)

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
