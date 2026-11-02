# 09: Shell surfaces

**What to build:** let the agent read and act on the taskbar, the notification area (tray and its overflow), Start and the Action Center. agent-desktop exposes all of them (`ad:docs/platforms/windows.mdx:53-55`). winuse excludes them on purpose (`internal/win/enum.go:64-71`).

- `list-windows --shell` lists `Shell_TrayWnd`, `Shell_SecondaryTrayWnd`, the overflow window (`TopLevelWindowForOverflowXamlIsland` on Windows 11), and Start / Action Center when open.
- Their Window States read through UIA like any Window; Headless Element Actions apply (taskbar buttons and tray icons expose Invoke).
- Opening Start or the Action Center needs keys: Start through `Control_L+Escape` (Headed). The Action Center is Win+A or Win+N, which winuse refuses; use the UIA Invoke on the taskbar's notification button instead (Headless if it works; probe).
- Clicking a taskbar button activates its App: that is the user-visible effect of the Action; say so in the skill.

**Blocked by:** none

**Status:** needs-triage (needs a short ADR: reverses the enum exclusion; Windows-key refusal stays)

- [ ] Shell Windows in the listing behind `--shell`
- [ ] Probe: Invoke on taskbar, tray and notification buttons, foreground before and after
- [ ] Offline tests of the listing
- [ ] Opt-in e2e: read the taskbar, open the tray overflow
- [ ] ADR, SKILL.md
