package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hoangvu12/winuse/internal/apps"
	"image"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wgc"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// TreeReader reads one Window's tree through UI Automation on the helper's
// locked COM thread. The real implementation is uia.Client; tests fake it
// with recorded trees (seam 2 at the command level).
type TreeReader interface {
	Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error)
}

// ElementPointer is the element-side half of Actions: resolving an
// Element Index of a Window's latest Window State to the screen point to
// inject input at (scrolling the Element into view, clickable point or
// bounds centre), on the UIA worker. The real implementation is
// *uia.Client (Actions defaults to Trees when it is one); later Action
// tickets add their own element work as separate interfaces type-asserted
// from Services.Actions (ticket 07's ElementActor in setvalue.go: set
// value, secondary actions) rather than widening this one.
type ElementPointer interface {
	ElementPoint(ctx context.Context, hwnd uint64, index int) (uia.Point, error)
}

// EscWatch is the helper's Esc-stop service (ticket 12): Arm and Disarm
// bracket every scope the user can stop with the physical Esc key (an
// Action, a Batch, a launch wait — nestable), and Stopped reports whether
// the user pressed it inside the scope (cleared when the outermost scope
// ends, so a stop never leaks into the next command). The real watcher is
// *win.EscWatcher — the low-level keyboard hook on the UI thread;
// CommandsWith installs a no-op when there is none (offline tests, a
// helper whose hook could not start), so every command still runs.
type EscWatch interface {
	Arm()
	Disarm()
	Stopped() bool
}

