package cli

// The help text, all in one place: Usage is the full reference (`winuse
// help all`), and commandGroups gives every command its line in the short
// index (`winuse`, `winuse help`, `--help`) and its examples, which
// `winuse help <command>` (or `<command> --help`) prints before the
// command's blocks of Usage. A new flag goes into its command's block of
// Usage; a new command gets a block there and an entry in commandGroups
// (TestEveryCommandHasHelp checks both ways).

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
)

// usageHead and usageTail sandwich the Secondary Actions list in Usage
// below (from uia's canonical table, finding 18: this text, the Window
// State's listing and the Action validation share one source). A command
// is one block of the commands: section: its form at two spaces' indent,
// then its description at the 31st column; `winuse help <command>`
// prints the command's blocks, so a new flag goes into its block here.
const usageHead = `usage: winuse [--headed] <command> [arguments]

global options:
  each goes before the command or anywhere after it, except inside
  type-text's text, set-value's value and clipboard-set's text, where it is
  literal text; set-value refuses a value that ends with winuse flags,
  with nothing sent (-- before the value sets it as written)
  --headed                   run an Action the v1 way: bring the Window to the
                               foreground, use the real pointer and keyboard,
                               and show the in-control banner
  --no-wait                   fail at once when an Action is refused for a
                               reason that may pass on its own: without it, a
                               disabled Element, a modal dialog over the
                               Window or a name:/id: reference that matches
                               nothing waits up to 1.8 s (holding up no other
                               Action; Esc stops it) and the Action runs
                               again, with a Note: waited 0.4 s for element
                               14 to enable. Only a refusal that sent nothing
                               waits; a batch's steps never do
  --trace <file>              (or WINUSE_TRACE=<file>): append one JSON line
                               per command to the file: time, command, Agent,
                               Window, arguments, routes (Headless, Posted,
                               Headed, CDP), outcome, output (4 KiB; a Window
                               State only by its line count), screenshot
                               paths and duration. Typed text, values,
                               clipboard content and eval are redacted; past
                               32 MiB the file is renamed <name>.1 and a new
                               one started
  --trace-unredacted          keep the redacted values, for local debugging only
  --json                      print one JSON line instead of the text:
                               {"ok","command","out"} and, on a failure,
                               "error" with its code, message, hint, delivery
                               and retry. Exit codes are unchanged
  --help, -h                  print the command's usage and examples instead
                               of running it, as winuse help <command> does

headless (the default; your foreground, pointer and keyboard stay untouched):
  click (left, single), set-value, perform-secondary-action (except Raise)
  and scroll on an element index, name: or id:, or at an @x,y point, which
  acts on the Window's own Element under it (refused when none can take
  the Action) and names it in the ok; select-text and
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
  they name no Window and have no headed form, and so are the
  notification commands, which press a toast's own buttons.

headed only with --headed: every other Action (double, right and middle
  clicks, drag, hover, Raise, maximize-window, open-shell, press-key and
  type-text with no headless route, and a point with no usable Element) is refused
  with "nothing was sent" and the exact --headed retry. activate-window always takes the foreground: that is
  what it is for.

act and see the effect in one call: <action> ... --observe[=diff|full]
  [--interactive] [--depth N] [--compact] [--max-chars N | --out <file>] [--full]
  after any Action on a Window or Tab, or a batch (before its first Action),
  reads the Window and prints its Window State under the output. full prints
  all of it; --observe (diff) only the Element Indexes whose line changed
  since your last read of that Window: ~ changed, * new, - gone (no Element
  has that index now); an index not listed is unchanged. No earlier read, or
  a change that moves indexes, prints the whole State with a note. Either way
  the read replaces your Window State, as get-window-state does. The budgets
  and their defaults (shared IDs left out, the 28000-character cap; --full
  for Codex's text) shape it as on a read; like --headed they go before the
  command or anywhere after it outside type-text's text and set-value's
  value. A failed Action prints none; a read that fails is a note and the
  Action's result stands.

commands:
  list-windows [--details] [--app <name>] [--shell]
                               list visible Windows: Window ID, App and title;
                               --details adds a column before the title with the
                               visible frame "x,y WxH" (what move-window and
                               resize-window take), "pid N" and the flags focused,
                               minimized, maximized and admin; --app lists one
                               App's Windows (App name or executable name);
                               --shell adds the shell surfaces: the taskbar, and
                               the tray overflow, Start and the Action Center
                               while open, read and acted on by Window ID
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
  get-window-state <window> [--screenshot] [--skeleton | --root <element>] [--depth N] [--interactive] [--compact] [--max-chars N | --out <file>] [--full] [--diff] [--timeout <seconds>]
                               print a Window's State: its Element tree as text,
                               and with --screenshot a screenshot saved to a file
                               whose path is printed (sized to the model's
                               1,568-token image budget); a Chromium or Electron
                               Window without a Browser Connection whose web
                               content reads thin is read again for up to 5 s
                               while it builds (once per Window; later reads
                               of a Window that stayed thin retry once). The
                               read has 10 s; --timeout up to 60 gives a slow
                               Electron, Office or cold Chromium tree more,
                               and the extra time goes to that 5 s wait too.
                               --skeleton reads three levels with each deeper
                               part's child count; --root <element> reads one
                               Element's subtree (--depth N levels,
                               --interactive Elements only) and adds its
                               Elements after the Window State's indexes.
                               A read leaves out the IDs that several
                               Elements share (id: refuses those) and stops
                               before 28000 characters with a note on what it
                               left out; --max-chars N sets the cap, 0 none.
                               On a full read, --interactive prints only the
                               Elements you act on, --depth N the first N
                               levels, --compact folds unnamed containers;
                               the Window State is still read and kept whole,
                               so the indexes printed are its own and the
                               rest still work. --out <file> writes the
                               Window State to the file, uncut, and prints
                               its header, path and notes. --full prints
                               Codex's text byte for byte: every ID, no cap.
                               --diff prints only the Element Indexes whose
                               line changed since your last read of the
                               Window (~ changed, * new, - gone), as
                               --observe=diff does after an Action; with no
                               earlier read, or past 60% changed, the whole
                               Window State prints with a note. It takes the
                               budgets above, not --root or --skeleton
  find <window> [--role <r>] [--name <text>] [--text <text>] [--state <s>] [--first | --limit N | --count] [--max-chars N | --full]
                               search the Window's whole live tree, past the
                               Window State's caps: print the matching
                               Elements, added after its Element Indexes
                               (the earlier ones still work); --limit N keeps
                               the first N and says how many matched;
                               --count prints how many; a role that matches
                               nothing lists the roles present. Like a read,
                               it leaves out shared IDs and stops before
                               28000 characters (--max-chars, --full)
  record-tree <window> <file> record a Window's real tree to a fixture file
  zoom <window> <x>,<y>,<w>,<h>
                               save a full-resolution crop of a region of the
                               Window's latest screenshot and print its path
  click <window> <element index | name: | id: | @x,y> [options]
                               click an Element of a Window by Element Index,
                               name: or id: reference, or at @x,y
                               coordinates from the Window's latest
                               screenshot; options: --count 1|2|3 (default 1),
                               --button left|middle|right (default left),
                               --post (post the click to the classic
                               control there without the foreground: best
                               effort, the app may ignore it),
                               --modifiers ctrl,shift,alt (held during the
                               click: headless on a Page and for ctrl or
                               shift on a multi-select list item, as MK
                               flags with --post, else --headed)
  type-text <window> [--post | --into <element index | name: | id:>] [--interval-ms <n>] <text>
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
                               nothing sent. --interval-ms, also right after
                               the Window, pauses that many milliseconds
                               (0 to 1000) between characters when the text
                               is typed key by key (--headed, --post), for
                               Apps that drop fast input
  press-key <window> <key> [--post] [--force]
                               press a key or key combination by Codex's key
                               names, e.g. Return, Tab, Control_L+a, Numpad_0
                               (Return or Escape on a classic dialog presses
                               its default or Cancel button headless).
                               --post posts the key to the focused control
                               without the foreground instead: best effort,
                               and the app may ignore it (the output says
                               posted; read the Window State to check); a
                               chord with Control, Shift or Alt (not Control
                               and Alt together) is posted with the
                               modifiers held for the Window's thread
                               only. --post and --headed do not combine.
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
                               under a point of the latest screenshot. The
                               value is everything after the Element: one
                               ending with winuse flags (--observe, --headed,
                               --no-wait, --timeout ...) is refused, nothing
                               sent; put flags before the Element, or -- before
                               a value that really ends so
  perform-secondary-action <window> <element index | name: | id: | @x,y> <action>
                               perform one of the Element's Secondary Actions
                               (`

