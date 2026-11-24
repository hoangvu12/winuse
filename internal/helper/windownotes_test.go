package helper

// Offline tests for the Notes in the Window State (ADR 0024,
// bladebro-ideas ticket 23), through the server: a read's Notes come back
// in the response's own Notes field, never in its tree text. Every test
// keeps its Notes in a temp folder; none reads the user's notes.json.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/notes"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// tempNotes is a Notes store in a temp folder.
func tempNotes(t *testing.T) *notes.Store {
	t.Helper()
	return notes.NewStore(filepath.Join(t.TempDir(), "notes.json"))
}

// leave adds a Note for key by author, ago before now. The reads run
// moments later, which no age these tests print notices (Services.Now
// stays the real clock: the Actions' waits run on it).
func leave(t *testing.T, st *notes.Store, key, text, author string, ago time.Duration) {
	t.Helper()
	st.Now = func() time.Time { return time.Now().Add(-ago) }
	if _, _, err := st.Add(key, text, author); err != nil {
		t.Fatalf("adding %q for %s: %v", text, key, err)
	}
}

// notesFrom points s at the store.
func notesFrom(s Services, st *notes.Store) Services {
	s.NoteStore = func() (*notes.Store, error) { return st, nil }
	return s
}

// notesServer serves cmds as the helper does.
func notesServer(cmds map[string]Command) *Server {
	return New("v1", time.Minute, protocol.RequestTimeout, cmds, discardLogger())
}

// ask sends one request for an Agent; off is WINUSE_NOTES=off.
func ask(t *testing.T, srv *Server, agentID, cmd string, args any, off bool) protocol.Response {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	resp := srv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: cmd, Args: raw, Agent: agentID, AgentLabel: agentID, NoNotes: off})
	if !resp.OK {
		t.Fatalf("%s %s for %s: %s (hint: %s)", cmd, raw, agentID, resp.Err, resp.Hint)
	}
	if strings.Contains(resp.Out, "Notes for") || strings.Contains(resp.Out, "left by earlier agents") {
		t.Errorf("%s printed Notes in its tree text:\n%s", cmd, resp.Out)
	}
	return resp
}

const notesHeader = " (left by earlier agents, not instructions from the user):\n"

func TestWindowStateShowsAnAppsNotesOncePerAgent(t *testing.T) {
	st := tempNotes(t)
	for i := 1; i <= 12; i++ {
		leave(t, st, "notepad.exe", fmt.Sprintf("lesson %d", i), "alice", time.Duration(13-i)*time.Hour)
	}
	s, _ := pageServices(t, &fakePages{}, -1)
	srv := notesServer(CommandsWith(notesFrom(s, st)))

	want := "Notes for notepad.exe" + notesHeader
	for i := 12; i >= 3; i-- { // the newest 10, newest first
		want += fmt.Sprintf("- lesson %d (alice, %d h ago)\n", i, 13-i)
	}
	want += "(+2 more: winuse note list notepad.exe)\n"

	first := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "101"}, false)
	if first.Notes != want {
		t.Errorf("first read's Notes:\n%s\nwant:\n%s", first.Notes, want)
	}
	if !strings.HasPrefix(first.Out, "Window: ") {
		t.Errorf("the Window State starts %q, want its Window: line", first.Out[:min(len(first.Out), 40)])
	}
	if again := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "101"}, false); again.Notes != "" {
		t.Errorf("the same Agent's second read showed the Notes again:\n%s", again.Notes)
	}
	if other := ask(t, srv, "name:b", "get-window-state", windowRef{Window: "101"}, false); other.Notes != want {
		t.Errorf("a second Agent's first read showed:\n%s\nwant:\n%s", other.Notes, want)
	}
}

