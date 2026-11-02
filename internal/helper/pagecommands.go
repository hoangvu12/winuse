package helper

// The page-only Actions (browser merge ticket 06, spec "Routing and page
// Actions", ADR 0012): navigate, back, forward, reload, upload, dialog,
// wait and eval act on the active Tab of a browser Window over its Browser
// Connection. They are Headless and have no Headed form: no activation,
// no pointer, no keys, no banner, a passive Esc scope, and the Agent
// Cursor where there is a target (the address bar for navigation, the
// Page's document for a dialog, the file input for upload; none for wait
// and eval).
//
// The Tab is the one the Window shows now (page.FindTab, ticket 04's
// rule), so these need no Window State first. Its session is the Window
// State reader's own (page.Reader.TabSession), with the dialog and file
// chooser watch (page/watch.go): while the Page shows a JavaScript dialog,
// every Action but dialog fails at once and names `winuse dialog`.
//
// Downloads (launched and cloned browsers only) are tracked from the first
// page Action on a connection: Browser.setDownloadBehavior into
// pageDownloadDir, and `winuse wait <window> download` reports the file.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// The page-only commands' usage lines (also their argument hints).
const (
	navigateUsageHint = "usage: winuse navigate <window> <url> [--wait load|domcontentloaded|none]"
	uploadUsageHint   = "usage: winuse upload <window> [<element index | name: | id:>] <file>..."
	dialogUsageHint   = "usage: winuse dialog <window> accept|dismiss [text]"
	waitUsageHint     = "usage: winuse wait <window> selector <css> | text <text> | url <url or pattern with *> | load [domcontentloaded] | download [--timeout <seconds>]"
	evalUsageHint     = "usage: winuse eval <window> <expression>"
)

// historyUsageHint is back's, forward's or reload's usage line.
func historyUsageHint(cmd string) string {
	return "usage: winuse " + cmd + " <window> [--wait load|domcontentloaded|none]"
}

// pageWaitDefault is wait's default timeout; pageWaitMargin is kept
// between any page wait and the request's deadline, to answer in time.
const (
	pageWaitDefault = 10 * time.Second
	pageWaitMargin  = time.Second
	// dialogClosedWait bounds how long dialog waits for the browser to say
	// the dialog closed, so the next Action does not see it still open.
	dialogClosedWait = 500 * time.Millisecond
)

// pageCommands are the page-only commands' handlers, by wire name.
func pageCommands(s Services) map[string]Command {
	return map[string]Command{
		"navigate": pageNav(s, "navigate"),
		"back":     pageNav(s, "back"),
		"forward":  pageNav(s, "forward"),
		"reload":   pageNav(s, "reload"),
		"upload":   pageUpload(s),
		"dialog":   pageDialog(s),
		"wait":     pageWait(s),
		"eval":     pageEval(s),
	}
}

// pageOnlyStep is the Batch's scope question for the page-only commands:
// they have no Headed route at all, and a Window that is no browser Page
// fails them before anything is sent. hover and drag are Headless in their
// Page Element forms only.
func pageOnlyStep(kind string, args json.RawMessage) bool {
	switch kind {
	case "navigate", "back", "forward", "reload", "upload", "dialog", "wait", "eval":
		return true
	case "hover":
		var a protocol.HoverArgs
		return json.Unmarshal(args, &a) == nil && (a.ElementIndex != nil || a.Ref != "")
	case "drag":
		var a protocol.DragArgs
		return json.Unmarshal(args, &a) == nil && (a.FromIndex != nil || a.FromRef != "")
	}
	return false
}

// pageWin is a browser Window's active Tab, ready for a page Action.
type pageWin struct {
	w       win.WindowInfo
	name    string // the browser's short name
	conn    *browser.Connection
	reader  *page.Reader
	tab     page.Tab
	session string // "" while the Tab shows a browser page (tab.Internal)
	watch   *page.TabWatch
}

func (pw pageWin) id() string { return strconv.FormatUint(pw.w.ID, 10) }

