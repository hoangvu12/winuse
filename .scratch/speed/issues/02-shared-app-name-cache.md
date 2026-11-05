# 02: One App-name cache for every window enumeration

Status: ready-for-human (done on branch `speed-waits`)

**From:** research recommendation 10(b) (`notes-local-perf.md` §6 and §9 #5).

**What to build:** `win.Collect` built a fresh FileDescription cache on
every call (`enum.go`, `newAppNames()`). Every distinct exe on the desktop
then re-read its version resource every time. A Headless click enumerates
about 11 times. The research measured 5.38 ms median per Collect with a
fresh cache and 1.19 ms with a shared one (31 visible windows, 14 exes).

- [x] `Collect` uses the process-wide `sharedNames`, which `AppName`
  already used. A path's name is read once per helper. An exe updated in
  place keeps its old name until the helper restarts (it idles out after
  5 minutes).
- [x] `BenchmarkAppName` (`internal/win/process_test.go`): 4 system exes,
  2.87 ms fresh vs 7.6 µs shared, so about 0.7 ms per distinct exe per
  enumeration.
- [ ] Not done: resolving the Window once per request. The 4 pre-Action
  resolutions now cost about 1.2 ms each; sharing them would need the
  snapshot carried through every wrapper.

**Saving per click (research's median figures):** 11 Collects × 5.38 ms
≈ 59 ms before; 7 Collects (ticket 01 drops 4 of the settle's) × 1.19 ms
≈ 8 ms after. That is about 50 ms per Action, and more at p90 (14.7 ms per
Collect).

## Comments
