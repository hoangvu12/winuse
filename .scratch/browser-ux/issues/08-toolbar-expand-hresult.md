# 08: Expand on Brave's Extensions toolbar button fails with E_FAIL

Status: needs-info

**Evidence.**

```
winuse perform-secondary-action 20580206 id:view_1006 Expand
expanding the Element failed (HRESULT 0x80004005)
hint: the Action may have taken effect; read the Window State (winuse get-window-state 20580206) before repeating it
```

`list-windows --app brave` afterwards showed no new menu Window.

**Notes.** One sample, on a Brave Window covered by another App. Chromium's
toolbar buttons expose ExpandCollapse, but opening the extensions menu may need
the Window to be visible. Needs a reproduction on an uncovered Window (and on
Chrome) before deciding whether winuse should refuse with nothing sent, or try
Invoke instead.

## Comments
