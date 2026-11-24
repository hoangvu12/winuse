# 22: Notes: the store and `winuse note`

Status: resolved
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
  - **Public Suffix List source: decided 2026-11-24 by the user — add
    `golang.org/x/net/publicsuffix`** as a new Go dependency (correct for
    every suffix, including `co.uk`, `github.io`, `vercel.app`).
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

## Answer

Built on branch `agent/bladebro-ideas` (merge 9a9043f; review fixes in 081a150). `%LOCALAPPDATA%\winuse\notes.json` store (`internal/notes`), keys via `golang.org/x/net/publicsuffix` (user decision) with hosts converted to ASCII by `x/net/idna`; `winuse note add|list|remove|clear` with `--json`; corrupt files moved to `.bad`, `.bad.2`, …
