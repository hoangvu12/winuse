package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// The UI Automation walk of a browser Window leaves out the descendants of
// the document the Page replaces (uia.DocumentCut). These tests walk the
// faked Chrome frame through the real uia.Walk, over a source that serves
// chrome-frame.json and counts the chunks it reads.

// frameSource is a uia.TreeSource over a recorded tree's Elements (the
// handle is the Element's index). It reads the Elements back into the
// NodeData the walk renders them from.
type frameSource struct {
	t        *testing.T
	els      []wstate.Element
	focused  int
	before   func(i int) // runs at every Children call
	expanded []int
}

func (f *frameSource) Root() (int, uia.NodeData, error) { return 0, f.data(0), nil }

func (f *frameSource) Children(i int, remaining time.Duration) ([]int, []uia.NodeData, error) {
	f.expanded = append(f.expanded, i)
	if f.before != nil {
		f.before(i)
	}
	var kids []int
	var data []uia.NodeData
	for j := i + 1; j < len(f.els) && f.els[j].Depth > f.els[i].Depth; j++ {
		if f.els[j].Depth == f.els[i].Depth+1 {
			kids = append(kids, j)
			data = append(data, f.data(j))
		}
	}
	return kids, data, nil
}

func (f *frameSource) data(i int) uia.NodeData {
	e := f.els[i]
	return uia.NodeData{
		ControlType:      controlTypeOf(f.t, e.Role),
		Name:             e.Name,
		AutomationId:     e.AutomationId,
		HelpText:         e.Description,
		Enabled:          !e.Disabled,
		Focused:          i == f.focused,
		HasSelectionItem: e.Selectable,
		Selected:         e.Selected,
		HasValue:         e.Value != "" || e.Settable == "string",
		ValueReadOnly:    e.Settable != "string",
		ValueValue:       e.Value,
		HasRangeValue:    e.Settable == "float",
	}
}

func controlTypeOf(t *testing.T, role string) int32 {
	for ct := int32(wstate.ControlTypeButton); ct < 50050; ct++ {
		if wstate.RoleName(ct) == role {
			return ct
		}
	}
	t.Fatalf("no control type renders as %q", role)
	return 0
}

// walkTrees is pageTrees whose browser Windows are walked: Capture reads
// the whole frame, CaptureFrame leaves the cut document out, as
// *uia.Client does.
type walkTrees struct {
	*pageTrees
	walked map[uint64]func() *wstate.Tree
	// waitAt makes the chunk read of that Element Index wait for the cut's
	// answer, and opens gate first (the Page read waits on it), so the
	// answer comes exactly then; -1 waits nowhere.
	waitAt    int
	gate      chan struct{}
	supersede bool // the next splice finds a newer read
	reads     []walkRead
}

// walkRead is one walk: whether it was asked to cut, where it cut, and the
// Element Indexes whose children it read.
type walkRead struct {
	frame    bool
	cut      int
	expanded []int
}

func (w *walkTrees) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	if _, ok := w.walked[hwnd]; !ok {
		return w.pageTrees.Capture(ctx, hwnd, title, app, budget)
	}
	tree, _, err := w.walk(hwnd, nil)
	return tree, err
}

func (w *walkTrees) CaptureFrame(ctx context.Context, hwnd uint64, title, app string, budget time.Duration, cut uia.DocumentCut) (*wstate.Tree, int, error) {
	if _, ok := w.walked[hwnd]; !ok {
		w.t.Fatalf("CaptureFrame for window %d, which is no browser Window", hwnd)
	}
	return w.walk(hwnd, cut)
}

func (w *walkTrees) walk(hwnd uint64, cut uia.DocumentCut) (*wstate.Tree, int, error) {
	frame := w.walked[hwnd]()
	src := &frameSource{t: w.t, els: frame.Elements, focused: frame.FocusedIndex}
	if cut != nil && w.waitAt >= 0 {
		src.before = func(i int) {
			if i != w.waitAt {
				return
			}
			if w.gate != nil {
				close(w.gate)
				w.gate = nil
			}
			for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond) {
				if _, known := cut("", ""); known {
					return
				}
			}
			w.t.Error("the Page read never ended")
		}
	}
	res, err := uia.Walk[int](src, uia.WalkConfig{Cut: cut})
	if err != nil {
		return nil, -1, err
	}
	tree := &wstate.Tree{Title: frame.Title, App: frame.App, Elements: res.Elements, FocusedIndex: res.FocusedIndex, Notes: res.Notes}
	w.mu.Lock()
	w.state[hwnd] = tree.Elements
	delete(w.origins, hwnd)
	w.reads = append(w.reads, walkRead{frame: cut != nil, cut: res.Cut, expanded: src.expanded})
	w.mu.Unlock()
	return tree, res.Cut, nil
}

