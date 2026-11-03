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

Two main-window buttons open real system windows that the fixture does
not draw: **Classic Dialog...** opens a modeless `#32770` dialog with a
Win32 Edit (focused when it opens), a password Edit, OK (the default
button) and Cancel, and **Message Box...** opens a `MessageBox`
(OK/Cancel). They carry the window messages that winuse's classic
keyboard routes use, which the drawn Elements cannot. Their titles,
control IDs and behaviour are documented in `elements.go` next to the
Element table, and the state file reports their text, focus, and how they
closed.

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
environment, not the test's). `launch-app --arg` and `--env` can pass
either directly (headless-max ticket 01).

Two flags make it an App that brings itself to the foreground (headless-max
ticket 02): `-activate-on-restore` makes the main window call
SetForegroundWindow on itself whenever it is restored from minimized, and
`-allow-foreground <pid>` makes an instance grant that process the right to
set the foreground window (AllowSetForegroundWindow) each time the instance
is activated. Windows refuses a background process's SetForegroundWindow
unless the foreground process grants it, or in input states a test cannot
arrange, so a covering instance with the grant stands in for the user's
foreground App.

## Writing a test in this package

1. Call `optIn(t)` first — every test must be gated.
2. Start the fixture with `startFixture(t)` if the test needs it; wait for
   readiness or effects with `(*fixtureProc).waitState(t, timeout, pred)`.
3. Drive `winuse` with `runWinuse(t, args, extraEnv...)` — a real CLI
   invocation. Pass `WINUSE_HELPER_IDLE=`+`e2eIdle` so no helper process
   lingers after the suite. The idle period counts from the end of the
   last request, so a read longer than `e2eIdle` (a cold Chromium) keeps
   the helper, and its cached Window State, for the next command.
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
right, with `--headed`) and the Save As button changes the fixture's state; the dialog
appears as its own Window and its Cancel button clicks too; a
below-the-fold list item is scrolled into view before it is clicked; a
stale Element Index and a helper restart (no cached Window State) fail
with the read-the-state-again instruction; clicking the main window while
its modal dialog is open fails naming the dialog, without sending input;
`activate-window` works standalone and refuses the dialog-blocked main
window. Element Indexes are derived from the fixture's Element contract,
not restated. The ticket-06 keyboard tests (`type_text_test.go`), all with `--headed`
because the fixture draws its own text fields (no classic edit control,
so typing and keys there have no Headless route): typing
Unicode (non-Latin scripts and a surrogate-pair emoji) into the text
field, unquoted multi-word text, a multi-batch long text, the ticket's
acceptance flow (type, `Control_L+a`, type the replacement — and
Backspace deleting the selection), typing into the dialog's file-name
edit (`TestTypeTextIntoDialogEdit`: plain `type-text` is refused first
with "nothing was sent", the `winuse --headed type-text <dialog> <text>`
retry and no text in the dialog, then `--headed` types it), press-key by name (including numpad keys, which must arrive as
numpad virtual keys, and extended arrow keys), a chord, and the refusals
(unknown key with a hint, the forbidden Windows-key family, the empty
key, all refused before any route is chosen, so without `--headed`)
proven to send no input by the fixture's state generation staying put.
The ticket-07 tests (`set_value_test.go`): `set-value` sets
the fixture's text field (the value shows in a fresh Window State) and the
Save As dialog's file-name edit, joins unquoted multi-argument values, and
fails on a non-settable Element with Codex's message plus the type-text
hint; `perform-secondary-action` toggles the checkbox (the Window State
flips to `on`), expands and collapses the File menu (the dropdown's items
appear in the tree), selects a list item and scrolls the list down one
page, performs Raise on the window root (with `--headed`: Raise takes the
foreground by definition), and fails with the expected-name
list for an unknown action and with the offered list for an action the
Element does not offer. A stale Element Index fails pointing at a fresh
Window State read. `set-value` and `perform-secondary-action` at an
`@x,y` point (`TestSetValueByPoint`, `TestPerformSecondaryActionByPoint`)
pass `--headed`: the point form finds its Element only with the Window in
the foreground.

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
`--headed` `@x,y` click on the fixture canvas lands within one pixel of the aimed
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
are refused with the size and the recovery instruction. Every `@x,y`
click here goes through `runClickPoint`, which passes `--headed`: without
it a click at a point is refused before the mapping is read
(`headed_gate_test.go`). The coordinate
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
a `--headed` `@x,y` click aimed through a WGC-captured screenshot lands within one
pixel, and `TestWGCZoomStillFullResolution` pins zoom through the WGC
path (a full-resolution PNG crop containing the canvas mark). The pure
parts (IIDs, struct packing, frame-size cross-check, BGRA readback,
knobs) are offline-tested in `internal/wgc`.

