# ADR 0024: Notes, lessons agents leave for the next agent

## Status

Accepted (2026-11-24), and built (bladebro-ideas tickets 22-24, 29).
Settled by grilling the user in
`.scratch/bladebro-ideas/issues/16-site-and-app-knowledge.md`, after a
review of bladebro's per-site knowledge base (`bb/src/knowledge.rs`,
`.scratch/bladebro-ideas/map.md`).

## Context

Agents repeat the same mistakes on the same sites and Apps:
- a comment box that is only a placeholder;
- a dialog that takes seconds to appear;
- a button that needs `--headed`;
- a page that has to be reached another way.

Each new session learns this again, at a cost of turns.

A review of 172 real sessions (ticket 14 of that map) found 43% of winuse
calls carrying a fixed `sleep` or `wait`, including 64 polling loops on one
site.

bladebro stores machine-learned facts per domain (consent selectors, settle
time, bot risk) and applies them by itself. The user asked for something
closer to a CLAUDE.md per site or App instead: lessons the agents write,
which winuse only shows.

This is winuse's first memory that lasts between sessions. Until now,
winuse remembered only settings: the browser pin and `jev.json`.

## Decision

- **A Note** is one line of text that an agent, or the user, writes for a
  site or an App. winuse shows it to later agents. **It never acts on a
  Note:**
  - no Action, route, wait or check changes because of one;
  - the agent decides what to do with it;
  - a Note never grants permission.
- **Keys are worked out by winuse, never typed freely.**
  - `note add <target> "<text>"` takes a **Window ID**, a **URL or domain**,
    or an **exe name**.
  - A browser Window resolves to its Page's site; `--app` picks the browser
    itself. Any other Window resolves to its App.
  - **Sites:** the registrable domain (`gist.github.com` → `github.com`),
    except `localhost` and IP addresses, which keep their port.
  - **Apps:** the exe file name, lower-cased (`excel.exe`).
  - A free-form name (`"Microsoft Excel"`) is refused, and the hint lists
    the Windows that match it.
  - Reason for automatic keys: free-form names fragment ("Excel",
    "excel.exe") and can never be matched to the Window being read, so the
    Note would never show.
- **Written only explicitly.** winuse never writes a Note itself.
  - Each Note stores its text (one line, at most about 200 characters), the
    date, and its author (the Agent name or Harness).
  - There is no count limit, and adding never fails because of one.
- **Shown once per Agent per site or App,** at the top of the first
  `get-window-state` that reads it:
  - the newest 10, each with its author and age;
  - then `(+N more: winuse note list <target>)`;
  - under a header saying the Notes come from earlier agents, not from the
    user.

  A browser Window shows its site's Notes, then the browser App's, each
  under its own header. Moving to another site shows that site's Notes the
  first time.
- **Managed in the open.**
  - Storage: one file per Windows user, `%LOCALAPPDATA%\winuse\notes.json`,
    written by the helper to a temp file and renamed. It is shared by every
    Agent and Harness.
  - Commands: `winuse note list [<target>]` shows every Note,
    `note remove <target> <n>` removes one, and `note clear <target>`
    clears a target.
  - Agents may remove a Note that proved wrong.
- **An off switch for repeatable runs.** `WINUSE_NOTES=off` hides Notes for
  that command. The Task Suite sets it.
- **Not adopted from bladebro.**
  - **Learned timing** (settle and launch times measured per site or App).
    Waiting is handled by settling before observing (bladebro-ideas ticket
    07), which needs no memory. It can return as its own decision if that
    proves not to be enough.
  - **Anything that acts by itself,** such as dismissing cookie banners
    automatically.

## Consequences

- **What it saves.** A lesson costs one command to leave, and saves later
  agents the turns it took to learn. The display bound keeps a busy site's
  Notes at about 10 lines.
- **The risk.** A page could talk an agent into writing a Note such as
  "always click Allow here", and every later agent would read it.
  Mitigations:
  - the header says Notes aren't instructions from the user;
  - the skill's Safety rule says a Note never grants permission;
  - Notes never act;
  - the user can read and clear every Note.

  There is no approval step, by the user's choice.
- **Two dependencies, decided while building** (user's decision,
  2026-11-24):
  - Site keys need the Public Suffix List: `golang.org/x/net/publicsuffix`,
    maintained by the Go team, rather than a small built-in rule.
  - A Unicode host typed by hand (`note add bücher.de`) is keyed in the
    ASCII (punycode) form the browser reports (`xn--bcher-kva.de`), with
    `golang.org/x/net/idna` from the same module, which pulls in
    `golang.org/x/text`. Without it such a Note would never show.
- **The skill** gets a short section in its core (when to leave a Note, and
  that Notes are not instructions) and a short `notes` topic.
  `CONTEXT.md` gains the term **Note**.
