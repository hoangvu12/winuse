# 12: Chromium wrappers spend the depth and the character cap (measure first)

Status: done (fold before the cut landed 2026-11-07, user's go; logical depth skipped on the measurement)

**What's wrong (likely):** on a Chromium or Electron Window without a Browser Connection, depth counts every level, anonymous wrappers included: the walk (`wm:internal/uia/walk.go:279-286,327`), the skeleton's 3 levels (`wm:internal/uia/region.go:19-21`) and `--depth`. In the perf benchmark Discord's skeleton was 0.3 KB in winuse against 1.8 KB in agent-desktop, so the skeleton is probably all shell, and the full read hit the 28,000-character default cap (`.scratch/perf/r3.log`, `wm:internal/helper/views.go:30`). `--compact` exists but is opt-in.

**agent-desktop:** under Chromium, an unnamed Group, Custom or Pane with no value, no AutomationId and no action costs no logical depth (`ad:crates/windows/src/tree/walk/wrapper.rs:32-52`; "Web Wrapper", `ad:CONCEPTS.md`). It counted 54 of 81 such nodes on settled Chromium; skipping them recovers about a third of the depth (`ad:probes/windows/FINDINGS.md:231`, A16-8).

**What to build:**

1. Measure first, offline from stored reads or on the user's OK: how deep Discord's or VS Code's content is in raw versus logical levels, and how much of the cap wrappers use.
2. If it pays: count logical depth under a `Chrome_` root class for the skeleton and `--depth`, and fold bare containers before `--max-chars` cuts lines, as `--compact` does.
3. Keep the raw safety caps (nodes, time, depth 48).

**Blocked by:** none

- [ ] Measurement (two Electron apps, raw vs logical depth, chars spent on wrappers)
- [ ] If worth it: logical depth for skeleton and `--depth`; fold-before-cut

**Verified by the orchestrator:** the depth counting and agent-desktop's predicate. Inferred: the gain. Reviewer 2 rates it a limited win (it saves logical depth, not traversal), and winuse noted the research already (`wm:.scratch/browser-merge/agent-desktop-research.md:92`).

**Conflicts:** the skeleton definition (desktop-parity 04) and ADR 0004's default-budgets addendum.

## Comments

### 2026-11-07: not started (orchestrator)

- Nothing built: step 1 is a measurement, and no stored read has the trees it needs (`.scratch/perf/` keeps sizes only: `r2-baseline.json`, `r3-fixed.json`, `r3.log`). It needs live reads on the user's desktop, which agents don't start on their own.
- This PC has Discord but no VS Code (checked 2026-11-07), so only one of the ticket's two Electron apps can be measured here.
- Asked the user: run the Discord measurement (read-only), and when, or skip the ticket.

### 2026-11-07: measured on Discord (orchestrator, user's go-ahead)

Read-only reads of the user's open Discord window (maximized on the second monitor, on top, no Browser Connection), with winuse built from main fec05fb: `--skeleton`, the default read, `--max-chars 0`, `--full`, and `--compact` with and without the cap. No input was sent to Discord. Only counts are kept here; the reads hold the user's chat and are not committed. Wrapper = agent-desktop's predicate: an unnamed group, pane or custom with no value, no ID and no Secondary Action. Logical depth = raw depth minus wrapper ancestors.

- A cold first read came back thin. The default read settled for about 5.8 s, then printed the frame only (7 nested unnamed panes) with the note "Chromium may not build it while the Window is fully covered", but the Window was not covered. A read a few minutes later had the whole tree. The settle gave up too early for a cold Discord, and the note's "covered" guess was wrong. Not part of this ticket; listed for the user.
- Size: 784 Elements; 259 are wrappers (33%: 253 groups, 6 panes). agent-desktop counted 54 of 81 on settled Chromium; Discord has proportionally fewer.
- Depth: raw max 18, logical max 11. The Document sits at raw depth 7, logical 2. The non-wrapper Elements' median depth is raw 14, logical 7. The full read's depth cap (48) cut nothing.
- Skeleton (3 levels): raw, 5 Elements, 2 of them not wrappers, all frame. Counted logically it would show 5 non-wrappers, still only up to the app's root group (`app-mount`); logical depth 5 would show 60, depth 8 would show 352. At the skeleton's 3 levels, logical depth barely helps on Discord.
- Characters: the default read is 40,309 characters uncut. At the 28,000 default cap it prints 534 of 784 lines, 306 of them non-wrappers, and leaves 250 out. Wrapper lines are 17% of the characters, and tab indentation 30% (12,077), most of it under wrappers.
- `--compact` (folds bare containers and pulls their children's indent in): 29,732 characters uncut (-26%), 500 lines, max indent 11 instead of 19. At the cap it prints 458 lines and leaves 42 out, reaching Element 737 instead of 533.

Decision (recommended, waits on the user's go):
- Fold before the cut: when a default read would pass `--max-chars`, fold bare containers as `--compact` does before cutting lines, saying so in the note. On Discord the capped read would show about 1.5x as many useful Elements (458 lines against 306 non-wrappers), and small Windows that fit the cap print as today. Folded Elements' indexes keep working, as with `--compact`. `--full` stays Codex's text byte for byte.
- Skip logical depth for the skeleton and `--depth`: at 3 levels it moves the skeleton from the frame to the app's root group, which still shows nothing to act on; a useful skeleton needs about 5 logical levels, a separate change to the skeleton's definition (desktop-parity 04).
- One app, one channel view, one moment: VS Code is not installed, so the ticket's second Electron app was not measured.

### 2026-11-07: landed (fold before the cut)

- `internal/wstate/view.go`: `RenderView` renders once; when `--max-chars` would cut the Element lines (`cutLines` now reports it) and the view is not already `--compact`, it renders again with the bare containers folded, as `--compact` does, and cuts only what still does not fit. The left-out note says why: `Note: folding unnamed containers to fit --max-chars 28000 (as --compact does) left out N of the M Elements; their Element Indexes still work`. A read that fits prints exactly as before; `--out`, `--max-chars 0` and `--full` never fold. `find` and `--root` keep their own cut (unchanged).
- Test: `TestRenderViewFoldsBeforeTheCut` (folds and fits; folds, then cuts when still too long; a read that fits and an uncapped read print as before). `TestRenderViewMaxChars` unchanged and passing.
- Docs: SKILL.md (the 28000-character sentence), `references/reading.md`, ADR 0004's cap bullet (with the Discord numbers).
- Live check (read-only, the user's Discord, winuse built from this change): the default read now folds 290 of 831 Elements and prints through Element 706 before the cap, where the unfolded read stopped at Element 533 (of 784, earlier). No input sent.
- Not done: logical depth for the skeleton and `--depth` (the measurement showed it barely helps at the skeleton's 3 levels).
