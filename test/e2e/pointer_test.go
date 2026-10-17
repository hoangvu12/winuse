//go:build e2e

// The ticket-10 seam-1 tests: `winuse scroll`, `winuse drag` and
// `winuse hover` against the fixture app, on the live desktop. User-run
// only (see the package comment and test/e2e/README.md for the opt-in
// rules); an agent must never start them.
//
// Like the ticket-08 coordinate tests, every aim point is re-derived
// from the fixture's own state file (its canvas rectangle in screen
// pixels) and the printed screenshot scale with independent arithmetic,
// so a bug in winuse's mapping cannot pass them.

package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// runScroll invokes `winuse scroll <window> @x,y <dx> <dy>` with the
// suite's short helper idle timeout.
func runScroll(t *testing.T, window string, x, y, dx, dy int, extraEnv ...string) (string, string, int) {
	t.Helper()
	point := fmt.Sprintf("@%d,%d", x, y)
	return runWinuse(t, []string{"scroll", window, point, strconv.Itoa(dx), strconv.Itoa(dy)},
		append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// runDrag invokes `winuse drag <window> @fx,fy @tx,ty`.
func runDrag(t *testing.T, window string, fx, fy, tx, ty int, extraEnv ...string) (string, string, int) {
	t.Helper()
	from := fmt.Sprintf("@%d,%d", fx, fy)
	to := fmt.Sprintf("@%d,%d", tx, ty)
	return runWinuse(t, []string{"drag", window, from, to}, append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// runHover invokes `winuse hover <window> @x,y`.
func runHover(t *testing.T, window string, x, y int, extraEnv ...string) (string, string, int) {
	t.Helper()
	point := fmt.Sprintf("@%d,%d", x, y)
	return runWinuse(t, []string{"hover", window, point}, append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// shotCoordsOfClient converts a client-space point of the fixture window
// into the latest screenshot's coordinate space: client → screen through
// the canvas's screen rectangle (the fixture reports it live) and the
// canvas's client rect from the Element contract, then screen →
// screenshot through the frame bounds and the printed scale — the
// inverse of winuse's mapping, derived independently here.
func shotCoordsOfClient(t *testing.T, fix *fixtureProc, window string, scale float64, cx, cy int32) (int, int) {
	t.Helper()
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasRect != [4]int32{} })
	cr := st.CanvasRect
	canvasClient, _ := fixture.Rect(fixture.ElemCanvas, 0)
	screenX := cx + (cr[0] - canvasClient.Left)
	screenY := cy + (cr[1] - canvasClient.Top)
	return shotCoordsOfScreen(t, window, scale, screenX, screenY)
}

func TestScrollDownListAtPoint(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// Aim at the list's centre (the fixture's layout: the list occupies
	// client (8, 212)-(472, 332)); scrolling down 360 wheel units is 3
	// notches, and the fixture scrolls one item per notch.
	shotX, shotY := shotCoordsOfClient(t, fix, window, info.scale, 240, 272)
	stdout, stderr, exit := runScroll(t, window, shotX, shotY, 0, 360)
	assertActionOK(t, "scroll list down", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.ListFirstVisible == len(fixture.ListItems)-fixture.ListVisibleItems
	})
	if st.ListFirstVisible != 3 {
		t.Errorf("list scroll position = %d, want 3 (8 items, 5 visible)", st.ListFirstVisible)
	}

	// Scrolling up (a negative amount) scrolls back.
	stdout, stderr, exit = runScroll(t, window, shotX, shotY, 0, -360)
	assertActionOK(t, "scroll list up", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ListFirstVisible == 0 })
}

func TestScrollCanvasHorizontalAndVertical(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// The canvas records every wheel event in canvas coordinates (SendInput
	// semantics: positive x right, positive y up), and only when the wheel
	// lands within it — so this pins both axes and the point together.
	shotX, shotY := shotCoordsOfClient(t, fix, window, info.scale, 636, 232)
	stdout, stderr, exit := runScroll(t, window, shotX, shotY, 240, -240)
	assertActionOK(t, "scroll canvas", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return len(s.CanvasScrolls) >= 2 })
	var hasH, hasV bool
	for _, sc := range st.CanvasScrolls {
		if sc.X == 240 && sc.Y == 0 {
			hasH = true
		}
		if sc.X == 0 && sc.Y == 240 {
			hasV = true // scrollY -240 (up) is a positive wheel delta
		}
	}
	if !hasH {
		t.Errorf("no horizontal scroll of +240 recorded: %v", st.CanvasScrolls)
	}
	if !hasV {
		t.Errorf("no vertical scroll of +240 (up) recorded: %v", st.CanvasScrolls)
	}
}

func TestDragOnCanvas(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// Drag across the canvas from client (530, 80) to (680, 250) —
	// canvas-local (50, 52) to (200, 222), well past the fixture's
	// 4-px minimum so it registers as a drag, not a click.
	fromX, fromY := shotCoordsOfClient(t, fix, window, info.scale, 530, 80)
	toX, toY := shotCoordsOfClient(t, fix, window, info.scale, 680, 250)
	stdout, stderr, exit := runDrag(t, window, fromX, fromY, toX, toY)
	assertActionOK(t, "drag canvas", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return len(s.CanvasDrags) == 1 })
	d := st.CanvasDrags[0]
	if abs32(d.From.X-50) > 1 || abs32(d.From.Y-52) > 1 {
		t.Errorf("drag started at canvas (%d, %d), want (50, 52) within 1 px", d.From.X, d.From.Y)
	}
	if abs32(d.To.X-200) > 1 || abs32(d.To.Y-222) > 1 {
		t.Errorf("drag ended at canvas (%d, %d), want (200, 222) within 1 px", d.To.X, d.To.Y)
	}
}

