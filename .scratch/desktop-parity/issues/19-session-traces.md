# 19: Session traces with redaction

**What to build:** `--trace <file>` (or `WINUSE_TRACE`) appends one JSON line per command: time, command, Window (App and title), arguments, outcome, delivery, duration, the route taken (Headless, Posted, Headed, CDP), and references. agent-desktop: session JSONL traces, `trace show`/`export` to HTML, redaction (`ad:docs/guides/traces.mdx:7-109`; `ad:crates/core/src/trace/trace_sanitize.rs`).

- Redact by default: typed text, set-value values, clipboard content, eval expressions and results, Element names in password fields. Replace with `{"redacted":true,"len":N}`.
- `--trace-unredacted` for local debugging only.
- Screenshots are referenced by path, not embedded.
- Size cap per file (32 MiB like agent-desktop), then roll over.
- `winuse trace show <file>` prints a readable summary. HTML export is out of scope.

**Blocked by:** none (uses 07's codes and delivery fields when present)

**Status:** ready-for-agent

- [ ] Trace writer in the CLI (one place for every command)
- [ ] Redaction rules with tests
- [ ] `trace show`
- [ ] SKILL.md and README mention

## Comments

### 2026-11-03: triage decision (user)

Kept as written.
