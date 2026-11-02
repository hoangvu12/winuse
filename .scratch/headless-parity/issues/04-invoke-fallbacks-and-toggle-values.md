# 04: Invoke fallbacks and set-value on toggles

**What to build:**

- **Invoke fallback.** Toggle, Expand and Collapse fall back to one Invoke call. This covers both the Secondary Actions and the click route's legs. The fallback fires when:
  - the pattern call fails with a WriteError, or it leaves the known state from before unchanged;
  - and the Element offers Invoke.
- **set-value on toggles.** `set-value <element> on|off|true|false|checked|unchecked` works on a Toggle-only Element and is idempotent:
  - nothing is sent when the state already matches;
  - otherwise it toggles at most twice and verifies the result.

See spec: "Invoke fallbacks and set-value on toggles".

**Blocked by:** none

**Status:** done

- [x] The fallback fires at most once, only with a readable state before, and the result is verified again
- [x] set-value on a toggle: parsing of the six words, the no-op when already in the target state, a three-state box reaching its target in two toggles, verification
- [x] Offline tests for the decision logic (pure functions over states), as in value_test.go

## Comments

2026-11-02: Landed.
- `internal/uia/headless.go`: `invokeFallback` (pure decision) and `runWithInvokeFallback` (driver over injected steps), bound to COM by `actWithInvokeFallback`. Used by the click route's Toggle and Expand/Collapse legs and by `PerformSecondaryActionHeadless`. The Headed v1 `PerformSecondaryAction` is unchanged. The fallback fires from a known state before (any readable toggle state; collapsed for Expand, expanded for Collapse) when the pattern call failed with a WriteError or delivered, and the state read afterwards still equals the state before. A WriteError whose state did change does not invoke, because the call may have acted. A missing or failed Invoke leaves the first outcome.
- `internal/uia/value.go`: `setValue` (shared by Headless and Headed, so Headed keeps its focus-first step) gains a Toggle branch after Value and RangeValue. `parseToggleValue` takes the six words, any case. `toggleTo` drives the toggles: nothing is sent at the target; otherwise it toggles until the target reads, at most twice, and sends a second toggle only after the first visibly moved the state. The result is verified (VerifyError "set-value"). The number of toggles cannot be read off the state, because cycles differ (Win32/WPF go off, on, indeterminate; UIA documents on, off, indeterminate), so the bound is two with an early stop. A failed second toggle is always a WriteError (never "nothing was sent").
- New sentinels: `ErrToggleValue` (names the six words) and `ErrToggleUnreadable` (the state before cannot be read, so nothing is sent). Both are in `refusedBeforeCall` and have hints in `valueError`.
- Offline tests: `TestInvokeFallbackDecision`, `TestRunWithInvokeFallback` (headless_test.go), `TestParseToggleValue`, `TestToggleTo` with a fake three-state box (value_test.go), plus the helper mapping in headless_test.go and setvalue_test.go.
- The click route's Toggle leg only reaches its fallback when the first Invoke probe failed, because an Element offering Invoke takes the Invoke leg first.
