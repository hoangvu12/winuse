# 17: ADR 0013, CONTEXT.md, skill and the impossible list

**What to build:** the decision record and documentation for this feature.

- **ADR 0013 "Headless by readback, Posted by opt-in"**, amending ADR 0010 and 0011:
  1. A route with a readback may be Headless by default (accelerator lookup, menu command, context menu, select-text, point-to-Element, caret splice).
  2. A best-effort message route without a readback is opt-in per command (`--post`): Posted Keys, Posted Clicks, attached-state chords.
  3. Where no route exists, fail before sending with "nothing was sent" and the exact `--headed` retry. Never fall back on its own.
  4. No Headless or Posted route attaches to the foreground thread, sends input or moves the cursor.
  5. Launch and restore are non-activating calls; winuse reports when an App takes the foreground itself.
  Record each ADR reversal the tickets make (ADR 0010 Decision 1 for 11, the minimized refusal for 14, ADR 0011 Decision 5 for 07 and 12, ADR 0012 Decision 6 for 15).
- **CONTEXT.md**: narrow "Headed Action" to what is left; add Posted Clicks next to Posted Keys; add the new Actions (invoke-menu, select-text, ContextMenu).
- **SKILL.md**: a short "What winuse cannot do headless" list (from `../headless-gaps/headless-100.md` section 3) with the refusal wording, and when to reach for `--post` or `--headed`.
- **README**: one line on headless coverage.
- **docs/codex-parity.md**: reword rows the tickets contradict, keep each status; run `go test ./internal/skill/...`.

Write the ADR before the tickets that reverse earlier decisions land; update CONTEXT.md, SKILL.md and README as each ticket lands, then check the whole set here.

**Blocked by:** none for the ADR; the documentation pass follows 01-15

**Status:** done

- [x] ADR 0013 drafted and accepted by the user
- [x] CONTEXT.md terms
- [x] SKILL.md impossible list and routing rules
- [x] codex-parity rows reworded; parity guard test passes

## Comments

### 2026-11-03: round 3 probes (`../headless-gaps/probes-round3.md`)

- Add to the policy: **UIA SetFocus is never used in a Headless or Posted path.** It takes the foreground for any App, even without foreground rights (measured).
- Impossible list: take "keys into native Chromium without CDP" off (ticket 20), and "attaching CDP to an Electron app that is already running" too, when its fuse is on (ticket 15, opt-in). Minimized Chromium stays on the list.

### 2026-11-03: triage decision (user)

The policy is accepted, with two additions:

- UIA SetFocus is never used in a Headless or Posted path. It takes the foreground for any App (round 3).
- An exception the user chose: ticket 20's Chromium and Electron route is automatic, with no `--post`, even where no read-back exists. Its output then says the text was not verified.

Update the impossible list from round 3: Chromium and Electron keys without CDP come off; a running Electron with the fuse on comes off (opt-in, ticket 15); minimized Chromium stays on.

### 2026-11-03: ADR 0013 written; the doc pass continues

- `docs/adr/0013-headless-by-readback-posted-by-opt-in.md`: the five policy points, the user's two additions (no UIA SetFocus in a Headless or Posted path; ticket 20 automatic), a table of routes marked done or planned, the four reversals (ADR 0010 Decision 1 for 11, the minimized refusal for 14, ADR 0011 Decision 5 for 07 and 12, ADR 0012 Decision 6 for 15) and the impossible list updated from rounds 3 to 5.
- Pointers added to ADR 0010, 0011 and 0012, and to `../headless/spec.md` (the minimized refusal and "Headless `@x,y` actions").
- CONTEXT.md: Posted Clicks next to Posted Keys (marked planned); the Headed Action definition notes ADR 0013.
- SKILL.md: "What winuse cannot do headless" under "Choose Headless or Headed", with when to use `--post` or `--headed`. It describes only what winuse does today, so it names no planned route.
- Still open, as each code ticket lands: CONTEXT.md's Headed Action narrowing and the new Actions (invoke-menu, ContextMenu), the SKILL.md routes, README's headless line, and the docs/codex-parity.md rows. Then check the whole set here.

### 2026-11-03: the final doc pass

Checked the whole set against what landed (headless-max 01 to 21, desktop-parity, multi-agent 01 to 11) and fixed what had gone stale.

- **ADR 0013:** the Status no longer says most routes are planned (all landed but ticket 09); the impossible list points the shell surfaces at ADR 0015; the consequence about the skill's keys rule names the tickets that replaced it.
- **ADR cross-links:** ADR 0007 now notes ADR 0014's amendments (one press stops every agent; the hook's yield to the user's keys and mouse, with a WH_MOUSE_LL hook). ADR 0010 notes the Agent Cursor per Agent. ADR 0011's Status names its later amendments (headless-max 19 with desktop-parity 23, headless-max 03), which were only at the end of the file.
- **CONTEXT.md:** the stale "Planned" Posted Clicks entry (a duplicate of the landed one) is gone; Agent and Lane drop "Planned"; Headed Action says ADR 0013 narrowed the set and leaves out Chromium and Electron Windows and Posted Clicks from the double, right and middle clicks; Headless Action says Element Actions reach a minimized Window except a Store App's.
- **SKILL.md:** two contradictions fixed: a browser Window without a Browser Connection said typing needs `--headed` (ticket 20 sends it as window messages), and the clipboard paste said `Control_L+v` is Headed (it takes the key routes). The impossible list already matched ADR 0013; it stays the agent-facing subset (IME, a WebView2 attach and the self-activation guarantee are covered where the agent meets them). 55 KB.
- **README:** the headless paragraph says what runs in the background now (menus, shortcuts, text selection, named fields, minimized Windows, Chromium and Electron messages), points at ADR 0013's list for the rest, and says how a Headed Action behaves (it waits, gives the foreground and pointer back, stops when the user touches the keyboard or mouse).
- **docs/codex-parity.md:** reworded, statuses kept: the double, right and middle click refusals leave out Chromium and Electron Windows; press-key names the shortcut lookup; activate-window no longer says Headless Actions refuse every minimized Window; polite real input (multi-agent 08) in activate-window, process model's notes and the Esc stop. `go test ./internal/skill/...` passes.

### 2026-11-03: refreshed after desktop-parity 07, 18 and 24 and headless-max 09

Merged main c017f4c. These branches updated their own docs; the pass adds: ADR 0013's Status says every route has landed, and its impossible list says the tray overflow and the notification panel open only with `--headed open-shell` while a toast's own buttons are Headless; codex-parity's perform-secondary-action delivery row says where ContextMenu is Headless and that a WPF Element refuses it; README's headless sentence names right-click menus and a notification's buttons. CONTEXT.md's merge keeps both sides: the toast commands and the WPF ContextMenu from main, the narrowed Headed set and the minimized sentence from this pass. The SKILL.md result-phrase pass waits for the e2e-live-fixes branch (`ok (...)` becoming `ok` plus `Note:`).
