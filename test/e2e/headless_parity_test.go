//go:build e2e

// The headless-parity tests (ADR 0011, .scratch/headless-parity/): the
// Actions that ticket 01-07 made Headless, against the fixture app (and
// Notepad and Chrome where noted) with another Window holding the
// foreground. User-run only (see the package comment and
// test/e2e/README.md for the opt-in rules); an agent must never start
// them.
//
// This file holds the shared helpers and the capture and set-value tests.
// The others are window_actions_test.go, element_scroll_test.go,
// classic_keys_test.go and posted_keys_test.go.
//
// Every test covers its target the way headless_test.go does: a second
// fixture instance over the target, brought to the foreground with
// `winuse activate-window`. A Headless Action passes when it took effect
// (read back from the fixture's state file or the desktop) and the
// covering Window is still the foreground window afterwards.

package e2e

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// selectedItemColour is the fixture's selected-list-item fill as RGB: only
// the target fixture has a selected item, so a capture that contains it
// shows the target, not the fixture covering it.
var selectedItemColour = color.RGBA{
	R: uint8(fixture.SelectedItemCOLORREF & 0xFF), G: uint8(fixture.SelectedItemCOLORREF >> 8 & 0xFF),
	B: uint8(fixture.SelectedItemCOLORREF >> 16 & 0xFF), A: 0xFF,
}

// postedOK is the start of a posted-key Action's output: posting has no
// readable outcome, so the line says so instead of a plain ok.
const postedOK = "ok (posted;"

// coverAgain brings the covering Window back to the foreground, after
// something the test did may have raised a fixture window (a dialog the
// target opened).
func coverAgain(t *testing.T, cover uint64) {
	t.Helper()
	stdout, stderr, exit := runActivateWindow(t, strconv.FormatUint(cover, 10))
	assertActionOK(t, "activate-window (the covering Window)", stdout, stderr, exit)
	waitForeground(t, cover, 5*time.Second)
	waitOverlayGone(t, 5*time.Second)
}

// assertForeground checks that the covering Window is still the
// foreground window after a Headless Action.
func assertForeground(t *testing.T, what string, cover uint64) {
	t.Helper()
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("%s: the foreground window is %s, want the covering Window %d", what, describeWindow(t, fg), cover)
	}
}

// assertAbove checks that upper lies above lower in the desktop's z-order
// (win.Collect lists topmost first).
func assertAbove(t *testing.T, upper, lower uint64) {
	t.Helper()
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	upperZ, lowerZ := -1, -1
	for i, w := range all {
		switch w.ID {
		case upper:
			upperZ = i
		case lower:
			lowerZ = i
		}
	}
	if upperZ < 0 || lowerZ < 0 || upperZ > lowerZ {
		t.Fatalf("Window %d is not above %s in z-order (at %d and %d)", upper, describeWindow(t, lower), upperZ, lowerZ)
	}
}

// findWindowOf polls the desktop's window list until a visible window of
// the process with the class and title (any non-empty title when title is
// "") shows up, and returns its ID.
func findWindowOf(t *testing.T, pid uint32, class, title string, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		all, err := win.Collect()
		if err != nil {
			t.Fatalf("enumerating windows: %v", err)
		}
		for _, w := range all {
			titled := w.Title == title || (title == "" && w.Title != "")
			if w.PID == pid && w.Visible && w.Class == class && titled {
				return w.ID
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no visible %q window titled %q of process %d appeared within %s", class, title, pid, timeout)
	return 0
}

// logKeyTarget logs what winuse's keyboard routing reads off a Window
// (read-only: classes, styles, DM_GETDEFID). When a key or text Action
// finds no Headless route (and is refused without --headed, headless-max
// ticket 21), this line says why.
func logKeyTarget(t *testing.T, id uint64) {
	t.Helper()
	k := win.ProbeKeyTarget(id)
	t.Logf("key route probe of Window %d: dialog %v, default button %v (id %d, %q), Cancel %q, focus %q (style 0x%x, enabled %v)",
		id, k.Dialog, k.HasDefault, k.DefaultID, k.Default.Text, k.Cancel.Text, k.Focus.Class, k.Focus.Style, k.Focus.Enabled)
}

// imageVaries reports whether an image has real content: its brightest
// and darkest pixels are far apart (a blank or black capture is flat).
func imageVaries(img image.Image) bool {
	b := img.Bounds()
	lo, hi := 255, 0
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			l := int((r>>8)*299+(g>>8)*587+(bl>>8)*114) / 1000
			if l < lo {
				lo = l
			}
			if l > hi {
				hi = l
			}
		}
	}
	return hi-lo > 64
}

