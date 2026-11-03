//go:build e2e

// Several agents at once (multi-agent ticket 10): two or three agents,
// named by WINUSE_AGENT, run winuse commands at the same time through one
// helper.
//
//   - TestTwoAgentsActAtOnceOnTwoFixtures: alpha and beta each run a
//     Batch (a Headless click, ten Posted Keys 150 ms apart, a Headless
//     set-value) on their own covered
//     fixture at the same time. Both succeed, the Batches overlap in time
//     (parallel, not one after the other), and the foreground stays on the
//     cover.
//   - TestAgentsIndexesSurviveAnotherAgentsRead: on one fixture, alpha
//     reads, beta opens the File menu and reads again (beta's Element
//     Indexes shift by the menu's items), and alpha's click by its own
//     index still toggles alpha's Element, the checkbox; beta's click by
//     beta's index toggles it back.
//   - TestHeadedActionsTakeTurnsOnTheDesktop: alpha's Headed Batch holds the
//     desktop lane; beta's Headed click waits for it and then runs, and
//     while alpha holds it past the 12 s lane limit, beta's next Headed
//     click is refused with nothing sent, naming alpha.
//   - TestEscStopsEveryAgent: one physical Esc press stops alpha's and
//     beta's running Batches and gamma's command waiting behind alpha's.
//     Interactive: it needs WINUSE_E2E_HUMAN=1, as TestEscStopsTheLongBatch.
//
// The other multi-agent e2e tests are not repeated here: per-agent Window
// States (TestWindowStatesArePerAgent), cursors
// (TestEachAgentsCursorSitsAboveItsOwnWindow), lanes on one UI thread
// (TestTwoAgentsRunInTheirOwnLanes), the hung App
// (TestHungAppStallsOnlyItsOwnWorker) and polite real input
// (TestHeadedClickGivesTheForegroundAndCursorBack). User-run only (see
// the package comment and test/e2e/README.md); an agent must never start
// them.

package e2e

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// asAgent is the environment of a command sent by the named agent.
func asAgent(name string) []string {
	return []string{"WINUSE_AGENT=" + name, "WINUSE_HELPER_IDLE=" + e2eIdle}
}

// timedResult is an asynchronous command's result with when it started
// and when its result arrived.
type timedResult struct {
	actionResult
	start, end time.Time
}

// runTimedAsync starts a winuse command and reports its result with its
// start and end times.
func runTimedAsync(t *testing.T, args []string, env ...string) <-chan timedResult {
	t.Helper()
	start := time.Now()
	res := runActionAsync(t, args, env...)
	out := make(chan timedResult, 1)
	go func() {
		r := <-res
		out <- timedResult{actionResult: r, start: start, end: time.Now()}
	}()
	return out
}

// readAs reads window's State as the named agent.
func readAs(t *testing.T, name, window string) {
	t.Helper()
	if _, stderr, exit := runWinuse(t, []string{"get-window-state", window}, asAgent(name)...); exit != 0 {
		t.Fatalf("%s get-window-state %s: exit %d, stderr:\n%s", name, window, exit, stderr)
	}
}

func TestTwoAgentsActAtOnceOnTwoFixtures(t *testing.T) {
	optIn(t)
	fixA, windowA := startFixtureWithState(t)
	coverWindow(t, windowA)
	fixB, windowB := startFixtureWithState(t)
	cover := coverWindow(t, windowB)
	readAs(t, "alpha", windowA)
	readAs(t, "beta", windowB)
	idx := mainElementIndexes()
	checkbox, field := strconv.Itoa(idx[fixture.ElemCheckboxNotify]), strconv.Itoa(idx[fixture.ElemTextField])

	// The middle step takes about 1.5 s: ten characters posted 150 ms
	// apart.
	const pause = 1500 * time.Millisecond
	batch := func(window, text string) []string {
		return []string{"batch", window, "click", checkbox, "then", "type-text", "--post", "--interval-ms", "150", "abcdefghij", "then", "set-value", field, text}
	}
	alpha := runTimedAsync(t, batch(windowA, "alpha-text"), asAgent("alpha")...)
	beta := runTimedAsync(t, batch(windowB, "beta-text"), asAgent("beta")...)
	a, b := <-alpha, <-beta
	assertBatchOK(t, "alpha's Batch on fixture A", a.stdout, a.stderr, a.exit, 3)
	assertBatchOK(t, "beta's Batch on fixture B", b.stdout, b.stderr, b.exit, 3)
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox && s.Text == "alpha-text" })
	fixB.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox && s.Text == "beta-text" })

	// Parallel, not serial: each Batch pauses 1.5 s, so one after the
	// other they would overlap by nothing; at once, by about the pause.
	overlap := minTime(a.end, b.end).Sub(maxTime(a.start, b.start))
	t.Logf("alpha %s, beta %s, overlap %s", a.end.Sub(a.start), b.end.Sub(b.start), overlap)
	if overlap < pause*2/3 {
		t.Errorf("the two agents' Batches overlapped by %s; at once they overlap by about %s", overlap, pause)
	}
	assertForeground(t, "two agents' Headless Batches", cover)
}

