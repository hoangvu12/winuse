# 04: Batch: check step values before the first step

Status: ready-for-agent
Type: task
From: `map.md`; bladebro `bb/src/mcp/server/validate.rs:8-119`, which validates every step of a plan, including unchosen branches, before the first effect

## Problem

`internal/helper/batch.go:95-105` prepares every step before any input is
sent, but only checks:
- the step kind;
- that the arguments are well-formed JSON;
- headed or posted flags;
- Blocked Combos;
- step Windows.

Its own comment says value-level validation ("key names, secondary actions,
element availability") runs at the step's turn. So a typo in step 4, such as
a bad key name, an unknown secondary action, a malformed `@x,y`, a wrong
`wait` kind or a timeout out of range, fails after steps 1–3 have already
acted.

## Ask

- **First, list them.** Go through every per-step check and sort it into one
  of two groups, putting the list in the ticket:
  - **static:** it depends only on the arguments;
  - **live:** it depends on the Window or the Page.
- **Run every static check in the prepare loop,** so a Batch with a bad
  value never acts. Examples:
  - key and chord parsing (`keys.Parse`);
  - secondary action names;
  - Element Reference syntax (`name:`, `id:`, `@x,y`, an index);
  - `wait` kind and arguments;
  - timeout bounds;
  - scroll direction and steps;
  - `--count`, `--button` and `--modifiers` values;
  - file paths for `--file` and `upload`, which must exist.
- **The failure names the step** and says nothing was sent, as it does
  today.
- **Live checks stay at the step's turn,** e.g. whether an Element exists or
  is enabled.
- **Tests.** One table test per check: a Batch whose last step is bad sends
  nothing.
