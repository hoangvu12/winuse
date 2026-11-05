# 14: The extension's silent idle re-attach drops a Tab session's setup

Status: needs-triage

**Found while doing:** issue 07 (`07-page-read-burst.md`, "The extension
path"). Not caused by it. 07 only works around the part it owns, the DOM
and Accessibility domains.

## What happens

- The winuse extension detaches the debugger from a Tab nobody drove for
  5 minutes (`internal/bridge/extension/background.js`: `IDLE_DETACH_MS`,
  `touch`, `detach`).
- Its next command on that Tab attaches again transparently
  (`sessionCall` calls `ensureAttached`). The session id stays the same,
  because the extension's session id is the Tab's target id.
- `detach` forwards `Target.detachedFromTarget` for the Tab's iframe
  sessions only (`dropChildren`). `chrome.debugger.onDetach` does not fire
  for a detach the extension made itself, so winuse is never told that
  the Tab's own session went away.
- winuse's `page.Reader` keeps its cached `tabSession`, and its `TabWatch`
  keeps its state. Everything they set up on the old attachment is gone.

## What is lost, and what it does

Read from the code; not observed live.

| Set up by | On the old attachment | After the silent re-attach |
|---|---|---|
| `Reader.session`: `Target.setAutoAttach {flatten}` | Cross-origin iframes reported and attached | No `attachedToTarget` comes. `ts.children` was emptied by `dropChildren`'s events, so cross-origin iframes are left out of every later read with no note, until the session is dropped (a gone error, the helper's restart). |
| `browser.AttachPage`: `Emulation.setFocusEmulationEnabled` | A background or covered Tab keeps rendering and takes synthetic input | Off. Input to a background Tab may be dropped silently, the problem the call exists for (ADR 0017's hold). `HoldTab` turns it on again only for a Tab target Action. |
| `page.Watch`: `Page.enable` | `javascriptDialogOpening` and `fileChooserOpened` arrive | No events. A dialog opened afterwards is not seen. The read's `Ready` probe still fails within `AnswerTimeout`, but as "not answering" rather than at once with the dialog's text, and `dialog` Actions do not know the dialog. |
| `TabWatch.Intercept`: `Page.setInterceptFileChooserDialog` | Held during an Action | Watch state `on` may say interception is on while the new attachment has it off, so a chooser could open natively. Only if a hold spans the 5-minute idle; unlikely. |
| `DOM.enable`, `Accessibility.enable` | On | Handled by issue 07: a read more than `enableAgain` (4 min) after the session's last read turns them on again. |

## Options to triage

1. **The extension tells winuse.** On an idle `detach`, also forward
   `Target.detachedFromTarget {sessionId: targetId}` for the Tab itself, as
   `onDetach` does. winuse's client then fails the session's pending calls
   (`ErrSessionDetached`); the Reader's `isGone` drop-and-re-attach runs
   on the next read; and a Tab-target Action re-attaches. This changes the
   extension, so every user has to reload it (the Build hash shows it). An
   Action between the detach and the next read would also need the
   re-attach path, so check every `isGone` caller.
2. **winuse re-applies on a clock.** Generalize issue 07's `enableAgain`:
   past 4 minutes since the session's last use, re-run the session's setup
   (auto-attach with its settle, focus emulation, `Page.enable`) before the
   command. No extension change. It needs a "last used" stamp shared by
   reads and Actions, and is fragile if `IDLE_DETACH_MS` changes.
3. **The extension never idles a session winuse holds.** Keep the timer
   but skip `detach` while a connection lists the Tab. That loses the point
   of the idle detach: taking the "started debugging" bar away.

Leaning: 1, with 2's clock kept as a fallback for an older extension.

## To confirm live

- Through the extension, read a Tab with a cross-origin iframe, wait 5 or
  more minutes with no command on it, then read again. Expected today: the
  iframe's Elements are missing from the second read.
- In the same state, open an `alert()` from the page, then read. Expected
  today: "not answering" after `AnswerTimeout`, not the dialog's text.

## Comments
