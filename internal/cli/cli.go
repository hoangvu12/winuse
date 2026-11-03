package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/apps"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// usageHead and usageTail sandwich the Secondary Actions list in Usage
// below (from uia's canonical table, finding 18: this text, the Window
// State's listing and the Action validation share one source).
const usageHead = `usage: winuse [--headed] <command> [arguments]

global options:
  --headed                    run an Action the v1 way: bring the Window to the
                               foreground, use the real pointer and keyboard,
                               and show the in-control banner. Accepted before
                               or after the command; inside type-text's text,
                               set-value's value or clipboard-set's text it is
                               literal text
  --trace <file>              before the command (or WINUSE_TRACE=<file>):
                               append one JSON line per command to the file:
                               time, command, Agent, Window, arguments, routes
                               (Headless, Posted, Headed, CDP), outcome,
                               output (4 KiB; a Window State only by its line
                               count), screenshot paths and duration. Typed
                               text, values, clipboard content and eval are
                               redacted; past 32 MiB the file is renamed
                               <name>.1 and a new one started
  --trace-unredacted          keep the redacted values, for local debugging only

headless (the default; your foreground, pointer and keyboard stay untouched):
  click (left, single), set-value, perform-secondary-action (except Raise)
  and scroll on an element index, name: or id:; select-text and
  invoke-menu, which have no headed form; press-key Return or Escape
  on a classic dialog; type-text into a focused classic edit control, or
  with --into into a classic edit control or Page field it names, for
  text with no newline, tab or other control character; screenshots from
  get-window-state --screenshot and zoom (--headed activates the Window
  first); and the Window Actions restore-, minimize-, move-, resize- and
  close-window, which have no headed form (maximize-window is headed
  only). tab new, tab close and tab switch are headless (on a launched or
  cloned browser, tab switch selects the Tab in the Window's tab strip;
  --headed switches with real keys in the foreground instead). In a
  browser Window with a Browser Connection, every Action on its Page is
  headless, and so are navigate, back, forward, reload, upload, dialog,
  wait and eval, which have no headed form. The clipboard commands,
  list-displays and screenshot --display are headless Desktop commands:
  they name no Window and have no headed form.

headed only with --headed: every other Action (@x,y forms, double, right
  and middle clicks, drag, hover, Raise, maximize-window, press-key and
  type-text with no headless route) is refused with "nothing was sent"
  and the exact
  --headed retry. activate-window always takes the foreground: that is
  what it is for.

commands:
  list-windows [--details] [--app <name>]
                               list visible Windows: Window ID, App and title;
                               --details adds a column before the title with the
                               visible frame "x,y WxH" (what move-window and
                               resize-window take), "pid N" and the flags focused,
                               minimized, maximized and admin; --app lists one
                               App's Windows (App name or executable name)
  get-window <window>         print one Window's line (Window ID, App and title),
                               resolved by Window ID or an unambiguous App name
  list-apps                   list running and recently used Apps, with their Windows
  launch-app <name|exe path> [--minimized] [--cdp] [--arg <a>]... [--cwd <dir>] [--env KEY=VALUE]... [--timeout <seconds>]
                               start an App by name or executable path, report
                               its Window; waits 15 s, --timeout up to 120 for
                               a slow App. The App starts without taking the
                               foreground (--minimized: minimized); one that
                               takes it anyway, as a packaged App does, gets
                               a "Note: the App took the foreground" line.
                               --arg (one per argument, quoted for you),
                               --cwd and --env (over the helper's
                               environment) need an executable. --cdp starts
                               an Electron or WebView2 executable with a
                               loopback debugging port, so its pages are read
                               and acted on like a browser's, headless
  close-app <app> [--force]   ask every Window of an App to close, headless,
                               and wait up to 5 s for it to exit; one that
                               keeps running (it may ask to save) gets an ok
                               that says so. --force then ends the App's
                               processes, unsaved work included: the only
                               command that ends a process. Windows' own
                               processes (explorer, dwm, csrss, ...) and
                               administrator Apps are refused, nothing sent
  get-window-state <window> [--screenshot] [--timeout <seconds>]
                               print a Window's State: its Element tree as text,
                               and with --screenshot a screenshot saved to a file
                               whose path is printed; a Chromium or Electron
                               Window without a Browser Connection whose web
                               content reads thin is read again for up to 5 s
                               while it builds (once per Window; later reads
                               of a Window that stayed thin retry once). The
                               read has 10 s; --timeout up to 60 gives a slow
                               Electron, Office or cold Chromium tree more,
                               and the extra time goes to that 5 s wait too
  record-tree <window> <file> record a Window's real tree to a fixture file
  zoom <window> <x>,<y>,<w>,<h>
                               save a full-resolution crop of a region of the
                               Window's latest screenshot and print its path
  click <window> <element index | name: | id: | @x,y> [options]
                               click an Element of a Window by Element Index,
                               name: or id: reference, or at @x,y
                               coordinates from the Window's latest
                               screenshot; options: --count 1|2|3 (default 1),
                               --button left|middle|right (default left)
  type-text <window> [--post | --into <element index | name: | id:>] <text>
                               type Unicode text into the Window's focused
                               Element (headless into a classic edit control
                               such as Notepad's or a Win32 dialog's).
                               --post, right after the Window, posts the text
                               to the focused control without the foreground
                               instead: best effort, as for press-key.
                               --into, right after the Window, types into
                               that Element instead, whatever has the focus:
                               headless into a classic edit control (a
                               covered Notepad's editor too) or a Page
                               field; any other Element only with --headed
                               (click it, then type), otherwise refused with
                               nothing sent
  press-key <window> <key> [--post] [--force]
                               press a key or key combination by Codex's key
                               names, e.g. Return, Tab, Control_L+a, Numpad_0
                               (Return or Escape on a classic dialog presses
                               its default or Cancel button headless).
                               --post posts the key to the focused control
                               without the foreground instead: best effort,
                               no modifiers, and the app may ignore it (the
                               output says posted; read the Window State to
                               check). --post and --headed do not combine.
                               Alt+F4, Alt+Tab, Ctrl+Shift+Escape and any
                               chord holding one of them are refused with
                               nothing sent unless --force (also per batch
                               step); the Windows key is never sent
  set-value <window> <element index | name: | id: | @x,y> <value>
                               set an Element's value directly (a settable
                               edit, a numeric range, on|off for a check
                               box or other toggle, an item's name for a
                               combo box or list, or an option's label or
                               value for a Page's list); @x,y sets the Element
                               under a point of the latest screenshot
  perform-secondary-action <window> <element index | name: | id: | @x,y> <action>
                               perform one of the Element's Secondary Actions
                               (`

