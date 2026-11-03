package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// The browser Window of these tests, and its Page fixtures (shared with
// internal/page's golden tests).
const browserWindowID = 301

func browserWindow() win.WindowInfo {
	return win.WindowInfo{ID: browserWindowID, Class: chromiumWindowClass, PID: 4242, App: "Google Chrome",
		ExePath: `C:\Program Files\Google\Chrome\Application\chrome.exe`, Title: "Sign in - Google Chrome"}
}

func pageFixture(t *testing.T, name string) *page.Raw {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "page", "testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw page.Raw
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return &raw
}

// chromeFrame is the faked UI Automation frame of the browser Window.
func chromeFrame(t *testing.T, title string, focused int) *wstate.Tree {
	t.Helper()
	frame, err := wstate.LoadFixture(filepath.Join("..", "page", "testdata", "chrome-frame.json"))
	if err != nil {
		t.Fatal(err)
	}
	frame.Title = strings.ReplaceAll(frame.Title, "TITLE", title)
	for i := range frame.Elements {
		frame.Elements[i].Name = strings.ReplaceAll(frame.Elements[i].Name, "TITLE", title)
	}
	frame.FocusedIndex = focused
	return frame
}

// pageTrees is a TreeReader and a PageSplicer over canned frames: the
// live-Element store's metadata (Elements and origins) without COM, with
// uia's rules (a splice applies only to the latest read).
type pageTrees struct {
	t       *testing.T
	frames  map[uint64]func() *wstate.Tree
	mu      sync.Mutex
	state   map[uint64][]wstate.Element
	origins map[uint64]map[int]any
	splices int
}

func newPageTrees(t *testing.T) *pageTrees {
	return &pageTrees{t: t, frames: map[uint64]func() *wstate.Tree{}, state: map[uint64][]wstate.Element{}, origins: map[uint64]map[int]any{}}
}

func (p *pageTrees) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	mk, ok := p.frames[hwnd]
	if !ok {
		return &wstate.Tree{Title: title, App: app, FocusedIndex: -1}, nil
	}
	tree := mk()
	p.mu.Lock()
	p.state[hwnd] = tree.Elements
	delete(p.origins, hwnd)
	p.mu.Unlock()
	return tree, nil
}

func (p *pageTrees) SplicePage(ctx context.Context, hwnd uint64, base, merged []wstate.Element, from []int, origins map[int]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state[hwnd]
	if len(st) != len(base) || len(st) == 0 || &st[0] != &base[0] {
		return uia.ErrSuperseded
	}
	p.state[hwnd] = merged
	p.origins[hwnd] = origins
	p.splices++
	return nil
}

func (p *pageTrees) ElementOrigin(ctx context.Context, hwnd uint64, index int) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st, ok := p.state[hwnd]
	if !ok {
		return nil, fmt.Errorf("%w: window %d", uia.ErrNoState, hwnd)
	}
	if index < 0 || index >= len(st) {
		return nil, fmt.Errorf("%w: element %d", uia.ErrStale, index)
	}
	return p.origins[hwnd][index], nil
}

// ResolveElementRef makes pageTrees an ElementResolver, as *uia.Client is.
func (p *pageTrees) ResolveElementRef(ctx context.Context, hwnd uint64, ref string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st, ok := p.state[hwnd]
	if !ok {
		return 0, uia.ErrNoState
	}
	return wstate.ResolveElementRef(st, ref)
}

// ElementPoint makes pageTrees an ElementPointer (Services.Actions).
func (p *pageTrees) ElementPoint(ctx context.Context, hwnd uint64, index int) (uia.Point, error) {
	return uia.Point{X: 1, Y: 2}, nil
}

// fakePages is a PageReader over a recorded Page read.
type fakePages struct {
	mu    sync.Mutex
	raw   *page.Raw
	err   error
	block bool // wait for the sub-budget to run out
	calls int
}

func (f *fakePages) Browser(w win.WindowInfo) string {
	if w.Class == chromiumWindowClass {
		return "chrome"
	}
	return ""
}

func (f *fakePages) ReadPage(ctx context.Context, w win.WindowInfo, name string) (*page.Raw, error) {
	f.mu.Lock()
	f.calls++
	raw, err, block := f.raw, f.err, f.block
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return raw, err
}

