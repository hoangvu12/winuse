//go:build e2e

// The ticket-13 seam-1 tests: the in-control overlay against the fixture
// app, on the live desktop. User-run only (see the package comment and
// test/e2e/README.md for the opt-in rules); an agent must never start
// them.
//
// One long type-text Action provides the observation window. While it
// runs, the tests:
//
//   - read the desktop's window list (win.Collect — read-only) and find
//     the overlay window: raised exactly over the fixture Window, with
//     the layered/click-through/topmost/tool-window/no-activate styles;
//   - check the fixture stays the foreground window (the overlay never
//     takes focus) and that a click aimed through the pill banner — the
//     overlay's most opaque pixels — still lands on the fixture Window
//     (MouseClick's occlusion check fails loudly if a window eats the
//     point), so input delivery is unaffected;
//   - take a winuse screenshot concurrently and scan it for the overlay's
//     accent colour: it must be absent (the capture runs with the overlay
//     hidden; see internal/helper's capture bracket);
//   - after the Action, the overlay is gone and the fixture's text field
//     holds exactly the typed text — the input was delivered under it.

package e2e

import (
	"image"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// actionResult is the outcome of one background winuse command.
type actionResult struct {
	stdout, stderr string
	exit           int
}

// runActionAsync runs `winuse <args>` in the background — a real CLI
// invocation like runWinuse, but one whose result arrives on a channel so
// the test can observe the desktop while the Action runs. It must be
// started from the test goroutine (only exec errors fail the test here;
// the command's own outcome is asserted from the channel, on the test
// goroutine).
func runActionAsync(t *testing.T, args []string, extraEnv ...string) <-chan actionResult {
	t.Helper()
	cmd := exec.Command(winuseBinary(t), args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting winuse %v: %v", args, err)
	}
	done := make(chan actionResult, 1)
	go func() {
		err := cmd.Wait()
		exit := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			exit = exitErr.ExitCode()
		} else if err != nil {
			stderr.WriteString(err.Error())
			exit = -1
		}
		done <- actionResult{stdout: stdout.String(), stderr: stderr.String(), exit: exit}
	}()
	return done
}

// overlayWindow returns the currently shown overlay window, if any, from
// the desktop's window list (read-only: no input, no activation). The
// overlay window exists but stays hidden between Actions, so "collected"
// means "shown".
func overlayWindow(t *testing.T) (win.WindowInfo, bool) {
	t.Helper()
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	for _, w := range all {
		if w.Class == win.OverlayClassName {
			return w, true
		}
	}
	return win.WindowInfo{}, false
}

// waitOverlay polls the desktop until the overlay is shown or the timeout
// passes.
func waitOverlay(t *testing.T, timeout time.Duration) win.WindowInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if w, ok := overlayWindow(t); ok {
			return w
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the in-control overlay did not appear within %s while an Action ran", timeout)
	return win.WindowInfo{}
}

// waitOverlayGone polls the desktop until the overlay is hidden again.
func waitOverlayGone(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, ok := overlayWindow(t); !ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the in-control overlay is still shown after the Action ended")
}

// The overlay's Win32 extended styles, restated here so the test asserts
// the observable desktop state directly (the documented click-through,
// no-focus, no-Alt-Tab contract of the indicator).
const (
	exLayered     = 0x00080000 // WS_EX_LAYERED: colour-keyed transparency
	exTransparent = 0x00000020 // WS_EX_TRANSPARENT: click-through, even over opaque pixels
	exTopmost     = 0x00000008 // WS_EX_TOPMOST: stays above the controlled Window
	exToolWindow  = 0x00000080 // WS_EX_TOOLWINDOW: no taskbar, no Alt-Tab
	exNoActivate  = 0x08000000 // WS_EX_NOACTIVATE: never takes focus
)

