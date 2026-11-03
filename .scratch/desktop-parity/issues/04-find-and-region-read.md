# 04: `find` and a region read

**What to build:**

- `find <window> [--role <r>] [--name <text>] [--text <text>] [--state <s>] [--first | --count]`: prints the matching lines in the ADR 0004 grammar, with Element Indexes the agent can act on. On a role miss, list the roles present (agent-desktop's `roles_present`).
- `get-window-state <window> --root <element> [--depth N] [--interactive]`: reads one subtree.

agent-desktop: `find` with filters and shaping; `--skeleton` (depth 3 with child counts) and `--root REF` drill-down (`ad:docs/skeleton-and-drill-down.mdx:7-153`). winuse reads the whole tree every time, bounded by caps and a truncation note (`internal/uia/walk.go:101-103`).

Numbering decision (user, 2026-11-03, option c): `find` and `--root` **add** their Elements to the Window's live store with new Element Indexes after the existing ones (a full read ends at 240, `find` prints 241, 242, ...). The full read's indexes keep working. A full `get-window-state` still renumbers from 1 and replaces the store. Staleness checks are unchanged: an index whose Element changed still gets the stale message. No snapshot-qualified refs. **Revisit** if agents are seen failing on stale indexes: then consider agent-desktop's snapshot-qualified refs with re-identification (see Comments).

- `find` searches the live tree with a bounded walk, not only the last printed state, so it reaches Elements past the truncation caps.
- `--skeleton` (depth 3, `(+N children)` markers) is part of this ticket.
- Pages: route `find` to the CDP snapshot with the same filters.

**Blocked by:** none

**Status:** done (the live e2e run is still to do: paused while the user games)

- [x] Filters and output in CLI, protocol, helper
- [x] Region read and skeleton in the walk
- [x] `find` and `--root` add to the live store after the last index; a full read replaces it; stale messages unchanged
- [x] Offline tests over recorded trees (`internal/wstate/testdata`)
- [x] ADR 0004 amendment; SKILL.md "dense Windows"

## Comments

### 2026-11-03: triage decision (user)

Search options like agent-desktop's `find` (role, name, text, state; `--first`, `--count`; `roles_present` on a role miss; `--skeleton`, `--root`). Numbering: winuse's Element Index stays, with option (c): `find` and region reads add indexes after the existing ones instead of replacing the store. Options considered: (a) fresh numbering that replaces the store; (b) agent-desktop's saved, snapshot-qualified refs with re-identification. (b) was not chosen: it replaces the Codex `element_index` model and a re-identification can pick the wrong Element. **The user asked to revisit this if agents are seen failing because their indexes went stale.**

### 2026-11-03: done (worker, branch dp04-find)

- `winuse find <window> [--role] [--name] [--text] [--state] [--first | --count] [--timeout]` (CLI `find.go`, protocol `FindArgs`, helper `find.go`; an observation command, so it is not serialized, and `--timeout` goes up to 60 s like get-window-state).
  - The filters live in `wstate.Query`: role as printed (case and spacing ignored); name a case-insensitive part of the name; text a part of name, value or description; state one of disabled, enabled, selected, selectable, expanded, collapsed, settable, on, off, indeterminate, interactive.
  - `uia.Client.Find` walks the live tree with the usual chunked walk, up to 20000 Elements (`FindMaxNodes`) within the read budget, so it reaches Elements past a printed state's 3000-Element cap. It keeps the matches and adds them to the Agent's store.
  - Output: the header, one `\t<index> <line>` per match, the walk's partial note if any, and a note naming the added range.
  - Misses: `--count` prints only the number. A miss fails with "nothing was added": a role miss lists `the Window shows these roles: ...`, any other miss says to loosen the filters.
- `get-window-state <window> --root <element> [--depth N] [--interactive]`: `uia.Client.ReadRegion` walks from the stored Element (a new `comSource.from` root built with BuildUpdatedCache). It is indented from the root and added to the store the same way; `--interactive` keeps the root plus `wstate.Element.Interactive()` Elements. `--root` takes an index or a name:/id: reference.
- `get-window-state <window> --skeleton`: a full read with `MaxDepth = 3` and `WalkConfig.CountHidden`. Each Element where it stops gets `(+N children)` (new `wstate.Element.MoreChildren`); no depth-cap note, plus a note pointing at `--root`. It replaces the store like any full read.
- Refused combinations: `--skeleton` with `--root`, `--depth`/`--interactive` without `--root`, and `--screenshot` with either.
- Numbering (option c) in `uia.store.add`: the Elements go after the Agent's stored ones (a fresh list, so concurrent readers are not disturbed); an Agent with no state gets one numbered from 0. `windowState.base` records the read's own count (install and splice set it). `ResolveElementRef` resolves against the read's own Elements first and reaches the added ones only on no match, so `name:Save` still names the read's Save after a `find` that repeated it. A full read releases the added Elements; stale messages are unchanged.
- Pages: a browser Window with a Browser Connection is filtered from a fresh full read (`readWindowTree`, the Page over CDP), with a note that the earlier indexes were replaced. `--root` on a Page Element, or on the document the Page was spliced under, prints the stored subtree with its existing indexes, because the Page was read whole and a UIA re-read would miss it.
- Docs: ADR 0004 amendment (grammar gains `(+N children)`), SKILL.md "Dense Windows" plus the command list, codex-parity stale-elements row (status kept) and extensions row, CLI usage.
- Tests: `internal/wstate/find_test.go` (filters, states, interactive, roles present, skeleton marker, over `testdata/fixture-app.json`), `internal/uia/region_test.go` (skeleton count, pickMatches, store add and base-first refs, add without a state, ambiguous added refs offset, partialOnly), `internal/helper/find_test.go` (find output, count, first, misses, refused args, region read, skeleton note, refused shapes, find and --root on a browser Window over the page rig), `internal/cli/find_test.go`. Opt-in e2e `TestFindAndRegionReadOnTheFixture` (`test/e2e/find_test.go`), compiled, NOT run.
- Concurrency note: find and get-window-state are both unserialized reads. A find that adds Elements between a browser read's UIA walk and its Page splice makes that splice see a newer list, so that read falls back to UI Automation with its note. That is rare, and safe.

