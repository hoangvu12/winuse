//go:build e2e

// Lanes (multi-agent 06) with two agents through one helper: agent alpha
// runs a Batch on fixture A that holds A's UI-thread lane for 2 s (a click,
// then a 2 s pause); meanwhile agent beta's Headless click on fixture B (its
// own process, so its own UI thread) finishes at once, and agent gamma's
// click on fixture A waits its turn behind alpha's Batch and then runs. All
// Actions are Headless, so the covers keep the foreground. User-run only
// (see the package comment and test/e2e/README.md); an agent must never
// start it.

package e2e

import (
	"strconv"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestTwoAgentsRunInTheirOwnLanes(t *testing.T) {
	optIn(t)
	fixA, windowA := startFixtureWithState(t)
	coverWindow(t, windowA)
	fixB, windowB := startFixtureWithState(t)
	cover := coverWindow(t, windowB)

	agent := func(name string) []string {
		return []string{"WINUSE_AGENT=" + name, "WINUSE_HELPER_IDLE=" + e2eIdle}
	}
	for _, read := range []struct{ agent, window string }{{"alpha", windowA}, {"beta", windowB}, {"gamma", windowA}} {
		if _, stderr, exit := runWinuse(t, []string{"get-window-state", read.window}, agent(read.agent)...); exit != 0 {
			t.Fatalf("%s get-window-state: exit %d, stderr:\n%s", read.agent, exit, stderr)
		}
	}
	checkbox := strconv.Itoa(mainElementIndexes()[fixture.ElemCheckboxNotify])
	button := strconv.Itoa(mainElementIndexes()[fixture.ElemButtonClick])

	type result struct {
		stdout, stderr string
		exit           int
		took           time.Duration
	}
	run := func(name string, args ...string) result {
		start := time.Now()
		out, errOut, exit := runWinuse(t, args, agent(name)...)
		return result{out, errOut, exit, time.Since(start)}
	}

	alpha := make(chan result, 1)
	go func() { alpha <- run("alpha", "batch", windowA, "click", checkbox, "then", "wait", "2000") }()
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox }) // alpha's Batch holds A's lane now

	beta := run("beta", "click", windowB, checkbox)
	assertActionOK(t, "beta's click on fixture B", beta.stdout, beta.stderr, beta.exit)
	if beta.took > 1500*time.Millisecond {
		t.Errorf("beta's click on another App's Window took %s: it waited for alpha's lane", beta.took)
	}
	fixB.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })

	gamma := run("gamma", "click", windowA, button)
	assertActionOK(t, "gamma's click on fixture A", gamma.stdout, gamma.stderr, gamma.exit)
	if gamma.took < 500*time.Millisecond {
		t.Errorf("gamma's click on alpha's Window took only %s: it did not wait for alpha's lane", gamma.took)
	}
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastButton == "Click Me" })

	a := <-alpha
	assertActionOK(t, "alpha's Batch", a.stdout, a.stderr, a.exit)
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the last cover %d must stay in front", describeWindow(t, fg), cover)
	}
}
