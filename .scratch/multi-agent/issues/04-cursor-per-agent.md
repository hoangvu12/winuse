# 04: A cursor per agent, like cua

**What to build:** one Agent Cursor per agent, placed and timed like cua's (`libs/cua-driver/rust/crates/cua-driver-core/src/agent_cursor.rs`, `cursor-overlay`, MIT).

- One small layered window per agent (WS_EX_LAYERED, TRANSPARENT, TOOLWINDOW, NOACTIVATE), with its own colour and a label: the `WINUSE_AGENT` name, or a short default.
- **Z-order like cua:** not topmost. Insert it just above the target Window (HWND_NOTOPMOST, then after `GetWindow(target, GW_HWNDPREV)`), so the user's Windows above the target also cover the cursor. Re-place it on each Action. One shared window gives the wrong z-order with several targets (cua #1800).
- **Lifecycle like cua:** it fades out after an idle timeout and comes back on the agent's next Action; it hides at once when the agent is gone (helper idle eviction of that agent).
- Excluded from winuse's own captures, as today (ADR 0006).
- **Absorbs desktop-parity 21 (cursor polish):** the insertion caret after a Headless type-text or set-value, and the label and colour options, now per agent: `WINUSE_AGENT` for the label, `WINUSE_CURSOR_COLOR` for the colour. Colours are assigned automatically otherwise.
- The Headed banner stays one banner (one desktop lane, ticket 06), naming the agent.

**Blocked by:** 02

**Status:** done (the live e2e run is still to do)

- [x] Cursor manager keyed by agent; per-agent window, colour and label
- [x] Z-order placement above the target; idle fade; hide on eviction
- [ ] desktop-parity 21's caret and options: the options are done (WINUSE_AGENT as the label, WINUSE_CURSOR_COLOR); the caret moved to `11-insertion-caret.md`
- [x] Offline tests: colour assignment, option parsing, placement decision against a faked z-order
- [ ] Opt-in e2e: two agents' cursors visible on two covered fixtures; the user's cover stays above them (visual check by the user). Written and compiled; not run live yet.

## Comments

### 2026-11-03: built (branch ma04-cursor-per-agent)

- `internal/win`:
  - `AgentCursor` gains `TravelAbove` and `ClickAbove`, which take the Window to sit above. `Travel` and `Click` are those with no anchor.
  - `NewAgentCursorIn(do, accent)` draws a cursor in its own colour. `NewAgentCursor` keeps the default accent (#4299FF).
  - The cursor windows lose WS_EX_TOPMOST. `cursor_zorder.go` places them just above the anchor: the first window above it that is not a cursor window (so Agents' cursors never anchor each other) or, when that is topmost or missing, HWND_TOP of the non-topmost band. The decision is the pure `cursorInsertAfter`. Each window leaves the topmost band (HWND_NOTOPMOST) before it is inserted.
  - `cursor_colors.go` has the palette (seven colours, none the default accent) and `ParseCursorColor` ("#RRGGBB" or "RRGGBB").
- Colour option: `WINUSE_CURSOR_COLOR` travels as `protocol.Request.AgentColor` (CLI `agent.Resolve`) into `agent.Identity.Color`.
- `internal/helper/cursors.go`:
  - The `cursorSet` is the helper's `Services.Cursor`. As a Cursor it is the default Agent's cursor of before; its `HideWhile` hides every Agent's cursor around winuse's own captures (ADR 0006).
  - `cursorFor(ctx, w)` gives the requesting Agent its own cursor, made on first use in the asked colour or the first free palette colour, with its name ahead of each label (`reviewer: click "Save"`; an automatic Agent shows its program name, such as `claude`) and placed above `w`.
  - The default Agent's cursor shows no name and keeps its colour.
  - An Agent idle for the helper's idle period loses its cursor, which closes. At most 16 cursors stay open, the least recently used closing first.
  - A cursor that cannot be made falls back to the default one.
  - `cursorTravelRect` and `cursorClick` take ctx and the Window: signature changes at their call sites only.
- The idle fade is the existing one (30 s after the last Travel or Click); the cursor comes back on the next Action.
- The Headed banner is untouched. Naming the Agent in it belongs with the desktop lane (ticket 06).
- Tests: `internal/win/cursor_zorder_test.go` (the z-order decision against a faked z-order, the anchor reaching the view, colour parsing and the palette) and `internal/helper/cursors_test.go` (the default Agent's cursor of before, a cursor, colour and name per Agent, WINUSE_CURSOR_COLOR, free colours first, captures hiding all, the idle close, the cap, the fallback, and a Headless click by a named Agent showing its own cursor above its Window).
- E2e:
  - `TestAgentCursorShownAndExcludedFromCaptures` now wants the cursor not topmost and above the fixture in the z-order.
  - The new `TestEachAgentsCursorSitsAboveItsOwnWindow` covers two Agents on two covered fixtures; each cursor sits between its fixture and that fixture's cover. It then minimizes the covers for a 5 s visual check.
  - Both are written and compiled, not run live (live runs are paused).
- The caret moved to ticket 11 (orchestrator decision, 2026-11-03): it needs new UIA text-range work on the worker, a fourth drawing path and hooks in the set-value and type-text routes that headless-max 21 is changing.

