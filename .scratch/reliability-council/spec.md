Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

# Reliability council: what winuse can learn from agent-desktop and cua

## The question

What can winuse improve, in reliability, correctness, speed, token cost, agent experience and testing, by learning from vercel-labs/agent-desktop and trycua/cua? (`handoff.md`)

## How it ran (2026-11-07)

- Three reviewers, each reading everything (all three codebases, the whole question), no split. Read-only: nobody ran winuse, agent-desktop or cua.
- Reviewer 1 and Reviewer 2 reported. Reviewer 3 failed twice on its provider (HTTP 502) before reporting; the user had said two is enough if it fails.
- **Version judged: GitHub `main` at 03fc3a4 (release 0.3.0)**, not this PC's `main`. GitHub `main` is 57 commits ahead of this PC's `main` (`.scratch/speed`, `--observe=diff` (ADR 0021), `multi-agent`, `headless-max` ...), and the handoff did not know about them. The perf change on `perf-catch-up` (437466b) was given to the reviewers as a pending change. It conflicts with GitHub `main` in `internal/helper/opened.go`, `state.go` and `skills/winuse/SKILL.md` (GitHub's `speed/01` reworked the same opened settle), which is outside this council.
- Sources: agent-desktop b43ac4f, cua 59c2fa6. In the tickets `wm:` is winuse at 03fc3a4, `ad:` agent-desktop, `cua-w:` `cua/libs/cua-driver/rust/crates/platform-windows/src/`.
- Every finding below was checked against the code by the orchestrator. Each ticket says what was verified and what is inferred.

| Reviewer | Score |
|---|---|
| Reviewer 1 | 7.5/10 |
| Reviewer 2 | 8/10 |
| Reviewer 3 | no score (failed) |

Both put winuse ahead of both tools on speed, output size, headless reach and browser pages. What is left to learn is failure classification (what an error means, whether a retry is safe), proof that a read was complete, and automated live testing.

## Tickets, ranked by impact

| # | Title | Area | Size | Reviewers |
|---|---|---|---|---|
| 01 | The UIA transaction timeout carries over from the last tree chunk | reliability | S | 1 |
| 02 | Write failures say "nothing was sent" on a transport error; delivery is read from English text | correctness | S-M | 1, 2 |
| 03 | `name:` / `id:` takes the one match in a partial read as unique | correctness | S-M | 1 |
| 04 | A failed root read is always "the Window may have closed", with no retry | reliability | S | 1, 2 |
| 05 | A failed child enumeration leaves the tree looking complete | correctness | S | 1, 2 |
| 06 | Wait briefly on a transient refusal instead of failing at once | agent experience | S-M | 1, 2 |
| 07 | The tree read never checks IsPassword | correctness, privacy | S | 1, 2 |
| 08 | No CI runs tests; live claims are tracked in free text | testing | S-M | 1, 2 |
| 09 | Window identity: re-check at the write; the shared-record gap | correctness | M | 1, 2 |
| 10 | A plain get-window-state cannot print a diff | tokens | S | 1 |
| 11 | `--interactive` hides disabled controls | agent experience | S | 1 |
| 12 | Chromium wrappers spend the depth and the character cap (measure first) | tokens | S-M | 1 (2: limited) |
| 13 | LibreOffice and other SAL windows: probe before building an MSAA walker | reliability | S then L | 1, 2 |

## The handoff's leads, settled

- Auto-wait for actionability: confirmed, narrowed to refusals that sent nothing, outside the lane, 1.5-2 s (ticket 06).
- Diff reads: `--observe=diff` covers Actions (`wm:internal/wstate/diff.go`, ADR 0021). Only a plain read lacks it (ticket 10). cua's "reindexed" diff stays rejected (ADR 0021, Considered).
- `--interactive` on a whole-Window read: **already supported** (`wm:internal/helper/views.go:51-57`). Lead refuted.
- Electron wrappers: ticket 12, measure first.
- E_FAIL retry: confirmed (ticket 04). Copy the bounded retry, not cua's retry of every HRESULT.
- MSAA walker: ticket 13, probe first.
- Empty CoreWindow / desktop-root walk: mostly does not apply: winuse already knows the ApplicationFrameWindow / CoreWindow pair (`wm:internal/win/enum.go:76-81`; Reviewer 1 says the read targets the frame, not checked further). cua's own fallback marks itself incomplete because a pid search finds other Windows' controls (`cua-w:uia/mod.rs:445-449`). Folded into 13.
- Cache-skip for client-side providers: **not ticketed.** Reviewer 1: agent-desktop's own measurement, with its pid check, batches even Notepad, and batching was faster (`ad:probes/windows/FINDINGS.md:445`, A24-17). Reviewer 2: an experiment at most.
- Process-start stalls: neither repo solves it; not diagnosed. Listed under ideas.

## Not to copy (both reviewers)

- agent-desktop's whole-window uniqueness proof before every ref action, and a machine-wide lease for every headless action: they would undo winuse's live Elements and lanes (ADR 0014).
- agent-desktop's 5 s default actionability wait: too long while a lane is held. 1.5-2 s, outside the lane (ticket 06).
- cua's whole-subtree cache read, its retry of every cache-build HRESULT, its pid-only desktop fallback as a complete Window, a physical MSAA dropdown click without `--headed`, screenshot on every read, telemetry, the UIAccess worker.
- cua's EnableWindow guard: its "91% to 0 of 507" is confounded. cua never turns AutoSetFocus off (no match in `cua-w/`), and UIA's own SetFocus is a likely cause of the focus steal; winuse turns it off (`wm:internal/uia/thread.go:83-88`). (Reviewer 1; the user already declined the guard.)
- Removing the Agent Cursor's glide (user's decision, perf 02).
- Benchmark numbers carried across machines or provider strategies.

## Ideas beyond the question (not acted on)

- Trace a process-start stall (Defender's first-sight scan?); Authenticode signing might help. A persistent CLI-compatible command stream would hide it.
- Check whether RuntimeId (30000) can ride the cache request; if so ADR 0004's "a round trip each" no longer holds and durable indexes get cheap.
- Persist the Window identity records so they survive the helper's 5-minute idle exit.
- Test whether a fresh UIA client sees a cold Electron tree that the long-lived worker keeps serving as a shell (`ad:probes/windows/FINDINGS.md:234`, A16-11).
- Mark offscreen and virtualised rows in the tree.
- Resolve a slow live `name:` walk before taking the Window lane (it can hold the lane up to 8 s).
- An occlusion check within the Window for Headed clicks (ElementFromPoint).
- Request IDs and a completion journal for a CLI that disconnects after an uncertain delivery.
- Completeness and provenance fields in JSON output.
- Privacy canaries: assert a planted secret never reaches snapshots, errors, diffs or traces.