// pageWindow resolves the Window, its Browser Connection and the Tab it
// shows, and attaches the Tab's session (with its watch). A Tab that shows
// a browser page (chrome://newtab/) is not attached: runPageWindow refuses
// it, but for navigate. Nothing reaches the Page's input.
func (s Services) pageWindow(ctx context.Context, ref string) (pageWin, *Error) {
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return pageWin{}, rerr
	}
	lp, ok := s.Pages.(*livePages)
	if !ok {
		return pageWin{}, Fail("this winuse helper has no Browser Connections", "run winuse browser status")
	}
	name := lp.Browser(w)
	if name == "" {
		return pageWin{}, Fail(fmt.Sprintf("window %d is not a browser Window, so it has no Page", w.ID),
			"navigate, back, forward, reload, upload, dialog, wait and eval act on Chrome, Edge, Brave, Chromium, Vivaldi and Opera Windows; run winuse list-windows")
	}
	conn, err := lp.connection(ctx, w, name)
	if err != nil {
		return pageWin{}, windowConnError(err, w, name)
	}
	tab, err := page.FindTab(ctx, conn, lp.geometry(w))
	if err != nil {
		return pageWin{}, tabListError(err, w, name)
	}
	pw := pageWin{w: w, name: name, conn: conn, reader: lp.reader, tab: tab}
	if !tab.Internal {
		if rerr := pw.attach(ctx); rerr != nil {
			return pageWin{}, rerr
		}
	}
	s.pageDownloads(ctx, conn)
	return pw, nil
}

// attach attaches the Tab's session, with its watch.
func (pw *pageWin) attach(ctx context.Context) *Error {
	session, err := pw.reader.TabSession(ctx, pw.conn, pw.tab.TargetID)
	if err != nil {
		if p, ok := browser.AsProblem(err, pw.name); ok {
			return Fail(p.Sentence(), p.Next())
		}
		return Fail(fmt.Sprintf("attaching to the Tab of window %d failed: %v", pw.w.ID, err), "retry; run winuse browser status "+pw.name+" if it keeps failing")
	}
	pw.session, pw.watch = session, page.WatchOf(pw.conn.Client, session)
	return nil
}

// browserPageError refuses a page Action on a Tab that shows a browser
// page (chrome://newtab/), which winuse never reads or acts in. Callers
// add "nothing was sent".
func browserPageError(w win.WindowInfo, tab page.Tab) *Error {
	id := strconv.FormatUint(w.ID, 10)
	return Fail(fmt.Sprintf("the active Tab of window %d shows a browser page (%s), which winuse does not read or act in", w.ID, tab.URL),
		"run winuse navigate "+id+" <url> to open a site")
}

// dialogHint names the dialog command for a Window.
func dialogHint(id string) string {
	return "run winuse dialog " + id + " accept or winuse dialog " + id + " dismiss (a prompt takes its text: winuse dialog " + id + " accept <text>)"
}

// dialogOpenError is the fast failure of an Action on a Page that shows a
// JavaScript dialog. Callers add "nothing was sent".
func dialogOpenError(w win.WindowInfo, d page.Dialog) *Error {
	return Fail(fmt.Sprintf("window %d shows %s, and its Page takes no input until it is answered", w.ID, d.Describe()),
		dialogHint(strconv.FormatUint(w.ID, 10)))
}

// pageNotReady maps page.Ready's refusal to the Action's failure, nil when
// the Page takes Actions. Callers add "nothing was sent".
func pageNotReady(w win.WindowInfo, err error) *Error {
	var open *page.DialogOpenError
	var quiet *page.NotAnsweringError
	switch {
	case errors.As(err, &open):
		return dialogOpenError(w, open.Dialog)
	case errors.As(err, &quiet):
		id := strconv.FormatUint(w.ID, 10)
		return Fail(fmt.Sprintf("the Page of window %d did not answer within %s: it most likely shows a JavaScript dialog (alert, confirm or prompt) that winuse did not see open, or a script keeps it busy", w.ID, quiet.After),
			dialogHint(id)+"; if it shows no dialog, retry in a moment")
	}
	return nil
}

// pagePrelude runs before any Page Element Action (pageFresh) and any
// input aimed at a Page (runPageInput): a Page that shows a dialog fails
// at once (page.Ready: within seconds when the watch missed the dialog),
// and a launched or cloned browser's downloads are tracked from here on.
// Callers add "nothing was sent".
func (s Services) pagePrelude(ctx context.Context, w win.WindowInfo, o *page.Origin) *Error {
	if rerr := pageNotReady(w, page.Ready(ctx, o.Client, o.TopSession())); rerr != nil {
		return rerr
	}
	if lp, ok := s.Pages.(*livePages); ok && o.Browser != "" {
		if conn, err := lp.connection(ctx, w, o.Browser); err == nil && conn.Client == o.Client {
			s.pageDownloads(ctx, conn)
		}
	}
	return nil
}

// pageDownloadDir is where launched and cloned browsers save downloads:
// a winuse folder in the user's own Downloads folder, so the user finds
// what the agent downloaded where they look for downloads, kept apart from
// their own files (and the renames never touch them). Tests replace it.
var pageDownloadDir = liveDownloadDir