const usageTail = `);
                               @x,y acts on the Element under a point of the
                               latest screenshot
  batch <window> [--timeout <seconds>] <action> [arguments] [then <action> [arguments]] ...
                               run several Actions on one Window in order,
                               stopping at the first failure (which names
                               the failed Action); e.g.
                               winuse batch 101 click name:File then type-text hello then press-key Return
                               A Batch has 60 s; --timeout before the first
                               Action gives it up to 300
  scroll <window> <element index | name: | id:> <up|down|left|right> [steps]
                               scroll the list or page holding an Element by
                               steps (1 to 50, default 3)
  scroll <window> <element index | name: | id:> into-view
                               scroll an Element into view
  scroll <window> @x,y <dx> <dy>
                               scroll the Window at a point of its latest
                               screenshot: positive dy scrolls down, positive
                               dx right; one wheel notch is 120
  select-text <window> <element index | name: | id:> <start> <length> | all
                               select text in a field without keys or the
                               mouse: length characters (UTF-16 units) from
                               start, counting from 0, or all of it; a
                               type-text afterwards replaces the selection
  invoke-menu <window> "<menu> > <item>"
                               run a command of a classic (Win32) menu bar
                               without opening the menu, such as
                               "File > Save As..."; names match without case,
                               & and the shortcut; a greyed item is refused
                               with nothing sent
  drag <window> @x,y @x,y     drag the pointer from one point to another,
                               both in the Window's latest screenshot's pixels;
                               drag <window> <element> <element | @x,y> drags
                               a Page Element headless
  hover <window> @x,y         move the pointer to a point of the Window's
                               latest screenshot and leave it there (reveals
                               hover-only UI and tooltips); hover <window>
                               <element> hovers a Page Element headless
  activate-window <window>    bring a Window to the foreground
  restore-window <window>     restore a minimized or maximized Window to its
                               normal size without activating it (the output
                               says when the App brings itself to the front)
  minimize-window <window>    minimize a Window without activating another
  maximize-window <window>    maximize a Window for real; headed only: Windows
                               cannot maximize without bringing the Window to
                               the foreground, so without --headed nothing is
                               sent
  move-window <window> <x> <y>
                               move the Window's visible frame top-left to a
                               screen point, in physical pixels
  resize-window <window> <width> <height>
                               set the Window's visible frame size, in physical
                               pixels (both use the screenshot line's window
                               at X,Y WxH; they refuse a maximized or
                               minimized Window: restore it first)
  close-window <window>       ask a Window to close, as its close button does;
                               an app may keep it open to ask about saving
  browser status [browser]    connect to a Chromium browser (the pinned or
                               only debuggable one) and say how, or say in
                               one sentence what is wrong and what to run
  browser use [browser|auto]  pin the browser the browser commands use (it
                               persists); auto removes the pin, no argument
                               prints it
  browser launch [browser]    start the browser with winuse's own profile and
                               connect (a normal browser window opens; it
                               keeps rendering while covered)
  browser clone [browser] [--profile <folder>] [--fresh] [--close-source]
                               copy your real profile (your logins) to
                               winuse's clone, keep it synced, launch it and
                               connect; --close-source first closes the
                               running browser cleanly so logins copy, and
                               reopens its tabs in the clone
  browser connect [browser]   use your running browser through its inspect
                               toggle: you turn on remote debugging, and
                               Chrome asks once to allow the connection
  browser stop [browser]      close a browser winuse launched or cloned,
                               keeping its profile
  browser ext install [browser|all]
                               set up the winuse extension, which drives your
                               running browser with your logins and no
                               prompts, and print the one step the user does
                               (Developer mode, Load unpacked)
  browser ext status [browser]
                               say whether the extension is connected, or in
                               one sentence what is missing and what to run
  browser attach <window>     give a running Electron App's Window a Browser
                               Connection: opens the App's Node.js inspector
                               and runs winuse's code inside the App, so ask
                               the user first; refused when the App turned
                               its inspector off (nodeCliInspect fuse)
  browser detach <window>     end the attach and close the App's inspector
  tab list <window>           list the Tabs of a browser Window with a Browser
                               Connection, one per line: * for the active
                               Tab, its id, title and URL
  tab new <window> [url]      open a Tab in the background of that Window and
                               print its id
  tab close <window> <tab>    close a Tab by its id from tab list (4 or more
                               characters of it); the last Tab is refused,
                               run close-window to close the Window
  tab switch <window> <tab>   make a Tab the active one in its Window (the
                               Page commands act on the active Tab); headless,
                               --headed switches with real keys
  navigate <window> <url> [--wait load|domcontentloaded|none]
                               open a URL in the Window's active Tab, wait for
                               it to load (default load), and print the URL it
                               ended on; never type into the address bar
  back <window> [--wait ...]  go one page back in the Tab's history
  forward <window> [--wait ...]
                               go one page forward in the Tab's history
  reload <window> [--wait ...]
                               reload the Tab's page
  upload <window> [<element>] <file>...
                               give files to a file input of the Page, or
                               without an element to the file chooser a click
                               opened (winuse holds it back, so no file dialog
                               shows)
  dialog <window> accept|dismiss [text]
                               answer the JavaScript dialog the Page shows
                               (alert, confirm, prompt, or leave the page);
                               text answers a prompt. While one shows, every
                               other Page Action fails and says so
  wait <window> selector <css> | text <text> | url <url or pattern> | load [domcontentloaded] | download
                               wait until an element shows, the text appears,
                               the URL matches (* is a wildcard), or the page
                               loads (10 s by default, --timeout <seconds> up
                               to 300; in a batch, the Batch's budget bounds
                               it); wait download prints the path of the
                               file a click downloaded, on a browser winuse
                               launched or cloned
  eval <window> <expression>  run a JavaScript expression in the Page and
                               print its value (JSON for objects, cut at 4000
                               characters)
  clipboard-get [--format text|image|files] [--out <path>]
                               print the clipboard's text exactly as it is
                               (the default), its files one path per line, or
                               write its image to a PNG (--out, or a new file
                               in %TEMP%\winuse) and print the path and size;
                               a clipboard without that format fails and says
                               what it holds
  clipboard-set <text> | --image <png> | --file <path>...
                               replace the clipboard's content with the text
                               (literal, like type-text's), a PNG image or a
                               list of existing files
  clipboard-clear             empty the clipboard
  list-displays               list the displays, one per line: its number (0
                               is the primary display), its rectangle "x,y WxH"
                               in physical pixels, its scale, and primary
  screenshot --display <n>    save a screenshot of display n from list-displays
                               and print its path, like get-window-state
                               --screenshot's line; it has no @x,y points or
                               zoom, which belong to a Window's screenshot;
                               --screen <n> is the same (agent-desktop's
                               spelling)
  skill                       print the agent skill (SKILL.md) this binary
                               was built with; read it again after an upgrade
  skill install <dir>         write that SKILL.md to <dir>\winuse\SKILL.md, for
                               a skills folder such as ~\.agents\skills
  status                      print one line each: the winuse and helper
                               versions and the helper's uptime, whether the
                               desktop is locked, the integrity level, DPI
                               awareness, the Browser Connections the helper
                               holds and the Windows build. Read-only: it
                               never starts the helper (it says "not
                               running") and never connects a browser
  trace show <file>           print a trace file readably: per command its
                               time, Agent, Window, routes, outcome and
                               duration, then its arguments, error and files
  version                     print the winuse version

a <window> is a Window ID, or an App name when that App has exactly one Window;
an element index, name:<text>, id:<AutomationId> or @x,y (the latest
screenshot's pixels) name an Element of the Window's latest Window State
`

