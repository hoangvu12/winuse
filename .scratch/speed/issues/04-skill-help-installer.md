# 04: A lean skill core, cheap help, and a skill install Claude Code finds

**What to build:** the agent-UX items of the speed research (`.scratch/research-speed/report.md` recs 3, 4 and 10a, `notes-skills.md`, `notes-cli.md` gap list).

1. **Shrink the skill.** `skills/winuse/SKILL.md` was 1,138 lines, about 15.9k tokens. Claude Code re-attaches only the first 5,000 tokens of a skill after compaction, which cut it near line 385, so Read the result, the Esc stop, Refusals and recovery, and Safety were lost. Make a core of about 200 lines and under 5k tokens, with Safety, Esc and Recover first. Move the rest into topics that the binary serves as `winuse skill <topic>` (as agent-browser's `skills get` does), and list the topics in the core. Add the one-line batch nudge. Nothing may be dropped, only moved, and a test checks that each old section has a home.
2. **Installer bug.** `install.ps1:92-98` wrote the skill only to `~\.agents\skills`, which Claude Code does not read. So a Claude Code user without Node got no skill. Write to every folder a supported harness reads at user level.
3. **Help.** `winuse`, `--help` and a typo each printed the 30.7 KB usage and exited 2 (`cli.go:512, 600`), and `click --help` was "unknown flag". Add:
   - a short index from `winuse`, `winuse help` and `--help`, exiting 0;
   - `winuse help <command>` and `<command> --help`, each printing examples and then the usage;
   - "did you mean" plus the one-line usage for a typo;
   - the full reference at `winuse help all`.
   Fix the cheap items on the CLI gap list.

Out of scope: output formats, perf and the cursor. Another worker is adding the output flags (`--interactive`, `--depth`, `--max-chars`, `--out`, `find --limit`). All help text is now in `internal/cli/help.go`. A new flag goes into its command's block of `Usage`, and a new command also gets an entry in `commandGroups`; `TestEveryCommandHasHelp` checks both ways.

**Status:** done

- [x] Core SKILL.md (Safety, Esc and Recover before the loop) plus 12 topics in `skills/winuse/references/`
- [x] `winuse skill <topic> | list | all`, and `skill install <dir>` writes the whole folder and removes topics an older copy had that this one does not
- [x] Section map test (`TestEveryOldSectionHasAHome`), plus a sentence-level check (`.scratch/speed/preserve_check.py`)
- [x] install.ps1 writes `~\.agents\skills` and `~\.claude\skills` (or `CLAUDE_CONFIG_DIR\skills`)
- [x] Help index, per-command help, `help global`, `help all`, did-you-mean, exit 0 for help
- [x] Global flags (`--json`, `--trace`, `--trace-unredacted`, `--help`/`-h`) accepted after the command outside literal text, by the same rule `--headed` already had
- [x] README, ADR 0001 (amended), ADR 0008, `tasksuite/README.md`, `docs/codex-parity.md`; the Task Suite copies the whole skill folder
- [x] `go vet ./...` and `go test ./...` pass

## Comments

### 2026-11-05: done (branch speed-agent-ux, off dbce051)

**Sizes.** Token counts use the o200k tokenizer as a proxy for Claude's, which runs somewhat higher.

| File | Lines | Bytes | Tokens (o200k) |
|---|---|---|---|
| Old SKILL.md | 1,138 | 61,923 | 15,893 |
| **New core SKILL.md** | **227** | **13,045** | **3,360** |
| references/batch-and-wait.md | 63 | 3,280 | 835 |
| references/browser.md | 145 | 8,563 | 2,152 |
| references/commands.md | 71 | 5,187 | 1,487 |
| references/headed.md | 118 | 6,844 | 1,694 |
| references/input.md | 215 | 11,188 | 2,858 |
| references/multi-agent.md | 32 | 1,783 | 440 |
| references/reading.md | 123 | 6,280 | 1,623 |
| references/setup.md | 36 | 1,840 | 502 |
| references/shell.md | 34 | 2,080 | 525 |
| references/tabs.md | 63 | 3,307 | 923 |
| references/troubleshooting.md | 102 | 5,874 | 1,482 |
| references/windows.md | 138 | 7,667 | 1,952 |

- Every task now loads 3.4k tokens instead of 15.9k (−79%), and the whole core survives compaction.
- A browser task loads the core plus `browser` (plus `tabs`): about 5.5k–6.4k tokens.
- The core plus every topic is 19.8k tokens. That is more than the old file, because the core restates the essentials and every topic carries a title and a "Read when" line.

**Help output** (bytes / o200k tokens; before, every one of these was 30.7 KB, about 5.6k tokens, with exit 2):

| Command | Bytes | Tokens | Exit |
|---|---|---|---|
| `winuse`, `winuse help`, `--help`, `-h` (the index) | 3,581 | 775 | 0 |
| `winuse help click` / `click --help` | 1,415 | 313 | 0 |
| `winuse help get-window-state` | 1,851 | 376 | 0 |
| `winuse help wait` | 2,485 | 497 | 0 |
| `winuse help browser` (every browser form) | 3,049 | 573 | 0 |
| `winuse help global` | 4,074 | 894 | 0 |
| `winuse help all` (the full reference) | 30,420 | 5,586 | 0 |
| A typo, `winuse clik 1 2` (3 lines) | 163 | 47 | 2 |
| `winuse skill list` | 2,146 | 514 | 0 |

**Exit codes.** Help in any form exits 0 and prints on stdout. `winuse` with no arguments is the index with exit 0, like gh, and no longer a usage error. An unknown command or topic exits 2 on stderr. Under `--json`, a typo's line carries the message "unknown command "x"; did you mean y?" and the usage line as its hint.

**The split** (`TestEveryOldSectionHasAHome` holds the map):

| Old sections | Now in |
|---|---|
| Safety, the Esc stop, the loop | The core, word for word |
| Refusals and recovery, Read the result | Short tables in the core; the full text in `troubleshooting` |
| Name an Element | A short form in the core; the full text in `reading` |
| Everything else | One topic each |

- The sentence-level check found 573 of 588 old sentences and code lines word for word.
- The other 15 are deliberate edits:
  - "see X" pointers now name the topic, for example "(see `winuse skill browser`)".
  - The `skill`/`skill install` text describes topics and both skills folders.
  - "Read this whole file before you decide winuse cannot do something" became "Check that table before you decide winuse cannot do something".
- The batch nudge is in the loop: "When you know the next few routine steps (fill a form, then OK), send them as one `winuse batch` rather than a command per turn: each turn costs seconds."
- The frontmatter description is unchanged. The research's richer description (`notes-skills.md` §5.2) changes triggering, so it should wait for the trigger-eval set (§5.5).

**Installer.** Where each harness reads skills at user level, per the docs:

| Harness | User-level folders | Source |
|---|---|---|
| Claude Code | `~/.claude/skills/<name>/SKILL.md` only; on Windows `%USERPROFILE%\.claude`, or `CLAUDE_CONFIG_DIR`. The docs never mention `.agents/skills`. | https://code.claude.com/docs/en/skills.md, https://code.claude.com/docs/en/claude-directory.md |
| GitHub Copilot | `~/.copilot/skills` or `~/.agents/skills` ("For personal skills, shared across projects, create a `~/.copilot/skills` or `~/.agents/skills` directory") | https://docs.github.com/en/copilot/how-tos/use-copilot-agents/cloud-agent/create-skills |
| Codex | `$HOME/.agents/skills` (from a summary of a truncated fetch) | https://learn.chatgpt.com/docs/build-skills |
| Gemini CLI | `~/.gemini/skills` or the `~/.agents/skills` alias | https://geminicli.com/docs/cli/skills/ |
| Cursor | `~/.agents/skills` and `~/.cursor/skills`, plus `~/.claude/skills` and `~/.codex/skills` for compatibility | https://cursor.com/docs/context/skills |

- So `~\.agents\skills` covers everything but Claude Code, and `~\.claude\skills` covers Claude Code. The installer now runs `winuse skill install` into both, and into `CLAUDE_CONFIG_DIR\skills` when that is set. `npx skills` still runs first when Node is present.
- Claude Code follows a symlinked skill folder. That is `npx skills`' layout (`~/.claude/skills/winuse -> ~/.agents/skills/winuse`), so the install writes through it.
- Claude Code watches `~/.claude/skills` for changes. If that folder did not exist when a session started, the docs say to run `/reload-skills`.
- install.ps1 was only parsed (0 errors) and never run: it downloads a release and stops running winuse processes.

**Global flags anywhere (gap list 4 and 9).**
- `headed.go`'s literal-text rule is now generic: a flag in type-text's text, set-value's value, clipboard-set's text, batch step text or a launch-app `--arg`/`--cwd`/`--env` value stays literal text. `hoistGlobalFlags` moves `--json`, `--trace <file>`, `--trace-unredacted` and `--help`/`-h` from after the command to before it, outside that literal text.
- A bare `--trace` at the end is left in place for the command to refuse; moved before the command, it would take the command as its file.
- `--trace` with no file now prints one usage line, not the 30 KB Usage.

**Left for later** (not small or not in this scope):
- Did-you-mean for a command's unknown flags. Each command's parser prints its own error, so this needs a shared parser.
- Gap 5: set error codes at the failure site.
- Gap 8: distinct exit codes for stale, needs `--headed`, busy and delivered-unverified.
- Gap 10: `--dry-run`.
- Gap 6: quote the stale line in the stale error.
- Gap 4's `"v":1` and a structured `data` field in `--json`.
- The richer frontmatter description, with trigger evals.
- An A/B Task Suite run comparing the old and new skill (rec 9).
- A `.claude-plugin/marketplace.json`.
- `~\.copilot\skills`: Copilot already reads `~\.agents\skills`.

**Incident (no app touched).**
- While checking the literal-text cases, I ran `type-text 1 --help` and `set-value 1 2 --help` from a %TEMP% build. They are real Actions, and correctly so: the text is literal.
- That build started its own helper from `%TEMP%\wu-speed\winuse.exe`. Both requests failed with "no open Window with ID 1", so nothing was sent to any app.
- The installed `winuse.exe` process (PID 26168) kept its start time, so it was not restarted. I stopped the temp-build helper by its PID once I saw it. From then on I ran only help, skill, version and usage-error paths, and covered the literal-text cases with unit tests (`TestHoistGlobalFlags`).
- Correction (orchestrator): the temp build's helper did make the installed helper exit, as ticket 25 restarts a helper of another build; PID 26168 was another winuse process. The orchestrator restarted the helper. The rule since: run no non-installed winuse command that reaches the helper.

### 2026-11-05: fix, skill install through a junction (on speed-all, after main 62868e1)

**Bug.** `npx skills` links `~\.claude\skills\winuse` to `~\.agents\skills\winuse` with a junction. Through that layout, `winuse skill install "$env:USERPROFILE\.claude\skills"` failed with `mkdir ...\.claude\skills\winuse: Cannot create a file when that file already exists.` So install.ps1's second install failed.

**Cause** (probed in a temp dir with Go 1.27.1):
- `os.Lstat` reports a junction as an irregular file, not a directory. `os.Stat` and opening a path through one fail with "The path cannot be traversed because it contains an untrusted mount point". So `MkdirAll` tries to create it.
- `filepath.EvalSymlinks` returns the junction's path unchanged. Only `os.Readlink` gives its target.

**Fix.**
- `installSkill` now resolves every existing junction or symlink along `<dir>\winuse` with `os.Readlink` (`followLinks`), resolving relative targets against the link's folder, at most 32 hops.
- It writes into the target and never through the link. The link is left in place, and the printed path is still `<dir>\winuse\SKILL.md`.
- Installing into both folders writes the one target folder twice, which is fine.
- A reparse point that is no link (a cloud-files placeholder) is kept as it is.

**install.ps1.**
- Each folder's install runs in its own try/catch, with its stderr captured. A failed folder is reported in yellow with its reason and its `winuse skill install` retry, and the other folder is still written.
- The success line lists only the folders that installed. `npx skills add` gets the same try/catch.
- Checked with `installer_loop_check.ps1` (session scratchpad, not committed): it extracts install.ps1's own loop and runs it in Windows PowerShell 5.1 against temp folders only. The folders were `.agents\skills`, a `.claude\skills` whose `winuse` is a junction to it, and a folder blocked by a file. The blocked one was reported, the other two installed (12 topics), the junction was kept, and the script went on.

**Tests** (`internal/cli/skilllink_test.go`):
- Installing through a `winuse` junction, in the installer's order and again in either order; a stale topic in the target is removed and the junction is kept.
- Installing into a skills folder that is itself a junction.
- Installing through a relative directory symlink (skipped where symlinks need a privilege).
- A plain path is unchanged.

The three install tests failed with the reported error when `followLinks` was bypassed.