// pageServices is testServices plus the browser Window, its frame and a
// Page reader.
func pageServices(t *testing.T, pages *fakePages, focused int) (Services, *pageTrees) {
	trees := newPageTrees(t)
	title := "Sign in"
	if pages.raw != nil {
		title = pages.raw.Title
	}
	trees.frames[browserWindowID] = func() *wstate.Tree { return chromeFrame(t, title, focused) }
	trees.frames[101] = cannedTree
	s := testServices(trees)
	s.Windows = func() ([]win.WindowInfo, error) { return append(fakeWindows(), browserWindow()), nil }
	s.Pages = pages
	return s, trees
}

func TestBrowserWindowStateMergesThePage(t *testing.T) {
	raw := pageFixture(t, "login-form")
	pages := &fakePages{raw: raw}
	s, trees := pageServices(t, pages, -1)
	out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
	if cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	m, err := page.Merge(chromeFrame(t, raw.Title, -1), raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := wstate.Render(m.Tree); out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
	if !strings.Contains(out, "\t\t\t\t10 edit (settable, string) Email Value: ada@example.com\n") || strings.Contains(out, "UI Automation button") {
		t.Errorf("the Page did not replace the document's UIA descendants:\n%s", out)
	}
	if strings.Contains(out, "Note:") {
		t.Errorf("a merged read carries no note:\n%s", out)
	}
	if trees.splices != 1 {
		t.Fatalf("%d splices", trees.splices)
	}
	// Element Indexes across both parts, each with its origin.
	w := browserWindow()
	for i, want := range map[int]int64{9: 3, 10: 113, 13: 121} {
		o, perr := pageOrigin(context.Background(), Services{Actions: trees}, w, i)
		if perr != nil || o == nil || o.BackendNodeID != want || o.SessionID != "S1" || o.LoaderID != "L1" || o.DocIndex != 8 {
			t.Errorf("index %d origin = %+v, %v", i, o, perr)
		}
	}
	for _, i := range []int{0, 7, 8, 16, 17} {
		if o, perr := pageOrigin(context.Background(), Services{Actions: trees}, w, i); perr != nil || o != nil {
			t.Errorf("frame index %d: origin %+v, %v", i, o, perr)
		}
	}
}

// TestBrowserWindowFallbackIsTodayPlusANote: without a Browser Connection
// (or a Tab match, or a failed read) the output is today's UI Automation
// read, byte for byte, plus exactly one note line.
func TestBrowserWindowFallbackIsTodayPlusANote(t *testing.T) {
	reason := "no Browser Connection is open to Chrome; run winuse browser status chrome"
	note := "Note: the Page is read through UI Automation because " + reason + ".\n"
	for _, focused := range []int{7, -1} {
		pages := &fakePages{err: &page.Unavailable{Reason: reason}}
		s, trees := pageServices(t, pages, focused)
		out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
		if cmdErr != nil {
			t.Fatal(cmdErr.Error())
		}
		today := wstate.Render(chromeFrame(t, "Sign in", focused))
		want := today + note
		if focused < 0 {
			want = today + "\n" + note // the tail's blank line, then the note
		}
		if out != want {
			t.Errorf("focus %d:\n got %q\nwant %q", focused, out, want)
		}
		if trees.splices != 0 {
			t.Error("a fallback must not splice")
		}
		if o, err := pageOrigin(context.Background(), Services{Actions: trees}, browserWindow(), 10); err != nil || o != nil {
			t.Errorf("a fallback read has UIA indexes only: %+v, %v", o, err)
		}
	}

	// The other reasons, one note each.
	for _, c := range []struct {
		err  error
		want string
	}{
		{&page.NoTabError{}, "Note: the Page is read through UI Automation because no Tab of the Browser Connection matches this Window's title and bounds.\n"},
		{&page.AmbiguousTabError{Candidates: []page.Tab{{TargetID: "AAAA1111", Title: "Sign in"}, {TargetID: "BBBB2222", Title: "Sign in"}}},
			`Note: the Page is read through UI Automation because several Tabs match this Window's title and bounds: "Sign in" (target AAAA1111), "Sign in" (target BBBB2222).` + "\n"},
		{errors.New("Accessibility.getFullAXTree: boom"), "Note: the Page is read through UI Automation because reading it through the Browser Connection failed: Accessibility.getFullAXTree: boom.\n"},
	} {
		s, _ := pageServices(t, &fakePages{err: c.err}, 7)
		out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
		if cmdErr != nil {
			t.Fatal(cmdErr.Error())
		}
		if want := wstate.Render(chromeFrame(t, "Sign in", 7)) + c.want; out != want {
			t.Errorf("%v:\n got %q\nwant %q", c.err, out, want)
		}
	}

	// No document in the frame: the read falls back too.
	raw := pageFixture(t, "login-form")
	s, trees := pageServices(t, &fakePages{raw: raw}, 7)
	trees.frames[browserWindowID] = func() *wstate.Tree {
		f := chromeFrame(t, raw.Title, 7)
		f.Elements = f.Elements[:8]
		return f
	}
	out, _ := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
	if !strings.HasSuffix(out, "Note: the Page is read through UI Automation because UI Automation shows no document for the active Tab.\n") {
		t.Errorf("no document:\n%s", out)
	}
}

func TestNonBrowserWindowsAreUntouched(t *testing.T) {
	pages := &fakePages{err: &page.Unavailable{Reason: "never"}}
	s, _ := pageServices(t, pages, -1)
	out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "101"})
	if cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	if out != wstate.Render(cannedTree()) {
		t.Errorf("output changed:\n%s", out)
	}
	if pages.calls != 0 {
		t.Error("a Page read ran for a non-browser Window")
	}
	// A Chrome_WidgetWin_1 Window of an app that is not a browser (an
	// Electron app) is not a browser Window either.
	lp := &livePages{browsers: pageConns{identify: map[string]string{`C:\Chrome\chrome.exe`: "chrome"}}}
	if got := lp.Browser(win.WindowInfo{Class: chromiumWindowClass, ExePath: `C:\Apps\Slack\slack.exe`}); got != "" {
		t.Errorf("Electron app named %q", got)
	}
	if got := lp.Browser(win.WindowInfo{Class: "Notepad", ExePath: `C:\Chrome\chrome.exe`}); got != "" {
		t.Errorf("a non-Chromium class named %q", got)
	}
	if got := lp.Browser(win.WindowInfo{Class: chromiumWindowClass, ExePath: `C:\Chrome\chrome.exe`}); got != "chrome" {
		t.Errorf("Chrome named %q", got)
	}
}

