# 06: Wait briefly on a transient refusal instead of failing at once

Status: done

**What's wrong:** a Headless Action fails at once when the Element is disabled (`wm:internal/uia/value.go:887-888`, `wm:internal/helper/headless.go:630-633`), when the Window is blocked by a modal dialog that may be closing (`wm:internal/win/activate.go:84-97`), or when a `name:` reference has not appeared yet. A "Next" button that enables 300 ms later costs the agent a full turn and a re-read. `wait <window> <element> enabled` exists (`wm:internal/helper/waits.go`, desktop-parity 03), but the agent has to know to use it.

**agent-desktop:** polls actionability up to 5 s by default and fails fast on permanent errors (`ad:crates/core/src/ref_action/ref_action_poll.rs:28-101`). It polls outside its lease and re-checks once it holds it, and retries only when the failure proves nothing was delivered (`ad:crates/core/src/ref_action/ref_action_wait.rs:80-143`, Reviewer 2).

**What to build:**

- Only for refusals that sent nothing: disabled, modal dialog closing, name not found yet.
- Poll for 1.5-2 s (not 5 s) **before** taking the Window lane, then act under the lane after one more check. Never wait while holding a lane (ADR 0014).
- Never replay a write whose outcome is uncertain (ticket 02).
- Don't copy agent-desktop's visible or unoccluded checks for Headless Actions: acting on covered Windows is the point of Headless.
- The result says so: "waited 0.4 s for 14 to enable".
- An opt-out (`--no-wait`, or `--wait 0`) for agents that want the old behaviour.

**Blocked by:** 02 (the poll must know which failures are safe to retry)

- [x] Pre-dispatch poll for the three refusals, outside the lane
- [x] Note in the result; opt-out flag
- [x] Offline tests: enables within the wait; stays disabled (old error, after the wait); dialog closes; name appears; the lane is not held while polling
- [x] SKILL.md: the "disabled" advice and the waits section

**Verified by the orchestrator:** the immediate refusals at the cited lines. Inferred: the turn saved per case; the right timeout.

**Conflicts:** SKILL.md's current advice for a disabled Element; ADR 0014 lanes (respected by polling outside). It may touch `internal/helper/state.go` and command wrapping, which the pending perf change also touches. Must not bring back a fixed post-Action wait.

## Comments

### 2026-11-07: landed