// downloadDirLog logs a download folder that cannot be found once, not on
// every page Action.
var downloadDirLog sync.Once

func liveDownloadDir() (string, error) {
	if err := win.LiveDesktopGuard(); err != nil {
		return "", err
	}
	dir, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
	if err != nil || dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("finding the Downloads folder: %v", err)
		}
		dir = filepath.Join(home, "Downloads")
	}
	return filepath.Join(dir, "winuse"), nil
}

// pageDownloads turns download tracking on for a launched or cloned
// browser (once per connection) and returns its tracker; nil for any other
// connection, or when it could not be turned on (logged).
func (s Services) pageDownloads(ctx context.Context, conn *browser.Connection) *page.Downloads {
	if conn.Mode != browser.ModeLaunch && conn.Mode != browser.ModeClone {
		return nil
	}
	dir, err := pageDownloadDir()
	if err != nil {
		downloadDirLog.Do(func() { log.Printf("downloads are not tracked: %v", err) })
		return nil
	}
	if d := page.DownloadsOf(conn.Client); d != nil && d.Dir == dir {
		return d
	}
	d, err := page.EnableDownloads(ctx, conn.Client, dir)
	if err != nil {
		log.Printf("turning on download tracking in %s: %v", conn.Browser, err)
		return nil
	}
	return d
}

// pageWindowRun is one page-only Action's own part of runPageWindow.
type pageWindowRun struct {
	// acts says the command acts on the Page (all but wait): a locked
	// desktop refuses it, like every Action.
	acts bool
	// dialogOK lets the command run while a dialog is open (dialog itself).
	dialogOK bool
	// browserPage decides about a Tab that shows a browser page, which is
	// not attached: nil there lets act run (it sees pw.session "" and
	// handles it), an *Error refuses. Without it the command is refused
	// there with browserPageError. Either refusal comes before the Agent
	// Cursor moves and says nothing was sent.
	browserPage func(pw pageWin) *Error
	// target is where the Agent Cursor goes (none: no cursor), and label
	// what it says.
	target func(ctx context.Context, pw pageWin) (win.Rect, bool)
	label  string
	// act does the Action. delivered says something reached the Page (the
	// ripple, and which Esc stop applies).
	act func(ctx context.Context, pw pageWin, stopped func() bool) (out string, delivered bool, e *Error)
}

// runPageWindow is the pipeline of the page-only Actions: refuse a locked
// desktop, resolve the Window's Tab, fail at once on an open dialog, then
// in a passive Esc scope glide the Agent Cursor, act, and ripple once
// something was delivered. Every refusal before the act says "nothing was
// sent".
func (s Services) runPageWindow(ctx context.Context, ref string, run pageWindowRun) (string, *Error) {
	if run.acts {
		if locked, desktop := s.Locked(); locked {
			return "", nothingSent(lockedDesktopFailure(desktop))
		}
	}
	pw, rerr := s.pageWindow(ctx, ref)
	if rerr != nil {
		return "", nothingSent(rerr)
	}
	if pw.tab.Internal {
		rerr := browserPageError(pw.w, pw.tab)
		if run.browserPage != nil {
			rerr = run.browserPage(pw)
		}
		if rerr != nil {
			return "", nothingSent(rerr)
		}
	}
	if !run.dialogOK && !pw.tab.Internal {
		if rerr := pageNotReady(pw.w, page.Ready(ctx, pw.conn.Client, pw.session)); rerr != nil {
			return "", nothingSent(rerr)
		}
	}
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()
	var rect win.Rect
	shown := false
	if run.target != nil {
		target, _ := run.target(ctx, pw)
		rect, shown = s.cursorTravelRect(pw.w, target, run.label)
	}
	if esc.Stopped() {
		return "", escStopBeforeActError()
	}
	out, delivered, e := run.act(ctx, pw, esc.Stopped)
	if shown && delivered {
		s.cursorClick(rect)
	}
	if esc.Stopped() {
		if delivered {
			return "", escStopError()
		}
		return "", escStopBeforeActError()
	}
	if e != nil {
		return "", e
	}
	return out, nil
}

