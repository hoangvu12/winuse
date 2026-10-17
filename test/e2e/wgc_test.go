//go:build e2e

// The ticket-09 seam-1 tests: Windows.Graphics.Capture as the capture
// chain's first method, and the automatic fallback when it is switched
// off or times out. User-run only (see the package comment and
// test/e2e/README.md for the opt-in rules); an agent must never start
// them.
//
// Which method captured a frame is observable through the helper's log
// (%LOCALAPPDATA%\winuse\helper.log — the only file a detached helper
// writes, and the documented diagnostic surface): the WGC method logs
// "wgc: captured window …" on success and "wgc: window … fell back: …"
// whenever it defers to the GDI methods. The tests read the log's delta
// around one CLI invocation, never winuse internals.
//
// The forced-failure levers are the method's environment knobs, read once
// per helper process: WINUSE_WGC=off disables it outright;
// WINUSE_WGC_BUDGET=1ms starves the frame poll so no frame arrives in
// time. Each env-sensitive test kills the running helper first so the
// fresh one inherits this invocation's environment.

package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/shot"
)

// helperLogPath is the helper's log file.
func helperLogPath(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		t.Fatal("LOCALAPPDATA is not set")
	}
	return filepath.Join(dir, "winuse", "helper.log")
}

// helperLogSince returns the helper log's content appended after offset,
// tolerating the 1 MiB truncation (which restarts the file).
func helperLogSince(t *testing.T, offset int64) string {
	t.Helper()
	data, err := os.ReadFile(helperLogPath(t))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("reading the helper log: %v", err)
	}
	if int64(len(data)) < offset {
		return string(data)
	}
	return string(data[offset:])
}

// runWinuseLogged runs one winuse invocation and returns its stdout,
// stderr, exit code and the helper log lines appended while it ran.
func runWinuseLogged(t *testing.T, args []string, extraEnv ...string) (string, string, int, string) {
	t.Helper()
	var offset int64
	if fi, err := os.Stat(helperLogPath(t)); err == nil {
		offset = fi.Size()
	}
	stdout, stderr, exit := runWinuse(t, args, append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
	return stdout, stderr, exit, helperLogSince(t, offset)
}

// wgcUnavailable reports whether the helper log says this machine cannot
// do Windows.Graphics.Capture at all (before Windows 10 1903, or the
// GraphicsCaptureSession statics refuse): the fallback tests still apply,
// but the "captured via WGC" ones skip — the criterion is "when
// available".
func wgcUnavailable(t *testing.T, log string) bool {
	t.Helper()
	return strings.Contains(log, "fell back: Windows.Graphics.Capture is not supported")
}

// runWGCScreenshot takes one screenshot of the fixture window through the
// real CLI, returning the parsed Screenshot line and the helper log delta
// of the invocation.
func runWGCScreenshot(t *testing.T, window string, extraEnv ...string) (screenshotInfo, string) {
	t.Helper()
	stdout, stderr, exit, log := runWinuseLogged(t,
		[]string{"get-window-state", window, "--screenshot"}, extraEnv...)
	if exit != 0 {
		t.Fatalf("get-window-state --screenshot: exit %d, stderr:\n%s", exit, stderr)
	}
	assertFixtureTree(t, stdout)
	return parseScreenshotLine(t, stdout), log
}

// startFixtureForWGC starts the fixture and waits for its Window ID,
// killing any running helper first so the helper this test spawns
// inherits this test's environment (the WGC knobs are helper-side).
func startFixtureForWGC(t *testing.T) (*fixtureProc, string) {
	t.Helper()
	killHelper(t)
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	return fix, strconv.FormatUint(st.WindowID, 10)
}

// assertScreenshotDecodes checks the Screenshot line's file exists under
// %TEMP%\winuse, decodes, matches the printed size, and respects the
// model-friendly cap.
func assertScreenshotDecodes(t *testing.T, info screenshotInfo) {
	t.Helper()
	if !strings.HasPrefix(info.path, filepath.Join(os.TempDir(), "winuse")) {
		t.Errorf("screenshot path = %q, want under %%TEMP%%\\winuse", info.path)
	}
	img := decodeImage(t, info.path)
	b := img.Bounds()
	if long := maxOf(b.Dx(), b.Dy()); long > shot.MaxLongEdge {
		t.Errorf("screenshot is %dx%d; the long edge must be at most %d px", b.Dx(), b.Dy(), shot.MaxLongEdge)
	}
	if b.Dx() != info.w || b.Dy() != info.h {
		t.Errorf("printed size %dx%d, file is %dx%d", info.w, info.h, b.Dx(), b.Dy())
	}
}

// TestWGCScreenshotCapturedViaWindowsGraphicsCapture is the positive
// criterion: with WGC available and enabled (the default), the fixture's
// screenshot is captured by Windows.Graphics.Capture — the helper log
// names it — and the saved image is the expected model-friendly
// screenshot.
func TestWGCScreenshotCapturedViaWindowsGraphicsCapture(t *testing.T) {
	optIn(t)
	_, window := startFixtureForWGC(t)

	info, log := runWGCScreenshot(t, window)
	assertScreenshotDecodes(t, info)
	if wgcUnavailable(t, log) {
		t.Skip("Windows.Graphics.Capture is not supported on this machine; the fallback tests still apply")
	}
	if !strings.Contains(log, "wgc: captured window "+window+" ") {
		t.Errorf("the screenshot was not captured via Windows.Graphics.Capture; helper log delta:\n%s", log)
	}
}

// TestWGCDisabledFallsBackToGDI is the forced-failure criterion:
// WINUSE_WGC=off makes the method decline, and the screenshot still
// arrives — through the GDI methods (the log shows the fallback, and no
// WGC capture).
func TestWGCDisabledFallsBackToGDI(t *testing.T) {
	optIn(t)
	_, window := startFixtureForWGC(t)

	info, log := runWGCScreenshot(t, window, "WINUSE_WGC=off")
	assertScreenshotDecodes(t, info)
	if strings.Contains(log, "wgc: captured window") {
		t.Errorf("a disabled Windows.Graphics.Capture must not capture; helper log delta:\n%s", log)
	}
	if !strings.Contains(log, "wgc: window "+window+" fell back") {
		t.Errorf("the disabled method must log its fallback; helper log delta:\n%s", log)
	}
}

// TestWGCTimeoutFallsBackToGDI is the timeout failure: a starved frame
// budget (1 ms) means no frame arrives in time, and the screenshot still
// arrives through the GDI methods. (Whether a frame squeaks through 1 ms
// is timing-dependent, so only the screenshot is asserted.)
func TestWGCTimeoutFallsBackToGDI(t *testing.T) {
	optIn(t)
	_, window := startFixtureForWGC(t)

	info, _ := runWGCScreenshot(t, window, "WINUSE_WGC_BUDGET=1ms")
	assertScreenshotDecodes(t, info)
}

// TestCoordinateClickExactViaWGC pins the coordinate criterion: a
// screenshot taken through Windows.Graphics.Capture maps exactly — a
// `@x,y` click aimed at the fixture canvas's lower-right quadrant (its
// screen rectangle reported by the fixture, converted to screenshot
// pixels with independent arithmetic from the printed scale) lands within
// one pixel of the aim.
func TestCoordinateClickExactViaWGC(t *testing.T) {
	optIn(t)
	fix, window := startFixtureForWGC(t)

	info, log := runWGCScreenshot(t, window)
	if wgcUnavailable(t, log) {
		t.Skip("Windows.Graphics.Capture is not supported on this machine")
	}
	if !strings.Contains(log, "wgc: captured window "+window+" ") {
		t.Fatalf("the screenshot was not captured via Windows.Graphics.Capture; helper log delta:\n%s", log)
	}

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasRect != [4]int32{} })
	cr := st.CanvasRect
	targetX := (cr[0] + cr[2]) / 2
	targetY := (cr[1] + cr[3]) / 2
	shotX, shotY := shotCoordsOfScreen(t, window, info.scale, targetX, targetY)

	stdout, stderr, exit := runClickPoint(t, window, shotX, shotY)
	assertActionOK(t, "click @x,y via a WGC screenshot", stdout, stderr, exit)

	st = fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return len(s.CanvasClicks) > 0 })
	last := st.CanvasClicks[len(st.CanvasClicks)-1]
	if abs32(last.X-targetX) > 1 || abs32(last.Y-targetY) > 1 {
		t.Errorf("click @%d,%d (from a WGC screenshot) landed at canvas (%d, %d), wanted screen (%d, %d) within 1 px",
			shotX, shotY, last.X+cr[0], last.Y+cr[1], targetX, targetY)
	}
}

