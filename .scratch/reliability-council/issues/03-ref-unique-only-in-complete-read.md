# 03: `name:` / `id:` takes the one match in a partial read as unique

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:** `ResolveElementRef` counts matches over whatever Elements it is given and never asks whether the read was complete (`wm:internal/wstate/refs.go:116-146`). The cache is tried first (`wm:internal/uia/liverefs.go:73-90`), and the stored Window State can be:

- a skeleton read (depth 3, `wm:internal/uia/region.go:19-21`; `walkConfig` sets it, `wm:internal/uia/thread.go:302-304`);
- or a read cut by time, the 3,000-node cap or the 1,000-children cap (`wm:internal/uia/walk.go:279-327`, notes in `WalkResult.Notes`, `walk.go:133-150`).

The live walk (`liverefs.go:116-155`) also accepts a single match without looking at its own partial notes (`liveMatches`, `liverefs.go:160-173`). So `click name:"Delete"` can act on the first "Delete" while a second one sits past the cut. CONTEXT.md says a name that matches several Elements is never a guess; this is one.

**agent-desktop:** a ref resolves only when the capture saw the whole window and exactly one element matched (`ad:CONCEPTS.md`, "Enumeration Exhaustion" and ref resolution).

**What to build:**

- Answer from the cache only when the stored read is complete: not a skeleton, no partial note, no truncation mark. Otherwise go live.
- When the live walk is partial too, refuse a single match: "1 match in the part read; the read was partial", with the hint to narrow (`--root`, `find`).
- "No match" in a partial read is reported as incomplete, not as absent.

**Blocked by:** none (ticket 05 adds a reason a read is partial)

- [ ] Completeness carried with the stored Window State and with the live walk's result
- [ ] Offline tests: unique in a complete read resolves; unique in a skeleton / capped / timed-out read goes live, then refuses if still partial; no match in a partial read says so
- [ ] SKILL.md: the new refusal under Element references

**Verified by the orchestrator:** `refs.go:116-146` and `liverefs.go:73-190` read no completeness; the walk records partial reasons only in `Notes`. Inferred: how often a duplicate name sits past a cut.

**Conflicts:** none; it enforces CONTEXT.md. A find-based fallback for big Windows may get slower (a refusal instead of a guess).