func TestHeadlessScreenshotAndZoomOfCoveredWindow(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	indexes := mainElementIndexes()

	// Select a list item in the target only: its fill colour then tells a
	// capture of the target from a capture of the cover, which looks
	// otherwise the same.
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(indexes[fixture.ElemItemAlpha])})
	assertActionOK(t, "Headless click (select Alpha)", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.SelectedItem == fixture.Name(fixture.ElemItemAlpha, "") })
	assertCovered(t, fix, window, fixture.ElemItemAlpha, cover)
	pointer := pointerPos(t)

	res, overlaySeen := runWatched(t, []string{"get-window-state", window, "--screenshot"})
	if res.exit != 0 {
		t.Fatalf("get-window-state --screenshot of the covered Window: exit %d, stderr:\n%s", res.exit, res.stderr)
	}
	assertForeground(t, "get-window-state --screenshot", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during a screenshot")
	}
	if got := pointerPos(t); got != pointer {
		t.Errorf("the real pointer moved from %v to %v during a screenshot", pointer, got)
	}
	info := parseScreenshotLine(t, res.stdout)
	img := decodeImage(t, info.path)
	if !imageVaries(img) {
		t.Errorf("the screenshot %s is blank (flat colour)", info.path)
	}
	if n := countColourPixels(img, selectedItemColour); n < 100 {
		t.Errorf("the screenshot shows %d pixels of the selected item's fill, want the target's selected list item (a capture of the covering Window has none)", n)
	}

	// zoom crops the selected item from a fresh headless capture.
	cx, cy := elementScreenCentre(t, fix, fixture.ElemItemAlpha)
	sx, sy := shotCoordsOfScreen(t, window, info.scale, cx, cy)
	res, overlaySeen = runWatched(t, []string{"zoom", window, strconv.Itoa(sx-60) + "," + strconv.Itoa(sy-10) + ",120,20"})
	if res.exit != 0 {
		t.Fatalf("zoom of the covered Window: exit %d, stderr:\n%s", res.exit, res.stderr)
	}
	assertForeground(t, "zoom", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during a zoom")
	}
	path, _, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(res.stdout), "Zoom: "), " (")
	if !ok || !strings.HasPrefix(res.stdout, "Zoom: ") {
		t.Fatalf("no parsable Zoom line in %q", res.stdout)
	}
	crop := decodeImage(t, path)
	if n := countColourPixels(crop, selectedItemColour); n < 50 {
		t.Errorf("the zoom crop shows %d pixels of the selected item's fill, want the target's selected list item", n)
	}
}

func TestHeadlessScreenshotRefusesMinimizedWindow(t *testing.T) {
	optIn(t)
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	window := strconv.FormatUint(st.WindowID, 10)

	// A screenshot first, so zoom has a mapping and fails on the minimized
	// Window rather than on a missing screenshot.
	if _, stderr, exit := runGetWindowStateScreenshot(t, window); exit != 0 {
		t.Fatalf("get-window-state --screenshot: exit %d, stderr:\n%s", exit, stderr)
	}
	stdout, stderr, exit := runWinuse(t, []string{"minimize-window", window}, "WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "minimize-window", stdout, stderr, exit)
	fg := win.ForegroundWindow()

	stdout, stderr, exit = runGetWindowStateScreenshot(t, window)
	assertActionFailed(t, "screenshot of a minimized Window", stdout, stderr, exit, "is minimized; there is nothing on screen to capture")
	if !strings.Contains(stderr, "winuse restore-window "+window) {
		t.Errorf("the refusal must name restore-window: %q", stderr)
	}
	stdout, stderr, exit = runZoom(t, window, 10, 10, 100, 100)
	assertActionFailed(t, "zoom of a minimized Window", stdout, stderr, exit, "is minimized")

	// Nothing activated or restored the Window.
	if !win.IsMinimized(st.WindowID) {
		t.Error("the refused capture restored the Window")
	}
	if got := win.ForegroundWindow(); got != fg {
		t.Errorf("the foreground window changed from %d to %s on a refused capture", fg, describeWindow(t, got))
	}
}

func TestSetValueOnOffTheCheckbox(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	idx := mainElementIndexes()[fixture.ElemCheckboxNotify]
	assertCovered(t, fix, window, fixture.ElemCheckboxNotify, cover)

	steps := []struct {
		value string
		want  bool
		noop  bool // the box is in the target state already: nothing is sent
	}{
		{"on", true, false},
		{"ON", true, true},
		{"checked", true, true},
		{"off", false, false},
		{"unchecked", false, true},
		{"True", true, false},
		{"false", false, false},
	}
	for _, s := range steps {
		before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
		stdout, stderr, exit := runSetvalue(t, window, idx, s.value)
		assertActionOK(t, "set-value "+s.value, stdout, stderr, exit)
		st := fix.waitState(t, 5*time.Second, func(st *fixture.State) bool { return st.Checkbox == s.want })
		if s.noop {
			// Let any stray toggle land before comparing.
			time.Sleep(300 * time.Millisecond)
			st = fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
			if st.Generation != before.Generation || st.Checkbox != s.want {
				t.Errorf("set-value %s on a box already %v sent something: generation %d -> %d, checkbox %v",
					s.value, s.want, before.Generation, st.Generation, st.Checkbox)
			}
		}
		assertForeground(t, "set-value "+s.value, cover)
	}

	// A word that is not a toggle state is refused before anything is sent.
	before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
	stdout, stderr, exit := runSetvalue(t, window, idx, "maybe")
	if exit == 0 || stdout != "" {
		t.Fatalf("set-value maybe on the checkbox: exit %d, stdout %q", exit, stdout)
	}
	if !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("the refusal must say nothing was sent: %q", stderr)
	}
	if st := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.Generation != before.Generation {
		t.Errorf("a refused set-value changed the fixture: generation %d -> %d", before.Generation, st.Generation)
	}
}
