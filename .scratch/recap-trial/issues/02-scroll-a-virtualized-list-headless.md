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

## Comments

### 2026-11-24, agent rt-02: diagnosis and fix (branch `agent/rt-02`)

**What Recap exposes** (read-only probes through UIA, System.Windows.Automation
from PowerShell, and winuse):

- Framework: not WPF/WinUI/WebView2/Electron. FrameworkId `zgx`, window
  class `Window Class`: a Rust UI toolkit (zgx, Luau scripts in
  `Programs\Recap\ui`, winit + wgpu) whose UIA tree comes from AccessKit
  (`accesskit_windows`). The transcript is `ui.list { scrollbar = true }`,
  a virtualized list (`screens/notes/transcript.luau`).
- The list `pane` offers **no pattern at all**: no ScrollPattern, no
  ItemContainer. Nothing above it scrolls either (the window has
  Window and Transform only). So `scroll` found no ScrollPattern up the
  chain: its refusal was accurate, but the only check it made.
- Every row (`button <speaker> <time> <text>`) and every part of a row
  offers Invoke and **ScrollItem**; no VirtualizedItem. Rows outside the
  view are in the tree with bounds past the pane's edge (IsOffscreen reads
  false even for rows above the top), and the list builds more rows as it
  scrolls. `ScrollItem.ScrollIntoView` on the last built row scrolls the
  list Headless and builds the next rows.
- `Vertical scrollbar`: RangeValue only, **IsReadOnly = true**, Minimum 0,
  Maximum 3716 (pixels), SmallChange/LargeChange 0, empty bounds. 50 was
  in range; the write failed because the range is read-only. 0x80131509
  is UIA_E_INVALIDOPERATION (UIA reuses .NET's InvalidOperationException
  code), which a provider returns for a write to a read-only value.
  winuse renders every RangeValue as `settable, float`, so the line looked
  writable.

**General cause** (not Recap-specific): Element scroll by steps had one
route, a ScrollPattern on the Element or an ancestor. A list whose toolkit
exposes only ScrollItem on its items (AccessKit-based UIs, custom-drawn
lists) had no Headless route at all, and set-value on a read-only
RangeValue reported an unknown outcome instead of refusing.

**Fix**:

- `scroll <w> <ref> <dir> [n]`: when no ScrollPattern scrolls that axis up
  the chain, winuse looks for the nearest Element at or above the target
  (stopping at the Window's root, never the desktop) whose ScrollItem
  children reach past its bounds on that axis, and scrolls it one item per
  step: ScrollIntoView on the item nearest past the edge, read back by that
  item's bounds (a second item is tried once when the first did not move
  the list). Naming a row, a word in it, the list or a scroll bar inside
  it all find the list. No item past the edge at the first step refuses
  with "the list is already at its end ...; nothing was sent"; a first step
  that moves nothing is the existing delivered "did not scroll" failure.
  (`internal/uia/scrollitems.go`)
- `set-value` on a RangeValue reads IsReadOnly, Minimum and Maximum first:
  a read-only range refuses ("the Element's range value is read-only, so
  set-value cannot set it; nothing was sent", hint: scroll instead), a
  value outside known bounds refuses naming the bounds. A write the
  provider rejects whose value reads the same afterwards says "nothing was
  changed" (delivery not_delivered) instead of "may have taken effect".
  (`internal/uia/rangevalue.go`)
- `scroll ... into-view` already worked on Recap's rows (ScrollItem);
  unchanged. Its IsOffscreen read-back cannot catch a provider that never
  reports offscreen (Recap's), noted here, not changed.
- Not built: a scroll bar's sibling ScrollPattern, or stepping a writable
  RangeValue scroll bar; neither shape was found in Recap, and a scroll
  bar is normally a child of its scroll container, which the ancestor
  climb already covers.
- Tests: offline, generic trees (`scrollitems_test.go`: overflow and
  edge picking, a fake virtualized list for the step driver;
  `rangevalue_test.go`; helper wording and delivery). The fixture App gains
  `-list-without-scroll` (Item List without Scroll, items keep ScrollItem)
  and an e2e test `TestElementScrollThroughListItems` (ledger row added;
  not run by the agent).

**Live proof** (Headless, binary of this branch, `WINUSE_AGENT=rt-02`):

- Recap 595280, Transcript tab: the first read listed rows 00:00 to 01:53.
  `scroll 595280 "name:Vertical scrollbar" down 10` printed `ok` and the
  next read listed rows to 05:14 ("Me 05:14 Oh, I think I fixed it
  already."); after `down 10` again, to 06:04; `scroll 595280 "name:Speaker
  1 00:00 ..." down 50` reached the last row, "Speaker 1 09:49 Thank you.
  Thank you. ... Bye-bye." (the recording is 10:01). One more `down` was
  refused with nothing sent; `up 50` returned to the top. The screenshot
  after the scroll showed the transcript at 05:14-05:21.
- `set-value 595280 "name:Vertical scrollbar" 50` now: "the Element's range
  value is read-only, so set-value cannot set it; nothing was sent".
- Second App, the fixture launched in the background with
  `-list-without-scroll`: list `down 2` moved listFirstVisible 0 -> 2,
  `down 10` by an item ended at 3, `down` at the end refused with nothing
  sent, `up 3` back to 0; `left`/`right` refused (no horizontal overflow).
- Recap was left on the same meeting, Summary tab selected.
- Note: before the climb was bounded at the Window root, one probe on the
  fixture (`scroll ... Hotel left`) climbed to the desktop and called
  ScrollIntoView on another App's top-level Chromium window, which failed
  with E_FAIL (no visible effect). The bound is in the committed code.