// frameElementRect is the live bounds of the first Element of the Window's
// latest Window State that is a UIA Element of the frame (not of the Page)
// and passes match.
func (s Services) frameElementRect(ctx context.Context, w win.WindowInfo, match func(role, name string) bool) (win.Rect, bool) {
	st, ok := s.Actions.(storedElements)
	if !ok {
		return win.Rect{}, false
	}
	els, ok := st.StoredElements(w.ID)
	if !ok {
		return win.Rect{}, false
	}
	splicer, _ := s.Actions.(PageSplicer)
	b, ok := s.Actions.(elementBounder)
	if !ok {
		return win.Rect{}, false
	}
	for i, el := range els {
		if !match(el.Role, el.Name) {
			continue
		}
		if splicer != nil {
			if o, err := splicer.ElementOrigin(w.ID, i); err != nil || o != nil {
				continue
			}
		}
		l, t, r, bt, _, err := b.ElementBounds(ctx, w.ID, i)
		if err != nil {
			continue
		}
		return win.Rect{Left: l, Top: t, Right: r, Bottom: bt}, true
	}
	return win.Rect{}, false
}

// pageDocRect is the Page's UIA document Element on the screen.
func (s Services) pageDocRect(ctx context.Context, pw pageWin) (win.Rect, bool) {
	return s.frameElementRect(ctx, pw.w, func(role, name string) bool { return role == "document" })
}

// addressBarRect is the browser's address bar, else the Page's document.
func (s Services) addressBarRect(ctx context.Context, pw pageWin) (win.Rect, bool) {
	if r, ok := s.frameElementRect(ctx, pw.w, func(role, name string) bool {
		return role == "edit" && strings.Contains(strings.ToLower(name), "address")
	}); ok {
		return r, true
	}
	return s.pageDocRect(ctx, pw)
}

// pageNav runs navigate, back, forward and reload.
func pageNav(s Services, cmd string) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		hint := historyUsageHint(cmd)
		if cmd == "navigate" {
			hint = navigateUsageHint
		}
		var a protocol.NavigateArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail(cmd+": "+err.Error(), hint)
		}
		if strings.TrimSpace(a.Window) == "" {
			return "", Fail(cmd+": a window reference is required", hint)
		}
		switch {
		case cmd == "navigate" && strings.TrimSpace(a.URL) == "":
			return "", Fail("navigate: a URL is required", hint)
		case cmd != "navigate" && a.URL != "":
			return "", Fail(cmd+" takes no URL; to open one, run winuse navigate "+a.Window+" <url>", hint)
		}
		state, err := page.ParseLoadState(a.Wait)
		if err != nil {
			return "", Fail(cmd+": "+err.Error(), hint)
		}
		target := page.NavURL(a.URL)
		label := cmd
		if cmd == "navigate" {
			label = cursorLabel("go to", hostOf(target))
		}
		return s.runPageWindow(ctx, a.Window, pageWindowRun{
			acts: true, target: s.addressBarRect, label: label,
			browserPage: func(pw pageWin) *Error {
				// The extension cannot attach to a browser page: only the
				// bridge's navigateTab leaves it (navigateFromBrowserPage).
				if pw.conn.Mode == browser.ModeExtension && cmd != "navigate" {
					return Fail(fmt.Sprintf("the active Tab of window %d shows a browser page (%s), and through the winuse extension only navigate can leave it", pw.w.ID, pw.tab.URL),
						"run winuse navigate "+pw.id()+" <url> to open a site")
				}
				return nil
			},
			act: func(ctx context.Context, pw pageWin, stopped func() bool) (string, bool, *Error) {
				return s.navigate(ctx, pw, cmd, target, state, stopped)
			},
		})
	}
}

// hostOf is a URL's host for the Agent Cursor's label.
func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host
	}
	return u
}

