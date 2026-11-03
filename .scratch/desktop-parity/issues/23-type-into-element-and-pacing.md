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

**Status:** done

- [x] `--into` in CLI, protocol, Batch
- [x] `--interval-ms` in CLI, protocol, Batch
- [x] Routing by Element origin and kind (Page, classic edit, Headed/refusal)
- [x] ~~Routing: the Text-pattern leg~~ moved to headless-max 13
- [x] Offline tests: each route, the refusal
- [x] Offline tests: the interval note
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

### 2026-11-03: triage decision (user)

Kept: `--interval-ms` is what is left.

### 2026-11-03: `--interval-ms` landed (branch dp23-interval)

- CLI: `type-text <window> [--post | --into <ref>] [--interval-ms <n>] <text>`. The flag sits among the leading flags in either order, at most once each, and the same holds in a Batch step. The value must be 0 to 1000; a missing or bad value is a usage error. After the flags, a flag's spelling is text, as before.
- Protocol: `TypeTextArgs.IntervalMS` (`interval_ms`). The helper checks the range again and refuses with nothing touched.
- Helper (`interval.go`): the routes that type key by key are paced, one piece at a time with `Services.Sleep` between pieces. A piece is a rune, and a `\r\n` pair stays one piece. The paced routes are Headed type-text, `--into --headed`, and Posted type-text, which keeps the count of messages posted for its partial-failure wording. A stop (Esc, or the request's deadline) between pieces ends the typing as the batches did.
- The Headless routes (the classic edit insert, `--into`'s classic and caret-splice routes, and the Page) put the text in at once. Their `ok` gains "--interval-ms does not apply: this route puts the text in at once", joined to any note they already print.
- Not paced: press-key (one key) and the Page's typing, which goes through CDP in one call.
- Tests: `internal/helper/interval_test.go` (pieces, the pause and stop, Headed and Posted pacing, the Headless note, the range refusal) and `internal/cli/interval_test.go` (flag orders, text after the flags, errors, the Batch step). No e2e: the pacing is timing only, and the routes it paces have e2e already.
- SKILL.md: the typing section and the command reference.

