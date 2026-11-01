# 04 — Skill, README, usage text and glossary

**What to build:** SKILL.md and README teach Headless vs Headed, `--headed`, the Agent Cursor and the new failure texts; CONTEXT.md gains Headless Action, Headed Action and Agent Cursor; the CLI usage lists `--headed`.

**Blocked by:** 01, 02, 03

**Status:** done

- [x] Skill parity test passes with the new text
- [x] Glossary entries added

## Comments

- 2026-11-02 orchestrator (reviewed, done): Worker C updated SKILL.md (new "Headless and Headed Actions" and "Failures: was anything sent?" sections; Typing/Batches/Esc/Refusals/Commands/Rules adjusted; the batch example is now `--headed batch …`), README "Staying in control", docs/codex-parity.md (delivery rows intentionally different; esc stop and overlay split Headed matched / Headless intentionally different; `--headed` and the Agent Cursor under extensions), and CONTEXT.md (Headless Action, Headed Action, Agent Cursor; Esc stop adjusted). Skill and parity tests pass. Open: the CLI usage's batch example (`click name:File then type-text …`) still mixes a Headless click with typing; see the focus-click question in 05's comments.