// Services carries the commands' dependencies so tests can inject fakes
// (the desktop enumeration, the tree source and the Action pipeline's
// Win32 steps); zero values fall back to the real ones.
type Services struct {
	Windows func() ([]win.WindowInfo, error) // visible Windows; nil: the live desktop
	Trees   TreeReader                       // nil: a real UI Automation client (lazy COM)
	Budget  time.Duration                    // tree-read time limit; 0: uia.DefaultTreeBudget

	// The Action pipeline (ticket 05). Nil values fall back to the real
	// Win32 implementations; tests fake them so offline tests never
	// activate a window or inject input.
	Locked    func() (locked bool, desktop string)                                     // locked-desktop refusal
	Integrity func(win.WindowInfo) error                                               // administrator-Window refusal
	Activate  func(hwnd uint64) error                                                  // foreground activation + verification
	Actions   ElementPointer                                                           // element-side Action work (UIA worker); a HeadlessActor too routes Element Actions Headless
	Click     func(target uint64, x, y int32, button win.MouseButton, count int) error // SendInput click

	// The Headless Actions (ADR 0010, headless.go). Ready replaces the
	// activation: it refuses a Window that cannot take a pattern call
	// without the foreground (a modal dialog, minimized); nil falls back to
	// win.HeadlessReady. Cursor is the Agent Cursor that shows a Headless
	// Action instead of the banner; nil means no cursor (offline tests, a
	// helper whose cursor could not start). The real one is
	// *win.AgentCursor, wired by Main.
	Ready  func(hwnd uint64) error
	Cursor Cursor

	// Keyboard input (ticket 06). Nil values fall back to the real
	// SendInput implementations; tests fake them. stopped is the Esc-stop
	// checkpoint (ticket 12), polled between typing batches.
	TypeText  func(text string, stopped func() bool) error // Unicode key-event typing
	PressKeys func(chord keys.Chord) error                 // key or chord press (downs in order, ups in reverse)

	// The keyboard routes (ADR 0011, Decision 4; keyroute.go). KeyRoute is
	// the Window probe press-key and type-text route on, read after the
	// Window resolves and before anything is sent; nil (Services built
	// without defaultServices) keeps every key and text Headed.
	// DialogCommand posts a classic dialog's WM_COMMAND(id, BN_CLICKED);
	// EditState and ReplaceSel are the Headless type-text route into a
	// classic edit control. defaultServices fills the real ones; tests fake
	// them so offline tests never send a message.
	KeyRoute      func(hwnd uint64) win.KeyTarget                      // the dialog, its default and Cancel buttons, the focused control
	DialogCommand func(dialog uint64, id uint16, control uint64) error // PostMessage(WM_COMMAND)
	EditState     func(edit uint64) (win.EditState, error)             // WM_GETTEXTLENGTH and EM_GETSEL
	ReplaceSel    func(edit uint64, text string) error                 // SendMessageTimeout(EM_REPLACESEL, TRUE)

	// select-text's classic edit route (headless-max ticket 10,
	// selecttext.go): EditControl reads the window behind an Element's
	// NativeWindowHandle, SetEditSel selects in it; EditState (above) reads
	// the length and the selection back. Nil values fall back to the real
	// calls; tests fake them so offline tests never send a message.
	EditControl func(hwnd uint64) win.Control           // class, style and enabled state, never the text
	SetEditSel  func(edit uint64, start, end int) error // SendMessageTimeout(EM_SETSEL, start, end)

	// invoke-menu's classic menu bar route (headless-max ticket 08,
	// invokemenu.go): MenuBar reads the Window's menu bar, ReadMenu one
	// menu's items, InitMenuPopup lets the App update a submenu before it
	// is read, MenuItemRect places the Agent Cursor on the bar item, and
	// MenuCommand posts the command. Nil values fall back to the real
	// calls; tests fake them so offline tests never send a message.
	MenuBar       func(hwnd uint64) uint64                    // GetMenu: 0 when the Window has no classic menu bar
	ReadMenu      func(menu uint64) ([]win.MenuItem, error)   // the items of one HMENU, by position
	InitMenuPopup func(hwnd, submenu uint64, pos int) error   // SendMessageTimeout(WM_INITMENUPOPUP)
	MenuItemRect  func(hwnd uint64, pos int) (win.Rect, bool) // GetMenuItemRect of a menu bar item
	MenuCommand   func(hwnd uint64, id uint16) error          // PostMessage(WM_COMMAND, MAKEWPARAM(id, 0))

	// Posted Keys (ADR 0011, Decision 5; keyroute.go): press-key and
	// type-text with `post`. Each returns how many messages it posted, so
	// a failure can tell "nothing was sent" from a partial post. Nil values
	// fall back to the real PostMessage calls; tests fake them.
	PostKeys func(target uint64, chord keys.Chord) (int, error) // WM_KEYDOWN in order, WM_KEYUP in reverse
	PostText func(target uint64, text string) (int, error)      // WM_CHAR per UTF-16 unit

	// The Esc stop (ticket 12): nil falls back to a no-op (no physical-Esc
	// stopping); tests fake it with a latch they can press.
	Esc EscWatch
	// The screenshot services (tickets 08 and 09). Nil values fall back
	// to the real ones; tests fake them so offline tests never capture the
	// screen. Screenshots and zoom are headless by default (ADR 0011):
	// CaptureHeadless, after Minimized refuses a minimized Window. Capture
	// is the full chain that --headed runs after activation.
	Capture         func(hwnd uint64) (shot.Frame, error)  // the full chain (Windows.Graphics.Capture → region → PrintWindow)
	CaptureHeadless func(hwnd uint64) (shot.Frame, error)  // the headless chain (Windows.Graphics.Capture → PrintWindow)
	Minimized       func(hwnd uint64) bool                 // the Window is minimized: nothing to capture headless
	Shots           *shot.Store                            // per-Window latest screenshot mappings (`@x,y`, zoom)
	FrameOrigin     func(hwnd uint64) (image.Point, error) // the Window's extended frame bounds top-left, read fresh at Action time

	// The pointer input (ticket 10). Nil values fall back to the real
	// SendInput implementations; tests fake them. dx/dy are Codex's
	// scroll amounts (positive right/down, wheel units — the win layer
	// flips the vertical to wheel semantics). Drag's stopped is the abort
	// checkpoint between its interpolation steps (the Esc stop or the
	// request's deadline, finding 10).
	Scroll func(target uint64, x, y, dx, dy int32) error                        // wheel scroll at a point
	Drag   func(target uint64, fx, fy, tx, ty int32, stopped func() bool) error // left-button drag, from point to point
	Hover  func(target uint64, x, y int32) error                                // move the pointer and leave it there

	// The Window Actions (ADR 0011, windowactions.go). Nil values fall
	// back to the real non-activating Win32 calls; tests fake them so
	// offline tests never place or close a real Window. Minimized (above)
	// is their iconic probe too. Sleep paces their verification polls;
	// tests fake it so a poll runs instantly.
	RestoreWindow     func(hwnd uint64) error                      // ShowWindowAsync(SW_SHOWNOACTIVATE)
	MinimizeWindow    func(hwnd uint64) error                      // ShowWindowAsync(SW_SHOWMINNOACTIVE)
	MoveWindow        func(hwnd uint64, x, y int32) error          // SetWindowPos: visible frame top-left, no activation, no z-order change
	ResizeWindow      func(hwnd uint64, width, height int32) error // SetWindowPos: visible frame size, no activation, no z-order change
	CloseWindow       func(hwnd uint64) error                      // PostMessage(WM_CLOSE)
	WindowFrame       func(hwnd uint64) (win.Rect, error)          // the visible frame (extended frame bounds) move and resize set
	Maximized         func(hwnd uint64) bool                       // IsZoomed
	RestoresMaximized func(hwnd uint64) bool                       // WPF_RESTORETOMAXIMIZED: the minimized Window was maximized
	WindowOpen        func(hwnd uint64) bool                       // the Window still exists and is visible
	Foreground        func() uint64                                // GetForegroundWindow: restore-window's and launch-app's self-activation check
	WindowPID         func(hwnd uint64) uint32                     // the Window's process: another Window of the same App
	Sleep             func(d time.Duration)                        // the verification polls' pause; nil: time.Sleep

	// StartApp starts a resolved App for launch-app with a non-activating
	// show state (headless-max ticket 01). Nil falls back to apps.StartApp;
	// tests fake it so offline tests never start a process. launch-app's
	// Window polls use Windows, Foreground and Sleep above.
	StartApp func(spec apps.Spec, opts win.LaunchOptions) (pid uint32, err error)

	// The Windows an Action opened (desktop-parity 02, opened.go):
	// Surfaces lists every visible top-level window, unfiltered, menus and
	// popups included (win.Collect), for the before-and-after diff and for
	// resolving a reported menu's Window ID. Nil falls back to win.Collect,
	// which refuses inside an offline test binary (the diff is then
	// skipped); tests fake it.
	Surfaces func() ([]win.WindowInfo, error)

	// The in-control indicator (ticket 13): raised over the target Window
	// for the duration of every Action, click-through, never focused, and
	// hidden around every capture. Nil means no indicator (offline tests;
	// a helper whose indicator could not start). The real one is
	// *win.Overlay, wired by Main; the Esc keyboard hook of ticket 12 will
	// join its UI thread.
	Overlay Overlay

	// The Browser Connections (browser.go, browser merge ticket 01): one
	// persistent connection per browser for the helper's lifetime. Nil
	// falls back to a *browser.Manager over browser.Live, which probes and
	// dials nothing until a browser command runs (and refuses to in a test
	// binary); helper.Main wires its own so it can close it on exit.
	Browser BrowserService
	// Pages reads a browser Window's Page through its Browser Connection,
	// for the one Window State of a browser Window (pagestate.go, browser
	// merge ticket 04). Nil falls back to a reader over Browser; tests
	// fake it with recorded reads.
	Pages PageReader

	// The status probes (desktop parity ticket 16, status.go), all
	// read-only. Started is when the helper started and Now the clock its
	// uptime is read on; OwnIntegrity, DPIAwareness and WindowsBuild read
	// the helper process and the machine. Nil and zero values fall back to
	// the real ones (Started: when CommandsWith ran, at helper start);
	// Locked (above) is the desktop probe. Tests fake them.
	Started      time.Time
	Now          func() time.Time
	OwnIntegrity func() (uint32, error) // the helper's integrity level RID
	DPIAwareness func() string          // the helper's DPI awareness, named
	WindowsBuild func() string          // the Windows build, named

	// The clipboard (desktop parity ticket 01, clipboard.go). Nil falls
	// back to the live *win.Clipboard, one per helper, so an abandoned
	// read refuses the clipboard commands after it; tests fake it.
	Clipboard ClipboardService

	// actionGate serializes every command that activates a Window, injects
	// input or shows the overlay (finding 11): two concurrent CLI calls
	// must never interleave control of the desktop. It is a capacity-1
	// channel — a mutex whose wait is bounded by the waiting request's own
	// budget. Created by defaultServices so one Services (one helper)
	// shares one gate; observation-only commands skip it. Headless Actions
	// (ADR 0010) stay serialized too: they write into apps and drive the
	// one Agent Cursor, and one command at a time is the promise either
	// way. The Window Actions (ADR 0011) are serialized as well: they
	// place and close the Windows that other Actions act in, and so are
	// the Desktop commands (the clipboard's): a paste in a Batch must not
	// see the clipboard change halfway.
	actionGate chan struct{}
}