func (w *walkTrees) SplicePage(ctx context.Context, hwnd uint64, base, merged []wstate.Element, from []int, origins map[int]any) error {
	if w.supersede {
		w.supersede = false
		return uia.ErrSuperseded
	}
	return w.pageTrees.SplicePage(ctx, hwnd, base, merged, from, origins)
}

// readUnder reports whether read r read the children of an Element under
// the frame's document (index 8; its descendants are 9 to 11).
func (r walkRead) readUnder() bool {
	for _, i := range r.expanded {
		if i >= 8 && i <= 11 {
			return true
		}
	}
	return false
}

// gatedPages is fakePages whose read ends only once gate is open.
type gatedPages struct {
	*fakePages
	gate chan struct{}
}

func (g gatedPages) ReadPage(ctx context.Context, w win.WindowInfo, name string) (*page.Raw, error) {
	<-g.gate
	return g.fakePages.ReadPage(ctx, w, name)
}

// electronWindow is a Chromium-class Window of an app that is no browser.
func electronWindow() win.WindowInfo {
	return win.WindowInfo{ID: 302, Class: chromiumWindowClass, PID: 77, App: "Slack", ExePath: `C:\Apps\Slack\slack.exe`, Title: "Sign in - Slack"}
}

// electronAware is fakePages that, like livePages, names no browser for an
// Electron app.
type electronAware struct{ *fakePages }

func (e electronAware) Browser(w win.WindowInfo) string {
	if strings.HasSuffix(w.ExePath, `slack.exe`) {
		return ""
	}
	return e.fakePages.Browser(w)
}

// walkServices is pageServices over walkTrees: the browser Window and an
// Electron Window both show frame.
func walkServices(t *testing.T, pages PageReader, frame func() *wstate.Tree) (Services, *walkTrees) {
	trees := newPageTrees(t)
	trees.frames[101] = cannedTree
	wt := &walkTrees{pageTrees: trees, walked: map[uint64]func() *wstate.Tree{browserWindowID: frame, 302: frame}, waitAt: 0}
	s := testServices(wt)
	s.Windows = func() ([]win.WindowInfo, error) {
		return append(fakeWindows(), browserWindow(), electronWindow()), nil
	}
	s.Pages = pages
	return s, wt
}

func golden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "page", "testdata", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestFrameSourceWalksTheFixture: the source reads chrome-frame.json back
// exactly, so a whole walk of it is today's frame.
func TestFrameSourceWalksTheFixture(t *testing.T) {
	for _, focused := range []int{-1, 7} {
		frame := chromeFrame(t, "Sign in", focused)
		res, err := uia.Walk[int](&frameSource{t: t, els: frame.Elements, focused: focused}, uia.WalkConfig{})
		if err != nil {
			t.Fatal(err)
		}
		got := wstate.Render(&wstate.Tree{Title: frame.Title, App: frame.App, Elements: res.Elements, FocusedIndex: res.FocusedIndex, Notes: res.Notes})
		if want := wstate.Render(frame); got != want {
			t.Errorf("the walk of the fixture:\n%s\nwant:\n%s", got, want)
		}
	}
}

