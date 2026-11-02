# 24: Batch across several Windows

**What to build:** let a Batch step name its own Window, so one Batch can, for example, click a button in an App and then act in the dialog it opened. agent-desktop's `batch` takes a JSON list where each step has its own target (research row 60).

Today a Batch is `batch <window> <action> ... then ...`, one Window for all steps (`internal/cli/batch.go`; `skills/winuse/SKILL.md`).

- Step syntax: an optional `in <window>` prefix (`then in 3412 press-key Return`). Steps without it use the Batch's Window.
- A step's Window may be one that an earlier step opened: allow `in new` to mean "the Window the previous step opened" (uses `02-report-opened-windows.md`'s diff).
- Element Indexes refer to the latest Window State of that step's Window; a step on a Window with no read state accepts only `name:`/`id:` (resolved live through ticket 17) or Window-level Actions.
- Esc scope: Headed if any step is Headed, as today, computed over all steps.

**Blocked by:** 02 (for `in new`), 17 (for `name:` on unread Windows)

**Status:** needs-triage (SKILL.md Batch rules; Batch scope design)

- [ ] Parser and protocol per-step Window
- [ ] `in new`
- [ ] Offline tests
- [ ] Opt-in e2e: click a fixture button, then Return in the message box, in one Batch
- [ ] SKILL.md Batch section