func TestPageReadOutOfTimeFallsBack(t *testing.T) {
	pages := &fakePages{block: true}
	s, _ := pageServices(t, pages, 7)
	ctx, cancel := context.WithTimeout(context.Background(), pageMargin+300*time.Millisecond)
	defer cancel()
	tree, terr := readWindowTree(s, ctx, browserWindow())
	if terr != nil {
		t.Fatal(terr.Error())
	}
	n := tree.Notes[len(tree.Notes)-1]
	if !strings.HasPrefix(n, "the Page is read through UI Automation because reading it through the Browser Connection took longer than ") {
		t.Errorf("note = %q", n)
	}
	if ctx.Err() != nil {
		t.Error("the Page sub-budget used the whole request budget")
	}
}

// TestActionsRefusePageElements: the UIA side refuses a Page Element (by
// index or by name), and frame Elements resolve as before. The commands
// route Page Elements over the Browser Connection before that refusal
// (ticket 05, pageactions_test.go): here the Tab check fails, so the
// click sends nothing through the v1 path.
func TestActionsRefusePageElements(t *testing.T) {
	raw := pageFixture(t, "login-form")
	s, trees := pageServices(t, &fakePages{raw: raw}, -1)
	if _, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"}); cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	s.Actions = trees
	w := browserWindow()
	ten, seven := 10, 7
	_, err := elementTarget{index: &ten}.resolve(context.Background(), s, w)
	if err == nil || err.Msg != "element 10 is in the Page of window 301, and winuse could not route this Action to the Page; nothing was sent" {
		t.Fatalf("page index: %+v", err)
	}
	if err.Hint != "every Element Action but perform-secondary-action takes Page Elements; run winuse get-window-state 301 again and retry with a fresh Element Index" || strings.ContainsAny(err.Msg+err.Hint, "\u2013\u2014") {
		t.Errorf("hint = %q", err.Hint)
	}
	if _, err := (elementTarget{ref: "name:Email"}).resolve(context.Background(), s, w); err == nil || !strings.Contains(err.Msg, "element 10 is in the Page") {
		t.Errorf("page name: %+v", err)
	}
	if i, err := (elementTarget{index: &seven}).resolve(context.Background(), s, w); err != nil || i != 7 {
		t.Errorf("frame index: %d, %+v", i, err)
	}

	// Through the click command: nothing is clicked.
	spy := &actionSpy{point: uia.Point{X: 1, Y: 2}}
	cs := spy.services()
	cs.Windows = s.Windows
	cs.Actions = struct {
		*actionSpy
		PageSplicer
	}{spy, trees}
	_, cmdErr := runCommand(t, CommandsWith(cs), "click", clickArgs("301", 10, 1, "left"))
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, "nothing was sent") {
		t.Fatalf("click on a Page Element: %+v", cmdErr)
	}
	for _, step := range spy.recorded() {
		if step == "click" || step == "point" {
			t.Errorf("the refused click ran %q", step)
		}
	}
}

