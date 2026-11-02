# 15: `winuse skill` prints the embedded skill

**What to build:** `winuse skill` prints the SKILL.md embedded in the binary (`go:embed skills/winuse/SKILL.md`); `winuse skill install <dir>` writes it to a skills folder. agent-desktop has `skills get` (research section 1, System rows).

- The text always matches the binary's version, so an agent can re-read it after an upgrade.
- `install.ps1` can call `winuse skill install` instead of downloading the file separately.
- `go:embed` cannot reach outside the package directory: embed from a small package at the repo root or move the copy step into `go generate`; a test checks the embedded copy equals `skills/winuse/SKILL.md`.

**Blocked by:** none

**Status:** done

- [x] Embed and the two subcommands
- [x] Equality test
- [x] install.ps1 uses it
- [x] README and SKILL.md mention

## Comments

### 2026-11-02: landed

- `skill.go` (new root package `github.com/hoangvu12/winuse`): `//go:embed skills/winuse/SKILL.md` into `winuse.Skill`. The embed lives at the repo root because go:embed cannot reach up from `internal/skill` (a test-only package guarding the shipped files), and a Go file inside `skills/` would sit next to what `npx skills add` scans. No `go generate` copy step, so there is no second copy to drift.
- `internal/cli/skill.go`: `winuse skill` prints the embedded SKILL.md verbatim; `winuse skill install <dir>` creates `<dir>\winuse\`, writes (or replaces) `SKILL.md` there and prints its path. Neither talks to the helper or checks the input desktop, so both work with no desktop. Wrong arguments exit 2 with `usage: winuse skill | winuse skill install <skills dir>`; a filesystem failure (or an empty dir) exits 1 with `winuse: installing the skill: ...`. Dispatched from `cli.Main`; the usage text lists `skill` and `skill install <dir>`. `internal/helper/parity_test.go` lists `skill` as a CLI-only command next to `version`.
- Equality test: `internal/skill/skill_test.go` `TestEmbeddedSkillMatchesTheShippedFile` compares `winuse.Skill` with `skills/winuse/SKILL.md` byte for byte. CLI tests in `internal/cli/skill_test.go`: print equals the embed, install into a temp dir (fresh and over an older copy), install failure exits 1, usage errors exit 2, usage text.
- `install.ps1`: the skill step now always runs `& $exe skill install ~\.agents\skills` (the folder the skills CLI links agents from), so the installed skill matches the installed binary and the step works without Node.js. With Node.js, `npx skills add hoangvu12/winuse -g -y` still runs first to link the skill into every detected agent, and the binary's copy then overwrites the canonical one. `WINUSE_NO_SKILL=1` still skips it, now pointing at `winuse skill install`. Checked with PowerShell's parser only (0 errors); **install.ps1 was not run**, so the npx link-then-overwrite order is untested live (it assumes the skills CLI's default symlink install; a copy install would leave other agents on the `main` copy).
- Docs: SKILL.md's "If winuse is missing" section says to re-read the skill with `winuse skill` after an upgrade and how to `skill install`; its command block lists `skill | skill install <dir>`. README's Install section describes the new skill step and the from-source `skill install`, and the SKILL.md line says `winuse skill` prints the binary's copy. CONTEXT.md and docs/codex-parity.md were not changed (the ticket names no term or Codex row).
- `go build ./...`, `go vet ./...`, `go test ./...` and `go vet -tags e2e ./test/e2e` pass. No e2e test was added or run.