const usageTail = `);
                               @x,y acts on the Element under a point of the
                               latest screenshot
  batch <window> [--timeout <seconds>] [--stop-on-error] [--observe[=diff|full]] <action> [arguments] [then <action> [arguments]] ...
                               run several Actions on one Window in order and
                               print each one's output as Action N (<action>):
                               ...; e.g.
                               winuse batch 101 click name:File then type-text hello then press-key Return
                               A failed Action does not stop the Batch: the
                               rest run, and the Batch then fails (exit 1)
                               naming every failed Action, uncertain ones
                               marked; --stop-on-error stops at the first.
                               A Batch has 60 s; --timeout before the first
                               Action gives it up to 300. An Action starting
                               in <window> acts in that Window, and in new in
                               the Window the Action before it opened.
                               get-window-state and find are steps too, with
                               their own flags and no Window, so one Batch can
                               act, read and act again; --observe before the
                               first Action reads the Batch's Window once
                               after the last (see --observe below)
  batch -                    the same Batch read from stdin: the Window and
                               the Batch flags on the first line, then one
                               Action per line with no then; double quotes
                               group words (\" is a quote inside them), and
                               blank lines and lines starting with # are
                               skipped; --observe goes on the command line
  scroll <window> <element index | name: | id:> <up|down|left|right> [steps]
                               scroll the list or page holding an Element by
                               steps (1 to 50, default 3)
  scroll <window> <element index | name: | id:> into-view
                               scroll an Element into view
  scroll <window> @x,y <dx> <dy> [--post] [--modifiers ctrl,shift,alt]
                               scroll the Window at a point of its latest
                               screenshot: positive dy scrolls down, positive
                               dx right; one wheel notch is 120 (three
                               steps of the list or page under the point,
                               up to 16 notches; --headed turns the real
                               wheel; --post posts the wheel to the classic
                               control there, best effort)
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
                               a Page Element headless, and any other Element
                               from its clickable point with --headed;
                               --drop-delay <ms> holds at the end before the
                               drop
  hover <window> @x,y         move the pointer to a point of the Window's
                               latest screenshot and leave it there (reveals
                               hover-only UI and tooltips); hover <window>
                               <element> hovers a Page Element headless, and
                               any other Element at its clickable point with
                               --headed
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
  browser ext install [browser|all] [--open]
                               set up the winuse extension, which drives your
                               running browser with your logins and no
                               prompts, and print the one step the user does
                               (Developer mode, Load unpacked); --open also
                               opens the extensions page and the folder
  browser ext status [browser] [--wait <seconds>]
                               say whether the extension is connected, or in
                               one sentence what is missing and what to run;
                               --wait polls until it connects (up to 600 s)
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
                               print its id; work in it as <window>:<tab>
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
  wait <window> <element index | name: | id:> enabled | disabled | gone | value <value> | checked | unchecked | expanded | collapsed
                               wait until an Element of the Window's latest
                               Window State reaches that state (gone: the
                               Element is gone, its Window closed too); text
                               on a Window that is no browser waits in its
                               UI Automation tree. Both read the Window
                               without changing its Element Indexes, and fail
                               when it closes
  wait window <app or title> [--gone]
                               wait until a Window of that App, or whose title
                               holds the text, shows, and print its line;
                               --gone: until none is left. Desktop waits only
                               read and poll, take --timeout like the Page's,
                               and never hold up an Action
  wait <milliseconds>         pause, up to 300000 (300 s)
  eval <window> <expression> | --file <path or -> [--max <n>]
                               run a JavaScript expression in the Page and
                               print its value (JSON for objects, cut at 4000
                               characters or --max, up to 100000); --file
                               reads it from a file or stdin, past any shell
                               quoting
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
  list-notifications [--app <name>] [--text <text>]
                               list the notifications the toast banners show
                               now, one per line: its number, App, title,
                               text and buttons; --app and --text keep the
                               ones whose App, or title or text, hold that
                               text. A banner shows about 5 s; older ones
                               are in the Notification Center (open-shell
                               notifications)
  notification-action <n> <button>
                               press a button of notification n from your
                               latest list-notifications or wait
                               notification, headless; refused with nothing
                               sent when that notification no longer shows
                               with the App, title and text it was listed
                               with. The output says when the foreground
                               moved (an App that is not running hands it
                               to the shell)
  notification-dismiss <n>    close notification n's banner the same way;
                               Windows keeps it in the Notification Center
  wait notification [--app <name>] [--text <text>]
                               wait until a banner shows a matching
                               notification and list it, numbered; takes
                               --timeout like the other waits
  open-shell start|notifications|quick-settings|overflow
                               headed only: open Start (Control_L+Escape),
                               the notification panel (Win+N), quick
                               settings (Win+A) or the tray overflow (a
                               click on the hidden-icons chevron) and print
                               its line; read it with get-window-state.
                               Without --headed nothing is sent; one open
                               already is printed with nothing sent
  skill [<topic> | list | all]
                               print the agent skill this binary was built
                               with: its core (SKILL.md), one topic of it,
                               the topics with what each covers, or all of
                               it; read it again after an upgrade
  skill install <dir>         write the skill's folder (SKILL.md and its
                               topics) to <dir>\winuse, for a skills folder
                               such as ~\.claude\skills or ~\.agents\skills
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
  update [--to <version>] [--no-skill]
                               replace this winuse.exe with the latest
                               release, or the one --to names (older ones
                               too), checked against its SHA256SUMS, and
                               rewrite the skill copies in ~\.agents\skills
                               and ~\.claude\skills; a copy that links
                               elsewhere (Fleet) is left alone, and
                               --no-skill skips them all. A running helper
                               finishes its requests and restarts at the
                               next command; nothing changes when the
                               download fails
  version                     print the winuse version
  help [<command> | global | all]
                               print the command index, a command's examples
                               and usage, the global options, or this whole
                               reference; exit 0

a <window> is a Window ID, or an App name when that App has exactly one Window;
an element index, name:<text>, id:<AutomationId> or @x,y (the latest
screenshot's pixels) name an Element of the Window's latest Window State.
A Tab target, <window>:<tab> (a tab id from tab list or tab new), names a Tab
of that browser shown or not: get-window-state, click, set-value, scroll,
hover, drag, select-text, type-text, press-key, upload, navigate, back,
forward, reload, dialog, wait, eval and batch read and act on that Tab
through its Browser Connection, headless, without switching the Tab the
user sees; its Element Indexes are its own, and --headed, --post and @x,y
are refused
`

