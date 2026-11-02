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

**Status:** ready-for-agent (measured on Win32, WinForms and WPF; see Comments)

- [ ] Probe on Notepad (Win11), WordPad, Paint, an MFC app, a WinForms app and a WPF app: Ctrl+S, Ctrl+A, Ctrl+Z, Shift+Tab, Alt+F; foreground and user key state before and after
- [ ] If adopted: attach, state, post, restore on a disposable thread; restore on every exit path
- [ ] Offline tests: foreground-thread refusal, Win refusal, Alt policy
- [ ] Opt-in e2e on the Apps the probe proved
- [ ] ADR 0011 amendment, CONTEXT.md "Posted Keys", SKILL.md

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
