package page

import (
	"errors"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// switchedFrame is the frame Chrome 155 showed through UI Automation
// after a Headless tab switch through the extension (measured live, the
// same 300 ms and 3 s later): the previous Tab's document, still whole
// with its 7 children and moved to the window's top, and an empty unnamed
// document where the active Tab is shown. The address bar shows the
// active Tab's URL without its scheme. staleFirst puts the stale document
// before the placeholder.
func switchedFrame(title string, staleFirst bool) *wstate.Tree {
	stale := []wstate.Element{
		{Depth: 2, Role: "document", Name: "Example Domain", AutomationId: "RootWebArea", Value: "https://example.com/"},
		{Depth: 3, Role: "heading", Name: "Example Domain"},
		{Depth: 3, Role: "text", Name: "This domain is for use in documentation examples without needing permission."},
		{Depth: 3, Role: "text", Name: "Avoid use in operations."},
		{Depth: 3, Role: "link", Name: "Learn more"},
		{Depth: 4, Role: "text", Name: "Learn more"},
		{Depth: 3, Role: "group"},
		{Depth: 3, Role: "text", Name: "end"},
	}
	placeholder := []wstate.Element{{Depth: 2, Role: "document", AutomationId: "view_1033"}}
	docs := append(append([]wstate.Element(nil), placeholder...), stale...)
	if staleFirst {
		docs = append(append([]wstate.Element(nil), stale...), placeholder...)
	}
	els := []wstate.Element{
		{Depth: 0, Role: "window", Name: title + " - Google Chrome"},
		{Depth: 1, Role: "pane", Name: "Google Chrome"},
		{Depth: 2, Role: "tab list", Name: "Tabs"},
		{Depth: 3, Role: "tab", Name: "Example Domain", Selectable: true},
		{Depth: 3, Role: "tab", Name: title, Selectable: true, Selected: true},
		{Depth: 2, Role: "toolbar", AutomationId: "view_1000"},
		{Depth: 3, Role: "edit", Name: "Address and search bar", Settable: "string", Value: "127.0.0.1:49901/browser-pages.html"},
	}
	els = append(els, docs...)
	els = append(els,
		wstate.Element{Depth: 2, Role: "group", Name: `"winuse" started debugging this browser`},
		wstate.Element{Depth: 3, Role: "button", Name: "Cancel"})
	return &wstate.Tree{Title: title + " - Google Chrome", App: "chrome.exe", Elements: els, FocusedIndex: -1}
}

// switchedRaw is the active Tab's Page read: the login form under the
// given title and URL, with the previous Tab (example.com) still open.
func switchedRaw(t *testing.T, title, url string) *Raw {
	raw := loadRaw(t, "login-form")
	raw.Title, raw.URL = title, url
	raw.Others = []string{"https://example.com/", "chrome://newtab/"}
	return raw
}

// TestMergeAfterAHeadlessTabSwitch: the two measured shapes. The stale
// Tab's document is never picked, even when named like the active Tab, and
// leaves the Window State with its descendants; the Page goes under the
// empty placeholder, which sits where the Page is shown.
func TestMergeAfterAHeadlessTabSwitch(t *testing.T) {
	cases := map[string]*Raw{
		"another title":  switchedRaw(t, "Example Domains", "http://127.0.0.1:49901/browser-pages.html"),
		"the same title": switchedRaw(t, "Example Domain", "https://example.org/"),
	}
	for name, raw := range cases {
		for _, staleFirst := range []bool{true, false} {
			frame := switchedFrame(raw.Title, staleFirst)
			before := wstate.Render(frame)
			m, err := Merge(frame, raw)
			if err != nil {
				t.Fatalf("%s (stale first %v): %v", name, staleFirst, err)
			}
			if wstate.Render(frame) != before {
				t.Errorf("%s: Merge modified the frame", name)
			}
			doc := m.Tree.Elements[m.Doc]
			if doc.Role != "document" || doc.AutomationId != "view_1033" || frame.Elements[m.From[m.Doc]].AutomationId != "view_1033" {
				t.Errorf("%s (stale first %v): picked %+v", name, staleFirst, doc)
			}
			for _, e := range m.Tree.Elements {
				if e.AutomationId == "RootWebArea" || e.Name == "Learn more" {
					t.Errorf("%s (stale first %v): the stale document's %s %q is still shown", name, staleFirst, e.Role, e.Name)
				}
			}
			// From maps every kept frame Element back, and the Page's to -1.
			if len(m.From) != len(m.Tree.Elements) {
				t.Fatalf("%s: %d frame indexes for %d Elements", name, len(m.From), len(m.Tree.Elements))
			}
			page := 0
			for i, f := range m.From {
				switch {
				case f < 0:
					page++
					if i <= m.Doc || i > m.Doc+len(m.Origins) {
						t.Errorf("%s: a Page Element at %d, outside the document", name, i)
					}
				case frame.Elements[f].Name != m.Tree.Elements[i].Name || frame.Elements[f].Role != m.Tree.Elements[i].Role:
					t.Errorf("%s: Element %d is not frame Element %d", name, i, f)
				}
			}
			if page != len(m.Origins) || page == 0 {
				t.Errorf("%s: %d Page Elements, %d origins", name, page, len(m.Origins))
			}
			for _, o := range m.Origins {
				if o.DocIndex != m.Doc {
					t.Errorf("%s: DocIndex %d, want %d", name, o.DocIndex, m.Doc)
				}
			}
			if m.Tree.FocusedIndex != m.Doc+2 || m.Tree.Elements[m.Tree.FocusedIndex].Name != "Email" {
				t.Errorf("%s: focused %d", name, m.Tree.FocusedIndex)
			}
		}
	}

	// The whole Window State, for the same title with the stale document
	// first: the frame, the placeholder with the Page under it, the infobar.
	raw := cases["the same title"]
	m, err := Merge(switchedFrame(raw.Title, true), raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `Window: "Example Domain - Google Chrome", App: chrome.exe.
	0 window Example Domain - Google Chrome
		1 pane Google Chrome
			2 tab list Tabs
				3 tab (selectable) Example Domain
				4 tab (selectable, selected) Example Domain
			5 toolbar ID: view_1000
				6 edit (settable, string) Address and search bar Value: 127.0.0.1:49901/browser-pages.html
			7 document ID: view_1033
				8 heading Sign in to Example
				9 edit (settable, string) Email Value: ada@example.com
				10 edit (settable, string) Password
				11 check box Remember me off Secondary Actions: Toggle
				12 button Sign in
				13 link Forgot your password?
				14 button (disabled) Create account Description: Opens the sign-up page
			15 group "winuse" started debugging this browser
				16 button Cancel

The focused UI element is 9 edit (settable, string) Email Value: ada@example.com.
`
	if got := wstate.Render(m.Tree); got != want {
		t.Errorf("Window State:\n%s\nwant:\n%s", got, want)
	}
}

// TestDocumentRuleWithoutTheOtherTabs: a read that does not know the other
// Tabs (Others empty) calls no document stale, so every fallback is
// today's, word for word.
func TestDocumentRuleWithoutTheOtherTabs(t *testing.T) {
	raw := switchedRaw(t, "Example Domains", "http://127.0.0.1:49901/browser-pages.html")
	raw.Others = nil
	_, err := Merge(switchedFrame(raw.Title, true), raw)
	var u *Unavailable
	if !errors.As(err, &u) || FallbackNote(u.Reason) != "the Page is read through UI Automation because UI Automation shows several documents and none is named after the active Tab." {
		t.Fatalf("err = %v", err)
	}
	// The other Tab's URL equal to the active Tab's proves nothing either.
	raw = switchedRaw(t, "Example Domains", "https://example.com/")
	if _, err := Merge(switchedFrame(raw.Title, true), raw); !errors.As(err, &u) || !strings.Contains(u.Reason, "several documents") {
		t.Fatalf("same URL: err = %v", err)
	}
}

// TestDocumentRuleCases: the rest of the rule.
func TestDocumentRuleCases(t *testing.T) {
	raw := loadRaw(t, "login-form")
	raw.Others = []string{"https://example.com/"}
	var u *Unavailable

	// Only stale documents: none for the active Tab, with today's reason.
	frame := switchedFrame(raw.Title, true)
	frame.Elements = append(frame.Elements[:7], frame.Elements[7:15]...)
	frame.Elements = append(frame.Elements, wstate.Element{Depth: 2, Role: "group", Name: "infobar"})
	if _, err := Merge(frame, raw); !errors.As(err, &u) || u.Reason != "UI Automation shows no document for the active Tab" {
		t.Fatalf("only stale: err = %v", err)
	}

	// A stale document named like the Tab before the Tab's own: the Tab's
	// own is picked by name, the stale one left out.
	frame = frameFor(t, raw.Title)
	staleDoc := []wstate.Element{
		{Depth: 2, Role: "document", Name: raw.Title, Value: "https://example.com/#top"},
		{Depth: 3, Role: "button", Name: "old"},
	}
	frame.Elements = append(append(append([]wstate.Element(nil), frame.Elements[:5]...), staleDoc...), frame.Elements[5:]...)
	m, err := Merge(frame, raw)
	if err != nil || m.From[m.Doc] != 10 || m.Doc != 8 {
		t.Fatalf("named stale first: %+v, %v", m, err)
	}
	if got, want := wstate.Render(m.Tree), wstate.Render(mustMerge(t, frameFor(t, raw.Title), raw).Tree); got != want {
		t.Errorf("named stale first:\n%s\nwant the golden:\n%s", got, want)
	}

	// A stale document under the Tab's (an iframe showing another Tab's
	// URL) goes with the rest of it: the golden, unchanged.
	frame = frameFor(t, raw.Title)
	frame.Elements = append(frame.Elements[:12:12], append([]wstate.Element{
		{Depth: 3, Role: "document", Name: "embed", Value: "https://example.com/"},
		{Depth: 4, Role: "text", Name: "embedded"},
	}, frame.Elements[12:]...)...)
	if got, want := wstate.Render(mustMerge(t, frame, raw).Tree), wstate.Render(mustMerge(t, frameFor(t, raw.Title), raw).Tree); got != want {
		t.Errorf("stale under the document:\n%s\nwant:\n%s", got, want)
	}

	// Several candidates, none named after the Tab (the title changed
	// between the reads, a side panel is open): no pick, even when only
	// one has the Tab's URL. Nothing stale is involved, so the fallback is
	// today's.
	frame = frameFor(t, raw.Title)
	frame.Elements[8].Name = "Old title"
	frame.Elements[8].Value = raw.URL
	frame.Elements = append(frame.Elements, wstate.Element{Depth: 1, Role: "document", Name: "Reading list", Value: "chrome://read-later.top-chrome/"})
	if _, err := Merge(frame, raw); !errors.As(err, &u) || !strings.Contains(u.Reason, "several documents") {
		t.Fatalf("several: err = %v", err)
	}
	// A side panel is no stale document: it stays, whole.
	frame.Elements[8].Name = raw.Title
	if m := mustMerge(t, frame, raw); m.Tree.Elements[len(m.Tree.Elements)-1].Name != "Reading list" {
		t.Errorf("the side panel is gone: %+v", m.Tree.Elements[len(m.Tree.Elements)-1])
	}

	// The focus on a stale document counts as none in the frame.
	raw = switchedRaw(t, "Example Domain", "https://example.org/")
	frame = switchedFrame(raw.Title, true)
	frame.FocusedIndex = 7
	if m := mustMerge(t, frame, raw); m.Tree.Elements[m.Tree.FocusedIndex].Name != "Email" {
		t.Errorf("focus on the stale document: %d", m.Tree.FocusedIndex)
	}
	// The focus in the address bar stays there, after a stale document.
	frame = switchedFrame(raw.Title, true)
	frame.FocusedIndex = 6
	frame.Elements = append(frame.Elements[:5:5], append(frame.Elements[7:15:15], frame.Elements[5:7]...)...)
	frame.Elements = append(frame.Elements, switchedFrame(raw.Title, true).Elements[15:]...)
	frame.FocusedIndex = 14
	if m := mustMerge(t, frame, raw); m.Tree.Elements[m.Tree.FocusedIndex].Name != "Address and search bar" || m.Tree.FocusedIndex != 6 {
		t.Errorf("focus in the address bar: %d", m.Tree.FocusedIndex)
	}
}

// TestDocRuleNamed is the walk's half of the rule.
func TestDocRuleNamed(t *testing.T) {
	r := RuleFor(&Raw{Title: "Example Domain", URL: "https://example.org/", Others: []string{"https://example.com/", "https://example.org/#x", ""}})
	for _, c := range []struct {
		name, value string
		want        bool
	}{
		{"Example Domain", "", true},
		{"Example Domain", "https://example.org/", true},
		{"Example Domain", "https://example.org/#more", true},
		{"Example Domain", "https://example.net/", true}, // no Tab's URL: proves nothing
		{"Example Domain", "https://example.com/", false},
		{"Example Domain", "https://example.com/#top", false},
		{"", "", false},
		{"Other", "https://example.org/", false},
	} {
		if got := r.Named(c.name, c.value); got != c.want {
			t.Errorf("Named(%q, %q) = %v", c.name, c.value, got)
		}
	}
	if (DocRule{}).Named("", "") || (DocRule{}).Stale("https://example.com/") {
		t.Error("the zero rule names or calls stale")
	}
}

func mustMerge(t *testing.T, frame *wstate.Tree, raw *Raw) *Merged {
	t.Helper()
	m, err := Merge(frame, raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
