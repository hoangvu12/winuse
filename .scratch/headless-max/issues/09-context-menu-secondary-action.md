# 09: ContextMenu Secondary Action

**What to build:** `perform-secondary-action <window> <element> ContextMenu` opens the Element's context menu without the foreground, verifies that a menu opened, and lets the agent act on it.

Routes (`../headless-gaps/headless-100.md` G6):

1. UIA `IUIAutomationElement3::ShowContextMenu`. Chromium implements it (`ax_platform_node_win.cc:5849-5858`). Measured: on a classic Edit and on RichEdit it returned S_OK and showed no menu, so it needs a readback.
2. Classic HWND: post WM_CONTEXTMENU (wParam = the control, lParam = -1). Measured: the Edit's context menu appeared, foreground unchanged.

Readback: a new `#32768` window, a UIA MenuOpened event, or a new popup Window of the App within the verify wait. None: VerifyError "delivered; no menu opened".

Then:

- Print the menu's Window ID so the agent reads it with `get-window-state` and clicks an item (the Headless click route). Unmeasured: whether UIA Invoke on a classic `#32768` item works headless. Fallback: MN_GETHMENU on the menu window, then post WM_COMMAND to the owner.
- A menu left open is closed with WM_CANCELMODE (measured) when the agent's next Action targets another Window, or by `press-key <menu> Escape`.
- The menu is topmost and appears over the user's work. It takes nothing, but it is visible. The Agent Cursor shows it.

**Blocked by:** none

**Status:** ready-for-agent (routes measured; see Comments)

- [ ] Probe: cross-process WM_CONTEXTMENU, item Invoke on `#32768`, ShowContextMenu on Chrome and a WinUI app
- [ ] Route selection and readback
- [ ] Menu Window reported in the output; cancel path
- [ ] Offline tests: route per Element kind, readback timeout
- [ ] Opt-in e2e: fixture edit box and Chrome frame, covered
- [ ] CONTEXT.md Secondary Action list; SKILL.md

## Comments

### 2026-11-02: safe live batch (aborted)

- Fixture's drawn text field, cross-process [M]: UIA ShowContextMenu returned S_OK and opened no menu, and a posted WM_CONTEXTMENU(-1) opened no menu either. This confirms the readback requirement.
- Not measured: the real Edit routes, item choice (UIA Invoke on `#32768`, MN_GETHMENU, posted mnemonic), and the WPF TextBox. The guard fired while the fixture's Classic Dialog opened: its own `ShowWindow(SW_SHOW)` took the foreground from the League client.
- Recommendation: keep `needs-triage`. Rerun with the user away. Details: `../../headless-gaps/probes-safe-batch.md`.

### 2026-11-03: triage decision (user)

Build it once the probe has measured the routes. It is also the preferred route for right-clicks on an Element (ticket 12's posted right-click is the fallback). Probe: cross-process WM_CONTEXTMENU on a classic Edit, ShowContextMenu on Chrome and on a WPF or WinUI window the probe builds, and item choice (UIA Invoke on `#32768`, MN_GETHMENU). the laptop probe chat (358cd6b7) runs it; the user is never at the laptop during probes.

### 2026-11-03: routes measured (`../headless-gaps/probes-round4.md` section 2)

Routes per framework:

| Framework | Open the menu | Pick an item | Close |
|---|---|---|---|
| Classic Win32 | posted WM_CONTEXTMENU(control, -1); readback: a new `#32768` of the target pid | read the items through MN_GETHMENU (names and ids), then **post the item's mnemonic key to the `#32768`**; WM_COMMAND to the owner only when the menu is not TPM_RETURNCMD (verify by the menu closing and the effect) | WM_CANCELMODE to the owner |
| Chromium (frame and page Elements) | UIA ShowContextMenu on the Element (not the top-level or RWHV); readback: a new Chrome_WidgetWin_1 popup of the same pid | UIA Invoke on the popup's MenuItem (foreground unchanged) | WM_CANCELMODE to the top-level |
| WinUI 3 | UIA ShowContextMenu; readback: new `PopupHost` windows | not yet measured | WM_CANCELMODE does not close it; use Escape through Posted Keys or UIA, not yet measured |
| WPF | no Headless route (ShowContextMenu and WM_CONTEXTMENU both open nothing in an inactive window) | | |

Rules:

- **Never UIA Invoke an item of a classic `#32768` menu**: it took the foreground (measured).
- **Never SendMessage WM_CONTEXTMENU**: TrackPopupMenu is modal and would block the helper.
- WPF and anything with no readback refuse with "nothing was sent" and the `--headed` hint.
- Choosing an item is part of this ticket: `perform-secondary-action <window> <element> ContextMenu` reports the menu's Window ID, and the item is clicked by its index in the menu's Window State. The helper routes that click by the menu's class (mnemonic for `#32768`, Invoke for Chromium).
- The laptop probe for this ticket is done.

### 2026-11-03: WinUI and Chrome items measured (`../headless-gaps/probes-round5.md` section 2)

This updates the route table above.

- **WinUI 3: pick and close are measured.**
  - The menu is a `PopupHost` (PopupWindowSiteBridge) top-level. Dedupe its items against the main tree.
  - **UIA Invoke on a MenuFlyoutItem or AppBarButton works and keeps the foreground** (unlike classic `#32768`).
  - Close it with Window.Close on the Popup element, or Escape posted to the popup's InputSiteWindowClass. Not WM_CANCELMODE, not Escape to the PopupHost or the top-level.
  - The App sees a Window.Activated event, but the system foreground does not change.
- **Chrome:**
  - Items are Invoke-able and named with their shortcut. Close with Escape posted to the menu window (Collapse fails).
  - **Edit commands in the menu act on Chrome's current focus, not on the right-clicked Element** (Select all selected the document). Only report and allow commands that do not depend on focus, or tell the agent that edit items may act elsewhere. Page Elements should keep using CDP.
  - A fully covered Chrome without the occlusion flag has no page tree at all. The UIA route applies only to browsers winuse launched (ticket 04's flag) or to visible ones.

### 2026-11-03: never Collapse a Chromium menu

On a second open Chrome menu, UIA Collapse on the SubmenuView hung the UIA client indefinitely in 1 of 3 runs (`../headless-gaps/probes-round5.md` section 4). Close Chromium menus only with Escape posted to the menu window.
