# Ledger of live claims

One row per end-to-end test in this package: the claim it backs, in plain
words, and where it runs.

- **runner**: the e2e lane (`.github/workflows/e2e.yml`) runs it on
  GitHub's hosted `windows-latest` runner, against winuse's own fixture app
  only. The lane selects exactly these tests (`go run ./cmd/e2eledger
  pattern` builds the `-run` pattern from this table) and fails when one of
  them fails, skips or does not run (`go run ./cmd/e2eledger check`).
- **user**: it needs something the runner lacks or the lane must not drive
  (another App, a browser, the shell, toasts, a person at the keyboard), so
  only the user runs it, on their own PC. The reason says what.

**Last run** and **Result** are filled by hand from a lane's job summary
(runner rows) or from a user's run (user rows). The lane does not edit this
file. `-` means no result recorded here yet; the older live results are in
the tickets' comments.

Rules for the table, checked by `internal/e2eledger` (offline, in `go test
./...`): every top-level test in `test/e2e` has exactly one row and every
row names a test that exists; Runs on is `runner` or `user`; a `user` row
says why; no cell contains `|`.

| Test | Claim | Runs on | Why not on the runner | Last run | Result |
|---|---|---|---|---|---|
| `TestWindowStatesArePerAgent` | Each agent keeps its own Window State and Element Indexes on the same Window | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestEachAgentsCursorSitsAboveItsOwnWindow` | Each agent's cursor shows above the Window that agent acts on | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestLaunchAppCDPOnCoveredElectron` | launch-app --cdp gives a covered Electron App a Browser Connection | user | needs a prebuilt Electron (WINUSE_E2E_ELECTRON); not the fixture | - | - |
| `TestLaunchAppCDPOnWebView2` | launch-app --cdp reaches a WebView2 App's web content | user | needs a WebView2 host App (WINUSE_E2E_WEBVIEW2); not the fixture | - | - |
| `TestAttachToRunningElectron` | winuse attaches to an Electron App started without debugging | user | needs a prebuilt Electron (WINUSE_E2E_ELECTRON); not the fixture | - | - |
| `TestClickByNameReference` | A click by name: reference clicks the fixture Element with that Name | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickByIDReference` | A click by id: reference clicks the fixture Element with that AutomationId | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickAmbiguousNameListsCandidates` | An ambiguous name: reference fails and lists the candidates | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickAmbiguousIDListsCandidates` | An ambiguous id: reference fails and lists the candidates | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickMissingReferenceFailsWithHint` | A reference to no Element fails with a hint | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestReferencesResolveAgainstTheLatestWindowState` | References resolve against the latest Window State read | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueByReference` | set-value takes a name: or id: reference | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPerformSecondaryActionByReference` | perform-secondary-action takes a name: or id: reference | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestBatchTypesThenClicksAndSucceeds` | A Batch types, then clicks, and reports success | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestBatchRunsPastAFailedStep` | A Batch runs past a failed step and reports it | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestBatchPausesBetweenTwoActions` | A Batch's wait step pauses between two Actions | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestBatchStopOnErrorStopsAtTheFirstFailure` | A stop-on-error Batch stops at its first failure and names the step | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestBatchFromStdin` | batch - reads its steps from stdin | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestBatchUnknownWindowFailsBeforeAnyStep` | A Batch on an unknown Window fails before any step | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestBatchUsageErrorsExit2WithoutDialing` | Batch usage errors exit 2 without starting the helper | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestBatchAgainstTheDialog` | A Batch acts in the fixture's Save As dialog | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestLaunchedBrowserCoveredKeepsItsTree` | A covered browser winuse launched keeps its web tree | user | starts Edge or Chrome; browsers are not the fixture | - | - |
| `TestBrowserPages` | Page commands work in a browser on a throwaway profile | user | starts Edge or Chrome; browsers are not the fixture | - | - |
| `TestBrowserPagesExtension` | Page commands work through the extension in the user's browser | user | acts in the user's own browser and profile (WINUSE_E2E_USER_BROWSER) | - | - |
| `TestBrowserProbe` | The browser merge probe measures the browser routes | user | starts Edge or Chrome; browsers are not the fixture | - | - |
| `TestChromiumMessagesOnACoveredBrowser` | Typing, keys and clicks reach a covered browser without the foreground | user | starts Edge or Chrome; browsers are not the fixture | - | - |
| `TestChromiumMessagesOnACoveredElectron` | Typing, keys and clicks reach a covered Electron App without the foreground | user | needs a prebuilt Electron (WINUSE_E2E_ELECTRON); not the fixture | - | - |
| `TestTypeTextIntoAWPFBoxSplicesAtTheCaret` | type-text --into a WPF box splices at the caret, Headless | user | drives a WPF window hosted by Windows PowerShell, not the fixture | - | - |
| `TestIsolatedEdgeStaysSignedOut` | Edge on winuse's isolated profile stays signed out | user | starts Edge; browsers are not the fixture | - | - |
| `TestChromiumWebContentInWindowState` | A browser page's links, buttons and inputs show in one Window State | user | starts Edge or Chrome; browsers are not the fixture | - | - |
| `TestHeadlessReturnAndEscapeOnClassicDialog` | Return and Escape reach the fixture's classic dialog Headless | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessReturnAndEscapeOnMessageBox` | Return and Escape reach the fixture's MessageBox Headless | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessTypeTextIntoClassicEdit` | type-text reaches a covered classic Edit Headless | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessTypeTextIntoPasswordEdit` | type-text reaches a covered password Edit Headless | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessTypeTextIntoNotepad` | type-text reaches a covered Windows 11 Notepad Headless | user | drives Windows 11 Notepad, not the fixture | - | - |
| `TestHeadlessTypeTextIntoElement` | type-text --into an Element types there Headless | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickButtonChangesFixtureState` | Clicking the fixture's button presses it | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickCheckboxTogglesState` | Clicking the checkbox toggles it | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestDoubleClickAndRightClickCanvas` | Double and right clicks land on the canvas (Headed) | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickScrollsOffscreenElementIntoView` | A below-the-fold list item is scrolled into view before the click | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickOpensSaveAsDialogAsNewWindow` | Save As opens its dialog as a Window of its own, and Cancel closes it | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickStaleElementIndex` | A stale Element Index fails with the read-again instruction | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickWithoutCachedWindowState` | A click after a helper restart fails with the read-again instruction | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickWhileModalDialogOpenFails` | A click on a Window blocked by its modal dialog fails naming the dialog, with nothing sent | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestActivateWindowStandalone` | activate-window works, and refuses a dialog-blocked Window | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClickFlagsAnywhereAndJoined` | click takes its flags anywhere on the line | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestClipboard` | winuse's clipboard commands set and read text, images and files another program sees | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCoveredElectronReadCold` | A covered cold Electron App's read says when its web tree is thin | user | needs a prebuilt Electron (WINUSE_E2E_ELECTRON); not the fixture | - | - |
| `TestContextMenuOnACoveredBareControl` | A covered bare control's context menu opens and is read without the foreground | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestContextMenuOnACoveredWinFormsApp` | A covered WinForms ContextMenuStrip opens and is read | user | needs a prebuilt WinForms App (WINUSE_E2E_WINFORMS_APP); not the fixture | - | - |
| `TestContextMenuOnACoveredWinUIApp` | A covered WinUI 3 MenuFlyout opens and is read | user | needs a prebuilt WinUI 3 App (WINUSE_E2E_WINUI_APP); not the fixture | - | - |
| `TestListDisplaysAndCapturePrimary` | list-displays lists the displays and the primary one is captured | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestDisplayScreenshotFallsBackToTheScreenRegion` | A display screenshot falls back to the screen region | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestContextMenuOnACoveredEdit` | A covered Edit's context menu opens and is read without the foreground | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestContextMenuOnACoveredBrowserPage` | A covered browser page's context menu opens and is read | user | starts Edge or Chrome; browsers are not the fixture | - | - |
| `TestElementScrollByStepsAndIntoView` | Element scroll moves by steps and into view | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCloseAppClosesTwoWindowsWithOneCommand` | close-app closes both Windows of an App in one command and leaves other Apps | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCloseAppForceEndsAnAppThatStaysOpen` | close-app --force ends an App that ignores the close | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestEscStopsTheLongBatch` | The physical Esc key stops a long Batch | user | needs a person to press the physical Esc key (WINUSE_E2E_HUMAN) | - | - |
| `TestInjectedEscDoesNotStopTheBatch` | An injected Esc does not stop a Batch | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadedRightClickKeepsALateExplorerMenu` | A Headed right click keeps a late Explorer context menu | user | drives File Explorer, not the fixture | - | - |
| `TestContextMenuOnACoveredExplorerItem` | A covered Explorer item's context menu opens and is read | user | drives File Explorer, not the fixture | - | - |
| `TestListWindowsShowsFixtureWindow` | list-windows lists the fixture's Window with its ID, App and title, not its hidden dialog | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadedDoubleClickOpensAnExplorerFolder` | A Headed double click opens an Explorer folder | user | drives File Explorer, not the fixture | - | - |
| `TestHeadlessClickOpensAnExplorerFolder` | A Headless click opens an Explorer folder | user | drives File Explorer, not the fixture | - | - |
| `TestFindAndRegionReadOnTheFixture` | find and the region read find the fixture's Elements | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadedOnlyActionsAreRefusedWithoutTheFlag` | Headed-only Actions are refused without --headed, with nothing sent | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessChromiumKeepsTheForeground` | Headless Actions in a browser keep the foreground where it was | user | starts Edge or Chrome; browsers are not the fixture | - | - |
| `TestGetWindowStateByWindowID` | get-window-state by Window ID prints the fixture's full tree | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestGetWindowStateByAppName` | get-window-state by App name prints the fixture's full tree | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestGetWindowStateAmbiguousAppListsCandidates` | An App with two Windows is ambiguous and lists them | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestGetWindowStateUnknownWindowID` | An unknown Window ID fails | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestGetWindowStateAppWithNoWindows` | An App with no Windows fails saying so | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestGetWindowStateTinyBudgetIsPartialWithNote` | A tiny tree budget gives a partial tree with a note, not a hang | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestRecordTreeWritesFixture` | The recorder writes a loadable seam-2 fixture | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessScreenshotAndZoomOfCoveredWindow` | A covered Window's screenshot and zoom are taken Headless | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessScreenshotRefusesMinimizedWindow` | A minimized Window's screenshot is refused | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueOnOffTheCheckbox` | set-value on and off sets the checkbox | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessClickUnderCoveringWindow` | A Headless click acts on a covered Window and keeps the foreground | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessSetValueKeepsFocusAndForeground` | A Headless set-value keeps focus and the foreground | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessClickWithoutPatternRouteFails` | A Headless click with no pattern route fails | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadedClickBringsTheWindowForward` | A Headed click brings the Window forward | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestAgentCursorShownAndExcludedFromCaptures` | The Agent Cursor shows and is left out of captures | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHungAppStallsOnlyItsOwnWorker` | A hung App stalls only its own worker; another App's Actions go on | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestJSONOutputOfAClickAndAStaleIndex` | --json prints a click's result and a stale index's failure as JSON | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTwoAgentsRunInTheirOwnLanes` | Two agents' requests run in their own lanes | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessInvokeMenuOnClassicDialog` | A classic dialog's menu command runs Headless | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestListAppsShowsFixtureApp` | list-apps shows the running fixture App with its Window | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestLaunchAppByPathOpensFixtureWindow` | launch-app by path reports exactly the Window the fixture opened | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestLaunchAppNotFound` | launch-app of a bogus name fails with app not found | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestLaunchAppNoWindowTimesOut` | launch-app of a windowless executable times out saying so | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTheInsertionCaretShowsAfterASplice` | The Agent Cursor shows an insertion caret after a splice | user | drives a WPF window hosted by Windows PowerShell, not the fixture | - | - |
| `TestLaunchAppKeepsTheForeground` | launch-app keeps the foreground where it was | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestLaunchAppMinimized` | launch-app --minimized opens the App minimized | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestLaunchAppNotepadReportsTheForeground` | launch-app says when Notepad took the foreground | user | drives Windows 11 Notepad, not the fixture | - | - |
| `TestListWindowsOutput` | list-windows prints one tab-separated line per visible Window | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHelperIsReusedAcrossInvocations` | Two quick commands share one helper | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHelperIdleExitAndTransparentRestart` | The helper exits when idle and the next command restarts it | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHelperHasNoConsoleWindow` | The helper has no console Window and is not listed | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestGetWindowPrintsTheResovedLine` | get-window prints the resolved Window's line by ID and by App | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestNameReferenceAfterHelperRestart` | A name: reference still resolves after a helper restart | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessActionsOnMinimizedWindow` | Headless Actions work on a minimized Window | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCtrlClickOnASingleSelectListIsRefused` | A Ctrl+click on a single-select list is refused | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCtrlAndShiftClickSelectInAMultiSelectList` | Ctrl+ and Shift+clicks select in a multi-select list | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestObserveOnTheFixture` | observe reports the fixture's changes | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestNotepadSessionParsing` | A measured Notepad Window State parses into tabs and the save prompt (offline) | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestBatchInNewActsInTheOpenedMessageBox` | A Batch step in the new Window acts in the MessageBox it opened | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestNotificationsPressAndDismissARaisedToast` | A raised toast is pressed and dismissed | user | raises toasts (WINUSE_E2E_TOASTS); notifications are out of the lane | - | - |
| `TestNotificationIdentityGuardRefusesAGoneToast` | An Action on a toast that is gone is refused | user | raises toasts (WINUSE_E2E_TOASTS); notifications are out of the lane | - | - |
| `TestOpenShellRefusesWithoutHeaded` | open-shell is refused without --headed | user | acts on the user's own shell (WINUSE_E2E_SHELL) | - | - |
| `TestOpenShellOpensTheNotificationPanel` | open-shell opens the notification panel | user | acts on the user's own shell (WINUSE_E2E_SHELL); notifications are out of the lane | - | - |
| `TestListNotificationsOnlyReads` | list-notifications only reads, and a notification wait times out | user | reads the notification banners; notifications are out of the lane | - | - |
| `TestActionReportsOpenedAndClosedWindows` | An Action reports the Windows it opened and closed | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestOverlayDuringAction` | The overlay shows during an Action | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestOverlayOnlyDuringActions` | The overlay shows only during Actions | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPointFormsActHeadlessOnTheCoveredWindow` | The @x,y forms act Headless on a covered Window | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestAppBlock` | The list-apps output parses (offline) | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestScrollDownListAtPoint` | A scroll at a point scrolls the list | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestScrollCanvasHorizontalAndVertical` | A scroll on the canvas scrolls it both ways | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestDragOnCanvas` | A drag on the canvas lands | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHoverRevealsHoverOnlyToolbar` | A hover reveals the hover-only toolbar | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestScrollRefusedWithoutScreenshot` | A point scroll without a screenshot is refused | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPointerRefusalsAfterRestartAndOutside` | Pointer Actions are refused after a restart and outside the screenshot | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestDragBetweenElements` | A drag between two Elements lands | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadedClickGivesTheForegroundAndCursorBack` | A Headed click gives the foreground and the cursor back | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPostedClicksOnACoveredEdit` | Posted clicks reach a covered Edit | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPostedChordFiresAClassicAccelerator` | A posted chord fires a classic accelerator on a covered Window | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPostedChordFiresAWPFKeyBinding` | A posted chord fires a WPF KeyBinding on a covered Window | user | drives a WPF window hosted by Windows PowerShell, not the fixture | - | - |
| `TestPageReadSnapshot` | The Page read snapshot matches across routes | user | starts Edge or Chrome; browsers are not the fixture | - | - |
| `TestPageReadSnapshotExtension` | The Page read snapshot works through the extension | user | acts in the user's own browser and profile (WINUSE_E2E_USER_BROWSER) | - | - |
| `TestPostedKeysIntoClassicEdit` | Posted keys reach a covered classic Edit | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPostedKeysRefusedOnBlockedAndMinimizedWindows` | Posted keys are refused on blocked and minimized Windows | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPostedReturnIntoCoveredChromeAddressBar` | A posted Return reaches a covered Chrome's address bar | user | starts Chrome; browsers are not the fixture | - | - |
| `TestMultiLineSetValueIntoNotepadReadsBack` | A multi-line set-value into Notepad reads back | user | drives Windows 11 Notepad, not the fixture | - | - |
| `TestRenameInExplorerReadsBack` | A rename in Explorer reads back | user | drives File Explorer, not the fixture | - | - |
| `TestScreenshotSavedScaledAndPathPrinted` | A screenshot is saved scaled and its path printed | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestScreenshotOnlyWhenRequested` | A screenshot is taken only when asked for | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCoordinateClickLandsOnAimedPoint` | A coordinate click lands on the aimed point | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCoordinateClickMarkVisibleInScreenshot` | A coordinate click's mark shows in the next screenshot | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestZoomCropsFullResolution` | zoom crops at full resolution | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestScreenshotShowsOpenDropdownMenu` | A screenshot shows an open dropdown menu | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestScreenshotOfBlockedWindowShowsDialog` | A blocked Window's screenshot shows its dialog | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCoordinateClickWithoutScreenshotAfterRestart` | A coordinate click after a restart, with no screenshot, is refused | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCoordinateClickOutsideScreenshotRefused` | A coordinate click outside the screenshot is refused | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestZoomWithoutScreenshotRefused` | zoom without a screenshot is refused | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueSelectsAListItemByName` | set-value selects a list item by name | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueSelectsAComboItemByName` | set-value selects a combo box item by name | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessSelectTextInClassicEdit` | select-text selects in a covered classic Edit Headless | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadlessSelectTextInNotepad` | select-text selects in a covered Notepad Headless | user | drives Windows 11 Notepad, not the fixture | - | - |
| `TestHeadlessClickReportsSelfActivation` | A Headless click reports an App that activated itself | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueSetsTheTextField` | set-value sets the text field and a fresh read shows it | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueSetsTheDialogFileName` | set-value sets the dialog's file name | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueJoinsUnquotedValueArguments` | set-value joins unquoted value words | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueOnNonSettableElementFails` | set-value on a non-settable Element fails with the type-text hint | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSecondaryActionTogglesTheCheckbox` | perform-secondary-action toggles the checkbox | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSecondaryActionExpandsAndCollapsesTheMenu` | perform-secondary-action expands and collapses the File menu | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSecondaryActionSelectsAListItem` | perform-secondary-action selects a list item | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSecondaryActionScrollsTheListDown` | perform-secondary-action scrolls the list down a page | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSecondaryActionRaiseOnTheWindowRoot` | Raise on the Window root takes the foreground (Headed) | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestUnsupportedSecondaryActionFails` | An unknown secondary action fails listing the names | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSecondaryActionTheElementDoesNotOfferFails` | An action the Element does not offer fails listing what it offers | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueStaleElementIndex` | set-value with a stale Element Index fails | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestSetValueByPoint` | set-value at an @x,y point works (Headed) | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPerformSecondaryActionByPoint` | perform-secondary-action at an @x,y point works (Headed) | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestShortcutRunsAClassicMenuCommand` | A shortcut runs a classic menu command | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTraceRecordsRedactedLines` | The trace records redacted lines | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestShellReadsTheTaskbar` | list-windows --shell lists the taskbar and its Window State reads Headless | user | reads the shell's taskbar, not the fixture; a hosted runner's shell is not guaranteed | - | - |
| `TestShellRefusesWithoutHeaded` | Shell Actions are refused without --headed, with nothing sent | user | acts on the user's own shell (WINUSE_E2E_SHELL) | - | - |
| `TestShellOpensAndClosesTheTrayOverflow` | The tray overflow opens and closes | user | acts on the user's own shell (WINUSE_E2E_SHELL) | - | - |
| `TestTwoAgentsActAtOnceOnTwoFixtures` | Two agents act at once on two fixtures | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestAgentsIndexesSurviveAnotherAgentsRead` | An agent's Indexes survive another agent's read | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestHeadedActionsTakeTurnsOnTheDesktop` | Two agents' Headed Actions take turns on the desktop | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestEscStopsEveryAgent` | The physical Esc key stops every agent | user | needs a person to press the physical Esc key (WINUSE_E2E_HUMAN) | - | - |
| `TestWaitForADialogAndItsClose` | wait sees a dialog open and close | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestWaitForAValueChange` | wait sees a value change | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTypeTextTypesUnicode` | type-text types Unicode, including a surrogate-pair emoji (Headed) | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTypeTextWordsJoinWithoutQuotes` | type-text joins unquoted words | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTypeTextLongText` | type-text types a long text in batches | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTypeTextSelectAllAndReplace` | Control+A then type-text replaces the text | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTypeTextBackspaceReplacesSelection` | Backspace deletes the selection | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTypeTextIntoDialogEdit` | type-text into the dialog's edit is refused without --headed, then types with it | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPressKeyNames` | press-key sends keys by name, numpad and extended keys included | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPressKeyChord` | press-key sends a chord | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPressKeyUnknownKeyFailsWithoutInput` | An unknown key fails with a hint and sends nothing | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPressKeyWindowsKeyRefused` | The Windows key family is refused | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestTypeTextUsageErrors` | type-text usage errors are refused | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestPressKeyEmptyKeyIsRefused` | An empty key is refused | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestWGCScreenshotCapturedViaWindowsGraphicsCapture` | A screenshot is captured through Windows.Graphics.Capture | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestWGCDisabledFallsBackToGDI` | With WGC disabled, the screenshot falls back to GDI | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestWGCTimeoutFallsBackToGDI` | A WGC timeout falls back to GDI | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestCoordinateClickExactViaWGC` | A coordinate click is exact on a WGC screenshot | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestWGCZoomStillFullResolution` | zoom on a WGC screenshot is still full resolution | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestWindowActionsKeepTheForeground` | Window Actions keep the foreground | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestMaximizeWindowNeedsHeaded` | maximize-window needs --headed | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
| `TestRestoreWindowReportsSelfActivation` | restore-window reports an App that activated itself | runner | - | 2026-11-07, lane runs 42367421412 and 42367782401 | passed |