// Usage is the short command surface (grows as tickets land). The
// perform-secondary-action line lists the Secondary Actions from uia's
// canonical table, so this text, the Window State's listing and the
// Action validation share one source (finding 18).
var Usage = usageHead + secondaryActionsPhrase() + usageTail

// secondaryActionsPhrase renders the canonical Secondary Actions for the
// usage text, comma-joined and wrapped at the usage's column.
func secondaryActionsPhrase() string {
	const (
		indent = "                               "
		width  = 72
	)
	var lines []string
	line := ""
	for i, name := range uia.SecondaryActionNames {
		item := name
		if i > 0 {
			item = ", " + name
		}
		if line != "" && len(indent)+len(line)+len(item) > width {
			lines = append(lines, line+",")
			line = name
			continue
		}
		line += item
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"+indent)
}

// Main runs the CLI for args (without the program name) and returns the exit
// code: 0 success, 1 command failure, 2 usage error. The first command
// invocation starts the background helper automatically; later ones reuse
// it. status never starts it (status.go).
//
// The global --headed flag (headed.go, ADR 0010) is taken out first,
// before the command or anywhere after it outside literal text, and
// forwarded to the Actions that have a Headless form, and to
// get-window-state and zoom, whose captures are headless (ADR 0011).
func Main(args []string) (code int) {
	// The session trace (trace.go, desktop-parity ticket 19): one line per
	// command, written when it exits.
	args, trace, ok := startTrace(args, os.Getenv)
	if !ok {
		return 2
	}
	activeTrace = trace
	defer func() { trace.finish(code) }()
	args, headed := extractHeaded(args)
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, Usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Println(version.Version)
		return 0
	case "list-windows":
		return listWindowsCmd(args[1:])
	case "get-window":
		return getWindowCmd(args[1:])
	case "get-window-state":
		return getWindowStateCmd(args[1:], headed)
	case "record-tree":
		return recordTreeCmd(args[1:])
	case "list-apps":
		return listAppsCmd(args[1:])
	case "launch-app":
		return launchAppCmd(args[1:])
	case "close-app":
		return closeAppCmd(args[1:]) // closeapp.go, desktop-parity ticket 10
	case "zoom":
		return zoomCmd(args[1:], headed)
	case "click":
		return clickCmd(args[1:], headed)
	case "type-text":
		return typeTextCmd(args[1:], headed)
	case "press-key":
		return pressKeyCmd(args[1:], headed)
	case "set-value":
		return setValueCmd(args[1:], headed)
	case "perform-secondary-action":
		return performSecondaryActionCmd(args[1:], headed)
	case "batch":
		return batchCmd(args[1:], headed)
	case "scroll":
		return scrollCmd(args[1:], headed)
	case "select-text":
		return selectTextCmd(args[1:])
	case "invoke-menu":
		return invokeMenuCmd(args[1:])
	case "drag":
		return dragCmd(args[1:], headed)
	case "hover":
		return hoverCmd(args[1:], headed)
	case "activate-window":
		return activateWindowCmd(args[1:])
	case "restore-window", "minimize-window", "close-window":
		return windowActionCmd(args[0], args[1:])
	case "move-window":
		return moveWindowCmd(args[1:])
	case "resize-window":
		return resizeWindowCmd(args[1:])
	case "maximize-window":
		return maximizeWindowCmd(args[1:], headed) // window.go, desktop-parity ticket 11
	case "browser":
		return browserCmd(args[1:])
	case "tab":
		return tabCmd(args[1:], headed)
	case "navigate", "back", "forward", "reload", "upload", "dialog", "wait", "eval":
		return pageCmd(args[0], args[1:]) // page.go, browser merge ticket 06
	case "list-displays":
		return listDisplaysCmd(args[1:]) // display.go, desktop-parity ticket 08
	case "screenshot":
		return screenshotCmd(args[1:])
	case "clipboard-get":
		return clipboardGetCmd(args[1:]) // clipboard.go, desktop-parity ticket 01
	case "clipboard-set":
		return clipboardSetCmd(args[1:])
	case "clipboard-clear":
		return clipboardClearCmd(args[1:])
	case "skill":
		return skillCmd(args[1:])
	case "status":
		return statusCmd(args[1:])
	case "trace":
		return traceCmd(args[1:]) // trace.go, desktop-parity ticket 19
	default:
		fmt.Fprintf(os.Stderr, "winuse: unknown command %q\n\n%s", args[0], Usage)
		return 2
	}
}