The ticket-13 tests (`overlay_test.go`) cover the in-control overlay:
while a long `--headed` type-text Action runs against the fixture (the
banner is the Headed pipeline's), the overlay
window appears in the desktop's window list raised exactly over the
fixture Window, carrying the layered/click-through/topmost/tool-window/
no-activate styles; the fixture stays the foreground window (the overlay
never takes focus); the system hit-tests the fixture Window, not the
overlay, under the pill banner (click-through, asked of the desktop
directly because winuse serializes Actions); a concurrent
`get-window-state --screenshot` queues behind the Action and contains
none of the overlay's accent colour; when the Action
ends the overlay is gone and the fixture's text field holds exactly the
typed text — input was delivered under it. A second test pins that
observation commands never raise the indicator. The overlay's brackets
and layout are offline-tested (`internal/helper`, `internal/win`); these
e2e tests pin the live window.

The ticket-10 tests (`pointer_test.go`) cover the pointer Actions, all
with `--headed` (the point forms of scroll, drag and hover on the fixture
have no Headless route; the helpers pass the flag): `scroll` at a point scrolls the fixture list down and back up (the
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
Scale), and once with the fixture's window on a secondary monitor
(`TestCoordinateClickLandsOnAimedPoint` is the one that proves it); the
assertions are scale-independent on purpose. Every test starts a fresh
fixture, so place it with `WINUSE_FIXTURE_ORIGIN=x,y` (screen pixels of
the main window's top-left, e.g. `2100,120` for a 1920-wide monitor to
the right of the primary; the fixture inherits the test's environment). A real Win32 popup over the
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
each step's `Action N (<action>): ok` line; a Batch with a bad step (the
ambiguous `name:Twin`) runs on past it (desktop-parity 20): step 2's line
is the failure with the candidates in its hint, step 3's `set-value` lands,
and the Batch exits 1 naming step 2; with `--stop-on-error` the same
Batch stops there, step 3 never runs, and the failure names step 2 with
the candidates; an unknown Window fails the Batch before any step; a Batch on the Save As dialog
sets the file name and clicks Save in one call; usage errors (no steps,
an empty step, an unknown action) exit 2 without dialing. Reference
resolution (parsing, matching semantics, candidate lines) and Batch
parsing are offline-tested in `internal/wstate`, `internal/uia`,
`internal/helper` and `internal/cli`; these tests pin the live wiring
only.

The ticket-12 tests (`esc_test.go`) cover the Esc stop.
**`TestEscStopsTheLongBatch` is interactive — a human must be at the
keyboard** (with `-v`, the run prints `PRESS THE PHYSICAL ESC KEY NOW`).
It skips unless `WINUSE_E2E_HUMAN=1` is set as well, so an unattended run
does not fail on it: only a physical press can trip the low-level hook, by
design (user-mode code cannot make a key the hook sees as not injected:
SendInput and keybd_event set `LLKHF_INJECTED`), so the
test starts a long Batch against the fixture (click, a ~30 s type-text,
then a button click and more typing — steps that must never run once the
user stops it), waits for the typing to start, prompts, and then asserts
the full stop contract: the command exits 130 (Codex's interrupt code)
with the message that says the user stopped winuse with the physical
Escape key plus the stop-and-ask hint; the typing aborted as a proper
prefix of the long text; the fixture's `lastKey` is not `Escape` (the
hook swallowed the key — the app must not act on the stop key); and the
in-control overlay is gone. `TestInjectedEscDoesNotStopTheBatch` is
self-contained: while a Batch types, the test injects Escape with its own
SendInput (a concurrent winuse `--headed press-key` would wait for the
desktop lane the Batch holds and be refused, ADR 0014 Decision 6) — it
must reach the app (the fixture records the key-down) without stopping the
Batch, which still prints each step's `ok`. The
hook's decision table (injected filtering, swallow pairing, the armed
scope), the abort checkpoints and the exit-130 mapping are
offline-tested in `internal/win`, `internal/apps`, `internal/helper`;
these tests pin the live hook only.

The headless-feature tests (`headless_test.go`, ADR 0010) cover Headless
Element Actions, `--headed` and the Agent Cursor. A second fixture
instance is started over the first (`-origin`) and brought to the
foreground with `activate-window`, so the target sits behind another
Window (coverage is checked through DWM frame bounds and the desktop's
z-order). `TestHeadlessClickUnderCoveringWindow`: a Headless click on the
button and on the checkbox changes the fixture's state while the
covering Window stays the foreground window, the real pointer does not
move, and the in-control banner never shows.
`TestHeadlessSetValueKeepsFocusAndForeground`: a Headless set-value
writes the text field with the foreground unchanged and no UI
Automation SetFocus call reaching the fixture (its state file counts
them in `focusRequests`); a `--headed` set-value then does focus first
and takes the foreground, proving the counter is live.
`TestHeadlessClickWithoutPatternRouteFails`: a click on the pattern-less
canvas exits 1 with "nothing was sent" and the exact
`winuse --headed click <window> <index>` retry, with no input reaching
the app (state generation unchanged) and the foreground unchanged.
`TestHeadedClickBringsTheWindowForward`: `--headed` before and after the
command clicks the covered fixture the v1 way, bringing it to the
foreground. `TestAgentCursorShownAndExcludedFromCaptures`: during and
right after a Headless click a visible top-level window of class
`winuse.cursor` exists, owned by the helper, layered, click-through,
tool-window and no-activate, not topmost but above the fixture in the
z-order (multi-agent ticket 04), lying over the clicked button; a
concurrent `get-window-state --screenshot` contains none of its accent
colour, and the cursor is back after the capture. The cursor's own
`WDA_EXCLUDEFROMCAPTURE` keeps it out of any screen capture, so the test
observes it through the window list, not a capture.
`TestEachAgentsCursorSitsAboveItsOwnWindow` (`agent_cursors_test.go`,
multi-agent ticket 04): two Agents named by `WINUSE_AGENT` click on two
covered fixtures through one helper; each fixture has a cursor window
between it and its cover in the z-order, none topmost. Then the covers are
minimized for five seconds: look for two cursors in two colours with
`alpha: click` and `beta: click` in their pills (the cursors are excluded
from captures, so the test cannot check that itself).
`TestTheInsertionCaretShowsAfterASplice` (`insertion_caret_test.go`,
multi-agent ticket 11) **needs Windows PowerShell**: on the test's own
WPF TextBox, left uncovered, `type-text --into id:box XY` after the caret
is put after "hello" must leave a caret bar among the cursor windows (a
few pixels wide, a line high) within its showing; the log says where, for
the user to check it stands right after "helloXY".

Since Element clicks are Headless by default, the tests that rely on a
real-pointer click pass `--headed`: the Batches that click the text field
to focus it before typing (`TestBatchTypesThenClicksAndSucceeds`, both
Esc tests — the field offers no Headless click route) and the
scroll-into-view click (`TestClickScrollsOffscreenElementIntoView`; a
Headless Select does not scroll).