// lostOrigins is a live-Element store whose Page Elements lost their
// origins: ElementOrigin answers uia.ErrPageElement for them, as
// *uia.Client does for a nil handle with no origin stored.
type lostOrigins struct{ *pageTrees }

func (l lostOrigins) ElementOrigin(ctx context.Context, hwnd uint64, index int) (any, error) {
	o, err := l.pageTrees.ElementOrigin(ctx, hwnd, index)
	if err == nil && o != nil {
		return nil, fmt.Errorf("%w: element %d of window %d", uia.ErrPageElement, index, hwnd)
	}
	return o, err
}

// rereadOrigins is a live-Element store read again between a command's
// route and its act: index pageAfter is a UIA Element for the first n
// lookups and a Page Element afterwards.
type rereadOrigins struct {
	*pageTrees
	mu        sync.Mutex
	n         int
	pageAfter int
}

func (r *rereadOrigins) ElementOrigin(ctx context.Context, hwnd uint64, index int) (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index == r.pageAfter && r.n > 0 {
		r.n--
		return nil, nil
	}
	return r.pageTrees.ElementOrigin(ctx, hwnd, index)
}

// TestUnroutedPageElementsAreRefusedBeforeActivating: a Page Element the
// commands cannot route (its origin is gone) is refused before a Headed
// route activates the Window, with nothing sent; one the Window State
// turned into a Page Element after the route was picked is refused after
// the activation, also with nothing sent, never clicked.
func TestUnroutedPageElementsAreRefusedBeforeActivating(t *testing.T) {
	raw := pageFixture(t, "login-form")
	s, trees := pageServices(t, &fakePages{raw: raw}, -1)
	if _, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"}); cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	want := "element 10 is in the Page of window 301, and winuse could not route this Action to the Page; nothing was sent"
	scrollArgs, _ := json.Marshal(protocol.ScrollArgs{Window: "301", ElementIndex: idx(10), Direction: "down", Headed: true})
	setArgs, _ := json.Marshal(protocol.SetValueArgs{Window: "301", ElementIndex: idx(10), Value: "x", Headed: true})
	for _, tc := range []struct {
		cmd  string
		args json.RawMessage
	}{
		{"click", clickArgs("301", 10, 2, "left")},
		{"scroll", scrollArgs},
		{"set-value", setArgs},
	} {
		spy := &actionSpy{point: uia.Point{X: 1, Y: 2}}
		cs := spy.services()
		cs.Windows = s.Windows
		cs.Actions = struct {
			*actionSpy
			PageSplicer
		}{spy, lostOrigins{trees}}
		_, cmdErr := runCommand(t, CommandsWith(cs), tc.cmd, tc.args)
		if cmdErr == nil || cmdErr.Msg != want || !strings.Contains(cmdErr.Hint, "winuse get-window-state 301 again") {
			t.Errorf("%s on a Page Element with no origin: %+v", tc.cmd, cmdErr)
		}
		for _, step := range spy.recorded() {
			if step == "activate" || step == "click" || step == "point" {
				t.Errorf("%s: the refused Action ran %q", tc.cmd, step)
			}
		}
	}

	// Read again meanwhile: the route saw a UIA Element, the act a Page
	// Element. The double click is sent with `headed`: off a Page it has
	// only the Headed route (refused without it, headless-max ticket 21).
	spy := &actionSpy{point: uia.Point{X: 1, Y: 2}}
	cs := spy.services()
	cs.Windows = s.Windows
	cs.Actions = struct {
		*actionSpy
		PageSplicer
	}{spy, &rereadOrigins{pageTrees: trees, n: 1, pageAfter: 10}}
	_, cmdErr := runCommand(t, CommandsWith(cs), "click", headedArgs(clickArgs("301", 10, 2, "left")))
	if cmdErr == nil || cmdErr.Msg != want {
		t.Fatalf("click on an Element that became a Page Element: %+v", cmdErr)
	}
	for _, step := range spy.recorded() {
		if step == "click" || step == "point" {
			t.Errorf("the refused click ran %q", step)
		}
	}
}

