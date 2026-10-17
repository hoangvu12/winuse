package helper

// The screenshot commands' offline tests (ticket 08): get-window-state
// --screenshot, zoom, and the `@x,y` coordinate click. Everything
// capture-shaped, activation and input is faked through Services; the
// coordinate maths is asserted exactly (the pure Mapping is unit-tested in
// internal/shot; these tests pin how the commands use it).

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// shotSpy fakes the screenshot services over the standard fake windows:
// a configurable capture (a synthetic frame), the real shot.Store, a
// configurable frame origin, and the action pipeline's fakes.
type shotSpy struct {
	capture    func(hwnd uint64) (shot.Frame, error)
	frame      shot.Frame // the default capture answer
	captureErr error
	captures   int

	origin    image.Point
	originErr error

	locked        bool
	lockedDesktop string
	activateErr   error
	activations   int

	clickErr  error
	clicks    [][2]int32
	clickedID uint64

	svc Services
}

func newShotSpy(t *testing.T) *shotSpy {
	t.Helper()
	s := &shotSpy{origin: image.Point{X: 100, Y: 200}}
	s.svc = testServices(&fakeTrees{trees: map[uint64]*wstate.Tree{
		101: cannedTree(),
	}})
	s.svc.Shots = shot.NewStore() // visible to the test (CommandsWith copies Services by value)
	s.svc.Locked = func() (bool, string) { return s.locked, s.lockedDesktop }
	s.svc.Integrity = func(win.WindowInfo) error { return nil }
	s.svc.Activate = func(hwnd uint64) error { s.activations++; return s.activateErr }
	s.svc.Actions = nil // coordinate clicks need no UIA client
	s.svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		s.clicks = append(s.clicks, [2]int32{x, y})
		s.clickedID = target
		return s.clickErr
	}
	s.svc.FrameOrigin = func(hwnd uint64) (image.Point, error) {
		if s.originErr != nil {
			return image.Point{}, s.originErr
		}
		return s.origin, nil
	}
	s.svc.Capture = func(hwnd uint64) (shot.Frame, error) {
		s.captures++
		if s.capture != nil {
			return s.capture(hwnd)
		}
		if s.captureErr != nil {
			return shot.Frame{}, s.captureErr
		}
		return s.frame, nil
	}
	return s
}

// syntheticFrame builds a w×h frame anchored at its own rect top-left
// (a plain window capture: offset 0) with position-encoding colours.
func syntheticFrame(w, h int, left, top int) shot.Frame {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 241), B: 90, A: 255})
		}
	}
	return shot.Frame{
		Image:  img,
		Rect:   image.Rect(left, top, left+w, top+h),
		Anchor: image.Point{X: left, Y: top},
	}
}

func runJSON(t *testing.T, cmds map[string]Command, cmd string, args any) (string, *Error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	c, ok := cmds[cmd]
	if !ok {
		t.Fatalf("command %q not registered", cmd)
	}
	return c(context.Background(), raw)
}

func TestGetWindowStateScreenshotOutput(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)

	out, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("get-window-state --screenshot: %v", cmdErr.Error())
	}
	// The tree is still the head of the output...
	if !strings.HasPrefix(out, wstate.Render(cannedTree())) {
		t.Errorf("output must start with the tree render:\n%s", out)
	}
	// ...followed by exactly one Screenshot line.
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	var shotLines []string
	for _, l := range lines {
		if strings.HasPrefix(l, "Screenshot: ") {
			shotLines = append(shotLines, l)
		}
	}
	if len(shotLines) != 1 {
		t.Fatalf("want exactly one Screenshot line, got %d in:\n%s", len(shotLines), out)
	}
	line := shotLines[0]
	if !strings.Contains(line, "(800x470 px, scale 1)") {
		t.Errorf("screenshot line = %q, want the size and scale of a window under the cap", line)
	}

	// The path points at a real, decodable JPEG file under %TEMP%\winuse.
	path := strings.TrimSuffix(strings.TrimPrefix(line, "Screenshot: "), " (800x470 px, scale 1)")
	if !strings.HasPrefix(path, filepath.Join(os.TempDir(), "winuse")) {
		t.Errorf("screenshot path = %q, want it under %%TEMP%%\\winuse", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the screenshot: %v", err)
	}
	defer f.Close()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatalf("the screenshot must be a JPEG: %v", err)
	}
	if cfg.Width != 800 || cfg.Height != 470 {
		t.Errorf("decoded size = %dx%d, want 800x470 (within the cap, no scaling)", cfg.Width, cfg.Height)
	}

	// The Window was activated before the capture (the region method needs
	// the foreground), and the capture ran exactly once.
	if s.activations != 1 || s.captures != 1 {
		t.Errorf("activations/captures = %d/%d, want 1/1", s.activations, s.captures)
	}
}

