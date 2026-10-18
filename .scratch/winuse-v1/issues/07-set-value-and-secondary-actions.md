# 07 — Set value and Secondary Actions

**What to build:** `winuse set-value <window> <element> <value>` sets an Element's value directly; `winuse perform-secondary-action <window> <element> <action>` performs one of the Element's Secondary Actions (Expand, Collapse, Toggle, Select, Scroll Down, …). The Window State now lists each Element's Secondary Actions and state (toggled, selected, expanded, value), following Codex's format. See spec: Observation details, Acting details.

**Blocked by:** 05 — Click by Element Index

**Status:** done

- [x] Window State lines show Secondary Actions and Element state in Codex's format
- [x] `set-value` works on settable Elements; non-settable Elements fail with a hint to use `type-text`
- [x] Unsupported Secondary Actions fail listing the ones the Element offers
- [x] Offline tests cover rendering of Secondary Actions and state from recorded trees
- [x] End-to-end: set the fixture's text field, toggle its checkbox, expand its menu (verified by user run)

## Comments

- 2026-10-18 — live e2e run (Windows 11, 100% scaling, primary monitor, after the live-fixes merge into winuse-v1): TestSetValueSetsTheTextField, TestSecondaryActionTogglesTheCheckbox and TestSecondaryActionExpandsAndCollapsesTheMenu passed; the @x,y forms (TestSetValueByPoint, TestPerformSecondaryActionByPoint) pass since the fixture implements ElementProviderFromPoint.
