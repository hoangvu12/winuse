# 05: Headless tab switch on launched browsers

**What to build:** `tab switch` on a launched or cloned browser without the foreground. Today it activates and sends Ctrl+<n> or Ctrl+Tab (`internal/helper/tabs.go:509-560`); through the extension it is already Headless.

Candidates, probe first:

1. UIA SelectionItem.Select (or Invoke) on the tab-strip TabItem, through the existing Headless click route. Chromium maps Select to `ISelectionItemProviderSetSelected` (`ui/accessibility/platform/ax_platform_node_win.cc:3205-3209`). Unmeasured: whether the tab strip activates the window. Ticket 03's guard may be needed.
2. Load the winuse extension into profiles winuse launches, then use the measured `chrome.tabs.update` path. Unverified: branded Chrome 137+ may ignore `--load-extension`; CDP `Extensions.loadUnpacked` may need `--enable-unsafe-extension-debugging`.
3. `Target.activateTarget` stays forbidden (`tabs.go:17-25`; browser-merge spec "Tabs").

**Blocked by:** none (may use 03)

**Status:** ready-for-agent (route 1 measured; see Comments)

- [ ] Probe: route 1 on a covered launched Chrome, foreground before and after; route 2's flag behaviour on the installed Chrome
- [ ] Implement the measured route; keep the Headed route as `--headed`
- [ ] Offline tests of the routing
- [ ] Opt-in e2e: tab switch on a covered launched Chrome, foreground unchanged
- [ ] ADR 0012 amendment and SKILL.md tabs section

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
