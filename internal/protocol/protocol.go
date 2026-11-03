// Package protocol defines the request/response contract between the winuse
// CLI and the background helper.
//
// Transport is a named pipe (see internal/pipe) carrying JSON lines, one
// request per connection: the CLI dials, writes a single Request line, reads
// a single Response line and closes. There is no multiplexing; cancellation
// (the Esc key) is detected by the helper itself and reported through
// Response.Exit.
package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Version is the wire protocol version. The helper refuses mismatching
// requests with a version error.
const Version = 1

// ExitEsc is the exit code reported when the user pressed Esc to stop an
// Action or Batch (Codex's interrupt code).
const ExitEsc = 130

// RequestTimeout is the default per-request time limit (~10 s, Codex-like).
// The client's connection deadline adds slack on top.
const RequestTimeout = 10 * time.Second

// LaunchTimeout is the launch-app budget: Codex gives launching an App
// 15 s instead of the default 10 s (orchestrator decision).
const LaunchTimeout = 15 * time.Second

// BatchTimeout is the batch budget: one request runs several Actions in
// order, each with the activate-and-verify pipeline, so a Batch needs
// more than the default 10 s. Codex has no batch tool to follow here
// (codex-surface.md §1); 60 s is winuse's own bound, enough for dozens
// of steps while keeping a stuck Batch bounded.
const BatchTimeout = 60 * time.Second

// MaxLine bounds one JSON line in either direction (a Window State tree can
// be large, but not unbounded).
const MaxLine = 4 << 20

// The Browser Connection budgets (browser merge ticket 01). A dial may
// wait 20 s for Chrome's remote-debugging approval; launch waits for the
// started browser's endpoint; connect waits for the user to turn on the
// inspect toggle; clone copies a profile and may first close the user's
// browser.
const (
	BrowserTimeout        = 30 * time.Second  // browser status, launch, stop
	BrowserConnectTimeout = 90 * time.Second  // browser connect
	BrowserCloneTimeout   = 180 * time.Second // browser clone
)

// commandTimeouts holds the per-command request budgets that differ from
// RequestTimeout.
var commandTimeouts = map[string]time.Duration{
	"launch-app":      LaunchTimeout,
	"batch":           BatchTimeout,
	"browser-status":  BrowserTimeout,
	"browser-launch":  BrowserTimeout,
	"browser-stop":    BrowserTimeout,
	"browser-connect": BrowserConnectTimeout,
	"browser-clone":   BrowserCloneTimeout,
	// The winuse extension (ticket 02): updating an outdated extension
	// waits up to 15 s for it to reconnect.
	"browser-ext-install": BrowserTimeout,
	"browser-ext-status":  BrowserTimeout,
	// The attach to a running Electron App (headless-max 15): the signal,
	// the inspector's port and the shim's install.
	"browser-attach": BrowserTimeout,
	"browser-detach": BrowserTimeout,
	// The tab commands (ticket 07) may dial the Browser Connection first,
	// which can wait for the browser's approval prompt.
	"tab-list":   BrowserTimeout,
	"tab-new":    BrowserTimeout,
	"tab-close":  BrowserTimeout,
	"tab-switch": BrowserTimeout,
	// The page-only Actions (ticket 06, page.go): the same reason, and
	// their waits (load, wait, eval) stay under this budget.
	"navigate": BrowserTimeout,
	"back":     BrowserTimeout,
	"forward":  BrowserTimeout,
	"reload":   BrowserTimeout,
	"upload":   BrowserTimeout,
	"dialog":   BrowserTimeout,
	"wait":     BrowserTimeout,
	"eval":     BrowserTimeout,
}

// CommandTimeout returns a command's explicitly configured budget, if it
// has one (launch-app: 15 s, batch: 60 s). It is the one timeout helper
// (finding 17): the CLI composes it with RequestTimeout for its
// connection deadlines, and the helper overrides its configured default
// with the explicit entries only.
func CommandTimeout(cmd string) (time.Duration, bool) {
	d, ok := commandTimeouts[cmd]
	return d, ok
}

