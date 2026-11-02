# 13: Hover and drag by UIA Element

**What to build:** `hover <window> <element>` and `drag <window> <element> <element | @x,y> [--drop-delay <ms>]` on native Elements. Today both accept only `@x,y` or a Page Element (`internal/helper/pointer.go:100-101`, `:136-137`). agent-desktop takes refs for both.

- Resolve the Element's clickable point, as a Headed click does, then the existing Headed hover or drag.
- `--drop-delay`: wait between the last move and the button-up, for targets that need hover time before accepting a drop.
- Native hover and drag stay Headed: no headless route exists outside classic Win32 (`../../headless-gaps/headless-100.md` G3). Page Elements keep their Headless CDP route.

**Blocked by:** none

**Status:** ready-for-agent (CONTEXT.md still lists hover and drag as Headed; no change)

- [ ] Element forms in CLI, protocol, Batch
- [ ] `--drop-delay` on both drag routes (CDP and native)
- [ ] Offline tests of parsing and point resolution
- [ ] Opt-in e2e: fixture drag between two Elements
- [ ] SKILL.md
