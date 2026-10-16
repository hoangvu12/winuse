# End-to-end test runner (seam 1)

These are winuse's primary tests: the real `winuse` binary driven exactly as
an agent would drive it, against the **fixture app**, asserting only on
what `winuse` prints and on the fixture's end state — never on internals.

## They are opt-in only, and agents must not run them

These tests take over the screen: they start a GUI app, and winuse will
click, type, activate windows and take screenshots in later tickets. The
user may be using the machine. So:

- The tests compile only with the `-tags e2e` build tag.
- They skip unless `WINUSE_E2E=1` is set in the environment.
- **Only the user sets that variable and starts the run.** An agent must
  never enable the opt-in, never run these tests, and never start the
  fixture app by itself, without the user's explicit go-ahead.

How the user runs them:

```
set WINUSE_E2E=1
go test -tags e2e -v ./test/e2e
```

They never run as part of the default test command (`go test ./...` is
offline and hermetic; it only unit-tests the fixture's element contract and
the rest of the module).

## The fixture app

`cmd/winusefixture` (logic in `internal/fixture`) is a purpose-built
Windows app: pure Go, no cgo, one main window with a fixed layout and a
"Save As"-style owned dialog. Its Elements are known in advance because the
app *is* a server-side UI Automation provider: buttons, a text field, a
checkbox, a list, a menu bar with dropdown menus, a hover-only toolbar, a
canvas with a sparse subtree, and the dialog.

**The Element contract lives in exactly one place:
[`internal/fixture/elements.go`](../../internal/fixture/elements.go)** —
every Element's Name, AutomationId, role, control patterns and behaviour.
Later tickets' e2e tests (click, type, set-value, secondary actions,
coordinate clicks, scroll/drag/hover, `name:`/`id:` references, Batches,
Esc, overlay) must import that package for the constants and must not
restate or invent names or ids; extend the table there instead.

The app reports its end state as JSON to the file given by `-state`:
text field contents, checkbox state, last button pressed, menu invoked,
dialog result, list selection and scroll position, toolbar visibility,
canvas clicks/drags/scrolls (in canvas coordinates, with the canvas's
screen rectangle), the last key pressed, and a generation counter. The
schema is documented on `internal/fixture`'s `State` type. It is rewritten
atomically on every change.

The runner builds the fixture as a GUI-subsystem binary
(`go build -ldflags -H windowsgui`), starts it with a temp state file,
and kills it when the test ends.

## Writing a test in this package

1. Call `optIn(t)` first — every test must be gated.
2. Start the fixture with `startFixture(t)` if the test needs it; wait for
   readiness or effects with `(*fixtureProc).waitState(t, timeout, pred)`.
3. Drive `winuse` with `runWinuse(t, args, extraEnv...)` — a real CLI
   invocation. Pass `WINUSE_HELPER_IDLE=`+`e2eIdle` so no helper process
   lingers after the suite.
4. Assert on stdout/stderr/exit code and on the fixture's state file.

Current tests: the ticket-01 helper/list-windows contract tests
(`list_windows_test.go`) and `TestListWindowsShowsFixtureWindow`
(`fixture_test.go`) — the first seam-1 test: `winuse list-windows` must list
the fixture's Window, identified by the exact Window ID and title the
fixture reports in its state file.
