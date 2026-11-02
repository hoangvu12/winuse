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

**Status:** needs-triage (probe first; changes ADR 0011 Decision 5 and the headless-parity spec out-of-scope "Posting chords with modifiers")

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
