# 04: Occlusion flag for launched browsers

**What to build:** browsers that winuse launches get `--disable-features=CalculateNativeWinOcclusion`, so a covered window keeps rendering and builds its UIA tree.

Chromium marks covered windows occluded through its native occlusion tracker (`ui/aura/native_window_occlusion_tracker_win.cc`; feature `CalculateNativeWinOcclusion` in `ui/base/ui_base_features.cc`). The Laptop probe measured that a cold, fully covered Chrome kept a 36-node UIA tree for 14 s, and settled in about 0.1 s with this flag. With a Browser Connection the Page does not need UIA, but the frame (tab strip, address bar, dialogs) still does, and the UIA fallback needs the whole tree.

- Merge with any `--disable-features` the launch already passes.
- Consider `--disable-backgrounding-occluded-windows` too, for `Page.captureScreenshot` on a covered Page (CDP docs). Measure first.
- The user's own browser cannot be changed; say nothing about it.

**Blocked by:** none

**Status:** done

- [x] Flag added in the browser launch arguments
- [x] Offline test of the argument list
- [x] Opt-in e2e: launch covered, read the Window State, the frame and the Page are present

## Comments

### 2026-11-02: landed

- `internal/browser/commands.go`: `launchArgs` now ends with `--disable-features=CalculateNativeWinOcclusion`, so `browser launch` and `browser clone` (`cloneArgs` builds on `launchArgs`) both get it. The comment gives the reason (the 14 s against 0.1 s measurement; the frame and the UIA fallback still need the tree). New pure `disableFeatures(args, features...)` adds features to an existing `--disable-features=` switch, skipping duplicates and empty entries, or appends one switch. It never adds a second switch, and it does not change its input. No launch passes `--disable-features` today, so the merge only matters for the next flag that does.
- Tests (`internal/browser/commands_test.go`): the launch test's exact argument list includes the flag. The clone test checks for it. `TestLaunchArgsTurnOffNativeOcclusion` checks that launch and clone (with session restore) carry exactly one `--disable-features` and that it is this one. `TestDisableFeaturesMergesIntoAnExistingSwitch` covers append, merge, a feature that is already listed, an empty switch, and an unchanged input.
- Opt-in e2e `test/e2e/browser_occlusion_test.go`, `TestLaunchedBrowserCoveredKeepsItsTree`. For Chrome, then Edge, it runs `winuse browser launch` and covers the first Window with the fixture as soon as it exists. Then it runs `winuse navigate` to `browser-pages.html` and polls `get-window-state` until both the address bar edit (the frame) and the Page's "e2e count" button show through the Browser Connection. It allows 5 s for that, and every read is checked to leave the cover in the foreground. It skips a browser whose launch profile is already running, because a warm browser is not a cold one. It is described in `test/e2e/README.md`. **It is written and passes `go vet -tags e2e ./test/e2e`, but it has not been run.** Nothing in this ticket was measured live.
- Docs: the CLI usage line for `browser launch` and SKILL.md's browser routes say a launched or cloned browser keeps rendering while covered. README's `browser launch` bullet says the same. CONTEXT.md and docs/codex-parity.md have no row or term this contradicts, so they are unchanged. Nothing is said about the user's own browser.
- Not done: `--disable-backgrounding-occluded-windows`. The ticket asks to measure first, and this session could not launch a browser, so it was not added. Before anyone adds it, someone needs to measure it live: on a fully covered launched browser, does `Page.captureScreenshot` (and the input route) behave differently with and without the flag? Note that `.scratch/browser-merge/probe-results.md` already found page reads and input Headless when covered or minimized without it. Screenshots were not part of that probe.

### 2026-11-03: choosing the launched Window

- A full e2e run once failed this test and `TestBrowserPages` with "no open Window with ID N" on a cold Chrome's first Window. Both picked it with `findWindowOf`, which checks only the process, class, visibility and title. Window IDs resolve against `win.KeepWindow`, which also drops cloaked, tool, no-activate and zero-area windows. `TestBrowserPages` retried `navigate` three times on the same ID, so a Window that went away or was never listed failed every try. The old logs (`e2e-pages9.log`, `e2e-pages10.log`, 2026-11-02) show this: the same ID failed three times, and the page ended up in another Window.
- Not reproduced: two diagnostic cold launches through `winuse browser launch` (kept profile), and two with plain Chrome on fresh temp profiles, each logged every window of the process for 10 to 12 s. Each showed one listed Window that never changed. No winuse-side cause was found, and `browser launch` reports no Window ID.
- Test fix: new `launchedBrowserWindow` (browser_pages_test.go) waits for the one Window of the process that winuse lists and returns it once it has been unchanged for 500 ms. Both tests use it, and they pick the Window again on each navigate retry. The cover now goes up once the Window is steady, about half a second after it appears, not at its first frame.
- Live, 2026-11-03: this test passed (frame and Page 1.91 s after the navigation), and `TestBrowserPages/chrome` passed 8 of 8.
