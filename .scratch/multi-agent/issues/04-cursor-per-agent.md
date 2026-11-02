# 04: A cursor per agent, like cua

**What to build:** one Agent Cursor per agent, placed and timed like cua's (`libs/cua-driver/rust/crates/cua-driver-core/src/agent_cursor.rs`, `cursor-overlay`, MIT).

- One small layered window per agent (WS_EX_LAYERED, TRANSPARENT, TOOLWINDOW, NOACTIVATE), with its own colour and a label: the `WINUSE_AGENT` name, or a short default.
- **Z-order like cua:** not topmost. Insert it just above the target Window (HWND_NOTOPMOST, then after `GetWindow(target, GW_HWNDPREV)`), so the user's Windows above the target also cover the cursor. Re-place it on each Action. One shared window gives the wrong z-order with several targets (cua #1800).
- **Lifecycle like cua:** it fades out after an idle timeout and comes back on the agent's next Action; it hides at once when the agent is gone (helper idle eviction of that agent).
- Excluded from winuse's own captures, as today (ADR 0006).
- **Absorbs desktop-parity 21 (cursor polish):** the insertion caret after a Headless type-text or set-value, and the label and colour options, now per agent: `WINUSE_AGENT` for the label, `WINUSE_CURSOR_COLOR` for the colour. Colours are assigned automatically otherwise.
- The Headed banner stays one banner (one desktop lane, ticket 06), naming the agent.

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] Cursor manager keyed by agent; per-agent window, colour and label
- [ ] Z-order placement above the target; idle fade; hide on eviction
- [ ] desktop-parity 21's caret and options
- [ ] Offline tests: colour assignment, option parsing, placement decision against a faked z-order
- [ ] Opt-in e2e: two agents' cursors visible on two covered fixtures; the user's cover stays above them (visual check by the user)
