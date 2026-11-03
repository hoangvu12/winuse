//go:build e2e

// The UIA worker pool and the hung pre-check (multi-agent 07) with two
// fixtures through one helper: fixture A runs with -hang-on-click, so a
// click on its Click Me stops its UI thread for 15 s. While A hangs, agent
// alpha reads A (the read waits on A's UIA worker), and agent beta's
// Headless click on fixture B, another App with a worker of its own, still
// lands within its budget, quickly. An Action on A is refused at once with
// nothing sent. All Actions are Headless, so the covers keep the
// foreground. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start it.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestHungAppStallsOnlyItsOwnWorker(t *testing.T) {
	optIn(t)
	fixA := startFixture(t, "-hang-on-click", "15000")
	windowA := strconv.FormatUint(fixA.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 }).WindowID, 10)
	coverWindow(t, windowA)
	fixB, windowB := startFixtureWithState(t)
	cover := coverWindow(t, windowB)

	agent := func(name string) []string {
		return []string{"WINUSE_AGENT=" + name, "WINUSE_HELPER_IDLE=" + e2eIdle}
	}
	for _, read := range []struct{ agent, window string }{{"alpha", windowA}, {"beta", windowB}} {
		if _, stderr, exit := runWinuse(t, []string{"get-window-state", read.window}, agent(read.agent)...); exit != 0 {
			t.Fatalf("%s get-window-state: exit %d, stderr:\n%s", read.agent, exit, stderr)
		}
	}
	checkbox := strconv.Itoa(mainElementIndexes()[fixture.ElemCheckboxNotify])
	button := strconv.Itoa(mainElementIndexes()[fixture.ElemButtonClick])

	// Hang A: its Click Me is recorded, then its UI thread sleeps.
	stdout, stderr, exit := runWinuse(t, []string{"click", windowA, button}, agent("alpha")...)
	assertActionOK(t, "the click that hangs fixture A", stdout, stderr, exit)
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastButton == "Click Me" })
	hungAt := time.Now()
	time.Sleep(500 * time.Millisecond) // the posted hang has begun

	// alpha reads the hung A: the read waits on A's UIA worker.
	readA := make(chan struct{})
	go func() {
		defer close(readA)
		runWinuse(t, []string{"get-window-state", windowA}, agent("alpha")...)
	}()
	time.Sleep(200 * time.Millisecond)

	// beta clicks B meanwhile: B's App has its own worker.
	start := time.Now()
	stdout, stderr, exit = runWinuse(t, []string{"click", windowB, checkbox}, agent("beta")...)
	took := time.Since(start)
	assertActionOK(t, "beta's click on fixture B while A hangs", stdout, stderr, exit)
	if took > 2*time.Second {
		t.Errorf("beta's click on B took %s while A hung: it waited for A's worker", took)
	}
	fixB.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })

	// An Action on the hung A is refused at once, with nothing sent.
	start = time.Now()
	stdout, stderr, exit = runWinuse(t, []string{"click", windowA, checkbox}, agent("gamma")...)
	if exit != 1 || !strings.Contains(stderr, "is not responding; nothing was sent") {
		t.Errorf("a click on the hung A: exit %d, stdout %q, stderr %q; want the not-responding refusal", exit, stdout, stderr)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the refusal took %s", took)
	}

	select {
	case <-readA:
	case <-time.After(20 * time.Second):
		t.Error("alpha's read of the hung A never answered")
	}
	time.Sleep(time.Until(hungAt.Add(16 * time.Second))) // A wakes
	if st := fixA.waitState(t, 5*time.Second, func(*fixture.State) bool { return true }); st.Checkbox {
		t.Error("the refused click reached fixture A after it woke")
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the last cover %d must stay in front", describeWindow(t, fg), cover)
	}
}
