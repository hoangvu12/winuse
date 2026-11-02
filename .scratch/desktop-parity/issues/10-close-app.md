# 10: close-app

**What to build:** `close-app <app>` posts WM_CLOSE to every top-level Window of the App and waits for the process to exit (up to a few seconds). agent-desktop has `close-app` with `--force` and a protected-process list (`ad:crates/windows/src/system/app_ops.rs:7-17`).

- Headless: posted WM_CLOSE, as `close-window` does.
- An App still running after the wait: `ok (the close was requested; <App> is still running, it may be asking to save)`, like `close-window`.
- Packaged apps: the same, per Window.
- Refuse administrator Apps and a locked desktop with "nothing was sent".
- `--force` (user decision, 2026-11-03): TerminateProcess the App's own processes after the posted close, as agent-desktop does. Refuse agent-desktop's protected list (csrss, wininit, winlogon, services, lsass, smss, lsaiso, dwm, explorer). Reword SKILL.md's "winuse never ends a process" (`skills/winuse/SKILL.md:326-327`): only `close-app --force` does.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] App-to-Windows resolution, posted closes, exit wait
- [ ] CLI, protocol, helper
- [ ] `--force` with the protected list
- [ ] Offline tests: all closed, still running, admin refusal, `--force`, protected refusal
- [ ] Opt-in e2e: two fixture Windows closed by one command
- [ ] SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop, `--force` and the protected-process list included.
