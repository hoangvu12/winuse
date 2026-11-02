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

**Status:** ready-for-agent (opt-in only; a default switch would conflict with ADR 0001, ADR 0004 and the codex-parity "tree format" and "error style" rows)

- [ ] Response fields and the code table
- [ ] Codes set at every failure site; the coverage test
- [ ] `--json` printer in the CLI
- [ ] Offline tests: envelope per outcome, every phrase mapped
- [ ] SKILL.md short section; codex-parity note

## Comments

### 2026-11-03: triage decision (user)

Kept: `--json` is opt-in; the ADR 0004 text stays the default and the Window State stays text inside `out`.
