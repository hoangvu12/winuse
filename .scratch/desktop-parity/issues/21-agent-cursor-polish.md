# 21: Agent Cursor polish

**What to build:** from agent-desktop's recent overlay work (`main` after v1.0.2: #349, #350, #353, #357, #361):

- After a Headless type-text or set-value into a field with a Text pattern, draw a short insertion caret at the caret position for the Agent Cursor's dwell time.
- Optional label text and accent colour through environment variables (`WINUSE_CURSOR_LABEL`, `WINUSE_CURSOR_COLOR`); defaults unchanged.
- The cursor stays hidden from captures and never activates (ADR 0006, CONTEXT.md).

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Caret rectangle from TextPattern selection bounds
- [ ] Environment options
- [ ] Offline tests of option parsing
- [ ] Opt-in e2e: visual check by the user

## Comments

### 2026-11-03: triage decision (user)

Kept as written.
