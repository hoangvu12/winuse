# Browser UX: setup, the Agent Cursor on browser Actions, and the field report

Source: the Zeron chat "Research UI/UX Design Resources" (362fdd9e), an agent
driving the user's own Brave through winuse 0.2.0 on 2026-11-04 to research
X/Twitter. Read from its session log
(`~/.claude/projects/C--Users-HP-MEDIA-Desktop-nguyenvu-winuse/da69124b-....jsonl`),
since the Zeron transcript summarizes tool calls without their output. Plus two
problems the user reported themselves (the Agent Cursor lagging the Action,
and no cursor for browser Actions).

## Timeline of the session (what it cost)

| time (UTC) | what happened |
| --- | --- |
| 07:11:37 | `winuse browser status` + `list-windows --app chrome`: Chrome not running, Brave is. Hint offers four routes. |
| 07:11:49 | `winuse browser ext install brave`: files written, host registered under `HKCU\Software\BraveSoftware\Brave-Browser\NativeMessagingHosts`, steps printed for the user. |
| 07:11:54 | Agent hands the user the 3 manual steps and stops. User replies "help me with that". |
| 07:12:23–07:12:51 | Agent loads the unpacked extension itself with winuse (click "Load unpacked", set the Folder box, Select Folder). Works. |
| 07:12:51–07:14:26 | **95 s and 14 commands** of debugging: `ext status` says "loaded but has not connected"; the agent re-registers, reloads, reads `background.js`, pipes a hello into `winuse.exe` by hand, and finally reads the registry. Brave only connected after `winuse browser ext install chrome` wrote **Chrome's** key. |
| 07:14:36 | Research starts. |
| 07:15–07:18 | Two intermittent `no Tab of the Browser Connection matches window 20580206's title and bounds; nothing was sent`; every result past 4000 characters paged by hand with `eval "window.__out.slice(...)"`; selectors rewritten around PowerShell 5.1 quote stripping. |

## Problems

| # | Problem | Root cause | Issue |
| --- | --- | --- | --- |
| 1 | The extension never connects in Brave | `hostKeys["brave"]` names Brave's own key; Brave on Windows reads Chrome's | [01](issues/01-brave-native-host-key.md) |
| 2 | Extension setup is many manual steps and a poll loop | `ext install` only prints steps; `ext status` has no wait; `browser status` lists four routes with no recommendation | [02](issues/02-extension-setup-flow.md) |
| 3 | The Agent Cursor shows the Action after it happened | the ripple fires after the whole act returns (read-back, a navigation's load); the label fades in only after the cursor lands | [03](issues/03-cursor-lags-the-action.md) |
| 4 | No (or a wrong) cursor for browser Page Actions | the page→screen transform needs the UIA document Element, which a covered Chromium does not build; it then falls back to the Window's centre | [04](issues/04-page-cursor-transform.md) |
| 5 | Intermittent "no Tab matches" right after navigate | the Tab match compares a Window title read once, before the Action, with the Tab's title, while the page is still changing it | [05](issues/05-tab-match-title-race.md) |
| 6 | eval output cut at 4000 characters; long expressions mangled by PowerShell 5.1 | fixed `page.EvalLimit`; the expression only comes from argv | [06](issues/06-eval-limit-and-file.md) |
| 7 | `find` on a browser Window without a Browser Connection prints two contradicting notes | `findInFreshRead` always says "through its Browser Connection" | [07](issues/07-find-note-contradiction.md) |
| 9 | Agents reach for `browser launch` / `clone` instead of the user's own browser | the skill and the `NotRunning` / `AwaitingApproval` hints present them as peers or steer to them | [09](issues/09-prefer-the-users-browser.md) |
| 8 | `perform-secondary-action ... Expand` on Brave's Extensions toolbar button failed with HRESULT 0x80004005 | Chromium's ExpandCollapse on that view; one sample, not reproduced | [08](issues/08-toolbar-expand-hresult.md) |

Not winuse: curl's `000` (Windows schannel revocation checks) and `wait selector 'article'` timing out on an X search that had no results.

## Decisions

- **Browser Actions keep the native Agent Cursor** (ADR 0016), mapped from the Page to the screen through the browser's content child window, not an in-page cursor injected by the extension.
- **On a Page, the ripple marks delivery, not completion**: it plays when the first input event that acts (a press, a key, text, a wheel step) or a navigation request is accepted. UI Automation Actions keep rippling after the act, because an act there can still refuse with nothing sent (issue 03).
- **The label is readable when the Action happens**: a new label reveals during the last part of the travel, so it is whole on landing.
- **`ext install` registers the native host under every key the browser may read** (its own and Chrome's for Brave, Vivaldi, Opera and Chromium).

## Not changed

- The cursor stays just above the Window it acts on rather than topmost (ADR 0014): a covered browser Window keeps its cursor covered too.
- `install.ps1`: the session never ran it (winuse was installed), so there is no evidence against it here.
