# 08: Display list and full-screen capture

**What to build:** `list-displays` (index, bounds in physical pixels, effective scale, primary) and `screenshot --display N`. agent-desktop: `list-displays` and display capture by index (`ad:crates/windows/src/system/display.rs:5-13`).

- EnumDisplayMonitors plus GetDpiForMonitor (winuse is per-monitor-aware v2, `internal/win/dpi.go:9-31`).
- Capture with WGC `CreateForMonitor`, falling back to BitBlt of the monitor rectangle. No activation: Headless.
- A display screenshot is observation only. It gets no `@x,y` binding, because `@x,y` binds to a Window's latest screenshot (CONTEXT.md "Window State"). The output says so.
- The Agent Cursor and overlay stay excluded from the capture (ADR 0006).
- Desktop commands (term from ticket 01).

**Blocked by:** 01 (the "Desktop command" term)

**Status:** ready-for-agent (after 01 adds the Desktop command term)

- [ ] Monitor enumeration and capture
- [ ] CLI, protocol, helper
- [ ] Offline tests of the listing format and DPI maths
- [ ] Opt-in e2e: list and capture the primary display
- [ ] SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop: `list-displays` and a capture by display index, Headless. The display capture has no `@x,y` binding and says so.