// Usage is the full reference, `winuse help all` (it grows as tickets
// land). The
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

// commandHelp is one command's line in the index and its examples.
type commandHelp struct {
	name     string   // the command's first word, as its Usage blocks start
	summary  string   // the index's one line
	examples []string // full command lines, shown first by help <command>
}

// commandGroups is the index, in the order it prints.
var commandGroups = []struct {
	title    string
	commands []commandHelp
}{
	{"Find and start Windows", []commandHelp{
		{"list-windows", "the visible Windows: Window ID, App, title", []string{
			"winuse list-windows",
			"winuse list-windows --app notepad --details"}},
		{"get-window", "re-resolve one Window and print its line", []string{
			"winuse get-window 4242"}},
		{"list-apps", "running and recent Apps, with their Windows", []string{
			"winuse list-apps"}},
		{"launch-app", "start an App and print the Window it opened", []string{
			"winuse launch-app notepad",
			`winuse launch-app "C:\Tools\app.exe" --arg --verbose --cwd C:\work`}},
		{"close-app", "ask every Window of an App to close", []string{
			"winuse close-app notepad"}},
	}},
	{"Read a Window", []commandHelp{
		{"get-window-state", "the Element tree as text, --screenshot too", []string{
			"winuse get-window-state 4242",
			"winuse get-window-state 4242 --screenshot",
			"winuse get-window-state 4242 --skeleton",
			"winuse get-window-state 4242 --root 42 --depth 2 --interactive",
			"winuse get-window-state 4242 --interactive --compact --max-chars 20000",
			"winuse get-window-state 4242 --out state.txt",
			"winuse get-window-state 4242 --full",
			"winuse get-window-state 4242 --diff"}},
		{"find", "search the whole live tree: role, name, text", []string{
			"winuse find 4242 --role button --name save",
			`winuse find 4242 --text "invoice" --first`,
			`winuse find 4242 --role "list item" --limit 20`}},
		{"zoom", "a full-resolution crop of the latest screenshot", []string{
			"winuse zoom 4242 100,80,400,300"}},
	}},
	{"Act in a Window (Headless unless --headed)", []commandHelp{
		{"click", "click an Element, or at an @x,y point", []string{
			"winuse click 4242 3",
			`winuse click 4242 "name:Save As"`,
			"winuse click 4242 id:okButton",
			"winuse click 4242 @412,88",
			"winuse click 4242 id:okButton --observe"}},
		{"set-value", "fill a field, set a toggle, pick an item", []string{
			`winuse set-value 4242 id:searchBox "invoice 2026"`,
			`winuse set-value 4242 "name:Notify me" on`}},
		{"type-text", "type text into the focused or a named field", []string{
			"winuse type-text 4242 hello world",
			"winuse type-text 4242 --into id:searchBox invoice 2026"}},
		{"press-key", "press a key or a chord", []string{
			"winuse press-key 4242 Return",
			"winuse press-key 4242 Control_L+s"}},
		{"perform-secondary-action", "Expand, Toggle, Select, ContextMenu...", []string{
			"winuse perform-secondary-action 4242 5 Expand",
			"winuse perform-secondary-action 4242 7 ContextMenu"}},
		{"select-text", "select text in a field", []string{
			"winuse select-text 4242 1 0 5",
			"winuse select-text 4242 id:searchBox all"}},
		{"invoke-menu", "run a classic menu bar command", []string{
			`winuse invoke-menu 4242 "File > Save As..."`}},
		{"scroll", "scroll a list or page, or an Element into view", []string{
			"winuse scroll 4242 12 down 5",
			"winuse scroll 4242 name:Results into-view",
			"winuse scroll 4242 @400,300 0 360"}},
		{"drag", "drag (a Page Element Headless, else --headed)", []string{
			"winuse --headed drag 4242 id:card id:done",
			"winuse --headed drag 4242 @100,200 @400,200"}},
		{"hover", "hover (a Page Element Headless, else --headed)", []string{
			"winuse hover 4242 12",
			"winuse --headed hover 4242 @300,40"}},
		{"batch", "several Actions in one call", []string{
			"winuse batch 4242 click id:field then type-text hello then press-key Return",
			"winuse batch 4242 click name:Delete then in new press-key Return",
			"winuse batch 4242 --observe set-value id:qty 3 then press-key Return",
			"winuse batch 4242 click name:Open then in new get-window-state --interactive"}},
		{"wait", "wait for text, a state, a Window or a Page", []string{
			`winuse wait 4242 text "Saved"`,
			"winuse wait 4242 name:Next enabled",
			`winuse wait window "Save As"`,
			`winuse wait 4242 selector "#results"`}},
	}},
	{"Manage Windows", []commandHelp{
		{"activate-window", "bring a Window to the foreground", []string{
			"winuse activate-window 4242"}},
		{"restore-window", "un-minimize or un-maximize, Headless", []string{
			"winuse restore-window 4242"}},
		{"minimize-window", "minimize a Window", []string{
			"winuse minimize-window 4242"}},
		{"maximize-window", "maximize a Window (needs --headed)", []string{
			"winuse --headed maximize-window 4242"}},
		{"move-window", "move a Window's frame, physical pixels", []string{
			"winuse move-window 4242 0 0"}},
		{"resize-window", "resize a Window's frame, physical pixels", []string{
			"winuse resize-window 4242 1280 800"}},
		{"close-window", "ask a Window to close", []string{
			"winuse close-window 4242"}},
	}},
	{"Browsers and Pages", []commandHelp{
		{"browser", "connect to a Chromium browser, and how", []string{
			"winuse browser status",
			"winuse browser ext install chrome --open",
			"winuse browser ext status chrome --wait 120"}},
		{"tab", "list, open, switch and close Tabs", []string{
			"winuse tab list 4242",
			"winuse tab new 4242 https://example.com",
			"winuse click 4242:1a2b3c4d 7"}},
		{"navigate", "open a URL in the Tab and wait for the load", []string{
			"winuse navigate 4242 https://example.com",
			"winuse navigate 4242:1a2b3c4d https://example.com --wait domcontentloaded"}},
		{"back", "go back in the Tab's history", []string{
			"winuse back 4242"}},
		{"forward", "go forward in the Tab's history", []string{
			"winuse forward 4242"}},
		{"reload", "reload the Tab's page", []string{
			"winuse reload 4242"}},
		{"eval", "run JavaScript in the Page, print the value", []string{
			"winuse eval 4242 document.title",
			"winuse eval 4242 --file x.js --max 20000"}},
		{"upload", "give files to a file input or chooser", []string{
			`winuse upload 4242 7 C:\reports\q3.pdf`,
			`winuse upload 4242 C:\reports\q3.pdf`}},
		{"dialog", "answer the Page's JavaScript dialog", []string{
			"winuse dialog 4242 accept",
			`winuse dialog 4242 accept "my answer"`}},
	}},
	{"Desktop", []commandHelp{
		{"clipboard-get", "the clipboard's text, files or image", []string{
			"winuse clipboard-get",
			"winuse clipboard-get --format image --out shot.png"}},
		{"clipboard-set", "replace the clipboard: text, an image, files", []string{
			"winuse clipboard-set some text",
			"winuse clipboard-set --file report.pdf"}},
		{"clipboard-clear", "empty the clipboard", []string{
			"winuse clipboard-clear"}},
		{"list-displays", "the displays, their rectangles and scales", []string{
			"winuse list-displays"}},
		{"screenshot", "a screenshot of a whole display", []string{
			"winuse screenshot --display 0"}},
		{"list-notifications", "the toast banners showing now, numbered", []string{
			"winuse list-notifications --app Outlook"}},
		{"notification-action", "press a toast's button", []string{
			"winuse notification-action 1 Reply"}},
		{"notification-dismiss", "close a toast's banner", []string{
			"winuse notification-dismiss 1"}},
		{"open-shell", "open Start, notifications... (needs --headed)", []string{
			"winuse --headed open-shell start"}},
	}},
	{"winuse itself", []commandHelp{
		{"skill", "the agent guide; skill list for its topics", []string{
			"winuse skill",
			"winuse skill list",
			"winuse skill browser",
			`winuse skill install "$env:USERPROFILE\.claude\skills"`}},
		{"status", "versions, helper, lock, browsers, agents", []string{
			"winuse status"}},
		{"trace", "summarize a --trace file", []string{
			"winuse --trace run.jsonl click 4242 3",
			"winuse trace show run.jsonl"}},
		{"update", "install the latest or a given release", []string{
			"winuse update",
			"winuse update --to 0.4.0"}},
		{"version", "the winuse version", []string{
			"winuse version"}},
		{"help", "this index, or one command's help", []string{
			"winuse help click",
			"winuse click --help",
			"winuse help all"}},
		{"record-tree", "record a tree fixture (winuse's own tests)", []string{
			"winuse record-tree 4242 notepad.json"}},
	}},
}

