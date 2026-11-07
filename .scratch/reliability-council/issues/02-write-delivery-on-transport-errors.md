# 02: Write failures say "nothing was sent" on a transport error; delivery is read from English text

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:**

1. `hrStale` counts RPC_E_DISCONNECTED, CO_E_OBJNOTCONNECTED and RPC_E_SERVERFAULT as "the Element is gone" (`wm:internal/uia/com.go:196-205`). On a write, `callErr` turns them into `ErrStale` (`wm:internal/uia/value.go:883-886`), and `refusedBeforeCall` adds "nothing was sent" (`wm:internal/helper/headless.go:620-623,641-649`). A provider that faults or disconnects in the middle of Invoke may already have acted. winuse tells the agent a retry is safe, and the agent can submit twice.
2. The JSON `delivery` and `retry` fields are worked out afterwards by matching the message text against regexes (`wm:internal/protocol/codes.go:153-174`). A failure whose wording matches none of them gets empty fields. A reworded message silently changes the machine-readable answer.

**agent-desktop:** its Mutation Classifier puts a disposition on the error at the native write. Only UIA_E_ELEMENTNOTAVAILABLE, E_ACCESSDENIED, E_INVALIDARG and not-enabled are "not delivered". Transport failures (RPC_E_SERVERFAULT, RPC_E_DISCONNECTED, RPC_S_*) and UIA_E_TIMEOUT are uncertain (`ad:crates/windows/src/actions/mutation.rs:63-114`; `ad:CONCEPTS.md`, "Delivery Semantics").

**What to build:**

- Split the write path from the read path. On a write, the RPC and CO codes become `WriteError` ("may have taken effect"); only UIA_E_ELEMENTNOTAVAILABLE stays stale. Reads keep `hrStale` as it is.
- Carry delivery and retry as typed values from the native error to the Response. Keep the regexes only as a fallback, and default an unknown write failure to uncertain.

**Blocked by:** none

- [ ] Write-path mapping for the RPC/CO codes, with tests per HRESULT
- [ ] Typed delivery on the helper errors; `Stamp` uses it before the regexes
- [ ] Test: every error the Action paths can return has a non-empty delivery
- [ ] CONTEXT.md "Delivery" wording checked; SKILL.md unchanged unless the hint text changes

**Verified by the orchestrator:** all four code sites above, and agent-desktop's classifier. Inferred: how often a provider faults mid-write.

**Conflicts:** none; this is what CONTEXT.md's Delivery already promises. `docs/codex-parity.md`: text output unchanged.
