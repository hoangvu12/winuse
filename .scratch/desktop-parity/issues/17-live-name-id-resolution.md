# 17: Live re-resolution of `name:`/`id:`

**What to build:** when the cached Window State is stale or missing (helper restart, a newer read of another region, a re-rendered tree), resolve a `name:` or `id:` reference against a fresh bounded walk of the Window instead of failing.

Today `name:`/`id:` resolve against the cached state only (`internal/helper/refs.go:90-140`) and fail with the stale message (`internal/helper/actions.go:265`). agent-desktop re-identifies every ref from stored evidence (`ad:docs/snapshots-and-refs.mdx:94-199`); this gets most of that benefit without replacing Codex's Element Index.

- Element Indexes stay strict: valid only for the latest Window State.
- `name:`/`id:`: try the cache; on a miss or a stale Element, walk the live tree (bounded, under the request budget) for the same match rules. Ambiguity still lists candidates and refuses.
- The output notes when it re-resolved: `ok (found "Save" in a fresh read)`.
- The fresh read does not replace the printed Window State; indexes the agent holds stay as they were.

**Blocked by:** none (shares the live search with 04)

**Status:** done (the live e2e run is still to do: paused while the user games)

- [x] Live fallback in the resolver
- [x] Offline tests: cache hit, stale then found, ambiguous, absent
- [x] Opt-in e2e: restart the helper, act by `name:`
- [x] SKILL.md references section

## Comments

### 2026-11-03: triage decision (user)

Kept as written. Together with 04's added numbering it is winuse's answer to agent-desktop's re-identified refs.

### 2026-11-03: done (worker, branch dp17-live-refs)

- `uia.Client.ResolveElementRefLive` (`internal/uia/liverefs.go`) does cache first, as before (`resolveBaseFirst` from dp-04), and returns that index when its Element is alive (its RuntimeId reads; a Page Element counts as alive and is checked by the Action as before).
  - It walks live on: no Window State (helper restart), no match, or a stale Element. The walk is the same bounded walk as `find` (up to 20000 Elements). The budget is min(3 s, half the request's time left), so the Action keeps most of its budget.
  - The cache's own ambiguity refuses without walking, and so does an Agent that has no Window State because another Agent read the Window (ADR 0014 Decision 2 kept).
  - A fresh read's single match is added after the Window State (dp-04's `store.add`) and returned with `Fresh`; a fresh ambiguity adds every candidate and refuses, listing them under their new indexes (`LiveRefError`); a fresh miss refuses (`LiveRefError`). Element Indexes are never re-resolved.
- Helper (`liverefs.go`, `refs.go`): `resolveElementRef` uses the `LiveResolver` when `Services.Actions` has it.
  - A fresh resolution adds `Note: found name:Save in a fresh read of window 101, as Element Index 245; the Window State you have is unchanged` after the Action's ok. The note is collected per request by `reportRefs`, innermost in `reportAfter`, and appears once per Batch per reference. The ticket's form was `ok (found ... in a fresh read)`; I used a Note line like the other notes.
  - Failure wording names the fresh read; the cache's own failures keep theirs.
- De-duplication (the orchestrator's dp-04 follow-up): `store.add` now takes RuntimeIds (`runtimeID`, `IUIAutomationElement::GetRuntimeId` with a small SAFEARRAY reader) and returns each Element's index. An Element already added keeps its index and the new handle is released. `uia.Added` carries `Indexes` instead of `First`; find's note now names "3 to 5" or a list. The full read's own Elements are not keyed, since that would cost a RuntimeId round trip each.
- Docs: SKILL.md "Name an Element", ADR 0004 addendum, codex-parity stale-elements row (status kept).
- Tests: `internal/uia/liverefs_test.go` (cache hit, stale then found, not in the cache, no Window State, cache ambiguity, another Agent's read, fresh match, fresh ambiguity with added indexes, fresh miss, add-once by RuntimeId, `runtimeKey`), `internal/helper/liverefs_test.go` (note, cache-hit silence, once per Batch, refusals' wording), and dp-04's tests updated. Opt-in e2e `TestNameReferenceAfterHelperRestart` (`test/e2e/live_refs_test.go`): kill the helper, then click by `name:Click Me` and set-value by `id:text-field` on the covered fixture, each with the note; the same name again needs no fresh read; `name:Twin` refuses after a restart. Compiled, NOT run.