func TestGetWindowStateScreenshotScalesBigWindow(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(3840, 2160, 0, 0)
	cmds := CommandsWith(s.svc)

	out, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	if !strings.Contains(out, "(1568x882 px, scale 0.408)") {
		t.Errorf("the long edge must cap at 1568 px with the scale recorded:\n%s", out)
	}
	path := screenshotPath(t, out)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer f.Close()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Width != 1568 || cfg.Height != 882 {
		t.Errorf("decoded size = %dx%d, want 1568x882", cfg.Width, cfg.Height)
	}

	// The mapping recorded for the Window describes the saved image.
	m, ok := s.svc.Shots.Lookup(101)
	if !ok {
		t.Fatal("the mapping must be recorded")
	}
	if m.Width != 1568 || m.Height != 882 {
		t.Errorf("recorded mapping size = %dx%d, want 1568x882", m.Width, m.Height)
	}
	if m.Scale != 1568.0/3840 {
		t.Errorf("recorded scale = %v, want %v", m.Scale, 1568.0/3840)
	}
}

func screenshotPath(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Screenshot: ") {
			rest := strings.TrimPrefix(l, "Screenshot: ")
			if i := strings.Index(rest, " ("); i >= 0 {
				return rest[:i]
			}
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("no Screenshot line in:\n%s", out)
	return ""
}

func TestGetWindowStateScreenshotLockedDesktopRefuses(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	s.locked, s.lockedDesktop = true, "LockApp"
	cmds := CommandsWith(s.svc)

	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr == nil {
		t.Fatal("a locked desktop must refuse the screenshot")
	}
	if !strings.Contains(cmdErr.Error(), "Windows desktop is locked (input desktop is LockApp)") {
		t.Errorf("error = %q", cmdErr.Error())
	}
	// Nothing touched the screen.
	if s.activations != 0 || s.captures != 0 {
		t.Errorf("activations/captures = %d/%d, want 0/0", s.activations, s.captures)
	}
}

func TestGetWindowStateScreenshotActivationFailure(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	s.activateErr = &win.ModalDialogError{WindowID: 101, DialogID: 404}
	cmds := CommandsWith(s.svc)

	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr == nil {
		t.Fatal("a failed activation must fail the screenshot")
	}
	if !strings.Contains(cmdErr.Error(), "has a modal dialog open (window 404)") {
		t.Errorf("error = %q, want the dialog named", cmdErr.Error())
	}
	if s.captures != 0 {
		t.Error("no capture may run when activation fails")
	}
}

func TestGetWindowStateScreenshotCaptureFailure(t *testing.T) {
	s := newShotSpy(t)
	s.captureErr = fmt.Errorf("%w: screen region: the Window is partly off-screen; PrintWindow: PrintWindow did not answer within 1.5s", shot.ErrCapture)
	cmds := CommandsWith(s.svc)

	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr == nil {
		t.Fatal("a failed capture must fail the command")
	}
	if !strings.Contains(cmdErr.Error(), "capturing the window failed") {
		t.Errorf("error = %q", cmdErr.Error())
	}
	if !strings.Contains(cmdErr.Hint, "operate from the Element tree") {
		t.Errorf("hint = %q", cmdErr.Hint)
	}
	// No mapping was recorded for a failed screenshot.
	if _, ok := s.svc.Shots.Lookup(101); ok {
		t.Error("a failed screenshot must not record a mapping")
	}
}

