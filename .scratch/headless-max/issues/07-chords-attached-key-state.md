# 07: Chords through attached key state (opt-in)

**What to build:** `press-key <window> <chord> --post` accepts modifiers (Ctrl, Shift, Alt) on classic Win32 targets. Today `--post` refuses any chord (`internal/helper/keyboard.go:121`; ADR 0011 Decision 5).

Route (`../headless-gaps/headless-100.md` G4):

1. AttachThreadInput(a helper thread, the target Window's thread).
2. SetKeyboardState with the modifier keys (VK_CONTROL and VK_LCONTROL, and so on) down.
3. Post WM_KEYDOWN/WM_KEYUP for the key to the focused control (as `--post` does today).
4. Restore the previous key state and detach.

Measured: a separate background process with a Ctrl+S accelerator (TranslateAccelerator loop) received `WM_COMMAND id=201`. The foreground was unchanged, and the user's async Ctrl state was 0 before and after. Without step 2 the app saw GetKeyState(CTRL)=0 and no accelerator fired.

**This contradicts agent-desktop (A4-5), cua (`hotkey.rs:303-330`) and the Laptop probe, which listed "AttachThreadInput key-state forging" as a dead end.** The measurement used a test window the probe wrote, not a real App. Step 0 of this ticket is a cross-process probe on real Apps before anything is built.

Rules:

- Refuse when the target thread is the foreground thread: attaching would share the user's keyboard state. "nothing was sent".
- Run the attach on a disposable thread with a timeout, so a hung target cannot stall the helper.
- Alt chords that open menus need WM_SYSKEYDOWN and the menu loop: measure, else refuse Alt.
- Win-key chords stay refused.
- Output as for Posted Keys: `ok (posted; the app may ignore posted keys, read the Window State to check)`.

**Blocked by:** none (06 is the preferred route where an Element carries the shortcut)

**Status:** done (the live e2e run is still to do)

- [x] Probe on Notepad (Win11), WordPad, Paint, an MFC app, a WinForms app and a WPF app: Ctrl+S, Ctrl+A, Ctrl+Z, Shift+Tab, Alt+F; foreground and user key state before and after (done in rounds 3 and 4 on the probe's own Win32, WinForms, WPF, Chrome, Edge and Electron windows; MFC unmeasured)
- [x] If adopted: attach, state, post, restore on a disposable thread; restore on every exit path
- [x] Offline tests: foreground-thread refusal, Win refusal, Alt policy
- [ ] Opt-in e2e on the Apps the probe proved: written and compiled (the fixture's Win32 accelerator, a WPF KeyBinding); not run live yet
- [x] ADR 0011 amendment, CONTEXT.md "Posted Keys", SKILL.md

## Comments

### 2026-11-02: safe live batch (aborted)

- Fixture control only [M]: attach + SetKeyboardState(Ctrl) gave GetKeyState(CTRL)=0xff81 on the attached thread. A posted 'A' then left the text unchanged, where the plain post typed 'a': TranslateMessage in the target process saw Ctrl down [I]. User's async Ctrl was 0x0 before and after. Foreground unchanged.
- The fixture tracks Ctrl from WM_KEYDOWN messages, so the forged state alone did not trigger its Ctrl+A. Apps like this also need posted VK_CONTROL down/up around the key.
- Not measured: real accelerators (msinfo32 Ctrl+F/Ctrl+O, WPF). The batch aborted when another step took the foreground.
- Recommendation: keep `needs-triage`. Rerun the msinfo32 and WPF legs with the user away. Details: `../../headless-gaps/probes-safe-batch.md`.

### 2026-11-03: round 3 probes (`../headless-gaps/probes-round3.md`)

- **Chromium and Electron: measured working.** Ctrl+A, Shift+Tab and Ctrl+V through AttachThreadInput + SetKeyboardState on Chrome's UI thread, with the PR #400 activation (Chrome, Edge, Electron). The foreground was unchanged, and the step was refused if that thread owned the foreground. That route is ticket 20.
- Classic Win32, WPF and MFC Apps are still to test.

### 2026-11-03: triage decision (user)

Kept as an opt-in `--post` route for classic Win32 Apps. The Chromium and Electron chords are measured and built in ticket 20. The probe on real classic Apps (a Win32 accelerator loop, WinForms, WPF and MFC test windows the probe builds itself): the laptop probe chat (358cd6b7) runs it; the user is never at the laptop during probes. Adopt per App type only where it is measured.

### 2026-11-03: classic Apps measured (`../headless-gaps/probes-round4.md` section 5)

| Target | Ctrl or Ctrl+Shift chord | Needs the activation pair |
|---|---|---|
| Win32 TranslateAccelerator | fired (WM_COMMAND) | no |
| WinForms ShortcutKeys | fired | no |
| WPF KeyBinding | fired only with the pair | **yes** |
| Win32 Alt+F (menu) | the menu opened only with the pair; the mnemonic then chose the item | **yes** |

- The user's async key state stayed 0, and the foreground and cursor were unchanged throughout.
- The activation pair is the same as ticket 20's (WM_NCACTIVATE, WM_ACTIVATE, WM_SETFOCUS, then the reverse).

Build:

- Attach plus SetKeyboardState, then post the keys to GetGUIThreadInfo's focus (or the top-level), then restore and detach. Run this on a disposable thread with a timeout.
- Send the activation pair around the chord for WPF (`HwndWrapper[` classes) and for Alt chords. Skip it for plain Win32 and WinForms, where it is not needed.
- Never on a minimized Window when the pair is sent.
- Alt chords are adopted: menus open, and ticket 09's mnemonic choice picks items.
- Stays opt-in `--post` (no read-back; ADR 0013 rule 2).
- MFC is unmeasured. Treat it as Win32 (TranslateAccelerator) until shown otherwise.
- The laptop probe for this ticket is done.

### 2026-11-03: built (branch hm07-chords)

- `internal/win/postchord.go`:
  - `PlanPostedChord(chord, topClass)` (pure) gives the modifiers to hold (generic and left VK), the one key, whether it is an Alt (system) chord, and whether the activation pair is needed.
  - The pair is used for Alt chords, WPF (`HwndWrapper[`) and Chromium (`Chrome_WidgetWin_*`, measured in round 3), and is skipped for Win32, WinForms and MFC.
  - Refused: a chord of modifiers alone, more than one key, and Control and Alt together (unmeasured).
  - `PostChord(top, target, plan)` runs on a disposable, OS-locked thread bounded at 3 s. It refuses the thread that owns the foreground (`ErrChordForeground`, nothing sent), then: AttachThreadInput, save the key state, SetKeyboardState with the modifiers down, the activation pair (sent with a 500 ms bound each) when planned, the key down posted (WM_SYSKEYDOWN with the Alt context bit for Alt), a 150 ms hold, the key up, and a short wait. On every exit path it undoes the pair, restores the saved state and detaches.
- Helper:
  - `press-key --post` with a modifier now goes to `postChord` instead of the old refusal: planned for the Window's class, posted to the focused control (or the Window), with the Posted Keys output. A chord the plan refuses keeps a refusal that says nothing was sent. The foreground thread gets the `--headed press-key` hint.
  - **A Blocked Combo is never posted, even with `--force`** (a posted Alt+F4 would close the Window without the user seeing it); `--headed --force` stays its only way.
  - hm-21's refusal hint (`pressKeyAlt`) now offers `--post` for chords the route can post.
- Fixture: the main window gains one accelerator, `Control_L+Shift_L+k` (TranslateAccelerator to WM_COMMAND, counted in `accelerators`).
- Tests:
  - `internal/win/postchord_test.go`: the plan per class and chord, the messages (SYSKEY and the Alt bit), and the held state touching only the modifiers. PostChord is in the live guard.
  - `internal/helper/keyroute_test.go`: chords posted with their plans, the refusals, and the foreground thread. The old "chords are refused" tests are replaced, and the `--post --force` Blocked Combo test now wants the new refusal.
- E2e, written and compiled, not run:
  - New: `TestPostedChordFiresAClassicAccelerator` (fixture) and `TestPostedChordFiresAWPFKeyBinding` (a WPF window started through Windows PowerShell). Both are covered, and both check that the foreground is unchanged and that the user's Control key is not down afterwards.
  - Changed: `TestPostedKeysIntoClassicEdit` now expects the refusal from `Control_L+Alt_L+a`.
- Docs: SKILL.md Posted Keys, CONTEXT.md Posted Keys, the CLI usage text, ADR 0013 route row 07 done, and the headless-max spec row.
- Not built here: choosing an item in an open menu by its mnemonic (ticket 09's route). Alt+F opens the menu; the next key goes through ticket 09 once it lands.


### 2026-11-03: the WPF chord's foreground (laptop/fx-gone-wait-window-closed)

- Live failure (full laptop run at 0693039): after `press-key Control_L+s --post`, the foreground was the WPF window, and press-key's own watch printed `Note: the App took the foreground`. That watch reports only a move during the Action, and PostChord refuses when the target's thread owns the foreground, so WPF was not the foreground when the chord started.
- Not decided between: (a) a late WPF start-up activation that fell inside press-key (the failing run was fast, 1.83 s for the whole test); (b) the activation pair, sent while attached to WPF's thread just after the cover's Headed activate-window gave the helper the foreground right (round 4's probe had no such right). No behavior was changed: that waits for the orchestrator's decision.
- Test change (`test/e2e/posted_chords_test.go`): one second after the cover, the test fails before the chord if the foreground has already moved, so (a) now fails before the chord and (b) after it.
- Live: `TestPostedChordFiresAWPFKeyBinding` passed 2 runs in a row under the lock (7.3 s, 7.2 s). The foreground stayed with the cover through the chord both times.
