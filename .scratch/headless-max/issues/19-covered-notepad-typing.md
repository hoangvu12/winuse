# 19: Headless typing into a covered Windows 11 Notepad

**What to build:** a Headless route for `type-text` (and `press-key` Return/Escape where they apply) into Windows 11 Notepad while it is covered. Today winuse falls back to the Headed route: it activates Notepad, shows the banner and types with real keys.

Found by the first live e2e run (2026-11-03; `TestHeadlessTypeTextIntoNotepad` fails, and a diagnostic confirmed the cause):

- While Notepad is the foreground, its thread's focus is the editor (`RichEditD2DPT`, style 0x50000104), and the classic edit route (EM_REPLACESEL) works.
- Once another Window takes the foreground, Notepad (a XAML-island app) reports no focused control at all: the key route probe reads `focus ""` (style 0x0).
- With no focused edit control, the routing predicate finds no Headless route, and type-text goes Headed. Live result: the foreground moved to Notepad, and with the e2e test's timing some keys were lost ("hello oot").
- `select-text` is not affected. It targets the Element's own window (UIA NativeWindowHandle), not the thread's focus, and stays headless on covered Notepad (`TestHeadlessSelectTextInNotepad` passes).

Options to weigh:

1. Route by the Element, not the thread focus: `type-text <window> --into <element>` (desktop-parity 23) could target the editor's NativeWindowHandle with EM_REPLACESEL even when the thread has no focus. Needs the caret position: EM_REPLACESEL inserts at the control's own selection, which survives deactivation.
2. Remember the last focused edit control per Window from the last read while it was active. This is fragile, and a stale handle could type into the wrong tab.
3. Keep the Headed fallback, but say in the output why the route was Headed ("Notepad reports no focused control while covered").

**Blocked by:** none (relates to desktop-parity 23)

**Status:** done (options 1 and 3, 2026-11-03; ADR 0011 Decision 4 amended)

- [x] Pick an option
- [x] Offline tests of the routing decision
- [x] e2e: `TestHeadlessTypeTextIntoNotepad` passes covered

## Comments

### 2026-11-03: options 1 and 3 picked and landed

The orchestrator picked option 1 and option 3; option 2 is rejected (a
stale handle could type into the wrong tab). Branch `hm19-notepad-into`.

- **Option 1:** `type-text <window> --into <element> <text>` from
  desktop-parity 23 (see its Comments). On an Element whose
  NativeWindowHandle is a writable classic edit control (RichEditD2DPT
  included) it inserts with EM_REPLACESEL at the control's own selection
  and reads the length back, Headless, whatever the thread's focus. With
  no Headless route and no `--headed`, it refuses with "nothing was sent"
  and the `--headed` retry. It never falls back.
- **Option 3:** plain `type-text` keeps its Headed fallback. When the
  probe finds no focused control, the output is `ok (typed Headed: window
  N reports no focused control while it is in the background, so winuse
  brought it to the foreground; to type without the foreground, name the
  field: winuse type-text N --into <element> <text>)`.
- **Docs:** ADR 0011 amendment (2026-11-03), CONTEXT.md's Headless and
  Headed Action, SKILL.md ("Type into a named field"), test/e2e/README.md.
- **Offline tests:** `internal/helper/typeinto_test.go` (each route, the
  refusals, the Headed click and type, the Page route, the predicate, the
  Batch scope, the option-3 note); CLI and Batch parsing in
  `internal/cli`.
- **e2e (2026-11-03, live):** `TestHeadlessTypeTextIntoNotepad` now
  finds the `Text editor` Element in get-window-state and types with
  `--into`: PASS, foreground unchanged, no banner. The new
  `TestHeadlessTypeTextIntoElement` (fixture password Edit by `id:1002`
  while the name Edit has the focus, a Batch step by `id:1001`, the OK
  button refused): PASS. `TestHeadlessSelectTextInNotepad` and
  `TestHeadlessTypeTextIntoClassicEdit`: PASS. In this run, the covered
  Notepad's probe read focus `RichEditD2DPT` (style 0x50000104), not
  `""` as in the first run. So this run did not reproduce the no-focus
  state; the `--into` route does not depend on it either way.
