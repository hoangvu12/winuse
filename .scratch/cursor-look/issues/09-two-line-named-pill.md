# 09: A named Agent's pill shows its name over the action

Status: done

Asked by the user after issue 06; amends ADR 0018 Decision 8.

**Asked.** One pill, two lines when the Agent has a name: the name, smaller
(about 10.5 pt), semibold, in the Agent's colour lightened to read on the
pill; the action text below at today's size. No more
`reviewer: click "Save"` on one line. The default Agent keeps its one action
line exactly as before. The name line shows whenever the Agent has a name
(not cua's "on hover or with two or more cursors"). The pill keeps its
place, reveal timing, colours, maximum width and per-monitor DPI; each line
is cut on its own.

**Done.**

- `win.AgentCursor.TravelAs(anchor, origin, target, name, label)`;
  `TravelAbove` is it with no name. The helper's `anchoredCursor` passes the
  Agent's name instead of joining it into the label (`internal/helper/cursors.go`).
- `cursorLook.Name`; `cursorUI.lookFor` sanitizes and ellipsizes the name
  (at `cursorPillNameFontPx`, 10.5 px) and the action (12.5 px) each to the
  pill's width; a name with no action stays the one line, as before.
  `pillSize2` / `pillHeight2`: the wider line plus padding, capped like one
  line; 4.5 + 14 + 17 + 4.5 px high. The corners keep the one-line pill's
  radius (`pillRadius`). `layoutCursorWindow` stacks `Name` over `Text`.
- `cursorWin.drawLine`: each line in its own face size, colour and
  alignment; a named pill's lines are left-aligned, the name in
  `nameColor` (the Agent's colour mixed 72 % toward white), both over a
  1 px dark text shadow at 45 % (`putTextShadowed`), since the name sits on
  the pill's light end, itself mixed toward the Agent's colour. The default
  Agent's one line is drawn exactly as before (centred, no shadow).
- `measureLabel` takes the font size.
- Tests: `TestNamedAgentsPillHasTwoLines`; helper `TestCursorPerAgent…`
  assertions now check the name and the action separately.
- Contact sheet re-rendered (`.scratch/cursor-look/cursor-sheet.png`, label
  section): the default Agent, then named Agents with short and long names
  and actions, at 100 % and 200 %, on dark and light. The README GIF shows
  only the default Agent's one-line label, so it is unchanged.

`TestUsersTabComesBackWhenTheGuardMisses` (background-tabs) fails about one
run in five on main as on this branch; it is unrelated.

## Comments