const listWindowsUsage = "usage: winuse list-windows [--details] [--app <name>]"

// listWindowsCmd prints one line per visible Window:
// "<Window ID>\t<App>\t<title>". --details adds the details column before
// the title; --app keeps one App's Windows (desktop-parity 22). Without
// flags the request carries no arguments, Codex's list_windows.
func listWindowsCmd(args []string) int {
	a, err := parseListWindowsArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, listWindowsUsage)
		return 2
	}
	if a == (protocol.ListWindowsArgs{}) {
		err = runSimple("list-windows")
	} else {
		err = runWithArgs("list-windows", a)
	}
	if err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// parseListWindowsArgs parses list-windows's flags: --details, and
// --app <name> or --app=<name>.
func parseListWindowsArgs(args []string) (protocol.ListWindowsArgs, error) {
	var a protocol.ListWindowsArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--details":
			a.Details = true
		case arg == "--app" || strings.HasPrefix(arg, "--app="):
			if a.App != "" {
				return protocol.ListWindowsArgs{}, errors.New("--app is given twice")
			}
			name, ok := strings.CutPrefix(arg, "--app=")
			if !ok {
				if i+1 >= len(args) {
					return protocol.ListWindowsArgs{}, errors.New("--app takes an App name")
				}
				i++
				name = args[i]
			}
			if strings.TrimSpace(name) == "" {
				return protocol.ListWindowsArgs{}, errors.New("--app takes an App name")
			}
			a.App = name
		default:
			return protocol.ListWindowsArgs{}, fmt.Errorf("list-windows: unexpected argument %q", arg)
		}
	}
	return a, nil
}