// The caller-set timeout caps (desktop-parity ticket 14): --timeout widens
// a slow command's budget, for slow Electron, Office and cold Chromium
// trees, a slow App start, a long Batch or a long page wait, up to these.
// Action commands keep their fixed budget: a slow Action is a bug or a
// hung App.
const (
	MaxStateTimeout  = 60 * time.Second  // get-window-state
	MaxLaunchTimeout = 120 * time.Second // launch-app
	MaxBatchTimeout  = 300 * time.Second // batch
	MaxWaitTimeout   = 300 * time.Second // wait (the wait itself; the request adds WaitMargin)
)

// WaitMargin is kept between a page wait and its request's deadline, so a
// wait that runs out still answers in time: wait --timeout asks for the
// wait plus this margin as its request budget.
const WaitMargin = time.Second

// maxTimeouts holds the commands that take a caller-set timeout, with
// its cap.
var maxTimeouts = map[string]time.Duration{
	"get-window-state": MaxStateTimeout,
	"find":             MaxStateTimeout,
	"launch-app":       MaxLaunchTimeout,
	"batch":            MaxBatchTimeout,
	"wait":             MaxWaitTimeout + WaitMargin,
}

// MaxTimeout returns the cap on a command's caller-set timeout; ok is
// false for a command that takes none.
func MaxTimeout(cmd string) (time.Duration, bool) {
	d, ok := maxTimeouts[cmd]
	return d, ok
}

// CallerTimeout returns the budget the request's caller set (TimeoutMS),
// clamped to its command's cap; ok is false when the request sets none or
// its command takes none.
func CallerTimeout(req *Request) (time.Duration, bool) {
	limit, ok := maxTimeouts[req.Cmd]
	if !ok || req.TimeoutMS <= 0 {
		return 0, false
	}
	d := time.Duration(req.TimeoutMS) * time.Millisecond
	if d > limit {
		d = limit
	}
	return d, true
}

// Budget returns a request's budget given the default one: the command's
// explicit budget (CommandTimeout) or the default, widened to the
// caller-set timeout. A caller-set timeout under that budget leaves it as
// it is: --timeout only widens.
func Budget(req *Request, def time.Duration) time.Duration {
	budget := def
	if d, ok := CommandTimeout(req.Cmd); ok {
		budget = d
	}
	if d, ok := CallerTimeout(req); ok && d > budget {
		budget = d
	}
	return budget
}

// Request is one command sent by the CLI.
type Request struct {
	V         int             `json:"v"`                    // wire protocol version
	Ver       string          `json:"ver"`                  // CLI binary version; a helper from a different build exits (see Response.Stale)
	Cmd       string          `json:"cmd"`                  // command name, e.g. "list-windows"
	Args      json.RawMessage `json:"args,omitempty"`       // command arguments, command-specific JSON
	TimeoutMS int             `json:"timeout_ms,omitempty"` // the caller-set budget (--timeout) of a command in MaxTimeout, clamped to its cap; 0: the command's own

	// The Agent the request comes from (multi-agent ticket 02, ADR 0014
	// Decision 3; internal/agent): its identity and the label the Agent
	// Cursor and messages show. Both empty: the shared default Agent.
	Agent      string `json:"agent,omitempty"`
	AgentLabel string `json:"agent_label,omitempty"`
	AgentColor string `json:"agent_color,omitempty"` // WINUSE_CURSOR_COLOR, for the Agent's cursor (multi-agent ticket 04)

	// Trace asks for Response.Trace (desktop-parity ticket 19, trace.go):
	// the CLI sets it under --trace or WINUSE_TRACE.
	Trace bool `json:"trace,omitempty"`
}

// ListWindowsArgs are list-windows's optional arguments (desktop-parity
// 22): Details adds the details column (visible frame, pid, focused,
// minimized, maximized, admin); App keeps only that App's Windows,
// matched as a Window reference matches an App name. Neither is set for
// Codex's plain list_windows.
type ListWindowsArgs struct {
	Details bool   `json:"details,omitempty"`
	App     string `json:"app,omitempty"`
	Shell   bool   `json:"shell,omitempty"` // desktop-parity 09: add the shell surfaces (taskbar, tray overflow, Start, Action Center)
}

// GetWindowArgs are get-window's arguments: the Window to re-resolve and
// print as one list-windows line — Codex's get_window "id"/"app" step
// (winuse names the Window by Window ID or unambiguous App name).
type GetWindowArgs struct {
	Window string `json:"window"`
}

