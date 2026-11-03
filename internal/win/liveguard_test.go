package win

import (
	"errors"
	"testing"

	"github.com/hoangvu12/winuse/internal/keys"
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
		// The Window Actions (ADR 0011): hwnd 0 is no Window, and the guard
		// refuses before any call is made.
		"RestoreNoActivate":  RestoreNoActivate(0),
		"MinimizeNoActivate": MinimizeNoActivate(0),
		"MoveNoActivate":     MoveNoActivate(0, 1, 1),
		"ResizeNoActivate":   ResizeNoActivate(0, 1, 1),
		"PostClose":          PostClose(0),
		// The keyboard routes (ADR 0011, Decision 4).
		"PostDialogCommand": PostDialogCommand(0, IDCancel, 0),
		"ReplaceSelection":  ReplaceSelection(0, "x"),
		"SetEditSelection":  SetEditSelection(0, 0, 1),
		// invoke-menu (headless-max ticket 08).
		"InitMenuPopup":   InitMenuPopup(0, 0, 0),
		"PostMenuCommand": PostMenuCommand(0, 1),
		// Posted Keys (ADR 0011, Decision 5).
		"PostKeys": func() error { _, err := PostKeys(0, keys.Chord{{Name: "Return", VK: 0x0D}}); return err }(),
		"PostText": func() error { _, err := PostText(0, "x"); return err }(),
	}
	for name, err := range checks {
		if !errors.Is(err, ErrLiveDesktopInTest) {
			t.Errorf("%s: err = %v, want ErrLiveDesktopInTest", name, err)
		}
	}
}
