package helper

// find, --root and --skeleton (desktop-parity 04): find prints the matches
// the walk added with their new Element Indexes, --count only the count, a
// role that matched nothing the roles present; --root reads a subtree into
// new indexes, a Page Element's from the stored Window State with its own;
// --skeleton is a full read with its note; the flag combinations that make
// no sense are refused. The tree reader is a fake; no COM.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// findTrees is a tree reader with find and the region read.
type findTrees struct {
	fakeTrees
	found      uia.FindResult
	findErr    error
	findCalls  int
	first      bool
	countOnly  bool
	matched    []bool // the canned Elements the query's match accepted
	region     uia.Added
	regionErr  error
	regionArgs [3]int // index, depth, interactive (1)
}

// cannedFindElements are what the fake walk saw.
var cannedFindElements = []wstate.Element{
	{Role: "window", Name: "title"},
	{Depth: 3, Role: "button", Name: "Save", AutomationId: "save"},
	{Depth: 5, Role: "button", Name: "Open"},
	{Depth: 2, Role: "text", Name: "Status"},
}

func (f *findTrees) Find(ctx context.Context, hwnd uint64, budget time.Duration, match func(wstate.Element) bool, first, countOnly bool) (uia.FindResult, error) {
	f.findCalls++
	f.first, f.countOnly = first, countOnly
	f.matched = nil
	for _, e := range cannedFindElements {
		f.matched = append(f.matched, match(e))
	}
	return f.found, f.findErr
}

func (f *findTrees) ReadRegion(ctx context.Context, hwnd uint64, index, depth int, interactive bool, budget time.Duration) (uia.Added, error) {
	in := 0
	if interactive {
		in = 1
	}
	f.regionArgs = [3]int{index, depth, in}
	return f.region, f.regionErr
}

func findServices() (Services, *findTrees) {
	trees := &findTrees{fakeTrees: fakeTrees{trees: map[uint64]*wstate.Tree{101: cannedTree()}}}
	s := testServices(trees)
	return s, trees
}

func TestFindPrintsTheAddedMatches(t *testing.T) {
	s, trees := findServices()
	trees.found = uia.FindResult{Count: 2, Added: uia.Added{Indexes: []int{3, 4}, Elements: cannedFindElements[1:3]}}
	out, e := runCommand(t, CommandsWith(s), "find", protocol.FindArgs{Window: "101", Role: "Button"})
	if e != nil {
		t.Fatalf("find: %+v", e)
	}
	want := "Window: \"Untitled - Notepad\", App: notepad.exe.\n" +
		"\t3 button Save ID: save\n" +
		"\t4 button Open\n" +
		"\n" +
		"Note: these Elements (matched role \"Button\") are in window 101's Window State as 3 to 4; its earlier Element Indexes still work until the next full get-window-state\n"
	if out != want {
		t.Errorf("output:\n%q\nwant:\n%q", out, want)
	}
	if got := trees.matched; len(got) != 4 || got[0] || !got[1] || !got[2] || got[3] {
		t.Errorf("the query matched %v", got)
	}
}

func TestFindCountFirstAndMisses(t *testing.T) {
	s, trees := findServices()
	cmds := CommandsWith(s)
	trees.found = uia.FindResult{Count: 7}
	if out, e := runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Text: "x", Count: true}); e != nil || out != "7\n" || !trees.countOnly {
		t.Errorf("count: %q, %+v", out, e)
	}
	trees.found = uia.FindResult{Count: 1, Added: uia.Added{Indexes: []int{3}, Elements: cannedFindElements[1:2]}}
	if _, e := runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Name: "save", First: true}); e != nil || !trees.first {
		t.Errorf("first: %+v", e)
	}

	trees.found = uia.FindResult{RolesPresent: []string{"button", "text", "window"}}
	_, e := runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Role: "slider"})
	if e == nil || e.Msg != `no Element of window 101 matches role "slider"; nothing was added` || e.Hint != "the Window shows these roles: button, text, window" {
		t.Errorf("role miss: %+v", e)
	}
	_, e = runCommand(t, cmds, "find", protocol.FindArgs{Window: "101", Name: "nothing"})
	if e == nil || !strings.Contains(e.Hint, "loosen the filters") {
		t.Errorf("name miss: %+v", e)
	}

	calls := trees.findCalls
	for _, a := range []protocol.FindArgs{
		{Window: "101", State: "pressed"},
		{Window: "101", First: true, Count: true},
	} {
		if _, e := runCommand(t, cmds, "find", a); e == nil {
			t.Errorf("%+v was accepted", a)
		}
	}
	if trees.findCalls != calls {
		t.Error("a refused find walked the tree")
	}
}

