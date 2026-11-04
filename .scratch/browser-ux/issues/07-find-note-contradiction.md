# 07: find's two notes contradict each other

Status: done

**Evidence.** Before the extension connected:

```
winuse find 20580206 --text winuse
...
Note: the Page is read through UI Automation because this Brave window belongs to Brave process 18440, which has no debugging connection winuse can use; ...
Note: find read window 20580206's Window State afresh through its Browser Connection, so its earlier Element Indexes were replaced by these
```

**Root cause.** `internal/helper/find.go` `findInFreshRead` always ends with
"afresh through its Browser Connection", even when the read fell back to UI
Automation (`page.FallbackNote`).

**Fix.** Say "through its Browser Connection" only when the read did not fall
back.

## Comments

Done on branch browser-ux: `internal/helper/find.go`, `page.FallbackPrefix`. Test: `TestFindWithoutABrowserConnectionSaysSo`.
