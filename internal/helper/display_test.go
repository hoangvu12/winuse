package helper

// The displays' Desktop commands (desktop parity ticket 08) over fake
// monitors and captures: the listing, the screenshot line and note, the
// refusals, and the capture's fallback. The screen is never captured.

import (
	"errors"
	"image"
	"image/jpeg"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
)

var testDisplays = []win.Display{
	{Index: 0, Handle: 11, Bounds: win.Rect{Left: 0, Top: 0, Right: 3200, Bottom: 2000}, DPI: 192, Primary: true},
	{Index: 1, Handle: 12, Bounds: win.Rect{Left: -1920, Top: 0, Right: 0, Bottom: 1080}, DPI: 96},
}

// displayServices fakes the monitors and their capture: a solid grey
// frame of the display's size, and records which display was captured.
func displayServices(captured *[]int) Services {
	return Services{
		Trees:    &fakeTrees{},
		Locked:   func() (bool, string) { return false, "" },
		Displays: func() ([]win.Display, error) { return testDisplays, nil },
		CaptureDisplay: func(d win.Display) (shot.Frame, error) {
			*captured = append(*captured, d.Index)
			r := image.Rect(int(d.Bounds.Left), int(d.Bounds.Top), int(d.Bounds.Right), int(d.Bounds.Bottom))
			img := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
			for i := range img.Pix {
				img.Pix[i] = 128
			}
			return shot.Frame{Image: img, Rect: r, Anchor: r.Min}, nil
		},
	}
}

func TestListDisplaysPrintsTheListing(t *testing.T) {
	var captured []int
	out, cmdErr := runCommand(t, CommandsWith(displayServices(&captured)), "list-displays", nil)
	if cmdErr != nil {
		t.Fatal(cmdErr.Msg)
	}
	if want := win.FormatDisplays(testDisplays); out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if !observationOnlyCommand("list-displays") || observationOnlyCommand("screenshot") {
		t.Error("list-displays only reads and stays concurrent; screenshot is serialized")
	}
}

// TestDisplayScreenshot: the line names the display and its rectangle,
// the image is scaled to the long-edge cap like a Window's screenshot, and
// the note says it has no @x,y binding. Nothing is recorded for @x,y.
func TestDisplayScreenshot(t *testing.T) {
	t.Setenv("TMP", t.TempDir())
	t.Setenv("TEMP", os.Getenv("TMP"))
	var captured []int
	s := displayServices(&captured)
	s.Shots = shot.NewStore()
	out, cmdErr := runCommand(t, CommandsWith(s), "screenshot", protocol.ScreenshotArgs{Display: intPtr(1)})
	if cmdErr != nil {
		t.Fatal(cmdErr.Msg)
	}
	line := regexp.MustCompile(`^Screenshot: (.+\.jpg) \(1568x882 px, scale 0.817, display 1 at -1920,0 1920x1080\)\n`)
	m := line.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("out = %q, want the display Screenshot line", out)
	}
	if !strings.HasSuffix(out, displayScreenshotNote) || !strings.Contains(displayScreenshotNote, "no @x,y") {
		t.Errorf("out = %q, want the no-binding note last", out)
	}
	f, err := os.Open(m[1])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1568 || b.Dy() != 882 {
		t.Errorf("image %dx%d, want the 1568-px long edge (1568x882)", b.Dx(), b.Dy())
	}
	if len(captured) != 1 || captured[0] != 1 {
		t.Errorf("captured %v, want display 1", captured)
	}
	for _, id := range []uint64{0, 1} {
		if _, ok := s.Shots.Lookup(agent.DefaultID, id); ok {
			t.Errorf("a display screenshot recorded an @x,y mapping under %d", id)
		}
	}

	out, cmdErr = runCommand(t, CommandsWith(s), "screenshot", protocol.ScreenshotArgs{Display: intPtr(0)})
	if cmdErr != nil || !strings.Contains(out, "px, scale 0.49, display 0 at 0,0 3200x2000)") {
		t.Errorf("display 0: %q, %v", out, cmdErr)
	}
}

