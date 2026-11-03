package helper

// One Window State for a browser Window (browser merge ticket 04, ADR
// 0012). A Chromium browser Window (class Chrome_WidgetWin_1, owned by a
// browser winuse knows) is read twice at once: the frame through UI
// Automation as before, and the active Tab's Page through its Browser
// Connection (internal/page). The Page replaces the UIA descendants of
// the frame's document Element, and the live-Element store follows, so
// Element Indexes are numbered across both parts and each records its
// origin: a UIA Element, or a page.Origin.
//
// Without a Browser Connection, with no Tab matching, or when the Page
// read fails or runs out of its sub-budget, the Window State is today's
// UI Automation read plus one note saying why. Every other Window is read
// exactly as before, with no note.
//
// The UI Automation walk does not read what the Page replaces: once the
// Page read ends well, the walk leaves out the descendants of the document
// Element named after the Tab (uia.DocumentCut), the one page.Merge picks.
// Until then it reads on, so a failed Page read finds that walk whole, and
// its output is today's. A stale Tab's document (page.DocRule) is read
// whole, and Merge leaves it out.
//
// For ticket 05, which routes Actions by origin:
//
//   - pageOrigin(ctx, s, w, index) resolves an Element Index of w's latest
//     Window State: nil for a UIA Element, the *page.Origin for a Page
//     Element, the usual no-state and stale failures otherwise;
//   - checkPageOrigin(ctx, w, index, o) is the Page staleness check (the
//     page navigated, the node is gone, the session or connection ended),
//     failing with exactly the UIA stale message;
//   - o.Rect(ctx) is the node's CSS box in the Tab's viewport plus the
//     devicePixelRatio, and o.DocIndex the UIA document Element the
//     cursor transform starts from.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// chromiumWindowClass is the Win32 class of every Chromium browser
// Window.
const chromiumWindowClass = "Chrome_WidgetWin_1"

// pageBudget is the Page read's own sub-budget: finding the Tab, the
// accessibility tree, the cursor-pointer pass and the iframes. It runs
// alongside the UI Automation frame read, inside the same request budget;
// running out falls back to the UI Automation read with a note.
const pageBudget = 4 * time.Second

// pageMargin is kept between the Page read and the request deadline, to
// merge and render in time.
const pageMargin = time.Second

// pageGrace is how long the Window State waits, once pageBudget is spent,
// for the Page read to return its own reason. It fits inside pageMargin.
const pageGrace = 200 * time.Millisecond

// PageReader reads the Page of a browser Window. The live one goes through
// the helper's Browser Connections (livePages); tests fake it with
// recorded reads.
type PageReader interface {
	// Browser names the Chromium browser a Window belongs to ("chrome",
	// "edge", ...), or "" for any other Window, which is read through UI
	// Automation alone and left untouched.
	Browser(w win.WindowInfo) string
	// ReadPage reads the active Tab of the browser Window w. A
	// *page.Unavailable (and the mapping's *page.NoTabError and
	// *page.AmbiguousTabError) says why it could not, for the note.
	ReadPage(ctx context.Context, w win.WindowInfo, browserName string) (*page.Raw, error)
}

// PageSplicer is the live-Element store's side of the merge: *uia.Client.
type PageSplicer interface {
	SplicePage(ctx context.Context, hwnd uint64, base, merged []wstate.Element, from []int, origins map[int]any) error
	ElementOrigin(ctx context.Context, hwnd uint64, index int) (any, error)
}

