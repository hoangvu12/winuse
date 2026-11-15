# ADR 0022: Updates on request, by rename-swap

## Status

Accepted (`.scratch/winuse-update/spec.md`, 2026-11-08), with the user.
Pairs with hoangvu12/fleet#32, which pins winuse per Machine.

## Context

Upgrading meant rerunning `install.ps1`, which stopped every running
winuse process with `Stop-Process -Force` before replacing the exe. With
several Agents on one desktop (ADR 0014), that killed the other Agents'
commands mid-Action. Nothing could install a given version, and nothing
kept the skill text and the binary in step on Machines where Fleet carries
the skill.

## Decision

- **No automatic update.** winuse never upgrades itself, and does not yet
  check for a newer release. A swap changes behaviour, and the user (or
  Fleet's pin) decides when that happens. If an update-available note is
  added later, the helper checks at most once a day in the background,
  caches the answer, commands only read the cache, and failures stay
  silent: it must never delay a command.
- **`winuse update [--to <version>]`** installs the latest release or a
  given one (older ones too), checked against the release's `SHA256SUMS`
  (published from v0.4.0; older releases install with a note) and probed
  with its own `version` before it is put in place.
- **Rename-swap, never a stop.** A running exe can't be overwritten on
  Windows, but it can be renamed. The old `winuse.exe` becomes
  `winuse.exe.old-<random>` and the new one takes its name. The running
  helper keeps going from the renamed file. At the next command it answers
  stale and drains its requests in flight before it exits (server.go), and
  the CLI starts the new one. Leftover `.old-*` files are removed by the
  next update or install. `install.ps1` swaps the same way.
- **Skill copies:** update rewrites, with the new binary's embedded skill,
  only the copies that are already there. A `winuse` entry that links
  somewhere winuse did not write (Fleet's link into its repo) is left
  alone, with a line saying so. `--no-skill` and `WINUSE_NO_SKILL=1` skip
  the skill entirely. `install.ps1` also skips the skill step when
  `~\.agents\skills\winuse` is a link.

## Consequences

- An update never interrupts another Agent. The cost is a stale helper
  that lives until the next command, plus a leftover file.
- Fleet (or any pinning tool) installs a version with
  `WINUSE_VERSION=<v> WINUSE_NO_SKILL=1` and the installer from that tag,
  which works with no winuse installed yet. It moves the skill itself.
