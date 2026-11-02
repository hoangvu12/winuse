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

**Status:** needs-triage (changes the key-routing rule of ADR 0011 Decision 4; option 1 overlaps desktop-parity 23)

- [ ] Pick an option
- [ ] Offline tests of the routing decision
- [ ] e2e: `TestHeadlessTypeTextIntoNotepad` passes covered
