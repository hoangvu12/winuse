# 12: Select an option by name in UIA combos and lists

**What to build:** `set-value <combo-or-list> <label>` on a UIA ComboBox or List picks the item with that name. Page `<select>` lists already work this way. agent-desktop's `select` does a bounded descendant search (research section 1, Interaction rows).

- If the Element has a Value pattern that accepts the label (editable combo), keep today's route.
- Otherwise: Expand if collapsed (ExpandCollapse), find the item by exact name (then case-insensitive) in a bounded search, using ItemContainer.FindItemByProperty and VirtualizedItem.Realize for virtualized lists, `SelectionItem.Select`, Collapse, then read the selection back.
- Ambiguous names: refuse, listing the candidates. No match: refuse with "nothing was sent" except the Expand, which is collapsed again ("the list was opened and closed; no item named ...").
- Headless; `--headed` activates first, then the same pattern calls.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Route in `internal/uia` with read-back
- [ ] Offline tests: collapsed combo, virtualized list, ambiguous, missing
- [ ] Opt-in e2e: fixture combo box and list, covered
- [ ] SKILL.md set-value section
