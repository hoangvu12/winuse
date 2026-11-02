# 11: maximize-window

**What to build:** `maximize-window <window>`. agent-desktop has `maximize`.

Windows has no non-activating maximize (ADR 0011 consequences): `ShowWindow(SW_MAXIMIZE)` activates. Options:

1. **Headed Window Action** (research recommendation): requires `--headed`, activates, `SW_MAXIMIZE`, verifies `IsZoomed`. Without `--headed`: "nothing was sent", hint to pass `--headed`, or to use `move-window` and `resize-window` to the monitor's work area instead.
2. **Headless "fill the work area"**: `SetWindowPos(SWP_NOACTIVATE)` to the monitor's work area. Looks maximized, is not (no maximized state, borders stay). Could be `resize-window <window> --fill`.


**Blocked by:** none

**Status:** ready-for-agent (option 1; amend CONTEXT.md "Every Window Action is always Headless", `CONTEXT.md:69-70`, and ADR 0011)

- [ ] The chosen form(s) in CLI, protocol, helper
- [ ] Offline tests
- [ ] Opt-in e2e on the fixture
- [ ] CONTEXT.md, ADR 0011 amendment, SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop: a real maximize (option 1). It activates, so it is a Headed Window Action and needs `--headed`; without it, "nothing was sent" with the hint. Option 2 is not built.
