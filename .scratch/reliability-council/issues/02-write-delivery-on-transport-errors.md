# 02: Write failures say "nothing was sent" on a transport error; delivery is read from English text

Status: done

**What's wrong:**

1. `hrStale` counts RPC_E_DISCONNECTED, CO_E_OBJNOTCONNECTED and RPC_E_SERVERFAULT as "the Element is gone" (`wm:internal/uia/com.go:196-205`). On a write, `callErr` turns them into `ErrStale` (`wm:internal/uia/value.go:883-886`), and `refusedBeforeCall` adds "nothing was sent" (`wm:internal/helper/headless.go:620-623,641-649`). A provider that faults or disconnects in the middle of Invoke may already have acted. winuse tells the agent a retry is safe, and the agent can submit twice.
2. The JSON `delivery` and `retry` fields are worked out afterwards by matching the message text against regexes (`wm:internal/protocol/codes.go:153-174`). A failure whose wording matches none of them gets empty fields. A reworded message silently changes the machine-readable answer.

**agent-desktop:** its Mutation Classifier puts a disposition on the error at the native write. Only UIA_E_ELEMENTNOTAVAILABLE, E_ACCESSDENIED, E_INVALIDARG and not-enabled are "not delivered". Transport failures (RPC_E_SERVERFAULT, RPC_E_DISCONNECTED, RPC_S_*) and UIA_E_TIMEOUT are uncertain (`ad:crates/windows/src/actions/mutation.rs:63-114`; `ad:CONCEPTS.md`, "Delivery Semantics").

**What to build:**

- Split the write path from the read path. On a write, the RPC and CO codes become `WriteError` ("may have taken effect"); only UIA_E_ELEMENTNOTAVAILABLE stays stale. Reads keep `hrStale` as it is.
- Carry delivery and retry as typed values from the native error to the Response. Keep the regexes only as a fallback, and default an unknown write failure to uncertain.

**Blocked by:** none

- [x] Write-path mapping for the RPC/CO codes, with tests per HRESULT
- [x] Typed delivery on the helper errors; `Stamp` uses it before the regexes
- [x] Test: every error the Action paths can return has a non-empty delivery
- [x] CONTEXT.md "Delivery" wording checked; SKILL.md unchanged unless the hint text changes

**Verified by the orchestrator:** all four code sites above, and agent-desktop's classifier. Inferred: how often a provider faults mid-write.

**Conflicts:** none; this is what CONTEXT.md's Delivery already promises. `docs/codex-parity.md`: text output unchanged.

## Comments

### 2026-11-07: landed

- Write path (`internal/uia/value.go`, `callErr`): a failed pattern write is stale only on UIA_E_ELEMENTNOTAVAILABLE (new `uiaErrElementGone`, `internal/uia/com.go`). RPC_E_DISCONNECTED, CO_E_OBJNOTCONNECTED and RPC_E_SERVERFAULT are now a `WriteError` ("... failed (HRESULT ...)", hint "the Action may have taken effect; read the Window State ... before repeating it"). `hrStale` is unchanged: it still classifies reads and the reads before a write (a pattern fetch or QueryInterface that fails stale sent nothing). Every other `hrStale` caller in uia is such a read; the direct `WriteError` sites (scroll, toggle again, multi-select, tab strip, splice) were already uncertain.
- Typed delivery (`internal/helper/delivery.go`, new): `helper.Error` gains `Delivery`. `nothingSent` sets not delivered, `mayHaveActed` uncertain, `wasDelivered` delivered-unverified; `headlessFailure`, `keyFailure`, `shortcutFailure` (its "the command may have run" matched no phrase, so its JSON delivery was empty), the Headless tab switch, the notification press and the pre-act Esc stop set theirs. `nativeDelivery` maps the native errors (`uia.VerifyError` delivered, `uia.WriteError` uncertain, `ErrNoClickRoute`, `ErrRaiseHeadless` and every `refusedBeforeCall` refusal not delivered). `queuedOutError` (`lanes.go`) is typed not delivered: it never ran, though its text does not say so.
- For ticket 06: `helper.SentNothing(err error) bool` (and `helper.Delivery(err) string`) answer for an `*Error` (its own value, else its text) or a native error (`nativeDelivery`, else its text); only a proven refusal is true. `protocol.SentNothing(delivery)` and `protocol.RetryOf(delivery)` are the same question on a Response's string.
- Response (`internal/protocol/codes.go`): `Stamp(code, delivery)` takes the site's delivery first, then `DeliveryOf`'s regexes, then usage (exit 2) as not delivered; retry comes from `RetryOf`. `Server.handle` passes the command's delivery on the failure and Esc paths (the timeout path keeps "winuse request timed out", uncertain). The CLI's own failures call `Stamp("", "")`.
- Default: every Action in the registry (`state.go`: not observation-only, not `wait`) is wrapped by `actionDelivered`. A failure with no typed delivery and no phrase becomes uncertain (copied, never mutating a shared `*Error`). A Batch's stop carries its step's delivery, and `uncertainFailure` (`batch.go`) reads the typed value before the phrases.
- Text output: unchanged except the ticket's change (the RPC/CO write failures above no longer say "nothing was sent"). JSON: Action failures that said nothing (for example `no open Window with ID 42` on a click, the shortcut and notification "may have ..." hints) now carry `delivery_uncertain` / `unsafe` instead of empty fields.
- Docs: CONTEXT.md "Delivery" says where the value is set and that an Action's failure that cannot say is uncertain; `docs/codex-parity.md`'s `--json` paragraph says the same. SKILL.md and `references/troubleshooting.md` unchanged (no hint text changed; "a failure that does not say ... treat the outcome as unknown" already matches).
- Tests: `internal/uia/action_test.go` `TestWriteHRESULTs` (each of the four HRESULTs, UIA_E_TIMEOUT and E_FAIL through `callErr`; disabled; the constant's hex); `internal/protocol/codes_test.go` (`TestStamp`: typed first, typed over contrary text, untyped unknown stays empty; `TestRetryOfAndSentNothing`); `internal/helper/delivery_test.go` (`TestHeadlessFailuresCarryTheirDelivery`: every native kind through `headlessFailure`, typed value equal to the text's; `TestDeliveryOfAnError`; `TestActionDeliveredDefaultsToUncertain`; `TestEveryActionFailureHasADelivery`: every registered Action, refused at its lanes with the desktop lane held, by the lane limit and by its budget, has a delivery); `internal/helper/codes_test.go` gains a typed case in `TestHandleStampsEveryFailure`.
- Green: `go vet ./...`, `CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go test ./...`.
- Open: `TestEveryActionFailureHasADelivery` reaches only the lane refusals offline (running each Action's handler with the real default services could drive the desktop); `actionDelivered` itself is covered by its unit test. A Batch step whose failure says nothing about delivery is not marked "uncertain" in the Batch's text (its steps are raw commands, outside the wrapper); the Batch's own JSON delivery then defaults to uncertain, but its step line does not say so. Live: not run (offline only).