func TestAgentsIndexesSurviveAnotherAgentsRead(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	closed := mainElementIndexes()
	open := elementIndexes(fixture.BuildTree(fixture.State{OpenMenu: string(fixture.ElemMenuFile)}), fixture.ElemWindow, mainFrameElements)
	if closed[fixture.ElemCheckboxNotify] == open[fixture.ElemCheckboxNotify] {
		t.Fatal("the open File menu must shift the checkbox's Element Index for this test to mean anything")
	}

	readAs(t, "alpha", window)
	readAs(t, "beta", window)
	// beta opens the File menu and reads again: beta's indexes shift.
	stdout, stderr, exit := runWinuse(t, []string{"click", window, strconv.Itoa(closed[fixture.ElemMenuFile])}, asAgent("beta")...)
	assertActionOK(t, "beta opens the File menu", stdout, stderr, exit)
	before := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.OpenMenu == string(fixture.ElemMenuFile) })
	readAs(t, "beta", window)

	// alpha clicks by alpha's index: alpha's Element, the checkbox, is
	// the one acted on, not what that index names in beta's tree.
	stdout, stderr, exit = runWinuse(t, []string{"click", window, strconv.Itoa(closed[fixture.ElemCheckboxNotify])}, asAgent("alpha")...)
	assertActionOK(t, "alpha's click by alpha's index", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox != before.Checkbox })
	if st.LastButton != before.LastButton || st.SelectedItem != before.SelectedItem || st.LastMenu != before.LastMenu || st.Text != before.Text {
		t.Errorf("alpha's click changed more than the checkbox: before %+v, after %+v", before, st)
	}

	// beta clicks by beta's index: the same checkbox, back.
	stdout, stderr, exit = runWinuse(t, []string{"click", window, strconv.Itoa(open[fixture.ElemCheckboxNotify])}, asAgent("beta")...)
	assertActionOK(t, "beta's click by beta's index", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox == before.Checkbox })
	assertForeground(t, "two agents' clicks in one Window", cover)
}

func TestHeadedActionsTakeTurnsOnTheDesktop(t *testing.T) {
	optIn(t)
	parkPointer(t)
	fixA, windowA := startFixtureWithState(t)
	coverWindow(t, windowA)
	fixB, windowB := startFixtureWithState(t)
	cover := coverWindow(t, windowB)
	readAs(t, "alpha", windowA)
	readAs(t, "beta", windowB)
	canvas := strconv.Itoa(mainElementIndexes()[fixture.ElemCanvas])
	// alpha's Headed Batch: a double click on the canvas (it opens
	// nothing), then about 3 s holding the desktop lane: ten characters
	// typed 300 ms apart. The canvas takes no text, so they change
	// nothing.
	holdDesktop := []string{"--headed", "batch", windowA, "click", canvas, "--count", "2", "then", "type-text", "--interval-ms", "300", "abcdefghij"}

	alpha := runTimedAsync(t, holdDesktop, asAgent("alpha")...)
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasDoubleClicks >= 1 })

	// beta's Headed click waits for the desktop, then runs.
	start := time.Now()
	stdout, stderr, exit := runWinuse(t, []string{"--headed", "click", windowB, canvas, "--count", "2"}, asAgent("beta")...)
	took := time.Since(start)
	assertActionOK(t, "beta's Headed click behind alpha's", stdout, stderr, exit)
	fixB.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasDoubleClicks >= 1 })
	select {
	case a := <-alpha:
		assertBatchOK(t, "alpha's Headed Batch", a.stdout, a.stderr, a.exit, 2)
	default:
		t.Error("beta's Headed click ended before alpha's Batch released the desktop")
	}
	if took < time.Second {
		t.Errorf("beta's Headed click took %s: it did not wait for alpha's desktop lane", took)
	}

	// Held past the 12 s lane limit, the desktop refuses beta. alpha holds
	// it with a wait for a state that never comes (the disabled button
	// stays disabled), which sends no input; a caller --timeout cannot
	// shorten beta's wait instead, since it only widens a budget
	// (desktop-parity 14).
	disabled := strconv.Itoa(mainElementIndexes()[fixture.ElemButtonDisabled])
	holdLong := []string{"--headed", "batch", windowA, "click", canvas, "--count", "2", "then", "wait", disabled, "enabled", "--timeout", "15"}
	alpha = runTimedAsync(t, holdLong, asAgent("alpha")...)
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasDoubleClicks >= 2 })
	start = time.Now()
	stdout, stderr, exit = runWinuse(t, []string{"--headed", "click", windowB, canvas, "--count", "2"}, asAgent("beta")...)
	if exit != 1 || !strings.Contains(stderr, "agent alpha is using the desktop; nothing was sent") {
		t.Errorf("beta's Headed click behind alpha's long Batch: exit %d, stdout %q, stderr %q; want the desktop refusal naming alpha", exit, stdout, stderr)
	}
	if took := time.Since(start); took < 10*time.Second || took > 14*time.Second {
		t.Errorf("the refusal came after %s; a request waits 12 s for its lanes", took)
	}
	a := <-alpha
	if a.exit != 1 || !strings.Contains(a.stdout, "Action 1 (click): ok") {
		t.Errorf("alpha's long Headed Batch: exit %d, stdout %q, stderr %q; want its click ok and its wait failed", a.exit, a.stdout, a.stderr)
	}
	if st := fixB.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.CanvasDoubleClicks != 1 {
		t.Errorf("fixture B saw %d double clicks; the refused Batch must send nothing", st.CanvasDoubleClicks)
	}
	assertForeground(t, "after the Headed Actions (polite input gives it back)", cover)
}

