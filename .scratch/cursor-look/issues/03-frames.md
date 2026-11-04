# 03: About 120 frames per second while animating, nothing when hidden

Status: done

Spec decision 3 (ADR 0018 Decision 4).

**Done.**

- `cursorFrameInterval` is 8 ms. While the model animates, the view holds
  `timeBeginPeriod(1)` (one count across every Agent's cursor,
  `holdFastFrames`, cua's overlay.rs pattern) and runs a frame clock: a
  goroutine with an 8 ms `time.Ticker` posting `wmCursorFrame` to the
  arrow window, coalesced by an atomic flag so a busy UI thread is never
  handed a backlog. Measured: `SetTimer` alone stayed at 64 frames per
  second on this Windows 11 even with the resolution raised; the clock
  gives 124–126.
- Resting: the clock stops, the resolution is given back, and a `SetTimer`
  timer wakes for the float (30 ms) or the idle deadline; hidden, nothing.
- Cost cuts found by `BenchmarkCursorFrame` and the live measurement:
  pressed sprites kept in 0.005 steps; the reveal's dimming in integers;
  the outline's glow tabled (no `math.Erf` per pixel) and its pop redrawn
  in 1/250 steps; a layered window whose place, opacity and pixels did not
  change is not presented again.
- `WINUSE_CURSOR_TRACE=1` also logs each animation's frames, rate and
  drawing time (`cursorUI.traceFrame`).
- Measured (`TestAgentCursorFrameRate`, this machine, 100 %): 124–126
  frames per second; drawing 0.47 ms per frame steady (0.9 ms over the first
  travels, while the master image and sprites are made); process CPU about
  12–15 % of one core while animating, 0.4–0.8 % floating, 0 hidden.

## Comments
