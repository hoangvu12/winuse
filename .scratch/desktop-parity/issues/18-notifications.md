# 18: Notifications

**What to build:** `list-notifications`, `notification-action <id> <button>`, `notification-dismiss <id>`, and `wait notification [--app] [--text]`. agent-desktop has four notification commands plus `wait --notification` (research section 1, Notifications rows).

- Read toasts and the Action Center through UIA. A visible toast is its own Window (`Windows.UI.Core.CoreWindow`, "New notification").
- The Action Center must be open to list older ones: open it through ticket 09's route (Headless if the taskbar button Invokes cleanly, otherwise Headed).
- Identity guard (agent-desktop): an action re-reads the notification's App and text before acting and refuses if they changed.
- Alternative to probe: `Windows.UI.Notifications.Management.UserNotificationListener` (needs the user's permission and package identity; likely unusable from an unpackaged binary).

**Blocked by:** 01 (Desktop command term), 09 (shell surfaces)

**Status:** done (the live toast and open-shell e2e runs wait for the user's OK)

- [x] Probe: toast and Action Center trees, Invoke on toast buttons, foreground before and after (round 5; the panel's tree is out of reach)
- [x] Commands and the identity guard
- [x] Offline tests over recorded trees (the banner tree from the probe's notes; a record-tree capture waits for the live run)
- [x] Opt-in e2e: a toast raised by the e2e test itself (written; not run live, toasts are off)
- [x] SKILL.md

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

### 2026-11-03: built (laptop/dp-18-notifications)

Built on laptop/dp-09-shell (5dab95d) and origin/main (lanes merged), with 09's two pieces (orchestrator brief).

What landed:

- **The banner is told apart from the panel.** `win.ShellNotification` ("notification banner"): ShellExperienceHost's CoreWindow captioned "New notification". It is the one shell surface recognised by its caption: the panel shares its class and host, and nothing else measured tells them apart. Under another display language the banner is taken for the Action Center, as before. `list-windows --shell` lists it while it shows. An Action on a shell surface no longer reports a banner that shows meanwhile as a Window it opened.
- **`list-notifications [--app] [--text]`** reads the showing banners through UIA, without replacing their Window States (PeekTree). It prints one line per toast view (AutomationId `...ToastView`): `<n>\t<App>\t<title>\t<text>\tbuttons: A, B`, from SenderName, Title, MessageText and the VerbButtons. The numbers are per Agent (ADR 0014). With no banner it says so and points at `--headed open-shell notifications`.
- **`notification-action <n> <button>` and `notification-dismiss <n>`** use the identity guard. The banners are read again, and the command acts only on the notification that still shows with the Window, App, title and text that number was listed with. Otherwise it refuses with nothing sent, and so does a button the toast lacks. The press is UIA Invoke (HeadlessClick) on the VerbButton or DismissButton. The read-back is the notification leaving the banner within 2 s; one that stays fails as delivered ("do not repeat it"). When the foreground moved, the `ok` says so, naming the App or the shell (headless-max 03's detect-and-report). A Headless click on the banner's Elements stays refused (09's rule), and its hint now names the notification commands. These two are Desktop commands: no Window, so they take the desktop lane, and they show no Agent Cursor.
- **`wait notification [--app] [--text] [--timeout]`** is a desktop wait: it polls the banners every 100 ms, takes no lane, and prints and numbers the matches as list-notifications does.
- **`winuse --headed open-shell start|notifications|quick-settings|overflow`** (09's piece 1) is Headed only and refused with nothing sent without `--headed`. start presses Control_L+Escape, notifications Win+N and quick-settings Win+A, under the indicator over the taskbar and an armed Esc scope; overflow is the real click on the hidden-icons chevron, 09's measured route. The surface showing within 2 s is the read-back, and its line is printed with the foreground note. A surface open already is printed with nothing sent, since the accelerators toggle. The Windows key comes from `keys.ShellAccelerator`, which no key name reaches: press-key still refuses it, and CONTEXT.md's rule is unchanged. ADR 0015's open point is closed (decisions 8-10).
- The output of list-notifications and wait notification is redacted in traces: it is the user's messages.

Files: `internal/helper/notifications.go`, `internal/helper/openshell.go`, `internal/cli/notifications.go`, `internal/fixture/toast.go` (new); `internal/win/shell.go`, `internal/win/enum.go`, `internal/helper/shell.go`, `internal/helper/opened.go`, `internal/helper/waits.go`, `internal/helper/state.go` (registration, the per-Agent log), `internal/keys/keys.go`, `internal/protocol/protocol.go` (appended types), `internal/protocol/page.go`, `internal/cli/cli.go`, `internal/cli/wait.go`, `internal/cli/trace.go`; ADR 0015, CONTEXT.md, SKILL.md, README.md, docs/codex-parity.md, test/e2e/README.md.

Tests (offline): `internal/helper/notifications_test.go` covers the recorded banner (`testdata/toast-banner.json`, hand-authored from the probe's measured tree: the probe kept no record-tree file), stacked toasts, the listing and its filters, the press and its Element, the foreground note, the identity guard (no listing, a number past it, changed text, a timed-out banner, a missing button, another Agent's listing, a locked desktop), dismiss, a press that leaves the toast showing, the wait, the banner as a shell surface with its refusal hint, the opened-note exclusion, and open-shell (refused without --headed, each accelerator, already open, did not show, the chevron and no chevron). Also: `internal/win/shell_test.go` (the caption case), `internal/keys/keys_test.go` (ShellAccelerator; Parse still refuses Win), `internal/cli/notifications_test.go` (parsing, exit 2, --headed carried), `internal/cli/trace_test.go` (redaction), `internal/fixture/toast_test.go` (the toast XML).

E2e (`test/e2e/notifications_test.go`): `TestListNotificationsOnlyReads` (usual opt-in); `TestNotificationsPressAndDismissARaisedToast` and `TestNotificationIdentityGuardRefusesAGoneToast` (`WINUSE_E2E_TOASTS=1`, skipped while toasts are off); `TestOpenShellRefusesWithoutHeaded` and `TestOpenShellOpensTheNotificationPanel` (`WINUSE_E2E_SHELL=1`). The toast raiser is pure Go WinRT (combase, x/sys only). It uses winuse's own AUMID `winuse.e2e.toast`, registered under HKCU\Software\Classes\AppUserModelId for the run; cleanup clears its Notification Center entries and removes that key and the settings key Windows makes. So the tests never touch a notification of the user's.

Live (under the lock, on this laptop):

- Read-only: the shell CoreWindows on this desktop. ShellExperienceHost had no CoreWindow while no toast showed, so the banner's window properties could not be read without a toast.
- The raiser up to Show: ToastNotifier.Setting read 2 (DisabledForUser), and the XmlDocument, CreateToastNotification, put_Tag, add_Activated and add_Dismissed calls all returned S_OK. Nothing was shown or registered, and no key was left.
- `TestListNotificationsOnlyReads` passed (6.4 s), and `TestShellReadsTheTaskbar` passed again (2.1 s) after the shellKind change.
- Not run (they change what the user sees and wait for the user's OK): the two toast tests, which need the Settings toggle on and restored, and the two open-shell tests.

Open:

- Unmeasured: Show, the activation and History.Clear from this raiser, and whether a registry-registered AUMID's toast shows "winuse e2e" as its sender. The tests match the toast by its unique title, so they do not depend on the sender name. Also unmeasured: the panel's tree and quick settings' host, recognised as ShellHost.
- `testdata/toast-banner.json` should be replaced by a record-tree capture at the first live toast run.
- SKILL.md: re-fitted after the PC's trim (1a727cc). Two bullets in "The taskbar and the shell", the shell line of "What winuse cannot do headless", and four lines in the command reference (54.9 KB).
