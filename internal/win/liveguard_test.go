package win

import (
	"errors"
	"testing"
)

// TestLiveDesktopEntryPointsRefuseInTests pins the guard on every entry
// point that touches the live desktop: none of them may act from the
// default (offline) test command.
func TestLiveDesktopEntryPointsRefuseInTests(t *testing.T) {
	t.Setenv("WINUSE_E2E", "")
	checks := map[string]error{
		"sendInput":   sendInput(input{}),
		"moveCursor":  moveCursor(1, 1),
		"Activate":    Activate(1),
		"region":      func() error { _, err := CaptureScreenRegion(Rect{Right: 1, Bottom: 1}); return err }(),
		"printWindow": func() error { _, _, err := CapturePrintWindow(1); return err }(),
	}
	for name, err := range checks {
		if !errors.Is(err, ErrLiveDesktopInTest) {
			t.Errorf("%s: err = %v, want ErrLiveDesktopInTest", name, err)
		}
	}
}
