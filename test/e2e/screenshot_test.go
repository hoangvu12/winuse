//go:build e2e

// The ticket-08 seam-1 tests: screenshots (--screenshot, zoom) and
// coordinate clicks (@x,y) against the fixture app, on the live desktop.
// User-run only (see the package comment and test/e2e/README.md for the
// opt-in rules); an agent must never start them.
//
// The coordinate tests re-derive the expected landing point from the
// printed scale and the fixture's own state file (its canvas rectangle in
// screen pixels) with independent arithmetic, so a bug in winuse's
// mapping cannot pass them.
//
// Display-scaling acceptance: these tests run at whatever scaling the
// machine has; the ticket's 100%, 150% and 175% and secondary-monitor
// verification happens by the user re-running the suite with the display
// (and the fixture's monitor) changed (see test/e2e/README.md).

package e2e

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
)

// runGetWindowStateScreenshot invokes `winuse get-window-state <window>
// --screenshot` with the suite's short helper idle timeout.
func runGetWindowStateScreenshot(t *testing.T, window string, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, []string{"get-window-state", window, "--screenshot"},
		append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// runZoom invokes `winuse zoom <window> <x>,<y>,<w>,<h>`.
func runZoom(t *testing.T, window string, x, y, w, h int, extraEnv ...string) (string, string, int) {
	t.Helper()
	region := fmt.Sprintf("%d,%d,%d,%d", x, y, w, h)
	return runWinuse(t, []string{"zoom", window, region}, append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// runClickPoint invokes `winuse click <window> @x,y [options]`.
func runClickPoint(t *testing.T, window string, x, y int, extraEnv ...string) (string, string, int) {
	t.Helper()
	ref := fmt.Sprintf("@%d,%d", x, y)
	return runWinuse(t, []string{"click", window, ref}, append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// screenshotInfo is a parsed Screenshot output line.
type screenshotInfo struct {
	path  string
	w, h  int
	scale float64
}

// parseScreenshotLine finds and parses the "Screenshot: <path> (<W>x<H>
// px, scale <s>)" line of a get-window-state --screenshot output.
func parseScreenshotLine(t *testing.T, stdout string) screenshotInfo {
	t.Helper()
	re := regexp.MustCompile(`(?m)^Screenshot: (.*) \((\d+)x(\d+) px, scale ([0-9.]+)\)$`)
	m := re.FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("no parsable Screenshot line in:\n%s", stdout)
	}
	scale, err := strconv.ParseFloat(m[4], 64)
	if err != nil {
		t.Fatalf("scale %q: %v", m[4], err)
	}
	w, _ := strconv.Atoi(m[2])
	h, _ := strconv.Atoi(m[3])
	return screenshotInfo{path: m[1], w: w, h: h, scale: scale}
}

// decodeImage opens and decodes an image file (JPEG or PNG).
func decodeImage(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return img
}

// startFixtureForShots starts the fixture, waits for its Window ID, and
// takes a screenshot of it, so the helper records the coordinate mapping.
// It returns the fixture, the Window reference and the parsed Screenshot
// line.
func startFixtureForShots(t *testing.T) (*fixtureProc, string, screenshotInfo) {
	t.Helper()
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	window := strconv.FormatUint(st.WindowID, 10)
	stdout, stderr, exit := runGetWindowStateScreenshot(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state --screenshot: exit %d, stderr:\n%s", exit, stderr)
	}
	assertFixtureTree(t, stdout)
	return fix, window, parseScreenshotLine(t, stdout)
}

// shotCoordsOfScreen converts a screen-space (physical) point of the
// fixture's window into the screenshot's coordinate space, using the
// printed scale and the window's extended frame bounds — the inverse of
// winuse's mapping, derived independently here.
func shotCoordsOfScreen(t *testing.T, window string, scale float64, x, y int32) (int, int) {
	t.Helper()
	b, err := win.FrameBounds(uint64(mustWindowID(t, window)))
	if err != nil {
		t.Fatalf("reading the fixture window's frame bounds: %v", err)
	}
	return int(float64(x-b.Left) * scale), int(float64(y-b.Top) * scale)
}

func mustWindowID(t *testing.T, window string) uint64 {
	t.Helper()
	id, err := strconv.ParseUint(window, 10, 64)
	if err != nil {
		t.Fatalf("window reference %q: %v", window, err)
	}
	return id
}

func TestScreenshotSavedScaledAndPathPrinted(t *testing.T) {
	optIn(t)
	_, _, info := startFixtureForShots(t)

	// The file exists under %TEMP%\winuse and decodes as an image.
	if !strings.HasPrefix(info.path, filepath.Join(os.TempDir(), "winuse")) {
		t.Errorf("screenshot path = %q, want under %%TEMP%%\\winuse", info.path)
	}
	img := decodeImage(t, info.path)
	b := img.Bounds()
	// The model-friendly cap (orchestrator decision): the long edge is at
	// most 1568 px.
	if long := maxOf(b.Dx(), b.Dy()); long > shot.MaxLongEdge {
		t.Errorf("screenshot is %dx%d; the long edge must be at most %d px", b.Dx(), b.Dy(), shot.MaxLongEdge)
	}
	// The printed size matches the file.
	if b.Dx() != info.w || b.Dy() != info.h {
		t.Errorf("printed size %dx%d, file is %dx%d", info.w, info.h, b.Dx(), b.Dy())
	}
	// The fixture window (800×470 client plus frame) is under the cap at
	// the test's scaling, so the scale is 1 and no pixel is wasted.
	if info.scale != 1 {
		t.Logf("scale = %v (display scaling or window size made the screenshot shrink)", info.scale)
	}
}

func TestScreenshotOnlyWhenRequested(t *testing.T) {
	optIn(t)
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	window := strconv.FormatUint(st.WindowID, 10)

	stdout, stderr, exit := runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	if strings.Contains(stdout, "Screenshot:") {
		t.Errorf("without --screenshot no screenshot may be taken (image tokens cost):\n%s", stdout)
	}
}

func TestCoordinateClickLandsOnAimedPoint(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// Aim at the centre of the canvas's lower-right quadrant: a point the
	// fixture reports in screen pixels.
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasRect != [4]int32{} })
	cr := st.CanvasRect
	targetX := (cr[0] + cr[2]) / 2
	targetY := (cr[1] + cr[3]) / 2

	// The screenshot pixel that covers that screen point (independent
	// arithmetic; the scale comes from the printed Screenshot line).
	shotX, shotY := shotCoordsOfScreen(t, window, info.scale, targetX, targetY)

	stdout, stderr, exit := runClickPoint(t, window, shotX, shotY)
	assertActionOK(t, "click @x,y", stdout, stderr, exit)

	// The click registered at the aimed point, within one pixel (the
	// footprint-centre rounding at any scale).
	st = fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return len(s.CanvasClicks) > 0 })
	last := st.CanvasClicks[len(st.CanvasClicks)-1]
	if abs32(last.X-targetX) > 1 || abs32(last.Y-targetY) > 1 {
		t.Errorf("click @%d,%d landed at canvas (%d, %d), wanted screen (%d, %d) within 1 px",
			shotX, shotY, last.X+cr[0], last.Y+cr[1], targetX, targetY)
	}
}

