# 16: `status`

**What to build:** `winuse status` prints, one line each:

- winuse and helper versions, whether the helper is running, its uptime;
- input desktop state (locked or not, `internal/win/desktop.go`);
- the helper's integrity level (`internal/win/integrity.go`);
- DPI awareness;
- Browser Connections (a one-line summary from `browser status`);
- the Windows build.

agent-desktop has `status` and `permissions` (research section 1, System rows). Starts the helper if it is not running? No: `status` reports "not running" and starts nothing.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Command in CLI; helper part when running
- [ ] Offline tests of the output with fakes
- [ ] SKILL.md troubleshooting line
