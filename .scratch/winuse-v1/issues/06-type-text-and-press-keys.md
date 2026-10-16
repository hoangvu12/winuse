# 06 — Type text and press keys

**What to build:** `winuse type-text <window> <text>` types arbitrary Unicode text into the focused Element of the activated Window; `winuse press-key <window> <key>` sends keys and key combinations using Codex's X keysym names (e.g. `Return`, `Tab`, `Control_L+a`). See spec: Acting details.

**Blocked by:** 05 — Click by Element Index

**Status:** ready-for-agent

- [ ] Unicode text (including non-Latin scripts and emoji) is typed correctly
- [ ] Key names and combinations match Codex's accepted names; unknown names fail with a hint
- [ ] Offline tests cover key-name parsing
- [ ] End-to-end: typing into the fixture's text field, then `Control_L+a` and a replacement key, leaves the expected text (verified by user run)