// navigate sends the navigation, then waits for the new document to commit
// and load (state) within the request's budget, and prints the Tab's URL.
//
// A Tab that shows a browser page (chrome://newtab/) arrives unattached.
// A launched or cloned browser attaches to it, and Page.navigate leaves it
// as it leaves any page. The extension cannot attach there, so navigate
// goes through the bridge instead (navigateFromBrowserPage); back, forward
// and reload were refused before the cursor moved.
func (s Services) navigate(ctx context.Context, pw pageWin, cmd, target, state string, stopped func() bool) (string, bool, *Error) {
	if pw.tab.Internal {
		if pw.conn.Mode == browser.ModeExtension {
			return s.navigateFromBrowserPage(ctx, pw, target, state, stopped)
		}
		if rerr := pw.attach(ctx); rerr != nil {
			return "", false, nothingSent(rerr)
		}
	}
	c, sess, id := pw.conn.Client, pw.session, pw.id()
	var committed func(ctx context.Context) bool
	switch cmd {
	case "navigate":
		loader, err := page.Navigate(ctx, c, sess, target)
		if err != nil {
			return s.navFailure(err, pw, "navigation")
		}
		if loader != "" {
			committed = func(ctx context.Context) bool {
				l, _, err := page.MainFrame(ctx, c, sess)
				return err == nil && l == loader
			}
		} else {
			state = page.LoadNone // a #fragment: the document stays
		}
	case "back", "forward":
		cur, entries, err := page.History(ctx, c, sess)
		if err != nil {
			return "", false, nothingSent(Fail(fmt.Sprintf("reading the history of window %d's Tab failed: %v", pw.w.ID, err), "retry; run winuse browser status "+pw.name+" if it keeps failing"))
		}
		to := cur - 1
		if cmd == "forward" {
			to = cur + 1
		}
		if to < 0 || to >= len(entries) {
			which := "earlier"
			if cmd == "forward" {
				which = "later"
			}
			return "", false, nothingSent(Fail(fmt.Sprintf("the Tab of window %d has no %s page in its history", pw.w.ID, which),
				"open a page with winuse navigate "+id+" <url>"))
		}
		if err := page.GoToEntry(ctx, c, sess, entries[to].ID); err != nil {
			return s.navFailure(err, pw, "move "+cmd)
		}
		committed = func(ctx context.Context) bool {
			now, _, err := page.History(ctx, c, sess)
			return err == nil && now == to
		}
	case "reload":
		before, _, _ := page.MainFrame(ctx, c, sess)
		if err := page.Reload(ctx, c, sess); err != nil {
			return s.navFailure(err, pw, "reload")
		}
		committed = func(ctx context.Context) bool {
			l, _, err := page.MainFrame(ctx, c, sess)
			return err == nil && l != before
		}
	}
	return s.navWait(ctx, pw, cmd, state, committed, stopped)
}

// navWait waits for a delivered navigation's document to commit (committed:
// nil to skip) and load (state) within the request's budget, and prints the
// Tab's URL.
func (s Services) navWait(ctx context.Context, pw pageWin, cmd, state string, committed func(ctx context.Context) bool, stopped func() bool) (string, bool, *Error) {
	c, sess, id := pw.conn.Client, pw.session, pw.id()
	lctx, cancel := pageWaitContext(ctx, 0)
	defer cancel()
	err := page.WaitLoad(lctx, c, sess, state, committed, stopped)
	var dialog *page.DialogError
	var slow *page.LoadTimeoutError
	switch {
	case errors.Is(err, page.ErrStopped):
		return "", true, escStopError()
	case errors.As(err, &dialog):
		return "", true, Fail("the "+cmd+" was delivered, and "+dialog.Error(), dialogHint(id)+"; do not repeat the "+cmd)
	case errors.As(err, &slow):
		u := page.CurrentURL(ctx, c, sess)
		return fmt.Sprintf("ok %s (%s; winuse wait %s load waits longer)\n", u, slow.Error(), id), true, nil
	case err != nil:
		return "", true, Fail(fmt.Sprintf("the %s was delivered, and waiting for the page failed: %v", cmd, err),
			"read winuse get-window-state "+id+" before repeating it")
	}
	return "ok " + page.CurrentURL(ctx, c, sess) + "\n", true, nil
}

// browserPagePoll is how often navigateFromBrowserPage reads the Tab while
// it leaves a browser page.
const browserPagePoll = 100 * time.Millisecond

// navigateFromBrowserPage is navigate through the extension on a Tab that
// shows a browser page, where chrome.debugger cannot attach and so
// Page.navigate cannot reach. The bridge's Winuse.navigateTab
// (chrome.tabs.update url, which never focuses the window or changes the
// selected Tab) sends it. The Tab's URL in the target list says it was
// taken. Once the Tab shows a site, its session is attached (again until
// the browser lets it, as the browser page may still be the document for
// a moment) and its load waited for as navigate does. A destination that
// is a browser page too is printed as soon as the Tab shows it, unread;
// so is any destination with --wait none.
func (s Services) navigateFromBrowserPage(ctx context.Context, pw pageWin, target, state string, stopped func() bool) (string, bool, *Error) {
	c, id := pw.conn.Client, pw.id()
	if err := page.NavigateTabThroughExtension(ctx, c, pw.tab.TargetID, target); err != nil {
		return s.navFailure(err, pw, "navigation")
	}
	lctx, cancel := pageWaitContext(ctx, 0)
	defer cancel()
	var now page.Tab
	for now.TargetID == "" {
		if stopped() {
			return "", true, escStopError()
		}
		t, err := page.TabNow(lctx, c, pw.tab.TargetID)
		switch {
		case errors.Is(err, page.ErrTabGone):
			return "", true, Fail(fmt.Sprintf("the navigation was delivered, and the Tab of window %d closed", pw.w.ID), "read winuse tab list "+id)
		case err == nil && t.URL != pw.tab.URL:
			now = t
			continue
		}
		select {
		case <-lctx.Done():
			return "", true, Fail(fmt.Sprintf("the navigation was delivered, and the Tab of window %d still showed %s", pw.w.ID, pw.tab.URL),
				"read winuse get-window-state "+id+" before repeating it")
		case <-time.After(browserPagePoll):
		}
	}
	if now.Internal || state == page.LoadNone {
		return "ok " + now.URL + "\n", true, nil
	}
	for {
		rerr := pw.attach(lctx)
		if rerr == nil {
			break
		}
		if stopped() {
			return "", true, escStopError()
		}
		select {
		case <-lctx.Done():
			return "", true, Fail("the navigation was delivered, and reading the new page failed: "+rerr.Msg, "read winuse get-window-state "+id+" before repeating it")
		case <-time.After(browserPagePoll):
		}
	}
	return s.navWait(ctx, pw, "navigate", state, nil, stopped)
}

