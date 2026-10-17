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
// environment).
//
// It is a plain GUI app: it takes over the desktop when run. It is never
// run by agents; only the user starts it (directly, or through the opt-in
// end-to-end tests).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func main() {
	state := flag.String("state", os.Getenv("WINUSE_FIXTURE_STATE"), "path of the JSON state file to keep updated, or set WINUSE_FIXTURE_STATE")
	flag.Parse()
	if *state == "" {
		fmt.Fprintln(os.Stderr, "usage: winusefixture -state <path> (or set WINUSE_FIXTURE_STATE)")
		os.Exit(2)
	}
	os.Exit(fixture.Main(*state))
}