// CommandsWith builds the command registry over the given services.
// Commands() wires the real ones.
//
// Commands that activate a Window, inject input or show the overlay are
// serialized on the helper-wide action gate (finding 11): one winuse
// command controls the desktop at a time, exactly what codex-parity's
// "one request at a time" promises. Observation-only commands —
// list-windows, get-window, list-apps, the plain tree read, record-tree,
// launch-app, status — stay concurrent; get-window-state's --screenshot branch
// and zoom join the serialized set inside their handlers.
func CommandsWith(s Services) map[string]Command {
	s = defaultServices(s)
	// The indicator's brackets nest (finding 12): a Batch brackets its
	// whole run and the steps' own brackets ride inside without hiding
	// the indicator between Actions. Wrapping before the commands are
	// built wires every bracket (and every capture's HideWhile) through
	// the nesting.
	if s.Overlay != nil {
		s.Overlay = newBracketOverlay(s.Overlay)
	}
	// The Start-apps listing is shared by list-apps and launch-app and
	// cached for the helper's lifetime (it is a ~1 s PowerShell call).
	store := &apps.StoreSource{Run: apps.GetStartApps}
	cmds := map[string]Command{
		"list-windows":             listWindows(s),
		"get-window":               getWindow(s),
		"get-window-state":         func(ctx context.Context, args json.RawMessage) (string, *Error) { return getWindowState(s, ctx, args) },
		"record-tree":              func(ctx context.Context, args json.RawMessage) (string, *Error) { return recordTree(s, ctx, args) },
		"zoom":                     zoom(s),
		"click":                    click(s),
		"activate-window":          activateWindow(s),
		"restore-window":           restoreWindow(s),
		"minimize-window":          minimizeWindow(s),
		"move-window":              moveWindow(s),
		"resize-window":            resizeWindow(s),
		"close-window":             closeWindow(s),
		"type-text":                typeText(s),
		"press-key":                pressKey(s),
		"set-value":                setValue(s),
		"perform-secondary-action": performSecondaryAction(s),
		"scroll":                   scroll(s),
		"select-text":              selectText(s),
		"invoke-menu":              invokeMenu(s),
		"drag":                     drag(s),
		"hover":                    hover(s),
		"list-apps":                listApps(store),
		"launch-app":               launchApp(s, store),
		StatusCommand:              statusCommand(s),
	}
	// The Batch dispatches its steps to the other commands, and brackets
	// itself as one armed Esc scope and one overlay bracket around them
	// (batch.go, tickets 11 and 12, finding 12). It dispatches to the raw
	// step commands: the batch as a whole is serialized below, and the
	// action gate is not reentrant.
	cmds["batch"] = batch(cmds, s)
	// The Browser Connection commands (browser.go).
	for name, cmd := range browserCommands(s) {
		cmds[name] = cmd
	}
	// The tab commands (tabs.go, browser merge ticket 07).
	for name, cmd := range tabCommands(s) {
		cmds[name] = cmd
	}
	// The page-only Actions (pagecommands.go, browser merge ticket 06).
	for name, cmd := range pageCommands(s) {
		cmds[name] = cmd
	}
	// The clipboard's Desktop commands (clipboard.go, desktop parity
	// ticket 01): serialized below like the Actions.
	for name, cmd := range clipboardCommands(s) {
		cmds[name] = cmd
	}

	// The serialized registry: every control-path command runs alone, and
	// the Actions that can open a Window report it (opened.go). The Batch
	// keeps the raw step commands, so it diffs once around all its steps.
	out := make(map[string]Command, len(cmds))
	for name, cmd := range cmds {
		if observationOnlyCommand(name) {
			out[name] = cmd
			continue
		}
		out[name] = s.serialized(s.reportOpened(name, cmd))
	}
	return out
}

