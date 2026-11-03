# 08: Polite real input

**What to build:** Headed Actions (in the desktop lane) disturb the user as little as possible, following cua's and Raymond Chen's practice.

- **Wait while the user is typing:** GetLastInputInfo idle under about 500 ms delays the start, up to a limit, then "nothing was sent; the user is typing".
- **Save and restore:** remember the foreground Window and the cursor position before the Action. Afterwards, restore the cursor only if it is within 2 px of where winuse left it, and give the foreground back to the user's Window when winuse took it.
- **Confirm the target:** after activating, confirm the target is exactly the foreground for up to 500 ms (a WM_NULL round trip), else "nothing was sent".
- **One SendInput batch per step**, then a drain sentinel before restoring.
- **Yield to the user:** the low-level hooks tell real input from injected input (LLMHF_INJECTED, LLKHF_INJECTED). Real user mouse or key input during a Headed Action stops it, like Esc.
- **Release held keys and buttons on every exit path** (a ledger of what winuse pressed).
- Chromium re-activates itself after a restore: re-check the foreground for a short while and give it back unless the user changed it.
- Check integrity through TokenIntegrityLevel, never through SendInput's return value (as today).

**Blocked by:** 06

**Status:** done

- [x] Typing wait; save and restore; target confirmation
- [x] Injected-vs-real detection and yield; key ledger
- [x] Offline tests over faked input state and timings
- [x] Opt-in e2e: a Headed click on a fixture puts the cover back in front and the cursor back where it was (written and compiled; the live run is still to do, live runs paused)
- [x] SKILL.md Headed section

## Comments

**2026-11-03, what was built.** `internal/helper/polite.go` and `internal/win/polite.go`, wired into `runAction` and the Headed Batch.