// TestBrowserWalkLeavesTheDocumentOut: with the Page read before the walk
// reaches the document (at entry) or while it reads it (late), the walk
// never reads under the document past the answer, and the Window State is
// the golden, with the same origins.
func TestBrowserWalkLeavesTheDocumentOut(t *testing.T) {
	for _, name := range []string{"login-form", "clickable-div", "cross-origin-iframe", "password-field"} {
		for _, late := range []bool{false, true} {
			raw := pageFixture(t, name)
			fp := &fakePages{raw: raw}
			var pages PageReader = fp
			gate := make(chan struct{})
			if late {
				pages = gatedPages{fp, gate}
			}
			s, trees := walkServices(t, pages, func() *wstate.Tree { return chromeFrame(t, raw.Title, -1) })
			if late {
				trees.waitAt, trees.gate = 8, gate
			}
			out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
			if cmdErr != nil {
				t.Fatal(cmdErr.Error())
			}
			if want := golden(t, name); out != want {
				t.Errorf("%s (late %v):\n%s\nwant:\n%s", name, late, out, want)
			}
			if len(trees.reads) != 1 || !trees.reads[0].frame || trees.reads[0].cut != 8 {
				t.Fatalf("%s (late %v): reads %+v", name, late, trees.reads)
			}
			readDoc := false
			for _, i := range trees.reads[0].expanded {
				readDoc = readDoc || i == 8
				if i > 8 && i <= 11 {
					t.Errorf("%s (late %v): read the children of %d", name, late, i)
				}
			}
			if readDoc != late {
				t.Errorf("%s (late %v): the document's children read: %v", name, late, readDoc)
			}
			m, err := page.Merge(chromeFrame(t, raw.Title, -1), raw)
			if err != nil {
				t.Fatal(err)
			}
			for i := range m.Origins {
				o, perr := pageOrigin(Services{Actions: trees}, browserWindow(), m.Doc+1+i)
				if perr != nil || o == nil || o.BackendNodeID != m.Origins[i].BackendNodeID || o.DocIndex != 8 {
					t.Errorf("%s: index %d origin %+v, %v", name, m.Doc+1+i, o, perr)
				}
			}
		}
	}

	// References and stale checks run over the pruned read as over today's.
	raw := pageFixture(t, "login-form")
	s, trees := walkServices(t, &fakePages{raw: raw}, func() *wstate.Tree { return chromeFrame(t, raw.Title, -1) })
	if _, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"}); cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	if i, err := trees.ResolveElementRef(browserWindowID, "name:Email"); err != nil || i != 10 {
		t.Errorf("name:Email = %d, %v", i, err)
	}
	if i, err := trees.ResolveElementRef(browserWindowID, "name:Cancel"); err != nil || i != 17 {
		t.Errorf("name:Cancel (the infobar, after the Page) = %d, %v", i, err)
	}
	if _, perr := pageOrigin(Services{Actions: trees}, browserWindow(), 18); perr == nil {
		t.Error("an index past the Window State resolves")
	}
}

