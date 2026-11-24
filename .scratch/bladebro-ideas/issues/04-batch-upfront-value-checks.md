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

## The checks, sorted

Every batchable Action (`protocol.BatchStepKinds`) has an entry in
`stepChecks` (`internal/helper/batchcheck.go`). Each static check is the
command's own front half, which the command still runs when sent alone.
`read`, `extract` and `note` are not batchable, so they have none.

**Static** (the arguments alone; run while the Batch prepares):

- **Any step:** arguments of the wrong JSON type.
- **Element References**, on every step that takes one (`parseElementTarget`):
  - exactly one form;
  - a non-negative index;
  - a non-negative `@x,y`;
  - `name:<text>` or `id:<id>` with text (new; it used to fail only at
    resolution).
- **click:** `--count` 1–3, `--button`, no right or middle double click,
  `--modifiers`.
- **type-text:**
  - text given;
  - `--interval-ms` 0–1000;
  - `--into` without `--post`, and `--into`'s reference.
- **press-key:**
  - the key parses (`keys.Parse`);
  - a Blocked Combo needs `--force`;
  - a posted chord with a modifier can be posted, and is never a Blocked
    Combo.
- **perform-secondary-action:** an action is given, and it is one of
  winuse's Secondary Actions.
- **scroll:**
  - `--modifiers`;
  - Element form: no `--post` or `--modifiers`, a direction, steps 1–50,
    `into-view` without steps;
  - point form: no direction or steps, coordinates, amounts not both zero
    and within ±1,000,000.
- **drag:** `--drop-delay` 0–5000, both ends' references or coordinates.
- **hover:** the reference or coordinates.
- **select-text:** the span (`all` or start and length).
- **invoke-menu:** a path with no empty item.
- **tab-close, tab-switch:** a tab id.
- **navigate, back, forward, reload:** a URL for navigate only, `--wait`'s
  load state.
- **upload:** at least one file, each path absolute, existing and not a
  folder; the reference.
- **dialog:** `accept` or `dismiss`; text only with `accept`.
- **eval:** an expression; `--max` 1 to the limit.
- **wait:**
  - `--timeout` 0–300 s (the upper bound is new in the helper);
  - the kind;
  - selector, text, url and js need a value;
  - `load`'s state;
  - `idle` takes no value and no `--gone`;
  - `--gone` only with selector and text;
  - a pause of 1 ms to 300 s;
  - `window` needs an App or title;
  - `element`'s state, a value only for `value`, and its reference.
- **get-window-state:** the output budgets (`budgetError`), and
  `--screenshot`, `--diff`, `--skeleton`, `--root`, `--depth`,
  `--compact` and `--out` together.
- **find:** one of `--first`/`--limit`/`--count`, the numbers, `--full`
  without `--max-chars`, a known `--state`.
- **activate-window, tab-new, tab-list:** nothing but the Window.

**Live** (the Window or the Page; at the step's turn):

- the Window resolving, including an `in new` step's;
- whether a `name:`/`id:` reference matches;
- whether the Element exists and is enabled, and whether it offers the
  Secondary Action;
- whether there is a Headless route;
- Page or desktop Element, file input or file chooser;
- an open JavaScript dialog, and whether a prompt takes text;
- whether a tab id names a Tab;
- the menu path in the Window's menus;
- whether a pause's length is an open Window's ID;
- desktop `wait text` with only whitespace (on a Page it is valid);
- every read-back.
