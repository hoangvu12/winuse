# 18: Notifications

**What to build:** `list-notifications`, `notification-action <id> <button>`, `notification-dismiss <id>`, and `wait notification [--app] [--text]`. agent-desktop has four notification commands plus `wait --notification` (research section 1, Notifications rows).

- Read toasts and the Action Center through UIA. A visible toast is its own Window (`Windows.UI.Core.CoreWindow`, "New notification").
- The Action Center must be open to list older ones: open it through ticket 09's route (Headless if the taskbar button Invokes cleanly, otherwise Headed).
- Identity guard (agent-desktop): an action re-reads the notification's App and text before acting and refuses if they changed.
- Alternative to probe: `Windows.UI.Notifications.Management.UserNotificationListener` (needs the user's permission and package identity; likely unusable from an unpackaged binary).

**Blocked by:** 01 (Desktop command term), 09 (shell surfaces)

**Status:** needs-triage

- [ ] Probe: toast and Action Center trees, Invoke on toast buttons, foreground before and after
- [ ] Commands and the identity guard
- [ ] Offline tests over recorded trees
- [ ] Opt-in e2e: a toast raised by the e2e test itself
- [ ] SKILL.md
