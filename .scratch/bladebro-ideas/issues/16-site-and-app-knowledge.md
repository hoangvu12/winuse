# 16: Per-site and per-App knowledge

Status: resolved
Type: grilling
From: `map.md`; bladebro `bb/src/knowledge.rs`

## What bladebro does

It saves one entry per domain in `~/.blade/knowledge/`:
- **Cookie banner:** the button that dismissed it and which consent
  platform it belongs to, with a confidence score that is applied
  automatically above 0.7.
- **Blocking config:** which resources to block.
- **Settle time:** averaged over visits.
- **Background traffic:** a flag for sites whose network never goes quiet.
- **Bot risk:** a level that decays after clean visits.
- **Repeated menus:** hashes of nav links seen on every page, folded out of
  reads.
- **A visit count.**

Its rules:
- Confidence goes +0.05 on success and −0.15 on failure.
- An entry is deleted when below 0.3 and older than 30 days.
- It learns only from successes.
- Files are written to a temp file and renamed; a corrupt file is deleted
  and rebuilt.

## Candidate facts for winuse

Each would change only timing or hints, never which action runs:
- a site's or App's settle time (feeds 07);
- "the network never goes quiet";
- an App's launch time and splash screen (`launch-app` timeout);
- an App whose tree is thin or slow;
- actions refused Headless in this App (hint `--headed` up front);
- cookie-banner button names (a hint only, never clicked).

## Decisions needed

- Scope and wording of every learned fact.
- Whether it's on by default.
- Where it lives, and whether Agents share it.
- How the user sees and clears it.
- How a learned fact shows up when it's applied.
- What a page or App could do to plant data in it.
- An ADR: this would be winuse's first state that persists across sessions.

## Answer

**Settled (2026-11-24).** Notes only:
- written by agents or the user, never acted on by winuse;
- keys worked out automatically from a Window ID, URL, domain or exe name;
- the newest 10 shown once per Agent per site or App;
- no count limit; `note list` shows them all.

Written up as `docs/adr/0024-notes.md` and the `CONTEXT.md` term **Note**.
Build tickets: 22 (store and commands), 23 (shown in the Window State),
24 (the skill). The rounds below record how the decisions got there; later
rounds replace earlier ones where they conflict.

### Decided, round 1 (2026-11-24)

1. **Two kinds.** The main feature is **notes**: short lessons agents write
   per site or App, like a CLAUDE.md, so future agents don't repeat a
   mistake. The second is a small set of **measured facts** winuse records
   itself: settle time per site and launch time per App only.
2. **Notes never make winuse act.** A note is text shown to the agent, and
   the decision stays with the agent. Measured facts may only change how
   long winuse waits.
3. **Keys.** Sites by registrable domain (`github.com` covers
   `gist.github.com`), except that `localhost` and IP addresses keep their
   port. Apps by exe name, lower-cased (`excel.exe`).
4. **Writing notes.** Notes are written only explicitly, with
   `winuse note add <app|site> "<lesson>"`. winuse never writes them, and
   the user may write them too. The skill says when to write one:
   - after the same mistake twice;
   - after finding a non-obvious trick;
   - when a future agent is likely to get it wrong.

   One line each, written as an instruction, with no secrets and no text
   copied from a page.
5. **Shown once per Agent per App or site,** at the top of
   `get-window-state`, under a header that says the notes come from earlier
   agents.
6. **Planting protection.**
   - The header says the notes are not instructions from the user.
   - The skill's Safety rule says a note never grants permission.
   - Limits: about 10 notes per App or site, about 200 characters each.
   - No approval step for the user.
7. **Storage and management.** One file,
   `%LOCALAPPDATA%\winuse\notes.json`, managed with `winuse note list
   [app|site]`, `note remove <app|site> <n>` and `note clear <app|site>`.
   Agents may remove wrong notes.
8. **Explicit flags win.** A measured fact never overrides an explicit flag
   such as `--timeout`.
9. **Shared.** One store per Windows user, used by every Agent and Harness.

### Decided, round 2 (2026-11-24)

- **Off switch.** `WINUSE_NOTES=off` turns notes off for a command, and the
  task suite sets it. Deleting the file resets everything.
- **Skill.** A section in the core `SKILL.md`, as short and simple as
  possible, plus a short `notes` topic.
- **Records.** ADR 0024 "Notes" and the `CONTEXT.md` term **Note**, then
  build tickets.
### Decided, round 3 (2026-11-24)

- **Browser Windows.** Show site notes and App notes, each under its own
  header (`Notes for github.com …`, `Notes for chrome.exe …`), site first.
- **No storage limit.** Show the newest 10 per site or App, then
  `(+N more: winuse note list <target>)`. `note list <target>` shows every
  note, so agents can look whenever they want. Each note is at most about
  200 characters, one line. Adding a note never fails because of a count.
  - **This replaces round 1's limit of 10 notes per site or App.**
- **Learned timing is dropped.** Ticket 16 is notes only, and the waiting
  problem goes to ticket 07. If 07 isn't enough, learned timing can come
  back as its own ticket.
  - **This replaces round 1's "two kinds" and round 2's off-switch scope.**
    `WINUSE_NOTES=off` now only hides notes.

Evidence that this is worth it: ticket 14 found 43% of real winuse calls
carry a fixed `sleep` or `wait` (about 3.4 h in total), including 64 polling
loops on claude.ai.
