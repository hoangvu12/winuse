# 01: Clipboard commands

**What to build:** `clipboard-get [--format text|image|files] [--out <path>]`, `clipboard-set <text> | --image <png> | --file <path>...`, `clipboard-clear`. agent-desktop has all three (`../../headless-gaps/agent-desktop-parity.md` section 1, Clipboard rows).

- OpenClipboard needs no foreground, so all three are Headless.
- Copy agent-desktop's hung-owner guard: OpenClipboard retries for a short bounded time; a delay-rendered format whose owner does not answer is abandoned on a worker with a timeout, never blocking the helper.
- Text as CF_UNICODETEXT; image as CF_DIB/PNG written to `--out` (default a temp file, path printed); files as CF_HDROP paths, one per line.
- Output for get: the content (text), or the file path. A secret in the clipboard is printed as-is, as agent-desktop does.

These are not aimed at a Window. CONTEXT.md says every Action is aimed at exactly one Window (`CONTEXT.md:13-14,49-51`). Add the term **Desktop command**: not an Action, no Agent Cursor, not a Batch step, serialized on the action gate.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Win32 clipboard layer with the hung-owner guard
- [ ] Three commands in CLI, protocol and helper
- [ ] Offline tests: format selection, owner timeout
- [ ] Opt-in e2e: set then get text, image and files
- [ ] CONTEXT.md "Desktop command"; SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop: get, set and clear for text, image and files, with the hung-owner guard. No clipboard-history exclusion formats and no "ask the user first" rule in the skill (both were winuse extras; the user declined them). The CONTEXT.md term **Desktop command** is accepted (not an Action, no Agent Cursor, not a Batch step, serialized on the action gate); this ticket adds it.
