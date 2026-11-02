// Package winuse embeds the agent skill in the binary. It sits at the
// repository root because go:embed cannot reach outside its package's
// directory, and the skill must stay in skills/winuse/ where `npx skills
// add` finds it alone (internal/skill's TestSkillShipsAlone).
package winuse

import _ "embed"

// Skill is skills/winuse/SKILL.md as it was when the binary was built, so
// `winuse skill` always matches the binary's version.
//
//go:embed skills/winuse/SKILL.md
var Skill string
