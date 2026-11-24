# 02: Recap's transcript list can't be scrolled Headless

Status: ready-for-agent
Type: task
From: a real session on 2026-11-24, reading the user's Recap App (`recap.exe`, `C:\Users\ADMIN\AppData\Local\Programs\Recap\recap.exe`)

## Problem

Recap's meeting window (Transcript tab) lists the transcript as `button
<speaker> <time> <text>` rows inside a `pane`, with a sibling
`scroll bar (settable, float) Vertical scrollbar`. Only the rows on screen
are in the tree, so reading the whole transcript needs scrolling. Both
Headless routes failed:

```
$ winuse set-value 595280 "name:Vertical scrollbar" 50
setting the Element's value failed (HRESULT 0x80131509)
hint: the Action may have taken effect; read the Window State (winuse get-window-state 595280) before repeating it
```
(nothing moved; 0x80131509 is .NET's InvalidOperationException)

```
$ winuse scroll 595280 16 down 10
neither the Element nor any of its ancestors scrolls vertically; nothing was sent
hint: ... winuse --headed scroll 595280 16 down 10
```

So the only route left was `--headed`, which takes the user's foreground.
The agent fell back to reading Recap's files on disk.

## Ask

1. **Diagnose** what Recap's UI exposes: the framework (WPF, WinUI,
   WebView2, Electron…), which Element carries `ScrollPattern` /
   `RangeValuePattern` / `ScrollItemPattern`, whether the list rows support
   `ScrollItemPattern.ScrollIntoView` or `VirtualizedItemPattern`, and why
   RangeValue SetValue throws. Probe read-only first; record findings here
   under `## Comments`.
2. **Fix** whatever is winuse's to fix, for example:
   - `scroll` finding a `ScrollPattern` on the scroll bar's sibling or
     parent pane, or on the scroll bar's owner, not only on ancestors of
     the named Element;
   - a scroll bar Element as the `scroll` target (`scroll <w> <scrollbar> down`)
     driving its owner's ScrollPattern, or RangeValue with a value inside
     its Minimum/Maximum (check whether 50 was out of range);
   - `set-value` on a scroll bar reporting a clear failure that says
     nothing moved when the readback shows no change, instead of "may
     have taken effect";
   - if no Headless route exists in this App, the refusal saying exactly
     why.
3. If a real Headless route exists, prove it on Recap: scroll the
   transcript of the meeting already open in Recap's meeting window and
   read rows that were off screen.
4. Tests: offline tests with a recorded or hand-built tree of this shape;
   the e2e fixture App if it can reproduce the shape.

## Constraints

- Recap is the user's real App, with their meetings. Headless only. Never
  `--headed`, never type, delete, rename, record, or change settings.
  Reading, selecting a tab and scrolling are fine. Leave the window on the
  meeting it shows.
