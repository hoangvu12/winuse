# 09: Opt-in e2e suite for browser Windows

**What to build:** e2e tests against real Chrome and Edge in isolated profiles, using local probe pages:

- drag and pointer capture
- dialogs
- cross-origin iframes
- upload
- downloads
- every page Action with another Window covering the browser, and the foreground unchanged afterwards

Write and compile only. The user runs them.

**Blocked by:** 06, 07

**Status:** ready-for-human (the user runs it)

- [x] `go vet -tags e2e ./test/e2e` passes
- [x] test/e2e/README.md lists the tests and their needs

## Comments

### 2026-11-02: written

Written and compiled only; not run. The tests drive winuse through its CLI as an agent would.

**Files.**
- `test/e2e/browser_pages_test.go` (new).
- `test/e2e/testdata/browser-pages.html`, `browser-pages-frame.html` and `browser-pages-next.html` (new).
- `test/e2e/README.md`: a new "The browser page tests" section.
- The probe's helpers are reused as they are: `browserWindows`, `listPages`, `currentBrowserPin`, `restoreBrowserPin`, `restoreForeground` and `closeBrowser`, plus `coverWindow` and `coverAgain`. The probe's file is unchanged.

**How it runs.**
- Two loopback httptest servers serve the pages. The second is addressed as `localhost`, another site, so its iframe is out of process.
- A fixture Window covers the browser and holds the foreground.
- After every Action, a guard checks the foreground, the fixture's z-order rank (leaving out `winuse.*` windows and untitled browser popups), that no browser Window is above the fixture, and that no `#32770` Window of the browser appeared. It closes any that did. `winuse eval` then reads the effect back.
- One helper serves each browser's run (10 minute idle, killed at the end), so the dialog watch, the held chooser and the download tracking stay in one helper.
- One setup step goes around winuse: the first tab is pointed at the page over the test's own CDP connection. winuse cannot navigate a tab that shows `chrome://newtab` (an internal page has no Tab).
- If winuse's launch profile is already running, the browser is skipped with a `winuse browser stop` hint, so a session in use is never driven.

**Tests.**
- `TestBrowserPages/<chrome|edge>/...`:
  - `page-actions`: click, `--count 2`; set-value on a text field, a select and a check box (on, off); click, `Control_L+a`, type-text; the `Control_L+Shift_L+k` chord; scroll by steps and into-view; hover.
  - `drag-and-pointer-capture`: HTML5 drag and drop Element to Element; a pointer-capture slider dragged to an Element outside its track.
  - `dialogs`:
    - an alert click fails and names the dialog;
    - while it shows, click, set-value, eval and navigate fail within 5 s with "nothing was sent", and the Window State has the Note line;
    - accept (the blocked click never landed), confirm dismissed (false), prompt answered with text;
    - `dialog` with none open fails.
  - `cross-origin-iframe`: the iframe's button and field are in the Window State, and take a click and a set-value (reported through postMessage).
  - `upload`: `upload` on a file input; a click that opens a chooser says so, no native dialog Window appears, and `upload` without an Element answers it.
  - `download`: the click says it started a download, `wait download` prints the path, and the name and content are checked. The file is removed.
  - `navigation-wait-eval`: navigate, back, forward, reload; wait selector, text, url pattern and load; eval.
  - `tabs`: tab new in the background (the active tab and the Window title stay), tab list, tab close. tab switch is Headed on a launched browser, so it is logged and not run.
- `TestBrowserPagesExtension/<chrome|edge>`: runs only when `winuse browser ext status <b>` exits 0 and exactly one bridge is listed, and logs a skip otherwise. In the user's topmost Window, under the fixture, it does tab new, then tab switch (Headless). On the test's own tab it runs a click, a set-value and eval. Then it switches back to the user's tab, closes its own, and checks the tab list.

**Run command (user only):**

```
set WINUSE_E2E=1
go test -tags e2e -v -timeout 20m -run "TestBrowserPages" ./test/e2e
```

**Not covered, or soft:**
- Right click and Chrome's context menu.
- `scroll @x,y`, which needs a screenshot.
- Edge, which is not installed on this PC.
- The extension's iframe, upload and dialog paths.
- If the click's download note is lost to event timing, that is an error, but `wait download` is still checked.

