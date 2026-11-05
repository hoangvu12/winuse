# ADR 0019: The opened-Window settle is watched, not a fixed wait

## Status

Accepted (speed ticket 01, `.scratch/speed/issues/01-event-driven-opened-settle.md`,
2026-11-05), from research recommendation 2(a) in
`.scratch/research-speed/report.md`. It changes how long the report of
desktop-parity 02 (`opened.go`) waits, not what it reports. The live check
is still to run.

## Context

After click, press-key, set-value, invoke-menu, perform-secondary-action
Expand or Select and a Batch, winuse reports the dialogs, menus and popups
the Action opened and whether the target closed. It found them by taking a
snapshot of every visible top-level window every 50 ms for 300 ms, and it
stopped early only when something changed. Most Actions open nothing, so
most Actions waited the whole 300 ms (plus six enumerations) after they
were done. In a Headless click that is the largest fixed wait after the
Agent Cursor's glide, which stays as it is (ADR 0018).

Waiting less in a fixed way would miss a dialog that shows late. An event
alone cannot end the wait either: "no window showed yet" says nothing about
whether one is about to. The wait can end only when there is evidence that
the App has finished handling the Action.

## Decision

1. **The diff stays the detector.** The settle takes the same snapshots
   and renders the same notes as before (`openedChanges`,
   `formatOpened`). Its looks are at 50 ms, 70 ms, then every 50 ms to
   300 ms: the polled settle's looks, plus one.
2. **A WinEvent hook wakes it.** Before the Action runs, winuse hooks
   `EVENT_OBJECT_CREATE` to `EVENT_OBJECT_HIDE`, out of context, for the
   target's process only (`win.AppWatch`). A top-level window of that
   process that shows, or the target hiding or being destroyed, makes the
   settle look at once rather than at its next look. A created top-level
   window that has not shown yet is pending. These are window-manager
   events, not the UI Automation event subscriptions ADR 0004 rules out,
   and they live on a pumping thread of their own, not the UI thread the
   Esc keyboard hook shares (ADR 0006).
3. **It stops when the App is quiet.** After each look from the second
   on, winuse samples the process's threads (one
   `NtQuerySystemInformation(SystemProcessInformation)`, about 3 ms). The
   settle ends early when, since the sample before, no thread of the
   process ran (same threads, same context-switch counts), none is running
   or ready to run, the target's UI thread waits for window messages
   (`WrUserRequest`), and no window the App created is still pending.
   Then the Action's input has been handled. A dialog or menu that its
   handler opened is already in the snapshot, and a window still being
   built is pending. Any doubt keeps the looks going to 300 ms, as before.
4. **Where the watch cannot follow, the settle stays polled:**
   - Chromium and Electron Windows (`Chrome_WidgetWin_*`): their page and
     popups come from other processes, so the UI thread can be idle while
     a renderer works.
   - Shell surfaces: Start and the Action Center show from their own hosts.
   - Page Actions with a new-Tab probe.
   - An App whose process or UI thread cannot be read, or whose hook does
     not install.
5. **A UWP App** is watched through its content process and the
   CoreWindow's thread, not ApplicationFrameHost.

## Consequences

- **When nothing opens, an Action on a quiet App returns about 230 ms
  sooner** (offline wall-clock benchmark: the settle takes 74 ms instead
  of 302 ms) and takes 2 snapshots instead of 6. A busy App (an
  animation, a media player, a busy Explorer) still waits the full
  300 ms.
- **A Window that shows is reported as soon as it shows,** not at the next
  50 ms look.
- **Residual risk:** a Window shown by a timer or an async continuation
  after the App went quiet for 20 ms, but before 300 ms, is no longer
  reported. In that window of time the App's threads did not run at all.
  The agent's next Action on the main Window still meets the modal
  refusal it met before desktop-parity 02.
- **The self-activation check** (ADR 0011 amendment, 2026-11-03) still
  runs after the settle. It also still looks again 50 ms later when a
  Window opened.
- **Per Action:** one hook install and removal on the watch thread and up
  to one sample per look. All of it is read-only: nothing is sent to the
  App.
