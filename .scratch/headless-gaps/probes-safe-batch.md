# Safe live probe batch (P1-P5): ABORTED by the foreground guard

- **Date:** 2026-11-02, about 22:05-22:30 local.
- **Machine:** DESKTOP-19EUMEB, Windows 11 Pro 10.0.26200. DISPLAY1 is primary at (0,0)-(1920,1080), DISPLAY2 at (1920,0)-(3840,1080).
- **Constraint:** the user was playing League of Legends on DISPLAY1. Not disturbing the game ranked above getting results.
- **Outcome:** the guard **fired** during the first probe run. The batch was aborted as designed. Only part of P1 and P2 and the P3 baseline were measured. P4 and P5 never ran.

Evidence tags: **[M]** measured in this batch, **[I]** inferred.

## 1. Foreground guard

- **Baseline, at the start:** HWND `0x11902d0`, process `League of Legends` (pid 24148, tid 16524). The window was fullscreen at (0,0)-(1920,1080).
- **`SPI_GETFOREGROUNDLOCKTIMEOUT`** was 2147483647 ms, so the lock never expires on time alone [M].
- **Design** (`probe/common.go`):
  - At start, `GetForegroundWindow` must equal the recorded HWND. Otherwise the probe refuses to start anything (exit 4).
  - A watchdog goroutine compares `GetForegroundWindow` every 15 ms.
  - `chk(step)` runs after every launch, move, message, pattern call and wait.
  - On a change, it logs the step and the new foreground's HWND, pid, tid and class. Then it closes every process the probe started (WM_CLOSE, then TerminateProcess after 3 s) and exits with code 3. It never tries to give the foreground back.
  - The game's HWND is never passed to any API except that comparison. Window enumerations skip it explicitly.
  - Attach refuses when the target's thread or process is the foreground's, or the original game's.
- **First run: refused, nothing started [M].** Between setup and the first run, the match ended. The foreground became the League client: `LeagueClientUx`, HWND `0x4f0616`, class `RCLIENT`, pid 9752, tid 2716. Process 24148 had exited. The user caused this change; the probe had started nothing.
  - The guard was re-armed on `0x4f0616`, and every League process was treated as off-limits.