Since headless-max ticket 21 (ADR 0013 rule 3), winuse never goes Headed
on its own: an Action whose only route is Headed is refused without
`--headed`, with exit 1, nothing on stdout, "nothing was sent" and the
exact `winuse --headed <command>` retry in the hint. So every test that
relied on the automatic Headed route passes `--headed` too: the `@x,y`
forms (click, scroll, drag, hover, set-value, perform-secondary-action),
double and right clicks on the canvas, Raise, and `type-text` and
`press-key` into the fixture's main window and its Save As dialog, which
draw their own text fields. Typing into the Classic Dialog's Edit,
Return and Escape on the Classic Dialog and the MessageBox, keys and text
into a browser Page through its Browser Connection, and `--post` and
`--into` stay Headless and take no flag.

`TestHeadedOnlyActionsAreRefusedWithoutTheFlag` (`headed_gate_test.go`)
pins the refusal on the covered fixture, after a `get-window-state
--screenshot` so the point forms have a mapping: a double click and a
right click on the canvas Element, a click, a hover and a drag at `@x,y`
points, `press-key Control_L+a`, `type-text` into the main window and
`perform-secondary-action Raise` on the window Element are each refused
with exit 1, empty stdout, "nothing was sent" and the exact
`winuse --headed ...` retry, with the cover still the foreground window,
the pointer unmoved, no banner, and the fixture's state generation
unchanged (no canvas click, drag or scroll, no toolbar, no text, no key,
and the fixture not raised above the cover). Then a Batch
`click id:button-click then press-key Control_L+a then click
id:checkbox-notify` prints step 2's refusal with nothing sent and the
retry between two `ok` lines, and exits 1 naming step 2 (desktop-parity
20): the first click took effect, no key reached the fixture, and the
checkbox step ran. The same Batch with `--stop-on-error` stops with
"batch stopped at Action 2 (press-key)", and the checkbox step never runs.
Its click at a point aims at the canvas, which offers no click route, so
headless-max ticket 11 refuses it too.

`TestPointFormsActHeadlessOnTheCoveredWindow` (`point_headless_test.go`,
headless-max ticket 11): on the covered fixture, after a screenshot, a
left click at the Click Me button's centre and at the Alpha list item's,
`set-value on` at the checkbox's and a scroll of one notch down and up at
the list each act on the fixture's own Element under the point (the
fixture records the button, the selection, the checkbox and the list's
first visible item: 3 after one notch, three steps, then 0). Each `ok`
names the Element and the point, the cover stays the foreground window,
the pointer does not move and no banner shows. A click at the canvas is
refused with the `--headed` retry and reaches no canvas click.

`TestPostedClicksOnACoveredEdit` (`posted_clicks_test.go`, headless-max
ticket 12): the covered fixture's Classic Dialog gets "alpha bravo" in its
name Edit, then a screenshot. `click @x,y --count 2 --post` a few pixels
into the Edit prints the posted output and selects the first word
(EM_GETSEL from 0 to 5 or 6), and `click @x,y --button right --post` opens
the Edit's context menu (a `#32768` Window of the fixture, closed with the
fixture). The cover stays the foreground window and no banner shows. The
posted wheel (`scroll @x,y --post`) is not run: the fixture has no classic
list box.

`TestChromiumMessagesOnACoveredBrowser` and
`TestChromiumMessagesOnACoveredElectron` (`chromium_messages_test.go`,
headless-max ticket 20): the first Chromium browser found, or the
`WINUSE_E2E_ELECTRON` electron.exe (skipped without it), is started
directly on the fixture page with no debugging port, so winuse has no
Browser Connection to it (the Window State says the Page is read through
UI Automation). With the browser covered, a double click on the page's
input (window messages, no activation pair), `type-text`, `press-key
Control_L+a` and a second `type-text` each print their window-messages
`ok`, the Window State shows the input's value after each typing, and
the cover stays the foreground window with no banner.

`TestCtrlClickOnASingleSelectListIsRefused` (`modifiers_test.go`,
desktop-parity ticket 06): `click <item> --modifiers ctrl` on an item of
the covered fixture's list, which is single-select, is refused with
nothing sent and the exact `--headed` retry; the selection is unchanged
and the cover keeps the foreground.
`TestCtrlAndShiftClickSelectInAMultiSelectList` is the positive leg: the
Classic Dialog's fruit list box (LBS_EXTENDEDSEL, its last control, so no
other Element Index moves) takes `click name:Apple --modifiers ctrl`, then
Cherry, then `--modifiers shift` on Elder (the range from Cherry) and Ctrl
on Apple again (deselected), each Headless with the cover in front and no
banner; the test reads the selection back with LB_GETSEL after each.

Run only these tests:

```
set WINUSE_E2E=1
go test -tags e2e -v -run "Headless|Headed|AgentCursor" ./test/e2e
```

The headless-parity tests (ADR 0011, `.scratch/headless-parity/`) cover
the Actions that became Headless after ADR 0010. Each one covers its
target the same way (a second fixture instance in front, brought to the
foreground with `activate-window`) and checks that the covering Window is
still the foreground window afterwards. The shared helpers are in
`headless_parity_test.go`.

- `TestHeadlessScreenshotAndZoomOfCoveredWindow` (`headless_parity_test.go`):
  `get-window-state --screenshot` and `zoom` of the covered fixture. The
  target has a list item selected and the cover does not, so the image
  must contain the selected item's fill colour: a capture of the screen
  region would show the cover instead. Also checks that the image is not
  blank, the pointer did not move and the in-control banner never showed.
- `TestHeadlessScreenshotRefusesMinimizedWindow`: after `minimize-window`,
  the screenshot and zoom fail with "is minimized; there is nothing on
  screen to capture" and the `restore-window` hint. The Window stays
  minimized and the foreground does not change.
