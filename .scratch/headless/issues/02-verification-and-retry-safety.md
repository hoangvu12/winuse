# 02 — Read-back verification and retry safety

**What to build:** after a Headless Toggle, Select, Expand, Collapse or Value set-value, read the Element's state live and fail on a mismatch with the observed state and "the Action was delivered; do not repeat it". Failures raised before any pattern call or input say "nothing was sent". See spec: Verification, Retry safety.

**Blocked by:** 01

**Status:** done

- [x] Toggle/Select/Expand/Collapse/set-value mismatches fail with the observed state
- [x] Invoke and Legacy report plain `ok`
- [x] Password fields are never echoed in a mismatch
- [x] Pre-call refusals carry "nothing was sent"

## Comments

- 2026-11-02 orchestrator: uia layer (headless.go, value.go focus flag, WriteError/VerifyError), protocol `headed` fields and win.HeadlessReady landed in the previous chat (uncommitted). Helper routing, CLI `--headed`, Batch scopes, passive Esc and failure mapping delegated to Worker A (a subagent).
- 2026-11-02 orchestrator (reviewed, done): `helper.headlessFailure` maps ErrNoClickRoute → "; nothing was sent" + `winuse --headed click <window> <ref>` hint; `*uia.VerifyError` → "the Action was delivered; do not repeat it, read the Window State"; `*uia.WriteError` → "the Action may have taken effect; read the Window State … before repeating it"; stale/no-state/disabled/not-settable/range/unavailable-action and every pre-call refusal (locked, admin, modal dialog, minimized) carry "; nothing was sent". An Esc stop before the pattern call keeps exit 130 with a nothing-sent hint. Bug fixed in the earlier uia code: `verifyValue` put a password field's typed value into the VerifyError's Expected text; it now says "the value that was set". Headed paths' messages are unchanged.