// getWindowCmd implements `winuse get-window <window>`: print the one
// list-windows line of the resolved Window (Codex's get_window — the
// re-resolve step after a helper restart or a stale Window ID).
func getWindowCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: winuse get-window <window>")
		return 2
	}
	if err := runWithArgs("get-window", protocol.GetWindowArgs{Window: args[0]}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// windowArgs is the CLI→helper argument shape of commands that name a
// Window.
type windowArgs struct {
	Window     string `json:"window"`
	Screenshot bool   `json:"screenshot"`       // get-window-state --screenshot
	Headed     bool   `json:"headed,omitempty"` // the global --headed flag: the screenshot activates first (ADR 0011)
}

// recordArgs is record-tree's CLI→helper argument shape.
type recordArgs struct {
	Window string `json:"window"`
	File   string `json:"file"`
}

// windowStateUsage is get-window-state's usage line.
const windowStateUsage = "usage: winuse get-window-state <window> [--screenshot] [--timeout <seconds>]"

// getWindowStateCmd prints the Window State: Codex's depth-first,
// indented, numbered Element tree — with --screenshot, the screenshot line
// after it. headed is the global --headed flag; --timeout widens the
// request budget (up to 60 s) and the Chromium settle with it.
func getWindowStateCmd(args []string, headed bool) int {
	args, timeout, err := takeTimeout("get-window-state", args)
	var req windowArgs
	if err == nil {
		req, err = windowStateRequest(args, headed)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, windowStateUsage)
		return 2
	}
	if err := runWithTimeout("get-window-state", req, timeout); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// recordTreeCmd records a real Window's tree to a fixture file (the
// recorder; see the README).
func recordTreeCmd(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: winuse record-tree <window> <file>")
		return 2
	}
	file, err := filepath.Abs(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: resolving %s: %v\n", args[1], err)
		return 2
	}
	if err := runWithArgs("record-tree", recordArgs{Window: args[0], File: file}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// listAppsCmd prints one line per App (name, running state, last used,
// use count, launch identifier) and one line per Window of running Apps.
func listAppsCmd(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "winuse: list-apps takes no arguments")
		return 2
	}
	if err := runSimple("list-apps"); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// launchAppCmd starts an App by name or executable path and prints the
// Window it opened (one list-windows-style line, then a note line when the
// App took the foreground). Paths — the App's and --cwd — are resolved
// against the CLI's working directory: the helper cannot see it.
// --timeout widens the 15 s launch budget (up to 120 s) for a slow App.
func launchAppCmd(args []string) int {
	const usage = "usage: winuse launch-app <name|exe path> [--minimized] [--cdp] [--arg <a>]... [--cwd <dir>] [--env KEY=VALUE]... [--timeout <seconds>]"
	a, timeout, err := parseLaunchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, usage)
		return 2
	}
	if err := runWithTimeout("launch-app", a, timeout); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}

