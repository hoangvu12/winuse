Status: ready-for-agent (triaged by the user 2026-11-03)

# Desktop parity with vercel-labs/agent-desktop

## Problem statement

agent-desktop (v1.0.2, `main` at 9eb9b29 on 2026-11-02) has 60 commands. winuse matches or beats 21, partly covers 17 and lacks 22. Four of the 22 are names agent-desktop only reserves and refuses, so 18 are real gaps. The full table is in `../headless-gaps/agent-desktop-parity.md` sections 1 and 2.

Headlessness is no longer the gap: winuse is more headless than agent-desktop on Windows (section 3 of the research). The gaps are breadth: clipboard, desktop waits, `find` and region reads for dense trees, reporting Windows an Action opened, displays, shell surfaces, notifications, traces, machine-readable output, blocked dangerous combos.

The user wants winuse to match agent-desktop as closely as possible, without giving up the headless work.

## Solution

Add agent-desktop's missing features as winuse extensions, and keep winuse's Codex-style defaults:

- the ADR 0004 text tree stays the default; JSON is opt-in (`--json`);
- the Element Index stays; `find`, region reads and live `name:`/`id:` re-resolution replace agent-desktop's snapshot-qualified refs;
- commands not aimed at a Window (clipboard, displays, notifications) get a new CONTEXT.md term, proposed "Desktop command" (ticket 01);
- every new command is Headless where a route exists. Headed-only additions (11, 13, part of 06 and 09) say so and refuse without `--headed` where the policy of `../headless-max/spec.md` requires it.

## Decisions (user, 2026-11-03)

Every open ticket was triaged and is ready-for-agent. The rule: do it like agent-desktop, Headless first; agent-desktop's screen-taking route is the fallback and needs `--headed`.

- 01: like agent-desktop; no clipboard-history exclusion and no ask-first rule. The CONTEXT.md term "Desktop command" is accepted.
- 04: winuse's Element Index stays; `find` and region reads add indexes after the existing ones (option c). **Revisit if agents are seen failing on stale indexes**: then consider agent-desktop's snapshot-qualified refs with re-identification.
- 07: `--json` opt-in; text stays the default.
- 09, 18: Headless reads and Invoke first; shell accelerators only with `--headed`.
- 10: `--force` with agent-desktop's protected-process list; SKILL.md's "winuse never ends a process" is reworded.
- 11: a real maximize, Headed, needs `--headed`.
- 20: a Batch continues past failures by default (uncertain ones too, marked); `--stop-on-error` stops.
- 24: per-step Windows (`in <window>`) plus `in new`.

Details are in each ticket's Comments.

## Research

- `../headless-gaps/agent-desktop-parity.md`: every command and feature, with `ad:` and `winuse:` citations, what not to adopt, and what winuse has that agent-desktop lacks (do not regress).
- `../headless-parity/agent-desktop-comparison.md` and `../browser-merge/agent-desktop-research.md`: the earlier headless and browser comparisons.

## Tickets

| # | Title | Priority | Effort | Status |
|---|---|---|---|---|
| 01 | Clipboard commands | P1 | S | done |
| 02 | Report Windows that an Action opened | P1 | S-M | done (live e2e run still to do) |
| 03 | Desktop waits | P1 | M | ready-for-agent |
| 04 | `find` and a region read | P1 | M-L | ready-for-agent |
| 05 | Blocked dangerous combos with `--force` | P1 | S | done |
| 06 | Modifiers on point clicks and wheel | P1 | S | ready-for-agent |
| 07 | Opt-in `--json` output with codes and delivery | P1 | L | ready-for-agent |
| 08 | Display list and full-screen capture | P2 | S | done |
| 09 | Shell surfaces | P2 | M | ready-for-agent |
| 10 | close-app | P2 | S-M | done |
| 11 | maximize-window | P2 | S | done |
| 12 | Select an option by name in UIA combos and lists | P2 | S-M | done |
| 13 | Hover and drag by UIA Element | P2 | S | ready-for-agent |
| 14 | Caller-set timeouts | P2 | S | done |
| 15 | `winuse skill` prints the embedded skill | P2 | S | done |
| 16 | `status` | P2 | S | done |
| 17 | Live re-resolution of `name:`/`id:` | P2 | M | ready-for-agent |
| 18 | Notifications | P3 | M | ready-for-agent |
| 19 | Session traces with redaction | P3 | M-L | ready-for-agent |
| 20 | Batch improvements | P3 | S-M | ready-for-agent |
| 21 | Agent Cursor polish | P3 | S | wontfix (folded into multi-agent 04) |
| 22 | list-windows details and filter | P2 | S | done |
| 23 | type-text into an Element, and pacing | P2 | S-M | ready-for-agent (`--into` done; Text-pattern leg moved to headless-max 13; left: `--interval-ms`) |
| 24 | Batch across several Windows | P3 | M | ready-for-agent |

24 tickets: 7 P1, 12 P2, 5 P3. Tickets 22-24 were added after a cross-check of the research's "partial" rows (rows 4, 23 and 60) against the tickets. The research's ticket 15 (`launch-app` options) moved into `../headless-max/issues/01-launch-without-foreground.md`, which changes the same code.

`needs-triage` meant the ticket conflicts with an ADR, CONTEXT.md, SKILL.md or a `docs/codex-parity.md` row, and the user decides first. Each ticket names the conflict.

## Testing decisions

- Offline (`go test ./...`): CLI parsing, helper logic behind Services fakes, a parity check that every documented command has a handler.
- `internal/skill/parity_test.go` only fails when a Codex row disappears or loses its status, so new commands never trip it. Rows whose text a ticket contradicts are reworded and keep their status.
- Opt-in e2e (`WINUSE_E2E=1`): written and compiled, run by the user only.

## Not adopted (from the research)

- Snapshot-qualified, disk-persisted refs as the default (replaces Codex's `element_index`).
- A JSON tree (more tokens than the ADR 0004 grammar).
- `key-down`, `key-up`, `mouse-down`, `mouse-up` (agent-desktop ships them as refusals only).
- `get` and `is` (the tree line carries the states; `wait` covers polling).
- A C library (ADR 0001).
- ~~Multi-agent cursors and agent ids (one helper serializes every Action).~~ Reversed 2026-11-03 by ADR 0014: see `../multi-agent/spec.md`.