func TestCoordinateClickMarkVisibleInScreenshot(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// Click the canvas's centre; the fixture paints a red X mark there.
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasRect != [4]int32{} })
	cr := st.CanvasRect
	targetX, targetY := (cr[0]+cr[2])/2, (cr[1]+cr[3])/2
	shotX, shotY := shotCoordsOfScreen(t, window, info.scale, targetX, targetY)
	if _, _, exit := runClickPoint(t, window, shotX, shotY); exit != 0 {
		t.Fatalf("click @%d,%d: exit %d", shotX, shotY, exit)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return len(s.CanvasClicks) > 0 })

	// A fresh screenshot shows the mark at the clicked coordinates.
	stdout, stderr, exit := runGetWindowStateScreenshot(t, window)
	if exit != 0 {
		t.Fatalf("screenshot after the click: exit %d, stderr:\n%s", exit, stderr)
	}
	info2 := parseScreenshotLine(t, stdout)
	markX := int(float64(shotX) * info2.scale / info.scale)
	markY := int(float64(shotY) * info2.scale / info.scale)
	if !hasReddishPixelNear(decodeImage(t, info2.path), markX, markY, 6) {
		t.Errorf("the canvas click mark is not visible around (%d, %d) of the screenshot", markX, markY)
	}
}

func TestZoomCropsFullResolution(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// Click the canvas centre to place a red mark, then zoom the region
	// around it.
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasRect != [4]int32{} })
	cr := st.CanvasRect
	targetX, targetY := (cr[0]+cr[2])/2, (cr[1]+cr[3])/2
	shotX, shotY := shotCoordsOfScreen(t, window, info.scale, targetX, targetY)
	if _, _, exit := runClickPoint(t, window, shotX, shotY); exit != 0 {
		t.Fatalf("click @%d,%d: exit %d", shotX, shotY, exit)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return len(s.CanvasClicks) > 0 })

	// A 120×80 region of the screenshot around the mark.
	rx, ry := shotX-60, shotY-40
	if rx < 0 || ry < 0 {
		t.Fatalf("the aim point %d,%d is too close to the screenshot's edge for the zoom test", shotX, shotY)
	}
	stdout, stderr, exit := runZoom(t, window, rx, ry, 120, 80)
	if exit != 0 {
		t.Fatalf("zoom: exit %d, stderr:\n%s", exit, stderr)
	}
	if !strings.HasPrefix(stdout, "Zoom: ") {
		t.Fatalf("output = %q", stdout)
	}
	re := regexp.MustCompile(`^Zoom: (.*) \((\d+)x(\d+) px\)$`)
	m := re.FindStringSubmatch(strings.TrimSpace(stdout))
	if m == nil {
		t.Fatalf("no parsable Zoom line in %q", stdout)
	}
	w, _ := strconv.Atoi(m[2])
	h, _ := strconv.Atoi(m[3])
	// Full resolution: the crop is the requested region in screenshot
	// pixels (the fixture's window is captured 1:1), not a scaled image.
	if w != 120 || h != 80 {
		t.Errorf("crop is %dx%d px, want 120x80 at full resolution", w, h)
	}
	img := decodeImage(t, m[1])
	// The zoomed crop is legible: the red mark is in it, around the same
	// relative position (60, 40 from the crop's top-left).
	if !hasReddishPixelNear(img, 60, 40, 8) {
		t.Errorf("the red mark is not in the zoom crop around (60, 40)")
	}
}

