# 04 — List and launch Apps

**What to build:** `winuse list-apps` shows Apps that are running or recently used, whether each is running, and its Windows. `winuse launch-app <name|exe path>` starts an App and reports the Window it opened, within Codex's launch timeout. See spec: Command surface (Discovery).

**Blocked by:** 02 — Fixture app + opt-in end-to-end tests

**Status:** done

- [x] `list-apps` lists running Apps with their Windows, plus recently used Apps marked not running
- [x] `launch-app` accepts an App name or an executable path
- [x] `launch-app` reports the new Window's ID, or a clear error on failure or timeout
- [x] Offline tests cover App name matching and output formatting
- [ ] End-to-end: launching the fixture by path yields its Window (verified by user run)
