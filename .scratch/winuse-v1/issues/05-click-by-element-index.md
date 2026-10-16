# 05 — Click by Element Index

**What to build:** `winuse click <window> <element index>` clicks an Element (single, double, right click), at its clickable point or bounds centre, scrolling it into view if needed. Every Action first brings its target Window to the foreground (with the standard foreground-lock workarounds) and verifies it is in front before injecting input via SendInput; `activate-window` exposes this on its own. Actions aimed at an administrator Window or a locked desktop are refused with explanatory errors. An Element Index whose Element no longer exists fails as stale. Establishes the Codex-style output contract for all Actions: `ok` or a one-line failure with a hint, non-zero exit on failure, Codex timeouts. See spec: Acting details, Output contract.

**Blocked by:** 03 — Window State text tree

**Status:** ready-for-agent

- [ ] Single, double and right click on an Element by Element Index
- [ ] Target Window is activated and verified in front before input; `activate-window` works standalone
- [ ] Administrator Windows are refused with an explanation (no input sent)
- [ ] Locked desktop is detected and refused
- [ ] Stale Element Index fails with an instruction to read the Window State again
- [ ] Output is `ok` or a one-line failure; failures exit non-zero
- [ ] Offline tests cover the output contract and error messages
- [ ] End-to-end: clicking the fixture's button and checkbox changes fixture state; the "Save As" dialog appears as a new Window (verified by user run)
