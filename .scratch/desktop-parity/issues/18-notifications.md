# 18: Notifications

**What to build:** `list-notifications`, `notification-action <id> <button>`, `notification-dismiss <id>`, and `wait notification [--app] [--text]`. agent-desktop has four notification commands plus `wait --notification` (research section 1, Notifications rows).

- Read toasts and the Action Center through UIA. A visible toast is its own Window (`Windows.UI.Core.CoreWindow`, "New notification").
- The Action Center must be open to list older ones: open it through ticket 09's route (Headless if the taskbar button Invokes cleanly, otherwise Headed).
- Identity guard (agent-desktop): an action re-reads the notification's App and text before acting and refuses if they changed.
- Alternative to probe: `Windows.UI.Notifications.Management.UserNotificationListener` (needs the user's permission and package identity; likely unusable from an unpackaged binary).

**Blocked by:** 01 (Desktop command term), 09 (shell surfaces)

**Status:** ready-for-agent (after 01 and 09; probe the toast and Action Center routes first, with the user away)

- [ ] Probe: toast and Action Center trees, Invoke on toast buttons, foreground before and after
- [ ] Commands and the identity guard
- [ ] Offline tests over recorded trees
- [ ] Opt-in e2e: a toast raised by the e2e test itself
- [ ] SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop: list, act, dismiss, `wait notification`, with the identity guard. Opening the Action Center follows 09: Headless first, the shell-accelerator route only with `--headed`.

### 2026-11-03: shell probe, partial (`../../headless-gaps/probes-round4.md` section 7)

Not measured: toasts are turned off on the laptop (ToastEnabled = 0), so no toast appeared, and the probe left the setting alone. The Clock OmniButton is the Action Center's entry point on Windows 11 (see dp-09). The toast and Action Center probe is still pending, on a machine with toasts on and with the user's OK.

### 2026-11-03: toasts measured (`../../headless-gaps/probes-round5.md` section 7)

**Visible toasts work Headless.**
- The banner is ShellExperienceHost's reused `Windows.UI.Core.CoreWindow` "New notification". Find it by name (a new-window diff misses it, because the window is reused).
- Its tree: the toast view, with the App name in its Name, then Title, MessageText, the action buttons (VerbButton) and DismissButton.
- Invoke on an action button reached the App. Dismiss worked. The foreground was unchanged when the App was listening.
- An action for an App with no live listener can hand the foreground to the shell: report it (headless-max 03's detect-and-report).

**The Notification Center cannot be opened through UIA** (the Clock Invoke opens nothing, see dp-09).
- Its list is reachable only through `ToastNotificationManager.History` for a known AUMID, which covers only Apps winuse can name. Other Apps' notifications there are out of reach.
- So build `list-notifications`, `notification-action` and `notification-dismiss` on the visible banners only, plus `wait notification` (poll for the banner by name).
- Older notifications need the panel, opened with `--headed` (Win+N through dp-09's fallback) and then read through UIA. That leg was not measured.

The identity guard stays: re-read the App and text before acting.

Probe notes:
- Toasts were off on the laptop, and writing ToastEnabled alone did not enable them; the Settings toggle did.
- An e2e test should raise its toast from a small native or C# helper (PowerShell 5.1 cannot receive the activation), and it needs toasts enabled.