func TestGetWindowStateWithoutFlagCapturesNothing(t *testing.T) {
	s := newShotSpy(t)
	cmds := CommandsWith(s.svc)
	out, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101"})
	if cmdErr != nil {
		t.Fatalf("get-window-state: %v", cmdErr.Error())
	}
	if strings.Contains(out, "Screenshot:") {
		t.Errorf("no screenshot line without the flag: %q", out)
	}
	if s.activations != 0 || s.captures != 0 {
		t.Errorf("activations/captures = %d/%d, want 0/0 (the tree alone touches nothing)", s.activations, s.captures)
	}
}

func TestZoomCropIsFullResolution(t *testing.T) {
	s := newShotSpy(t)
	// First a screenshot to record the mapping: a 3136×1960 window at
	// (100, 200) — over the cap, so it scales by exactly 0.5.
	s.frame = syntheticFrame(3136, 1960, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}

	// Zoom a 100×60 region of the scaled screenshot: it covers
	// 200×120 physical pixels around the screenshot's (400, 300).
	s.captures = 0
	out, cmdErr := runJSON(t, cmds, "zoom", protocol.ZoomArgs{
		Window: "101",
		Region: protocol.Region{X: 400, Y: 300, W: 100, H: 60},
	})
	if cmdErr != nil {
		t.Fatalf("zoom: %v", cmdErr.Error())
	}
	if s.captures != 1 {
		t.Errorf("zoom must capture fresh once, captured %d times", s.captures)
	}
	if s.activations != 2 {
		t.Errorf("activations = %d, want 2 (screenshot + zoom)", s.activations)
	}
	if !strings.HasPrefix(out, "Zoom: ") {
		t.Errorf("output = %q", out)
	}
	if !strings.Contains(out, "(200x120 px)") {
		t.Errorf("the crop must be full resolution (200x120 physical px):\n%s", out)
	}

	// The crop is a real PNG with exactly the expected pixels: the
	// synthetic frame's colour at the mapped source position.
	path := zoomPath(t, out)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the crop: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("the crop must be a PNG: %v", err)
	}
	if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 120 {
		t.Fatalf("crop size = %v, want 200x120", img.Bounds())
	}
	// Screenshot (400, 300) maps to window-space 800, 600 (scale 0.5):
	// the crop's (0,0) is the frame's physical pixel (800, 600).
	want := color.RGBA{R: uint8(800 % 251), G: uint8(600 % 241), B: 90, A: 255}
	if got := img.At(0, 0); got != want {
		t.Errorf("crop pixel (0,0) = %v, want %v (the frame's pixel at 800,600)", got, want)
	}
}

// zoomPath extracts the crop's path from a Zoom output line.
func zoomPath(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Zoom: ") {
			rest := strings.TrimPrefix(l, "Zoom: ")
			if i := strings.Index(rest, " ("); i >= 0 {
				return rest[:i]
			}
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("no Zoom line in:\n%s", out)
	return ""
}

func TestZoomWithoutScreenshotFails(t *testing.T) {
	s := newShotSpy(t)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "zoom", protocol.ZoomArgs{
		Window: "101",
		Region: protocol.Region{X: 10, Y: 10, W: 100, H: 100},
	})
	if cmdErr == nil {
		t.Fatal("zoom without a recorded screenshot must fail")
	}
	if !strings.Contains(cmdErr.Error(), "no screenshot is available for window 101") {
		t.Errorf("error = %q", cmdErr.Error())
	}
	if !strings.Contains(cmdErr.Hint, "--screenshot") {
		t.Errorf("hint must point at get-window-state --screenshot: %q", cmdErr.Hint)
	}
	// Nothing touched the screen (the cheap check refuses first).
	if s.activations != 0 || s.captures != 0 {
		t.Errorf("activations/captures = %d/%d, want 0/0", s.activations, s.captures)
	}
}

