# 27: `winuse extract` on native lists and grids

Status: resolved
Type: task
Blocked by: 26 (for the shared command surface)
From: tickets 13 and 17

## Ask

- **Same command.** `extract <window> [--root <el>] [--limit N]` on a
  native Window, with the same header and JSON output as ticket 26.
  - `--limit` defaults to 500 rows.
  - Without `--root`, it uses the Window's only Table or Grid, or else the
    largest list. The header names it.
- **How to read it** (ticket 13; never `GridPattern.GetItem`):
  - **Detection.** Use Table or Grid only to detect the table and to get
    headers (`GetColumnHeaders`) and RowCount.
  - **Rows.** One bounded cached read of the rows and their cells: Value,
    Name, ControlType, and GridItem row and column.
    - Place cells by GridItem column when present, or else by position.
    - Drop header rows and header cells.
  - **Cell text.** Use Value first, and Name only when there is no Value
    pattern.
    - Strip U+200E and U+200F.
    - Trim header names.
    - Don't take a cell Name that just repeats its column (Explorer's
      Size).
  - **Fallback when the patterns are missing** (.NET Framework DataGridView,
    and Services, whose cells have no GridItem): use child rows
    (DataItem/ListItem/Custom) with their children as cells, and headers
    from Header/HeaderItem elements.
- **Bounds.** Never request every row at once: an unbounded read crashed
  Event Viewer at 22,915 rows. Above the limit, walk in chunks and stop at
  the limit.
- **Virtualized lists** (Explorer, WPF).
  - Report `N of M rows (partial: virtualized)`, using RowCount and the
    ItemContainer count.
  - Never Realize by default, because it scrolls the App.
  - Optionally add `--names` for a names-only list read from the
    placeholders.
- **Tests.**
  - Unit tests over recorded trees (`record-tree`) for Explorer details,
    Services, .NET 8 ListView, and a WPF DataGrid.
  - An e2e test on a fixture list if the fixture App gains a grid. The
    current fixture has only a single-column list.

## Answer

Built on branch `agent/bladebro-ideas` (merge fd8412d; review fixes in 081a150). `extract` on native Windows reads the only Table/Grid or the longest list (default 500 rows), never `GetItem`, bounded cached reads; `record-tree --table` records fixtures. Fixtures are hand-written from ticket 13's notes; `--names` not built.
