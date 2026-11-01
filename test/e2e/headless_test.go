//go:build e2e

// The headless-feature seam-1 tests (ADR 0010, .scratch/headless/): Headless
// Element Actions, --headed and the Agent Cursor against the fixture app, on
// the live desktop. User-run only (see the package comment and
// test/e2e/README.md for the opt-in rules); an agent must never start them.
//
// A Headless Action works through UI Automation patterns alone: it must
// change the fixture's state while another Window covers the fixture,
// leave the foreground Window and the real pointer where they were, never
// raise the in-control banner, and never focus the Element (a Headed
// set-value focuses first; a Headless one must not — the fixture counts the
// UI Automation SetFocus calls its provider receives). An Element with no
// pattern route fails with nothing sent and the --headed hint; --headed
// gives v1's behaviour, the fixture coming to the foreground. The Agent
// Cursor is a set of visible top-level windows of class winuse.cursor
// owned by the helper, shown at the Element, and absent from winuse's own
// screenshots.
//
// The covering Window is a second fixture instance placed over the first
// (-origin) and brought to the foreground with `winuse activate-window`.
// Geometry is read through DWM frame bounds and the fixture's own state
// file (physical pixels), and coverage through the desktop's z-order, so
// the checks hold at any display scaling.

package e2e

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// noClickRouteMessage is the failure line of a Headless click on an Element
// that offers no pattern route (restated so the test asserts the observable
// output, not a shared constant).
const noClickRouteMessage = "cannot be clicked headless"

var procGetCursorPos = user32.NewProc("GetCursorPos")

// pointerPos reads the real pointer's position (read-only). The coordinate
// space does not matter: the tests only compare two readings.
func pointerPos(t *testing.T) [2]int32 {
	t.Helper()
	var p [2]int32
	if r, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p[0]))); r == 0 {
		t.Fatalf("reading the pointer position: %v", err)
	}
	return p
}

// elementScreenCentre is the screen-space (physical) centre of a main-window
// Element: its client rectangle from the Element contract, moved to the
// screen through the canvas rectangle the fixture reports live (as
// shotCoordsOfClient derives it).
func elementScreenCentre(t *testing.T, fix *fixtureProc, e fixture.Elem) (int32, int32) {
	t.Helper()
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasRect != [4]int32{} })
	cr := st.CanvasRect
	canvasClient, _ := fixture.Rect(fixture.ElemCanvas, 0)
	r, ok := fixture.Rect(e, st.ListFirstVisible)
	if !ok {
		t.Fatalf("the fixture contract has no rectangle for %s", e)
	}
	dx, dy := cr[0]-canvasClient.Left, cr[1]-canvasClient.Top
	return (r.Left+r.Right)/2 + dx, (r.Top+r.Bottom)/2 + dy
}

// describeWindow names a top-level window for a failure message: its ID,
// class and title as the desktop's window list reports them.
func describeWindow(t *testing.T, id uint64) string {
	t.Helper()
	all, err := win.Collect()
	if err == nil {
		for _, w := range all {
			if w.ID == id {
				return fmt.Sprintf("%d (class %q, title %q)", id, w.Class, w.Title)
			}
		}
	}
	return fmt.Sprintf("%d (not in the window list)", id)
}