// navFailure maps a navigation the browser did not start or finish.
func (s Services) navFailure(err error, pw pageWin, what string) (string, bool, *Error) {
	id := pw.id()
	var dialog *page.DialogError
	var nav *page.NavigateError
	var ce *cdp.Error
	switch {
	case errors.As(err, &dialog):
		return "", true, Fail("the "+what+" was delivered, and "+dialog.Error(),
			"the Page asks before it leaves: run winuse dialog "+id+" accept to leave it, or winuse dialog "+id+" dismiss to stay")
	case errors.Is(err, page.ErrDownloadStarted):
		if page.DownloadsOf(pw.conn.Client) == nil {
			return fmt.Sprintf("ok (%s; it goes to the browser's own download folder, which winuse does not track for this browser)\n", err.Error()), true, nil
		}
		return fmt.Sprintf("ok (%s; winuse wait %s download prints its path when it is done)\n", err.Error(), id), true, nil
	case errors.As(err, &nav):
		return "", true, Fail(nav.Error(), "check the address; the Tab shows the browser's error page now (winuse get-window-state "+id+")")
	case errors.As(err, &ce):
		if p, ok := browser.AsProblem(err, pw.name); ok {
			return "", false, nothingSent(Fail(p.Sentence(), p.Next()))
		}
		return "", false, nothingSent(Fail("the browser refused the "+what+": "+ce.Message, "read winuse get-window-state "+id))
	}
	return "", true, Fail(fmt.Sprintf("the browser did not answer the %s: %v", what, err),
		"it may still load; read winuse get-window-state "+id+" before repeating it")
}

// pageWaitContext bounds a page wait: want (0: the request's budget) and
// never past the request's deadline less pageWaitMargin.
func pageWaitContext(ctx context.Context, want time.Duration) (context.Context, context.CancelFunc) {
	d := want
	if dl, ok := ctx.Deadline(); ok {
		left := time.Until(dl) - pageWaitMargin
		if left < 100*time.Millisecond {
			left = 100 * time.Millisecond
		}
		if d <= 0 || left < d {
			d = left
		}
	}
	if d <= 0 {
		d = protocol.BrowserTimeout - pageWaitMargin
	}
	return context.WithTimeout(ctx, d)
}