// TestWGCZoomStillFullResolution pins zoom through the WGC path: the crop
// comes from a fresh full-resolution capture and the fixture's canvas
// mark is in it.
func TestWGCZoomStillFullResolution(t *testing.T) {
	optIn(t)
	fix, window := startFixtureForWGC(t)

	info, log := runWGCScreenshot(t, window)
	if wgcUnavailable(t, log) {
		t.Skip("Windows.Graphics.Capture is not supported on this machine")
	}

	// Click the canvas centre to place a red mark, then zoom around it.
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasRect != [4]int32{} })
	cr := st.CanvasRect
	targetX, targetY := (cr[0]+cr[2])/2, (cr[1]+cr[3])/2
	shotX, shotY := shotCoordsOfScreen(t, window, info.scale, targetX, targetY)
	if _, _, exit := runClickPoint(t, window, shotX, shotY); exit != 0 {
		t.Fatalf("click @%d,%d: exit %d", shotX, shotY, exit)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return len(s.CanvasClicks) > 0 })

	rx, ry := shotX-60, shotY-40
	if rx < 0 || ry < 0 {
		t.Fatalf("the aim point %d,%d is too close to the screenshot's edge for the zoom test", shotX, shotY)
	}
	stdout, stderr, exit := runZoom(t, window, rx, ry, 120, 80)
	if exit != 0 {
		t.Fatalf("zoom: exit %d, stderr:\n%s", exit, stderr)
	}
	m := regexp.MustCompile(`^Zoom: (.*) \((\d+)x(\d+) px\)$`).FindStringSubmatch(strings.TrimSpace(stdout))
	if m == nil {
		t.Fatalf("no parsable Zoom line in %q", stdout)
	}
	w, _ := strconv.Atoi(m[2])
	h, _ := strconv.Atoi(m[3])
	if w != 120 || h != 80 {
		t.Errorf("crop is %dx%d px, want 120x80 at full resolution", w, h)
	}
	if !hasReddishPixelNear(decodeImage(t, m[1]), 60, 40, 8) {
		t.Errorf("the red mark is not in the zoom crop around (60, 40)")
	}
}
