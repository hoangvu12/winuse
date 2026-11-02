# 06: Accelerator lookup route for press-key

**What to build:** `press-key <window> <chord>` first looks in the Window's UIA tree for an Element whose AcceleratorKey matches the chord, and Invokes it. Headless, with no flag, for every UIA framework. Otherwise the existing routing runs (classic dialog Return/Escape, then Headed).

cua does this (`tools/hotkey.rs:262-300`). It is verifiable through the Element and needs no keyboard state.

- Match AcceleratorKey case-insensitively after normalizing ("Ctrl+S", "Ctrl+Shift+S"; key names through `internal/keys`).
- Search menus that are already open, the menu bar, ribbons and toolbars. Bounded walk under the request budget.
- Zero matches: fall through to today's routing. Two or more enabled matches: fall through as well, and never guess.
- Disabled Element: refuse with "nothing was sent", naming the Element. Do not fall through to real keys, which would also do nothing.
- Classic menu bars do not expose their items until opened. Ticket 08 resolves these by HMENU; this ticket can call it once 08 lands.
- Output: `ok (invoked "Save" through its shortcut)`.

**Blocked by:** none (uses 08 for classic menus once it exists)

**Status:** needs-triage (one more live-Window route in ADR 0011 Decision 4; CONTEXT.md "Headed Action" lists keys)

- [ ] AcceleratorKey search with normalization
- [ ] Routing: lookup before the existing press-key routes; Batch probe picks it up
- [ ] Offline tests: match, ambiguous, disabled, none
- [ ] Opt-in e2e: Ctrl+O on Notepad and WordPad behind a covering Window
- [ ] SKILL.md keys section
