# 03: `name:` / `id:` takes the one match in a partial read as unique

Status: done

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

- [x] Completeness carried with the stored Window State and with the live walk's result
- [x] Offline tests: unique in a complete read resolves; unique in a skeleton / capped / timed-out read goes live, then refuses if still partial; no match in a partial read says so
- [x] SKILL.md: the new refusal under Element references

**Verified by the orchestrator:** `refs.go:116-146` and `liverefs.go:73-190` read no completeness; the walk records partial reasons only in `Notes`. Inferred: how often a duplicate name sits past a cut.

**Conflicts:** none; it enforces CONTEXT.md. A find-based fallback for big Windows may get slower (a refusal instead of a guess).

## Comments

### 2026-11-07: landed

Completeness is a typed flag, not a match on note text: the walk sets it, the stored Window State carries it, and reference resolution reads it.

- The walk (`internal/uia/walk.go`): `WalkResult.Partial` is set once, at the end of `Walk`: a partial-tree reason (time limit, a chunk that timed out, node cap, depth cap, unreadable children, all through `reasonNote`), a truncation marker (`w.truncated`; the children cap sets only this, never a note), or a skeleton node whose children were counted above 0 or could not be counted (new `w.hidden`; a skeleton whose depth-cap nodes all count 0 saw the whole Window and stays whole). A Document cut is not a loss: `cutAt` restores `hidden` with the reasons and the truncation flag, so what was read under the cut Document leaves no partial mark either. The retry policy's note-based `treeIsComplete` (`retry.go`) is untouched (ticket 04's area).
- Stored Window State (`internal/uia/store.go`, `capture.go`, `thread.go`): `captureResult.partial` goes through `installRead` into `store.install(..., partial)` and `windowState.partial`. A Page splice (`internal/uia/splice.go`, `SplicePage(..., pagePartial)`) keeps the frame read's flag and ORs in the Page read's: `page.Result.Partial()` (`internal/page/build.go`: a cut reason, the document's truncation, or any Page Element's children-cap marker) becomes `page.Merged.Partial` (`merge.go`), which `readWindowTree` passes (`internal/helper/pagestate.go`). A Tab target's own Window State carries it too (`tabState.partial`, `internal/helper/tabtarget.go`).
- The refusals (`internal/wstate/refs.go`): new `PartialRefError` (`name "Delete": 1 match in the part read; the read was partial`) with the match's index and tree line; `NoMatchRefError.Partial` (`no element named "X" in the part read; the read was partial`); `InPartialRead` maps a resolution in a partial Window State (one match → `PartialRefError`, none → marked miss, several → the existing `AmbiguousRefError`).
- Resolution (`internal/uia/liverefs.go`, `refs.go`): `store.refState` now returns the `cacheView` (Elements, base, partial, doubted). `resolveCacheFirst` answers with one match only when `cv.answers(i)` (the read was whole and the match is not doubted) and the Element is alive. Otherwise it goes live. A partial cache's ambiguity still refuses as before. In `resolveLive`, a partial fresh read (`res.Partial`) goes through `partialRead`: its one match is still added (so the refusal can name its index) and is refused, and its miss is marked incomplete. The added match is marked doubted (`store.doubt`), so asking again goes live again instead of taking it from the cache. Without that, the second try would have acted on it. A new full read or a Page splice clears the marks. Cache-only `ResolveElementRef` (pageResolve's Page check and fakes) gives the same refusals for a partial stored read or a doubted match.
- Messages and codes (`internal/helper/refs.go`, `partialRefError`): `name "Delete": 1 match in the part read; the read was partial (a fresh read of window 101)` (or `(the cached Window State of window 101)`), hint `narrow the read: winuse find 101 --role <role> --name <text> searches the live Window, or winuse get-window-state 101 --root <element> reads one part of it; then act on an Element Index it prints; if this is the Element you mean, act on it by its Element Index:` plus the match's line. Code `AMBIGUOUS_TARGET`. A miss: `no element named "X" in the part read; the read was partial (a fresh read of window 101), so it may be in the part not read`, same hint without the line, code `NOT_FOUND`. The codes are set explicitly, because "cached Window State" would otherwise classify as `STALE_REF`. Element Indexes (plain numbers) are never resolved, so they are unaffected.
- Callers, each checked: every Element-taking Action (`elementTarget.resolve`: click, set-value, secondary actions, scroll, type-into, menu items, context menu, Posted Clicks, pointer, Page elements) and `wait` (`waits.go`) go through `resolveElementRef` → `ResolveElementRefLive` and get the new rule and refusals. Batch steps run the same commands. `get-window-state --root name:…` / `find --root` (`regionRoot`, `internal/helper/find.go`) gets it too; the hint's "act by the index" means `--root <index>`. The Page check (`pageResolve`, cache only) treats the refusal like any unresolved reference: the command's own route then resolves live, so a Page Element in a partial browser read is no longer taken from the cache. A Tab target (`tabResolve`) has no fresh read: a partial Tab read refuses its one match with the index, and its miss as incomplete (`tabTargetRefError`, hints without `find`/`--root`, which a Tab target does not take). `find` itself resolves no reference and is unchanged.
- Tests: `internal/uia/complete_test.go`: `TestWalkPartialForEveryReason` (whole, time limit, chunk timed out, node cap, children cap, depth cap, unreadable children, skeleton with hidden children, skeleton whose count failed, skeleton that saw every leaf, Document cut), `TestWalkLateCutRestoresCompleteness`, `TestResolveCacheFirstNeedsAWholeRead` (one match in a partial cache goes live, several refuse from the cache, a whole cache answers), `TestPartialFreshRead` (one match refused with its new index, miss incomplete, several unchanged), `TestStoredReadCompleteness` (cache-only, whole vs partial install), `TestSplicePageCarriesCompleteness`, `TestDoubtedMatchNeverAnswersFromTheCache`. `internal/wstate/refs_test.go` `TestInPartialRead`, `internal/page/build_test.go` `TestResultPartial`, `internal/helper/liverefs_test.go` `TestPartialReadRefusals` (exact messages, codes, hints, nothing clicked; cache-only and Tab target). Existing test call sites updated for the new `install` / `SplicePage` / `refState` signatures.
- Docs: SKILL.md "Name an Element" names the refusal and the narrowing; `references/reading.md` gives the full wording; CONTEXT.md's Element Reference now says one match in a read that did not see the whole Window is not taken either.
- `go vet ./...`, `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go build ./...` pass.
- Live: not run (offline only). How often a big Window now refuses a reference it used to guess at (the "Conflicts" cost) is not measured.
- Open: an Element `find` or a region read added still answers a `name:`/`id:` from the cache when the stored read is whole and has no match of its own (desktop-parity 04's "reach an added one only when the read has none"). A `find --first` among several matches is then a guess. Not changed here. A doubted match stays doubted until the next full read, even after a whole fresh read finds it, so it walks live each time until then.
