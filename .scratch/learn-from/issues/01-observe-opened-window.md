# 01: --observe follows the Window the Action opened

Status: ready-for-human
Type: task
Branch: learn-observe-opened, off main 03fc3a4
From: `.scratch/learn-from/README.md` rank 1, `.scratch/learn-from/agent-desktop.md` §2.1 (agent-desktop's `data.surfaces`)

## Ask

- `--observe` reads only the Action's own Window (or Tab target), ADR 0021 Decision 2. When the Action opens a menu, dialog or new Window, the agent spends another turn on `get-window-state <Window ID>`. Make `--observe` print that Window's State too.
- Decide the rules: diff or whole for the new Window, several opened Windows, Batches, Tab targets, budgets and caps, a read that fails. Record them as an ADR 0021 amendment.
- Help, the skill, CONTEXT.md and the Codex-parity doc.

## What was built

```
winuse click 101 name:File --observe          (illustrative, abbreviated)
ok
Note: a new Window opened: 778 Notepad ""
Window: "Untitled - Notepad", App: notepad.exe.
No change since this agent's last read of window 101.
The Window the Action opened, 778, read whole; its Element Indexes go with window 778:
Window: "", App: notepad.exe.
	0 menu File
		1 menu item New
		...
```

- **Which Window** (`internal/helper/observe.go`): the opened-Window settle (`opened.go`, ADR 0019) records the new Windows its last look found (`recordOpened`) into the request when `--observe` is set; a Headless ContextMenu, which reports its menu itself (`contextmenu.go`), records the menu. Exactly one: it is read after the Action's own Window. None: nothing more. Several: none is read, and `Note: --observe read none of the 2 Windows the Action opened (777, 778); read the one you need with winuse get-window-state <Window ID>`.
- **Whole, never a diff**, in both modes, under `The Window the Action opened, <Window ID>, read whole; its Element Indexes go with window <Window ID>:`. A new Window has no earlier read worth diffing against, and a menu's Window ID can be reused. The read is `get-window-state <Window ID>`'s (`readWindowTree`, the Chromium settle, `sawWindow`), so it replaces the Agent's Window State of that Window and its indexes work at once.
- **Batch:** the settle around all its steps decides (the latest record wins), so only a Window still open after the last step and not open before the Batch is read. A dialog an `in new` step filled and closed is not. A Batch past failed steps reads it too, as its own observation.
- **Tab target:** its observation stays the Tab's. A native Window its browser opened is read like any other; a new Tab and a JavaScript dialog stay notes.
- **Budget and caps:** both reads share the observation's `ObserveTimeout` (10 s), the request budget is unchanged. Each read takes the observation's view on its own (the 28000-character cap, `--interactive`, `--depth`, `--compact`, `--full`). `--out observed.txt` writes the opened Window's State to `observed-<Window ID>.txt` and prints that file's line.
- **Failures are notes:** `Note: --observe could not read <Window ID> after the Action, which ran as reported above: ...`, also when the Window closed between the settle and the read (a fresh listing checks). The Action's output and its own observation stand. A failed Action and an Esc stop read nothing, as before.
- **Docs:** ADR 0021 amendment (with what was considered: the topmost of several, a shared cap, a second `ObserveTimeout`), CONTEXT.md (Window State, one clause), `docs/codex-parity.md`, help (the `--observe` paragraph in `help global`/`help all` and the line under each observable command), the skill core line and `batch-and-wait`.

## Tests

- `internal/helper/observe_opened_test.go`: one opened dialog read whole after the diff, in that order, and stored as the Agent's Window State of it; whole through `--interactive` even with a stored State; several opened Windows are a note and read nothing more; a failing read and a Window closed before the read are notes; a failed Action reads nothing; a failed Batch reads the opened Window under its lines; `--out` writes `observed-777.txt`; a Headless ContextMenu's menu is read through the whole registry; the latest record wins.
- `internal/cli/observe_test.go`: `help global` and `help click` say it.
- `test/e2e/observe_opened_test.go` (user-run): click "Classic Dialog..." on the covered fixture with `--observe`, the main Window then the dialog's whole State, the foreground stays with the cover, Escape on the printed Window ID closes it.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass.

## Live check (to run by a human)

These start the build's own helper, which makes an installed helper of another build exit.

- `go test -tags e2e -v -run TestObserveReadsTheOpenedDialog ./test/e2e`
- By hand: Notepad `click <window> name:File --observe` (the menu, whole, after the main Window's diff); `--headed press-key <window> Alt_L+e --observe`; Notepad Ctrl+S with `--observe` (the Save As dialog; check its Chromium-free settle finds it in time); `perform-secondary-action <window> <ref> ContextMenu --observe` in File Explorer (a late menu may be missed by the read-back, as before); a Chrome Window: right click with `--headed ... ContextMenu --observe` (a Chromium menu popup, read through UIA); a Batch `click name:Open then in new press-key Escape --observe` (nothing more read); `--observe --out <file>` (two files).

## Comments