func TestHoverRevealsHoverOnlyToolbar(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// The hover band under the menu bar (client y 28..56, left of the
	// canvas) hides the toolbar until the pointer enters it.
	shotX, shotY := shotCoordsOfClient(t, fix, window, info.scale, 200, 42)
	stdout, stderr, exit := runHover(t, window, shotX, shotY)
	assertActionOK(t, "hover the toolbar band", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ToolbarVisible })

	// The pointer stays at the point: a fresh Window State (which never
	// moves the pointer) still reports the toolbar, and its Elements are
	// in the tree for clicking.
	stdout, stderr, exit = runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state after hover: exit %d, stderr:\n%s", exit, stderr)
	}
	for _, name := range []string{"Hover Toolbar", "Bold", "Italic", "Underline"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("the hover-only toolbar's %q is not in the Window State after hover:\n%s", name, stdout)
		}
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ToolbarVisible })

	// Hovering elsewhere (the canvas) hides it again.
	shotX, shotY = shotCoordsOfClient(t, fix, window, info.scale, 636, 232)
	stdout, stderr, exit = runHover(t, window, shotX, shotY)
	assertActionOK(t, "hover the canvas", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return !s.ToolbarVisible })
}

func TestScrollRefusedWithoutScreenshot(t *testing.T) {
	optIn(t)
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	window := strconv.FormatUint(st.WindowID, 10)

	// A Window State without --screenshot records no mapping, so there is
	// no coordinate space to aim at.
	if _, _, exit := runGetWindowState(t, window); exit != 0 {
		t.Fatalf("get-window-state: exit %d", exit)
	}
	stdout, stderr, exit := runScroll(t, window, 100, 100, 0, 120)
	assertActionFailed(t, "scroll without a screenshot", stdout, stderr, exit,
		"no screenshot is available for window "+window)
	if !strings.Contains(stderr, "scroll @x,y") {
		t.Errorf("the failure must instruct scrolling @x,y: %q", stderr)
	}
}

func TestPointerRefusalsAfterRestartAndOutside(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	// A helper restart loses the recorded mapping, like @x,y clicks.
	killHelper(t)
	stdout, stderr, exit := runHover(t, window, 100, 100)
	assertActionFailed(t, "hover after helper restart", stdout, stderr, exit,
		"no screenshot is available for window "+window)

	// A fresh screenshot restores the coordinate space...
	if _, _, exit := runGetWindowStateScreenshot(t, window); exit != 0 {
		t.Fatalf("get-window-state --screenshot: exit %d", exit)
	}

	// ...and points outside it are refused with the size.
	stdout, stderr, exit = runDrag(t, window, info.w+50, info.h+50, 10, 10)
	assertActionFailed(t, "drag outside the screenshot", stdout, stderr, exit,
		fmt.Sprintf("point @%d,%d is outside the latest screenshot of window %s (%dx%d px)",
			info.w+50, info.h+50, window, info.w, info.h))

	// A scroll with no amounts is a usage failure, and nothing reaches
	// the app (the fixture's state generation stays put).
	before := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return true })
	stdout, stderr, exit = runScroll(t, window, 100, 100, 0, 0)
	assertActionFailed(t, "scroll with zero amounts", stdout, stderr, exit,
		"scroll: at least one scroll amount must be non-zero")
	time.Sleep(300 * time.Millisecond)
	after := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return true })
	if after.Generation != before.Generation {
		t.Errorf("a refused scroll changed the fixture state (generation %d -> %d): input must not be sent",
			before.Generation, after.Generation)
	}
}
