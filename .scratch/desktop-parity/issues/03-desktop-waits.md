# 03: Desktop waits

**What to build:** extend `wait` beyond Pages (today: selector, text, URL, load state, download; `internal/cli/cli.go:187-193`):

- `wait window <app|title> [--gone]`: a Window of that App or title appears (or disappears). Prints its list-windows line.
- `wait <window> text <text>`: the text appears in the Window's UIA tree (Pages keep today's route).
- `wait <window> <element> enabled|disabled|gone|value <v>|checked|unchecked|expanded|collapsed`.
- `wait <ms>`: a plain pause, capped.
- `--timeout <s>` on all of them; raise the cap above 25 s with the request deadline (ties into ticket 14).

agent-desktop's predicates are in `ad:docs/commands/` (row 52 of the research) plus post-action `--wait-for`. Post-action waits are out of this ticket.

- Poll: EnumWindows for window waits (Laptop probe: found a new Window in about 0.4 s); a bounded UIA walk every 250-500 ms for text and element waits. UIA events were measured as a cheaper signal (WindowOpened 0.63 s after launch, value change in 55 ms), but winuse banned event subscriptions after a Codex freeze; stay with polling and note it.
- A Window that goes away mid-wait fails with "the Window closed".
- Element references resolve once at the start; `gone` succeeds when the Element is stale.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Predicates in CLI and protocol, routed by origin (UIA Window vs Page)
- [ ] Polling loops bounded by the request deadline
- [ ] Offline tests per predicate with faked state over time
- [ ] Opt-in e2e: wait for a fixture dialog, a value change, a closed Window
- [ ] SKILL.md waits section