- `TestHeadlessActionsOnMinimizedWindow` (`minimized_test.go`,
  headless-max ticket 14): on the minimized fixture, a Headless `click`
  toggles the checkbox and `set-value` fills the text field, while Element
  `scroll` and the screenshot are refused with "is minimized". The Window
  stays minimized, and the foreground and the pointer do not change.
- `TestWindowStatesArePerAgent` (`agents_test.go`, multi-agent ticket 03):
  two Agents named by `WINUSE_AGENT` (alpha and beta) against one fixture
  through one helper. After alpha reads the Window, beta's click is refused
  with "has not been read by this agent" and nothing sent; after beta
  reads too, alpha's click still lands on alpha's Element, then beta's; the
  foreground stays put; `winuse status` lists both Agents.
- `TestSetValueOnOffTheCheckbox`: `set-value` with `on`, `ON`, `checked`,
  `off`, `unchecked`, `True` and `false` on the covered checkbox. Setting
  the state it already has sends nothing (the fixture's generation stays
  put), and `maybe` is refused with "nothing was sent".
- `TestWindowActionsKeepTheForeground` (`window_actions_test.go`):
  `minimize-window`, a `move-window` refused while minimized,
  `restore-window`, `move-window` (read back through GetWindowRect within
  8 px, and through the fixture's own canvas rectangle), `resize-window`
  and `close-window`, which must end the fixture process within 5 s.
  The fixture is activated once before it is covered, so `restore-window`
  prints a plain `ok`. An App that has not been the foreground since it
  started can take the foreground when it is first restored (ticket 02's
  2026-11-03 comment); without that activation the fixture sometimes did.
- `TestRestoreWindowReportsSelfActivation` (`window_actions_test.go`,
  headless-max ticket 02): the fixture with `-activate-on-restore`,
  covered by an instance that grants it the foreground
  (`-allow-foreground`), is minimized and restored. It must take the
  foreground, and `restore-window` must print
  `ok (the App brought itself to the foreground)`. It used to start
  charmap, which the research measured doing this, but charmap only does
  it when it has not been the foreground since it started, and whether a
  process the test starts takes the foreground at launch depends on the
  desktop's input state, so it skipped on most runs.
- `TestMaximizeWindowNeedsHeaded` (`window_actions_test.go`,
  desktop-parity ticket 11): on a covered fixture, `maximize-window`
  without `--headed` is refused with "nothing was sent" and the `--headed`
  retry, leaving the fixture at its size and the cover in front;
  `--headed maximize-window` brings the fixture to the foreground and
  maximizes it (IsZoomed), and `restore-window` returns it to its size.
- `TestCloseAppClosesTwoWindowsWithOneCommand` (`close_app_test.go`,
  desktop-parity ticket 10): two instances of a uniquely named copy of the
  fixture, closed by one `close-app <name>`, which prints `ok` once both
  processes have exited; a plain fixture beside them stays open. The copy's
  name keeps close-app away from any process the test did not start.
- `TestCloseAppForceEndsAnAppThatStaysOpen` (`close_app_test.go`): the
  copy with `-ignore-close` stays open on `close-app`, which prints the
  still-running `ok`; `close-app --force` then ends its process. The
  protected-process refusal is tested offline only: a live test would aim
  at the user's own explorer.exe.
- `TestLaunchAppKeepsTheForeground`, `TestLaunchAppMinimized` and
  `TestLaunchAppNotepadReportsTheForeground` (`launch_noactivate_test.go`,
  headless-max ticket 01): a cover fixture is made the foreground with
  `activate-window`, whose input gives the helper the foreground right, so
  only the launch's non-activating show state keeps the cover in front.
  `launch-app` starts the fixture by path, with its state file through
  `--arg`, its origin through `--env WINUSE_FIXTURE_ORIGIN` and a `--cwd`;
  the cover must stay the foreground (also a second later), the output is
  the Window line alone, and the fixture opens at the origin. With
  `--minimized` the Window opens minimized. Windows 11 Notepad, launched by
  its AUMID, may take the foreground (packaged activation has no option
  against it); the test demands only that the `Note: the App took the
  foreground` line matches what happened, and logs which it was. It skips
  when Notepad is already running, and ends the Notepad it launched.
- `TestElementScrollByStepsAndIntoView` (`element_scroll_test.go`): the
  list scrolls by steps (2, then the default 3, which stops at the end),
  fails at the end with "the Element did not scroll; it may already be at
  the end", scrolls through a list item (the nearest scrollable
  ancestor), and brings Hotel into view. Scrolling with no scrollable
  ancestor on the axis and `into-view` on the checkbox are refused with
  "nothing was sent". The fixture's `listFirstVisible` counts the steps
  exactly.
- `TestHeadlessReturnAndEscapeOnClassicDialog` and
  `TestHeadlessReturnAndEscapeOnMessageBox` (`classic_keys_test.go`):
  `press-key Return` closes the Classic Dialog or the MessageBox with
  "ok", and `press-key Escape` with "cancel".
- `TestHeadlessTypeTextIntoClassicEdit`: `type-text` into the Classic
  Dialog's Edit (non-ASCII text, then an appended character).
- `TestActionReportsOpenedAndClosedWindows` (`opened_windows_test.go`,
  desktop-parity 02): the Headless click on "Classic Dialog..." prints
  `ok` and `Note: a new Window opened: <id> <App> "Classic Dialog"`,
  `get-window <id>` finds that Window, and a Headless Escape on it prints
  `Note: window <id> closed`. The foreground stays put. Every other test
  accepts such note lines after `ok` and logs them (`assertActionOK`).
- `TestNameReferenceAfterHelperRestart` (`live_refs_test.go`,
  desktop-parity 17): after `get-window-state`, the helper is killed, and
  a click by `name:Click Me` and a set-value by `id:text-field` still act
  through a fresh read, each with its `Note: found ... in a fresh read`
  line. The same name again needs no fresh read, and `name:Twin` after
  another restart refuses with both candidates. The cover keeps the
  foreground.
