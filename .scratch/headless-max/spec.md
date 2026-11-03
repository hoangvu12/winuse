Status: ready-for-agent (triaged by the user 2026-11-03; ADR 0013 accepted)

# Headless max: every Action headless where Windows allows it

## Problem statement

ADR 0010 and ADR 0011 made Element Actions, screenshots, Window Actions, Element scroll, dialog Return/Escape, classic-edit typing and Posted Keys headless. ADR 0012 made browser Pages headless. 22 paths still take the user's foreground, real cursor or keyboard, or can let an App take the foreground. They are listed with file:line in `../headless-gaps/headless-100.md` section 1.

The user wants winuse to be "100% headless". The research shows that this is not reachable on the user's own desktop (section 3 of the research), but about two thirds of the remaining paths can get a headless or Posted route for classic Win32 targets, and a few for every UIA framework.

## Solution

Close every gap that has a route, and say plainly where none exists.

- Launch and restore: no foreground change from winuse, and an honest report when an App takes the foreground itself.
- Routes with a readback become Headless by default: an accelerator found through UIA, a menu command with a grey-state check, a context menu with a menu-opened check, text selection, a point resolved to an Element inside the target Window.
- Routes without a readback are opt-in, like `--post`: Posted Clicks (double, right, middle, point) and chords through attached key state.
- Everything else keeps failing with "nothing was sent" and the exact `--headed` retry. winuse never falls back on its own.

## Research

- `../headless-gaps/headless-100.md`: inventory, techniques per gap, live probe record (Windows 11 Pro 10.0.26200), impossible list, isolation options (MXC, Agent Workspace, hidden desktops, RDP loopback).
- Probe scripts: the session scratchpad `probe\` folder (not in the repo). Re-run against windows the probe opens itself only.

## Policy (proposed ADR 0013, ticket 17)

1. A route with a readback may be Headless by default.
2. A best-effort message route with no readback is opt-in per command (`--post`), never an automatic fallback.
3. Where no route exists: fail before sending, "nothing was sent", plus the exact `--headed` retry.
4. Never attach to the foreground thread, never SendInput, never SetCursorPos on a Headless or Posted route.

## Tickets

| # | Title | Effort | Status |
|---|---|---|---|
| 01 | Launch without the foreground, and launch options | S | done (live e2e not run yet) |
| 02 | restore-window reports self-activation | S | done |
| 03 | EnableWindow guard against self-activation | S | done (detect and report; live e2e run still to do) |
| 04 | Occlusion flag for launched browsers | S | done |
| 05 | Headless tab switch on launched browsers | S | done (live e2e run still to do) |
| 06 | Accelerator lookup route for press-key | S | ready-for-agent (low priority) |
| 07 | Chords through attached key state (opt-in) | M | ready-for-agent |
| 08 | invoke-menu for classic menu bars | M | done (live e2e pending) |
| 09 | ContextMenu Secondary Action | M | ready-for-agent |
| 10 | select-text | S | done |
| 11 | Point forms resolve to an Element in the target Window | M | ready-for-agent |
| 12 | Posted Clicks (opt-in) | M | ready-for-agent |
| 13 | Caret-aware typing into Text-pattern fields (and the `type-text --into` Text-pattern leg, from desktop-parity 23) | M | ready-for-agent |
| 14 | Headless Actions on minimized Windows | S | done (live e2e pending) |
| 15 | Electron and WebView2 Browser Connections at launch, and attach to a running Electron (opt-in) | L | done (live e2e runs still to do) |
| 16 | Spike: MXC session container | M | wontfix |
| 17 | ADR 0013, CONTEXT.md, skill and the impossible list | S | ready-for-agent |
| 18 | Longer settle for a cold Chromium UIA tree | S | done |
| 19 | Headless typing into a covered Windows 11 Notepad | S-M | done |
| 20 | Background typing, keys and clicks for Chromium and Electron without CDP | M | ready-for-agent |
| 21 | No Headed Action without `--headed` | M | ready-for-agent (build before the click and typing chains) |

21 tickets: 10 S, 1 S-M, 9 M (one spike), 1 L. Done: 01, 02, 04, 05, 08, 10, 14, 15, 18, 19.

## Impossible headless (stays a refusal with a `--headed` hint)

From `../headless-gaps/headless-100.md` section 3: hover outside classic Win32; native drag outside classic Win32, and OLE drag-and-drop between apps; clicks or scrolls on a surface with no Element and no HWND (canvases, games); keys into native Chromium without CDP and into raw-input apps; IME composition outside CDP; activate-window and a top-level Raise; guaranteeing an App never takes the foreground itself; administrator Windows; attaching CDP to an Electron or WebView2 app that is already running.

## Testing decisions

- Offline (`go test ./...`): routing per Action, flag and Window probe; refusals; read-back maths; all through Services fakes, as the existing helper tests do.
- Opt-in e2e (`WINUSE_E2E=1`): written and compiled with `go vet -tags e2e ./test/e2e`, run by the user only. Each test covers the target and checks the foreground is unchanged afterwards.

## Out of scope

- Parity features from agent-desktop that are not about headlessness: `../desktop-parity/spec.md`.
- SendInput tricks (VK_NONAME activation burn, synthetic pen or touch): they move the real input state.
- UIAccess and running elevated.
