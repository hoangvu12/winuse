//go:build e2e

// find, --root and --skeleton (desktop-parity 04) on the covered fixture:
// find adds its matches after the Window State's Element Indexes, a click
// on an added index acts on that Element and an earlier index still works;
// --root reads the list's subtree into new indexes; --skeleton shows the
// child counts. The foreground stays with the cover. User-run only (see
// the package comment and test/e2e/README.md); an agent must never start
// them.

package e2e

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestFindAndRegionReadOnTheFixture(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	run := func(args ...string) string {
		t.Helper()
		out, stderr, exit := runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit != 0 {
			t.Fatalf("winuse %s: exit %d, stderr:\n%s", strings.Join(args, " "), exit, stderr)
		}
		return out
	}

	state := run("get-window-state", window)
	lines := regexp.MustCompile(`(?m)^\t+(\d+) `).FindAllStringSubmatch(state, -1)
	last, _ := strconv.Atoi(lines[len(lines)-1][1])

	out := run("find", window, "--role", "list item", "--name", "charlie")
	m := regexp.MustCompile(`(?m)^\t(\d+) list item \(selectable\) Charlie`).FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, "were added to window "+window+"'s Window State") {
		t.Fatalf("find output:\n%s", out)
	}
	added, _ := strconv.Atoi(m[1])
	if added <= last {
		t.Errorf("find printed index %d, want one after the Window State's last (%d)", added, last)
	}
	if out := run("find", window, "--role", "list item", "--count"); strings.TrimSpace(out) != "8" {
		t.Errorf("find --count = %q, want 8", out)
	}
	if _, stderr, exit := runWinuse(t, []string{"find", window, "--role", "slider"}, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 1 || !strings.Contains(stderr, "the Window shows these roles:") {
		t.Errorf("role miss: exit %d, stderr %q", exit, stderr)
	}

	// The added index acts on its Element; an earlier index still works.
	assertActionOKOut(t, "click on the added index", run("click", window, strconv.Itoa(added)))
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.SelectedItem == "Charlie" })
	alpha := mainElementIndexes()[fixture.ElemItemAlpha]
	assertActionOKOut(t, "click on an earlier index", run("click", window, strconv.Itoa(alpha)))
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.SelectedItem == "Alpha" })

	list := mainElementIndexes()[fixture.ElemItemList]
	out = run("get-window-state", window, "--root", strconv.Itoa(list), "--depth", "1")
	if !regexp.MustCompile(`(?m)^\t\d+ list Item List`).MatchString(out) || strings.Count(out, "list item (selectable)") != 8 {
		t.Errorf("--root the list:\n%s", out)
	}
	out = run("get-window-state", window, "--skeleton")
	if !strings.Contains(out, "Note: this is a skeleton") {
		t.Errorf("--skeleton:\n%s", out)
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the cover %d must stay in front", describeWindow(t, fg), cover)
	}
}

// assertActionOKOut checks an Action's stdout is ok with any note lines.
func assertActionOKOut(t *testing.T, what, stdout string) {
	t.Helper()
	if _, ok := actionNotes(stdout); !ok {
		t.Errorf("%s: stdout %q, want ok", what, stdout)
	}
}
