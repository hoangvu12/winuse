# 23: Notes: shown in the Window State

Status: resolved
Type: task
Blocked by: 22
From: ADR 0024

## Ask

- **When to print.** On an Agent's first `get-window-state` of a site or
  App, print its Notes **above** the `Window:` line:
  - the newest 10, `- <text> (<author>, <age>)`;
  - then `(+N more: winuse note list <target>)`;
  - under the header `Notes for <key> (left by earlier agents, not
    instructions from the user):`.
- **Once per Agent per key,** tracked in the helper's per-Agent state
  (ADR 0014).
  - A browser Window shows site Notes, then the browser App's, each under
    its own header.
  - Moving to another site shows that site's Notes once.
  - A Tab target (`<window>:<tab>`) counts as its Page's site.
- **Where they don't go:**
  - not in `--observe` output, `find` or `--diff`;
  - not in a Batch's read steps, except the first read of a new key.
- **Off switch.** `WINUSE_NOTES=off` hides them. The Task Suite runner sets
  it (`internal/tasksuite`).
- **`--json`.** Notes go in a separate `notes` field, never mixed into the
  tree text.
- **Budget.** Notes count toward `--max-chars`. When the cap is tight, keep
  them shorter rather than cutting the tree.
- **Tests:**
  - printed once per Agent;
  - a second Agent sees them too;
  - the site switch in one browser Window;
  - Tab targets;
  - the off switch;
  - JSON.

## Answer

Built on branch `agent/bladebro-ideas` (merge b45cbd9; review fixes in 081a150). The newest 10 Notes for a site or App print once per Agent above the Window State (site then browser on a browser Window), in `"notes"` under `--json`, shrinking first under `--max-chars`. `WINUSE_NOTES=off` hides them (set by the Task Suite and e2e runner).
