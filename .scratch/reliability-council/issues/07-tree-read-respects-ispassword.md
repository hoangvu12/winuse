# 07: The tree read never checks IsPassword

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:** IsPassword (30019) is not among the cached tree properties (`wm:internal/uia/com.go:149-173`), and the walk copies Name, HelpText and ValueValue into every line with no secret check (`wm:internal/uia/capture.go:201-224`). The Action paths do check it (`wm:internal/uia/focusvalue.go:48`, `wm:internal/uia/textsplice.go:105`), and so do Page reads. A custom control that exposes its secret through Value puts it into the Window State, the diff, the trace, and the model's context.

**agent-desktop:** caches IsPassword and withholds value-bearing fields; an unreadable answer withholds too (`ad:crates/windows/src/tree/properties/element_properties.rs:38-43,75-118`, Reviewer 2). Its probe found built-in password EDITs don't leak (`ad:probes/windows/FINDINGS.md:172`, A14-6), and winuse's own probe saw WinUI's PasswordBox give "●●●●●●" (`wm:.scratch/headless-gaps/probes-round5.md:30`). The risk is in custom and non-conforming providers.

**What to build:** add 30019 to `treeProperties`; when it reads true, or fails to read on an Edit, leave the Value out of the line and the stored Element (keep role, name and index). Print a `password` state so the agent knows why.

**Blocked by:** none

- [ ] IsPassword cached; Value withheld before render, diff and store
- [ ] Offline tests: true, false, unreadable
- [ ] SKILL.md / CONTEXT.md: the `password` state on a line

**Verified by the orchestrator:** the property list and `data()`. Inferred: how often a real provider leaks.

**Conflicts:** none. The Codex tree grammar keeps its shape (`docs/codex-parity.md`); one more state word.