// LaunchArgs are launch-app's arguments: an App name from the catalog or
// an explicit identifier (executable path, shortcut path, App User Model
// ID) — Codex's launch_app "app" parameter — and agent-desktop's launch
// options (headless-max ticket 01). Args, Cwd and Env need an executable.
type LaunchArgs struct {
	App       string   `json:"app"`
	Minimized bool     `json:"minimized,omitempty"` // show the first Window minimized (SW_SHOWMINNOACTIVE)
	Args      []string `json:"args,omitempty"`      // command-line arguments, one per --arg, quoted by the helper
	Cwd       string   `json:"cwd,omitempty"`       // the working directory, absolute (the CLI resolves it)
	Env       []string `json:"env,omitempty"`       // KEY=VALUE entries over the helper's environment
	CDP       bool     `json:"cdp,omitempty"`       // an Electron or WebView2 App with a loopback debugging port (headless-max 15)
}

// CloseAppArgs are close-app's arguments (desktop-parity ticket 10): the
// App whose Windows to close, named as a Window reference names an App
// (display name or executable name), and with Force, agent-desktop's
// --force: end the App's processes still running after the posted close.
type CloseAppArgs struct {
	App   string `json:"app"`
	Force bool   `json:"force,omitempty"`
}

// FindArgs are find's arguments (desktop-parity 04): the Window and the
// filters, every one optional; First keeps the first match, Count prints
// only how many matched.
type FindArgs struct {
	Window string `json:"window"`
	Role   string `json:"role,omitempty"`
	Name   string `json:"name,omitempty"`
	Text   string `json:"text,omitempty"`
	State  string `json:"state,omitempty"`
	First  bool   `json:"first,omitempty"`
	Count  bool   `json:"count,omitempty"`
}

// AttachArgs are the arguments of browser attach and browser detach
// (headless-max 15): a Window of the running Electron App.
type AttachArgs struct {
	Window string `json:"window"`
}

// BrowserArgs are the arguments of browser status, use, launch, connect
// and stop: a browser's short name (chrome, brave, edge, chromium,
// vivaldi, opera), or empty for the pinned or only debuggable one. For
// browser use, "auto" removes the pin and empty prints it.
type BrowserArgs struct {
	Browser string `json:"browser,omitempty"`
}

// BrowserCloneArgs are browser clone's arguments.
type BrowserCloneArgs struct {
	Browser     string `json:"browser,omitempty"`
	Profile     string `json:"profile,omitempty"`      // the profile folder to copy; empty is Default
	Fresh       bool   `json:"fresh,omitempty"`        // copy again from scratch instead of syncing
	CloseSource bool   `json:"close_source,omitempty"` // close the running browser first (cleanly), so logins copy, and reopen its tabs in the clone
}

// ClickArgs are click's arguments — Codex's click parameters. Ticket 05
// implements the Element Index form; ticket 08 adds the `@x,y` coordinate
// form (Point); ticket 11 adds the name:/id: Element Reference form (Ref).
// The three forms are mutually exclusive.
type ClickArgs struct {
	Window       string   `json:"window"`                 // Window ID, or an App name when that App has exactly one Window
	ElementIndex *int     `json:"element_index"`          // the Element to click, from that Window's latest Window State
	Ref          string   `json:"ref,omitempty"`          // name:<text> or id:<AutomationId>: the Element to click, resolved against the Window's latest Window State
	Point        *Point   `json:"point,omitempty"`        // @x,y: coordinates in the latest screenshot's coordinate space
	ClickCount   int      `json:"click_count,omitempty"`  // 1 (default), 2 (double click) or 3 (triple click)
	MouseButton  string   `json:"mouse_button,omitempty"` // "left" (default), "right" or "middle"; aliases l, r, m
	Headed       bool     `json:"headed,omitempty"`       // force v1's foreground click with the real pointer (ADR 0010); default Headless for an Element click
	Post         bool     `json:"post,omitempty"`         // post the click's mouse messages to the classic control there, best effort (headless-max ticket 12); not with headed
	Modifiers    []string `json:"modifiers,omitempty"`    // ctrl, shift, alt held during the click (desktop-parity ticket 06)
}