func TestEscStopsEveryAgent(t *testing.T) {
	optIn(t)
	if os.Getenv("WINUSE_E2E_HUMAN") != "1" {
		t.Skip("needs a person to press the physical Esc key: set WINUSE_E2E_HUMAN=1 and stay at the keyboard (injected keys cannot trip the hook, by design)")
	}
	fixA, windowA := startFixtureWithState(t)
	coverWindow(t, windowA)
	fixB, windowB := startFixtureWithState(t)
	coverWindow(t, windowB)
	readAs(t, "alpha", windowA)
	readAs(t, "beta", windowB)
	readAs(t, "gamma", windowA)
	idx := mainElementIndexes()
	checkbox, button := strconv.Itoa(idx[fixture.ElemCheckboxNotify]), strconv.Itoa(idx[fixture.ElemButtonClick])

	// alpha and beta: Headless Batches that pause 25 s before clicking
	// Click Me, a step that must never run once the user stops them.
	long := func(window string) []string {
		return []string{"batch", window, "click", checkbox, "then", "wait", "25000", "then", "click", button}
	}
	alpha := runTimedAsync(t, long(windowA), asAgent("alpha")...)
	beta := runTimedAsync(t, long(windowB), asAgent("beta")...)
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })
	fixB.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })
	// gamma's click on A waits behind alpha's Batch (A's UI-thread lane),
	// up to the 12 s lane limit.
	gamma := runTimedAsync(t, []string{"click", windowA, button}, asAgent("gamma")...)
	time.Sleep(300 * time.Millisecond)

	fmt.Printf("\n\n    >>> PRESS THE PHYSICAL ESC KEY NOW — you have ~10 s <<<\n\n\n")
	t.Log("PRESS THE PHYSICAL ESC KEY NOW (~10 s before gamma's wait for its lane ends)")

	for _, c := range []struct {
		who string
		res <-chan timedResult
	}{{"alpha's Batch", alpha}, {"beta's Batch", beta}, {"gamma's waiting click", gamma}} {
		select {
		case r := <-c.res:
			assertEscStopped(t, c.who, &r.actionResult)
		case <-time.After(40 * time.Second):
			t.Fatalf("%s did not end within 40 s; did you press Esc?", c.who)
		}
	}
	if st := fixA.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.LastButton != "" {
		t.Errorf("fixture A's Click Me ran (%q) after the stop", st.LastButton)
	}
	if st := fixB.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.LastButton != "" {
		t.Errorf("fixture B's Click Me ran (%q) after the stop", st.LastButton)
	}

	// A command sent after the press runs as usual.
	stdout, stderr, exit := runWinuse(t, []string{"click", windowA, button}, asAgent("gamma")...)
	assertActionOK(t, "gamma's click after the stop", stdout, stderr, exit)
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastButton == "Click Me" })
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
