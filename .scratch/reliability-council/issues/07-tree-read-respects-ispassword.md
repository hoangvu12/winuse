# 07: The tree read never checks IsPassword

Status: done

**What's wrong:** IsPassword (30019) is not among the cached tree properties (`wm:internal/uia/com.go:149-173`), and the walk copies Name, HelpText and ValueValue into every line with no secret check (`wm:internal/uia/capture.go:201-224`). The Action paths do check it (`wm:internal/uia/focusvalue.go:48`, `wm:internal/uia/textsplice.go:105`), and so do Page reads. A custom control that exposes its secret through Value puts it into the Window State, the diff, the trace, and the model's context.

**agent-desktop:** caches IsPassword and withholds value-bearing fields; an unreadable answer withholds too (`ad:crates/windows/src/tree/properties/element_properties.rs:38-43,75-118`, Reviewer 2). Its probe found built-in password EDITs don't leak (`ad:probes/windows/FINDINGS.md:172`, A14-6), and winuse's own probe saw WinUI's PasswordBox give "●●●●●●" (`wm:.scratch/headless-gaps/probes-round5.md:30`). The risk is in custom and non-conforming providers.

**What to build:** add 30019 to `treeProperties`; when it reads true, or fails to read on an Edit, leave the Value out of the line and the stored Element (keep role, name and index). Print a `password` state so the agent knows why.

**Blocked by:** none

- [x] IsPassword cached; Value withheld before render, diff and store
- [x] Offline tests: true, false, unreadable
- [x] SKILL.md / CONTEXT.md: the `password` state on a line

**Verified by the orchestrator:** the property list and `data()`. Inferred: how often a real provider leaks.

**Conflicts:** none. The Codex tree grammar keeps its shape (`docs/codex-parity.md`); one more state word.

## Comments

### 2026-11-07: landed

- Property list (`internal/uia/com.go`, `treeProperties`): 30019 IsPassword is cached with the rest of the tree read, so the check costs no extra round trip.
- Per-element copy (`internal/uia/capture.go`, `comSource.data`): `NodeData` gains `IsPassword` and `PasswordUnread`, read through `propBoolRead` (`internal/uia/value.go`), which tells a VT_BOOL answer from none.
- Withholding (`internal/uia/walk.go`, `elementFrom` and `NodeData.secret`): when IsPassword reads true, or does not read on an Edit, the Element gets `Password` and no `Value`; role, name, ID, settable and index stay. The Value is dropped before the Element exists, so the render, the diff, the trace and the stored Window State never hold it. An unreadable answer on any other role keeps the Value (a browser's Document URL, a combo box's text). The Document cut still sees a Document's URL, as before; it is not stored.
- Rendering (`internal/wstate`): `Element.Password` (JSON `password`) prints `password` last in the parenthesised states, e.g. `edit (settable, string, password) Password`. One more state word; the Codex grammar keeps its shape.
- Tests: `TestWalkWithholdsPasswordValues` (`internal/uia/value_test.go`): true, false, unreadable on an Edit, unreadable on a combo box, through the walk, `wstate.Render` and `wstate.Diff` (a changed secret is no change); `TestRenderPasswordState` (`internal/wstate/render_test.go`).
- Docs: SKILL.md and `references/reading.md` name the `password` state; CONTEXT.md (Window State) says a secret field's Value never enters it; codex-parity.md's tree format names the word as a divergence under `--full`.
- `go vet ./...`, `CGO_ENABLED=0 go build ./...` and `CGO_ENABLED=0 go test ./...` green.
- Live: not run (offline only). How often a real provider leaks is still inferred.
- Open: `find --state password` is not a filter (`wstate.States`); the value wait (`readElementState`, `internal/uia/wait.go`) has its own `Secret` check, which treats an unreadable IsPassword as not secret, unlike the tree read now.
