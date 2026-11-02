# 17: Live re-resolution of `name:`/`id:`

**What to build:** when the cached Window State is stale or missing (helper restart, a newer read of another region, a re-rendered tree), resolve a `name:` or `id:` reference against a fresh bounded walk of the Window instead of failing.

Today `name:`/`id:` resolve against the cached state only (`internal/helper/refs.go:90-140`) and fail with the stale message (`internal/helper/actions.go:265`). agent-desktop re-identifies every ref from stored evidence (`ad:docs/snapshots-and-refs.mdx:94-199`); this gets most of that benefit without replacing Codex's Element Index.

- Element Indexes stay strict: valid only for the latest Window State.
- `name:`/`id:`: try the cache; on a miss or a stale Element, walk the live tree (bounded, under the request budget) for the same match rules. Ambiguity still lists candidates and refuses.
- The output notes when it re-resolved: `ok (found "Save" in a fresh read)`.
- The fresh read does not replace the printed Window State; indexes the agent holds stay as they were.

**Blocked by:** none (shares the live search with 04)

**Status:** ready-for-agent (codex-parity "stale elements" row is already "matched (extended)")

- [ ] Live fallback in the resolver
- [ ] Offline tests: cache hit, stale then found, ambiguous, absent
- [ ] Opt-in e2e: restart the helper, act by `name:`
- [ ] SKILL.md references section

## Comments

### 2026-11-03: triage decision (user)

Kept as written. Together with 04's added numbering it is winuse's answer to agent-desktop's re-identified refs.