- `TestFindAndRegionReadOnTheFixture` (`find_test.go`, desktop-parity
  04): after a full read, `find --role "list item" --name charlie` prints
  Charlie with an index after the read's last, and a click on it selects
  Charlie; a click on Alpha's index from the full read still works.
  `--count` prints 8, a `slider` role miss lists the roles present,
  `--root <list> --depth 1` prints the list and its 8 items, and
  `--skeleton` ends with its note. The cover keeps the foreground.
- `TestHeadlessClickReportsSelfActivation` (`self_activation_test.go`,
  headless-max 03): the same click on the covered fixture must end with
  `Note: the App took the foreground` exactly when the fixture holds the
  foreground right after it, and never otherwise. The App only takes the
  foreground when the foreground lock has lapsed (in the probe, the
  user's last-input process had exited), so the log says which case ran.
- `TestHeadlessTypeTextIntoPasswordEdit`: a posted Tab moves the focus to
  the password Edit, then `type-text` fills it and the output is exactly
  `ok`, without the text.
- `TestHeadlessTypeTextIntoNotepad` **needs the Windows 11 Notepad**
  (RichEditD2DPT editor) and skips otherwise. It also skips while Notepad
  is running, because it ends the Notepad process it types into. It opens
  an empty temp file, covers it, and types with `type-text --into` aimed
  at the `Text editor` Element from `get-window-state`. Covered, Notepad
  reports no focused control, so plain `type-text` has no Headless route
  (headless-max ticket 19): the test sends it first and wants it refused
  with "nothing was sent", the `winuse --headed type-text <window> <text>`
  retry and the `--into` alternative, the cover still in front and the
  text not in the editor (ticket 21; it used to go Headed on its own and
  print `ok (typed Headed: ...)`). It reads the editor's text back with
  WM_GETTEXT and wants the foreground unchanged and no banner. If
  Notepad restores sessions, its next start may show that temp file's
  tab with the test text: close it.
- `TestHeadlessTypeTextIntoElement`: with the focus in the covered
  Classic Dialog's name Edit, `type-text --into id:1002` fills the
  password Edit (the name Edit stays empty), a Batch step `type-text
  --into id:1001` fills the name Edit, and `--into id:1` (the OK button)
  is refused with "nothing was sent" and the `--headed` retry. The
  foreground and the pointer stay put.
- `TestTypeTextIntoAWPFBoxSplicesAtTheCaret` (`caret_typing_test.go`,
  headless-max ticket 13) **needs Windows PowerShell**, which shows the
  test's own WPF TextBox (`id:box`, "hello world") without activating it.
  With the box covered, `select-text id:box 5 0` puts the caret after
  "hello" and `type-text --into id:box xy😀` splices the text there:
  the box reads `helloxy😀 world` and the output says no key events
  fired. A second `--into` must follow the caret (or the output must say
  it could not be placed, logged as MEASURED). The foreground and the
  pointer stay put, and no banner shows.
- `TestHeadlessSelectTextInClassicEdit` (`select_text_test.go`): types
  `hello world` into the covered Classic Dialog's Edit, `select-text`
  6 5 on it by `id:1001`, then `type-text winuse` replaces the selection
  (`hello winuse`); `select-text all` and a replacement of the whole
  text follow, and a span past the end is refused with "nothing was
  sent". The foreground and the pointer stay put.
- `TestHeadlessSelectTextInNotepad` **needs the Windows 11 Notepad** and
  skips like the type-text Notepad test: `select-text` on the `Text
  editor` Element, then `type-text` replaces the selection, read back with
  WM_GETTEXT; then `all`.
- The Page input is covered in `TestBrowserPages`'s page Actions:
  `select-text` 6 4 on the text field, read back through its own
  `selectionStart` and `selectionEnd`, then `type-text` replaces it, then
  `all`. The UI Automation Text pattern route has no e2e target yet.
- `TestHeadlessInvokeMenuOnClassicDialog` (`invoke_menu_test.go`): the
  covered Classic Dialog has a real menu bar. `invoke-menu "Edit > Select
  All"` selects the name Edit's text, which a `type-text` then replaces;
  `Edit > Clear Name`, greyed until the dialog's WM_INITMENUPOPUP enables
  it, empties the Edit (`classicMenuInits` must be above 0); `Edit >
  Locked`, always greyed, is refused with "nothing was sent"; `file >
  FILL NAME` matches without case and &; a Batch step runs Select All
  before a `type-text`; and the main window's drawn menu bar is refused
  as "no classic menu bar". The foreground and the pointer stay put, and
  no banner shows.
- `TestPostedKeysIntoClassicEdit` (`posted_keys_test.go`):
  `type-text --post` and `press-key BackSpace --post` reach the Classic
  Dialog's Edit, the output starts with `ok (posted;`, and
  `Control_L+Alt_L+a --post` (Control and Alt together) is refused with "nothing was sent".
- `TestPostedChordFiresAClassicAccelerator` (`posted_chords_test.go`,
  headless-max ticket 07): `press-key Control_L+Shift_L+k --post` on the
  covered fixture fires its main window's accelerator
  (TranslateAccelerator, `accelerators` 1) with no activation; the
  foreground stays put and the user's Control key does not read as down.
- `TestPostedChordFiresAWPFKeyBinding` **needs Windows PowerShell**, which
  shows the test's own WPF window (KeyBinding Ctrl+S) without activating
  it. With it covered, `press-key Control_L+s --post` fires the binding
  (through the activation pair), the foreground stays put and the user's
  Control key does not read as down.
- `TestPostedKeysRefusedOnBlockedAndMinimizedWindows`: posted keys and
  text into the main window are refused while the Save As dialog blocks
  it and while it is minimized, and no key reaches the fixture.
- `TestPostedReturnIntoCoveredChromeAddressBar` **needs Chrome** (Edge
  does not count) and skips without it. It is a measurement, not an
  assertion: Chrome starts with an isolated temp profile on
  `about:blank`, its address bar is set to a `data:` URL with
  `set-value`, and Return is posted. Read the `MEASURED:` line in the
  `-v` output: it says whether Chrome navigated. The test fails only if
  winuse's own output is wrong or the foreground changed.

What to look at when one fails. The keyboard routes are decided from the
live Window: the focused control that GetGUIThreadInfo reports for its
thread, and the dialog's default button. If the probe reads something
other than the offline tests assume, the Action has no Headless route
and, sent without `--headed`, is refused with "nothing was sent" (it no
longer goes Headed on its own, headless-max ticket 21), so the test fails
on the exit and the refusal says why. The tests
that use these routes log a `key route probe` line first, with the focus
class, its style and the default button. The risks the tickets left to
measure live are:

- whether a background thread's focus reads back (Classic Dialog,
  password Edit, Notepad);
