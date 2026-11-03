//go:build e2e

// Lanes (multi-agent 06) with two agents through one helper: agent alpha
// types twenty characters into fixture A as Posted Keys, 200 ms apart,
// which holds A's UI-thread lane for about 4 s; meanwhile agent beta's
// Headless click on fixture B (its own process, so its own UI thread)
// finishes while alpha still types, and agent gamma's click on fixture A
// waits its turn behind alpha's typing and finishes after it. The test
// compares when each command ended rather than how long it took (a click
// takes 0.8 to 1.9 s on its own). No Action takes the foreground, so the
// covers keep it. User-run only
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
	const typed = "abcdefghijklmnopqrst" // at 200 ms a character, about 4 s in A's lane
	button := strconv.Itoa(mainElementIndexes()[fixture.ElemButtonClick])

	type result struct {
		stdout, stderr string
		exit           int
		took           time.Duration
		end            time.Time
	}
	run := func(name string, args ...string) result {
		start := time.Now()
		out, errOut, exit := runWinuse(t, args, agent(name)...)
		return result{out, errOut, exit, time.Since(start), time.Now()}
	}

	alpha := make(chan result, 1)
	go func() { alpha <- run("alpha", "type-text", windowA, "--post", "--interval-ms", "200", typed) }()
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Typed != "" }) // alpha's typing holds A's lane now

	beta := run("beta", "click", windowB, checkbox)
	assertActionOK(t, "beta's click on fixture B", beta.stdout, beta.stderr, beta.exit)
	fixB.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })

	gamma := run("gamma", "click", windowA, button)
	assertActionOK(t, "gamma's click on fixture A", gamma.stdout, gamma.stderr, gamma.exit)
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastButton == "Click Me" })

	a := <-alpha
	t.Logf("alpha took %s, beta %s, gamma %s", a.took, beta.took, gamma.took)
	if !beta.end.Before(a.end) {
		t.Errorf("beta's click on another App's Window ended %s after alpha's typing: it waited for alpha's lane", beta.end.Sub(a.end))
	}
	if !gamma.end.After(a.end) {
		t.Errorf("gamma's click on alpha's Window ended %s before alpha's typing: it did not wait for alpha's lane", a.end.Sub(gamma.end))
	}
	assertActionOK(t, "alpha's posted typing", a.stdout, a.stderr, a.exit)
	fixA.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Typed == typed })
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the last cover %d must stay in front", describeWindow(t, fg), cover)
	}
}