- **Second run: GUARD FIRED [M].**
  - **Step:** `P2 invoke Classic Dialog button`, a UIA Invoke (S_OK) on the fixture's "Classic Dialog..." button. The fixture handles it with `CreateDialogIndirectParamW`, then `ShowWindow(SW_SHOW)`, then `SetFocus(edit)` (`internal/fixture/classic.go:155-171`).
  - **Timing:** the watchdog saw the change 19 ms after the Invoke returned (22:21:33.525, then .544).
  - **New foreground:** `0x8f02b0`, class `#32770`, pid 30360 (the probe's own fixture), tid 16088.
  - **Cleanup:** the fixture exited on WM_CLOSE. No TerminateProcess was needed.
  - Afterwards, the foreground was back on `LeagueClientUx` `0x4f0616` without any action by the probe [M].
- **Why the lock did not hold [I]:** the user's last input most likely went to the game process, which had exited. The client then took the foreground at the end of the match without new input. With no live process holding "last input", Windows let a background process's `ShowWindow(SW_SHOW)` take the foreground.
  - The earlier record (headless-100 section 5: charmap restore with `targetBecameFg=true`) fits the same pattern. The foreground lock is not a safety net on this PC.

## 2. Results per probe

### P2 (partial): context menu, cross-process, on the fixture

- **Setup:** `winusefixture.exe -state <out>\state.json -origin 2000,100`, launched with CreateProcess (`STARTF_USESHOWWINDOW|STARTF_USEPOSITION`, `SW_SHOWNOACTIVATE`, dwX/dwY = 2000,100). The window appeared at (2000,100)-(2816,609) on DISPLAY2 [M]. The UIA client ran in a separate process with `AutoSetFocus=FALSE`.
- **(a) UIA `IUIAutomationElement3::ShowContextMenu`** on the drawn "Text Field" Element (the fixture's own provider): `hr=0x0`, 0 visible `#32768` windows, foreground unchanged [M].
- **(b) Posted `WM_CONTEXTMENU(main, wParam=main, lParam=-1)`:** 0 menus, foreground unchanged [M].
- **Verdict:** the fixture's drawn field has no context menu, so both routes do nothing, and ShowContextMenu still returns S_OK. This confirms again that the route needs a readback [M]. It says nothing new about real context menus.
- **Not run:** the Classic Dialog Edit routes (the guard fired while that dialog opened), the msinfo32 Edit, and the WPF TextBox. The cross-process item choice (UIA Invoke on `#32768`, MN_GETHMENU, a posted mnemonic) also remains unmeasured.

### P1 (partial): chords through attached key state

The fixture has no accelerator table. Its Ctrl+A is tracked from WM_KEYDOWN(VK_CONTROL) messages, not from GetKeyState (`internal/fixture/app.go:684-714`). So it was used as a control only. The text field was first set to `abc` by UIA SetValue (`hr=0x0`).

| Step | Readback (state file) |
|---|---|
| Posted WM_KEYDOWN/UP 'A' to the main window, no attach | `text=abca`. TranslateMessage made WM_CHAR 'a' [M] |
| Attach + SetKeyboardState(VK_CONTROL, VK_LCONTROL down) + the same post, held 400 ms, then restore and detach | `attach=1 detach=1`. GetKeyState(CTRL) on the attached thread = `0xff81` (down). `text=abc` (unchanged). `textSelected=false`. User's GetAsyncKeyState(CTRL) `0x0` before and after. Foreground unchanged [M] |
| Message-only chord: posted VK_CONTROL down, 'A' down/up, VK_CONTROL up | `text=a` [M]. The fixture's Ctrl+A selected all, then the WM_CHAR 'a' that TranslateMessage made replaced the selection [I] |

- **Verdict:** the forged Ctrl state reached the target process's TranslateMessage. 'A' became a control character (0x01) instead of 'a', so the text did not change [I, from the unchanged text].
  - An app that tracks Ctrl from messages does not see the forged state. That calls for posting VK_CONTROL keydown/keyup around the key as well as setting the state.
  - The user's async Ctrl state was untouched [M].
- **Not run:** msinfo32 Ctrl+F and Ctrl+O, real accelerators through TranslateAccelerator in a real App.

### P3 (baseline only): minimized windows

- **Fixture, normal:** `FindAll(TreeScope_Subtree, TrueCondition)` returned 34 Elements, `hr=0x0` [M].
- **Not run:** minimizing, the minimized tree count, pattern calls while minimized, and restore. Nothing was run on WPF or Calculator.

### P4: Save As without the foreground. NOT RUN

### P5: modifiers seen by WPF. NOT RUN

## 3. Skipped, and why

- **Everything after the guard fired:** the rest of P2 (Classic Dialog Edit, item choice), the P1 msinfo32 accelerators, P3 minimized (fixture, WPF), P4 and P5. The batch rule is to abort the whole batch on the first fire.
- **Calculator:** not attempted, even before the abort. Activation by AUMID cannot pass a window position, and Calculator restores its last position, which may be on DISPLAY1 over the game. The activation path's foreground behaviour is also unknown.
- **powershell_ise.exe:** present at `C:\Windows\System32\WindowsPowerShell\v1.0\powershell_ise.exe` and not running. It was not reached.
- **No App was already running:** none of msinfo32, charmap, Calculator, the fixture or powershell_ise was running at the start, so no rule-6 skip was needed.

## 4. Lessons for the rerun

1. **On this PC the foreground lock does not protect a game** once the process that got the last input has gone, for example at the end of a match. Any target App that calls `ShowWindow(SW_SHOW)`, `SetForegroundWindow`, or a modal dialog on its own can take the foreground.
2. **High-risk steps in this batch, each with an App-side activation:**
   - the fixture's Classic Dialog (fired);
   - msinfo32 File > Save and Ctrl+O (the common file dialog shows itself activated);
   - WPF button and mouse handling (it may call `Focus()`);
   - restoring a minimized window (charmap did this before).
3. **Run the rest when the user is away from the PC, or in a VM or a second session.** Do not run it while a game is up: a steal minimizes a fullscreen game.
4. **If the rerun must happen with the user present,** drop every step that opens a dialog or restores a window. Keep only pattern calls on already-open, already-minimized windows. Even then a steal is possible [I].

## 5. Scripts and raw output

- **Probe source:**
  - `C:\Users\ADMIN\AppData\Local\Temp\claude\C--Users-ADMIN-Desktop-nguyenvu-winuse\7f81330a-865c-4df8-b176-5cc7ba425b1b\scratchpad\b2\probe\`
  - `common.go`: guard, launch, cleanup, raw-vtable UIA, attached key state.
  - `fixture.go`: P1, P2, P3 on the fixture.
  - `msinfo.go`: P1, P2, P4 on msinfo32 (not run).
  - `wpf.go` and `wpf.ps1`: P3 and P5 on a WPF window (not run).
  - `main.go`: usage `probe2.exe <expectedForegroundHex> fixture|msinfo|wpf`.
- **Fixture build:** `...\scratchpad\b2\fx\winusefixture.exe`, built from `./cmd/winusefixture`. The repo was not changed.
- **Run log:** `...\scratchpad\b2\out\run.log`.
- **Fixture state file:** `...\scratchpad\b2\out\fixture-222127\state.json`.
- **Cleanup check after the abort [M]:** no `winusefixture`, `msinfo32` or `probe2` process was left. pid 30360 had exited. No window opened by the probe remained.
