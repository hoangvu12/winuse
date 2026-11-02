# 10: close-app

**What to build:** `close-app <app>` posts WM_CLOSE to every top-level Window of the App and waits for the process to exit (up to a few seconds). agent-desktop has `close-app` with `--force` and a protected-process list (`ad:crates/windows/src/system/app_ops.rs:7-17`).

- Headless: posted WM_CLOSE, as `close-window` does.
- An App still running after the wait: `ok (the close was requested; <App> is still running, it may be asking to save)`, like `close-window`.
- Packaged apps: the same, per Window.
- Refuse administrator Apps and a locked desktop with "nothing was sent".
- **No `--force`** unless the user decides otherwise: SKILL.md says "winuse never ends a process" (`skills/winuse/SKILL.md:326-327`). If `--force` is wanted, copy agent-desktop's protected list and TerminateProcess only the App's own processes.

**Blocked by:** none

**Status:** needs-triage (decide on `--force`; SKILL.md rule)

- [ ] App-to-Windows resolution, posted closes, exit wait
- [ ] CLI, protocol, helper
- [ ] Offline tests: all closed, still running, admin refusal
- [ ] Opt-in e2e: two fixture Windows closed by one command
- [ ] SKILL.md
