# 15 — Skill, installer, README

**What to build:** Everything a user needs to adopt winuse from any Harness. A SKILL.md adapting Codex's Windows Computer Use guidance (observe, then one Action or a Batch, then observe again; Element Indexes go stale; screenshot only when the tree isn't enough; check focus before typing; stop and ask after Esc or a locked desktop; explain admin-Window refusals; install the binary if missing). It is self-contained and does not mention use-browser. An `install.ps1` one-liner that downloads the release binary onto PATH, mirroring use-browser; a release workflow that builds the binary; a skill layout installable via `npx skills add` into both Claude Code's and Pi's skill locations; a README including how to allow the unsigned binary in Defender/SmartScreen. Publishing the GitHub repo and release is the user's step. See spec: Delivery, Skill guidance.

**Blocked by:** 04 — List and launch Apps; 09 — Windows.Graphics.Capture screenshots; 10 — Scroll, drag, hover; 12 — Esc stop; 13 — Codex-style overlay; 14 — Chromium / Electron apps

**Status:** done

- [x] SKILL.md covers the full command surface and the Codex-derived guidance, with a concise description for lazy loading
- [x] SKILL.md never references use-browser
- [x] `install.ps1` installs the binary from a GitHub Release onto PATH
- [x] Release workflow builds the binary; nothing is published without the user
- [x] Skill installs via `npx skills add` for both Claude Code and Pi
- [x] README documents install, the unsigned-binary Defender note, and the opt-in rule for end-to-end tests
