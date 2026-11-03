# 24: set-value read-back false alarms

**What to build:** two set-value read-backs in the 2026-11-03 Task Suite run (`../../../tasksuite/results/2026-11-03-claude-opus-5-5.md`) reported a mismatch although the write worked. Both said "do not repeat", which was right, but the agent spent steps checking.

- Explorer rename: set-value on the rename box read back `""`. The box closes when the rename commits, so the read finds an empty or gone field. Treat a field that closes after the write, with the item now named as asked, as delivered and verified (read the item's name instead), or say so in a Note.
- Notepad multi-line note: the read-back saw CR where the value had CRLF. Compare text with line endings normalised (CRLF, CR and LF are the same line break) before reporting a mismatch.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Normalise line endings in every text read-back comparison
- [ ] Explorer rename: a closed rename box plus the item's new name counts as verified
- [ ] Offline tests for both
- [ ] Live: a multi-line set-value into Notepad (a test-launched copy) and a rename in an Explorer window of the test's own report a plain ok

## Comments

### 2026-11-03: from the Task Suite run (orchestrator 2)

Found by b6e5189e in the transcripts; approved by the user ("YES").
