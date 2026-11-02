# 04: `find` and a region read

**What to build:**

- `find <window> [--role <r>] [--name <text>] [--text <text>] [--state <s>] [--first | --count]`: prints the matching lines in the ADR 0004 grammar, with Element Indexes the agent can act on. On a role miss, list the roles present (agent-desktop's `roles_present`).
- `get-window-state <window> --root <element> [--depth N] [--interactive]`: reads one subtree.

agent-desktop: `find` with filters and shaping; `--skeleton` (depth 3 with child counts) and `--root REF` drill-down (`ad:docs/skeleton-and-drill-down.mdx:7-153`). winuse reads the whole tree every time, bounded by caps and a truncation note (`internal/uia/walk.go:101-103`).

Numbering decision (recommended by the research): `find` and `--root` print a fresh numbering of what they print and replace the live store for that Window, so "an index is valid only for the latest Window State" still holds. No snapshot-qualified refs.

- `find` searches the live tree with a bounded walk, not only the last printed state, so it reaches Elements past the truncation caps.
- `--skeleton` (depth 3, `(+N children)` markers) is part of this ticket.
- Pages: route `find` to the CDP snapshot with the same filters.

**Blocked by:** none

**Status:** needs-triage (ADR 0004 amendment for region numbering)

- [ ] Filters and output in CLI, protocol, helper
- [ ] Region read and skeleton in the walk
- [ ] Live store replaced by the region; stale messages unchanged
- [ ] Offline tests over recorded trees (`internal/wstate/testdata`)
- [ ] ADR 0004 amendment; SKILL.md "dense Windows"