func TestScreenshotShowsOpenDropdownMenu(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// Open the File menu: the dropdown is drawn over the window's content.
	idx := mainElementIndexes()[fixture.ElemMenuFile]
	if _, _, exit := runClick(t, []string{window, strconv.Itoa(idx)}); exit != 0 {
		t.Fatalf("click menu-file: exit %d", exit)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.OpenMenu == string(fixture.ElemMenuFile) })

	stdout, stderr, exit := runGetWindowStateScreenshot(t, window)
	if exit != 0 {
		t.Fatalf("screenshot with the menu open: exit %d, stderr:\n%s", exit, stderr)
	}
	withMenu := decodeImage(t, parseScreenshotLine(t, stdout).path)

	// Close the menu (click it again), take another screenshot: the
	// dropdown region must differ (the screenshot shows the live screen,
	// not a cached or PrintWindow-rendered frame).
	if _, _, exit := runClick(t, []string{window, strconv.Itoa(idx)}); exit != 0 {
		t.Fatalf("click menu-file (close): exit %d", exit)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.OpenMenu == "" })
	stdout, stderr, exit = runGetWindowStateScreenshot(t, window)
	if exit != 0 {
		t.Fatalf("screenshot without the menu: exit %d, stderr:\n%s", exit, stderr)
	}
	withoutMenu := decodeImage(t, parseScreenshotLine(t, stdout).path)

	// The dropdown covers client rect (8, 28)-(208, 100) (the fixture's
	// layout); convert to screenshot pixels through the frame bounds.
	b, err := win.FrameBounds(mustWindowID(t, window))
	if err != nil {
		t.Fatalf("frame bounds: %v", err)
	}
	// The client origin: the canvas's screen rect minus its client coords
	// (the fixture's layout table gives the client rect).
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasRect != [4]int32{} })
	cr := st.CanvasRect
	canvasClient, _ := fixture.Rect(fixture.ElemCanvas, 0)
	clientX := cr[0] - canvasClient.Left
	clientY := cr[1] - canvasClient.Top
	lo := image.Pt(int(float64(clientX+8-b.Left)*info.scale), int(float64(clientY+28-b.Top)*info.scale))
	hi := image.Pt(int(float64(clientX+208-b.Left)*info.scale), int(float64(clientY+100-b.Top)*info.scale))

	differs := false
	for y := lo.Y; y < hi.Y && !differs; y++ {
		for x := lo.X; x < hi.X && !differs; x++ {
			if !withinBounds(withMenu, x, y) || !withinBounds(withoutMenu, x, y) {
				continue
			}
			p1, p2 := withMenu.At(x, y), withoutMenu.At(x, y)
			if !sameColor(p1, p2) {
				differs = true
			}
		}
	}
	if !differs {
		t.Errorf("the open dropdown menu is not visible in the screenshot (region %v..%v)", lo, hi)
	}
}

