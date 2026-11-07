Status: ready-for-agent (user, 2026-11-07: "do that, we gotta catch up")

# Performance: catch up with agent-desktop on the action path

## Problem statement

A benchmark against vercel-labs/agent-desktop 1.2.0 on Windows (`bench2.py`, results `r2-baseline.json`, 16 runs per case, whole CLI call, min / median) shows winuse ahead on every read and behind on every click:

| Window | Case | agent-desktop | winuse |
|---|---|---|---|
| Calculator (89 nodes) | read | 177 / 215 ms, 14.1 KB | 51 / 56 ms, 2.3 KB |
| Calculator | click | 178 / 203 ms | 375 / 387 ms |
| Explorer System32 | read | 346 / 382 ms, 51.7 KB | 136 / 193 ms, 14.8 KB |
| Explorer System32 | click | 455 / 759 ms | 383 / 905 ms |
| Discord (Electron) | read | 780 / 876 ms, 193 KB | 163 / 196 ms, 25.8 KB |
| Discord | skeleton | 784 / 870 ms | 4988 / 5136 ms |

The work itself is fast: with the fixed waits switched off in an experimental build, a winuse click takes 40-65 ms in the helper (trace `duration_ms`) and 55-60 ms for the whole CLI call. The time goes to waits:

- the 300 ms opened-window settle after every reported Action (ticket 01);
- the Agent Cursor's travel, which the Action waits for: about 260-300 ms whenever the target changes (ticket 02). With alternating targets a default click is 610-670 ms in the helper against about 40 ms with both waits off;
- the Chromium settle on a skeleton read, which always runs its whole 5 s budget (ticket 03).

Ticket 04 is a correctness bug found during the run: `launch-app` stops on any physical input and blames the Escape key.

## Method

`bench2.py` runs agent-desktop and winuse variants in blocks, two passes in opposite order. This machine stalls process starts at random (a usage error alone took up to 4.3 s), so minimums are the tools' own cost; medians are reported too. Clicks go only to Calculator's Seven and Explorer's Refresh; Discord is read-only. The winuse variants come from a scratch copy of HEAD with `WINUSE_BENCH_NOSETTLE` and `WINUSE_BENCH_NOCURSOR` switches (not in the product).

## Tickets

| # | Title | Status |
|---|---|---|
| 01 | Report opened Windows without the 300 ms wait | done |
| 02 | The Agent Cursor never holds an Action back | wontfix (user: keep the cursor as it is) |
| 03 | A skeleton read skips the Chromium settle | done |
| 04 | launch-app stops only on Esc, with the right message | done |

## Not in scope

What agent-desktop does that the benchmark says not to copy: a process per command, refs re-identified by a whole-window walk before every action (its click cost grows with the window: 178 ms on Calculator, 455 ms on Explorer), JSON as the default output.