// TestBrowserWalkFallbackIsTodaysWholeRead: for every reason the Page is
// not merged, the walk read the document whole, and the output is today's
// UI Automation read plus the note.
func TestBrowserWalkFallbackIsTodaysWholeRead(t *testing.T) {
	note := func(reason string) string {
		return "Note: the Page is read through UI Automation because " + reason + ".\n"
	}
	today := wstate.Render(chromeFrame(t, "Sign in", 7))
	for _, c := range []struct {
		err  error
		want string
	}{
		{&page.Unavailable{Reason: "no Browser Connection is open to Chrome; run winuse browser status chrome"}, note("no Browser Connection is open to Chrome; run winuse browser status chrome")},
		{&page.NoTabError{}, note("no Tab of the Browser Connection matches this Window's title and bounds")},
		{&page.AmbiguousTabError{Candidates: []page.Tab{{TargetID: "AAAA1111", Title: "Sign in"}, {TargetID: "BBBB2222", Title: "Sign in"}}},
			note(`several Tabs match this Window's title and bounds: "Sign in" (target AAAA1111), "Sign in" (target BBBB2222)`)},
		{errors.New("Accessibility.getFullAXTree: boom"), note("reading it through the Browser Connection failed: Accessibility.getFullAXTree: boom")},
	} {
		s, trees := walkServices(t, &fakePages{err: c.err}, func() *wstate.Tree { return chromeFrame(t, "Sign in", 7) })
		out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
		if cmdErr != nil {
			t.Fatal(cmdErr.Error())
		}
		if out != today+c.want {
			t.Errorf("%v:\n got %q\nwant %q", c.err, out, today+c.want)
		}
		if len(trees.reads) != 1 || trees.reads[0].cut != -1 || !trees.reads[0].readUnder() {
			t.Errorf("%v: reads %+v", c.err, trees.reads)
		}
	}

	// The sub-budget runs out: the walk never got an answer and read on.
	s, trees := walkServices(t, &fakePages{block: true}, func() *wstate.Tree { return chromeFrame(t, "Sign in", 7) })
	trees.waitAt = -1
	ctx, cancel := context.WithTimeout(context.Background(), pageMargin+300*time.Millisecond)
	defer cancel()
	tree, terr := readWindowTree(s, ctx, browserWindow())
	if terr != nil {
		t.Fatal(terr.Error())
	}
	if out := wstate.Render(tree); !strings.HasPrefix(out, today) || !strings.Contains(out, "took longer than") || !trees.reads[0].readUnder() {
		t.Errorf("out of time:\n%s", out)
	}

	// No document, or several and none named after the Tab: the whole walk,
	// nothing lost.
	raw := pageFixture(t, "login-form")
	several := func() *wstate.Tree {
		f := chromeFrame(t, raw.Title, 7)
		f.Elements[8].Name = "Other"
		f.Elements = append(f.Elements, wstate.Element{Depth: 2, Role: "document", Name: "Side panel"}, wstate.Element{Depth: 3, Role: "text", Name: "saved"})
		return f
	}
	noDoc := func() *wstate.Tree {
		f := chromeFrame(t, raw.Title, 7)
		f.Elements[8].Role = "group"
		return f
	}
	for reason, frame := range map[string]func() *wstate.Tree{
		"UI Automation shows several documents and none is named after the active Tab": several,
		"UI Automation shows no document for the active Tab":                             noDoc,
	} {
		s, trees := walkServices(t, &fakePages{raw: raw}, frame)
		out, _ := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
		if want := wstate.Render(frame()) + note(reason); out != want {
			t.Errorf("%s:\n got %q\nwant %q", reason, out, want)
		}
		if len(trees.reads) != 1 || trees.reads[0].cut != -1 || !trees.reads[0].readUnder() {
			t.Errorf("%s: reads %+v", reason, trees.reads)
		}
	}

	// The only document, not named after the Tab: read whole, merged as
	// today.
	only := func() *wstate.Tree {
		f := chromeFrame(t, raw.Title, 7)
		f.Elements[8].Name = "Other"
		return f
	}
	s, trees = walkServices(t, &fakePages{raw: raw}, only)
	out, _ := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
	m, err := page.Merge(only(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := wstate.Render(m.Tree); out != want || trees.reads[0].cut != -1 {
		t.Errorf("the only document:\n%s\nwant:\n%s", out, want)
	}
}

// TestBrowserWalkSupersededSpliceReadsTheWindowWhole: the merge fails
// after the walk left the document out (a newer read replaced the stored
// one): the Window is read whole again, so the fallback is today's.
func TestBrowserWalkSupersededSpliceReadsTheWindowWhole(t *testing.T) {
	raw := pageFixture(t, "login-form")
	s, trees := walkServices(t, &fakePages{raw: raw}, func() *wstate.Tree { return chromeFrame(t, raw.Title, 7) })
	trees.supersede = true
	out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
	if cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	want := wstate.Render(chromeFrame(t, raw.Title, 7)) + "Note: the Page is read through UI Automation because the Window was read again while its Page was read.\n"
	if out != want {
		t.Errorf("superseded:\n got %q\nwant %q", out, want)
	}
	if len(trees.reads) != 2 || trees.reads[0].cut != 8 || trees.reads[1].frame || !trees.reads[1].readUnder() {
		t.Errorf("reads %+v", trees.reads)
	}
	if o, err := pageOrigin(Services{Actions: trees}, browserWindow(), 10); err != nil || o != nil {
		t.Errorf("the whole read is stored, UIA only: %+v, %v", o, err)
	}
}

// TestBrowserWalkScreenshotAndRecordTree: --screenshot reads the Window
// State the same way; record-tree stays a whole UI Automation read.
func TestBrowserWalkScreenshotAndRecordTree(t *testing.T) {
	raw := pageFixture(t, "login-form")
	s, trees := walkServices(t, &fakePages{raw: raw}, func() *wstate.Tree { return chromeFrame(t, raw.Title, -1) })
	s.Locked = func() (bool, string) { return false, "" }
	s.Shots = shot.NewStore()
	fakeCapture(&s, func(uint64) (shot.Frame, error) { return syntheticFrame(80, 60, 0, 0), nil })
	out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301", Screenshot: true})
	if cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	if want := golden(t, "login-form"); !strings.HasPrefix(out, want) || !strings.HasPrefix(out[len(want):], "Screenshot: ") {
		t.Errorf("--screenshot:\n%s", out)
	}
	if len(trees.reads) != 1 || trees.reads[0].cut != 8 {
		t.Errorf("--screenshot reads %+v", trees.reads)
	}

	file := filepath.Join(t.TempDir(), "frame.json")
	out, cmdErr = runCommand(t, CommandsWith(s), "record-tree", recordArgs{Window: "301", File: file})
	if cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	if !strings.HasSuffix(out, ": 14 elements\n") || len(trees.reads) != 2 || trees.reads[1].frame || !trees.reads[1].readUnder() {
		t.Errorf("record-tree: %q, reads %+v", out, trees.reads)
	}
}

// TestElectronAndOtherWindowsAreWalkedWhole: a Chromium-class Window that
// is no browser, and any other Window, are read as before.
func TestElectronAndOtherWindowsAreWalkedWhole(t *testing.T) {
	fp := &fakePages{raw: pageFixture(t, "login-form")}
	s, trees := walkServices(t, electronAware{fp}, func() *wstate.Tree { return chromeFrame(t, "Sign in", -1) })
	out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "302"})
	if cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	if want := wstate.Render(chromeFrame(t, "Sign in", -1)); out != want {
		t.Errorf("Electron:\n%s\nwant:\n%s", out, want)
	}
	out, _ = runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "101"})
	if out != wstate.Render(cannedTree()) {
		t.Errorf("Notepad:\n%s", out)
	}
	if fp.calls != 0 || len(trees.reads) != 1 || trees.reads[0].frame || !trees.reads[0].readUnder() {
		t.Errorf("page reads %d, walks %+v", fp.calls, trees.reads)
	}
}