// readWindowTree reads a Window's State for get-window-state (with or
// without --screenshot): the UI Automation read, merged with the Page for
// a browser Window.
func readWindowTree(s Services, ctx context.Context, w win.WindowInfo) (*wstate.Tree, *Error) {
	name := ""
	if s.Pages != nil {
		name = s.Pages.Browser(w)
	}
	if name == "" {
		tree, err := s.Trees.Capture(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
		if err != nil {
			return nil, treeReadError(err)
		}
		return tree, nil
	}

	budget := pageBudgetFor(ctx)
	pctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	type result struct {
		raw *page.Raw
		err error
	}
	done := make(chan result, 1)
	cut := &pageCut{}
	go func() {
		raw, err := s.Pages.ReadPage(pctx, w, name)
		cut.settle(raw, err)
		done <- result{raw, err}
	}()

	tree, at, err := readFrame(s, ctx, w, cut.decide)
	if err != nil {
		return nil, treeReadError(err)
	}
	// fallback is the UI Automation read plus the note. A walk that left
	// the document out (only after a good Page read, so only when the merge
	// or the splice fails) reads the Window whole again first: the agent
	// gets today's read, never one missing the document's content.
	fallback := func(reason string) (*wstate.Tree, *Error) {
		if at >= 0 {
			full, err := s.Trees.Capture(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
			if err != nil {
				return nil, treeReadError(err)
			}
			tree, at = full, -1
		}
		return withPageNote(tree, reason), nil
	}
	var r result
	select {
	case r = <-done:
	case <-pctx.Done():
		// The Page read stops with pctx and may say why, such as an approval
		// prompt the browser still holds (the dial waits up to
		// browser.ApprovalWait, longer than pageBudget): give it a moment.
		select {
		case r = <-done:
		case <-time.After(pageGrace):
			r.err = pctx.Err()
			if at >= 0 {
				// The walk cut the document, so the Page read ended well and
				// its result is on its way.
				r = <-done
			}
		}
	}
	switch {
	case r.err != nil && errors.Is(r.err, context.DeadlineExceeded):
		return fallback(fmt.Sprintf("reading it through the Browser Connection took longer than %s", budget.Round(100*time.Millisecond)))
	case r.err != nil:
		return fallback(page.Reason(r.err))
	case r.raw == nil:
		return fallback("the Browser Connection returned no Page")
	}

	merged, err := page.Merge(tree, r.raw)
	if at >= 0 && (err != nil || merged.From[merged.Doc] != at) {
		// Not expected: the cut names the document Merge picks. Still, a
		// document left out must not be lost, so the Window is read whole
		// and merged as before.
		if tree, err = s.Trees.Capture(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget); err != nil {
			return nil, treeReadError(err)
		}
		at = -1
		merged, err = page.Merge(tree, r.raw)
	}
	if err != nil {
		return fallback(page.Reason(err))
	}
	splicer, ok := s.Trees.(PageSplicer)
	if !ok {
		return fallback("this helper cannot keep Page Elements")
	}
	origins := make(map[int]any, len(merged.Origins))
	for i := range merged.Origins {
		o := merged.Origins[i]
		origins[merged.Doc+1+i] = &o
	}
	if err := splicer.SplicePage(ctx, w.ID, tree.Elements, merged.Tree.Elements, merged.From, origins); err != nil {
		if errors.Is(err, uia.ErrSuperseded) {
			return fallback("the Window was read again while its Page was read")
		}
		return fallback(page.Reason(err))
	}
	return merged.Tree, nil
}

// FrameReader is the UI Automation read of a browser Window that leaves
// out the descendants of the document Element cut names: *uia.Client. at
// is that Element's index, or -1 when every document was read whole.
type FrameReader interface {
	CaptureFrame(ctx context.Context, hwnd uint64, title, app string, budget time.Duration, cut uia.DocumentCut) (tree *wstate.Tree, at int, err error)
}

// readFrame is the UI Automation read of a browser Window: with cut when
// the tree reader can leave the document out and also keep Page Elements,
// whole otherwise.
func readFrame(s Services, ctx context.Context, w win.WindowInfo, cut uia.DocumentCut) (*wstate.Tree, int, error) {
	fr, ok := s.Trees.(FrameReader)
	if _, splices := s.Trees.(PageSplicer); !ok || !splices {
		tree, err := s.Trees.Capture(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
		return tree, -1, err
	}
	return fr.CaptureFrame(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget, cut)
}

// pageCut is one read's uia.DocumentCut. It knows nothing until the Page
// read ends; then it names the document Element page.Merge replaces when
// Merge picks it by name (page.DocRule: named after the Tab, and no stale
// Tab's document), or none when the Page read failed. The walk asks it on
// the UIA worker thread, so it only loads what settle stored.
type pageCut struct {
	rule atomic.Pointer[page.DocRule]
}

// settle records the end of the Page read.
func (c *pageCut) settle(raw *page.Raw, err error) {
	var r page.DocRule
	if err == nil && raw != nil {
		r = page.RuleFor(raw)
	}
	c.rule.Store(&r)
}

func (c *pageCut) decide(name, value string) (cut, known bool) {
	r := c.rule.Load()
	if r == nil {
		return false, false
	}
	return r.Named(name, value), true
}

// pageBudgetFor is the Page read's sub-budget inside the request's.
func pageBudgetFor(ctx context.Context) time.Duration {
	d := pageBudget
	if dl, ok := ctx.Deadline(); ok {
		if left := time.Until(dl) - pageMargin; left < d {
			d = left
		}
	}
	if d < 100*time.Millisecond {
		d = 100 * time.Millisecond
	}
	return d
}

// withPageNote is the UI Automation read plus the one fallback note. The
// tree's Elements are shared, not changed.
func withPageNote(tree *wstate.Tree, reason string) *wstate.Tree {
	t := *tree
	t.Notes = append(append([]string(nil), tree.Notes...), page.FallbackNote(reason))
	return &t
}

// pageOrigin resolves an Element Index of w's latest Window State to its
// origin: nil for a UIA Element (or when the store keeps no origins), the
// Page node for a Page Element. A Window with no stored Window State and
// an index outside it fail with the usual read-the-State-again errors.
func pageOrigin(ctx context.Context, s Services, w win.WindowInfo, index int) (*page.Origin, *Error) {
	splicer, ok := s.Actions.(PageSplicer)
	if !ok {
		return nil, nil
	}
	o, err := splicer.ElementOrigin(ctx, w.ID, index)
	if err != nil {
		if errors.Is(err, uia.ErrPageElement) {
			return nil, pageElementError(w, index)
		}
		return nil, elementError(err, w, index)
	}
	po, _ := o.(*page.Origin)
	return po, nil
}

// checkPageOrigin fails a Page Element that went stale (the page
// navigated, the node is gone, the session or the Browser Connection
// ended) with exactly the message a stale UIA Element gets.
func checkPageOrigin(ctx context.Context, w win.WindowInfo, index int, o *page.Origin) *Error {
	err := o.Check(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(err, page.ErrStale) {
		return elementError(fmt.Errorf("%w: %v", uia.ErrStale, err), w, index)
	}
	return Fail(err.Error(), "run winuse get-window-state "+strconv.FormatUint(w.ID, 10)+" again; run winuse browser status if it keeps failing")
}

// refusePageElement fails a Page Element Index that reached a UI
// Automation route: one whose origin is missing (onPage refuses it before
// any activation), or one the Window State turned into a Page Element
// after the command picked its route (a get-window-state that ran
// meanwhile). A UIA Element, and an index that does not resolve (the
// Action's own step reports that), pass.
func refusePageElement(ctx context.Context, s Services, w win.WindowInfo, index int) *Error {
	splicer, ok := s.Actions.(PageSplicer)
	if !ok {
		return nil
	}
	o, err := splicer.ElementOrigin(ctx, w.ID, index)
	if errors.Is(err, uia.ErrPageElement) || (err == nil && o != nil) {
		return pageElementError(w, index)
	}
	return nil
}

// pageElementError refuses a Page Element on a route without Page
// Elements, before anything is sent.
func pageElementError(w win.WindowInfo, index int) *Error {
	id := strconv.FormatUint(w.ID, 10)
	return nothingSent(Fail(fmt.Sprintf("element %d is in the Page of window %d, and winuse could not route this Action to the Page", index, w.ID),
		"every Element Action but perform-secondary-action takes Page Elements; run winuse get-window-state "+id+" again and retry with a fresh Element Index"))
}

// livePages is the helper's PageReader over its Browser Connections.
type livePages struct {
	browsers pageBrowsers
	reader   *page.Reader
	geometry func(w win.WindowInfo) page.Geometry
}

// pageBrowsers is what the Page read needs of the Browser Connections:
// *browser.Manager. A Window is reached through the connection of the
// browser process that owns it, never through the pin.
type pageBrowsers interface {
	WindowConnection(ctx context.Context, name string, pid uint32) (*browser.Connection, error)
	Identify(exePath string) string
}

// connection is the Browser Connection of the browser process that owns w.
// A connection to another process (only a fake hands one out) counts as
// none for w: a *browser.Problem of kind NoWindowEndpoint.
func (p *livePages) connection(ctx context.Context, w win.WindowInfo, name string) (*browser.Connection, error) {
	conn, err := p.browsers.WindowConnection(ctx, name, w.PID)
	if err != nil {
		return nil, err
	}
	if conn.PID != 0 && w.PID != 0 && conn.PID != w.PID {
		return nil, &browser.Problem{Kind: browser.NoWindowEndpoint, Browser: name, PID: int(w.PID), Others: []string{strconv.FormatUint(uint64(conn.PID), 10)}}
	}
	return conn, nil
}

// open names the browser of Window w and returns its Browser Connection;
// name is "" (and nothing is looked up) for a Window that is no browser.
func (p *livePages) open(ctx context.Context, w win.WindowInfo) (name string, conn *browser.Connection, err error) {
	name = p.Browser(w)
	if name == "" {
		return "", nil, nil
	}
	conn, err = p.connection(ctx, w, name)
	return name, conn, err
}

// windowConnProblem is the Problem behind a failed connection lookup that
// is about the Window's own process: no endpoint belongs to it, or more
// than one does. Any other failure keeps the "no Browser Connection"
// wording.
func windowConnProblem(err error) (*browser.Problem, bool) {
	var p *browser.Problem
	if errors.As(err, &p) && (p.Kind == browser.NoWindowEndpoint || p.Kind == browser.SeveralEndpoints || p.Kind == browser.ExtSeveralProfiles) {
		return p, true
	}
	return nil, false
}

// windowConnError fails a command on Window w whose Browser Connection
// lookup failed. Callers may add "nothing was sent".
func windowConnError(err error, w win.WindowInfo, name string) *Error {
	display := browser.DisplayName(name)
	p, ok := windowConnProblem(err)
	switch {
	case !ok:
		return Fail(fmt.Sprintf("window %d is a %s Window with no Browser Connection", w.ID, display), "run winuse browser status "+name)
	case p.Kind == browser.NoWindowEndpoint:
		msg := fmt.Sprintf("window %d belongs to %s process %d, which has no Browser Connection", w.ID, display, p.PID)
		if r := p.Reachable(); r != "" {
			msg += " (winuse can reach " + r + ")"
		}
		return Fail(msg, p.Next())
	}
	return Fail(p.Sentence(), p.Next())
}

// newLivePages builds the live PageReader, or nil when the browser
// service holds no connections (a fake in tests).
func newLivePages(b BrowserService, maximized func(uint64) bool) PageReader {
	pb, ok := b.(pageBrowsers)
	if !ok {
		return nil
	}
	memory := page.NewWindowMemory()
	return &livePages{browsers: pb, reader: page.NewReader(), geometry: func(w win.WindowInfo) page.Geometry {
		r, _ := win.WindowRect(w.ID)
		return page.Geometry{
			Title:     w.Title,
			Rect:      page.Box{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: r.Bottom},
			DPI:       win.WindowDPI(w.ID),
			Minimized: w.Minimized,
			Maximized: maximized != nil && maximized(w.ID),
			HWND:      w.ID,
			PID:       w.PID,
			Memory:    memory,
		}
	}}
}

// appKinds is the optional side of pageBrowsers that knows the Apps
// launch-app --cdp started (headless-max 15): *browser.Manager.
type appKinds interface {
	AppKind(pid uint32) string
}

// Browser names a browser Window's browser, or the App kind
// (browser.AppElectron, browser.AppWebView2) of a Window whose process
// winuse launched with --cdp, whatever its window class.
func (p *livePages) Browser(w win.WindowInfo) string {
	if a, ok := p.browsers.(appKinds); ok && w.PID != 0 {
		if kind := a.AppKind(w.PID); kind != "" {
			return kind
		}
	}
	if w.Class != chromiumWindowClass {
		return ""
	}
	return p.browsers.Identify(w.ExePath)
}

func (p *livePages) ReadPage(ctx context.Context, w win.WindowInfo, name string) (*page.Raw, error) {
	conn, err := p.connection(ctx, w, name)
	if prob, ok := windowConnProblem(err); ok {
		return nil, &page.Unavailable{Reason: prob.Sentence() + "; " + prob.Next()}
	}
	var prob *browser.Problem
	if errors.As(err, &prob) && prob.Kind == browser.AwaitingApproval {
		// The browser holds the handshake for its approval prompt; the dial
		// goes on in the background, so a read after the user approves joins
		// it.
		return nil, &page.Unavailable{Reason: prob.Sentence() + "; " + prob.Next()}
	}
	if err != nil {
		return nil, &page.Unavailable{Reason: fmt.Sprintf("no Browser Connection is open to %s; run winuse browser status %s", browser.DisplayName(name), name)}
	}
	raw, err := p.reader.Read(ctx, conn, p.geometry(w))
	var open *page.DialogOpenError
	var quiet *page.NotAnsweringError
	var internal *page.InternalPageError
	switch { // ticket 06: the dialog shows as the Window State's note
	case errors.As(err, &internal):
		return nil, &page.Unavailable{Reason: fmt.Sprintf("the active Tab shows a browser page (%s); run winuse navigate %d <url> to open a site", internal.URL, w.ID)}
	case errors.As(err, &open):
		return nil, &page.Unavailable{Reason: fmt.Sprintf("it shows %s; answer it with winuse dialog %d accept or winuse dialog %d dismiss", open.Dialog.Describe(), w.ID, w.ID)}
	case errors.As(err, &quiet):
		return nil, &page.Unavailable{Reason: fmt.Sprintf("it did not answer within %s: it most likely shows a JavaScript dialog that winuse did not see open (answer it with winuse dialog %d accept or winuse dialog %d dismiss), or a script keeps it busy", quiet.After, w.ID, w.ID)}
	}
	return raw, err
}
