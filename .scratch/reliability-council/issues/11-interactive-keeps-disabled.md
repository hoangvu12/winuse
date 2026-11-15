# 11: `--interactive` hides disabled controls

Status: done

**What's wrong:** `Element.Interactive()` returns false for anything disabled (`wm:internal/wstate/find.go:112-119`), so `get-window-state --interactive` leaves out a disabled Submit button. The agent then looks for a control that is right there, or concludes the form has no Submit button.

**What to build:** keep disabled Elements in the interactive view with their `disabled` state, as the full tree prints them (SKILL.md line 122 lists `disabled` among the states).

**Blocked by:** none (pairs well with 06: the agent sees the button is disabled, and the Action waits for it)

- [x] `Interactive()` ignores `Disabled`, or the view applies it separately
- [x] views test updated
- [x] SKILL.md: what `--interactive` keeps

**Verified by the orchestrator:** `find.go:116-119`. One reviewer (1); the orchestrator agrees. Check `find`'s own filters still behave as intended.

**Conflicts:** none known.

## Comments

### 2026-11-07: landed

The view applies the disabled rule separately; `Interactive()` keeps its meaning, so `find --state interactive` still matches enabled Elements only.

- Rule (`internal/wstate/find.go`): the role/pattern test moves to a new `Element.ActedOn()` (a role it is acted on through, a settable value, a toggle, a selection or expansion state, a Secondary Action besides Raise), enabled or not. `Interactive()` is now `!e.Disabled && e.ActedOn()`, unchanged in behaviour.
- Callers, each checked: the `--interactive` views take `ActedOn()`: the full read and `--observe`'s diff and full read (`View.prints`, `internal/wstate/view.go`; `RenderChanges` goes through it), `--root` on a stored Page subtree (`storedSubtree`, `internal/helper/find.go`) and the live region read (`Client.ReadRegion`, `internal/uia/region.go`). Batch's `get-window-state --interactive` step only forwards the flag to these. `find --state interactive` (`hasState`) and `--out`'s "N interactive" count (`writeStateOut`, `internal/helper/views.go`) keep `Interactive()`: what the agent can act on now. The Task Suite does not use the rule.
- Tests: `TestRenderViewInteractive` (`internal/wstate/view_test.go`) marks Open disabled and expects `3 button (disabled) Open`; `TestInteractive` (`internal/wstate/find_test.go`) adds the disabled button to `ActedOn` while `--state interactive` still leaves it out; `TestFullReadViews` (`internal/helper/views_test.go`) now prints `2 button (disabled) Cancel` and no left-out note.
- Docs: SKILL.md "Read the Window State" says what `--interactive` keeps and how `find --state interactive` differs; `references/reading.md` likewise.
- `go vet ./...`, `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go build ./...` pass.
- Live: not run (offline only).
- Open: `get-window-state --help` still says `--interactive` "prints only the Elements you act on" (true, but silent on disabled ones); `--out`'s "N interactive" count counts enabled Elements only, so it can be lower than what `--interactive` prints.
