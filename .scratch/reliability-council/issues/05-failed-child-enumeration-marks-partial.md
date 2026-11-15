# 05: A failed child enumeration leaves the tree looking complete

Status: done (2026-11-07: landed on rc-05-children-partial; was ready-for-agent, user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:** in `Children`, a failed `get_Length` (or a negative length) returns no children and no error, so the node reads as a leaf. A failed `GetElement` skips that child silently (`wm:internal/uia/capture.go:179-190`). The walk does mark failures it is told about as partial (`wm:internal/uia/walk.go:293-301`), but these never reach it. A panel can look empty or a control can be missing with no partial note, and an `--observe=diff` reports the missing nodes as removed.

**agent-desktop:** "Enumeration Exhaustion": a truncated tree must never call itself complete; its walker tracks completeness apart from the nodes it returns (`ad:CONCEPTS.md`; `ad:crates/windows/src/tree/walk/walker_enumerate.rs`). **cua:** marks the branch incomplete (`cua-w:uia/mod.rs:639-660`, Reviewer 1).

**What to build:** return the children read so far plus an incomplete flag (or an error the walk turns into `reasonUnreadable`), so the read carries the partial note. Keep the children that did read.

**Blocked by:** none

- [x] `Children` reports a failed length and a skipped child
- [x] Offline tests: failed length, failed middle child, nil child, a real empty array (stays complete)

**Verified by the orchestrator:** `capture.go:179-190`. Impact is low (both reviewers), the fix small.

**Conflicts:** none; strengthens ADR 0004's partial note. Ticket 03 relies on the note being right.

## Comments

### 2026-11-07: landed

- `Children` (`internal/uia/capture.go`): a failed `get_Length` or a negative length now returns an error (no children) instead of a silent leaf. A failed or nil `GetElement` no longer just skips that child: the loop still reads the rest, then returns the children that did read, their NodeData, and an error naming the first child that failed. A nil array (no cached children) and a real empty array (length 0) stay a complete leaf with no error. Ticket 01's `defer` restore is unchanged.
- The walk (`internal/uia/walk.go`, `expand`): an error that is not `ErrChunkTimedOut` sets `reasonUnreadable` as before, and the walk now goes on to visit the children returned with it (the child cap, node cap, time limit and Document cut all apply to them as usual). A timed-out chunk drops any children returned with it and stops the walk, as before. The partial note therefore reaches `WalkResult.Notes` the same way every other unreadable chunk does: `reasonUnreadable` → `reasonNote` → "this Window State is partial: … not every Element's children could be read". The note is restored by a Document cut like the other reasons, and a tree with this note never gets the nearly-empty note. No new field or side channel. The `TreeSource` contract comment now says an error can come with the children that did read.
- `countHidden` (skeleton counts) is unchanged: on any error it drops the children and leaves no count.
- Tests: `internal/uia/children_test.go`, `TestChildrenReportsAFailedEnumeration`, runs the real `comSource.Children` over a fake node and a fake IUIAutomationElementArray (reusing `txtimeout_test.go`'s fake COM objects) with these cases: failed length, negative length, failed middle child (two of three kept, error), nil child (two of three kept, error), a real empty array (no error), and every child read (no error). `internal/uia/walk_test.go`, `TestWalkChildrenErrorKeepsTheChildrenThatRead`: a root chunk that fails with its children still returned. The panel and its button render and are expanded, and the only note is the unreadable-children partial note. `fakeSource` gained `errKeepsKids`.
- `go vet ./...`, `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go build ./...` pass. ADR 0004 and CONTEXT.md say nothing this changes.
- Live: not run (offline only).