- whether a MessageBox answers DM_GETDEFID or has its default set only by
  focus;
- whether Notepad's RichEditD2DPT accepts EM_REPLACESEL from another
  process while in the background;
- whether Chrome acts on a posted Return (the `MEASURED:` line).

Run only these tests:

```
set WINUSE_E2E=1
go test -tags e2e -v -run "Headless|SetValueOnOff|WindowActions|ElementScroll|Posted|OpenedAndClosed|SelfActivation|FindAndRegionRead|NameReferenceAfterHelperRestart" ./test/e2e
```

## The clipboard test

`TestClipboard` (`clipboard_test.go`, desktop-parity ticket 01) runs
`clipboard-set`, then `clipboard-get`, for text, an image and files, and
`clipboard-clear`, through the real binary. Windows PowerShell is the
program on the other side: it reads the text, the image (through the
CF_DIB winuse writes beside the PNG) and the file list winuse wrote, and
puts a .NET bitmap on the clipboard that winuse reads back (the CF_DIBV5
Windows synthesizes from it). A missing `--file` is refused with nothing
changed, and the foreground must not move.

The clipboard is the user's. The test saves its text first and puts it
back at the end, or empties it again if it was empty. It skips when the
clipboard holds any format beyond plain text, which it could not put
back: an image, files, or text with extra formats, such as what a
browser, .NET's `Set-Clipboard` or a password manager copies (the last
marks it `ExcludeClipboardContentFromMonitorProcessing`). Copy plain text
from Notepad, or empty the clipboard, to run it. Its writes land in the
clipboard history (Win+V) when that is on.

```
set WINUSE_E2E=1
go test -tags e2e -v -run "^TestClipboard$" ./test/e2e
```

## The display tests

`displays_test.go` (desktop-parity ticket 08) launches nothing and sends
no input. `TestListDisplaysAndCapturePrimary` checks `list-displays`
against the system's own metrics, read per-monitor DPI aware: as many
lines as `SM_CMONITORS`, one primary, numbered 0 and at 0,0 with the
`SM_CXSCREEN` x `SM_CYSCREEN` size. Then `screenshot --display 0` must
print the Screenshot line for that rectangle and the no-`@x,y` note, save
a JPEG of the printed size with its long edge at most 1568 px and not one
colour, log a Windows.Graphics.Capture display capture (unless this
Windows has none), and leave the foreground where it was. A display past
the last one is refused. `TestDisplayScreenshotFallsBackToTheScreenRegion`
restarts the helper with `WINUSE_WGC=off`, so the display is captured
through its screen region, and restarts it again afterwards.

```
set WINUSE_E2E=1
go test -tags e2e -v -run "Displays|DisplayScreenshot" ./test/e2e
```

## The browser merge probe

`TestBrowserProbe` (`browser_probe_test.go`, ticket 03 of
`.scratch/browser-merge/`) is a measurement, not a test. It answers what
tickets 04 to 07 need to know before they are built, and fails only when
its own setup breaks (no launch, no connection, no window). Every finding
is logged on a `MEASURED:` line and written to a report file.

It runs against Chrome, then Edge, and skips a browser that is not
installed. Each browser starts through the real `winuse browser launch`, in
winuse's own launch profile, so the user's own browser and profile are
never driven. A fixture Window covers the browser and holds the
foreground. Per browser, it records:

1. **Window to Tab mapping.** With 3 windows open and one title repeated in
   a background tab: for each `Chrome_WidgetWin_1` Window, whether
   `Browser.getWindowForTarget` bounds plus the active tab's title pick
   exactly one tab, with the raw bounds and DPI.
2. **Foreground safety.** The foreground and z-order before and after
   `Target.createTarget {background:true}`, the same with `newWindow`, and
   whether `winuse browser launch` itself took the foreground.
3. **Screen coordinates.** On `testdata/browser-probe.html`, the error
   between the UIA document top-left plus CSS rect x `devicePixelRatio`
   and UIA's own bounds for the same Element, at page zoom 100%, 125% and
   150%. The zoom keys are `--headed press-key Control_L+equal` on the
   probe's own browser Window, so that Window comes to the front for a few
   seconds; the zoom is reset to 100% afterwards.
4. **Covered and minimized windows.** `Accessibility.getFullAXTree`,
   `Input.insertText` and a `Input.dispatchMouseEvent` click while the
   browser is uncovered, fully covered and minimized.
5. **Posted keys in the address bar.** `type-text --post` and
   `press-key Return --post` into a new window, then `set-value` plus a
   posted Return, and whether the tab navigated.

It also records the port owner check, whether `/json/version` or the dial
raised a window, and whether the browser outlives the CDP connection and
the helper. At the end it closes the browsers it launched, removes the
launch profile if the run created it, puts back the browser pin, and gives
the foreground back to the Window that had it.

