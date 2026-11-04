// Package skill guards winuse's shipped agent-facing deliverables: the
// skill an agent loads (the core SKILL.md and its topics in references/),
// and the distribution files that install it (install.ps1, the release
// workflow). Its tests are the ticket-15 acceptance criteria expressed as
// offline checks: the skill's frontmatter stays valid for `npx skills
// add`, the skill keeps covering the full command surface as the CLI
// grows, it never mentions the tool it must not depend on, and the
// installer's asset names stay in sync with what the release workflow
// builds. Since speed issue 01 they also pin the split: the core keeps
// the safety rules, the Esc stop and recovery first and stays small,
// every topic is listed and every pointer to one resolves, and every
// section of the old single file has a home. The binary's embedded copy
// (the root package's go:embed, printed by `winuse skill`) is checked to
// equal the shipped files.
package skill