func TestZoomRegionOutsideScreenshotFails(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	_, cmdErr = runJSON(t, cmds, "zoom", protocol.ZoomArgs{
		Window: "101",
		Region: protocol.Region{X: 900, Y: 0, W: 50, H: 50}, // the screenshot is 800 wide
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "outside the latest screenshot of window 101 (800x470 px)") {
		t.Fatalf("want out-of-screenshot failure, got %v", cmdErr)
	}
	if s.captures != 1 {
		t.Errorf("no second capture for a refused region, got %d", s.captures)
	}
}

func TestZoomKeepsRecordedMapping(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	// A zoom whose fresh capture has different geometry (the window grew):
	// the recorded mapping is NOT replaced.
	s.frame = syntheticFrame(1200, 700, 300, 400)
	if _, cmdErr := runJSON(t, cmds, "zoom", protocol.ZoomArgs{
		Window: "101",
		Region: protocol.Region{X: 10, Y: 10, W: 50, H: 50},
	}); cmdErr != nil {
		t.Fatalf("zoom: %v", cmdErr.Error())
	}
	m, _ := s.svc.Shots.Lookup(101)
	if m.Width != 800 || m.Height != 470 {
		t.Errorf("zoom must not replace the mapping, got %dx%d", m.Width, m.Height)
	}
}

func TestZoomValidatesRegion(t *testing.T) {
	s := newShotSpy(t)
	cmds := CommandsWith(s.svc)
	for _, region := range []protocol.Region{
		{X: -1, Y: 0, W: 10, H: 10},
		{X: 0, Y: -1, W: 10, H: 10},
		{X: 0, Y: 0, W: 0, H: 10},
		{X: 0, Y: 0, W: 10, H: 0},
	} {
		_, cmdErr := runJSON(t, cmds, "zoom", protocol.ZoomArgs{Window: "101", Region: region})
		if cmdErr == nil {
			t.Errorf("region %+v must be refused", region)
		}
	}
}

func TestZoomLockedDesktopAndActivation(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	s.locked, s.lockedDesktop = true, ""
	if _, cmdErr := runJSON(t, cmds, "zoom", protocol.ZoomArgs{Window: "101", Region: protocol.Region{X: 1, Y: 1, W: 5, H: 5}}); cmdErr == nil {
		t.Fatal("a locked desktop must refuse zoom")
	}
	s.locked = false
	s.activateErr = &win.NotForegroundError{WindowID: 101}
	_, cmdErr = runJSON(t, cmds, "zoom", protocol.ZoomArgs{Window: "101", Region: protocol.Region{X: 1, Y: 1, W: 5, H: 5}})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "failed to activate window 101") {
		t.Fatalf("want activation failure, got %v", cmdErr)
	}
}

func TestClickCoordinateLandsOnMappedPixel(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}

	// The window moved since the screenshot: the frame origin is now
	// (150, 260). A window-anchored mapping shifts with it.
	s.origin = image.Point{X: 150, Y: 260}
	out, cmdErr := runJSON(t, cmds, "click", protocol.ClickArgs{
		Window: "101",
		Point:  &protocol.Point{X: 400, Y: 235},
	})
	if cmdErr != nil {
		t.Fatalf("click @x,y: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	// Screenshot (400, 235) is exactly the physical (400, 235) of the
	// window (scale 1, offset 0); with the frame bounds now at (150, 260)
	// the click lands at screen (550, 495).
	if len(s.clicks) != 1 {
		t.Fatalf("clicks = %v", s.clicks)
	}
	if s.clicks[0][0] != 550 || s.clicks[0][1] != 495 {
		t.Errorf("clicked at (%d, %d), want (550, 495) (mapped point plus the fresh anchor)", s.clicks[0][0], s.clicks[0][1])
	}
	if s.clickedID != 101 {
		t.Errorf("clicked window %d", s.clickedID)
	}
}

func TestClickCoordinateHalfScaleCentre(t *testing.T) {
	s := newShotSpy(t)
	// A 4K window: scale 1568/3840, so screenshot pixels cover multiple
	// physical pixels; the click aims at the footprint centre.
	s.frame = syntheticFrame(3840, 2160, 0, 0)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	s.origin = image.Point{X: -1920, Y: 0} // secondary monitor left of the primary
	out, cmdErr := runJSON(t, cmds, "click", protocol.ClickArgs{
		Window: "101",
		Point:  &protocol.Point{X: 0, Y: 0},
	})
	if cmdErr != nil {
		t.Fatalf("click @x,y: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q", out)
	}
	// Screenshot pixel 0 at scale 1568/3840 aims at the physical pixel
	// floor(0.5/s) = 1; the anchor is (-1920, 0).
	wantX, wantY := int32(-1920+1), int32(1)
	if s.clicks[0][0] != wantX || s.clicks[0][1] != wantY {
		t.Errorf("clicked at (%d, %d), want (%d, %d) (negative coordinates included: any monitor)",
			s.clicks[0][0], s.clicks[0][1], wantX, wantY)
	}
}

func TestClickCoordinateWithoutScreenshot(t *testing.T) {
	s := newShotSpy(t)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "click", protocol.ClickArgs{
		Window: "101",
		Point:  &protocol.Point{X: 10, Y: 10},
	})
	if cmdErr == nil {
		t.Fatal("click @x,y without a screenshot must fail")
	}
	if !strings.Contains(cmdErr.Error(), "no screenshot is available for window 101") {
		t.Errorf("error = %q", cmdErr.Error())
	}
	if !strings.Contains(cmdErr.Hint, "--screenshot") {
		t.Errorf("hint must point at get-window-state --screenshot: %q", cmdErr.Hint)
	}
	// The full action pipeline ran (checks, activation) but no input was
	// injected — the reference fails before the click.
	if s.activations == 0 {
		t.Error("the pipeline activates before resolving the reference")
	}
	if len(s.clicks) != 0 {
		t.Errorf("clicks = %v, want none", s.clicks)
	}
}