// observationOnlyCommand reports whether a command never activates a
// Window, injects input or shows the overlay, so it may run concurrently
// with a serialized one (finding 11). get-window-state is listed here for
// its plain tree read; its --screenshot branch serializes inside the
// handler. launch-app starts a process and watches for its Window — it
// injects nothing. The browser-* commands start, connect to and close
// browsers without touching the desktop's input, and can wait a minute or
// more for the user (an approval prompt, the inspect toggle, a profile
// copy), so they never hold the action gate; the manager serializes its
// own dials.
func observationOnlyCommand(name string) bool {
	switch name {
	case "list-windows", "get-window", "list-apps", "get-window-state", "record-tree", "launch-app",
		"tab-list",    // reads a browser Window's Tabs only (tabs.go)
		StatusCommand: // read-only probes (status.go): never waits behind an Action
		return true
	}
	return strings.HasPrefix(name, "browser-")
}

// serialized wraps one command in the helper-wide action gate (finding
// 11): the command runs alone — no other activating, injecting or
// overlay-showing command interleaves with it. The wait for the gate is
// bounded by the waiting request's own budget: a request queued behind
// another Action until its budget expired fails instead of acting late
// (the server's central timeout policy reports it).
func (s Services) serialized(cmd Command) Command {
	if s.actionGate == nil {
		return cmd // a zero Services (no defaultServices): tests wrap nothing
	}
	gate := s.actionGate
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return "", queuedOutError()
		}
		defer func() { <-gate }()
		if err := ctx.Err(); err != nil {
			// The budget expired in the instant the gate opened.
			return "", queuedOutError()
		}
		return cmd(ctx, args)
	}
}

