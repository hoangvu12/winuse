# 11: `--interactive` hides disabled controls

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:** `Element.Interactive()` returns false for anything disabled (`wm:internal/wstate/find.go:112-119`), so `get-window-state --interactive` leaves out a disabled Submit button. The agent then looks for a control that is right there, or concludes the form has no Submit button.

**What to build:** keep disabled Elements in the interactive view with their `disabled` state, as the full tree prints them (SKILL.md line 122 lists `disabled` among the states).

**Blocked by:** none (pairs well with 06: the agent sees the button is disabled, and the Action waits for it)

- [ ] `Interactive()` ignores `Disabled`, or the view applies it separately
- [ ] views test updated
- [ ] SKILL.md: what `--interactive` keeps

**Verified by the orchestrator:** `find.go:116-119`. One reviewer (1); the orchestrator agrees. Check `find`'s own filters still behave as intended.

**Conflicts:** none known.
