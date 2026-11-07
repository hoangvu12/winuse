# 05: A failed child enumeration leaves the tree looking complete

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:** in `Children`, a failed `get_Length` (or a negative length) returns no children and no error, so the node reads as a leaf. A failed `GetElement` skips that child silently (`wm:internal/uia/capture.go:179-190`). The walk does mark failures it is told about as partial (`wm:internal/uia/walk.go:293-301`), but these never reach it. A panel can look empty or a control can be missing with no partial note, and an `--observe=diff` reports the missing nodes as removed.

**agent-desktop:** "Enumeration Exhaustion": a truncated tree must never call itself complete; its walker tracks completeness apart from the nodes it returns (`ad:CONCEPTS.md`; `ad:crates/windows/src/tree/walk/walker_enumerate.rs`). **cua:** marks the branch incomplete (`cua-w:uia/mod.rs:639-660`, Reviewer 1).

**What to build:** return the children read so far plus an incomplete flag (or an error the walk turns into `reasonUnreadable`), so the read carries the partial note. Keep the children that did read.

**Blocked by:** none

- [ ] `Children` reports a failed length and a skipped child
- [ ] Offline tests: failed length, failed middle child, nil child, a real empty array (stays complete)

**Verified by the orchestrator:** `capture.go:179-190`. Impact is low (both reviewers), the fix small.

**Conflicts:** none; strengthens ADR 0004's partial note. Ticket 03 relies on the note being right.
