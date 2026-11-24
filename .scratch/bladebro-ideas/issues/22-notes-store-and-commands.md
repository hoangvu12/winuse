# 22: Notes: the store and `winuse note`

Status: ready-for-agent
Type: task
From: ADR 0024 (`docs/adr/0024-notes.md`), ticket 16

## Ask

- **The store.** `%LOCALAPPDATA%\winuse\notes.json` is owned by the helper,
  which takes one lock for writes, writes to a temp file and renames it.
  - Each Note holds its key, text, date and author (`WINUSE_AGENT`, or the
    Harness name the helper already knows).
  - A corrupt file is moved aside (`notes.json.bad`) and the command says
    so. Never silently drop it.
- **Keys** (ADR 0024):
  - **Window ID:** a browser Window → its Page's registrable domain;
    `--app` → its App. Any other Window → its App.
  - **URL or domain:** the registrable domain. `localhost` and IP addresses
    keep their port.
  - **Exe name:** lower-cased file name.
  - **Free-form text:** refused, and the hint lists the Windows whose App
    name or title matches.
  - Decide the Public Suffix List source (`golang.org/x/net/publicsuffix`
    or a small built-in rule), and write the choice in the ticket.
    `AGENTS.md` may require asking before adding a dependency.
- **Commands:**
  - `winuse note add <target> [--app] <text> | --stdin | --file <path>`
    - one line, about 200 characters at most. A longer text is refused,
      with nothing stored.
    - an exact duplicate on the same key is refused.
  - `winuse note list [<target>]`
    - lists every Note, numbered, with author and age. With no target, it
      lists every key and its count.
  - `winuse note remove <target> <n>` and `winuse note clear <target>`.
  - `--json` on all of them.
- **Help.** `winuse help note`, and an entry in `commands.md`.
- **Tests:**
  - key normalisation: subdomains, ports, IPs, `file://`, `chrome://`, an
    exe with a path, mixed case;
  - concurrent adds from two Agents;
  - corrupt-file handling.
