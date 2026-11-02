# 05: Headless tab switch on launched browsers

**What to build:** `tab switch` on a launched or cloned browser without the foreground. Today it activates and sends Ctrl+<n> or Ctrl+Tab (`internal/helper/tabs.go:509-560`); through the extension it is already Headless.

Candidates, probe first:

1. UIA SelectionItem.Select (or Invoke) on the tab-strip TabItem, through the existing Headless click route. Chromium maps Select to `ISelectionItemProviderSetSelected` (`ui/accessibility/platform/ax_platform_node_win.cc:3205-3209`). Unmeasured: whether the tab strip activates the window. Ticket 03's guard may be needed.
2. Load the winuse extension into profiles winuse launches, then use the measured `chrome.tabs.update` path. Unverified: branded Chrome 137+ may ignore `--load-extension`; CDP `Extensions.loadUnpacked` may need `--enable-unsafe-extension-debugging`.
3. `Target.activateTarget` stays forbidden (`tabs.go:17-25`; browser-merge spec "Tabs").

**Blocked by:** none (may use 03)

**Status:** needs-triage (probe decides the route; changes ADR 0012 and the browser-merge spec "Tabs" section)

- [ ] Probe: route 1 on a covered launched Chrome, foreground before and after; route 2's flag behaviour on the installed Chrome
- [ ] Implement the measured route; keep the Headed route as `--headed`
- [ ] Offline tests of the routing
- [ ] Opt-in e2e: tab switch on a covered launched Chrome, foreground unchanged
- [ ] ADR 0012 amendment and SKILL.md tabs section