**The extension questions** (the `chrome.windows` ids in question 1, the
tab create and `chrome.tabs.update` activation in question 2) use the
user's own browser and run only when the winuse extension is connected to
it. To include them, first run `winuse browser ext install chrome` (and
`edge`), follow the printed steps, and check with
`winuse browser ext status chrome` (exit 0). The probe then opens one
background tab there, switches to it and back, and closes it. The user's
tab titles are not written to the report. `ext status` updates the
extension when its build differs from this checkout. Without the
extension, the report says "extension not connected, skipped".

**Before and between runs.** Other windows of Chrome or Edge can stay
open: the probe uses its own profile. Do not use the mouse or keyboard
while it runs (about 2 minutes per browser). Run it once at each display
scale, 100%, 150% and 175% (Settings > System > Display > Scale), with
nothing else changed. Each run writes
`.scratch/browser-merge/probe-run-<browser>-<scale>-<time>.md` at the
repo root and logs the path. `WINUSE_PROBE_REPORT` may name another folder,
or a `.md` file (the browser name is added before the extension). Copy the
results into `.scratch/browser-merge/probe-results.md`.

```
set WINUSE_E2E=1
go test -tags e2e -v -timeout 20m -run TestBrowserProbe ./test/e2e
```

## The browser page tests

`browser_pages_test.go` (ticket 09 of `.scratch/browser-merge/`) drives
every Page Action through the winuse CLI, as an agent would, against real
Chrome and Edge. It skips a browser that is not installed. A fixture
Window covers the browser and holds the foreground. After each Action the
test checks four things: the fixture is still the foreground window, it
kept its place in the z-order, no browser Window rose above it, and no
native dialog Window of the browser appeared. Then it reads the Page back
with `winuse eval` to check that the Action took effect.

The pages are `testdata/browser-pages.html`, `browser-pages-frame.html`
and `browser-pages-next.html`. Two loopback httptest servers serve them.
The second server is addressed as `localhost`, another site than
`127.0.0.1`, so its iframe is cross-origin and out of process.

`TestBrowserPages` starts each browser through the real
`winuse browser launch`, in winuse's own launch profile, so the user's own
browser and profile are never driven. It skips the browser when that
profile is already running: close it with `winuse browser stop chrome`
first. The first step points the first tab, which shows the browser's
New Tab page, at the test page with `winuse navigate`. If that fails, the
test reports it and does the step over a CDP connection of its own, so the
later subtests still run. Its subtests:

- `page-actions`: click (single and `--count 2`); set-value on a text
  field, a select (by label) and a check box (on, then off); a click into
  the field, `press-key Control_L+a` and `type-text`, which replaces the
  text; `press-key Control_L+Shift_L+k`, seen by the page as that chord;
  `scroll` by steps on a list row and `into-view` on the last row; and
  `hover`.
- `drag-and-pointer-capture`: an HTML5 drag and drop between two Elements,
  and a slider that follows the pointer only through pointer capture,
  dragged to an Element outside its track.
- `dialogs`: a click that opens an alert fails and names the dialog. While
  the alert shows, click, set-value, eval and navigate fail within 5 s
  with "nothing was sent", and the Window State carries the
  `Note: the Page is read through UI Automation because it shows an alert
  dialog` line. Then `dialog accept` answers the alert (the blocked click
  never landed), `dialog dismiss` answers a confirm (the page sees false),
  `dialog accept <text>` answers a prompt, and `dialog` with none open
  fails.
- `cross-origin-iframe`: the iframe's button and field are in the Window
  State, and take a click and a set-value.
- `upload`: `upload` on a file input; then a click that opens a file
  chooser says so, no native file dialog Window appears, and `upload`
  without an Element answers the chooser.
- `download`: a click on a download link says it started a download, and
  `wait download` prints the path. The file must be there, with the
  served name and content. It lands in winuse's download folder
  (`%USERPROFILE%\Downloads\winuse` unless the Downloads folder is
  redirected), and the test removes it.
- `navigation-wait-eval`: `navigate`, `back`, `forward` and `reload`
  (a value set by eval is gone after the reload); `wait` for a selector,
  text, a URL pattern and the load; and `eval`.
- `tabs`: `tab new` opens a background tab (the active tab and the
  Window's title stay), `tab list` lists it, `tab switch` makes it the
  active tab and back through the covered Window's tab strip (Headless,
  headless-max 05: the foreground, the z-order and the Window's title are
  checked), and `tab close` closes it.

`TestBrowserPagesExtension` runs a short subset in the user's real browser
through the winuse extension. It runs only when
`winuse browser ext status chrome` (or `edge`) exits 0, and logs a skip
otherwise. In the user's topmost browser Window, under the covering
fixture, it opens one background tab with `tab new` and switches to it
with `tab switch` (Headless through the extension). On that tab it runs
click, set-value and eval. Then it switches back to the user's tab and
closes its own. The user's tabs are never acted on; the Window shows the
test's tab for a few seconds.

**What they need.** Chrome installed (Edge too, to cover it; neither is
required). The extension is optional. Do not use the mouse or keyboard
while they run (a few minutes per browser). Both tests put the browser
pin and the foreground back, and close every tab, window and browser they
opened, the launched browser's new profile, the fixture and the helper.
One helper serves a whole browser's run (a 10 minute idle time, killed at
the end), because the open dialog, the held file chooser and the download
tracking live in it. If that helper restarted while the extension is
connected to the same browser, its next connection would go through the
extension instead of the launched browser, and the Page Actions would
fail because the Window belongs to another Chrome process.

```
set WINUSE_E2E=1
go test -tags e2e -v -timeout 20m -run "TestBrowserPages" ./test/e2e
```

`-run "TestBrowserPages$/chrome/dialogs"` runs one subtest, and
`-run TestBrowserPagesExtension` only the extension subset.

## The occlusion flag test