func TestRegionRead(t *testing.T) {
	s, trees := findServices()
	trees.region = uia.Added{Indexes: []int{3, 4, 5}, Elements: []wstate.Element{
		{Role: "pane", Name: "Body"},
		{Depth: 1, Role: "button", Name: "Go"},
		{Depth: 2, Role: "text", Name: "label"},
	}, Notes: []string{"this Window State is partial: the depth cap (2) was reached"}}
	out, e := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "101", Root: "1", Depth: 2, Interactive: true})
	if e != nil {
		t.Fatalf("--root: %+v", e)
	}
	want := "Window: \"Untitled - Notepad\", App: notepad.exe.\n" +
		"\t3 pane Body\n" +
		"\t\t4 button Go\n" +
		"\t\t\t5 text label\n" +
		"\n" +
		"Note: this Window State is partial: the depth cap (2) was reached\n" +
		"Note: these Elements (the interactive Elements under 1) are in window 101's Window State as 3 to 5; its earlier Element Indexes still work until the next full get-window-state\n"
	if out != want {
		t.Errorf("output:\n%q\nwant:\n%q", out, want)
	}
	if trees.regionArgs != [3]int{1, 2, 1} {
		t.Errorf("region read %v", trees.regionArgs)
	}
}

func TestSkeletonAndRefusedShapes(t *testing.T) {
	s, _ := findServices()
	cmds := CommandsWith(s)
	out, e := runCommand(t, cmds, "get-window-state", windowRef{Window: "101", Skeleton: true})
	if e != nil || !strings.HasSuffix(out, "Note: this is a skeleton: 3 levels below the Window, with each deeper part's child count; read a part whole with winuse get-window-state 101 --root <element>\n") {
		t.Errorf("skeleton: %q, %+v", out, e)
	}
	for _, a := range []windowRef{
		{Window: "101", Skeleton: true, Root: "1"},
		{Window: "101", Depth: 2},
		{Window: "101", Interactive: true},
		{Window: "101", Root: "1", Screenshot: true},
		{Window: "101", Root: "-1"},
		{Window: "101", Root: "Save"},
	} {
		if _, e := runCommand(t, cmds, "get-window-state", a); e == nil {
			t.Errorf("%+v was accepted", a)
		}
	}
}

// storedPageTrees adds the stored Window State (StoredElements) to the
// page test rig's tree reader.
type storedPageTrees struct{ *pageTrees }

func (p storedPageTrees) StoredElements(ctx context.Context, hwnd uint64) ([]wstate.Element, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	els, ok := p.state[hwnd]
	return els, ok
}

// A browser Window's Page is read whole over CDP: find filters a fresh
// full read and says so; --root on a Page Element prints its subtree with
// the indexes it has.
func TestFindAndRootOnABrowserWindow(t *testing.T) {
	raw := pageFixture(t, "login-form")
	s, trees := pageServices(t, &fakePages{raw: raw}, -1)
	s.Actions = storedPageTrees{trees}
	cmds := CommandsWith(s)
	out, e := runCommand(t, cmds, "find", protocol.FindArgs{Window: "301", Role: "edit"})
	if e != nil {
		t.Fatalf("find: %+v", e)
	}
	if !strings.Contains(out, "\t10 edit (settable, string) Email Value: ada@example.com\n") ||
		!strings.Contains(out, "Note: find read window 301's Window State afresh through its Browser Connection") {
		t.Errorf("find on the Page:\n%s", out)
	}
	if trees.splices != 1 {
		t.Errorf("%d splices, want the one fresh read", trees.splices)
	}
	// The document the Page was spliced under: its stored subtree.
	out, e = runCommand(t, cmds, "get-window-state", windowRef{Window: "301", Root: "8", Interactive: true})
	if e != nil {
		t.Fatalf("--root on the document: %+v", e)
	}
	if !strings.HasPrefix(out, "Window: \"Sign in - Google Chrome\", App: chrome.exe.\n\t8 document Sign in") ||
		!strings.Contains(out, "\t\t10 edit (settable, string) Email") || strings.Contains(out, "9 heading") ||
		strings.Contains(out, "16 group") || !strings.Contains(out, "Element 8 holds Page Elements") {
		t.Errorf("--root 8 --interactive:\n%s", out)
	}
	// A Page Element itself.
	out, e = runCommand(t, cmds, "get-window-state", windowRef{Window: "301", Root: "9"})
	if e != nil || !strings.Contains(out, "\t9 heading Sign in to Example\n") {
		t.Errorf("--root 9: %q, %+v", out, e)
	}
}