func TestOverlayDuringAction(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// The long Action: a text long enough that typing runs for several
	// seconds (16 runes per batch, a pause between batches, and the fixture
	// rewrites its state file per rune), typed into the text field the
	// fixture starts with focus in. It must still finish well inside the
	// 10 s request budget.
	const words = 250 // 4500 runes
	text := strings.Repeat("winuse in control ", words)
	bg := runActionAsync(t, []string{"type-text", window, text}, "WINUSE_HELPER_IDLE="+e2eIdle)

	// The overlay appears, raised over the fixture Window.
	ov := waitOverlay(t, 6*time.Second)
	fb, err := win.FrameBounds(mustWindowID(t, window))
	if err != nil {
		t.Fatalf("reading the fixture window's frame bounds: %v", err)
	}
	if d := rectDistance(ov.Bounds, fb); d > 2 {
		t.Errorf("the overlay is at %+v, want the fixture Window's bounds %+v (edges %d px apart)", ov.Bounds, fb, d)
	}

	// The styles a click-through, never-focused indicator must carry.
	missing := []string{}
	if ov.ExStyle&exLayered == 0 {
		missing = append(missing, "layered")
	}
	if ov.ExStyle&exTransparent == 0 {
		missing = append(missing, "click-through (WS_EX_TRANSPARENT)")
	}
	if ov.ExStyle&exTopmost == 0 {
		missing = append(missing, "topmost")
	}
	if ov.ExStyle&exToolWindow == 0 {
		missing = append(missing, "tool-window (no Alt-Tab, no taskbar)")
	}
	if ov.ExStyle&exNoActivate == 0 {
		missing = append(missing, "no-activate")
	}
	if len(missing) > 0 {
		t.Errorf("the overlay's extended style 0x%x lacks: %s", ov.ExStyle, strings.Join(missing, ", "))
	}

	// It never takes focus: the fixture stays the foreground window.
	if fg := win.ForegroundWindow(); fg != mustWindowID(t, window) {
		t.Errorf("the overlay took focus: the foreground window is %d, want the fixture %d", fg, mustWindowID(t, window))
	}

	// Click-through in practice: a click aimed through the pill banner
	// (the overlay's most opaque pixels, at the top centre) must land on
	// the fixture Window — winuse's own occlusion check fails loudly if
	// any window eats the point.
	pillX := (ov.Bounds.Left + ov.Bounds.Right) / 2
	pillY := ov.Bounds.Top + 32 // inside the pill at display scalings 100–250%
	shotX, shotY := shotCoordsOfScreen(t, window, info.scale, pillX, pillY)
	stdout, stderr, exit := runClickPoint(t, window, shotX, shotY)
	assertActionOK(t, "click through the overlay's pill", stdout, stderr, exit)

	// Absent from winuse's own screenshots: a capture taken while the
	// Action (and with it the overlay) is still running. The capture
	// bracket hides the indicator for the capture's duration.
	stdout, stderr, exit = runGetWindowStateScreenshot(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state --screenshot during an Action: exit %d, stderr:\n%s", exit, stderr)
	}
	during := decodeImage(t, parseScreenshotLine(t, stdout).path)
	if n := countAccentPixels(during); n > 0 {
		t.Errorf("the overlay appears in winuse's own screenshot: %d pixels of its accent colour", n)
	}

	// The overlay is restored after the capture hid it (the Action is
	// still running, so the indicator must be back up).
	select {
	case res := <-bg:
		// The Action already ended (a slow machine): the restore is not
		// observable any more; the remaining assertions still hold.
		assertActionOK(t, "type-text under the overlay", res.stdout, res.stderr, res.exit)
		t.Log("the Action finished before the restore check could run; skipping it")
	default:
		if _, ok := overlayWindow(t); !ok {
			t.Error("the overlay did not come back after the capture hid it")
		}
		res := <-bg
		assertActionOK(t, "type-text under the overlay", res.stdout, res.stderr, res.exit)
	}

	// The input was delivered while the indicator covered the Window,
	// and the overlay disappeared after the Action.
	fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.Text == text })
	waitOverlayGone(t, 5*time.Second)
}

func TestOverlayOnlyDuringActions(t *testing.T) {
	optIn(t)
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	window := strconv.FormatUint(st.WindowID, 10)

	// Observation commands never raise the indicator: a Window State read
	// (with a screenshot, so a helper is running and has an overlay
	// window, hidden) leaves the desktop without a shown overlay.
	if _, _, exit := runGetWindowStateScreenshot(t, window); exit != 0 {
		t.Fatalf("get-window-state --screenshot: exit %d", exit)
	}
	if ov, ok := overlayWindow(t); ok {
		t.Errorf("the overlay is shown (%+v) while no Action is running", ov.Bounds)
	}
}

// countAccentPixels counts pixels within tolerance of the overlay's
// accent colour (teal). The fixture paints nothing near it (its palette
// is gray, white, black, orange, dark yellow and red), so any hit is the
// overlay.
func countAccentPixels(img image.Image) int {
	const tol = 30
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if nearColour(uint8(r>>8), win.OverlayAccent.R, tol) &&
				nearColour(uint8(g>>8), win.OverlayAccent.G, tol) &&
				nearColour(uint8(bl>>8), win.OverlayAccent.B, tol) {
				n++
			}
		}
	}
	return n
}

func nearColour(v, want uint8, tol int) bool {
	d := int(v) - int(want)
	if d < 0 {
		d = -d
	}
	return d <= tol
}

// rectDistance returns how far two rects' edges are apart in total (0
// when they are equal); bounds read at slightly different moments can
// differ by a pixel.
func rectDistance(a, b win.Rect) int32 {
	d := abs32(a.Left - b.Left)
	d += abs32(a.Top - b.Top)
	d += abs32(a.Right - b.Right)
	d += abs32(a.Bottom - b.Bottom)
	return d
}
