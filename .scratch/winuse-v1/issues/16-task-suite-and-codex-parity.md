# 16 — Task Suite + Codex parity checklist

**What to build:** The proof that winuse is as good as Codex. A Task Suite of about 10 real tasks spread across ordinary Windows apps, Chromium/Electron apps, a dialog-heavy flow and a canvas app, each working only in a scratch folder and each with a Check script that inspects end results (no model judges). A runner executes every task unattended via `claude -p` with Opus 5.5 under fixed time and turn limits and prints a pass/fail table with steps and time; a smaller text-only subset runs through Pi with glm-5.3. Task ideas may be adapted from WindowsAgentArena (MIT); the method follows Cua Driver Bench (same Harness, model and prompt across runs) without copying its FSL code. Plus a feature-by-feature parity checklist against Codex's Windows tool surface. Runs take over the screen and are started only by the user. See spec: Testing Decisions (Task Suite).

**Blocked by:** 15 — Skill, installer, README

**Status:** done

- [x] About 10 tasks, each with setup in a scratch folder and a Check script
- [x] Checks inspect files, settings or app state only; no model grading
- [x] Runner uses `claude -p` with Opus 5.5 and prints a pass/fail table with steps and time
- [x] Text-only subset runs through Pi with glm-5.3
- [x] Runner refuses to start unless the user explicitly launches it
- [x] Codex parity checklist covers every Codex Windows tool and parameter, marking each as matched or intentionally different
