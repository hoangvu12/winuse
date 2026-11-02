# 14: Caller-set timeouts

**What to build:** `--timeout <s>` on `get-window-state`, `find`, `launch-app`, `batch` and `wait`, clamped to a maximum, for slow Electron, Office and cold Chromium trees. agent-desktop's per-command budgets: `ad:docs/skeleton-and-drill-down.mdx:332-343`.

Today the budgets are fixed: 10 s per request, launch 15 s, batch 60 s, browser 30-180 s, Page `wait --timeout` up to 25 s (`internal/protocol/protocol.go:28-74`).

- The CLI's pipe deadline and the helper's request context both follow the flag.
- The UIA walk budget and worker abandonment already follow the context (`internal/uia/thread.go:297-346`, `:443-456`).
- Caps: get-window-state 60 s, launch 120 s, batch 300 s, wait 300 s (proposal).
- Action commands keep their fixed budget: a slow Action is a bug or a hung App.

**Blocked by:** none

**Status:** done

- [x] Flag in CLI, protocol; deadlines wired
- [x] Offline tests: clamping, deadline propagation
- [x] SKILL.md; codex-parity note

## Comments

### 2026-11-02: landed

- Protocol: `Request.TimeoutMS` (`timeout_ms`, omitted when unset) carries the caller-set budget. `protocol.MaxTimeout` lists the commands that take one with their caps: get-window-state 60 s, launch-app 120 s, batch 300 s, wait 300 s plus `WaitMargin` (1 s). `CallerTimeout` clamps to the cap and ignores every other command, Actions included. `Budget(req, def)` is the one budget rule both ends use: the command's explicit budget or the default, widened to the caller's. A value under that budget leaves it as it is, so `--timeout` only widens.
- Deadlines: the CLI's pipe deadline (`client.roundTrip`) and the helper's request context and connection deadline (`server.serveConn`, `handle`) now come from `protocol.Budget`, so all three follow the flag.
- get-window-state: the handler puts a wider Chromium settle on the context (`uia.WithSettle`, read by `uia.SettleOf` in `CaptureFrame` and passed to `readTree`'s settle parameter). It is `uia.DefaultChromiumSettle` plus whatever the request budget adds over the 10 s default (`stateSettle`): `--timeout 40` settles for up to 35 s. `settlePolls` still bounds the settle by the request deadline. The walk budget and worker abandonment already followed the context.
- launch-app: the poll budget is `launchPoll(ctx)`, the request deadline minus the same 500 ms margin, never under the old 14.5 s.
- wait: `--timeout` now goes up to 300 s (was 25). The CLI asks for the wait plus `WaitMargin` as the request budget. The helper's `pageWaitMargin` is `protocol.WaitMargin`. Inside a Batch, the Batch's budget bounds a wait step.
- CLI (`internal/cli/timeout.go`): `--timeout <s>` or `--timeout=<s>` on get-window-state and launch-app goes anywhere. On batch it goes before the first Action (before or after the Window), because a later one belongs to a wait step. Values outside (0, cap] are usage errors (exit 2) that name the cap; the protocol also clamps for other clients. Usage text, SKILL.md (launch, Window State, wait, Batches, the timeout recovery bullet, command reference), README's command table and codex-parity.md's "timeouts" row (an **extension** note; the row keeps **matched**) are updated. CONTEXT.md needed no new term.
- `find` is not built: it does not exist in this tree (desktop-parity 04 is not done). When 04 lands, add `"find": MaxStateTimeout` (or its own cap) to `protocol.maxTimeouts`, take the flag in its CLI parser, and widen its settle the way get-window-state does.
- Tests: `protocol_test.go` (`TestCallerTimeout` clamping and Action exclusion, `TestBudget` widen-only, the JSON shape), `helper/timeout_test.go` (through the server: the command's context deadline follows `timeout_ms`, clamped, with click unaffected; get-window-state's settle widens through `CommandsWith`; `stateSettle`, `launchPoll`), `uia/retry_test.go` (`TestSettleOf`), `cli/timeout_test.go` (flag parsing and caps, batch's position rule, wait's request budget, and the pipe deadline plus the sent `timeout_ms`). No e2e test; nothing was run against real Windows.
