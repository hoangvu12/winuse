//go:build e2e

// Package e2e holds winuse's seam-1 end-to-end tests: the real winuse
// binary (and the fixture app) against the live desktop.
//
// # These tests take over the screen. Opt-in only.
//
// They compile only with the `e2e` build tag and skip unless WINUSE_E2E=1
// is set in the environment. Only the user starts them:
//
//	set WINUSE_E2E=1
//	go test -tags e2e -v ./test/e2e
//
// Agents must never enable the opt-in: never set WINUSE_E2E=1, never run
// these tests, and never start the fixture app outside them, without the
// user's explicit go-ahead. The user may be using the machine.
//
// # What runs here
//
// The runner drives the real winuse binary exactly as an agent would (see
// runWinuse) and asserts on two things only, per the spec's testing
// decisions: what winuse prints, and the fixture app's end state (its
// -state JSON file, read back through internal/fixture's State).
//
// The fixture app (cmd/winusefixture, logic in internal/fixture) provides
// the deterministic target: its Element contract — every Element's Name,
// AutomationId, role, patterns and behaviour — is documented in one place,
// internal/fixture/elements.go. Import that package for the constants;
// never restate names or ids in tests.
//
// # Adding an end-to-end test
//
//  1. optIn(t) first — every test in this package must be gated.
//  2. Need the fixture? startFixture(t) (builds it, starts it with a state
//     file, kills it on cleanup); wait for readiness or effects with
//     (*fixtureProc).waitState.
//  3. Drive winuse through runWinuse(t, args, extraEnv...) — a real CLI
//     invocation. Pass WINUSE_HELPER_IDLE=e2eIdle so no helper lingers.
//  4. Assert on stdout/stderr/exit code and on the fixture's state file —
//     never on winuse internals.
//
// See test/e2e/README.md for the same rules in prose.
package e2e
