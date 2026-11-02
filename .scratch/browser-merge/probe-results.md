Status: done (Chrome measured; Edge is not installed on this PC)

# Browser merge probe: results

Ticket 03 (`issues/03-probe-unknowns.md`). The probe is `TestBrowserProbe` in `test/e2e/browser_probe_test.go`; how to run it is in `test/e2e/README.md` ("The browser merge probe").

Run on 2026-11-02 by the orchestrator, with the user's go-ahead ("can u do that for me ... automated everything"). The winuse extension was installed into the user's own Chrome (Developer mode, Load unpacked) through winuse itself. Display 1 (1920x1080) was switched to 150% and 175% through Settings for the later runs, then put back to 100%.

The decisions below are the orchestrator's proposals. The user can overrule any of them.

## Runs

| Browser | Version | Display scale | Extension connected | Report file |
|---|---|---|---|---|
| Chrome | 155.0.8059.85 | 100% | no | `probe-run-chrome-100pct-20261102-110259.md` |
| Chrome | 155.0.8059.85 | 100% | yes | `probe-run-chrome-100pct-20261102-110521.md` |
| Chrome | 155.0.8059.85 | 150% | yes | `probe-run-chrome-150pct-20261102-110705.md` |
| Chrome | 155.0.8059.85 | 175% | yes | `probe-run-chrome-175pct-20261102-110822.md` |
| Edge | not installed | | | skipped |

## Launch and connection (ticket 01 risks)

| Browser | Scale | Launch took the foreground | Port owner is the launched pid, identified right | /json/version or the dial raised a window | Browser alive after the CDP connection closed | Browser alive after the helper exited |
|---|---|---|---|---|---|---|
| Chrome | 100% | yes | yes, "chrome" | no | yes | yes |
| Chrome | 150% | yes | yes, "chrome" | no | yes | yes |
| Chrome | 175% | yes | yes, "chrome" | no | yes | yes |

Decision for tickets 04 to 07: keep launch as is. It takes the foreground once, which is expected for a new headed browser; the skill says so. The port-owner check and the persistent connection work as built.

## 1. Window to Tab mapping

Bounds (`Browser.getWindowForTarget`) plus the active tab's title against each HWND, with 3 windows open and one title repeated in a background tab.

| Browser | Scale | Windows | Match | Miss | Ambiguous | Bounds alone unique | Title alone unique | Which bounds compare matched |
|---|---|---|---|---|---|---|---|---|
| Chrome | 100% | 3 | 3 | 0 | 0 | 1 | 2 or 3 | DIP x scale vs GetWindowRect, off 0 px |
| Chrome | 150% | 3 | 3 | 0 | 0 | 1 | 2 | DIP x scale vs GetWindowRect, off 0 px |
| Chrome | 175% | 3 | 3 | 0 | 0 | 1 | 3 | DIP x scale vs GetWindowRect, off 0 px |

Windows opened with `newWindow:true` all get the same default bounds, so bounds alone often cannot tell them apart. The title breaks the tie, and the repeated title in a background tab is ruled out by bounds. Neither signal is enough alone; together they matched every Window.

Extension `chrome.windows` ids (the user's own browser):

| Browser | Scale | windowIds | HWNDs | HWNDs matched to exactly one windowId by title | windowIds stable |
|---|---|---|---|---|---|
| Chrome | 100 to 175% | 1 | 1 | 1 | yes |

Only one window of the user's Chrome was open, so this shows the ids are usable, not that they separate several windows. The extension reports no window bounds yet (it never calls `chrome.windows.get`).

Decision for ticket 04: match a Window to its Tab by `Browser.getWindowForTarget` bounds (DIP x the Window's DPI scale, against GetWindowRect, within a few px) intersected with the active tab's title against the Window title minus the " - <browser>" suffix. More than one candidate after both is an error naming them. On the extension, add `chrome.windows.get` bounds to the bridge so it can use the same rule, because `Browser.*` is refused there.

## 2. Foreground safety

With a fixture Window in the foreground. "Kept" means the foreground did not change and no browser Window rose above the fixture.

| Browser | Scale | `createTarget {background}` kept | `createTarget {newWindow, background}` kept | New window shown minimized or below | Extension tab create kept | Extension `activateTab` kept, tab became active |
|---|---|---|---|---|---|---|
| Chrome | 100% | yes | yes | below the cover, not minimized | yes | yes, yes |
| Chrome | 150% | yes | yes | below the cover, not minimized | yes | yes, yes |
| Chrome | 175% | yes | yes | below the cover, not minimized | yes | yes, yes |

Decision for tickets 05 and 07: `tab new` uses `Target.createTarget {background:true}` (or the extension's background tab) and is Headless. `tab switch` through the extension (`chrome.tabs.update active`) is Headless. `tab switch` on a launched or cloned browser was not measured (the probe never calls `Target.activateTarget`), so it stays Headed for now as the spec says.

## 3. Screen coordinates

Max error in physical px between the UIA document top-left plus CSS rect x `devicePixelRatio` and UIA's BoundingRectangle of the same Element. Target: 2 px or less. The column in brackets is the error with the monitor scale alone instead of `devicePixelRatio`.

| Browser | Display scale | Page zoom 100% | Page zoom 125% | Page zoom 150% | AutomationId exposed for every id |
|---|---|---|---|---|---|
| Chrome | 100% | 0.0 (0.0) | 0.5 (110) | 0.0 (220) | yes |
| Chrome | 150% | 0.0 (0.0) | 0.5 (165) | 0.5 (330) | yes |
| Chrome | 175% | 0.5 (0.5) | 0.5 (193) | 0.8 (385) | yes |

Decision for ticket 05 (the Agent Cursor transform): screen = UIA document top-left + CSS rect x `window.devicePixelRatio`, read fresh for each Action. Never use the monitor scale alone: it is wrong as soon as the page is zoomed.

## 4. Covered and minimized windows

Focus emulation on. Each cell: pass or fail, plus the AX node count. Same result at 100%, 150% and 175%.

| Browser | Scale | Case | getFullAXTree nodes | Input.insertText | dispatchMouseEvent click | Foreground unchanged |
|---|---|---|---|---|---|---|
| Chrome | all | uncovered (not foreground) | pass, 13 | pass | pass | yes |
| Chrome | all | fully covered | pass, 16 | pass | pass | yes |
| Chrome | all | minimized | pass, 16 | pass | pass | yes |

Decision for tickets 04 and 05: page reads and page input are Headless in every case, minimized included. No `--disable-backgrounding-occluded-windows` flag is needed. The uncovered read found 13 nodes and the later reads 16; the probe does not say why, so ticket 04 should not rely on a fixed count.

## 5. Posted keys in the address bar (information only)

| Browser | Scale | Focus class in the new window | Posted text reached the address bar | Posted text plus posted Return navigated | set-value plus posted Return navigated | Foreground unchanged |
|---|---|---|---|---|---|---|
| Chrome | all | none found (the background window has no focused child) | no | no | no | yes |

Decision for tickets 05 and 06: posted keys do nothing in Chrome's address bar, so `navigate <window> <url>` is the only Headless way to go to a URL. The skill should say so and never suggest typing into the omnibox when a Browser Connection exists.

## Not measured

- Edge (not installed here). The Edge rows can be filled by running the probe on a PC with Edge.
- The Chrome 144+ approval prompt (`browser connect` on the user's real profile), clone and `--restore-last-session`.
- The extension's child sessions (cross-origin iframes), the debugging bar's Cancel, self-update, and the developer-mode bubble.
- `Target.activateTarget` on a launched browser, and the extension with several user windows open.
