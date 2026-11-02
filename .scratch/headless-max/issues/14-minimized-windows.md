# 14: Headless Actions on minimized Windows

**What to build:** allow Headless pattern calls on a minimized Window where the probe shows they work, instead of refusing every minimized Window (`internal/win/activate.go:85-87`, mapped at `internal/helper/headless.go:480-487`).

Evidence (`../headless-gaps/headless-100.md` G14):

- charmap's UIA tree had 14 Elements minimized and restored; root bounds Empty when minimized.
- Pattern calls on a minimized Window were not measured.
- cua claims UIA Invoke works on minimized Windows (`tools/launch_app.rs:177`).
- Electron's tree drops from 30 to 9 nodes when minimized (agent-desktop A24-5). The Laptop probe saw Calculator at 0 and Explorer at 7.

Plan:

1. Probe: Invoke, Toggle, SetValue, SelectionItem on minimized fixture (W32), a WPF app and Calculator (XAML). Record tree size and effect.
2. Where it works (expected: HWND-backed W32 and WPF trees), drop the minimized refusal for Headless Element Actions. Keep it for XAML and Chromium-via-UIA, and for screenshots (WGC fails on minimized Windows).
3. Agent Cursor: no rectangle on screen. Skip the cursor, or show it at the taskbar button.
4. The refusal hint keeps naming `restore-window`.

**Blocked by:** none

**Status:** ready-for-agent (classic Win32, WPF and WinUI 3 measured; see Comments)

- [ ] Probe per framework, recorded in this ticket
- [ ] Readiness check by framework class
- [ ] Offline tests of the readiness decision
- [ ] Opt-in e2e: minimized fixture click and set-value

## Comments

### 2026-11-02: safe live batch (aborted)

- Only the baseline was measured: the fixture's normal tree had 34 Elements (FindAll subtree) [M].
- Minimized tree counts, minimized Invoke/Toggle/SetValue/Select, and the SW_SHOWNOACTIVATE restore were not reached. The batch aborted earlier, when a fixture dialog took the foreground.
- Lesson for the rerun: on this PC the foreground lock does not protect once the last-input process has exited. Restores and App-opened dialogs can take the foreground.
- Recommendation: keep `needs-triage`. Probe still pending; run it with the user away. Details: `../../headless-gaps/probes-safe-batch.md`.

### 2026-11-03: round 3 probes (`../headless-gaps/probes-round3.md`)

- **Classic Win32: measured working** on the PC. A minimized window stayed iconic with the foreground unchanged. UIA read all 120 Elements, and Invoke (WM_COMMAND), Value.SetValue (read back), EM_REPLACESEL and SelectionItem.Select (LB_GETCURSEL=7) all worked.
- **Chromium: no.** The activation messages restore it and take the foreground (see ticket 20).
- WPF and XAML are still to test.

### 2026-11-03: triage decision (user)

Drop the minimized refusal for Headless Element Actions on classic Win32 now (measured in round 3). Keep it for Chromium: its activation messages restore it and take the foreground. WPF and XAML follow the probe, the laptop probe chat (358cd6b7) runs it; the user is never at the laptop during probes. Screenshots stay refused.

### 2026-11-03: WPF and WinUI 3 measured (`../headless-gaps/probes-round4.md` section 1)

On the laptop, the probe's own WPF and WinUI 3 windows were minimized with SW_SHOWMINNOACTIVE. Invoke, Toggle, SetValue (read back) and SelectionItem.Select all worked; the windows stayed iconic; foreground and cursor were unchanged; the tree size was the same as when restored. Scope now: lift the minimized refusal for UIA-pattern Actions on classic Win32, WPF and WinUI 3. Keep it for Chromium (message routes and the Chromium frame), for packaged UWP (`ApplicationFrameWindow`, not measured) and for screenshots. The laptop probe for this ticket is done.

### 2026-11-03: Store apps measured (`../headless-gaps/probes-round5.md` section 5)

Calculator, a packaged UWP app, measured on the laptop:

- **Minimized, its UIA tree is empty** (0 Elements in both the frame and the detached CoreWindow, over 20 s). The app is most likely suspended. There is nothing to act on.
- **Restoring it with SW_SHOWNOACTIVATE took the foreground.**
- Its launch through IApplicationActivationManager took the foreground too. That is relevant to ticket 01: packaged-app launches cannot be kept in the background, so report it.

Rule:

- Keep the minimized refusal for `ApplicationFrameWindow` (UWP). Name the reason in the refusal: "a minimized Store app has no UI to act on".
- The `restore-window` hint stays, adding that restoring a Store app brings it to the front.

Final scope of this ticket: lift the refusal for classic Win32, WPF and WinUI 3 Desktop; keep it for UWP and Chromium.
