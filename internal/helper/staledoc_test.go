package helper

import (
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// The document after a Headless tab switch through the extension: Chrome
// keeps the previous Tab's document in its UI Automation tree and shows
// the active Tab's place as an empty document (page.DocRule). These tests
// walk such frames with the real uia.Walk and read them through
// get-window-state.

// switchedChromeFrame is the measured frame: the previous Tab's document
// (example.com, named stale) and the empty placeholder, both under the
// browser's pane, between the toolbar and the infobar. With docName set,
// the Tab's own document follows the placeholder's place instead, named
// docName, with two UIA descendants.
func switchedChromeFrame(title, staleName, docName string) *wstate.Tree {
	els := []wstate.Element{
		{Depth: 0, Role: "window", Name: title + " - Google Chrome"},
		{Depth: 1, Role: "pane", Name: "Google Chrome"},
		{Depth: 2, Role: "toolbar", AutomationId: "view_1000"},
		{Depth: 3, Role: "edit", Name: "Address and search bar", Settable: "string", Value: "example.test/login"},
		{Depth: 2, Role: "document", Name: staleName, AutomationId: "RootWebArea", Value: "https://example.com/"},
		{Depth: 3, Role: "text", Name: "Example Domain"},
		{Depth: 3, Role: "link", Name: "Learn more"},
		{Depth: 4, Role: "text", Name: "Learn more"},
	}
	if docName == "" {
		els = append(els, wstate.Element{Depth: 2, Role: "document", AutomationId: "view_1033"})
	} else {
		els = append(els,
			wstate.Element{Depth: 2, Role: "document", Name: docName, AutomationId: "RootWebArea", Value: "https://example.test/login"},
			wstate.Element{Depth: 3, Role: "group"},
			wstate.Element{Depth: 4, Role: "text", Name: "read through UI Automation"})
	}
	els = append(els,
		wstate.Element{Depth: 2, Role: "group", Name: `"winuse" started debugging this browser`},
		wstate.Element{Depth: 3, Role: "button", Name: "Cancel"})
	return &wstate.Tree{Title: title + " - Google Chrome", App: "chrome.exe", Elements: els, FocusedIndex: -1}
}

// TestWindowStateAfterAHeadlessTabSwitch: the measured shape with the same
// title on both Tabs. The walk cuts nothing (the placeholder has nothing to
// cut), Merge puts the Page under the placeholder, and the stale document
// leaves the Window State and the stored Elements; one walk, no fallback.
func TestWindowStateAfterAHeadlessTabSwitch(t *testing.T) {
	raw := pageFixture(t, "login-form")
	raw.Title, raw.URL = "Example Domain", "https://example.org/"
	raw.Others = []string{"https://example.com/"}
	frame := func() *wstate.Tree { return switchedChromeFrame(raw.Title, raw.Title, "") }
	s, trees := walkServices(t, &fakePages{raw: raw}, frame)
	out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
	if cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	m, err := page.Merge(frame(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := wstate.Render(m.Tree); out != want {
		t.Errorf("Window State:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "Learn more") || strings.Contains(out, "Note:") || !strings.Contains(out, "\t\t\t4 document ID: view_1033\n\t\t\t\t5 heading Sign in to Example\n") {
		t.Errorf("Window State:\n%s", out)
	}
	if len(trees.reads) != 1 || trees.reads[0].cut != -1 {
		t.Errorf("reads %+v", trees.reads)
	}
	for i, e := range trees.state[browserWindowID] {
		if e.AutomationId == "RootWebArea" {
			t.Errorf("stored Element %d is the stale document", i)
		}
	}
	o, perr := pageOrigin(Services{Actions: trees}, browserWindow(), m.Doc+1)
	if perr != nil || o == nil || o.DocIndex != 4 {
		t.Errorf("origin %+v, %v", o, perr)
	}
	if i, err := trees.ResolveElementRef(browserWindowID, "name:Cancel"); err != nil || i != len(m.Tree.Elements)-1 {
		t.Errorf("name:Cancel = %d, %v", i, err)
	}
}

// TestWalkCutsTheTabsDocumentNotTheStaleOne: a stale document named like
// the Tab comes first; the walk reads it whole and cuts the Tab's own
// document, the one Merge picks, at entry and late. One walk, no re-read,
// and the stale document is left out.
func TestWalkCutsTheTabsDocumentNotTheStaleOne(t *testing.T) {
	for _, late := range []bool{false, true} {
		raw := pageFixture(t, "login-form")
		raw.Others = []string{"https://example.com/"}
		frame := func() *wstate.Tree { return switchedChromeFrame(raw.Title, raw.Title, raw.Title) }
		fp := &fakePages{raw: raw}
		var pages PageReader = fp
		gate := make(chan struct{})
		if late {
			pages = gatedPages{fp, gate}
		}
		s, trees := walkServices(t, pages, frame)
		if late {
			trees.waitAt, trees.gate = 8, gate // the Tab's document's chunk
		}
		out, cmdErr := runCommand(t, CommandsWith(s), "get-window-state", windowRef{Window: "301"})
		if cmdErr != nil {
			t.Fatal(cmdErr.Error())
		}
		m, err := page.Merge(frame(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if want := wstate.Render(m.Tree); out != want || strings.Contains(out, "Learn more") {
			t.Errorf("late %v:\n%s\nwant:\n%s", late, out, want)
		}
		if len(trees.reads) != 1 || trees.reads[0].cut != 8 || m.From[m.Doc] != 8 || m.Doc != 4 {
			t.Fatalf("late %v: reads %+v, doc %d from %d", late, trees.reads, m.Doc, m.From[m.Doc])
		}
		readStale, readUnder := false, false
		for _, i := range trees.reads[0].expanded {
			readStale = readStale || i == 4
			readUnder = readUnder || i == 9
		}
		if !readStale || readUnder {
			t.Errorf("late %v: expanded %v", late, trees.reads[0].expanded)
		}
	}
}