// waitForeground polls until the Window is the foreground window.
func waitForeground(t *testing.T, id uint64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if win.ForegroundWindow() == id {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Window %d did not become the foreground window within %s (foreground: %d)", id, timeout, win.ForegroundWindow())
}

// coverWindow starts a second fixture instance over the target Window and
// brings it to the foreground with `winuse activate-window` (a Headed
// Action), so the target sits behind the user's "own" Window. It returns
// the covering Window's ID; the instance is killed on cleanup.
func coverWindow(t *testing.T, target string) uint64 {
	t.Helper()
	fb, err := win.FrameBounds(mustWindowID(t, target))
	if err != nil {
		t.Fatalf("reading the target window's frame bounds: %v", err)
	}
	statePath := filepath.Join(t.TempDir(), "cover-state.json")
	cmd := exec.Command(fixtureBinary(t), "-state", statePath, "-origin", fmt.Sprintf("%d,%d", fb.Left, fb.Top))
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the covering fixture: %v", err)
	}
	proc := cmd.Process
	t.Cleanup(func() {
		_ = proc.Kill()
		_, _ = proc.Wait()
	})
	st := waitStateFile(t, statePath, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	cover := strconv.FormatUint(st.WindowID, 10)

	stdout, stderr, exit := runActivateWindow(t, cover)
	assertActionOK(t, "activate-window (the covering Window)", stdout, stderr, exit)
	waitForeground(t, st.WindowID, 5*time.Second)
	waitOverlayGone(t, 5*time.Second) // activate-window is Headed: its banner must be down before the Headless Action
	return st.WindowID
}

// assertCovered checks that the covering Window lies over the Element's
// centre and above the target Window in the desktop's z-order (win.Collect
// lists topmost first) — the Element is not reachable by a real click.
func assertCovered(t *testing.T, fix *fixtureProc, target string, e fixture.Elem, cover uint64) {
	t.Helper()
	x, y := elementScreenCentre(t, fix, e)
	cb, err := win.FrameBounds(cover)
	if err != nil {
		t.Fatalf("reading the covering window's frame bounds: %v", err)
	}
	if x < cb.Left || x >= cb.Right || y < cb.Top || y >= cb.Bottom {
		t.Fatalf("the covering Window %+v does not lie over %s at (%d, %d)", cb, e, x, y)
	}
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	coverZ, targetZ := -1, -1
	for i, w := range all {
		switch w.ID {
		case cover:
			coverZ = i
		case mustWindowID(t, target):
			targetZ = i
		}
	}
	if coverZ < 0 || targetZ < 0 || coverZ > targetZ {
		t.Fatalf("the covering Window is not above the target in z-order (cover at %d, target at %d)", coverZ, targetZ)
	}
}

// runWatched runs `winuse <args>` in the background and samples the
// desktop while it runs: whether the in-control overlay was ever shown.
func runWatched(t *testing.T, args []string) (res actionResult, overlaySeen bool) {
	t.Helper()
	bg := runActionAsync(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case res = <-bg:
			return res, overlaySeen
		default:
		}
		if _, ok := overlayWindow(t); ok {
			overlaySeen = true
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("winuse %v did not finish within 30 s", args)
	return res, overlaySeen
}

// agentCursorWindows returns the shown Agent Cursor windows (class
// winuse.cursor) from the desktop's window list — read-only.
func agentCursorWindows(t *testing.T) []win.WindowInfo {
	t.Helper()
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	var out []win.WindowInfo
	for _, w := range all {
		if w.Class == win.AgentCursorClassName {
			out = append(out, w)
		}
	}
	return out
}

func TestHeadlessClickUnderCoveringWindow(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	indexes := mainElementIndexes()

	cases := []struct {
		name string
		elem fixture.Elem
		done func(*fixture.State) bool
	}{
		{"button", fixture.ElemButtonClick, func(s *fixture.State) bool { return s.LastButton == "Click Me" }},
		{"checkbox", fixture.ElemCheckboxNotify, func(s *fixture.State) bool { return s.Checkbox }},
	}
	for _, c := range cases {
		assertCovered(t, fix, window, c.elem, cover)
		pointer := pointerPos(t)

		res, overlaySeen := runWatched(t, []string{"click", window, strconv.Itoa(indexes[c.elem])})
		assertActionOK(t, "Headless click ("+c.name+")", res.stdout, res.stderr, res.exit)
		fix.waitState(t, 5*time.Second, c.done)

		if overlaySeen {
			t.Errorf("%s: the in-control banner was shown during a Headless click", c.name)
		}
		if fg := win.ForegroundWindow(); fg != cover {
			t.Errorf("%s: the foreground window is %s after a Headless click, want the covering Window %d (target %s)",
				c.name, describeWindow(t, fg), cover, window)
		}
		if got := pointerPos(t); got != pointer {
			t.Errorf("%s: the real pointer moved from %v to %v during a Headless click", c.name, pointer, got)
		}
	}
}

func TestHeadlessSetValueKeepsFocusAndForeground(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	idx := mainElementIndexes()[fixture.ElemTextField]
	assertCovered(t, fix, window, fixture.ElemTextField, cover)
	before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })

	stdout, stderr, exit := runSetvalue(t, window, idx, "set headless")
	assertActionOK(t, "Headless set-value", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "set headless" })
	if st.FocusRequests != before.FocusRequests {
		t.Errorf("a Headless set-value focused the Element: %d UI Automation SetFocus calls, want none",
			st.FocusRequests-before.FocusRequests)
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground window is %s after a Headless set-value, want the covering Window %d (target %s)",
			describeWindow(t, fg), cover, window)
	}

	// The focus counter is live: a Headed set-value focuses its Element
	// first (v1's order) and takes the foreground — so the zero above is a
	// real observation, not a counter that never moves.
	stdout, stderr, exit = runWinuse(t, []string{"--headed", "set-value", window, strconv.Itoa(idx), "set headed"},
		"WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "--headed set-value", stdout, stderr, exit)
	st = fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "set headed" })
	if st.FocusRequests <= before.FocusRequests {
		t.Errorf("a Headed set-value did not reach the provider's SetFocus (focusRequests %d); the Headless check above proves nothing",
			st.FocusRequests)
	}
	waitForeground(t, mustWindowID(t, window), 2*time.Second)
}

