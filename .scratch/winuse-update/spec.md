# winuse update: install a given release, safely, from winuse itself

Status: done (ADR 0022; released in v0.4.0)

Changed while building it: step 3 needs no helper stop at all. A running
exe can be renamed, so the swap leaves a running helper in place, and the
helper's existing stale exit (server.go) drains its in-flight requests at
the next command and hands over to the new build. `--force` was dropped,
and `install.ps1` now swaps the same way instead of `Stop-Process -Force`.
The `Zone.Identifier` step was dropped too: Go's download carries no Mark
of the Web.

Pairs with hoangvu12/fleet#32 (Programs), which pins winuse per Machine
and calls `winuse update --to {version}` from `fleet sync`.

## Problem

Upgrading winuse today means rerunning `install.ps1` by hand on each
Machine. Nothing pins a version, and nothing keeps the skill text and the
binary in step:

- Fleet carries the skill (`~/fleet/skills/universal/winuse`, linked from
  `~\.agents\skills\winuse` and `~\.claude\skills\winuse`) and follows
  `main`. `main` is 45 commits past v0.3.0, and 12 of them change the
  skill, so a fleet update would describe flags the installed 0.3.0
  doesn't have.
- `install.ps1` runs `winuse skill install ~\.agents\skills`. On a Fleet
  Machine that folder's `winuse` is a link into `~/fleet`, so the install
  writes into the Fleet repo and shows up there as uncommitted edits.
- Only the latest release can be installed (`releases/latest/download`),
  so there is no way to pin a version.

## Decisions so far (with the user, 2026-11-08)

- No silent auto-update. Swapping the binary stops the helper, which would
  break other Agents' commands (ADR 0014) and change behaviour mid-task.
- Updates happen when someone runs `winuse update`, or when `fleet sync`
  runs it because the pin moved.
- The "newer version available" note is deferred. On Fleet Machines,
  Fleet decides the version. If it is built later, it must never block a
  command: the helper checks in the background at most once a day, caches
  the answer, commands only read the cache, and failures stay silent.

## Behaviour

`winuse update [--to <version>] [--no-skill]`

1. Resolve the target: `--to 0.4.0` (or `v0.4.0`), or the latest release.
   If it equals `version.Version`, print `winuse 0.4.0 is already
   installed` and exit 0 with no download. Downgrades are allowed.
2. Download `https://github.com/hoangvu12/winuse/releases/download/v<v>/winuse_windows_<arch>.exe`
   to a temporary file next to the installed exe. No `gh` needed (the repo
   is public). Verify it against the release's `SHA256SUMS` (issue 02).
   An unknown version or a failed download is an error, and the installed
   exe stays untouched.
3. Stop the helper. If another Agent's command is in flight, wait for it,
   bounded (proposed 30 s; `--force` stops it anyway). Past the bound, exit
   1 and leave everything as it was, with a line naming what is still
   running.
4. Swap the exe. A running exe can't be overwritten on Windows, but it can
   be renamed. Rename `winuse.exe` to `winuse.exe.old`, move the new file
   in, and run `Unblock-File`'s equivalent (remove the `Zone.Identifier`
   stream). The next winuse start deletes a leftover `.old`. If any step
   fails, roll back the rename.
5. The skill: unless `--no-skill`, run the new binary's `skill install`
   for each folder the installer writes (`~\.agents\skills`,
   `~\.claude\skills` or `CLAUDE_CONFIG_DIR\skills`). Skip a folder whose
   `winuse` entry is a link or junction, with
   `skipped ~\.agents\skills\winuse: it links elsewhere (Fleet manages it)`.
   That folder belongs to whoever made the link. Fleet passes
   `--no-skill`.
6. Print `winuse 0.3.0 -> 0.4.0` and, unless the skill was skipped, which
   folders were rewritten. `--json` follows the usual one-line shape.

`install.ps1` gets the same link check for the skill folders and a
`WINUSE_VERSION` environment variable, so a first install can be pinned
too.

## Out of scope

- The update-available note (see Decisions).
- Updating the browser extension: it already self-updates through the
  bridge (`internal/browser/extension.go`).
- Signing the binary.

## Issues

- 01 `winuse update` (steps 1 to 6) and the `install.ps1` changes
- 02 release.yml publishes `SHA256SUMS`
