# The taskbar, Start, the tray and toasts

Read when: the task touches the taskbar, Start, Search, the tray or hidden icons, quick settings, the notification panel or toast notifications.

This is one topic of the winuse skill. `winuse skill shell` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

### The taskbar and the shell

`--shell` adds the shell's surfaces; the taskbar's line is titled
`Taskbar`. Name a shell surface by its Window ID only, never by its App
(Windows Explorer). `get-window-state` reads the taskbar Headless: Start,
Search, the notification area (every one of its buttons has the ID
`SystemTrayIcon`: the hidden-icons chevron, the clock, network, volume,
battery, Show Desktop) and one button per running App.

- Every click, `set-value` or Secondary Action on a shell Element is
  refused without `--headed`, with nothing sent: an App's taskbar button
  activates the App, Start, Search and the tray buttons open flyouts, and
  overflow icons and the items of an open Start or Action Center may
  activate an App. To bring an App forward, use
  `winuse activate-window <window>`; the refusal names the Window. With
  `--headed` these change the user's screen, so tell the user first; the
  note after `ok` says where the foreground went.
- `winuse --headed open-shell start|notifications|quick-settings|overflow`
  opens one and prints its line, to read next; Start and the overflow keep
  the foreground. Some setups ignore the injected Win+N or Win+A: then it
  fails with "did not show", and you ask the user to open the panel.
  `winuse press-key <overflow> Escape` closes the overflow Headless; the
  foreground then stays on the taskbar.
- Toasts: `winuse list-notifications [--app <a>] [--text <t>]` numbers
  the showing banners, `notification-action <n> <button>` and
  `notification-dismiss <n>` press them Headless (refused when it
  changed), `wait notification [--app] [--text]` waits for one. A banner
  lasts about 5 s: act at once. Older ones: `open-shell notifications`.