// launchValueFlag reports whether a launch-app flag takes the next token
// as its value, whatever that token looks like.
func launchValueFlag(tok string) bool {
	return tok == "--arg" || tok == "--cwd" || tok == "--env"
}

// parseLaunchArgs parses launch-app's command line in order: the App, and
// the flags anywhere around it. --arg and --env repeat and keep their
// order; the token after --arg, --cwd or --env is its value even when it
// starts with "-" (a flag of the App's own), and --flag=value works too.
// The App's path and --cwd become absolute here.
func parseLaunchArgs(args []string) (protocol.LaunchArgs, time.Duration, error) {
	var a protocol.LaunchArgs
	var timeout time.Duration
	var positional []string
	cwdSet := false
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if isTimeoutFlag(tok) {
			d, n, err := takeTimeoutAt("launch-app", args, i)
			if err != nil {
				return protocol.LaunchArgs{}, 0, err
			}
			timeout = d
			i += n - 1
			continue
		}
		if tok == "--minimized" {
			a.Minimized = true
			continue
		}
		if tok == "--cdp" {
			a.CDP = true
			continue
		}
		name, value, inline := strings.Cut(tok, "=")
		if !launchValueFlag(name) {
			if strings.HasPrefix(tok, "--") {
				return protocol.LaunchArgs{}, 0, fmt.Errorf("unknown flag %s", tok)
			}
			positional = append(positional, tok)
			continue
		}
		if !inline {
			if i+1 >= len(args) {
				return protocol.LaunchArgs{}, 0, fmt.Errorf("%s needs a value", name)
			}
			i++
			value = args[i]
		}
		switch name {
		case "--arg":
			a.Args = append(a.Args, value)
		case "--env":
			if _, _, err := win.ParseEnvEntry(value); err != nil {
				return protocol.LaunchArgs{}, 0, err
			}
			a.Env = append(a.Env, value)
		case "--cwd":
			if cwdSet {
				return protocol.LaunchArgs{}, 0, errors.New("--cwd given twice")
			}
			if strings.TrimSpace(value) == "" {
				return protocol.LaunchArgs{}, 0, errors.New("--cwd needs a folder")
			}
			cwdSet = true
			abs, err := filepath.Abs(value)
			if err != nil {
				return protocol.LaunchArgs{}, 0, fmt.Errorf("--cwd %q: %v", value, err)
			}
			a.Cwd = abs
		}
	}
	if len(positional) != 1 {
		return protocol.LaunchArgs{}, 0, errors.New("launch-app takes exactly one App")
	}
	a.App = positional[0]
	if apps.LooksLikePath(a.App) {
		if abs, err := filepath.Abs(a.App); err == nil {
			a.App = abs
		}
	}
	return a, timeout, nil
}

