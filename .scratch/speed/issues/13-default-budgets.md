# 13: Two output budgets become the default

Status: ready-for-human
Type: task
Branch: speed-defaults (worktree `winuse-wt/speed-defaults`, off main 34512dd)
From: `05-output-budgets.md`, proposed defaults 1 and 4, approved by the user

## Ask

Make two budgets the default on get-window-state (full reads, Tab targets) and wherever the same view applies:

1. Drop the AutomationIds two or more Elements of the read share (`id:` refuses those). Unique IDs stay. Folding stays opt-in under `--compact`.
2. A default `--max-chars` of about 28000, under Claude Code's ~30k Bash cap. `--max-chars 0` lifts it, `--out` still writes everything uncut, and an explicit `--max-chars N` overrides it.

Also add an escape hatch that prints the old output byte for byte, and note the Codex-parity impact.

## What was built

- **Shared IDs** (`wstate.SharedIDs`, `Element.WithoutSharedID`):
  - Every `RenderView` except `Full` leaves them out, the focused Element's sentence included.
  - `find` leaves out the IDs shared within its walk (`uia.FindResult.SharedIDs`). On a browser Window it uses the fresh read's shared IDs.
  - `--root` leaves out the IDs shared within the region walk, or within the stored Window State for a Page region.
- **Default cap** (`defaultMaxChars = 28000`, `internal/helper/views.go`):
  - It applies to a full read, `--skeleton`, a Tab target, `--root` and `find`.
  - The `--max-chars` note is reserved inside the cap. With `--screenshot`, another 300 characters are kept for the Screenshot line, so the whole output stays at or under 28000. That leaves about 2000 for the shell's own lines and stderr under the 30000 cap.
  - `max_chars` is now a pointer on the wire: absent means the default, 0 means no cap.
  - `--root` now takes `--max-chars`; it still refuses `--compact` and `--out`.
- **`--full`** (get-window-state and find): prints `wstate.Render`, Codex's text byte for byte, with every ID and no cap.
  - It refuses `--interactive`, `--depth`, `--compact` and `--max-chars` on a full read. With `--root`, `--depth` and `--interactive` still shape the walk.
  - `--out --full` writes Codex's text to the file.
  - I chose this over `--max-chars 0 --keep-ids` because it is one flag that means "as before", and it leaves nothing to combine wrongly.
- **`--out`**: writes the view uncut whatever the cap. An explicit nonzero `--max-chars` with `--out` is still refused; `--max-chars 0` with it is fine.
- **Docs**:
  - help (`internal/cli/help.go`: the usage lines, text and examples)
  - the skill: the core paragraph in `SKILL.md`, and `references/reading.md`
  - the ADR 0004 addendum
  - `docs/codex-parity.md` (tree format: shared IDs and the cap differ from Codex; `--full` matches it byte for byte)

## Effect on the 14 real Window States

These are the same 14 unique transcript states as issue 05, re-extracted read-only and deleted after the run. Tokens are counted with o200k as a proxy.

| | Old default (Codex text) | New default | Change |
|---|---|---|---|
| Tokens | 8,865 | 8,395 | **−5.3%** |
| Characters | 34,793 | 33,133 | **−4.8%** |

- Six of the 14 change, by −1.3% to −9.0% each. The other eight have no shared ID and print byte for byte as before.
- The largest is 17,260 characters, so the cap cuts none of them. It only matters on dense Windows: at the 3000-Element cap a read is about 40k tokens.
- The 6 repo fixtures: −0.4% tokens.

## Limits

- The cap counts runes. Claude Code counts UTF-16 units, so text full of emoji counts slightly more there; the 2000 margin covers it.
- Under `--json` the escaped text is longer than the cap.

## Tests

- `wstate`: `TestRenderViewFullIsRender` and `TestRenderViewDropsSharedIDs`, plus the view tests updated to the default.
- `helper`: `TestDefaultReadBudget` checks a 1,200-Element tree:
  - the cap lands just under 28000, with the notes and unique IDs kept and the shared ID dropped
  - `--max-chars 0` prints uncut
  - `--full` matches `Render` byte for byte
  - `--out` and `--out --full` write the right text
  - with `--screenshot`, the output stays under the cap
- `helper`: `TestFindDropsSharedIDs`, and refusals for `--full` combinations.
- `cli`: parsing of `--full`, `--max-chars 0`, the absent default, and `--out --max-chars 0`.
- `pagecut_test`'s 3000-Element fallback golden now asks for `--full`, since it pins the whole tree.

## Comments