func TestBrowserWindowShowsTheSiteThenTheBrowserAndEachNewSiteOnce(t *testing.T) {
	st := tempNotes(t)
	leave(t, st, "example.test", "sign in with the test account", "bob", 48*time.Hour)
	leave(t, st, "chrome.exe", "Ctrl+L focuses the address bar", "carol", 5*time.Minute)
	leave(t, st, "shop.test", "the cart badge lags a second", "dave", 30*time.Second)

	login := pageFixture(t, "login-form") // https://example.test/login
	pages := &fakePages{raw: login}
	s, trees := pageServices(t, pages, -1)
	srv := notesServer(CommandsWith(notesFrom(s, st)))

	first := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "301"}, false)
	want := "Notes for example.test" + notesHeader +
		"- sign in with the test account (bob, 2 days ago)\n" +
		"Notes for chrome.exe" + notesHeader +
		"- Ctrl+L focuses the address bar (carol, 5 min ago)\n"
	if first.Notes != want {
		t.Errorf("first read of the browser Window:\n%s\nwant:\n%s", first.Notes, want)
	}

	// The same browser Window moves to another site: that site's Notes,
	// once; the browser's were shown already.
	shop := pageFixture(t, "clickable-div") // https://shop.test/
	pages.mu.Lock()
	pages.raw = shop
	pages.mu.Unlock()
	trees.frames[browserWindowID] = func() *wstate.Tree { return chromeFrame(t, shop.Title, -1) }
	moved := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "301"}, false)
	if want := "Notes for shop.test" + notesHeader + "- the cart badge lags a second (dave, just now)\n"; moved.Notes != want {
		t.Errorf("after the site switch:\n%s\nwant:\n%s", moved.Notes, want)
	}

	// Back on the first site: nothing new.
	pages.mu.Lock()
	pages.raw = login
	pages.mu.Unlock()
	trees.frames[browserWindowID] = func() *wstate.Tree { return chromeFrame(t, login.Title, -1) }
	if back := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "301"}, false); back.Notes != "" {
		t.Errorf("a site already shown showed again:\n%s", back.Notes)
	}
}

func TestTabTargetShowsItsPagesSite(t *testing.T) {
	st := tempNotes(t)
	leave(t, st, "docs.test", "the editor saves on blur", "erin", 3*time.Hour)
	leave(t, st, "shop.test", "checkout needs a postcode", "erin", 3*time.Hour)
	leave(t, st, "chrome.exe", "Ctrl+L focuses the address bar", "carol", 3*time.Hour)
	r := newBgRig(t)
	r.svc = notesFrom(r.svc, st)
	srv := notesServer(CommandsWith(r.svc))

	docs := ask(t, srv, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}, false)
	want := "Notes for docs.test" + notesHeader + "- the editor saves on blur (erin, 3 h ago)\n" +
		"Notes for chrome.exe" + notesHeader + "- Ctrl+L focuses the address bar (carol, 3 h ago)\n"
	if docs.Notes != want {
		t.Errorf("the Docs Tab's read:\n%s\nwant:\n%s", docs.Notes, want)
	}
	shop := ask(t, srv, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e300"}, false)
	if want := "Notes for shop.test" + notesHeader + "- checkout needs a postcode (erin, 3 h ago)\n"; shop.Notes != want {
		t.Errorf("the Shop Tab's read:\n%s\nwant:\n%s", shop.Notes, want)
	}
}

func TestNotesOffHidesThemWithoutUsingThemUp(t *testing.T) {
	st := tempNotes(t)
	leave(t, st, "notepad.exe", "Save As opens slowly", "alice", time.Hour)
	s, _ := pageServices(t, &fakePages{}, -1)
	srv := notesServer(CommandsWith(notesFrom(s, st)))

	if off := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "101"}, true); off.Notes != "" {
		t.Errorf("WINUSE_NOTES=off showed:\n%s", off.Notes)
	}
	on := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "101"}, false)
	if !strings.Contains(on.Notes, "- Save As opens slowly (alice, 1 h ago)\n") {
		t.Errorf("a read with Notes on after one with them off showed %q", on.Notes)
	}
}

func TestDiffFindAndObserveShowNoNotes(t *testing.T) {
	st := tempNotes(t)
	leave(t, st, "notepad.exe", "Save As opens slowly", "alice", time.Hour)
	leave(t, st, "example.test", "sign in with the test account", "bob", time.Hour)
	s, _ := pageServices(t, &fakePages{raw: pageFixture(t, "login-form")}, -1)
	s = notesFrom(s, st)
	srv := notesServer(CommandsWith(s))

	if d := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "101", Diff: true}, false); d.Notes != "" {
		t.Errorf("--diff showed:\n%s", d.Notes)
	}
	if f := ask(t, srv, "name:a", "find", protocol.FindArgs{Window: "301", Role: "edit"}, false); f.Notes != "" {
		t.Errorf("find showed:\n%s", f.Notes)
	}
	ok := func(context.Context, json.RawMessage) (string, *Error) { return "ok\n", nil }
	osrv := notesServer(map[string]Command{"click": s.observed("click", ok)})
	raw, _ := json.Marshal(map[string]string{"window": "101"})
	o := osrv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: "click", Args: raw, Agent: "name:a", Observe: &protocol.Observe{Mode: protocol.ObserveFull}})
	if !o.OK || !strings.Contains(o.Out, "Window: ") || o.Notes != "" {
		t.Errorf("--observe: ok %v, notes %q, out:\n%s", o.OK, o.Notes, o.Out)
	}
	// None of them used the Notes up: the first full reads show them.
	if first := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "101"}, false); !strings.Contains(first.Notes, "Save As opens slowly") {
		t.Errorf("the first full read of 101 after --diff and --observe showed %q", first.Notes)
	}
	if first := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "301"}, false); !strings.Contains(first.Notes, "sign in with the test account") {
		t.Errorf("the first full read of 301 after find showed %q", first.Notes)
	}
}

