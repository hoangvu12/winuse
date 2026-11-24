//go:build e2e

// extract on native lists (bladebro ideas, ticket 27) on the fixture: the
// main window's Item List read whole as names, and the huge-list Window's
// 20,000-item list box read only up to --limit, its total from the items'
// set size when the proxy gives it. extract only reads: nothing is
// clicked or scrolled. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start them.

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func TestExtractNativeListOnTheFixture(t *testing.T) {
	optIn(t)
	_, window := startFixtureWithState(t)
	out, stderr, exit := runWinuse(t, []string{"extract", window}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("extract: exit %d\n%s", exit, stderr)
	}
	var want strings.Builder
	want.WriteString(`list: list "Item List": 8 of 8 rows (complete)` + "\n[\n")
	names := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"}
	for i, n := range names {
		want.WriteString(`{"name":"` + n + `"}`)
		if i < len(names)-1 {
			want.WriteString(",")
		}
		want.WriteString("\n")
	}
	want.WriteString("]\nhint: if this is the wrong list, pass --root <index> of the list or of one item, from winuse get-window-state " + window + "\n")
	if out != want.String() {
		t.Errorf("extract printed\n%s\nwant\n%s", out, want.String())
	}
}

func TestExtractNativeHugeListStopsAtTheLimit(t *testing.T) {
	optIn(t)
	fix := startFixture(t, "-huge-list", "20000")
	st := fix.waitState(t, 30*time.Second, func(s *fixture.State) bool { return s.HugeListWindowID != 0 })
	id := itoa64(st.HugeListWindowID)
	start := time.Now()
	out, stderr, exit := runWinuse(t, []string{"extract", id, "--limit", "5"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	took := time.Since(start)
	if exit != 0 {
		t.Fatalf("extract: exit %d\n%s", exit, stderr)
	}
	t.Logf("MEASURED: extract --limit 5 of 20,000 items took %s", took.Round(time.Millisecond))
	head, body, _ := strings.Cut(out, "\n")
	t.Logf("MEASURED: %s", head)
	if !strings.HasPrefix(head, "list: list") || !strings.Contains(head, ": 5 of ") || !strings.HasSuffix(head, " rows (partial: capped at 5)") {
		t.Errorf("header %q", head)
	}
	if !strings.HasPrefix(body, "[\n"+`{"name":"`+fixture.HugeListItemPrefix+`1"},`) || !strings.Contains(body, `{"name":"`+fixture.HugeListItemPrefix+`5"}`+"\n]\n") ||
		strings.Contains(body, fixture.HugeListItemPrefix+`6"`) {
		t.Errorf("rows:\n%s", body)
	}
}