const (
	helpFlag      = "--help"
	helpShortFlag = "-h"
)

// isHelpRequest reports whether a command line's first word asks for help.
func isHelpRequest(word string) bool {
	return word == "help" || word == helpFlag || word == helpShortFlag
}

// commandIndex is what `winuse`, `winuse help` and `winuse --help` print:
// one line per command, then where the rest is.
func commandIndex() string {
	var b strings.Builder
	b.WriteString("usage: winuse [global options] <command> [arguments]\n")
	for _, g := range commandGroups {
		fmt.Fprintf(&b, "\n%s:\n", g.title)
		for _, c := range g.commands {
			if len(c.name) > 20 {
				fmt.Fprintf(&b, "  %s\n  %-20s %s\n", c.name, "", c.summary)
				continue
			}
			fmt.Fprintf(&b, "  %-20s %s\n", c.name, c.summary)
		}
	}
	b.WriteString(`
<window> is a Window ID from list-windows; <ref> an Element Index, name:<text>,
id:<AutomationId> or @x,y from your latest get-window-state.
global options: --headed, --no-wait, --json, --trace <file>, --help (winuse help global).
winuse help <command>: its examples and usage. winuse help all: everything.
winuse skill: the agent guide.
`)
	return b.String()
}

// usageBlock is one command form of Usage's commands: section, with its
// description.
type usageBlock struct {
	words []string // the form's words: the command, then its arguments
	text  string   // the block's lines as Usage prints them
}

