# 13: UIA table patterns in native Apps

Status: resolved
Type: research
From: `map.md`; the native half of the `extract` idea; also the agent-desktop "Tables and grids" idea (`.scratch/learn-from/README.md`, rank 8)

## Question

How well do real Apps expose UIA Table, Grid, TableItem and GridItem
patterns? For each target, check:
- RowCount and ColumnCount;
- column headers;
- cells with row and column;
- virtualized rows;
- how long it takes to read every cell.

Targets: Explorer details view, Services, Event Viewer, Settings > Installed
apps (WinUI), and WinForms and WPF grids, including winuse's fixtures if it
has any. Nothing in winuse uses these patterns today.

The probe reads only. It opens its windows minimized and closes them after.
This blocks ticket 17.

## Answer

**Probe.** A C# probe against the COM `IUIAutomation` API that winuse uses,
run against 11 lists and grids. The WinForms and WPF targets were the probe's
own hosts, 1000 rows × 4 columns each. Code and raw output are in the chat's
scratch folder `exp\uia-tables\`.

| Target | Table/Grid | Headers | GetItem | One cached read of the rows | Virtualized |
|---|---|---|---|---|---|
| Explorer details (470 files) | yes | yes | works (only shown rows) | 27 ms | yes: the grid counts only the 22 shown rows; all 470 via Realize took 3.85 s and scrolls |
| Services (MMC) | yes | yes | **broken** (row 0 = headers, server fault) | 825 ms; cells have no GridItem | no |
| Event Viewer, list of logs | yes | yes | 3.1 s | 1.3 s | no |
| Event Viewer, Application log | yes, 22,915 rows | yes | 0.87 ms per cell | – | no; **listing every row crashed Event Viewer** |
| .NET 8 ListView | yes | yes | **null for every cell** | 101 ms | no |
| .NET 8 DataGridView | yes | yes | **E_NOTIMPL** | 11.5 s | no |
| .NET Framework ListView | yes | yes | 2.9 s | 767 ms | no |
| .NET Framework DataGridView | **no** | header row only | – | 1.7 s (child rows only) | no |
| WPF DataGrid / ListView | yes | yes | 5.7 s / 2.9 s, **and it scrolls** | 264 / 87 ms (shown rows only) | yes, about 30 rows shown of 1000 |

Not tested: Settings > Installed apps (the user's Settings window was open)
and Task Manager (it relaunches elevated).

**Gotchas**
- `GetItem` is unreliable, and in WPF it isn't read-only: it scrolls the
  grid.
- Explorer's row numbers count only the rows on screen.
- An empty cell can come back named after its column (Explorer's Size), and
  DataGridView names cells like `"Name Row 0, Not sorted."`. Read Value
  first.
- Explorer dates carry U+200E and U+200F characters.
- Header and row element types vary (HeaderItem, Header, SplitButton;
  ListItem, DataItem, Custom). Header names can carry trailing spaces.
- **Unbounded reads crash Apps:** Event Viewer died listing 22,915 rows.
- Apps launched minimized can show no content (Explorer).

**Recommendation: native extract works, but not on GetItem.**
- **Detection:** use Table or Grid only to detect a table and to get headers
  and RowCount.
- **Reading:** one bounded cached read of rows and their cells. Place cells
  by GridItem column, or else by position, and drop header rows.
- **Bounds:** cap at about 500 rows by default, walk in chunks above that,
  and never ask for everything at once.
- **Disclosure:** report "N of M rows (virtualized)". Realize is opt-in,
  because it scrolls the App.
- **Fallback when the patterns are missing:** child rows
  (DataItem/ListItem/Custom) with their children as cells.
- **Virtualized lists:** placeholder Names give a cheap names-only list.

**Side effects of the run**
- An elevated Task Manager was left open (it relaunched itself elevated, and
  the probe couldn't close it). The user was asked to close it.
- An Event Viewer crash left error events in the Application log.
- Task Manager's `settings.json` was rewritten.
