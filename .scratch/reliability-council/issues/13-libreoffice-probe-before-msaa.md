# 13: LibreOffice and other SAL windows: probe before building an MSAA walker

Status: wontfix (user, 2026-11-07: LibreOffice not installed; skip for now)

**What's unknown:** winuse reads every Window's root with one combined call, ElementFromHandleBuildCache (`wm:internal/uia/capture.go:136`). Nobody has tried it on LibreOffice.

**cua:** a code comment says the combined call hangs on LibreOffice's SAL windows while the two-call path works, and cua routes those windows to an MSAA walker (`cua-w:uia/mod.rs:337-379`, `cua-w:msaa.rs`). Its empty-wrapper fallback walks the desktop by pid and marks the result incomplete, because a pid search finds other Windows' controls (`cua-w:uia/mod.rs:430-478`, `445-449`). Both are from comments, not measurements; cua's hang is on a whole-subtree read, which winuse doesn't do.

**What to build:**

1. Probe (on the user's OK, or on the CI runner from ticket 08): read a LibreOffice Writer window; time the root call and the walk.
2. If the root hangs, switch SAL classes to ElementFromHandle plus BuildUpdatedCache (size S).
3. Build an MSAA walker only if UIA can't read the content at all (size L). Never a physical click without `--headed`.
4. A pid-wide fallback, if ever, is discovery only: never a complete per-Window State.

**Blocked by:** none (08 would let it run off the user's PC)

- [ ] Probe result recorded
- [ ] Decision on 2 and 3 from the result

**Verified by the orchestrator:** cua's routing and its incomplete mark. Inferred: everything about LibreOffice under winuse.

**Conflicts:** ADR 0002 (own native layer: write it, don't import cua's). ADR 0010/0013 (Headless stays Headless).

## Comments

### 2026-11-07: not started (orchestrator)

- Nothing built: every step depends on the probe. LibreOffice is not installed on this PC (no swriter.exe, checked 2026-11-07), and the probe needs a live read on the user's desktop.
- Ticket 08's hosted-runner e2e lane, if the user approves it, could run the probe there instead.
- Asked the user: install LibreOffice and allow the probe, or skip the ticket.

### 2026-11-07: wontfix (user)

- user, 2026-11-07: LibreOffice not installed; skip for now. Nothing was built or probed. If LibreOffice is installed later, the ticket's step 1 (a probe of one Writer window) is still the way in; the hosted-runner e2e lane from ticket 08 could also run it there.
