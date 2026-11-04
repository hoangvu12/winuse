# 05: Output budgets for the tree

Status: ready-for-human
Type: task
Branch: speed-output (worktree `winuse-wt/speed-output`)
From: `.scratch/research-speed/report.md`, recommendation 5 (`notes-local-perf.md` §5, `notes-cli.md` gap 7)

## Ask

- Allow `--interactive` and `--depth` on a full `get-window-state`, not only with `--root`.
- Add `find --limit`, `--max-chars` (cut with a clear marker and how to get the rest) and `--out <file>` (write the state to a file and print the path plus a short summary, as playwright-cli does).
- Fold unnamed containers and drop redundant IDs where safe. Keep Element Indexes stable, never hide what an agent needs, and propose any default change with numbers instead of flipping it.

## What was built

All of these are views. They shape the printed text only. The walk, its caps and the stored Window State are a plain full read's, so every printed index is the full read's and an Element left out still resolves (`internal/wstate/view.go`, `internal/helper/views.go`, ADR 0004 amendment).

```
winuse get-window-state <window> [--interactive] [--depth N] [--compact] [--max-chars N | --out <file>] [--screenshot]
winuse find <window> ... [--first | --limit N | --count] [--max-chars N]
```

| Flag | Prints |
|---|---|
| `--interactive` | Only `Element.Interactive()` lines (the same rule `find --state interactive` uses), plus the root and any line with a cut mark |
| `--depth N` | The first N levels below the Window; `(+N children)` where it stops, as the skeleton does |
| `--compact` | Folds an Element whose line would carry only its index and role (no name, state, value, description, toggle, Secondary Action or ID). Drops the `ID:`s several Elements share, which `id:` refuses as ambiguous anyway. An unnamed container with a unique ID keeps its line, because `--root id:<ID>` may need it. |
| `--max-chars N` | Cuts whole Element lines so the output stays within N characters; never cuts the header or the tail (focused Element, walk notes) |
| `--out <file>` | Writes the view to the file (atomic) and prints the header, `Window State: <path> (N Elements, K interactive; C characters; ...)` and the tail |
| `find --limit N` | The first N matches, with `Note: these are the first N of M matches (--limit N); ...`; `--first` is `--limit 1` |
| `find --max-chars N` | As above, for find's lines |

Each printed Element is indented one level below its nearest printed ancestor. A view that leaves Elements out ends the tail with one note:

```
Note: --interactive left out 120 of the 412 Elements; their Element Indexes still work
Note: --depth 3 left out 200 of the 412 Elements; their Element Indexes still work; read a part whole with winuse get-window-state 1234 --root <element>
Note: --max-chars 8000 stopped the output after Element 211: 200 more Element lines (from Element 212 on) are not printed; their Element Indexes still work. See them with winuse get-window-state 1234 --root <element>, narrow the read with --interactive, --compact or --depth, or write it all with --out <file>
```

Other details:

- A Tab target takes every view. Its notes leave out the `--root` hint, because a Tab target takes no `--root`.
- `--root` keeps its own `--depth`/`--interactive` and refuses `--compact`, `--max-chars` and `--out`.
- `--skeleton` refuses `--depth`.
- `--out` with `--max-chars` is refused.

## Measurements

The data is 14 unique real Window States re-extracted read-only from local Claude Code transcripts (670 Element lines: Calculator, Settings, Word, a Brave Page through CDP, the fixture Apps; up to 341 Elements). Six rendered repo fixtures were measured too. The states were parsed back to trees, and all 20 round-trip byte for byte through `Render`. Each view was then rendered by the real code, and tokens were counted with tiktoken o200k_base as a proxy for Claude's tokenizer, as in the research. The note each view adds is included. The copies were deleted after the run.

| View | 14 real states | 6 fixtures | Largest state (4,267 tokens) |
|---|---|---|---|
| `--interactive` | **−36.5%** | −16.5% | −57% (1,840) |
| `--compact` | **−11.4%** | −0.1% | −18% (3,483) |
| fold bare lines only | −5.8% | −1.0% | |
| drop shared IDs only | −5.3% | −0.4% | |
| `--interactive --compact` | **−38.1%** | −16.8% | −59% (1,747) |
| `--depth 3` | −67.5% | −0.8% | |

- Per state, `--interactive` ranges from −57% to +7%. On a 5–10 Element state the note costs more than it saves.
- `--compact` ranges from −21% to +11%.
- The research's figures (−41%, −17.5%) were measured without the notes, and dropped unnamed containers even when they had an ID. This build keeps those containers, so `--compact` saves less and stays safe for `--root id:`.
- Time: rendering costs nothing measurable (the research measured 2.3 ms per 3,000 Elements for `Render`; a view is one more pass).

## Proposed defaults (not applied)

1. **Drop shared IDs by default: −5.3% on real states, no loss.** An ID that several Elements share cannot be an `id:` reference (it is refused as ambiguous), so the line carries nothing an agent can use. The cost is one Codex-parity line difference. This is the safest default change.
2. **Fold bare lines by default: −5.8%.** It changes indentation, so trees no longer match Codex's byte for byte. It is safe for indexes, but I'd ship it only together with (1) as `--compact` becoming the default, after a Task Suite A/B (rec 9).
3. **`--interactive` stays opt-in.** It hides static text (a Calculator's display, a dialog's message), which an agent often needs to read. A good skill line: "on a dense Window, start with `--interactive --compact`".
4. **A default `--max-chars` near the Harness's cap** (Claude Code cuts Bash output at about 30,000 characters). A 3,000-Element read is about 40k tokens, and today the Harness truncates it silently in the middle. With a default of, say, 28,000, winuse would cut it at a line boundary with a note instead. I'd propose this one after the user decides, since it changes what a plain read prints.

## Tests

- `internal/wstate/view_test.go`: each view's exact output, and indexes kept on the recorded fixtures (every printed line is the full read's line for that index). It also checks that the `--max-chars` output stays within the cap, keeps the notes, and has a tiny cap that still prints the header and tail.
- `internal/helper/views_test.go`: full read views, `--max-chars`, `--out` (file contents and summary), `find --limit`/`--max-chars`/refusals.
- `internal/helper/find_test.go`: refused combinations.
- `internal/helper/tabtarget_test.go`: a Tab target takes the views.
- `internal/cli/find_test.go`: parsing, `--out` resolved to an absolute path.
- `internal/uia/region_test.go`: `pickMatches` with a limit counts every match.

## Comments