`browser_occlusion_test.go` (ticket 04 of `.scratch/headless-max/`)
checks that a browser winuse launches keeps building its UI Automation
tree while it is covered: `winuse browser launch` passes
`--disable-features=CalculateNativeWinOcclusion`. For Chrome, then Edge
(a browser that is not installed is skipped), it launches the browser in
winuse's own launch profile, covers the new Window with the fixture as
soon as it is steady (the one Window winuse lists, unchanged for half a
second; both browser tests pick the launched Window this way and again
on each retry), opens `testdata/browser-pages.html` with `winuse navigate`, and
then reads the Window State until it shows the address bar (the frame)
and the Page's "e2e count" button, through the Browser Connection. Both
must show within 5 s, and the fixture must keep the foreground. It skips
a browser whose launch profile is already running, since a warm browser
says nothing about a cold one: close it with `winuse browser stop chrome`
first. It closes the browser, the launch profile it created, the fixture
and the helper, and puts the pin and the foreground back.

```
set WINUSE_E2E=1
go test -tags e2e -v -timeout 10m -run TestLaunchedBrowserCoveredKeepsItsTree ./test/e2e
```

## The cold Electron settle test

`electron_settle_test.go` (ticket 18 of `.scratch/headless-max/`) reads
a covered Electron App cold. An Electron App has no Browser Connection,
so its Window State comes from UI Automation alone, and get-window-state
waits up to 5 s for a thin Chromium tree to settle. The test needs the
prebuilt Electron binary: set `WINUSE_E2E_ELECTRON` to `electron.exe`
from an Electron release (it skips without it). It starts Electron with
an isolated profile on `testdata/chromium-fixture.html`, covers the new
Window with the fixture at once, and reads its Window State once. The
read must answer within the 10 s request budget, the fixture must keep
the foreground, and the Window State must show either the page's
"winuse e2e button" or the thin-page note ("the page's accessibility
tree is still thin"), not both; the note must come after about 5 s of
settling. The log says which one happened. It closes Electron, the
fixture and the helper, and puts the foreground back. Measured on the
development PC with Electron 45.2.1 (ticket 18, 2026-11-03): a fully
covered cold Electron never built its web tree, even after 60 s, so the
test logs the thin note after about 5.3 s; with a 120 px strip left
uncovered, the first read showed the page in under 0.9 s.

Get the binary from an official Electron release, outside the repository
(the folder below is an example; any place works):

1. Open https://github.com/electron/electron/releases and pick a recent
   stable release (not a pre-release), e.g. `v45.2.1`.
2. Download `electron-vX.Y.Z-win32-x64.zip` and the release's
   `SHASUMS256.txt`, and check the zip's SHA-256 against it
   (`certutil -hashfile electron-vX.Y.Z-win32-x64.zip SHA256`).
3. Unzip it into its own folder, e.g.
   `C:\electron-dist\electron-vX.Y.Z-win32-x64\`. `electron.exe` is at
   the top of that folder.

```
set WINUSE_E2E=1
set WINUSE_E2E_ELECTRON=C:\electron-dist\electron-vX.Y.Z-win32-x64\electron.exe
go test -tags e2e -v -timeout 5m -run TestCoveredElectronReadCold ./test/e2e
```

## Electron and WebView2 Apps launched with --cdp

`app_cdp_test.go` (ticket 15 of `.scratch/headless-max/`).
`TestLaunchAppCDPOnCoveredElectron` uses the same `WINUSE_E2E_ELECTRON`
binary: `winuse launch-app <electron.exe> --cdp` on the fixture page with
an isolated profile must print the Window line and
`Note: a Browser Connection reaches the App's pages (Electron App, port
<n>)`. The fixture then covers the Window; get-window-state must show the
page's "winuse e2e input" field read over CDP (no "the Page is read
through UI Automation" note), a `set-value` on it must answer `ok`, and
`winuse eval` must read the value back. The cover keeps the foreground
throughout. `TestLaunchAppCDPOnWebView2` needs `WINUSE_E2E_WEBVIEW2`, an
executable that hosts a WebView2 control (it skips without it): the
launch must print the WebView2 note, and the covered Window's State must
come over CDP; the test logs it. `TestAttachToRunningElectron` starts
the same electron.exe the plain way (no debugging port), covers it, and
runs `winuse browser attach <window>`: the Window State must then show
the field read over CDP, `set-value` must be read back by `eval`, and
`winuse browser detach <window>` must answer `ok (detached` after which
`eval` fails. It sends the Node debug signal into the test's own Electron
process only. All three end the App, the fixture and the helper.

```
set WINUSE_E2E=1
set WINUSE_E2E_ELECTRON=C:\electron-dist\electron-vX.Y.Z-win32-x64\electron.exe
set WINUSE_E2E_WEBVIEW2=C:\path\to\WebView2App.exe
go test -tags e2e -v -timeout 5m -run "TestLaunchAppCDP|TestAttachToRunningElectron" ./test/e2e
```

## Shell surfaces (desktop-parity 09)

`test/e2e/shell_test.go` runs against the user's own taskbar: there is no
fixture shell. `TestShellReadsTheTaskbar` only reads (`list-windows
--shell`, the taskbar's Window State, the foreground unchanged) and runs
with the usual opt-in. The other two act on the shell and skip unless
`WINUSE_E2E_SHELL=1` is set as well, only when the user agreed:
`TestShellRefusesWithoutHeaded` checks that Start's button and
Control_L+Escape are refused without `--headed` (a regression would open
Start), and `TestShellOpensAndClosesTheTrayOverflow` opens the tray
overflow with a `--headed` click on the hidden-icons chevron (it takes
the foreground), reads its icons, closes it with the Headless Escape, and
activates the Window that was in the foreground before.

```
set WINUSE_E2E=1
set WINUSE_E2E_SHELL=1
go test -tags e2e -v -run "TestShell" ./test/e2e
```