// usageBlocks splits Usage's commands: section into its blocks: a line at
// two spaces' indent starts one, deeper lines continue it.
func usageBlocks() []usageBlock {
	_, section, _ := strings.Cut(Usage, "\ncommands:\n")
	section, _, _ = strings.Cut(section, "\n\n")
	var blocks []usageBlock
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "   ") && len(blocks) > 0 {
			blocks[len(blocks)-1].text += "\n" + line
			continue
		}
		form, _, _ := strings.Cut(strings.TrimSpace(line), "  ")
		blocks = append(blocks, usageBlock{words: strings.Fields(form), text: line})
	}
	return blocks
}

// blocksFor returns the Usage blocks of a command: every block of its
// first word, narrowed to the blocks that go on with the other words when
// some do (help browser ext: the browser ext blocks).
func blocksFor(words []string) []usageBlock {
	var all, narrow []usageBlock
	for _, b := range usageBlocks() {
		if len(b.words) == 0 || b.words[0] != words[0] {
			continue
		}
		all = append(all, b)
		if len(b.words) >= len(words) && slices.Equal(b.words[1:len(words)], words[1:]) {
			narrow = append(narrow, b)
		}
	}
	if len(narrow) > 0 {
		return narrow
	}
	return all
}

// lookupHelp finds a command's commandHelp.
func lookupHelp(name string) (commandHelp, bool) {
	for _, g := range commandGroups {
		for _, c := range g.commands {
			if c.name == name {
				return c, true
			}
		}
	}
	return commandHelp{}, false
}

