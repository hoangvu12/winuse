//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// TestHugeListDoesNotEndTheRead (learn-from 08): the fixture's huge-list
// Window holds a classic list box of 20,000 items and a button after it.
// A full read must reach the button, with the list cut either by the
// exact `(truncated: 1000, omitted ...)` suffix (the list came back as one
// chunk) or by the slow-list note (it was read one item at a time); the
// MEASURED lines say which and how long it took. The skeleton must reach
// the button too. Nothing is clicked: the reads leave the foreground alone.
func TestHugeListDoesNotEndTheRead(t *testing.T) {
	optIn(t)
	fix := startFixture(t, "-huge-list", "20000")
	st := fix.waitState(t, 30*time.Second, func(s *fixture.State) bool { return s.HugeListWindowID != 0 })
	id := itoa64(st.HugeListWindowID)
	env := "WINUSE_HELPER_IDLE=" + e2eIdle

	for _, args := range [][]string{
		{"get-window-state", id, "--max-chars", "0"},
		{"get-window-state", id, "--skeleton"},
	} {
		start := time.Now()
		out, stderr, exit := runWinuse(t, args, env)
		took := time.Since(start)
		if exit != 0 {
			t.Fatalf("%v: exit %d\n%s\n%s", args, exit, out, stderr)
		}
		t.Logf("MEASURED: %v took %s", args, took.Round(time.Millisecond))
		if !strings.Contains(out, "button "+fixture.HugeListButton) {
			t.Errorf("%v did not reach the button after the list:\n%s", args, tail(out, 1500))
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "Note:") || strings.Contains(line, "(truncated:") || strings.Contains(line, " children)") {
				t.Logf("MEASURED: %s", strings.TrimSpace(line))
			}
		}
		if args[len(args)-1] == "0" {
			if !strings.Contains(out, "(truncated: 1000, omitted ") && !strings.Contains(out, "were too slow to read at once") {
				t.Errorf("the list was not cut with the suffix or the slow-list note:\n%s", tail(out, 1500))
			}
		}
	}
}

// tail is the last n bytes of s, for a failure message.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
