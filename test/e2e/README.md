# End-to-end test runner (seam 1)

These are winuse's primary tests: the real `winuse` binary driven exactly as
an agent would drive it, against the **fixture app**, asserting only on
what `winuse` prints and on the fixture's end state — never on internals.

## They are opt-in only, and agents must not run them

These tests take over the screen: they start a GUI app, and winuse will
click, activate windows, type and take screenshots while they run. The
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
text field contents (and whether they are selected, e.g. after
Control+A), checkbox state, last button pressed, menu invoked,
dialog result, list selection and scroll position, toolbar visibility,
canvas clicks/drags/scrolls (in canvas coordinates, with the canvas's
screen rectangle), the last key pressed, and a generation counter. The
schema is documented on `internal/fixture`'s `State` type. It is rewritten
atomically on every change.

The runner builds the fixture as a GUI-subsystem binary
(`go build -ldflags -H windowsgui`), starts it with a temp state file,
and kills it when the test ends. The fixture also accepts its state path
from `WINUSE_FIXTURE_STATE` when `-state` is unset — for tests where
`winuse launch-app` starts it (launched Apps inherit the helper's
environment, not the test's).

## Writing a test in this package

1. Call `optIn(t)` first — every test must be gated.
2. Start the fixture with `startFixture(t)` if the test needs it; wait for
   readiness or effects with `(*fixtureProc).waitState(t, timeout, pred)`.
3. Drive `winuse` with `runWinuse(t, args, extraEnv...)` — a real CLI
   invocation. Pass `WINUSE_HELPER_IDLE=`+`e2eIdle` so no helper process
   lingers after the suite.
4. Assert on stdout/stderr/exit code and on the fixture's state file.

Current tests: the ticket-01 helper/list-windows contract tests
(`list_windows_test.go`), `TestListWindowsShowsFixtureWindow`
(`fixture_test.go`) — the first seam-1 test: `winuse list-windows` must list
the fixture's Window, identified by the exact Window ID and title the
fixture reports in its state file — and the ticket-03 Window State tests
(`get_window_state_test.go`): the full Element tree by Window ID and by App
name, ambiguous-App and unknown-Window failures, the partial-tree-with-note
behaviour under a tiny `WINUSE_TREE_BUDGET`, and the recorder writing a
loadable seam-2 fixture; and the ticket-04 tests
(`launch_app_test.go`): `list-apps` must show the fixture App running with
its Window; `launch-app` by path must report exactly the Window the
launched fixture reports (the runner kills any running helper first so the
fresh one inherits the fixture state environment, and cleans up the
launched process by Window); a bogus name fails with `app not found`; a
windowless executable fails with `launched app did not expose a targetable
window` after the 15 s timeout. `parsing_test.go` (no build tag) unit-tests
the listing-output parsing offline. The ticket-05 Action tests
(`click_test.go`): clicking the button, checkbox, canvas (double and
right) and the Save As button changes the fixture's state; the dialog
appears as its own Window and its Cancel button clicks too; a
below-the-fold list item is scrolled into view before it is clicked; a
stale Element Index and a helper restart (no cached Window State) fail
with the read-the-state-again instruction; clicking the main window while
its modal dialog is open fails naming the dialog, without sending input;
`activate-window` works standalone and refuses the dialog-blocked main
window. Element Indexes are derived from the fixture's Element contract,
not restated. The ticket-06 keyboard tests (`type_text_test.go`): typing
Unicode (non-Latin scripts and a surrogate-pair emoji) into the text
field, unquoted multi-word text, a multi-batch long text, the ticket's
acceptance flow (type, `Control_L+a`, type the replacement — and
Backspace deleting the selection), typing into the dialog's file-name
edit, press-key by name (including numpad keys, which must arrive as
numpad virtual keys, and extended arrow keys), a chord, and the refusals
(unknown key with a hint, the forbidden Windows-key family, the empty
key) proven to send no input by the fixture's state generation staying
put.
The ticket-07 tests (`set_value_test.go`): `set-value` sets
the fixture's text field (the value shows in a fresh Window State) and the
Save As dialog's file-name edit, joins unquoted multi-argument values, and
fails on a non-settable Element with Codex's message plus the type-text
hint; `perform-secondary-action` toggles the checkbox (the Window State
flips to `on`), expands and collapses the File menu (the dropdown's items
appear in the tree), selects a list item and scrolls the list down one
page, performs Raise on the window root, and fails with the expected-name
list for an unknown action and with the offered list for an action the
Element does not offer. A stale Element Index fails pointing at a fresh
Window State read.

The ticket-14 test (`chromium_test.go`) covers the Chromium path: it starts
Edge (and Chrome, when installed) with an **isolated profile in a temp
directory** — the user's real browser profile is never touched, and a
separate user-data-dir means a fresh browser process even when the user's
own browser is running — opens the local HTML fixture page
(`testdata/chromium-fixture.html`), waits for its Window to appear in
`list-windows`, and asserts the page's link, button, input and checkbox
Elements show up in one `get-window-state` call (the lazy-accessibility
retry path: Chromium enables its tree at the first UI Automation contact,
so the first read is nearly empty and the retry must surface the page). It
skips when no Chromium browser is installed. Offline, the retry and the
sparse-tree notes are tested against recorded Chromium/Electron trees in
`internal/uia/testdata/` (see `internal/uia/retry_test.go`).

The ticket-08 tests (`screenshot_test.go`) cover screenshots, zoom and
coordinate clicks: `get-window-state --screenshot` saves a JPEG whose path
is printed, with the long edge capped at 1568 px and the scale recorded;
the tree is unaffected and no screenshot is taken without the flag; a
`@x,y` click on the fixture canvas lands within one pixel of the aimed
screen point (the expected point is derived from the fixture's reported
canvas rectangle and the printed scale, independently of winuse's
mapping), and the click's red mark on the canvas is visible in a fresh
screenshot; `zoom` returns a full-resolution PNG crop of the requested
region containing the mark; the open dropdown menu is visible in the
screenshot (the capture sees the live screen); a screenshot of a
modal-dialog-blocked Window fails naming the dialog while the dialog
itself screenshots fine; a helper restart loses the coordinate mapping
(the `@x,y` click tells the agent to take a fresh screenshot instead of
guessing); out-of-screenshot coordinates and zoom without any screenshot
are refused with the size and the recovery instruction. The coordinate
mapping itself (scale, footprint-centre rounding, window-anchored
survival of moves, any monitor including negative coordinates) is
offline-tested in `internal/shot` — the e2e tests pin only the live
wiring. With ticket 09 all of these now run through Windows.Graphics
Capture first (the method is prepended to the chain), which is also why
the dropdown-menu test still passes: the fixture paints its dropdown
inside its own window.

The ticket-09 tests (`wgc_test.go`) cover the capture chain's first
method and its fallbacks. Which method captured a frame is observable
through the helper log (the only file a detached helper writes): the
tests read the log's delta around one CLI invocation and assert on its
`wgc: captured window …` / `wgc: window … fell back: …` lines. The
positive test (`TestWGCScreenshotCapturedViaWindowsGraphicsCapture`)
proves the default screenshot comes from Windows.Graphics.Capture and
is the expected model-friendly image — it skips when the machine cannot
do WGC at all (before Windows 10 1903), since the criterion is "when
available". The forced-failure tests use the method's environment knobs
(each kills the running helper first so the fresh one inherits them):
`WINUSE_WGC=off` (`TestWGCDisabledFallsBackToGDI`) must produce a valid
screenshot through the GDI methods with the fallback logged and no WGC
capture; `WINUSE_WGC_BUDGET=1ms` (`TestWGCTimeoutFallsBackToGDI`)
starves the frame poll and must still produce a valid screenshot.
`TestCoordinateClickExactViaWGC` pins ticket 09's coordinate criterion:
a `@x,y` click aimed through a WGC-captured screenshot lands within one
pixel, and `TestWGCZoomStillFullResolution` pins zoom through the WGC
path (a full-resolution PNG crop containing the canvas mark). The pure
parts (IIDs, struct packing, frame-size cross-check, BGRA readback,
knobs) are offline-tested in `internal/wgc`.

The ticket-13 tests (`overlay_test.go`) cover the in-control overlay:
while a long type-text Action runs against the fixture, the overlay
window appears in the desktop's window list raised exactly over the
fixture Window, carrying the layered/click-through/topmost/tool-window/
no-activate styles; the fixture stays the foreground window (the overlay
never takes focus); a `@x,y` click aimed through the pill banner still
lands on the fixture Window (winuse's own occlusion check would fail
loudly if the overlay ate it); a `get-window-state --screenshot` taken
concurrently contains none of the overlay's accent colour (the capture
bracket hides it) and the overlay comes back afterwards; when the Action
ends the overlay is gone and the fixture's text field holds exactly the
typed text — input was delivered under it. A second test pins that
observation commands never raise the indicator. The overlay's brackets
and layout are offline-tested (`internal/helper`, `internal/win`); these
e2e tests pin the live window.

