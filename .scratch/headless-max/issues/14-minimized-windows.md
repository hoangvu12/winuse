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

**Status:** needs-triage (probe first; ADR 0010 minimized refusal; CONTEXT.md)

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
