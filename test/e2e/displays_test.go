//go:build e2e

// The display tests (desktop-parity ticket 08): list-displays against the
// system's own monitor metrics, and screenshot --display 0 of the primary
// display, through the real winuse binary. Nothing is launched and no
// input is sent; the capture leaves the foreground where it is.

package e2e

import (
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/win"
)

var procE2EGetSystemMetrics = e2eUser32.NewProc("GetSystemMetrics")

const (
	smCXScreen  = 0
	smCYScreen  = 1
	smCMonitors = 80
)

func systemMetric(index int) int {
	r, _, _ := procE2EGetSystemMetrics.Call(uintptr(index))
	return int(int32(r))
}

var displayLine = regexp.MustCompile(`^(\d+)\t(-?\d+),(-?\d+) (\d+)x(\d+)\tscale ([0-9.]+)(\tprimary)?$`)

func TestListDisplaysAndCapturePrimary(t *testing.T) {
	optIn(t)
	// The metrics below in physical pixels, as the helper reads them.
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		t.Logf("per-monitor DPI awareness: %v", err)
	}

	stdout, stderr, exit := runWinuse(t, []string{"list-displays"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("list-displays: exit %d, stderr %q", exit, stderr)
	}
	t.Logf("list-displays:\n%s", stdout)
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if want := systemMetric(smCMonitors); len(lines) != want {
		t.Errorf("%d displays listed, GetSystemMetrics(SM_CMONITORS) says %d", len(lines), want)
	}
	primaries := 0
	for i, l := range lines {
		m := displayLine.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("line %d %q does not match \"<n>\\t<x>,<y> <w>x<h>\\tscale <s>[\\tprimary]\"", i, l)
		}
		if m[1] != strconv.Itoa(i) {
			t.Errorf("line %d is numbered %s", i, m[1])
		}
		if scale, _ := strconv.ParseFloat(m[6], 64); scale < 1 || scale > 5 {
			t.Errorf("line %d: scale %s", i, m[6])
		}
		if m[7] != "" {
			primaries++
			if i != 0 {
				t.Errorf("the primary display is line %d, want 0", i)
			}
		}
	}
	if primaries != 1 {
		t.Errorf("%d primary displays, want 1", primaries)
	}
	m := displayLine.FindStringSubmatch(lines[0])
	w, h := systemMetric(smCXScreen), systemMetric(smCYScreen)
	if m[2] != "0" || m[3] != "0" || m[4] != strconv.Itoa(w) || m[5] != strconv.Itoa(h) {
		t.Errorf("display 0 is at %s,%s %sx%s; the primary screen is 0,0 %dx%d", m[2], m[3], m[4], m[5], w, h)
	}

	fg := win.ForegroundWindow()
	stdout, stderr, exit, log := runWinuseLogged(t, []string{"screenshot", "--display", "0"})
	if exit != 0 {
		t.Fatalf("screenshot --display 0: exit %d, stderr %q", exit, stderr)
	}
	t.Logf("screenshot --display 0:\n%s", stdout)
	shotLine := regexp.MustCompile(fmt.Sprintf(`^Screenshot: (.+\.jpg) \((\d+)x(\d+) px, scale ([0-9.]+), display 0 at 0,0 %dx%d\)\nNote: a display screenshot has no @x,y points`, w, h))
	sm := shotLine.FindStringSubmatch(stdout)
	if sm == nil {
		t.Fatalf("output %q does not match the display Screenshot line and note", stdout)
	}
	f, err := os.Open(sm[1])
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(f)
	f.Close()
	if err != nil {
		t.Fatalf("decoding %s: %v", sm[1], err)
	}
	if b := img.Bounds(); strconv.Itoa(b.Dx()) != sm[2] || strconv.Itoa(b.Dy()) != sm[3] {
		t.Errorf("the image is %dx%d, the line says %sx%s", b.Dx(), b.Dy(), sm[2], sm[3])
	}
	if b := img.Bounds(); b.Dx() > 1568 || b.Dy() > 1568 {
		t.Errorf("the image is %dx%d; its long edge must be at most 1568", b.Dx(), b.Dy())
	}
	if uniform(img) {
		t.Error("the display screenshot is one colour; the capture came out blank")
	}
	switch {
	case strings.Contains(log, "wgc: captured display at 0,0"):
		t.Log("captured through Windows.Graphics.Capture")
	case wgcUnavailable(t, log):
		t.Log("Windows.Graphics.Capture is not available here; captured through the screen region")
	default:
		t.Errorf("the helper log shows no Windows.Graphics.Capture display capture:\n%s", log)
	}
	if now := win.ForegroundWindow(); now != fg {
		t.Errorf("the foreground moved from %d to %d; a display screenshot needs no foreground", fg, now)
	}
	os.Remove(sm[1])

	_, stderr, exit = runWinuse(t, []string{"screenshot", "--display", strconv.Itoa(len(lines))}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 1 || !strings.Contains(stderr, "there is no display "+strconv.Itoa(len(lines))) || !strings.Contains(stderr, "winuse list-displays") {
		t.Errorf("a display past the end: exit %d, stderr %q", exit, stderr)
	}
}

// TestDisplayScreenshotFallsBackToTheScreenRegion: with
// Windows.Graphics.Capture switched off (WINUSE_WGC=off, read by a fresh
// helper), the display is captured through its screen region.
func TestDisplayScreenshotFallsBackToTheScreenRegion(t *testing.T) {
	optIn(t)
	killHelper(t)
	t.Cleanup(func() { killHelper(t) }) // the next test's helper has Windows.Graphics.Capture again
	stdout, stderr, exit, log := runWinuseLogged(t, []string{"screenshot", "--display", "0"}, "WINUSE_WGC=off")
	if exit != 0 {
		t.Fatalf("screenshot --display 0 without WGC: exit %d, stderr %q", exit, stderr)
	}
	if !strings.Contains(log, "wgc: display at 0,0") || !strings.Contains(log, "fell back: Windows.Graphics.Capture is disabled") {
		t.Errorf("the helper log shows no disabled-WGC fallback for the display:\n%s", log)
	}
	sm := regexp.MustCompile(`^Screenshot: (.+\.jpg) \(`).FindStringSubmatch(stdout)
	if sm == nil {
		t.Fatalf("output %q has no Screenshot line", stdout)
	}
	f, err := os.Open(sm[1])
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(f)
	f.Close()
	if err != nil {
		t.Fatalf("decoding %s: %v", sm[1], err)
	}
	if uniform(img) {
		t.Error("the screen-region capture is one colour")
	}
	os.Remove(sm[1])
}

// uniform reports whether every sampled pixel of img has the same colour.
func uniform(img image.Image) bool {
	b := img.Bounds()
	r0, g0, b0, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += 7 {
		for x := b.Min.X; x < b.Max.X; x += 7 {
			r, g, bb, _ := img.At(x, y).RGBA()
			if r != r0 || g != g0 || bb != b0 {
				return false
			}
		}
	}
	return true
}
