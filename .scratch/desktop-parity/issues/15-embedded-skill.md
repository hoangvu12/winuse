# 15: `winuse skill` prints the embedded skill

**What to build:** `winuse skill` prints the SKILL.md embedded in the binary (`go:embed skills/winuse/SKILL.md`); `winuse skill install <dir>` writes it to a skills folder. agent-desktop has `skills get` (research section 1, System rows).

- The text always matches the binary's version, so an agent can re-read it after an upgrade.
- `install.ps1` can call `winuse skill install` instead of downloading the file separately.
- `go:embed` cannot reach outside the package directory: embed from a small package at the repo root or move the copy step into `go generate`; a test checks the embedded copy equals `skills/winuse/SKILL.md`.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Embed and the two subcommands
- [ ] Equality test
- [ ] install.ps1 uses it
- [ ] README and SKILL.md mention
