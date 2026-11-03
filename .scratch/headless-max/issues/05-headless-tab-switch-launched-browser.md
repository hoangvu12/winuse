# 05: Headless tab switch on launched browsers

**What to build:** `tab switch` on a launched or cloned browser without the foreground. Today it activates and sends Ctrl+<n> or Ctrl+Tab (`internal/helper/tabs.go:509-560`); through the extension it is already Headless.

Candidates, probe first:

1. UIA SelectionItem.Select (or Invoke) on the tab-strip TabItem, through the existing Headless click route. Chromium maps Select to `ISelectionItemProviderSetSelected` (`ui/accessibility/platform/ax_platform_node_win.cc:3205-3209`). Unmeasured: whether the tab strip activates the window. Ticket 03's guard may be needed.
2. Load the winuse extension into profiles winuse launches, then use the measured `chrome.tabs.update` path. Unverified: branded Chrome 137+ may ignore `--load-extension`; CDP `Extensions.loadUnpacked` may need `--enable-unsafe-extension-debugging`.
3. `Target.activateTarget` stays forbidden (`tabs.go:17-25`; browser-merge spec "Tabs").

**Blocked by:** none (may use 03)

**Status:** done (the live e2e run is still to do: paused while the user games)

- [x] Probe: route 1 on a covered launched Chrome, foreground before and after; route 2's flag behaviour on the installed Chrome
- [x] Implement the measured route; keep the Headed route as `--headed`
- [x] Offline tests of the routing
- [x] Opt-in e2e: tab switch on a covered launched Chrome, foreground unchanged
- [x] ADR 0012 amendment and SKILL.md tabs section

## Comments

### 2026-11-03: triage decision (user)

Build it once the probe picks the route. The probe of route 1 (SelectionItem.Select or Invoke on the tab-strip TabItem of a covered launched Chrome, foreground before and after) and route 2's `--load-extension` behaviour: the laptop probe chat (358cd6b7) runs it; the user is never at the laptop during probes. The ADR 0012 and browser-merge spec changes are accepted.

### 2026-11-03: route 1 measured (`../headless-gaps/probes-round4.md` section 4)

- **Route 1 works:** UIA SelectionItem.Select on the tab strip's TabItem switched the tab of a covered launched Chrome 156. The title and IsSelected followed, and the foreground and cursor were unchanged.
- TabItems have **no Invoke pattern**, so use Select only.
- **Route 2 is dead:** branded Chrome 156 ignores `--load-extension`, even with `DisableLoadExtensionCommandLineSwitch` disabled.

Build route 1:

- Find the Tab's TabItem in the frame's UIA tab strip by matching its title. Chrome appends notes such as " - Memory usage - 17.3 MB", so match on the prefix and refuse when several match.
- Select it, then read back through CDP (the Page target's active state) or IsSelected.
- The Headed Ctrl+Tab route stays as `--headed`.
- Update ADR 0012 and the browser-merge spec "Tabs" section: a tab switch on a launched browser is Headless.
- The laptop probe for this ticket is done.

### 2026-11-03: done (worker, branch hm05-tab-switch)

- `internal/uia/tabstrip.go`: `Client.SelectTab(ctx, hwnd, pick)` walks the Window's control view breadth-first (25 levels, 3000 nodes) for TabItems, never entering a Document (the web content's ARIA tabs) or a TabItem, lets the caller pick one by name, calls SelectionItem.Select, and polls IsSelected for up to 1 s. No strip is `uia.ErrNoTabStrip`; a failed Select a `WriteError`; no read-back a `VerifyError`. The driver (`selectTab`) is tested offline over a fake strip.
- `internal/helper/tabs.go`: `tab switch` on a launched or cloned browser goes through `tabSwitchStrip` (new `TabSelector` seam on `Services.Actions`, `*uia.Client` in the helper). It makes the Headless refusals first (locked desktop, admin, modal dialog, minimized), then runs in a passive Esc scope with the Agent Cursor on the strip item. The item is matched by the Tab's title with `stripMatches` (the title, or the title plus Chrome's " - ..." / ", ..." state; `stripPosition` now uses it). No match, two matches or no strip refuse with "nothing was sent" and the exact `winuse --headed tab switch <window> <tab>` retry: never an automatic Headed fallback (ADR 0013 rule 3). `--headed` keeps the Ctrl+<n>/Ctrl+Tab route. The extension route is unchanged and ignores `--headed`.
- `protocol.TabArgs.Headed`, CLI `tab switch` takes the global `--headed` (`tabRequest`), `takesHeaded("tab-switch")` so a `--headed` Batch injects it, and the Batch's scope question (`tabStepHeadless` gets the step's args): an unheaded tab switch is Headless.
- Docs: ADR 0012 amendment, ADR 0013 route table (05 done), browser-merge spec "Tabs", SKILL.md tabs section, codex-parity extensions row, CLI usage, e2e README.
- Tests: `internal/uia/tabstrip_test.go`, and in `internal/helper/tabs_test.go` the Headless switch (picked item, cursor to the strip item, passive scope, no activation or keys), the refusals (no item, two items, no strip, minimized, no TabSelector), failures after the Select (not read back, failed Select), `stripMatches`, the Batch scope both ways, and the existing Headed tests now pass `Headed: true`. CLI: `TestTabSwitchTakesHeaded`.
- Opt-in e2e (written, compiled with `go vet -tags e2e`, NOT run: live runs are paused while the user games): `TestBrowserPages`'s `tabs` subtest now switches to the new background Tab and back on the covered launched browser. `p.act`'s guard checks the foreground and z-order; the test also checks `tab list`'s active Tab and the Window's title. Run: `go test -tags e2e -v -run TestBrowserPages ./test/e2e`.
- Not measured: Edge (only Chrome 156 was probed), a Tab in a collapsed tab group, and pinned Tabs whose strip name may be shorter than the title (they would be refused with the `--headed` retry, not mis-switched).

### 2026-11-03: the live run (worker, branch chrome-tab-strip)

- `TestBrowserPages/chrome/tabs` failed in the full live run with "the Window shows no tab strip through UI Automation". Bisected live: alone, or after page-actions, drag and dialogs, it passes; after the download subtest, and after cross-origin-iframe plus upload, it fails.
- Measured four times after a download: the first switch through the tab strip worked (80-400 ms), and from then on every UI Automation call into the Chrome Window failed: `get-window-state` failed at once with E_FAIL (0x80004005) on its root read, and the strip walk's child lookups failed, for as long as the test waited (30 s). It is Chrome's UI Automation, not the walk: the strip sits about 8 levels down and is found whenever Chrome answers.
- Product: the walk no longer reports failed lookups as "no tab strip". With no item found and a lookup failed, it returns `uia.ErrTabStripUnanswered` with the HRESULT, and the helper refuses with "reading the tab strip of window N failed: the browser's UI Automation did not answer ... ; nothing was sent" and the `--headed` retry. The settle (up to 3 s) retries that and a failed root read too (an earlier commit added the settle for a lazy strip).
- e2e: the `tabs` subtest runs right after `dialogs`, before the subtests that leave Chrome's UI Automation broken, with the measurement in a comment. The full `TestBrowserPages` passed live in that order.
- Not explained: why Chrome's UI Automation stops answering after a download (the download bubble is the likely suspect). An agent meets it as the refusal above; `--headed` still switches.