// Point is a coordinate Element Reference: a point in the coordinate space
// of a Window's latest screenshot (`@x,y`). Screenshot pixels are mapped
// back to the Window's physical pixels through the recorded scale, so the
// reference is exact at any display scaling and on any monitor.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Region is a rectangle in the latest screenshot's coordinate space
// (x, y: top-left; w, h: size, in screenshot pixels): zoom's crop request.
type Region struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// ScrollArgs are scroll's arguments — Codex's scroll parameters: a
// point plus horizontal and vertical amounts. The point is a coordinate
// Element Reference: x, y in the coordinate space of the Window's latest
// screenshot. Positive scrollY scrolls down, negative up; positive
// scrollX scrolls right, negative left (Codex: "scrollY: 600 scrolls
// down ... Negative scrolls up or left"). Amounts are in wheel units:
// one notch is 120.
//
// The Element forms (headless-parity spec, "Element scroll") name an
// Element instead, by Element Index or name:/id: reference, with a
// direction: up, down, left or right by steps (default 3, at most 50), or
// into-view. They are Headless unless headed. The point form is Headless
// on a Page and Headed elsewhere, and Headed only with headed (headless-max
// ticket 21). A request uses one form: the point and amounts stay zero in
// an Element form.
type ScrollArgs struct {
	Window  string `json:"window"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	ScrollX int    `json:"scrollX"`
	ScrollY int    `json:"scrollY"`

	ElementIndex *int     `json:"element_index,omitempty"` // the Element to scroll, from the Window's latest Window State
	Ref          string   `json:"ref,omitempty"`           // name:<text> or id:<AutomationId>: the Element to scroll
	Direction    string   `json:"direction,omitempty"`     // up, down, left, right or into-view
	Steps        *int     `json:"steps,omitempty"`         // scroll steps for a direction (default 3, 1 to 50); none for into-view
	Headed       bool     `json:"headed,omitempty"`        // the v1 way: activate, then the wheel at the point or the Element (into-view: the ScrollItem call after activation); without it a point outside a Page is refused
	Post         bool     `json:"post,omitempty"`          // the point form: post the wheel to the classic control there, best effort (headless-max ticket 12); not with headed
	Modifiers    []string `json:"modifiers,omitempty"`     // the point form: ctrl, shift, alt held during the wheel, e.g. ctrl to zoom (desktop-parity ticket 06)
}

// DragArgs are drag's arguments — Codex's drag parameters: from and to
// points, both coordinate Element References in the latest screenshot's
// coordinate space.
type DragArgs struct {
	Window string `json:"window"`
	FromX  int    `json:"from_x"`
	FromY  int    `json:"from_y"`
	ToX    int    `json:"to_x"`
	ToY    int    `json:"to_y"`

	// Browser merge ticket 06: a drag from a Page Element (an Element Index
	// or name:/id: reference) to another Page Element or to the @x,y point
	// above (ToX, ToY). Headless over the Browser Connection.
	FromIndex *int   `json:"from_index,omitempty"`
	FromRef   string `json:"from_ref,omitempty"`
	ToIndex   *int   `json:"to_index,omitempty"`
	ToRef     string `json:"to_ref,omitempty"`

	// Headed is the agent's --headed (headless-max ticket 21): the point
	// form drags with the real pointer only with it, and is refused with
	// nothing sent without it. The Page form ignores it.
	Headed bool `json:"headed,omitempty"`

	// Desktop-parity 13: the Element form also starts on a UIA Element,
	// Headed like the point form (only with Headed), from the Element's
	// clickable point to the target Element's or to the point. DropDelayMS
	// (--drop-delay) holds the drag at its end before the drop, on every
	// route.
	DropDelayMS int `json:"drop_delay_ms,omitempty"`
}

// HoverArgs are hover's arguments: the point to move the pointer to,
// a coordinate Element Reference in the latest screenshot's coordinate
// space. Hover is a winuse extension (Codex has no hover); the parameter
// names follow Codex's coordinate parameters.
type HoverArgs struct {
	Window string `json:"window"`
	X      int    `json:"x"`
	Y      int    `json:"y"`

	// Browser merge ticket 06: a hover over a Page Element instead of a
	// point, Headless over the Browser Connection. A UIA Element is hovered
	// at its clickable point, Headed like the point form (desktop-parity
	// 13).
	ElementIndex *int   `json:"element_index,omitempty"`
	Ref          string `json:"ref,omitempty"`

	// Headed is the agent's --headed (headless-max ticket 21): the point
	// form moves the real pointer only with it, and is refused with nothing
	// sent without it. The Page form ignores it.
	Headed bool `json:"headed,omitempty"`
}

// ZoomArgs are zoom's arguments.
type ZoomArgs struct {
	Window string `json:"window"`
	Region Region `json:"region"`
	Headed bool   `json:"headed,omitempty"` // activate, then capture through the full chain (ADR 0011); default headless
}

// ActivateArgs are activate-window's arguments: the Window to bring to
// the foreground.
type ActivateArgs struct {
	Window string `json:"window"`
}

// WindowActionArgs are the arguments of the Window Actions that name only
// the Window (ADR 0011): restore-window, minimize-window and close-window.
type WindowActionArgs struct {
	Window string `json:"window"`
}

// MoveWindowArgs are move-window's arguments (ADR 0011): the screen point
// the Window's frame top-left moves to, in physical pixels.
type MoveWindowArgs struct {
	Window string `json:"window"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
}