func TestHeadlessClickWithoutPatternRouteFails(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// The canvas offers no Invoke, Toggle, SelectionItem, ExpandCollapse
	// or LegacyIAccessible default action (Element contract: no patterns),
	// so a Headless click has no route: it fails before any pattern call.
	idx := mainElementIndexes()[fixture.ElemCanvas]
	before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
	fg := win.ForegroundWindow()

	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionFailed(t, "Headless click on the canvas", stdout, stderr, exit, noClickRouteMessage)
	if exit != 1 {
		t.Errorf("exit %d, want 1 (a command failure)", exit)
	}
	if !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("the failure must say nothing was sent (retry is safe): %q", stderr)
	}
	if hint := "winuse --headed click " + window + " " + strconv.Itoa(idx); !strings.Contains(stderr, hint) {
		t.Errorf("the failure must name the --headed retry %q: %q", hint, stderr)
	}

	// Nothing reached the app, and the foreground did not move.
	st := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
	if len(st.CanvasClicks) != 0 || st.Generation != before.Generation {
		t.Errorf("input reached the app: canvasClicks %v, generation %d → %d", st.CanvasClicks, before.Generation, st.Generation)
	}
	if got := win.ForegroundWindow(); got != fg {
		t.Errorf("the foreground window changed from %d to %d on a failed Headless click", fg, got)
	}
}

func TestHeadedClickBringsTheWindowForward(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	id := mustWindowID(t, window)
	cover := coverWindow(t, window)
	indexes := mainElementIndexes()

	// --headed is global: before the command and after it both force v1's
	// pipeline (activate, real pointer, banner).
	cases := []struct {
		name string
		args []string
		done func(*fixture.State) bool
	}{
		{"--headed before the command", []string{"--headed", "click", window, strconv.Itoa(indexes[fixture.ElemButtonClick])},
			func(s *fixture.State) bool { return s.LastButton == "Click Me" }},
		{"--headed after the command", []string{"click", window, strconv.Itoa(indexes[fixture.ElemCheckboxNotify]), "--headed"},
			func(s *fixture.State) bool { return s.Checkbox }},
	}
	for i, c := range cases {
		if i > 0 {
			// Put the covering Window back in front.
			stdout, stderr, exit := runActivateWindow(t, strconv.FormatUint(cover, 10))
			assertActionOK(t, "activate-window (the covering Window)", stdout, stderr, exit)
			waitForeground(t, cover, 5*time.Second)
		}
		stdout, stderr, exit := runWinuse(t, c.args, "WINUSE_HELPER_IDLE="+e2eIdle)
		assertActionOK(t, c.name, stdout, stderr, exit)
		fix.waitState(t, 5*time.Second, c.done)
		waitForeground(t, id, 2*time.Second)
	}
}

