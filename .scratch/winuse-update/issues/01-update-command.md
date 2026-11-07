# 01: `winuse update [--to <version>] [--no-skill]`

Status: done

**Done.** `internal/cli/update.go` and its tests (`update_test.go`: a fake
release server, the latest and `--to`, an older release without SHA256SUMS,
the same version with no request, an unknown version, a checksum mismatch
or missing line, a wrong version inside, usage, the npx-skills, Fleet and
`--no-skill` skill layouts, and the swap rollback). `install.ps1` gets
`WINUSE_VERSION`, the rename-swap and the linked-skill skip. ADR 0022,
README, help, `references/setup.md` and the SKILL.md install line are
updated. Live: a built 0.4.0 in a temp folder ran
`update --to 0.3.0 --no-skill` against GitHub and swapped itself while
running (its own image stayed as `winuse.exe.old-*`, which the next update
removes).

Spec "Behaviour", steps 1 to 6, plus the `install.ps1` changes.

## To do

- CLI command `update` (cli-only, like `version` and `skill`; add it to
  `cliOnlyCommands` in the parity test) with `--to`, `--no-skill`,
  `--force`, `--json`, and a help entry.
- Resolve latest through the GitHub releases API, unauthenticated, with a
  short timeout. `--to` skips the lookup.
- Download to a temp file in the exe's folder and verify it against
  `SHA256SUMS`.
- Stop the helper and wait for in-flight commands, bounded. `--force`
  skips the wait.
- Rename-swap with rollback. A leftover `winuse.exe.old` is deleted at
  start-up.
- Rewrite the skill folders, skipping any whose `winuse` entry is a link
  or junction.
- `install.ps1`: `WINUSE_VERSION` picks `releases/download/v<v>/...`
  instead of latest, and the skill step skips linked folders the same way.
- `references/setup.md` and the help text: `winuse update` replaces
  "rerun the installer"; mention `--to` and Fleet.

## Tests

- Same version: no network, exit 0.
- Unknown version or a 404: exit 1, exe untouched.
- Checksum mismatch: exit 1, exe untouched.
- Swap failure after the rename: rolled back.
- Skill folder that is a junction: skipped with the message, and a plain
  folder next to it is still written.
- `--no-skill`: no folder touched.
- Use a fake release server through an env override (as the extension
  self-update tests fake their registry).