// queuedOutError is the failure a serialized command reports when its
// request's budget expired while another Action held the desktop.
func queuedOutError() *Error {
	return Fail("another winuse Action was still running when this request's budget expired",
		"retry once the other Action finishes")
}

// noopEscWatch is the Esc service when there is no watcher: no armed
// scopes, no stops. Commands still run; the user just cannot stop them
// with Esc (a helper whose hook could not start logs it).
type noopEscWatch struct{}

func (noopEscWatch) Arm()          {}
func (noopEscWatch) Disarm()       {}
func (noopEscWatch) Stopped() bool { return false }

// defaultServices fills a Services' zero values with the real
// implementations: the desktop enumeration, the UIA client and budget, the
// Action pipeline's Win32 steps, and the screenshot chain —
// Windows.Graphics.Capture first (ticket 09), then the GDI fallbacks
// (region, PrintWindow).
func defaultServices(s Services) Services {
	if s.Windows == nil {
		s.Windows = func() ([]win.WindowInfo, error) {
			all, err := win.Collect()
			if err != nil {
				return nil, err
			}
			return win.VisibleWindows(all), nil
		}
	}
	if s.Trees == nil {
		s.Trees = uia.New()
	}
	if s.Budget <= 0 {
		s.Budget = uia.DefaultTreeBudget
	}
	if s.Locked == nil {
		s.Locked = win.DesktopLocked
	}
	if s.Integrity == nil {
		s.Integrity = win.CheckWindowIntegrity
	}
	if s.Activate == nil {
		s.Activate = win.Activate
	}
	if s.Ready == nil {
		s.Ready = win.HeadlessReady
	}
	if s.Actions == nil {
		if ep, ok := s.Trees.(ElementPointer); ok {
			s.Actions = ep
		}
	}
	if s.Click == nil {
		s.Click = win.MouseClick
	}
	if s.TypeText == nil {
		s.TypeText = win.TypeText
	}
	if s.PressKeys == nil {
		s.PressKeys = win.PressChord
	}
	if s.KeyRoute == nil {
		s.KeyRoute = win.ProbeKeyTarget
	}
	if s.DialogCommand == nil {
		s.DialogCommand = win.PostDialogCommand
	}
	if s.EditState == nil {
		s.EditState = win.ReadEditState
	}
	if s.ReplaceSel == nil {
		s.ReplaceSel = win.ReplaceSelection
	}
	if s.EditControl == nil {
		s.EditControl = win.ReadControl
	}
	if s.SetEditSel == nil {
		s.SetEditSel = win.SetEditSelection
	}
	if s.MenuBar == nil {
		s.MenuBar = win.MenuBar
	}
	if s.ReadMenu == nil {
		s.ReadMenu = win.ReadMenu
	}
	if s.InitMenuPopup == nil {
		s.InitMenuPopup = win.InitMenuPopup
	}
	if s.MenuItemRect == nil {
		s.MenuItemRect = win.MenuBarItemRect
	}
	if s.MenuCommand == nil {
		s.MenuCommand = win.PostMenuCommand
	}
	if s.PostKeys == nil {
		s.PostKeys = win.PostKeys
	}
	if s.PostText == nil {
		s.PostText = win.PostText
	}
	if s.Esc == nil {
		s.Esc = noopEscWatch{}
	}
	if s.Scroll == nil {
		s.Scroll = win.MouseScroll
	}
	if s.Drag == nil {
		s.Drag = win.MouseDrag
	}
	if s.Hover == nil {
		s.Hover = win.MouseHover
	}
	if s.Shots == nil {
		s.Shots = shot.NewStore()
	}
	if s.Capture == nil || s.CaptureHeadless == nil {
		// The capture chains (spec, Observation details): Windows.Graphics
		// Capture first (ticket 09, Codex's method — falling back on any
		// failure, black frame or timeout), then the GDI fallbacks. The full
		// chain (--headed) tries the Window's screen region, then
		// PrintWindow; the headless chain (ADR 0011) only PrintWindow. Both
		// share one wgc method, which logs its captures into the helper log.
		// With an indicator or an Agent Cursor wired, every method captures
		// with them hidden (ticket 13, ADR 0010), so winuse's own
		// screenshots never contain them.
		wgcCapture := wgc.New()
		wgcCapture.Log = log.Printf
		if s.Capture == nil {
			s.Capture = shot.Chain{Methods: captureMethodsHiding(s.Overlay, s.Cursor, wgcCapture)}.CaptureWindow
		}
		if s.CaptureHeadless == nil {
			s.CaptureHeadless = shot.Chain{Methods: headlessCaptureMethods(s.Overlay, s.Cursor, wgcCapture)}.CaptureWindow
		}
	}
	if s.Minimized == nil {
		s.Minimized = win.IsMinimized
	}
	if s.FrameOrigin == nil {
		s.FrameOrigin = win.FrameOrigin
	}
	if s.RestoreWindow == nil {
		s.RestoreWindow = win.RestoreNoActivate
	}
	if s.MinimizeWindow == nil {
		s.MinimizeWindow = win.MinimizeNoActivate
	}
	if s.MoveWindow == nil {
		s.MoveWindow = win.MoveNoActivate
	}
	if s.ResizeWindow == nil {
		s.ResizeWindow = win.ResizeNoActivate
	}
	if s.CloseWindow == nil {
		s.CloseWindow = win.PostClose
	}
	if s.WindowFrame == nil {
		s.WindowFrame = win.WindowFrame
	}
	if s.Maximized == nil {
		s.Maximized = win.IsMaximized
	}
	if s.RestoresMaximized == nil {
		s.RestoresMaximized = win.RestoresToMaximized
	}
	if s.WindowOpen == nil {
		s.WindowOpen = win.WindowOpen
	}
	if s.Foreground == nil {
		s.Foreground = win.ForegroundWindow
	}
	if s.WindowPID == nil {
		s.WindowPID = win.WindowPID
	}
	if s.Sleep == nil {
		s.Sleep = time.Sleep
	}
	if s.StartApp == nil {
		s.StartApp = apps.StartApp
	}
	if s.Surfaces == nil {
		s.Surfaces = func() ([]win.WindowInfo, error) {
			// Read-only, but an offline test must not depend on the
			// user's desktop: the diff is skipped there.
			if err := win.LiveDesktopGuard(); err != nil {
				return nil, err
			}
			return win.Collect()
		}
	}
	if s.Browser == nil {
		s.Browser = browser.NewManager(browser.Live())
	}
	if s.Pages == nil {
		s.Pages = newLivePages(s.Browser, s.Maximized)
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Started.IsZero() {
		s.Started = s.Now()
	}
	if s.OwnIntegrity == nil {
		s.OwnIntegrity = func() (uint32, error) { return win.IntegrityLevel(0) }
	}
	if s.DPIAwareness == nil {
		s.DPIAwareness = win.DPIAwareness
	}
	if s.WindowsBuild == nil {
		s.WindowsBuild = win.WindowsBuild
	}
	if s.Clipboard == nil {
		s.Clipboard = win.NewClipboard()
	}
	if s.actionGate == nil {
		s.actionGate = make(chan struct{}, 1)
	}
	return s
}

// windowRef is the shared argument shape of commands that name a Window.
type windowRef struct {
	Window     string `json:"window"`           // a Window ID, or an App name (exactly one Window)
	Screenshot bool   `json:"screenshot"`       // get-window-state --screenshot: capture and save a screenshot too
	Headed     bool   `json:"headed,omitempty"` // with --screenshot: activate, then capture through the full chain (ADR 0011)
}

// getWindowState answers `winuse get-window-state <window>`: the resolved
// Window's State as Codex's depth-first, indented, numbered tree. A read
// that hits the tree budget returns the partial tree with a note. With
// --screenshot (screenshot.go), the Window is captured without activation
// (activated first under --headed), the image saved scaled (long edge at
// most 1568 px, JPEG q80) with its path printed and its mapping recorded
// for `@x,y` references and zoom regions.
func getWindowState(s Services, ctx context.Context, args json.RawMessage) (string, *Error) {
	var a windowRef
	if err := parseArgs(args, &a); err != nil {
		return "", Fail("get-window-state: "+err.Error(), "run winuse get-window-state <window> [--screenshot] [--timeout <seconds>]")
	}
	ctx = uia.WithSettle(ctx, stateSettle(ctx))
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return "", rerr
	}
	if a.Screenshot {
		// The screenshot path captures with the indicator and the Agent
		// Cursor hidden, and activates under --headed: serialize it like an
		// Action (finding 11), while the plain tree read stays concurrent.
		return s.serialized(func(ctx context.Context, args json.RawMessage) (string, *Error) {
			return windowStateScreenshot(s, ctx, w, a.Headed)
		})(ctx, args)
	}
	tree, terr := readWindowTree(s, ctx, w)
	if terr != nil {
		return "", terr
	}
	return wstate.Render(tree), nil
}