func TestDisplayScreenshotRefusals(t *testing.T) {
	var captured []int
	s := displayServices(&captured)
	for _, tc := range []struct {
		args protocol.ScreenshotArgs
		msg  string
	}{
		{protocol.ScreenshotArgs{}, "screenshot takes --display <n>"},
		{protocol.ScreenshotArgs{Display: intPtr(2)}, "there is no display 2; the desktop has 2 (0 to 1)"},
		{protocol.ScreenshotArgs{Display: intPtr(-1)}, "there is no display -1"},
	} {
		_, cmdErr := runCommand(t, CommandsWith(s), "screenshot", tc.args)
		if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.msg) {
			t.Errorf("%+v: %v, want %q", tc.args, cmdErr, tc.msg)
		}
	}
	s.Locked = func() (bool, string) { return true, "Winlogon" }
	if _, cmdErr := runCommand(t, CommandsWith(s), "screenshot", protocol.ScreenshotArgs{Display: intPtr(0)}); cmdErr == nil || !strings.Contains(cmdErr.Msg, "locked") {
		t.Errorf("a locked desktop: %v, want the locked refusal", cmdErr)
	}
	if len(captured) != 0 {
		t.Errorf("a refused screenshot captured %v", captured)
	}
}

// fakeMonitor is the Windows.Graphics.Capture half of a display capture.
type fakeMonitor struct {
	frame shot.Frame
	err   error
	calls []uintptr
}

func (f *fakeMonitor) CaptureMonitor(hmon uintptr, bounds image.Rectangle) (shot.Frame, error) {
	f.calls = append(f.calls, hmon)
	return f.frame, f.err
}

// TestDisplayCaptureFallsBackToTheScreenRegion: Windows.Graphics.Capture
// first; a failure or a blank frame falls back to the screen region; both
// failing names both. The indicator and the cursor are hidden around it.
func TestDisplayCaptureFallsBackToTheScreenRegion(t *testing.T) {
	d := testDisplays[1]
	bounds := image.Rect(-1920, 0, 0, 1080)
	solid := func(c uint8) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		for i := range img.Pix {
			img.Pix[i] = c
		}
		return img
	}
	regionCalls := 0
	region := func(r win.Rect) (*image.RGBA, error) {
		regionCalls++
		if r != d.Bounds {
			t.Errorf("region %+v, want the display's %+v", r, d.Bounds)
		}
		return solid(200), nil
	}

	// WGC works: no region capture.
	wgcFrame := shot.Frame{Image: solid(90), Rect: bounds, Anchor: bounds.Min}
	m := &fakeMonitor{frame: wgcFrame}
	f, err := displayCaptureWith(nil, nil, m, region)(d)
	if err != nil || f.Image.Pix[0] != 90 || regionCalls != 0 || len(m.calls) != 1 || m.calls[0] != d.Handle {
		t.Errorf("WGC path: %v, pix %d, region calls %d, monitor calls %v", err, f.Image.Pix[0], regionCalls, m.calls)
	}

	// WGC fails, then blank: the region capture, anchored at the display.
	for _, m := range []*fakeMonitor{{err: errors.New("unsupported")}, {frame: shot.Frame{Image: solid(0), Rect: bounds}}} {
		regionCalls = 0
		f, err := displayCaptureWith(nil, nil, m, region)(d)
		if err != nil || regionCalls != 1 || f.Image.Pix[0] != 200 || f.Rect != bounds || f.Anchor != bounds.Min {
			t.Errorf("fallback: %v, region calls %d, frame %+v", err, regionCalls, f.Rect)
		}
	}

	// Both fail.
	_, err = displayCaptureWith(nil, nil, &fakeMonitor{err: errors.New("no WGC")}, func(win.Rect) (*image.RGBA, error) {
		return nil, errors.New("BitBlt failed")
	})(d)
	if err == nil || !strings.Contains(err.Error(), "no WGC") || !strings.Contains(err.Error(), "BitBlt failed") {
		t.Errorf("both failing: %v", err)
	}

	// Hidden: the capture runs inside the cursor's and the overlay's
	// HideWhile.
	o, c := &hideCounter{}, &hideCounter{}
	inside := false
	m = &fakeMonitor{frame: wgcFrame}
	displayCaptureWith(o, c, monitorFunc(func() { inside = o.depth == 1 && c.depth == 1 }, m), region)(d)
	if !inside {
		t.Error("the capture did not run with the indicator and the cursor hidden")
	}
}

func intPtr(n int) *int { return &n }

// hideCounter counts how deep a HideWhile is open.
type hideCounter struct{ depth int }

func (h *hideCounter) HideWhile(f func()) { h.depth++; f(); h.depth-- }

// monitorProbe runs probe at capture time, then defers to m.
type monitorProbe struct {
	probe func()
	m     *fakeMonitor
}

func monitorFunc(probe func(), m *fakeMonitor) monitorProbe { return monitorProbe{probe, m} }

func (p monitorProbe) CaptureMonitor(hmon uintptr, bounds image.Rectangle) (shot.Frame, error) {
	p.probe()
	return p.m.CaptureMonitor(hmon, bounds)
}
