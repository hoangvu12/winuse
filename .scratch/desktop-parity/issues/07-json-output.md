# 07: Opt-in `--json` output with codes and delivery

**What to build:** a global `--json` flag. Every command then prints one JSON line:

`{"ok":true,"command":"click","out":"..."}` or `{"ok":false,"command":"click","error":{"code":"STALE_REF","message":"...","hint":"...","delivery":"not_delivered","retry":"safe"}}`

agent-desktop's envelope and codes: `ad:docs/json-output.mdx:7-128`, `ad:docs/errors-and-recovery.mdx:45-109`.

- Add `code` and `delivery` to `protocol.Response` (`internal/protocol/protocol.go:355-362`). Map today's phrases:
  - "nothing was sent" → `not_delivered`, retry `safe`;
  - "the Action was delivered; do not repeat it" → `delivered_unverified` or `delivered_verified`, retry `unsafe`;
  - "may have taken effect" → `delivery_uncertain`, retry `unsafe`.
- Codes, reusing agent-desktop's names where they match: `STALE_REF`, `AMBIGUOUS_TARGET`, `NOT_FOUND`, `PERM_DENIED` (administrator Window), `DESKTOP_LOCKED`, `BLOCKED_BY_MODAL`, `MINIMIZED`, `NO_HEADLESS_ROUTE`, `VERIFY_FAILED`, `TIMEOUT`, `STOPPED` (Esc), `USAGE`, `INTERNAL`.
- The Window State text tree stays a string inside `out`. No JSON tree.
- Exit codes unchanged.
- Each failure site in the helper sets its code where the message is built; a test fails if a helper error reaches the CLI without a code.

**Blocked by:** none

**Status:** done (opt-in only; a default switch would conflict with ADR 0001, ADR 0004 and the codex-parity "tree format" and "error style" rows)

- [x] Response fields and the code table
- [x] Codes set at every failure site; the coverage test
- [x] `--json` printer in the CLI
- [x] Offline tests: envelope per outcome, every phrase mapped
- [x] SKILL.md short section; codex-parity note

## Comments

### 2026-11-03: triage decision (user)

Kept: `--json` is opt-in; the ADR 0004 text stays the default and the Window State stays text inside `out`.

### 2026-11-03: done

- `protocol.Response` gained `code`, `delivery` and `retry` (`internal/protocol/codes.go`). The ~430 helper failure sites are unchanged: a code comes from the failure's message (and, for an administrator Window, its hint) through one ordered rule table, `protocol.CodeFor`; a site that knows better sets `helper.Error.Code` (the Batch's stop keeps its step's). The server stamps every failed Response in `handle` (`Response.Stamp`), with STOPPED for an Esc stop and TIMEOUT for a spent budget whatever the command said; anything no rule matches is INTERNAL.
- Delivery is the phrase, in one place (`protocol.DeliveryOf`): "may have (partly) taken effect" is `delivery_uncertain`, "nothing was sent/started/changed/added" `not_delivered` (retry `safe`), "was delivered" or "do not repeat" `delivered_unverified` (retry `unsafe`); the request timeout is `delivery_uncertain`, a usage error `not_delivered`. `delivered_verified` is not produced: no failure verifies its delivery. A failure whose text says nothing has no delivery.
- Coverage: `helper/codes_test.go` parses the helper's source and fails when a Fail or Error literal whose message has literal text matches no rule (messages that are mostly computed, `err.Error()` or `"<cmd>: "+err`, are classified at run time, INTERNAL when nothing matches), checks every delivery phrase at a failure site maps, and drives `handle` through each way a request fails to check each Response carries a code.
- CLI: `--json` among the leading flags (before the command, with `--headed` and the trace flags; after the command it stays the command's, so type-text's text keeps it). `Main` runs the command unchanged with stdout and stderr captured (`json.go`), then prints one line: `ok`, `command`, `out`, and on a failure `error` with the helper's code, message, hint, delivery and retry (`finish` records the last Response), or the CLI's own failure line classified the same way (a usage error is USAGE, the request timeout TIMEOUT). Exit codes are unchanged; a success's stderr notes stay on stderr. Without `--json` nothing changes.
- The dp-19 trace line copies `code` and `delivery` at its marked spot.
- Live e2e: `TestJSONOutputOfAClickAndAStaleIndex` (test/e2e/json_output_test.go) passed on 2026-11-03 (branch e2e-live-fixes-2).
