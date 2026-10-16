# 03 — Window State text tree

**What to build:** `winuse get-window-state <window>` prints the Window State as Codex's depth-first, indented, numbered tree (role, name, AutomationId, basic state). The helper reads the tree through UI Automation (hand-written COM vtable calls on one OS-locked, COM-initialised thread, using cache requests scoped to the Window) within a time limit, returning the partial tree with a note on timeout. The helper keeps the live Elements behind the latest Window State so later Actions can resolve Element Indexes. A Window can be named by Window ID, or by App name only when that App has exactly one Window (otherwise fail listing candidates). Includes a recorder that saves a real tree as a fixture file and the first seam-2 offline tests of rendering. No background UI Automation event monitoring. See spec: Observation details, Process model.

**Blocked by:** 02 — Fixture app + opt-in end-to-end tests

**Status:** ready-for-agent

- [ ] Output follows Codex's tree format; indexes are depth-first and stable within one Window State
- [ ] Reading a new Window State for a Window replaces the previous one in the helper
- [ ] Tree reads are bounded by a time limit; a timeout returns the partial tree plus a note
- [ ] Naming a Window by App resolves only when exactly one Window matches; otherwise an error lists the candidates
- [ ] Recorder captures a real Window's tree to a fixture file
- [ ] Offline tests render recorded trees and compare with expected text
- [ ] End-to-end: the fixture's Elements appear with expected roles, names and IDs (verified by user run)