func TestAgentCursorShownAndExcludedFromCaptures(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	idx := mainElementIndexes()[fixture.ElemButtonClick]
	cx, cy := elementScreenCentre(t, fix, fixture.ElemButtonClick)
	fg := win.ForegroundWindow()

	// The cursor glides to the Element before the pattern call and stays
	// shown until about 6 s after the last Action, so it is visible both
	// during the click and right after it.
	bg := runActionAsync(t, []string{"click", window, strconv.Itoa(idx)}, "WINUSE_HELPER_IDLE="+e2eIdle)
	var res actionResult
	finished := false
	var during []win.WindowInfo
	deadline := time.Now().Add(15 * time.Second)
	for !finished && time.Now().Before(deadline) {
		select {
		case res = <-bg:
			finished = true
		default:
			if ws := agentCursorWindows(t); len(ws) > 0 {
				during = ws
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !finished {
		t.Fatal("the Headless click did not finish within 15 s")
	}
	clicked := time.Now()
	assertActionOK(t, "Headless click with the Agent Cursor", res.stdout, res.stderr, res.exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastButton == "Click Me" })

	after := agentCursorWindows(t)
	if len(after) == 0 {
		t.Fatalf("no visible %s window right after a Headless click (seen during it: %d)", win.AgentCursorClassName, len(during))
	}

	// It is the helper's, presentation only (click-through, topmost,
	// never activated, not in Alt-Tab), and it sits at the Element.
	helper := helperPID(t)
	const slack = 4
	atElement := false
	for _, w := range after {
		if w.PID != helper {
			t.Errorf("cursor window %d belongs to process %d, want the helper %d", w.ID, w.PID, helper)
		}
		for _, s := range []struct {
			bit  uint32
			name string
		}{{exLayered, "layered"}, {exTransparent, "click-through"}, {exTopmost, "topmost"}, {exToolWindow, "tool-window"}, {exNoActivate, "no-activate"}} {
			if w.ExStyle&s.bit == 0 {
				t.Errorf("cursor window %d's extended style 0x%x is not %s", w.ID, w.ExStyle, s.name)
			}
		}
		b := w.Bounds
		if cx >= b.Left-slack && cx <= b.Right+slack && cy >= b.Top-slack && cy <= b.Bottom+slack {
			atElement = true
		}
	}
	if !atElement {
		t.Errorf("no cursor window lies at the button's centre (%d, %d): %+v", cx, cy, after)
	}
	if got := win.ForegroundWindow(); got != fg {
		t.Errorf("the foreground window changed from %d to %d (the cursor must never take it)", fg, got)
	}

	// winuse's own screenshot never contains the cursor (its captures run
	// with it hidden, like the banner): none of the accent colour it is
	// drawn in, although it sits over the fixture Window right now.
	stdout, stderr, exit := runGetWindowStateScreenshot(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state --screenshot: exit %d, stderr:\n%s", exit, stderr)
	}
	img := decodeImage(t, parseScreenshotLine(t, stdout).path)
	if n := countColourPixels(img, win.AgentCursorAccent()); n > 0 {
		t.Errorf("the Agent Cursor appears in winuse's own screenshot: %d pixels of its accent colour", n)
	}

	// The capture restores the cursor afterwards, while it has not yet
	// idled out.
	if time.Since(clicked) < 4*time.Second && len(agentCursorWindows(t)) == 0 {
		t.Errorf("the Agent Cursor did not come back after winuse's own capture")
	}
}
