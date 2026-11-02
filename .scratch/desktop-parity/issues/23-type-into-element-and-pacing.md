# 23: type-text into an Element, and pacing

**What to build:**

- `type-text <window> --into <element> <text>`: type into a named Element instead of whatever has the focus. agent-desktop's `type <ref> <text>` (research row 23).
- `--interval-ms <n>` on Headed and Posted typing: a pause between characters, for apps that drop fast input (agent-desktop #348).

Routes for `--into`, most headless first:

- Page Element: CDP focus (`DOM.focus`), then `Input.insertText`. Headless.
- Classic edit control (the Element has a NativeWindowHandle of class Edit/RichEdit): EM_SETSEL to the end (or keep the caret), then the existing EM_REPLACESEL route. Headless, no focus change.
- Text-pattern field: moved to `../../headless-max/issues/13-caret-aware-typing.md` (2026-11-03, see Comments).
- Otherwise Headed: activate, click the Element, then type. Without `--headed`, refuse with "nothing was sent" and the hint.

`--interval-ms` does not apply to the Headless routes (they insert in one call); the output says so if both are given.

**Blocked by:** none

**Status:** ready-for-agent (the `--into` legs for Page Elements and classic edit controls landed 2026-11-03; the Text-pattern leg moved to headless-max 13; left: `--interval-ms`)

- [x] `--into` in CLI, protocol, Batch
- [ ] `--interval-ms` in CLI, protocol, Batch
- [x] Routing by Element origin and kind (Page, classic edit, Headed/refusal)
- [x] ~~Routing: the Text-pattern leg~~ moved to headless-max 13
- [x] Offline tests: each route, the refusal
- [ ] Offline tests: the interval note
- [x] Opt-in e2e: fixture edit box by ID, covered (`TestHeadlessTypeTextIntoElement`)
- [x] SKILL.md typing section

## Comments

### 2026-11-03: `--into` landed (with headless-max 19)

Branch `hm19-notepad-into`. Landed together with headless-max ticket 19,
which needed `--into` to type into a covered Windows 11 Notepad. ADR 0011
has a dated amendment to Decision 4.

- **CLI:** `type-text <window> --into <element index | name: | id:> <text>`,
  with `--into` right after the Window (later it is text), and first in a
  Batch step. `@x,y` is refused; so is `--into` with `--post`. A `--headed`
  between the Window and the text is the global flag.
- **Protocol:** `TypeTextArgs.IntoIndex` (`into_index`) and `IntoRef`
  (`into_ref`).
- **Page Element:** cheap, so it is done. The field is read first (refused
  with nothing sent when it takes no text, is read-only or disabled), then
  `page.Origin.TypeInto` runs `DOM.focus` and the same `Input.TypeText` as
  type-text on the Page's focus (a newline is Return, a tab is Tab). There
  is no read-back. `--headed --into` on a Page Element is refused, as
  `set-value --headed` is.
- **Classic edit control:** the Element's NativeWindowHandle is read
  (`TextSelector.ElementWindow`, as select-text does). A writable Edit or
  RichEdit (RichEditD2DPT included) with text that has no control
  characters goes through the existing `typeIntoEdit` (EM_REPLACESEL with
  the length read back). It does **not** EM_SETSEL to the end first: the
  text goes in at the control's own selection, so select-text then
  type-text --into replaces the selection.
- **Otherwise:** with `--headed`, v1's pipeline: activate, a left click on
  the Element's point, then type. Without it, a refusal with "nothing was
  sent" and the exact retry `winuse --headed type-text <id> --into <ref>
  <text>`. The text is never echoed. A read-only or disabled edit control
  gets a pick-another-Element hint instead.
- **Batch:** an unheaded `--into` step is Headless or sends nothing, so
  `headlessStep` counts it as Headless without reading the Element.
- **Skipped:** `--interval-ms` (not trivial: it touches the Headed and
  Posted typing loops) and the Text-pattern leg (needs headless-max 13).
  Both stay on this ticket.

### 2026-11-03: Text-pattern leg moved to headless-max 13

The de-duplication pass moved the Text-pattern `--into` leg to
`../../headless-max/issues/13-caret-aware-typing.md`. It was the same splice
as that ticket, aimed at a named Element, so it waits for the same triage
decision. This ticket now covers only `--interval-ms`, which has no conflict
and stays ready-for-agent.