- Typing wait: `GetLastInputInfo` idle under 500 ms delays a Headed Action, polled every 50 ms for up to 3 s (clamped by the request's deadline); past that it is refused: "the user is typing or using the mouse right now, so winuse did not take the foreground; nothing was sent". winuse's own last input does not count: the pipeline records when its input ended, and an idle time within 100 ms of that is winuse's.
- Save and restore: the foreground Window, the cursor and the list of open Windows (`win.Collect`, popup menus included) are saved before the Action; a Batch saves once before its first step and gives back once after its last. The cursor goes back when it is within 2 px of where winuse left it (read after the drain).
- Target confirmation and drain: after the activation, a WM_NULL round trip (`SendMessageTimeout`, SMTO_ABORTIFHUNG, 500 ms) to the target; no answer is refused with nothing sent. After the act, a second round trip drains the injected input before anything is given back.
- One SendInput batch per step: left as it was. A click, a chord and a text chunk already go in as one `SendInput` call each; drag stays three calls (down, travel, up) by design, so its drop can wait. The drain sentinel is the WM_NULL round trip above.
- Yield: while a swallowing (Headed) Esc scope is armed, the hook latches a stop on any real key down (LLKHF_INJECTED clear) and, through a new WH_MOUSE_LL hook on the same thread, any real button down or wheel turn (LLMHF_INJECTED clear). The input passes through to its app. The stop exits 130 with "winuse stopped: the user used the keyboard or mouse during a Headed Action" and the Esc stop's hint. Mouse moves are not counted: unmeasured whether `SetCursorPos` moves read as injected in a WH_MOUSE_LL hook, and a nudge of the mouse should not stop an Action. Passive (Headless) scopes and waiting requests do not yield.
- Ledger: `sendInput`, the one `SendInput` gate, records key downs (by virtual key, with the extended flag) and button downs until their ups go through; text (KEYEVENTF_UNICODE) is never held. `runAction` releases what is still held on every exit path.
- Chromium: after a hand-back to the user's Window, a `Chrome_WidgetWin_` target is watched for 600 ms and the foreground handed back again if it re-activates, unless the user moved it elsewhere.
- Integrity: unchanged (the token check before any input).

**2026-11-03, decision (orchestrator, option C).** Giving the foreground back after every Headed Action, literally, would close what the Action opened: a popup menu, a dropdown, a dialog, Start. So the foreground goes back after a Headed Action (once at the end of a Batch) unless the Action left something open that needs it: a new Window of the target's App (a `#32768` popup menu included), or the foreground is no longer the target itself (a flyout, Start, or the user moved it). activate-window and maximize-window always keep it; hover keeps the pointer; a shell surface (the taskbar) keeps the foreground. The cursor goes back under the 2 px rule either way. When winuse keeps the foreground for a menu or Window it left open, or it moved elsewhere during the Action, the output ends with a Note: `Note: the foreground stays on window <id>: the Action left a menu or Window of its App open` or `Note: the foreground was not given back: it moved to window <id> during the Action`. A Batch that ran past failed steps keeps the Note after its step lines. The two offline tests that assumed the foreground stayed (selfactivation_test.go's Headed click, shell_test.go's Headed Start) now assert C: the user's Window is in front again after the click, and the taskbar keeps it after Control_L+Escape.

**2026-11-03, live.** Live runs are paused (the user is gaming). `test/e2e/polite_input_test.go` (a `--headed` double click on the fixture's canvas under a covering fixture: the cover is in front again, the pointer back within 2 px, `ok` alone) is written and compiled, not run.

**2026-11-03, live fix (branch ma08-live).** The first full live run (main 72a08a4) lost the tail of long Headed typing: `TestTypeTextLongText` got 186 of 240 runes with `ok`, and `TestOverlayDuringAction` and `TestInjectedEscDoesNotStopTheBatch` the same. The give-back moved the foreground while injected keys were still in the system's raw input stream, so the rest went to the Window given the foreground (possibly the user's own). The WM_NULL drain could not catch it: a sent message is handled before queued input. Fix: after the act, and after any release of held keys, `win.EscWatcher.FlushInput` injects a sentinel key (VK 0xE8 with winuse's own extra-info tag) that the Esc hook swallows and counts; once the hook has seen it, every earlier event has passed the hooks and sits in the target's queue. Without the hook, the pipeline pauses 200 ms instead. Offline tests: the hook swallows the sentinel armed or not and counts its up only; an untagged 0xE8 passes; the pipeline flushes after the act and after a release, before the give-back's activation. Live after the fix: the three typing tests pass. The yield ignores every injected event (LLKHF_INJECTED, LLMHF_INJECTED), as before; `TestInjectedEscDoesNotStopTheBatch` passes once it counts Escape presses (`EscapePresses`, new in the fixture) instead of `LastKey`, which the typed keys after a mid-typing Escape overwrite. `TestHeadedClickBringsTheWindowForward` and `TestHeadlessSetValueKeepsFocusAndForeground` now expect option C: the cover is in front again after the Headed Action.

### 2026-11-03: the tail of a Headed type-text was still lost (branch e2e-typing-tail)

- Full runs with a File Explorer window as the home Window lost the end of Headed type-text's text, with type-text ok: TestTypeTextTypesUnicode got "Hello, 世界! Caña 🎉" of "Hello, 世界! Caña 🎉 déjà-vu", and TestTypeTextLongText, TestOverlayDuringAction and TestInjectedEscDoesNotStopTheBatch lost theirs. A probe with a second fixture as the home Window showed the tail reached neither Window: it was dropped.
- Cause: the give-back calls win.Activate, whose Workaround 2 attached to the current foreground thread for SetForegroundWindow. At the give-back that thread is the target that was just typed into; keyboard input it had not handled yet was lost when the queues split again. The flush (every event past the hooks) and the WM_NULL round trip (sent messages go ahead of input) do not cover it. Measured: with the attach, 4 of 8 texts lost their tail; without it, 12 of 12 arrived whole.
- Fix: Activate attaches only as a last resort (bringForward): after the zero-distance move, the plain SetForegroundWindow is tried and verified first, and only when Windows refuses it, or the change does not show, is the call repeated under the attach. That also stops winuse attaching to the user's own foreground thread when it activates a target, which could drop the user's keys the same way. Offline: TestBringForwardAttachesOnlyWhenRefused. Live: the probe 12/12, and the four tests twice each with an Explorer Window of ours as the home Window.