// stateSettle is the Chromium settle budget of a get-window-state under
// ctx (desktop-parity ticket 14): uia.DefaultChromiumSettle, widened by
// whatever a caller-set --timeout adds to the default request budget, so
// a cold Chromium tree gets the extra time. The request's deadline still
// bounds the settle (uia's settlePolls).
func stateSettle(ctx context.Context) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return uia.DefaultChromiumSettle
	}
	extra := time.Until(dl) - protocol.RequestTimeout
	if extra <= 0 {
		return uia.DefaultChromiumSettle
	}
	return uia.DefaultChromiumSettle + extra
}

// recordArgs is record-tree's argument shape.
type recordArgs struct {
	Window string `json:"window"`
	File   string `json:"file"` // absolute output path (the CLI resolves it)
}

// recordTree answers `winuse record-tree <window> <file>`: it captures a
// real Window's tree and saves it as a fixture file in the recorder's
// format (wstate.Tree JSON) for the seam-2 offline rendering tests.
func recordTree(s Services, ctx context.Context, args json.RawMessage) (string, *Error) {
	var a recordArgs
	if err := parseArgs(args, &a); err != nil {
		return "", Fail("record-tree: "+err.Error(), "run winuse record-tree <window> <file>")
	}
	if strings.TrimSpace(a.File) == "" {
		return "", Fail("record-tree: an output file is required", "run winuse record-tree <window> <file>")
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return "", rerr
	}
	tree, err := s.Trees.Capture(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
	if err != nil {
		return "", treeReadError(err)
	}
	now := time.Now()
	tree.Format = wstate.FixtureFormat
	tree.Captured = &now
	tree.Source = fmt.Sprintf("recorded by winuse record-tree from %q (Window %d)", w.Title, w.ID)
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return "", Fail("encoding the fixture: "+err.Error(), "")
	}
	if err := writeFileAtomic(a.File, append(data, '\n')); err != nil {
		return "", Fail("writing "+a.File+": "+err.Error(), "")
	}
	note := ""
	if len(tree.Notes) > 0 {
		note = " (" + strings.Join(tree.Notes, "; ") + ")"
	}
	return fmt.Sprintf("recorded %s: %d elements%s\n", a.File, len(tree.Elements), note), nil
}

