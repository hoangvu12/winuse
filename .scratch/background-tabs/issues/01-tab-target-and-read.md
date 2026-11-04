# 01: Tab target `<window>:<tab>` and its CDP-only Window State

Status: done

Spec decisions 1 and 2 (ADR 0017 Decisions 1 and 2).

**Done.**

- `splitTabRef` and `tabTargeted` (`internal/helper/tabtarget.go`): the
  registry's outermost wrapper. A `window` argument of the form
  `<window>:<tab>` (tab id 4 to 32 letters or digits, after the last colon)
  is resolved over the Window's Browser Connection among every Tab of that
  browser (`page.ListTabs` + `page.ResolveTab`), attached
  (`Reader.TabSession`), held, and put in the context; the command then runs
  on the Window part. The tab commands take the Window part alone.
- `tabWindowState`: `page.Reader.ReadTab` (new, `internal/page/background.go`)
  + `page.Build`, under a `document` Element 0 named after the Tab (its URL
  as Value), ending with a note that says it is a Tab target, how to name
  it, and on a non-extension route that a link may show its new Tab.
- `tabStateStore`: per Agent ID, per (connection, target id), at most 32
  per Agent, dead connections dropped.
- Choke points: `pageResolve` (→ `tabResolve`, never UI Automation; every
  miss is a refusal with nothing sent), `pageElementName`, `pageDoc`,
  `pageTabCheck`, `pageWindow`, `pageStep`.
- `retarget`: failures and Note lines name "tab X of window N", hints run on
  `N:X`. The Page's own output (eval) is left alone.
- Tests: `TestSplitTabRef`, `TestRetargetNamesTheTab`,
  `TestTabTargetReadsABackgroundTab` (incl. another Agent has no state),
  `TestTabCommandsTakeATabTargetsWindow`.

## Comments
