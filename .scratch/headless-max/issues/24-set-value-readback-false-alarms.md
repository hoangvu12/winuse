# 24: set-value read-back false alarms

**What to build:** two set-value read-backs in the 2026-11-03 Task Suite run (`../../../tasksuite/results/2026-11-03-claude-opus-5-5.md`) reported a mismatch although the write worked. Both said "do not repeat", which was right, but the agent spent steps checking.

- Explorer rename: set-value on the rename box read back `""`. The box closes when the rename commits, so the read finds an empty or gone field. Treat a field that closes after the write, with the item now named as asked, as delivered and verified (read the item's name instead), or say so in a Note.
- Notepad multi-line note: the read-back saw CR where the value had CRLF. Compare text with line endings normalised (CRLF, CR and LF are the same line break) before reporting a mismatch.

**Blocked by:** none

**Status:** done (the Explorer false alarm not reproduced live; see the 2026-11-03 live comment)

- [x] Normalise line endings in every text read-back comparison
- [x] Explorer rename: a closed rename box plus the item's new name counts as verified
- [x] Offline tests for both
- [x] Live: a multi-line set-value into Notepad (a test-launched copy) and a rename in an Explorer window of the test's own report a plain ok

## Comments

### 2026-11-03: from the Task Suite run (orchestrator 2)

Found by b6e5189e in the transcripts; approved by the user ("YES").

### 2026-11-03: built (branch hm24-readback)

- **Line breaks:** new package `internal/linebreak` (`Normalize`, `Same`: CRLF, CR and LF are one line break, two breaks are still two). Every text read-back compares through it: the Headless set-value (`uia.verifyValue`), the caret splice (`uia/textsplice.go`), the Chromium typed read-back (`uia.VerifyTyped`, which had its own inline copy) and the Page value read-back (`page.ValueMatches`: a text area stores CRLF as LF; editable text already normalised CRLF but not a lone CR). A password field is compared the same way and still never echoed.
- **Explorer rename:** the transcript shows the write went to the file's list item itself, not a rename box: Explorer's list items expose their name as a settable Value, and setting it renames the file. The item is rebuilt, so the Element winuse wrote to reads `""`. `SetValueHeadless` now reads the Element's raw-view parent before the write; when the read-back fails with `""`, it looks among the parent's control-view children for one whose Name or Value is the new name, and that is the read-back (`uia/renamed.go`). A password field's withheld read never triggers it. The rename box case (an `edit` under the item) already printed a plain ok in the run.
- **Offline tests:** `linebreak` (the forms, two breaks are not one), `uia.readEmpty` (which failures look for the renamed item), `uia.namedAs`, `page.ValueMatches` (CRLF as LF in a text area, CR as LF in editable text, a missing break still a mismatch).
- **Live:** `test/e2e/readback_test.go`: a multi-line set-value into a test-launched Notepad on a temp file, and a rename in an Explorer window the test opens on a temp folder (closed with close-window, never killed); both want a plain ok.

### 2026-11-03: live

- `TestMultiLineSetValueIntoNotepadReadsBack` passes with the fix and fails on main 68568e3 with the run's false alarm (`reads "...

Opens at 9:30.
..." afterwards, not "...

..."`): Windows 11 Notepad's editor reads CRLF back as CR.
- `TestRenameInExplorerReadsBack` passes with the fix, and on main too: the false alarm did not reproduce, neither with one file nor with the run's layout (four files, the renamed one moving in the sort). Whatever rebuilt the item in the Task Suite run (the folder view, timing, the file open elsewhere) was not recreated. The sibling check stays as the fallback for an item that reads `""`, covered offline in its decisions; the live test pins that a rename prints a plain ok.

### 2026-11-03: review fix (orchestrator 3)

The sibling fallback could pass a write that did not take: a sibling already named the value (a label "Name" beside the edit set to "Name", another file already showing the name) would read as the renamed item. `SetValueHeadless` now checks the siblings before the write too, and the fallback counts only a name that is new (`uia.renamedInPlace`, offline-tested: found, already there, not found, another value read, no mismatch; the siblings are read again only when the name was new). The rename e2e registers its cleanup before Explorer starts and closes every window titled with its folder, asking up to three times, so a failing run leaves no window open.