// pageDialog runs `winuse dialog <window> accept|dismiss [text]`.
func pageDialog(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.DialogArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("dialog: "+err.Error(), dialogUsageHint)
		}
		if strings.TrimSpace(a.Window) == "" {
			return "", Fail("dialog: a window reference is required", dialogUsageHint)
		}
		action := strings.ToLower(strings.TrimSpace(a.Action))
		if action != "accept" && action != "dismiss" {
			return "", Fail(fmt.Sprintf("dialog: the answer must be accept or dismiss, got %q", a.Action), dialogUsageHint)
		}
		if a.Text != nil && action != "accept" {
			return "", Fail("dialog: text answers a prompt, so it goes with accept", dialogUsageHint)
		}
		return s.runPageWindow(ctx, a.Window, pageWindowRun{
			acts: true, dialogOK: true, target: s.pageDocRect, label: action + " dialog",
			act: func(ctx context.Context, pw pageWin, stopped func() bool) (string, bool, *Error) {
				id := pw.id()
				d := pw.watch.Dialog()
				if d != nil && a.Text != nil && d.Type != "prompt" {
					return "", false, nothingSent(Fail(fmt.Sprintf("window %d shows %s, and only a prompt takes text", pw.w.ID, d.Describe()),
						"run winuse dialog "+id+" "+action+" without text"))
				}
				params := map[string]any{"accept": action == "accept"}
				if a.Text != nil {
					params["promptText"] = *a.Text
				}
				changed := pw.watch.Changed()
				if _, err := pw.conn.Client.CallSession(ctx, pw.session, "Page.handleJavaScriptDialog", params); err != nil {
					var ce *cdp.Error
					if errors.As(err, &ce) && strings.Contains(strings.ToLower(ce.Message), "no dialog") {
						return "", false, nothingSent(Fail(fmt.Sprintf("window %d shows no JavaScript dialog", pw.w.ID), "read winuse get-window-state "+id))
					}
					if errors.As(err, &ce) {
						return "", false, nothingSent(Fail("the browser refused the answer: "+ce.Message, "read winuse get-window-state "+id))
					}
					return "", true, Fail(fmt.Sprintf("answering the dialog of window %d failed: %v", pw.w.ID, err),
						"it may have been answered; read winuse get-window-state "+id+" before repeating it")
				}
				// The browser reports the dialog closed right after; wait for
				// it, so the next Action does not still see it open.
				if pw.watch.Dialog() != nil {
					select {
					case <-changed:
					case <-time.After(dialogClosedWait):
					case <-ctx.Done():
					}
				}
				return okOutput, true, nil
			},
		})
	}
}

// pageWait runs `winuse wait <window> <for> [value] [--timeout s]`.
func pageWait(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.WaitArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("wait: "+err.Error(), waitUsageHint)
		}
		if strings.TrimSpace(a.Window) == "" {
			return "", Fail("wait: a window reference is required", waitUsageHint)
		}
		if a.TimeoutMS < 0 {
			return "", Fail("wait: the timeout must be positive", waitUsageHint)
		}
		kind := strings.ToLower(strings.TrimSpace(a.For))
		var expr string
		if kind != page.WaitDownload {
			var err error
			if expr, err = page.WaitExpr(kind, a.Value); err != nil {
				return "", Fail("wait: "+err.Error(), waitUsageHint)
			}
		}
		want := pageWaitDefault
		if a.TimeoutMS > 0 {
			want = time.Duration(a.TimeoutMS) * time.Millisecond
		}
		return s.runPageWindow(ctx, a.Window, pageWindowRun{
			// A download is the browser's, not the Page's: it is waited for
			// on a browser page too (a navigate to a file leaves the Tab
			// there).
			dialogOK: kind == page.WaitDownload, browserPage: waitBrowserPage(kind),
			act: func(ctx context.Context, pw pageWin, stopped func() bool) (string, bool, *Error) {
				wctx, cancel := pageWaitContext(ctx, want)
				defer cancel()
				if kind == page.WaitDownload {
					return s.waitDownload(wctx, pw, stopped)
				}
				id := pw.id()
				err := page.Poll(wctx, pw.conn.Client, pw.session, expr, stopped)
				var timeout *page.WaitTimeoutError
				var bad *page.EvalError
				var dialog *page.DialogOpenError
				switch {
				case err == nil:
					return okOutput, false, nil
				case errors.Is(err, page.ErrStopped):
					return "", false, escStopBeforeActError()
				case errors.As(err, &timeout):
					return "", false, Fail(fmt.Sprintf("waiting for %s in window %d: %s", waitWhat(kind, a.Value), pw.w.ID, timeout.Error()),
						"the page may need another Action first; read winuse get-window-state "+id+" (wait takes --timeout <seconds>, up to the request's budget)")
				case errors.As(err, &bad):
					return "", false, Fail(fmt.Sprintf("wait: the %s cannot be checked: %s", kind, bad.Msg), waitUsageHint)
				case errors.As(err, &dialog):
					return "", false, nothingSent(dialogOpenError(pw.w, dialog.Dialog))
				}
				return "", false, Fail(fmt.Sprintf("waiting in window %d failed: %v", pw.w.ID, err), "run winuse browser status "+pw.name)
			},
		})
	}
}

// waitBrowserPage lets wait download run on a browser page; every other
// wait is refused there (nil: browserPageError).
func waitBrowserPage(kind string) func(pageWin) *Error {
	if kind != page.WaitDownload {
		return nil
	}
	return func(pageWin) *Error { return nil }
}

// waitWhat names what a wait waited for.
func waitWhat(kind, value string) string {
	switch kind {
	case page.WaitLoadKind:
		if value == "" {
			return "the page to load"
		}
		return "the page to reach " + value
	case page.WaitURL:
		return fmt.Sprintf("a URL matching %q", value)
	}
	return fmt.Sprintf("%s %q", kind, value)
}