The ticket-10 tests (`pointer_test.go`) cover the pointer Actions:
`scroll` at a point scrolls the fixture list down and back up (the
fixture scrolls one item per wheel notch, so 360 units = 3 items), and
scrolls the canvas with both axes at once — the canvas records every
wheel event in canvas coordinates, which pins the point *and* both
signs (positive x right, scrollY up = positive wheel delta);
`drag` across the canvas registers exactly one drag with the from/to
points within a pixel of the aimed screenshot coordinates (the fixture
records from the button-down and to the button-up, so the interpolation
and the exact end pinning are both exercised);
`hover` over the hover band under the menu bar reveals the hover-only
toolbar (the pointer stays at the point: a subsequent Window State,
which never moves the pointer, still reports it and its Elements), and
hovering the canvas hides it again. The refusals: scrolling without a
screenshot, a helper restart losing the mapping (restored by a fresh
screenshot), points outside the screenshot, and a zero-amount scroll
proven to send no input by the state generation staying put.

**Display-scaling acceptance (user-run):** these tests exercise whatever
scaling the machine has. To accept ticket 08's `@x,y` criterion at
100%, 150% and 175% and on a secondary monitor, re-run the suite with
the display scaling changed between runs (Settings → System → Display →
Scale), and once with the fixture's window moved to a secondary monitor
(`TestCoordinateClickLandsOnAimedPoint` is the one that proves it); the
assertions are scale-independent on purpose. A real Win32 popup over the
target (a Notepad menu, a browser tooltip) is worth one manual
`get-window-state --screenshot` check as well: the fixture's dropdown is
painted inside its own window, so the popup-union path is only exercised
by real menus.

The ticket-11 tests (`batch_test.go`) cover `name:`/`id:` Element
References and Batches: clicking the button by `name:`, the Save As button
by `id:`; the deliberately ambiguous fixture Elements fail never-guess
(`name:Twin` and `id:solo` each list their two candidates, and no input
reaches the app); a missing name fails pointing at a fresh Window State
read; a reference resolves against the latest Window State (expanding the
File menu exposes `name:New`, which the previous read did not contain);
`set-value` and `perform-secondary-action` accept references too. Batch
tests: the acceptance flow — one Batch that clicks the text field by
`id:`, types into it and clicks a button by `name:` — succeeds and prints
`ok`; a Batch with a bad step (the ambiguous `name:Twin`) stops exactly
there, steps after it never run, and the failure names step 2 with the
candidates; an unknown Window fails step 1; a Batch on the Save As dialog
sets the file name and clicks Save in one call; usage errors (no steps,
an empty step, an unknown action) exit 2 without dialing. Reference
resolution (parsing, matching semantics, candidate lines) and Batch
parsing are offline-tested in `internal/wstate`, `internal/uia`,
`internal/helper` and `internal/cli`; these tests pin the live wiring
only.
