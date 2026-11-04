# 04: A lane per Tab, no cursor in the background, bounded screenshots

Status: done

Spec decisions 6 and 7, and item 6 of the request (ADR 0017 Decisions 5
and 6).

**Done.**

- `tabLane` (`lanes.go`): `3 tab <target id>`, after desktop, threads and
  Pages; `lanesFor` gives a Tab target that one lane for every command,
  a Batch included. Commands without a Tab target keep the Window's Page
  lane.
- Agent Cursor: `cursorTravelRect` and `pageScreenRect` show nothing for a
  Tab target in the background; one that is its window's selected Tab
  (the extension's active flag) gets the cursor at the content area.
  `runPageWindow` skips its cursor target read then.
- `--screenshot` on a Tab target: `page.CaptureTab`
  (`Page.captureScreenshot` JPEG from the surface), bounded by
  `tabShotWait` (3 s), scaled like a Window's screenshot and saved; a
  capture that does not come prints a Note and the tree. No `@x,y` mapping
  is recorded.
- Error codes: rules for the new failure sites (`protocol/codes.go`).
- Tests: `TestTabTargetsTakeALaneEach` (one Tab's click blocked on its
  press while the other Tab's click completes), `TestTabTargetScreenshot`.

## Comments