// waitDownload is `wait <window> download`: the oldest download winuse has
// not reported yet, followed to its end, printed as its file's path.
func (s Services) waitDownload(ctx context.Context, pw pageWin, stopped func() bool) (string, bool, *Error) {
	id := pw.id()
	switch pw.conn.Mode {
	case browser.ModeExtension:
		p := &browser.Problem{Kind: browser.ExtCannot, Browser: pw.name, Detail: "Browser.setDownloadBehavior"}
		return "", false, nothingSent(Fail("downloads through the winuse extension go to the browser's own download folder, and winuse cannot track them: "+p.Sentence(), p.Next()))
	case browser.ModeLaunch, browser.ModeClone:
	default:
		return "", false, nothingSent(Fail(fmt.Sprintf("winuse tracks downloads only in a browser it launched or cloned; this %s is your own, so its downloads go to its own download folder", browser.DisplayName(pw.name)),
			"run winuse browser launch "+pw.name+" or winuse browser clone "+pw.name+" to have winuse report downloaded files"))
	}
	d := page.DownloadsOf(pw.conn.Client)
	if d == nil {
		return "", false, nothingSent(Fail("winuse could not turn on download tracking in this browser (see the helper log)", "run winuse browser status "+pw.name))
	}
	got, err := d.Wait(ctx, stopped)
	var timeout *page.DownloadTimeoutError
	switch {
	case err == nil && got.Err != "":
		return fmt.Sprintf("ok %s (it keeps the browser's temporary name: %s)\n", got.Path, got.Err), false, nil
	case err == nil:
		return "ok " + got.Path + "\n", false, nil
	case errors.Is(err, page.ErrStopped):
		return "", false, escStopBeforeActError()
	case errors.Is(err, page.ErrDownloadCanceled):
		return "", false, Fail(fmt.Sprintf("the download of %q was canceled", got.Name), "start it again, then run winuse wait "+id+" download")
	case errors.As(err, &timeout) && !timeout.Started:
		return "", false, Fail(fmt.Sprintf("window %d: %s", pw.w.ID, timeout.Error()),
			"start the download first (click its link or button), then run winuse wait "+id+" download")
	case errors.As(err, &timeout):
		return "", false, Fail(fmt.Sprintf("window %d: %s", pw.w.ID, timeout.Error()), "run winuse wait "+id+" download again to keep waiting")
	}
	return "", false, Fail(err.Error(), "")
}

// pageEval runs `winuse eval <window> <expression>`. It is a page Action
// with no Agent Cursor (it has no target); winuse keeps no history of
// Window States to list it in.
func pageEval(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.EvalArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("eval: "+err.Error(), evalUsageHint)
		}
		if strings.TrimSpace(a.Window) == "" {
			return "", Fail("eval: a window reference is required", evalUsageHint)
		}
		if strings.TrimSpace(a.Expression) == "" {
			return "", Fail("eval: an expression is required", evalUsageHint)
		}
		return s.runPageWindow(ctx, a.Window, pageWindowRun{
			acts: true,
			act: func(ctx context.Context, pw pageWin, stopped func() bool) (string, bool, *Error) {
				id := pw.id()
				res, err := page.Eval(ctx, pw.conn.Client, pw.session, a.Expression, stopped)
				var thrown *page.EvalError
				var dialog *page.DialogError
				var ce *cdp.Error
				switch {
				case err == nil:
					out := res.Text + "\n"
					if res.Cut() {
						out += fmt.Sprintf("note: the result has %d characters, and winuse printed the first %d; return less, for example with .slice(0, 1000)\n", res.Total, page.EvalLimit)
					}
					return out, true, nil
				case errors.Is(err, page.ErrStopped):
					return "", true, escStopError()
				case errors.As(err, &thrown):
					return "", true, Fail("the expression threw: "+thrown.Msg,
						"fix the expression; it ran in the Page of window "+id+", so anything it did before throwing took effect")
				case errors.As(err, &dialog):
					return "", true, Fail("the expression ran, and "+dialog.Error(), dialogHint(id))
				case errors.Is(err, page.ErrNotJSON):
					return "", true, Fail(err.Error(), "")
				case errors.As(err, &ce):
					return "", false, nothingSent(Fail("the browser refused the expression: "+ce.Message, ""))
				}
				return "", true, Fail(fmt.Sprintf("running the expression in window %d failed: %v", pw.w.ID, err),
					"it may have run; read winuse get-window-state "+id+" before repeating it")
			},
		})
	}
}