// TestBrowserWalkLeavesTheCapsToThePage: a document dense enough to fill
// the element cap through UI Automation no longer does. Merged, the frame
// after the document (the infobar) stays and the Page has the cap the
// frame leaves; on a fallback the output is today's capped read.
func TestBrowserWalkLeavesTheCapsToThePage(t *testing.T) {
	raw := pageFixture(t, "login-form")
	dense := func() *wstate.Tree {
		f := chromeFrame(t, raw.Title, -1)
		els := append([]wstate.Element(nil), f.Elements[:12]...)
		for i := 0; i < uia.DefaultMaxNodes; i++ {
			if i%300 == 0 {
				els = append(els, wstate.Element{Depth: 3, Role: "group"})
			}
			els = append(els, wstate.Element{Depth: 4, Role: "text", Name: "dense"})
		}
		f.Elements = append(els, f.Elements[12:]...)
		return f
	}
	s, trees := walkServices(t, &fakePages{raw: raw}, dense)
	out, _ := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
	if out != golden(t, "login-form") {
		t.Errorf("merged over a dense document:\n%s", out)
	}
	if len(trees.reads[0].expanded) != 10 {
		t.Errorf("%d chunks read, want 10: the frame's 11 Elements but the document", len(trees.reads[0].expanded))
	}

	s, trees = walkServices(t, &fakePages{err: &page.NoTabError{}}, dense)
	out, _ = runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
	whole, _, err := trees.walk(browserWindowID, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := wstate.Render(withPageNote(whole, "no Tab of the Browser Connection matches this Window's title and bounds"))
	if out != want || !strings.Contains(out, "the element cap (3000) was reached") || strings.Contains(out, "started debugging") {
		t.Errorf("fallback over a dense document, ending:\n%s\nwant ending:\n%s", out[max(0, len(out)-300):], want[max(0, len(want)-300):])
	}
}