func TestClickCoordinateOutsideScreenshot(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	_, cmdErr = runJSON(t, cmds, "click", protocol.ClickArgs{
		Window: "101",
		Point:  &protocol.Point{X: 800, Y: 235},
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "point @800,235 is outside the latest screenshot of window 101 (800x470 px)") {
		t.Fatalf("want out-of-screenshot failure, got %v", cmdErr)
	}
	if len(s.clicks) != 0 {
		t.Errorf("clicks = %v, want none", s.clicks)
	}
}

func TestClickCoordinateRefusalsMatchPipeline(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	// A locked desktop refuses before anything else, exactly like the
	// Element Index form.
	s.locked = true
	_, cmdErr = runJSON(t, cmds, "click", protocol.ClickArgs{Window: "101", Point: &protocol.Point{X: 1, Y: 1}})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "Windows desktop is locked") {
		t.Fatalf("want locked-desktop refusal, got %v", cmdErr)
	}
	s.locked = false

	// Both reference forms at once is a usage error.
	idx := 3
	_, cmdErr = runJSON(t, cmds, "click", protocol.ClickArgs{
		Window:       "101",
		ElementIndex: &idx,
		Point:        &protocol.Point{X: 1, Y: 1},
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "not both") {
		t.Fatalf("want both-forms refusal, got %v", cmdErr)
	}

	// Negative coordinates are refused.
	_, cmdErr = runJSON(t, cmds, "click", protocol.ClickArgs{
		Window: "101",
		Point:  &protocol.Point{X: -1, Y: 1},
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "non-negative") {
		t.Fatalf("want negative-coordinate refusal, got %v", cmdErr)
	}
	if len(s.clicks) != 0 {
		t.Errorf("clicks = %v, want none", s.clicks)
	}
}

func TestClickCoordinateWindowGone(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	s.originErr = fmt.Errorf("%w (window 101): reading the extended frame bounds failed", win.ErrGone)
	_, cmdErr = runJSON(t, cmds, "click", protocol.ClickArgs{Window: "101", Point: &protocol.Point{X: 1, Y: 1}})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "window is gone") {
		t.Fatalf("want window-gone failure, got %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "list-windows") {
		t.Errorf("hint = %q", cmdErr.Hint)
	}
}

func TestScreenshotCommandsRegistered(t *testing.T) {
	cmds := Commands()
	if _, ok := cmds["zoom"]; !ok {
		t.Errorf("zoom missing from the command registry: %v", cmds)
	}
	// The default services carry the real capture chain and store.
	s := defaultServices(Services{})
	if s.Capture == nil || s.Shots == nil || s.FrameOrigin == nil {
		t.Error("CommandsWith must default Capture, Shots and FrameOrigin")
	}
}

func TestSaveShotWritesFileAndDirs(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	path, err := saveShot(img, 101, "screenshot", "jpg", func(w io.Writer) error {
		_, err := w.Write([]byte("jpeg bytes"))
		return err
	})
	if err != nil {
		t.Fatalf("saveShot: %v", err)
	}
	defer os.Remove(path)
	if filepath.Dir(path) != filepath.Join(os.TempDir(), "winuse") {
		t.Errorf("path = %q, want under %%TEMP%%\\winuse", path)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "jpeg bytes" {
		t.Fatalf("read back %q, %v", data, err)
	}
	if !strings.Contains(filepath.Base(path), "winuse-screenshot-101-") {
		t.Errorf("name = %q, want the kind and window id", filepath.Base(path))
	}
}

func TestFormatScale(t *testing.T) {
	cases := []struct {
		scale float64
		want  string
	}{
		{1, "1"},
		{0.5, "0.5"},
		{1568.0 / 3840, "0.408"},
	}
	for _, tc := range cases {
		if got := formatScale(tc.scale); got != tc.want {
			t.Errorf("formatScale(%v) = %q, want %q", tc.scale, got, tc.want)
		}
	}
}

func TestZoomFollowsFreshCaptureGeometry(t *testing.T) {
	// The window moved and grew between the screenshot and the zoom: the
	// region is mapped through the recorded mapping (window space), then
	// clipped to the fresh capture's coverage.
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	// Fresh capture: the window moved to (600, 900) and grew to 1000×600.
	// Window space is unchanged, so the region maps to the same offsets.
	s.frame = syntheticFrame(1000, 600, 600, 900)
	out, cmdErr := runJSON(t, cmds, "zoom", protocol.ZoomArgs{
		Window: "101",
		Region: protocol.Region{X: 400, Y: 235, W: 100, H: 100},
	})
	if cmdErr != nil {
		t.Fatalf("zoom: %v", cmdErr.Error())
	}
	// Region (400,235)+100×100 in screenshot px = physical (400, 235)-(500, 335)
	// in window space — inside the fresh frame.
	if !strings.Contains(out, "(100x100 px)") {
		t.Errorf("crop size wrong:\n%s", out)
	}
}

func TestZoomRegionNoLongerInWindow(t *testing.T) {
	s := newShotSpy(t)
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	// The fresh capture shrank below the requested region: window-space
	// (400, 235)+100x100 falls partly outside a 300×200 window.
	s.frame = syntheticFrame(300, 200, 100, 200)
	_, cmdErr = runJSON(t, cmds, "zoom", protocol.ZoomArgs{
		Window: "101",
		Region: protocol.Region{X: 400, Y: 235, W: 100, H: 100},
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "no longer within the Window") {
		t.Fatalf("want region-outside failure, got %v", cmdErr)
	}
}

// The recorded mapping is per Window: a screenshot of one Window never
// affects another's coordinate space.
func TestMappingsArePerWindow(t *testing.T) {
	s := newShotSpy(t)
	s.svc.Windows = func() ([]win.WindowInfo, error) {
		return append(fakeWindows(), win.WindowInfo{ID: 303, App: "Paint", Title: "Untitled"}), nil
	}
	s.svc.Trees = &fakeTrees{trees: map[uint64]*wstate.Tree{101: cannedTree(), 303: cannedTree()}}
	s.frame = syntheticFrame(800, 470, 100, 200)
	cmds := CommandsWith(s.svc)
	_, cmdErr := runJSON(t, cmds, "get-window-state", windowRef{Window: "101", Screenshot: true})
	if cmdErr != nil {
		t.Fatalf("screenshot: %v", cmdErr.Error())
	}
	if _, ok := s.svc.Shots.Lookup(303); ok {
		t.Fatal("window 303 must have no mapping")
	}
	_, cmdErr = runJSON(t, cmds, "click", protocol.ClickArgs{Window: "Paint", Point: &protocol.Point{X: 1, Y: 1}})
	if cmdErr == nil || !strings.Contains(cmdErr.Error(), "no screenshot is available for window 303") {
		t.Fatalf("want per-window no-screenshot failure, got %v", cmdErr)
	}
}