// inputDesktopExitError classifies an unavailable input desktop for the
// CLI: an interactive session whose input desktop cannot be opened is the
// lock screen / secure desktop, so the answer is the locked-desktop
// refusal — the same message and hint the helper's Actions use, exit 1 —
// never the session-0 message, which would send the agent looking for a
// service that is not the problem.
func inputDesktopExitError() error {
	err := win.InputDesktopRefusal()
	var locked *win.LockedDesktopError
	if errors.As(err, &locked) {
		return &exitError{msg: err.Error(), hint: win.LockedDesktopHint, code: 1}
	}
	return err
}

// runSimple sends a no-argument command to the helper and prints its
// response Codex-style: output on stdout, a single-line failure with a
// recovery hint on stderr, non-zero exit on failure.
func runSimple(cmd string) error {
	return runWithArgs(cmd, nil)
}

// runWithArgs sends one command (with its JSON arguments) to the helper and
// prints its response Codex-style.
func runWithArgs(cmd string, args any) error {
	return runRequest(cmd, args, nil)
}

// runRequest is runWithArgs with a hook that adjusts the request before
// it is sent (a caller-set timeout, timeout.go). edit may be nil.
func runRequest(cmd string, args any, edit func(*protocol.Request)) error {
	if !win.HasInputDesktop() {
		return inputDesktopExitError()
	}
	client, err := DefaultClient()
	if err != nil {
		return err
	}
	req, err := Request(cmd, args)
	if err != nil {
		return err
	}
	if edit != nil {
		edit(req)
	}
	req.Trace = tracing()
	resp, err := client.Call(context.Background(), req)
	activeTrace.observe(req, resp, err) // trace.go
	if err != nil {
		return err
	}
	return finish(resp)
}

// finish turns a response into an error carrying the exit code the CLI
// should use; helper-reported failures keep their exact message text.
func finish(resp *protocol.Response) error {
	if resp.OK {
		os.Stdout.WriteString(resp.Out)
		return nil
	}
	code := resp.Exit
	if code == 0 {
		code = 1
	}
	return &exitError{msg: resp.Err, hint: resp.Hint, code: code}
}

// exitError is a failure whose message is already the user-facing line,
// with a helper-suggested exit code (e.g. 130 for Esc stops).
type exitError struct {
	msg  string
	hint string
	code int
}

func (e *exitError) Error() string { return e.msg }

// ExitCode extracts the process exit code an error prescribes (default 1).
func ExitCode(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

// reportError prints a failure, Codex-style: one line, then an optional
// hint line. Helper-reported messages and request timeouts are printed
// verbatim (their text is the decided user-facing style, e.g. "winuse
// request timed out: <command>"); purely local failures get a "winuse: "
// prefix so no failure line is ever bare.
func reportError(w io.Writer, err error) {
	var timeout *TimeoutError
	if errors.As(err, &timeout) {
		fmt.Fprintln(w, timeout)
		return
	}
	var ee *exitError
	if errors.As(err, &ee) {
		fmt.Fprintln(w, ee.msg)
		if ee.hint != "" {
			fmt.Fprintf(w, "hint: %s\n", ee.hint)
		}
		return
	}
	fmt.Fprintf(w, "winuse: %v\n", err)
}
