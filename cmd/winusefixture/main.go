// Command winusefixture is the winuse end-to-end test fixture app: a
// purpose-built Windows app whose Elements are known in advance so the
// seam-1 tests in test/e2e can drive the real winuse binary against
// deterministic UI.
//
// The Element contract (names, AutomationIds, roles, patterns, behaviour)
// lives in internal/fixture/elements.go — the single source of truth. The
// app reports its end state as JSON to the file given by -state, or by
// WINUSE_FIXTURE_STATE when the flag is unset (apps started by
// `winuse launch-app` cannot take flags; they inherit the helper's
// environment). -origin x,y (or WINUSE_FIXTURE_ORIGIN) places the main
// window's top-left corner in screen pixels — e.g. on a secondary monitor
// for the display acceptance runs; by default Windows places it.
//
// It is a plain GUI app: it takes over the desktop when run. It is never
// run by agents; only the user starts it (directly, or through the opt-in
// end-to-end tests).
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func main() {
	state := flag.String("state", os.Getenv("WINUSE_FIXTURE_STATE"), "path of the JSON state file to keep updated, or set WINUSE_FIXTURE_STATE")
	originArg := flag.String("origin", os.Getenv("WINUSE_FIXTURE_ORIGIN"), "x,y screen position of the main window, or set WINUSE_FIXTURE_ORIGIN")
	flag.Parse()
	if *state == "" {
		fmt.Fprintln(os.Stderr, "usage: winusefixture -state <path> [-origin x,y] (or set WINUSE_FIXTURE_STATE)")
		os.Exit(2)
	}
	origin, err := parseOrigin(*originArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "winusefixture:", err)
		os.Exit(2)
	}
	os.Exit(fixture.Main(*state, origin))
}

// parseOrigin parses "x,y" (signed: monitors left of or above the primary
// have negative coordinates); "" means no origin.
func parseOrigin(s string) (fixture.Origin, error) {
	if s == "" {
		return fixture.Origin{}, nil
	}
	xs, ys, ok := strings.Cut(s, ",")
	x, errX := strconv.ParseInt(strings.TrimSpace(xs), 10, 32)
	y, errY := strconv.ParseInt(strings.TrimSpace(ys), 10, 32)
	if !ok || errX != nil || errY != nil {
		return fixture.Origin{}, fmt.Errorf("origin %q: want x,y", s)
	}
	return fixture.Origin{X: int32(x), Y: int32(y), Set: true}, nil
}
