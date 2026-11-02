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

**Status:** ready-for-agent (laptop probe first)

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
