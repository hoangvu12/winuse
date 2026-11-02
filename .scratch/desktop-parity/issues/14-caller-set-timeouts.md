# 14: Caller-set timeouts

**What to build:** `--timeout <s>` on `get-window-state`, `find`, `launch-app`, `batch` and `wait`, clamped to a maximum, for slow Electron, Office and cold Chromium trees. agent-desktop's per-command budgets: `ad:docs/skeleton-and-drill-down.mdx:332-343`.

Today the budgets are fixed: 10 s per request, launch 15 s, batch 60 s, browser 30-180 s, Page `wait --timeout` up to 25 s (`internal/protocol/protocol.go:28-74`).

- The CLI's pipe deadline and the helper's request context both follow the flag.
- The UIA walk budget and worker abandonment already follow the context (`internal/uia/thread.go:297-346`, `:443-456`).
- Caps: get-window-state 60 s, launch 120 s, batch 300 s, wait 300 s (proposal).
- Action commands keep their fixed budget: a slow Action is a bug or a hung App.

**Blocked by:** none

**Status:** ready-for-agent (codex-parity "timeouts" row: add an extension note)

- [ ] Flag in CLI, protocol; deadlines wired
- [ ] Offline tests: clamping, deadline propagation
- [ ] SKILL.md; codex-parity note