// commandUsage is `winuse help <command...>`: the command's examples, its
// Usage blocks, and how to name Windows and Elements when it takes them.
func commandUsage(words []string) (string, bool) {
	c, ok := lookupHelp(words[0])
	blocks := blocksFor(words)
	if !ok || len(blocks) == 0 {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "winuse %s: %s\n\nexamples:\n", c.name, c.summary)
	for _, e := range c.examples {
		fmt.Fprintf(&b, "  %s\n", e)
	}
	b.WriteString("\nusage:\n")
	window := false
	for _, bl := range blocks {
		b.WriteString(bl.text + "\n")
		window = window || strings.Contains(bl.text, "<window>")
	}
	if protocol.Observable(c.name) {
		b.WriteString(`
--observe prints the Window State after it: --observe (diff) the Element
Indexes whose line changed since your last read (~ changed, * new, - gone;
the rest are unchanged), --observe=full all of it; --interactive, --depth N,
--compact, --max-chars N, --out <file> and --full shape it as on a read (winuse help all).
`)
	}
	if window {
		b.WriteString(`
<window> is a Window ID from list-windows, or an App name when that App has
exactly one Window; <ref> an Element Index, name:<text>, id:<AutomationId>
or @x,y from your latest get-window-state of it. A browser Window's Tab is
<window>:<tab>.
`)
	}
	b.WriteString("global options: winuse help global; every command: winuse help.\n")
	return b.String(), true
}

