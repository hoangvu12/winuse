// Package winuse embeds the agent skill in the binary. It sits at the
// repository root because go:embed cannot reach outside its package's
// directory, and the skill must stay in skills/winuse/ where `npx skills
// add` finds it alone (internal/skill's TestSkillShipsAlone).
package winuse

import "embed"

// Skill is skills/winuse/SKILL.md, the skill's core, as it was when the
// binary was built, so `winuse skill` always matches the binary's version.
//
//go:embed skills/winuse/SKILL.md
var Skill string

// SkillFiles is the whole skill folder as it was when the binary was
// built: SKILL.md and its topics, references/<topic>.md, which
// `winuse skill <topic>` prints and `winuse skill install` writes.
//
//go:embed skills/winuse
var SkillFiles embed.FS
