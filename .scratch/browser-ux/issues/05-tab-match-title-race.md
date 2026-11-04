# 05: "no Tab matches" right after a navigation

Status: done

**Evidence.** The agent's search script ran `winuse navigate $w $url`,
`winuse wait $w selector 'article'`, then `winuse eval ...`. Twice the first
call after the navigation failed, then the next worked:

```
no Tab of the Browser Connection matches window 20580206's title and bounds; nothing was sent
hint: the Window may belong to another profile of the browser; run winuse browser status brave
```

The hint sent the agent the wrong way (one profile was connected); it
concluded "the title changes, so that's fine for now".

**Root cause.** `page.MatchTab` needs the Window title (`Geometry.Title`,
from the Window list read when the command resolved its Window) to show the
Tab's title (`Target.getTargets`). While X loads, both change at different
moments: the Win32 caption follows the Tab's title asynchronously.

**Fix.** On a `NoTabError`, re-read the caption and the Tabs and try again,
for up to about a second (`page.FindTabSettled`), before failing. The
geometry builder reads the caption fresh each time. The failure's hint names
the title race as well as another profile.

## Comments

Done on branch browser-ux: `page.FindTabSettled` (`internal/page/reader.go`), fresh caption in `livePages.geometry` (`win.WindowTitle`), used by page Actions and `PageActiveTab`; hint reworded in `internal/helper/tabs.go`. Test: `TestFindTabSettledWaitsOutATitleChange`. `tab` commands still match once.