func TestScreenshotOfBlockedWindowNamesDialog(t *testing.T) {
	optIn(t)
	fix, window, _ := startFixtureForShots(t)

	// Open the Save As dialog; a screenshot of the blocked main Window
	// fails naming the dialog (the region capture needs the foreground).
	saveAs := mainElementIndexes()[fixture.ElemButtonSaveAs]
	if _, _, exit := runClick(t, []string{window, strconv.Itoa(saveAs)}); exit != 0 {
		t.Fatalf("click button-save-as: exit %d", exit)
	}
	st := fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.DialogOpen })
	dialog := strconv.FormatUint(st.DialogWindowID, 10)

	stdout, stderr, exit := runGetWindowStateScreenshot(t, window)
	assertActionFailed(t, "screenshot of the blocked main window", stdout, stderr, exit,
		"has a modal dialog open (window "+dialog+")")
	if strings.Contains(stdout, "Screenshot:") {
		t.Errorf("no screenshot line may be printed on failure:\n%s", stdout)
	}

	// The dialog itself screenshots fine.
	stdout, stderr, exit = runGetWindowStateScreenshot(t, dialog)
	if exit != 0 {
		t.Fatalf("screenshot of the dialog: exit %d, stderr:\n%s", exit, stderr)
	}
	if !strings.Contains(stdout, "Screenshot: ") {
		t.Errorf("the dialog's screenshot line is missing:\n%s", stdout)
	}
}

func TestCoordinateClickWithoutScreenshotAfterRestart(t *testing.T) {
	optIn(t)
	_, window, _ := startFixtureForShots(t)

	// A helper restart loses the recorded mapping (like Element Indexes,
	// coordinates are only valid within the helper that took the
	// screenshot): the click says so instead of guessing.
	killHelper(t)
	stdout, stderr, exit := runClickPoint(t, window, 100, 100)
	assertActionFailed(t, "click @x,y after helper restart", stdout, stderr, exit,
		"no screenshot is available for window "+window)
	if !strings.Contains(stderr, "--screenshot") {
		t.Errorf("the failure must point at get-window-state --screenshot: %q", stderr)
	}
}

func TestCoordinateClickOutsideScreenshotRefused(t *testing.T) {
	optIn(t)
	_, window, info := startFixtureForShots(t)

	stdout, stderr, exit := runClickPoint(t, window, info.w+50, info.h+50)
	assertActionFailed(t, "click outside the screenshot", stdout, stderr, exit,
		fmt.Sprintf("point @%d,%d is outside the latest screenshot of window %s (%dx%d px)",
			info.w+50, info.h+50, window, info.w, info.h))
}

func TestZoomWithoutScreenshotRefused(t *testing.T) {
	optIn(t)
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	window := strconv.FormatUint(st.WindowID, 10)

	stdout, stderr, exit := runZoom(t, window, 10, 10, 100, 100)
	assertActionFailed(t, "zoom without a screenshot", stdout, stderr, exit,
		"no screenshot is available for window "+window)
	if !strings.Contains(stderr, "--screenshot") {
		t.Errorf("the failure must point at get-window-state --screenshot: %q", stderr)
	}
}

// hasReddishPixelNear reports whether img has a red-dominant pixel (the
// fixture's canvas click marks are drawn red) within r pixels of (x, y).
func hasReddishPixelNear(img image.Image, x, y, r int) bool {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			px, py := x+dx, y+dy
			if !withinBounds(img, px, py) {
				continue
			}
			cr, cg, cb, _ := img.At(px, py).RGBA()
			cr, cg, cb = cr>>8, cg>>8, cb>>8
			if cr > cg+40 && cr > cb+40 {
				return true
			}
		}
	}
	return false
}

func withinBounds(img image.Image, x, y int) bool {
	b := img.Bounds()
	return x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y
}

// sameColor reports whether two colours are close enough to be the same
// after JPEG compression.
func sameColor(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return abs32(int32(ar>>8)-int32(br>>8)) <= 8 &&
		abs32(int32(ag>>8)-int32(bg>>8)) <= 8 &&
		abs32(int32(ab>>8)-int32(bb>>8)) <= 8
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func maxOf(a, b int) int {
	if a > b {
		return a
	}
	return b
}
