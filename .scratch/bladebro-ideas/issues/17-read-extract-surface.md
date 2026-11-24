# 17: `read` and `extract`: command surface

Status: resolved
Type: grilling
From: `map.md`

## Decisions needed

- **New commands or flags?** `read` and `extract` could be new commands, or
  flags on `get-window-state`, `find` or `eval`. They print no Element
  Indexes; check what fits ADR 0001 and ADR 0004.
- **One `extract` for both?** Should one `extract` cover Pages (a list
  scorer, tables, JSON-LD) and native Apps (UIA tables)?
- **Output:** JSON lines, a JSON array or CSV; the budget flags
  (`--max-chars`, `--out`, `--limit`); and how a partial result is
  disclosed.
- **Scope:** `--root <element>` for a subtree.
- **Pagination:** leave it out, or provide a loop helper.
- **Attribution and licence** for the ported JS (Apache-2.0).

## Answer

Grilled 2026-11-24 (round 4).

1. **Two new commands.**
   - `winuse read <window>[:<tab>] [--root <el>]` prints the main content as
     Markdown. Pages only.
   - `winuse extract <window>[:<tab>] [--root <el>]` prints rows as JSON,
     for Pages and native lists and grids.

   Neither prints Element Indexes or changes the Window State.
2. **`--root` is optional for `extract`.** The output always names the
   container it used (`list: <ul> "Search results", 10 rows`) and suggests
   `--root <index>` if that's wrong. On a page with no repeated items it
   says "no list found" rather than guess.
3. **`extract` output.**
   - **Header:** one line, `N of M rows (complete)` or
     `(partial: virtualized / capped / page says N results)`.
   - **Body:** a JSON array.
   - **Modes:** `--as list|table|jsonld`, default auto.
   - **Bounds:** `--limit` defaults to 50 on Pages and 500 on native Apps.
     `--max-chars` and `--out` work as in other reads.
4. **Links in `read`.**
   - same-site links print as paths;
   - external links print as domain plus path, cut if long;
   - image URLs are dropped;
   - `--urls full` prints them unshortened.

Build tickets: 25 (`read`), 26 (`extract` on Pages), 27 (`extract` on native
Apps).
