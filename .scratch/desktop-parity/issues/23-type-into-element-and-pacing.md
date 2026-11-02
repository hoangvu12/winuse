# 23: type-text into an Element, and pacing

**What to build:**

- `type-text <window> --into <element> <text>`: type into a named Element instead of whatever has the focus. agent-desktop's `type <ref> <text>` (research row 23).
- `--interval-ms <n>` on Headed and Posted typing: a pause between characters, for apps that drop fast input (agent-desktop #348).

Routes for `--into`, most headless first:

- Page Element: CDP focus (`DOM.focus`), then `Input.insertText`. Headless.
- Classic edit control (the Element has a NativeWindowHandle of class Edit/RichEdit): EM_SETSEL to the end (or keep the caret), then the existing EM_REPLACESEL route. Headless, no focus change.
- Text-pattern field: the caret splice of `../../headless-max/issues/13-caret-aware-typing.md`, aimed at this Element. Headless.
- Otherwise Headed: activate, click the Element, then type. Without `--headed`, refuse with "nothing was sent" and the hint.

`--interval-ms` does not apply to the Headless routes (they insert in one call); the output says so if both are given.

**Blocked by:** none (the Text-pattern leg needs headless-max 13)

**Status:** ready-for-agent

- [ ] `--into` and `--interval-ms` in CLI, protocol, Batch
- [ ] Routing by Element origin and kind
- [ ] Offline tests: each route, the refusal, the interval note
- [ ] Opt-in e2e: fixture edit box by name, covered
- [ ] SKILL.md typing section