**Checks run:** `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are green. go.mod is unchanged. The new files have LF endings and no em or en dashes.

### 2026-11-02: clean run (Chrome 155, user away)

- `TestBrowserPages/chrome`: all 8 subtests pass (page-actions, drag-and-pointer-capture, dialogs, cross-origin-iframe, upload, download, navigation-wait-eval, tabs). The foreground jump seen in the first run's cross-origin-iframe subtest did not recur, so it was the user's input. Edge is not installed.
- `TestBrowserPagesExtension/chrome` passes after two findings:
  - With the user's Chrome on its New Tab page only, `tab list` fails ("no Tab of the Browser Connection matches"), because internal pages are not Tabs. Same root as the navigate open item. The run gave the window example.com first.
  - The z-order guard counted the taskbars (`Shell_TrayWnd`, `Shell_SecondaryTrayWnd`), which drop in and out of the visible set. The guard now skips them, and a rank failure names the windows above the fixture.
- Before the run the user's Chrome was still on extension 0.1.0 while the folder held newer files. The extension reports its build from `build.json` on disk, read at each connect, not from the code Chrome loaded, so a folder rewritten without a reload looks current and self-update never fires. Proposed fix: stamp the build into the written manifest (for example `version_name`) and report `chrome.runtime.getManifest()`'s value.
- Logs: `e2e-pages2.log` and `e2e-pages-ext3.log` in the orchestrator's scratchpad (`C:\Users\ADMIN\AppData\Local\Temp\claude\C--Users-ADMIN-Desktop-nguyenvu-winuse\0feeef4c-c4e4-4ab5-a8b7-9027b4c68044\scratchpad\`).

### 2026-11-02: after the internal-pages and build-check fixes

- The dev host was rebuilt and the user's Chrome self-updated from 0.1.2 to 0.1.3 live: the old code reported no build once `build.json` was gone, winuse reloaded it, and the loaded manifest now reports `0.1.3 build 1da0c44afc88`.
- By hand, through the extension, on a New Tab page: `tab list` lists it, the Window State note names it, `eval` is refused with the navigate hint, `navigate` leaves it.
- Run 3: `TestBrowserPages/chrome` 7 of 8 (page-actions failed on its first command), `TestBrowserPagesExtension/chrome` pass. Run 4 (`TestBrowserPages` only): 8 of 8. The setup's `winuse navigate` from the New Tab page worked in both runs (no CDP fallback).
- **Intermittent, watch it.** In run 3 the first command after the setup's navigate away from the New Tab page (`navigate` to the same URL) failed the 3 s Ready probe ("did not answer within 3s"). Not reproduced by hand in 5 tries (kept and fresh profiles, uncovered window, example.com). Differences from the hand runs: the covering fixture hides the window, and the local http server. Suspect the renderer swap from the WebUI New Tab page to a site renderer while the window is occluded.
- **Seen by hand:** two Chrome windows at the same default position showing the same site are ambiguous through the extension too ("several Tabs match"); winuse refuses rather than guess (open item 3). A sticky mapping (keep an HWND's last unique windowId while it still exists and its bounds still match) would settle the common case.
- Logs: `e2e-pages3.log`, `e2e-pages4.log` in the orchestrator's scratchpad.

### 2026-11-02: after the per-process connections and the cut walk

- By hand: get-window-state on a dense page (en.wikipedia.org Python article, launched Chrome, element cap reached) took 1.9 to 3.1 s before the cut walk and 0.8 to 1.6 s after, same 3004 lines. The user's Chrome (extension) and a launched Chrome were served side by side from one helper: eval, tab list, navigate, get-window-state on each, `browser status` listing both, `browser stop` leaving the extension connection alive.
- Run 5: `TestBrowserPages/chrome` 5 of 8. dialogs, cross-origin-iframe and upload failed only on the foreground guard: Discord ("@Mirai - Discord") held the foreground from `dialog accept` on, and every functional check passed. Most likely Discord raising itself; rerun to confirm.
- Run 5: `TestBrowserPagesExtension/chrome` failed: after the Headless `tab switch`, UIA keeps the old tab's document (moved to the window's top, y=0) and shows an empty placeholder document (`view_1033`) for the active tab, so the read fell back ("several documents and none is named after the active Tab"). With an equal title the stale document is chosen by name, 143 px off. Sent to a worker (document choice by URL as well as title), together with the remembered Window to Tab mapping.
- Log: `e2e-pages5.log` in the orchestrator's scratchpad.

### 2026-11-02: after the document choice and the remembered mapping

- Run 6 was disturbed: another Claude session (`winuse-a6`, started during the run) drove the desktop. A Character Map window took the foreground twice, a popup menu (`#32768`) rose above the cover, and the shared browser pin was changed to brave (the test restored what it found). Live runs paused until that session is done.
- Every failure in run 6 was the foreground or z-order guard from those windows. All functional checks passed: `TestBrowserPages/chrome` 8 of 8 functionally (cross-origin-iframe and navigation-wait-eval failed on the guard only), and `TestBrowserPagesExtension/chrome` read the Page after the Headless tab switch with no fallback note, which confirms the document-choice fix live.
- Rerun both when the desktop is free. Log: `e2e-pages6.log` in the orchestrator's scratchpad.

### 2026-11-02: run 7, desktop shared with another agent

- `TestBrowserPages/chrome`: 8 of 8 functionally; cross-origin-iframe failed only on the foreground guard (Zeron's window took the foreground). The launched browser used port 9222 this time (9223 before), and the kept launch profile was reused, so no "removed the launch profile" line.
- `TestBrowserPagesExtension/chrome` skipped: the user's Chrome was no longer running. At the start of the run the foreground was a Chrome "Allow remote debugging?" prompt (the Chrome 144+ approval prompt), which this session never caused. Another agent was running its own winuse build (`__helper` from a Zeron scratchpad) at the time. Not investigated further; the user's Chrome was left closed.
- Log: `e2e-pages7.log` in the orchestrator's scratchpad.