// parseArgs unmarshals a command's arguments, refusing missing or malformed
// JSON with a usage-flavoured message.
func parseArgs(args json.RawMessage, v any) error {
	if len(args) == 0 || string(args) == "null" {
		return errors.New("arguments are required")
	}
	if err := json.Unmarshal(args, v); err != nil {
		return errors.New("invalid arguments: " + err.Error())
	}
	return nil
}

// resolveWindow names a Window by Window ID or App name over the live
// desktop (see win.ResolveWindow for the matching rules). A Window ID
// that list-windows drops resolves too when it is an open menu or popup
// (win.OpenedSurface), so the agent can read the Window an Action
// reported opening.
func resolveWindow(s Services, ref string) (win.WindowInfo, *Error) {
	ws, err := s.Windows()
	if err != nil {
		return win.WindowInfo{}, Fail("enumerating windows: "+err.Error(), "")
	}
	w, rerr := win.ResolveWindow(ws, ref)
	if rerr != nil {
		if popup, ok := s.popupByID(ref); ok {
			return popup, nil // a menu or popup an Action reported (opened.go)
		}
		var re *win.ResolveError
		if errors.As(rerr, &re) {
			return win.WindowInfo{}, &Error{Msg: re.Msg, Hint: re.Hint, Exit: 1}
		}
		return win.WindowInfo{}, Fail(rerr.Error(), "")
	}
	return w, nil
}

// treeReadError maps a capture failure to a user-facing error. Context
// deadlines stay plain: the server's central timeout policy answers those.
func treeReadError(err error) *Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return Fail("the tree read did not complete in time", "retry; if it keeps failing, the app's accessibility tree is slow")
	}
	hint := ""
	switch {
	case errors.Is(err, uia.ErrWindowGone):
		hint = "the Window may have closed; run winuse list-windows"
	case errors.Is(err, uia.ErrInit):
		hint = "UI Automation could not start in the helper; report this if it persists"
	}
	return Fail(err.Error(), hint)
}

// writeFileAtomic writes a file via a temp sibling + rename, so a reader
// never sees a partial fixture.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
