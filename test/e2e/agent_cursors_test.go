//go:build e2e

// A cursor per Agent (ticket 04 of multi-agent): two Agents named by
// WINUSE_AGENT click on two covered fixtures through one helper. Each
// Agent's cursor sits just above its own fixture and below the Window
// covering it, never topmost. The colours and the names in the pills are
// for the user to look at: the cursors are excluded from screen captures,
// so the test cannot read them. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start them.

package e2e

import (
	"strconv"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// zOrderOf is a window's place in the z-order among the windows
// win.Collect lists (topmost first, from 0), or -1.
func zOrderOf(t *testing.T, id uint64) int {
	t.Helper()
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	for i, w := range all {
		if w.ID == id {
			return i
		}
	}
	return -1
}

func TestEachAgentsCursorSitsAboveItsOwnWindow(t *testing.T) {
	optIn(t)
	type target struct {
		agent  string
		fix    *fixtureProc
		window string
		cover  uint64
	}
	var targets []target
	for _, name := range []string{"alpha", "beta"} {
		fix, window := startFixtureWithState(t)
		cover := coverWindow(t, window)
		targets = append(targets, target{agent: name, fix: fix, window: window, cover: cover})
	}
	checkbox := strconv.Itoa(mainElementIndexes()[fixture.ElemCheckboxNotify])
	for _, tg := range targets {
		env := []string{"WINUSE_AGENT=" + tg.agent, "WINUSE_HELPER_IDLE=" + e2eIdle}
		if _, stderr, exit := runWinuse(t, []string{"get-window-state", tg.window}, env...); exit != 0 {
			t.Fatalf("%s get-window-state: exit %d, stderr:\n%s", tg.agent, exit, stderr)
		}
		stdout, stderr, exit := runWinuse(t, []string{"click", tg.window, checkbox}, env...)
		assertActionOK(t, tg.agent+"'s click", stdout, stderr, exit)
		tg.fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })
	}

	// Each fixture has a cursor window between it and its cover: the
	// user's cover stays above the Agent's cursor.
	cursors := agentCursorWindows(t)
	for _, tg := range targets {
		fz, cz := zOrderOf(t, mustWindowID(t, tg.window)), zOrderOf(t, tg.cover)
		between := false
		for _, w := range cursors {
			if w.ExStyle&exTopmost != 0 {
				t.Errorf("cursor window %d is topmost", w.ID)
			}
			if z := zOrderOf(t, w.ID); z > cz && z < fz {
				between = true
			}
		}
		if !between {
			t.Errorf("%s: no cursor window between the cover (z %d) and its fixture (z %d)", tg.agent, cz, fz)
		}
	}
	// For the user's eye: minimize the covers (Headless) and leave the
	// cursors showing for a few seconds, before their idle fade.
	for _, tg := range targets {
		stdout, stderr, exit := runWinuse(t, []string{"minimize-window", strconv.FormatUint(tg.cover, 10)}, "WINUSE_HELPER_IDLE="+e2eIdle)
		assertActionOK(t, "minimize-window (a cover)", stdout, stderr, exit)
	}
	t.Logf("LOOK: each fixture shows its own cursor, in two different colours, with \"alpha: click\" and \"beta: click\" in the pills")
	time.Sleep(5 * time.Second)
}
