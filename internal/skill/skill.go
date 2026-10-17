// Package skill guards winuse's shipped agent-facing deliverables: the
// SKILL.md an agent loads, and the distribution files that install it
// (install.ps1, the release workflow). Its tests are the ticket-15
// acceptance criteria expressed as offline checks: the skill's frontmatter
// stays valid for `npx skills add`, the SKILL.md keeps covering the full
// command surface as the CLI grows, it never mentions the tool it must not
// depend on, and the installer's asset names stay in sync with what the
// release workflow builds.
package skill