// globalHelp is `winuse help global`: Usage from its global options to
// its commands, then what names a Window and an Element.
func globalHelp() string {
	_, head, _ := strings.Cut(Usage, "\nglobal options:\n")
	head, _, _ = strings.Cut(head, "\ncommands:\n")
	_, tail, _ := strings.Cut(Usage, "\ncommands:\n")
	_, tail, _ = strings.Cut(tail, "\n\n")
	return "global options:\n" + head + "\n" + tail
}

// helpCmd implements `winuse help`, `winuse --help` and `<command>
// --help`: the index, one command's help, the global options or the full
// reference, on stdout with exit 0. An unknown command is a usage error.
func helpCmd(args []string) int {
	switch {
	case len(args) == 0:
		io.WriteString(os.Stdout, commandIndex())
		return 0
	case len(args) == 1 && args[0] == "all":
		io.WriteString(os.Stdout, Usage)
		return 0
	case len(args) == 1 && args[0] == "global":
		io.WriteString(os.Stdout, globalHelp())
		return 0
	}
	text, ok := commandUsage(args)
	if !ok {
		return unknownCommand(args[0])
	}
	io.WriteString(os.Stdout, text)
	return 0
}

// unknownCommand reports a command winuse does not have, with the nearest
// one's usage line when a near one exists, and exits 2.
func unknownCommand(name string) int {
	near := nearestCommands(name)
	if len(near) == 0 {
		fmt.Fprintf(os.Stderr, "winuse: unknown command %q\nrun winuse help for the commands\n", name)
		return 2
	}
	fmt.Fprintf(os.Stderr, "winuse: unknown command %q; did you mean %s?\n", name, strings.Join(near, " or "))
	if blocks := blocksFor(near[:1]); len(blocks) > 0 {
		fmt.Fprintf(os.Stderr, "usage: winuse %s\n", strings.Join(blocks[0].words, " "))
	}
	fmt.Fprintf(os.Stderr, "run winuse help %s for examples\n", near[0])
	return 2
}

// nearestCommands returns the commands a mistyped name most likely meant:
// the closest by edit distance within a budget that grows with the name,
// else the one command the name starts.
func nearestCommands(name string) []string {
	name = strings.ToLower(name)
	var names []string
	for _, g := range commandGroups {
		for _, c := range g.commands {
			names = append(names, c.name)
		}
	}
	budget := 1
	switch {
	case len(name) > 8:
		budget = 3
	case len(name) > 4:
		budget = 2
	}
	best, near := budget+1, []string(nil)
	for _, n := range names {
		switch d := editDistance(name, n); {
		case d < best:
			best, near = d, []string{n}
		case d == best:
			near = append(near, n)
		}
	}
	if len(near) > 0 {
		return near
	}
	var prefixed []string
	for _, n := range names {
		if len(name) >= 3 && strings.HasPrefix(n, name) {
			prefixed = append(prefixed, n)
		}
	}
	if len(prefixed) == 1 {
		return prefixed
	}
	return nil
}

// editDistance is the Levenshtein distance between a and b, by bytes.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
