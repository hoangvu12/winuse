# What winuse can learn from agent-browser and agent-desktop

Written 2026-11-15. Merges the two research notes beside this file:

- `agent-browser.md`: agent-browser at 5338477 (2026-11-15, just after v0.43.0).
- `agent-desktop.md`: agent-desktop at 8ffecc1 (2026-11-14, latest release v1.3.1).

The earlier comparisons (2026-11-02) are mostly closed: winuse now does what put
either tool ahead then. Several upstream changes since are things winuse already
did (AutoSetFocus off, cached child reads, launching behind the user's windows,
background tabs, same-site cross-origin iframes). The ideas left are smaller.
Each note has the citations and the effort estimate for each one.

## Ranked

Ranked by turns saved first, then output quality, then robustness.

| # | Idea | From | Where | Effort |
|---|------|------|-------|--------|
| 1 | `--observe` also reads the Window the Action opened (menu, dialog), not only the named Window. ADR 0021 Decision 2 reads only "the Action's Window" today. | agent-desktop | native | S, ADR 0021 amendment |
| 2 | Wait briefly before refusing: poll a disabled or missing target (agent-desktop polls up to 5 s), and after a scroll wait for the click point to settle and re-centre once. Each refusal today costs a `wait` and a retry turn. | both | native and Pages | S–M |
| 3 | Hints on unnamed or duplicate controls: icon, image file, test id, and the text of the row the control sits in. Row text also helps native `find` and `--interactive`. | agent-browser | Pages first, native row text | S–M, ADR 0004 amendment |
| 4 | `set-value` on custom ARIA combobox/listbox widgets (Radix, Headless UI) instead of refusing; picking an option takes about 3 turns now. | agent-browser | Pages | M |
| 5 | One huge list should not end the tree read: one 2 s chunk timeout stops the whole walk, and `--skeleton` fetches every child with every property just to count them. | agent-desktop | native | M, measure first |
| 6 | Measurement: a script that builds HEAD and the merge-base and times fixture and read-only real-app reads (p50/p95); plus skill evals that need no desktop (model + skill + task, check the commands it writes). | both | tooling | M |
| 7 | `--if-changed` screenshots: skip an image identical to the last one (about 1,560 image tokens each). | agent-browser | native and Pages | S |
| 8 | Tables and grids: in `--skeleton` a table is one line with its row and column counts; cells print their row and column. | agent-desktop | native | S–M |
| 9 | `--stdin` / `--file` for `type-text`, `set-value`, `clipboard-set`: keeps secrets off the command line and avoids PowerShell 5.1 quote stripping. | agent-desktop | CLI | S |
| 10 | Page errors and console: a `Note:` when an Action throws, plus a read command. Turning it on is visible to the page. | agent-browser | Pages | M |

Smaller ones: refuse a CDP call to a session that already detached, at send time;
optional link URLs in Page reads; `wait js <expr>`; golden files for `--json` and
`--observe` diffs and fake clocks in timing tests; three skill lines (never copy
a value through the user's clipboard, put an Action and its `wait` in one Batch,
Escape in web apps can open "discard changes?"); skip the UIA cache request for
in-process Win32 proxies if #6 shows it pays; annotated screenshots with Element
Indexes drawn on them. `agent-desktop.md` also lists five open upstream Windows
bugs to check against winuse (#617, #611, #647, #569, #479).

## Needs a decision

- **Agent Cursor time in Batches and unattended runs.** Each Headless Action has a
  600 ms minimum glide (ADR 0018); agent-desktop's overlay measured about 355 ms
  per action. `agent-desktop.md` lists options short of changing the default.

## Not adopting

- Saved flows (agent-browser): experimental, many open fixes, large safety surface.
- MCP profiles or an MCP wrapper: ADR 0001 chose skill plus CLI.
- Durable or snapshot-qualified refs: deferred in ADR 0021.
- `read <url>`: harnesses already have a web fetch tool.
- WebMCP, sandboxes, cursor off by default.

## Unverified

Neither tool publishes benchmark results. Points marked "(inference)" in the
notes rest on reading, not on a run: what an agent sees when a Tab closes,
whether a failed `--root` read keeps the Agent's Window State, and whether a
check box hidden behind its label appears in winuse's tree.
