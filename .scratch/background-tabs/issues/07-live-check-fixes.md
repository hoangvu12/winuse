# 07: Fixes from the first live check (Brave, build fc7ad2d)

Status: done

Live checks 1, 2 (select) and 4 passed. Three failures:

**1. A target=_blank link's Tab stayed selected.** `onCreated` keyed the
guard on `openerTabId` being a held tab; a target=_blank link is noopener,
and the browser can leave `openerTabId` out, so the guard never fired.

- `background.js` `openerOf`: the held opener, else, for a tab with no
  opener whose URL is a site's (not a browser page such as Ctrl+T's), the
  held tab a command is acting on in that window (`acting`, set by every
  `holdTab`, `ACTING_MS` 10 s). `adopt` groups or minimizes;
  `Winuse.adoptTab` does it on winuse's request.
- winuse's own check (`guardUsersTab`, `tabtarget.go`), run around every
  Page Action on a background Tab target through the extension: it notes
  the user's selected Tab before the input, then watches the window for 3 s;
  a new site Tab that became selected while the user's Tab is open is
  answered with `Winuse.activateTab` on the user's Tab and `adoptTab`. A
  Tab the user switched to themselves (one open before, or a browser page)
  ends the watch.
- `selected` is seeded by every `holdTab`, so a restarted worker knows the
  user's Tab again at the next command.
- Tests: `TestTabTargetLinkWithoutAnOpener`,
  `TestUsersTabComesBackWhenTheGuardMisses`,
  `TestTheUsersOwnSwitchIsLeftAlone`, `TestExtensionIdentity`.

**2. A date input had no settable Element.** Its CDP roles (`date`,
`dateTime`, `inputTime`, `colorWell`) were not in the role table, so it
rendered as a wrapper and its shadow spinners and picker button showed.

- `roles.go`: those roles are an `edit` of a new kind `leaf` (a line whose
  children are not), so the input is one `edit (settable, string)` Element
  and set-value writes it through the DOM (`picker` choice).
- The choice, picker and native-picker JS map a part of an input's shadow
  tree to the input, so a spinner or the picker button from an older read
  sets the input, and a click on the picker button in a background Tab is
  refused as "a date input".
- `aimJS`: a hit test that lands on the shadow host of the node is not a
  cover (the misleading "covered by <input id=d>" on the active Tab).
- Tests: `TestPickerInputsAreOneSettableField`,
  `TestPickerPartsStandForTheirInput`, the date input in
  `TestTabTargetReadsABackgroundTab` and `TestTabTargetNativePopups`.
- Left as is: an ARIA `role=spinbutton` that is no `<input>` still reads
  `settable, float` though set-value cannot write it.

**3. After replacing winuse.exe, the extension stayed unreachable.** The
running host's exe had been renamed (winuse.old.exe); Windows then names
the process by the new name, `bridge.HostAlive` compared it with the
recorded path, took the host for dead and deleted its bridge file. The host
kept the extension's port, so no new host started and no winuse saw it.

- `bridge.SameHostImage`: a winuse binary under another name is still the
  host.
- The host writes its bridge file again when it disappears
  (`HostConfig.Republish`, 5 s).
- `ext status` ends a stray host (a winuse process a browser started that
  no bridge file names, `bridge.NativeHosts`/`EndHost`), waits up to 35 s
  for the extension to connect again, and notes it; when it does not come
  back, `ExtStrayHost` says what happened and what to run.
- `background.js`: a host that ran over 5 s and went away is started again
  after 1 s instead of at the next 30 s alarm.
- Tests: `TestSameHostImage`, `TestHostRepublishesItsBridgeFile`,
  `TestExtStatusEndsAStrayHostAndWaitsForTheExtension`,
  `TestExtStatusNamesAStrayHostThatDidNotComeBack`,
  `TestExtStatusLeavesAHostWithABridgeFile`, the problem table.

## Comments

2026-11-04, second live check (d99e69a): the upgrade, target=_blank and date input fixes pass on Brave. A Tab target that names the Window twice (`W:W:tab`) is now refused with nothing sent and the right form in the hint (`TestTabTargetRefusals`).
