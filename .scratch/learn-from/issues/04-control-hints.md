# 04: Hints on unnamed or duplicate controls

Status: ready-for-human
Type: task
Branch: learn-control-hints (worktree `.claude/worktrees/agent-ac2a335d5da81a13d`), off main 03fc3a4
From: `.scratch/learn-from/agent-browser.md` §3.1 (rank 1, agent-browser ab#2578), `.scratch/learn-from/README.md`

## Ask

- **Pages:** a control with no accessible name, or whose name repeats, gets a short hint from data the Page read already has (the DOMSnapshot / clickables pass): an icon class or SVG title, an `<img>` file name, `data-testid`, the text of the nearest row, list item or card. No extra CDP round trips per Element; measure that the read's cost does not grow meaningfully.
- **Native (UIA):** for the same cases, the text of the containing row, list item or data item, from the cached read (no extra UIA call per Element), so `find` and `--interactive` lines can be told apart.
- Hints short, capped, printed only where they disambiguate. The line format as an ADR 0004 amendment. `--full` (Codex's text) unchanged unless justified; check `docs/codex-parity.md`.
- Measure the token cost on recorded fixtures the way `internal/wstate/observe_cost_test.go` does.

## What was built

```
		12 button Hint: icon trash
		14 button Delete Hint: in Blue mug
		19 button Delete Hint: in Red mug
		21 edit (settable, string) Hint: name email
		23 button Hint: img close-x.png, testid close-dialog
```

- **Line format** (ADR 0004 amendment): one more field, the last on the line, `Hint: <own>[, in <row>]`. `<own>` is the reader's parts (`icon <name>`, `img <file>`, then the first of `testid`, `name`, `id`), each cut to 40 characters; `in <row>` is the container's text, cut to 40.
- **Who** (`wstate.Hints`, `internal/wstate/hint.go`): a control (button, split button, link, check box, radio button, menu item, edit, combo box, slider, spinner, clickable) with no name, or whose role and name another control of the read shares.
- **Row part:** a Page control's comes from the DOM (`Element.RowText`). Otherwise it is the nearest ancestor in the read (up to six levels, never the Window or the read's root) that holds no other control of the same line and whose text no such control's ancestors have: its name, or, for an unnamed container of at most 16 Elements, the names in it without the control's own. So the "Delete" in each row gets its row whatever the row's role is (list item, group, custom, pane). A part two twins share is left out, but for an unnamed control's own part (it says what the control is).
- **Where it prints** (`View.viewHint`): the default full read prints the own part only, since the row is a few lines up. `--interactive` and `--depth` add `in` unless the row is a named Element the view prints. `--root` adds it under `--interactive` or `--depth`. `find` (UIA and fresh Page read), an `--observe` diff line and an ambiguous `name:`/`id:` reference's candidates always add it. `--compact` keeps a bare line that has a hint. `--full` (`wstate.Render`) and `find --full` print none, so Codex's text is unchanged byte for byte.
- **Page** (`internal/page/hints.go`): `marksOf` now takes the document's AX nodes. After the marks it picks the targets (the AX controls whose role and name are empty or repeat, and the clickables whose text is empty or repeats), then reads each one's hint from the same `docView` over the captureSnapshot answer: `<use href>` fragment, `<svg>` `data-icon`/`<title>`/id, an icon-library class (`lucide-`, `fa-`, `bi-`, `mdi-`, `ri-`, `ti-`, `icon-`, `glyphicon-`, `codicon-`, `octicon-`; `fa-solid` and the like skipped), `<img src>` file name (no `data:`), `data-testid`/`data-test-id`/`data-test`/`data-cy`, `name`, `id`; generated values (three digits in a row, `:r1:`) skipped. The row is the nearest `tr`, `li`, `article`, `[role=row|listitem|article]` or class containing `card`, up to 12 ancestors; its `innerText` (cached per row) without the control's own text. At most 300 per document. Stored as `Marks.Hints` (fixture JSON `hints`), and `Build` copies them onto `Element.Hint`/`RowText`. The clickables.js fallback has none.
- **Native** (`internal/uia/region.go`): `Find` and `ReadRegion` compute `wstate.Hints` over the whole walk and keep each kept Element's hint in `Added.Hints`, so a find match keeps the row it sat in although the row is not printed. A full read computes them at render time from the stored Elements.
- **Speed side-fix:** `AXValue.String` returns a plain JSON string without a decoder, and the target pass matches a role's raw JSON in place (`hintRole`), so looking at every node costs no allocation.
- **Docs:** ADR 0004 amendment, `docs/codex-parity.md` (tree format: hints are a third difference, none under `--full`), help (`get-window-state`, `find`), the skill's `reading` topic.

## Measurements

**Tokens** (`internal/wstate/hint_cost_test.go`, opt-in, `WINUSE_HINT_COST_OUT`, `WINUSE_HINT_TREES`): for each Window State, the default view, `--interactive` and a `find` of every control, with and without hints, rendered by the real code. Window States: the two recorded fixtures plus 41 distinct whole Window States re-extracted read-only from local Claude Code transcripts (2 to 341 Elements: Calculator, Settings, Word, Brave pages and frames, the fixture App, test targets...); every line parsed back to an Element that prints the same text. The copies were deleted after the run. Tokens: tiktoken o200k_base as a proxy, as in speed 05 and 10.

| View | Cases | With a hint | Hinted lines | Tokens without | With | Cost |
|---|---|---|---|---|---|---|
| default | 43 | 0 | 0 | 24,579 | 24,579 | +0.00% |
| `--interactive` | 43 | 6 | 13 | 15,238 | 15,335 | +0.64% |
| `find` of every control | 43 | 7 | 22 | 7,693 | 7,857 | +2.13% |

- Under 30 Elements nothing changes; at 30 or more, `--interactive` +0.88%, `find` +2.53%.
- Typical hints from the run: Explorer's save dialog `button Filter dropdown Hint: in Name` / `in Date modified` / `in Type` / `in Size`, `button Open Hint: in File name:` / `in Save as type:`; Brave's two `button Close`, `Hint: in New Tab - Memory usage - 42.2 MB` and the title bar's `in Minimize Restore`. Brave's extension toggles all share `On, extension enabled` and get none.
- **Not measurable offline: the Page's own part.** The printed states carry no DOM, and the recorded Page fixtures have no snapshot. An upper bound: the browser states hold 102 unnamed or duplicate controls in 15,170 tokens. If each got an own part of 4 tokens (` Hint: icon trash`, ` Hint: name email`) the default read grows 2.7%; at 11 tokens (` Hint: img close-x.png, testid close-dialog`) 7.4%. Most of those 102 are UIA frame controls, which get no own part, so the real figure is lower. Live check below.

**Read cost** (`BenchmarkHintPass`, `internal/page`, GOMAXPROCS=1, minimum of 8 runs on a loaded machine): the marks pass alone against marks plus hints, over one snapshot document.

| Page | Targets | Marks | Marks + hints | Added |
|---|---|---|---|---|
| the Page read benchmark's page (200 role-less clickables, a password field) | 1 | 0.52 ms, 2,590 allocs | 0.75 ms, 2,802 allocs | +0.23 ms |
| 50 rows, an icon button and a "Delete" each | 100 | 0.05 ms | 0.26 ms | +0.21 ms |
| 200 rows, the same | 400 | 0.18 ms | 0.85 ms | +0.67 ms |

Against a measured live snapshot read of 19 to 24 ms (speed-page, headless Chrome and Edge), that is about 1% on a usual page and about 3% on a page of 400 duplicate controls (the pass stops at 300 per document). The read's commands are unchanged: no command is added (`TestReadOverlapsItsRoundTrips` still counts 221 commands on the fallback, the snapshot tests count one captureSnapshot per session). The UIA side adds no call; `wstate.Hints` is two passes over the Elements plus a bounded climb per hinted control.

## Tests

- `internal/wstate/hint_test.go`: who gets a hint (duplicates in named and unnamed rows, unnamed controls, the reader's own part and row, the 40-character cap, nothing for a Window with nothing to tell apart); parts twins share left out and the container stopping at the next twin; the default view (own part only), `--interactive` (row added unless the named row prints), `--depth`, `--full` printing none (equal to `Render`); `--compact` keeping a hinted bare line; a diff line and an ambiguous reference's candidates with the whole hint, `--full` diff without; a hint is not a name.
- `internal/wstate/hint_cost_test.go`: the opt-in measurement; a printed line parses back to an Element that prints the same text.
- `internal/page/hints_test.go`: on a test DOM, the snapshot's hints (sprite icon, `fa-` class, image file and test id, field name over a generated id, a card's `data-icon` with the generated React id skipped, row text without the control's own, nothing for a unique name); `Build` carries them to the Elements and the views; generated values, `data:` URLs and the 300 cap; `BenchmarkHintPass`.
- `internal/page/snapshot_test.go`: the snapshot read and the clickables.js read still give the same marks and Elements but for the hints, which only the snapshot read has.
- `internal/uia/region_test.go`: find's matches keep the hints of the whole walk, parallel to the Elements.
- `internal/helper/find_test.go`: find prints each match's whole hint; `find --full` prints none.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass.

## Live check (to run by a human)

These start the build's own helper, which makes an installed helper of another build exit.

- A Page with icon buttons and repeated controls through the extension and a launched Chrome, e.g. a GitHub file list, a Gmail inbox, an e-commerce cart: `get-window-state <w>` (icon buttons show `Hint: icon ...`), `get-window-state <w> --interactive` and `find <w> --role button --name Delete` (each line with its `in <row>`), `--full` (no hint). Check the hints name real icons and rows, and note how often the own part is noise (class names that are not icons).
- Time a Page read before and after on the same page (`--trace <file>`, 3 runs each): it should stay within a millisecond or two.
- Native: Explorer's Details view (`find <w> --role button --name "Filter dropdown"` shows `in Name`, `in Date modified`...), Settings' Notifications list, a WinForms DataGridView with a button column.
- An ambiguous `click <w> name:Delete` lists the candidates with their hints.

## Comments
