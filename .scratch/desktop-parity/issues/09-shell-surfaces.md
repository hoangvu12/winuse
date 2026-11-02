# 09: Shell surfaces

**What to build:** let the agent read and act on the taskbar, the notification area (tray and its overflow), Start and the Action Center. agent-desktop exposes all of them (`ad:docs/platforms/windows.mdx:53-55`). winuse excludes them on purpose (`internal/win/enum.go:64-71`).

- `list-windows --shell` lists `Shell_TrayWnd`, `Shell_SecondaryTrayWnd`, the overflow window (`TopLevelWindowForOverflowXamlIsland` on Windows 11), and Start / Action Center when open.
- Their Window States read through UIA like any Window; Headless Element Actions apply (taskbar buttons and tray icons expose Invoke).
- Opening Start or the Action Center needs keys: Start through `Control_L+Escape` (Headed). The Action Center is Win+A or Win+N, which winuse refuses; use the UIA Invoke on the taskbar's notification button instead (Headless if it works; probe).
- Clicking a taskbar button activates its App: that is the user-visible effect of the Action; say so in the skill.

**Blocked by:** none

**Status:** ready-for-agent (write the short ADR that reverses the enum exclusion; probe the Invoke routes first, with the user away)

- [ ] Shell Windows in the listing behind `--shell`
- [ ] Probe: Invoke on taskbar, tray and notification buttons, foreground before and after
- [ ] Offline tests of the listing
- [ ] Opt-in e2e: read the taskbar, open the tray overflow
- [ ] ADR, SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop (Start, taskbar, tray, tray overflow, Action Center), Headless first: read and Invoke through UIA where the probe shows it works. agent-desktop's shell-accelerator route (Win+A and so on) is the fallback and runs only with `--headed`; the general Windows-key refusal stays for press-key.

### 2026-11-03: shell probe, partial (`../../headless-gaps/probes-round4.md` section 7)

Reading works: Shell_TrayWnd's UIA tree has 24 Elements, with Start, Search, the tray buttons ("Show Hidden Icons", Clock and so on) and the running-App buttons, read Headless with the foreground unchanged. The tray overflow window exists but has no Elements while it is hidden. **Not measured:** Invoke on the chevron and the Clock (it opens a flyout on the user's shell, and the user has not approved that yet). Build the listing and the reads now. The Invoke routes wait for that probe.
