# 17: ADR 0013, CONTEXT.md, skill and the impossible list

**What to build:** the decision record and documentation for this feature.

- **ADR 0013 "Headless by readback, Posted by opt-in"**, amending ADR 0010 and 0011:
  1. A route with a readback may be Headless by default (accelerator lookup, menu command, context menu, select-text, point-to-Element, caret splice).
  2. A best-effort message route without a readback is opt-in per command (`--post`): Posted Keys, Posted Clicks, attached-state chords.
  3. Where no route exists, fail before sending with "nothing was sent" and the exact `--headed` retry. Never fall back on its own.
  4. No Headless or Posted route attaches to the foreground thread, sends input or moves the cursor.
  5. Launch and restore are non-activating calls; winuse reports when an App takes the foreground itself.
  Record each ADR reversal the tickets make (ADR 0010 Decision 1 for 11, the minimized refusal for 14, ADR 0011 Decision 5 for 07 and 12, ADR 0012 Decision 6 for 15).
- **CONTEXT.md**: narrow "Headed Action" to what is left; add Posted Clicks next to Posted Keys; add the new Actions (invoke-menu, select-text, ContextMenu).
- **SKILL.md**: a short "What winuse cannot do headless" list (from `../headless-gaps/headless-100.md` section 3) with the refusal wording, and when to reach for `--post` or `--headed`.
- **README**: one line on headless coverage.
- **docs/codex-parity.md**: reword rows the tickets contradict, keep each status; run `go test ./internal/skill/...`.

Write the ADR before the tickets that reverse earlier decisions land; update CONTEXT.md, SKILL.md and README as each ticket lands, then check the whole set here.

**Blocked by:** none for the ADR; the documentation pass follows 01-15

**Status:** needs-triage (the user accepts or edits the policy first)

- [ ] ADR 0013 drafted and accepted by the user
- [ ] CONTEXT.md terms
- [ ] SKILL.md impossible list and routing rules
- [ ] codex-parity rows reworded; parity guard test passes

## Comments

### 2026-11-03: round 3 probes (`../headless-gaps/probes-round3.md`)

- Add to the policy: **UIA SetFocus is never used in a Headless or Posted path.** It takes the foreground for any App, even without foreground rights (measured).
- Impossible list: take "keys into native Chromium without CDP" off (ticket 20), and "attaching CDP to an Electron app that is already running" too, when its fuse is on (ticket 15, opt-in). Minimized Chromium stays on the list.
