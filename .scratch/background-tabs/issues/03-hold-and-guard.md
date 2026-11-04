# 03: Keep a held Tab alive; guard the user's Tab

Status: done

Spec decisions 4 and 5 (ADR 0017 Decisions 3 and 4).

**Done.**

- `page.HoldTab` / `page.ReleaseTab` (`internal/page/background.go`):
  `Emulation.setFocusEmulationEnabled` on/off, `Page.setWebLifecycleState
  active`, and through the extension `Winuse.holdTab` / `Winuse.releaseTab`
  (`bridge.MethodHoldTab`, `MethodReleaseTab`).
- `tabHolds` (`tabtarget.go`): every command on a Tab target renews the
  hold; it is released `tabHoldIdle` (5 min) after the last one, with a
  generation check so a late timer never ends a renewed hold.
- `background.js`: `holdTab` remembers the tab's own `autoDiscardable` and
  the window's selected tab; `releaseTab`, a detach and `onDetach` restore
  it (`unholdTarget`). `tabs.onCreated` with a held opener marks the new
  tab for `SPAWN_GUARD_MS` (2 s): if it becomes selected, the user's tab is
  selected again (`keepUsersTab`, `chrome.tabs.update active` inside the
  window); it joins the opener's group; a popup window it opened in is
  minimized (the one `chrome.windows.update` the extension has, pinned by
  `TestExtensionIdentity`).
- `bridgetest`: `HoldTab`/`ReleaseTab` handling, `Held`, `Holds`,
  `ActiveTab`, `Minimized`, `OpenFromTab` (a link or `window.open` in a tab,
  with the guard mirrored), and a detach releases.
- `AttachPage` is unchanged: winuse attaches only to Tabs a command
  targets, so focus emulation never lands on an untouched Tab.
- Tests: `TestTabTargetReadsABackgroundTab` (only the targeted Tab is
  held and emulated), `TestTabTargetLinkOpensANewTabBesideTheUsersTab`,
  `TestTabHoldEndsWhenIdle`, `TestExtensionIdentity`.

**Not verified live:** Chrome's actual event order for a `target=_blank`
link from a background tab (onCreated vs onActivated), whether the
browser raises its window for such a tab, and Brave's Energy Saver freeze
followed by `setWebLifecycleState`.

## Comments
