# 26: `winuse extract` on Pages

Status: resolved
Type: task
From: tickets 12 and 17

## Ask

- **The command.** `winuse extract <window>[:<tab>] [--root <el>]
  [--as list|table|jsonld] [--limit N (default 50)]
  [--max-chars N | --out <file>]`.
  - The first line is a header naming the container and the count:
    `list: <ul> "Search results": 10 of 10 rows (complete)`, or
    `(partial: capped at 50 / page says 1,234 results)`.
  - Then a JSON array.
  - Without `--root`, the hint says `--root <index>` if the container is
    wrong.
  - On a page with no repeated items, it prints "no list found" and nothing
    else.
- **Start from the port** in `../prototypes/extract/extract_list.js` (driver:
  `run.mjs`), which has `table` and `jsonld` modes.
  It's Apache-2.0: keep the attribution, and run it in an isolated world.
  Fix what ticket 12 found:
  - **Grouping.** Group cards loosely (same tag and class, or most children
    shared), so optional badges don't split a grid.
  - **Records.** Merge sibling rows of one record, like HN's two rows per
    story.
  - **Dedupe** only on the title link. Today's URL dedupe dropped 83 of 115
    table rows.
  - **Ranking.**
    - Stop preferring off-site links, so references don't beat tables.
    - Navs and footers never win.
    - Prefer JSON-LD over a weak list.
    - On article and doc pages, say "no list found".
  - **Root as an example.** When `--root` is a single item, treat it as an
    example and gather its similar siblings.
  - **"Page says N results."** Accept `10.3k`, `24,134,885 questions` and
    `1–30 of 208`.
  - **Field names.** A star count is `stars`, not `rating`. Rows don't
    carry vote and view noise in their descriptions.
  - **Block walls.** Detect them with ticket 05's check and say so.
- **Docs.** Add it to `browser.md` and `commands.md`.
- **Tests.** Golden files for:
  - search results;
  - a product grid with optional badges;
  - a table with a shared link column;
  - a two-row-per-record list;
  - an article (expects "no list found");
  - a JSON-LD recipe;
  - scoped to the list, to an ancestor, and to one item.

## Answer

Built on branch `agent/bladebro-ideas` (merge 27dc601; review fixes in 081a150). `winuse extract` on Pages prints a header naming the container and JSON rows (default 50), `--root` scoped, `--as table|jsonld`, "no list found" rather than a guess. Shared output in `internal/extract`. After the review: `N+` when the script's own cap stopped it, and a list inside `--root` wins when the root is that list.