// ResizeWindowArgs are resize-window's arguments (ADR 0011): the Window's
// new frame size, in physical pixels.
type ResizeWindowArgs struct {
	Window string `json:"window"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// MaximizeWindowArgs are maximize-window's arguments (desktop-parity
// ticket 11): the one Headed Window Action. Windows has no non-activating
// maximize, so without headed nothing is sent.
type MaximizeWindowArgs struct {
	Window string `json:"window"`
	Headed bool   `json:"headed,omitempty"` // the global --headed flag: activate, then SW_MAXIMIZE
}

// TypeTextArgs are type-text's arguments — Codex's type_text parameters:
// the text is typed into the Window's focused Element (the Window is
// activated first, like every Headed Action). Text without control
// characters into a focused classic edit control runs Headless (ADR 0011,
// Decision 4) unless headed. With post, the text is posted as Posted Keys
// instead (WM_CHAR to the focused control, no foreground); post and
// headed do not combine.
//
// With an Element in IntoIndex or IntoRef (desktop-parity ticket 23), the
// text goes into that Element instead of whatever has the focus: a Page
// Element over the Browser Connection, an Element whose own window is a
// classic edit control through EM_REPLACESEL, both Headless; any other
// Element only with headed (activate, click it, type). into does not
// combine with post.
type TypeTextArgs struct {
	Window    string `json:"window"`
	Text      string `json:"text"`
	Headed    bool   `json:"headed,omitempty"`     // force v1's activate-and-type, even where a Headless route exists
	Post      bool   `json:"post,omitempty"`       // Posted Keys: post WM_CHAR to the Window's focused control (ADR 0011, Decision 5)
	IntoIndex *int   `json:"into_index,omitempty"` // type into this Element, from the Window's latest Window State
	IntoRef   string `json:"into_ref,omitempty"`   // name:<text> or id:<AutomationId>: type into this Element
	// IntervalMS pauses this long between characters on the routes that
	// type character by character (Headed, Posted); 0 to 1000
	// (desktop-parity ticket 23).
	IntervalMS int `json:"interval_ms,omitempty"`
}

// PressKeyArgs are press-key's arguments — Codex's press_key parameters:
// a key or `+`-separated chord in Codex's X keysym-style names (Return,
// Tab, Control_L+a, Numpad_0). Return and Escape on a classic dialog run
// Headless (ADR 0011, Decision 4) unless headed; every other key is
// Headed. With post, the key is posted as Posted Keys instead (WM_KEYDOWN
// and WM_KEYUP to the focused control, no foreground; a chord with
// modifiers through attached key state, headless-max ticket 07); post
// and headed do not combine. A Blocked Combo (Alt+F4, Alt+Tab,
// Ctrl+Shift+Escape and their supersets) is refused unless force.
type PressKeyArgs struct {
	Window string `json:"window"`
	Key    string `json:"key"`
	Headed bool   `json:"headed,omitempty"` // force v1's activate-and-inject press, even where a Headless route exists
	Post   bool   `json:"post,omitempty"`   // Posted Keys: post the key to the Window's focused control (ADR 0011, Decision 5)
	Force  bool   `json:"force,omitempty"`  // send a Blocked Combo anyway (keys.Blocked)
}

// SetValueArgs are set-value's arguments — Codex's set_value parameters
// with ticket 11's Element Reference form: the Element (an Index or a
// name:/id: reference against the Window's latest Window State) and its
// new value.
type SetValueArgs struct {
	Window       string `json:"window"`           // Window ID, or an App name when that App has exactly one Window
	ElementIndex *int   `json:"element_index"`    // the Element to set, from that Window's latest Window State
	Ref          string `json:"ref,omitempty"`    // name:<text> or id:<AutomationId>: the Element to set
	Point        *Point `json:"point,omitempty"`  // @x,y: the Element under a point of the Window's latest screenshot
	Value        string `json:"value"`            // the new value (a string, or a number for range Elements)
	Headed       bool   `json:"headed,omitempty"` // force v1's activate-and-focus write (ADR 0010); default Headless
}

// SecondaryActionArgs are perform-secondary-action's arguments — Codex's
// perform_secondary_action parameters with ticket 11's Element Reference
// form: the Element (an Index or a name:/id: reference) and one of its
// Secondary Actions (case-insensitive).
type SecondaryActionArgs struct {
	Window       string `json:"window"`           // Window ID, or an App name when that App has exactly one Window
	ElementIndex *int   `json:"element_index"`    // the Element to act on, from that Window's latest Window State
	Ref          string `json:"ref,omitempty"`    // name:<text> or id:<AutomationId>: the Element to act on
	Point        *Point `json:"point,omitempty"`  // @x,y: the Element under a point of the Window's latest screenshot
	Action       string `json:"action"`           // e.g. "Raise", "Scroll Down", "Expand", "Collapse", "Toggle", "Select"
	Headed       bool   `json:"headed,omitempty"` // force v1's activate-first pipeline (ADR 0010); default Headless except Raise
}

// SelectTextArgs are select-text's arguments (headless-max ticket 10):
// the Element (an Index or a name:/id: reference) and the span to select,
// Length UTF-16 code units from Start, or with All the whole text. Always
// Headless: there is no headed form.
type SelectTextArgs struct {
	Window       string `json:"window"`        // Window ID, or an App name when that App has exactly one Window
	ElementIndex *int   `json:"element_index"` // the Element whose text to select, from that Window's latest Window State
	Ref          string `json:"ref,omitempty"` // name:<text> or id:<AutomationId>: the Element whose text to select
	Start        int    `json:"start"`         // the first UTF-16 unit selected
	Length       int    `json:"length"`        // how many UTF-16 units to select; 0 places the caret at Start
	All          bool   `json:"all,omitempty"` // select the whole text (Start and Length stay 0)
}

// InvokeMenuArgs are invoke-menu's arguments (headless-max ticket 08):
// the path of a classic menu bar command, one item name per level, such as
// ["File", "Save As..."]. Always Headless: there is no headed form.
type InvokeMenuArgs struct {
	Window string   `json:"window"` // Window ID, or an App name when that App has exactly one Window
	Path   []string `json:"path"`   // the item names from the menu bar down to the command
}

// ClipboardGetArgs are clipboard-get's arguments (desktop-parity ticket
// 01, a Desktop command): the format to read, text (the default), image
// or files, and for an image the absolute path the PNG is written to
// (the CLI resolves it; empty: a new file in %TEMP%\winuse).
type ClipboardGetArgs struct {
	Format string `json:"format,omitempty"`
	Out    string `json:"out,omitempty"`
}

// ClipboardSetArgs are clipboard-set's arguments (desktop-parity ticket
// 01): exactly one representation, the text (empty text included), an
// absolute PNG path or absolute file paths. It replaces everything on the
// clipboard.
type ClipboardSetArgs struct {
	Text  *string  `json:"text,omitempty"`
	Image string   `json:"image,omitempty"`
	Files []string `json:"files,omitempty"`
}

// ScreenshotArgs are screenshot's arguments (desktop-parity ticket 08, a
// Desktop command): the index of the display to capture, from
// list-displays (0 is the primary display).
type ScreenshotArgs struct {
	Display *int `json:"display"`
}

// BatchArgs are batch's arguments (ticket 11): several Actions on one
// Window in a single call, run in order, past a failed step unless
// StopOnError (desktop-parity ticket 20).
type BatchArgs struct {
	Window      string `json:"window"`                  // the Window the steps act on unless a step names its own (Window ID, or an App name when that App has exactly one Window)
	Steps       []Step `json:"steps"`                   // the Actions to run, in order
	Headed      bool   `json:"headed,omitempty"`        // every step runs Headed (ADR 0010)
	StopOnError bool   `json:"stop_on_error,omitempty"` // stop at the first failed step instead of running the rest
}

// Step is one Batch step: an Action and its arguments — the same
// arguments the standalone command takes, without the Window (the Batch
// names the Window once; the helper injects it into every step). Kind
// must be one of BatchStepKinds. A step may name its own Window instead
// (desktop-parity ticket 24, `in <window>`), or the Window the step before
// it opened (`in new`).
type Step struct {
	Kind   string          `json:"kind"`             // the Action's command name, e.g. "click"
	Args   json.RawMessage `json:"args,omitempty"`   // that command's arguments (window omitted)
	Window string          `json:"window,omitempty"` // this step's own Window, instead of the Batch's
	InNew  bool            `json:"in_new,omitempty"` // this step acts in the Window the step before it opened
}

// BatchStepKinds are the Action commands a Batch step can be — the
// commands that act on one Window. Ticket 10 extends the list with
// scroll, drag and hover. The CLI's batch step parser and the helper's
// step dispatch share this list so the two ends of the wire never disagree
// about what a Batch can run. A Batch prints each step's output under its
// number (desktop-parity ticket 20).
var BatchStepKinds = append(append([]string{
	"click", "type-text", "press-key", "set-value", "perform-secondary-action", "activate-window",
	"scroll", "drag", "hover",
	// Browser merge ticket 07: the tab commands; tab-list since
	// desktop-parity ticket 20 printed the steps' output.
	"tab-new", "tab-close", "tab-switch", "tab-list",
},
	// Browser merge ticket 06: the page-only Actions.
	PageCommands...),
	// Headless-max ticket 10: select-text, so a Batch can select a word
	// and type its replacement.
	"select-text",
	// Headless-max ticket 08: invoke-menu.
	"invoke-menu",
)

// Response is the helper's answer.
type Response struct {
	OK    bool   `json:"ok"`
	Stale bool   `json:"stale,omitempty"` // helper is from a different winuse build: it exits after answering; the CLI restarts one and retries
	Out   string `json:"out,omitempty"`   // text the CLI prints on stdout (before the error on a failure: a Batch's steps)
	Err   string `json:"err,omitempty"`   // single-line error message
	Hint  string `json:"hint,omitempty"`  // recovery hint printed after the error
	Exit  int    `json:"exit,omitempty"`  // exit code the CLI should use on failure

	// Trace is the helper's part of a trace line (desktop-parity ticket
	// 19, trace.go), only when Request.Trace asked for it.
	Trace *Trace `json:"trace,omitempty"`
}

// ErrLineTooLarge is returned when a line exceeds MaxLine.
var ErrLineTooLarge = errors.New("winuse: message exceeds line limit")

// WriteLine encodes v as one JSON line (json.Encoder terminates with '\n').
func WriteLine(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// ReadLine reads one '\n'-terminated JSON line capped at MaxLine and
// unmarshals it into v.
func ReadLine(r *bufio.Reader, v any) error {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			if len(buf) > MaxLine {
				return ErrLineTooLarge
			}
			continue
		}
		if err != nil {
			return err // EOF or connection error before a full line
		}
		break
	}
	if len(buf) > MaxLine {
		return ErrLineTooLarge
	}
	return json.Unmarshal(trimEOL(buf), v)
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// Errorf formats a failure response.
func Errorf(exit int, format string, args ...any) Response {
	return Response{OK: false, Err: fmt.Sprintf(format, args...), Exit: exit}
}

// OK formats a success response carrying text for stdout.
func OK(out string) Response {
	return Response{OK: true, Out: out}
}