// TestUIAPageElementErrorIsTheRefusal: a UIA call given a Page Element
// (uia.ErrPageElement) fails with the same refusal, never a bare error.
func TestUIAPageElementErrorIsTheRefusal(t *testing.T) {
	w := browserWindow()
	got := elementError(fmt.Errorf("%w: element 4 of window 301", uia.ErrPageElement), w, 4)
	if want := pageElementError(w, 4); got.Msg != want.Msg || got.Hint != want.Hint || got.Hint == "" {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if again := nothingSent(got); again.Msg != got.Msg {
		t.Errorf("nothing was sent twice: %q", again.Msg)
	}
}

// TestPageStalenessUsesTheUIAMessage: a stale Page Element fails with
// exactly the message a stale UIA Element gets.
func TestPageStalenessUsesTheUIAMessage(t *testing.T) {
	w := browserWindow()
	got := checkPageOrigin(context.Background(), w, 12, &page.Origin{SessionID: "S1", BackendNodeID: 117})
	want := elementError(uia.ErrStale, w, 12)
	if got == nil || got.Msg != want.Msg || got.Hint != want.Hint {
		t.Errorf("stale Page Element:\n got %+v\nwant %+v", got, want)
	}
	// No state and out of range: the usual errors.
	trees := newPageTrees(t)
	if _, perr := pageOrigin(context.Background(), Services{Actions: trees}, w, 3); perr == nil || !strings.Contains(perr.Msg, "no cached Window State") {
		t.Errorf("no state: %+v", perr)
	}
}

func TestLivePagesReasons(t *testing.T) {
	w := browserWindow()
	lp := &livePages{browsers: pageConns{err: errors.New("nothing debuggable")}, reader: page.NewReader(),
		geometry: func(win.WindowInfo) page.Geometry { return page.Geometry{} }}
	_, err := lp.ReadPage(context.Background(), w, "chrome")
	if r := page.Reason(err); r != "no Browser Connection is open to Chrome; run winuse browser status chrome" {
		t.Errorf("no connection: %q", r)
	}
	lp.browsers = pageConns{conn: &browser.Connection{Browser: "chrome", PID: 999}}
	_, err = lp.ReadPage(context.Background(), w, "chrome")
	if r := page.Reason(err); r != "this Chrome window belongs to Chrome process 4242, which has no debugging connection winuse can use (winuse can reach Chrome process 999); if it is the user's own Chrome, run winuse browser ext install chrome to reach it through the winuse extension; otherwise work in a window of Chrome process 999" {
		t.Errorf("another process: %q", r)
	}
}

type pageConns struct {
	identify map[string]string
	conn     *browser.Connection
	err      error
}

func (f pageConns) WindowConnection(ctx context.Context, name string, pid uint32) (*browser.Connection, error) {
	return f.conn, f.err
}

func (f pageConns) Identify(exePath string) string { return f.identify[exePath] }

func TestNewLivePagesNeedsTheManager(t *testing.T) {
	if p := newLivePages(nil, nil); p != nil {
		t.Error("no browser service, no Page reader")
	}
	if p := newLivePages(browser.NewManager(&browser.System{}), nil); p == nil {
		t.Error("the Manager gives a Page reader")
	}
}