- Design (`internal/helper/briefwait.go`, new; wired in `state.go`'s registry as `s.briefWaited(name, s.laned(...))`): the Action runs once on its lanes as before. When it is refused for one of the three reasons and the refusal provably sent nothing (`Error.wait` set at the source and `SentNothing`), it has already returned and released its lanes; winuse polls outside every lane, then runs the whole command again, which retakes its lanes and makes its own checks (the identity check, the hung-App check, Ready, the pattern call's refusal): that is the one more check under the lane. Chosen over a pre-dispatch poll because it costs nothing when the first try succeeds and needs no second copy of each command's checks. A refusal that comes back again sleeps one interval before the next poll, so two tries never run back to back. A Batch does not wait (its steps run inside the Batch's lanes, and `briefWaited` skips `batch`).
- Typed at the source (`Error.wait`, `refusalWait`, `waitsFor`): disabled in `headlessFailure` (`uia.ErrDisabled` with an Element Index; click, set-value, Secondary Actions, scroll, select-text and type-text --into all map through it); the modal dialog in `readyError` (Headless: already not delivered) and in `runAction`'s activation (`actions.go`: now `nothingSent`, so a Headed modal refusal ends in `; nothing was sent`; the foreground was refused before any input); a `name:`/`id:` that matches nothing, cached or in a fresh read, in `resolveElementRef` (`refs.go`, `refMissing`; it waits only where the Action types it not delivered, which the Headless pipeline does and the Headed one does not). Ambiguous references and every other refusal do not wait.
- Polls (`waitOut`, through `pollUntil` from `waits.go`, `uia.WithWaitPoll`, a passive Esc scope): disabled holds the Element (`ElementHolder`) and reads its live IsEnabled every 100 ms; the dialog re-runs `Services.Ready` (`win.HeadlessReady`) every 100 ms until it reports no `ModalDialogError`; a reference walks the tree with `PeekTree` every 250 ms, without replacing the Agent's Window State, until `wstate.ResolveElementRef` finds it (several matches count: the try then lists them). A poll that cannot tell (no holder, a stale Element, a failed walk) ends the wait with the refusal as it was. The wait is 1.8 s (`briefWaitLimit`), at most half the request's remaining budget, never a pause past it; Esc stops it with `escStopBeforeActError` (exit 130, nothing sent). No visible or unoccluded check; no fixed post-Action wait.
- Output: `ok` then `Note: waited 0.4 s for element 14 to enable` (or `for the modal dialog over window 101 to close`, `for name:Next to appear in window 101`). Past the wait, the old refusal with its message unchanged and the hint gaining `(winuse waited 1.8 s for element 14 to enable; to wait longer: winuse wait 101 14 enabled --timeout <seconds>)`; its delivery stays not delivered.
- Opt-out: global `--no-wait` (`internal/cli/nowait.go`; `wait` is a command and `--wait` already means a navigation's load state and `browser ext status`'s seconds), placed like `--headed` (hoisted by `leadingFlag`, kept in literal text, read by `startTrace`'s leading loop), carried as `protocol.Request.NoWait` (`no_wait`), put on the context by `Server.handle` (`withNoWait`). Commands that do not wait ignore it, a Batch among them.
- Tests: `internal/helper/briefwait_test.go` (enables within the wait, with the note and the lanes free at every poll while the click itself runs on its lane; stays disabled: the old message, the waited hint, still not delivered, one click; a modal dialog closes, Headless and Headed, both lanes free while polling; `name:Next` appears and the click lands on its new index, and one that never appears; `--no-wait` fails at once with no poll; an uncertain write is never tried again, also when it is the try after the wait; Esc during the poll; half the budget bounds it; a Batch step does not wait; `no_wait` reaches the command's context). `internal/cli/nowait_test.go` (positions, literal text, beside `--headed`, the wire field, help). The four existing modal-refusal tests (`TestClickModalDialogPointsAtTheDialog`, `TestHeadlessRefusalsBeforeAnyPatternCall`, `TestInvokeMenuRefusalsSayNothingWasSent`, `TestPressKeyDialogRefusalsSayNothingWasSent`) now send `--no-wait` (`runCommandNoWait`), the old immediate refusal; the Headed one expects `; nothing was sent`.
- Docs: CLI help (`--no-wait` among the global options and the index line); `skills/winuse/SKILL.md` (the Recover rows for a modal dialog and a disabled Element, and a paragraph after the commands table); `references/batch-and-wait.md` (a bullet under "Wait for a change", with the Batch advice), `references/troubleshooting.md`, `references/commands.md` (the usage line). CONTEXT.md and ADR 0014 unchanged: no sentence became wrong (the poll holds no lane).
- Green: `go vet ./...`, `CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go test ./...`.
- Open: Live: not run (offline only); the 1.8 s and the poll intervals are not measured on a real App. A Headed Action's disabled Element and missing reference do not wait (the Headed pipeline resolves after the activation and does not type those refusals not delivered). The menu-item click (`menuitem.go`) maps a disabled Element outside `headlessFailure` and does not wait. A Batch step fails at once as before. For ticket 09: the identity re-check belongs inside `laned` (`checkWindow`), so every try, the one after the wait included, re-checks it.

### 2026-11-07: after merging ticket 03 (orchestrator)

- `refMissing` (`internal/helper/refs.go`) no longer waits on a miss in a partial read (`NoMatchRefError.Partial`, ticket 03): the Element may sit in the part not read, which waiting does not change, so it fails at once with 03's refusal. A one-match refusal in a partial read (`PartialRefError`) never waited. Test: the partial case in `TestBriefWaitForANameToAppear`.
