//go:build e2e

package e2e

// --json (desktop-parity ticket 07) through the real binary and helper:
// one JSON line on stdout, nothing on stderr, the exit code unchanged,
// and a failure's code and delivery from the helper.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// jsonResult is the line --json prints.
type jsonResult struct {
	OK      bool   `json:"ok"`
	Command string `json:"command"`
	Out     string `json:"out"`
	Error   *struct {
		Code     string `json:"code"`
		Message  string `json:"message"`
		Hint     string `json:"hint"`
		Delivery string `json:"delivery"`
		Retry    string `json:"retry"`
	} `json:"error"`
}

// runJSONClick runs `winuse --json click <args>` and parses its line.
func runJSONClick(t *testing.T, args ...string) (jsonResult, int) {
	t.Helper()
	stdout, stderr, exit := runWinuse(t, append([]string{"--json", "click"}, args...), "WINUSE_HELPER_IDLE="+e2eIdle)
	if stderr != "" {
		t.Errorf("--json click %q: stderr %q, want nothing", args, stderr)
	}
	var res jsonResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("--json click %q: stdout %q is not one JSON line: %v", args, stdout, err)
	}
	return res, exit
}

func TestJSONOutputOfAClickAndAStaleIndex(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	idx := mainElementIndexes()[fixture.ElemButtonClick]
	res, exit := runJSONClick(t, window, strconv.Itoa(idx))
	if exit != 0 || !res.OK || res.Command != "click" || !strings.HasPrefix(res.Out, "ok\n") || res.Error != nil {
		t.Errorf("click: exit %d, line %+v; want ok with out \"ok\n\"", exit, res)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastButton == "Click Me" })

	res, exit = runJSONClick(t, window, "999")
	if exit != 1 || res.OK || res.Error == nil {
		t.Fatalf("stale click: exit %d, line %+v; want a failure", exit, res)
	}
	if res.Error.Code != "STALE_REF" || !strings.Contains(res.Error.Message, "element 999 is not available") ||
		!strings.Contains(res.Error.Hint, "get-window-state") {
		t.Errorf("stale click: error %+v; want STALE_REF with the read-again hint", *res.Error)
	}
}