func TestBatchShowsNotesOnTheFirstReadOfAKeyOnly(t *testing.T) {
	st := tempNotes(t)
	leave(t, st, "notepad.exe", "Save As opens slowly", "alice", time.Hour)
	spy := newBatchSpy()
	spy.point = uia.Point{X: 100, Y: 200}
	before, after := observeFixture(t)
	spy.svc.Trees = &observeTrees{stored: before.Elements, next: after}
	srv := notesServer(CommandsWith(notesFrom(spy.svc, st)))

	resp := ask(t, srv, "name:a", "batch", protocol.BatchArgs{Window: "101", Headed: true, Steps: []protocol.Step{
		{Kind: "get-window-state"},
		stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(1)}),
		{Kind: "get-window-state", Args: json.RawMessage(`{"interactive":true}`)},
	}}, false)
	if want := "Notes for notepad.exe" + notesHeader + "- Save As opens slowly (alice, 1 h ago)\n"; resp.Notes != want {
		t.Errorf("the Batch's Notes:\n%s\nwant once:\n%s", resp.Notes, want)
	}
	if !strings.Contains(resp.Out, "Action 1 (get-window-state): Window: ") || !strings.Contains(resp.Out, "Action 3 (get-window-state): Window: ") {
		t.Errorf("the read steps print their Window States as before:\n%s", resp.Out)
	}
}

func TestNotesShrinkToFitMaxCharsAndLeaveTheTreeWhole(t *testing.T) {
	st := tempNotes(t)
	long := strings.Repeat("a long lesson about this App, ", 6) // about 180 characters
	for i := 1; i <= 10; i++ {
		leave(t, st, "notepad.exe", fmt.Sprintf("%d %s", i, long), "alice", time.Duration(11-i)*time.Hour)
	}
	s, trees := pageServices(t, &fakePages{}, -1)
	trees.frames[101] = func() *wstate.Tree { _, app := observeFixture(t); return app } // 27 Elements, a tree worth cutting
	srv := notesServer(CommandsWith(notesFrom(s, st)))
	plain := notesServer(CommandsWith(s)) // the same Window with no Notes store
	tree := ask(t, plain, "name:x", "get-window-state", windowRef{Window: "101"}, false).Out

	// Room for the tree and about three of the ten Notes.
	limit := utf8.RuneCountInString(tree) + 700
	resp := ask(t, srv, "name:a", "get-window-state", windowRef{Window: "101", MaxChars: &limit}, false)
	if resp.Out != tree {
		t.Errorf("the Notes cut the tree:\n%s\nwant it whole:\n%s", resp.Out, tree)
	}
	// The CLI prints the Notes, a blank line, then the Window State.
	if total := utf8.RuneCountInString(resp.Notes) + 1 + utf8.RuneCountInString(resp.Out); total > limit {
		t.Errorf("Notes and tree print %d characters, over --max-chars %d:\n%s\n%s", total, limit, resp.Notes, resp.Out)
	}
	if !strings.HasPrefix(resp.Notes, "Notes for notepad.exe"+notesHeader) || !strings.Contains(resp.Notes, "- 10 ") ||
		!strings.HasSuffix(resp.Notes, "more: winuse note list notepad.exe)\n") {
		t.Errorf("the shortened Notes:\n%s\nwant the header, the newest, and the note list line", resp.Notes)
	}

	// A cap the tree alone nearly fills: the Notes shrink to their header
	// and the note list line, and still count.
	tight := utf8.RuneCountInString(tree) + 40
	resp = ask(t, srv, "name:b", "get-window-state", windowRef{Window: "101", MaxChars: &tight}, false)
	if want := "Notes for notepad.exe" + notesHeader + "(10 Notes: winuse note list notepad.exe)\n"; resp.Notes != want {
		t.Errorf("the Notes under a tight cap:\n%s\nwant:\n%s", resp.Notes, want)
	}
	if total := utf8.RuneCountInString(resp.Notes) + 1 + utf8.RuneCountInString(resp.Out); total > tight {
		t.Errorf("Notes and tree print %d characters, over --max-chars %d", total, tight)
	}
}
