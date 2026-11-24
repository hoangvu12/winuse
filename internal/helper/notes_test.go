package helper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/notes"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// noteBed is the note commands over a store in a temp folder, the fake
// desktop plus one browser Window (301), and a Page URL reader that
// answers from urls (by tab id, "" for the shown Tab). It never touches
// the user's notes.json.
type noteBed struct {
	store *notes.Store
	cmds  map[string]Command
	urls  map[string]string
	asked []string // the tab ids pageURL was asked for
	now   time.Time
}

func newNoteBed(t *testing.T) *noteBed {
	t.Helper()
	b := &noteBed{
		store: notes.NewStore(filepath.Join(t.TempDir(), "notes.json")),
		urls:  map[string]string{"": "https://gist.github.com/someone/abc"},
		now:   time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC),
	}
	b.store.Now = func() time.Time { return b.now }
	s := testServices(&fakeTrees{})
	s.Windows = func() ([]win.WindowInfo, error) {
		return append(fakeWindows(), win.WindowInfo{ID: 301, App: "Google Chrome", ExePath: `C:\Program Files\Google\Chrome\Application\chrome.exe`,
			Title: "abc - GitHub - Google Chrome", Class: chromiumWindowClass}), nil
	}
	s.Pages = &fakePages{}
	s.Now = func() time.Time { return b.now }
	b.cmds = noteCommandsWith(s, func() (*notes.Store, error) { return b.store, nil },
		func(ctx context.Context, w win.WindowInfo, tab string) (string, *Error) {
			b.asked = append(b.asked, tab)
			u, ok := b.urls[tab]
			if !ok {
				return "", Fail("no tab has the id "+tab, "")
			}
			return u, nil
		})
	return b
}

// run runs a note command as Agent "reviewer".
func (b *noteBed) run(t *testing.T, cmd string, a protocol.NoteArgs) (string, *Error) {
	t.Helper()
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	ctx := agent.NewContext(context.Background(), agent.Identity{ID: "name:reviewer", Label: "reviewer"})
	return b.cmds[cmd](ctx, raw)
}

func (b *noteBed) mustRun(t *testing.T, cmd string, a protocol.NoteArgs) string {
	t.Helper()
	out, e := b.run(t, cmd, a)
	if e != nil {
		t.Fatalf("%s %+v failed: %s (hint: %s)", cmd, a, e.Msg, e.Hint)
	}
	return out
}

func (b *noteBed) keyNotes(t *testing.T, key string) []notes.Note {
	t.Helper()
	list, _, err := b.store.List(key)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestNoteAddWorksOutTheKeyFromEachTargetForm(t *testing.T) {
	cases := []struct {
		name string
		args protocol.NoteArgs
		key  string
	}{
		{"a native Window is its App", protocol.NoteArgs{Target: "101"}, "notepad.exe"},
		{"a browser Window is its Page's site", protocol.NoteArgs{Target: "301"}, "github.com"},
		{"--app on a browser Window is the browser", protocol.NoteArgs{Target: "301", App: true}, "chrome.exe"},
		{"a URL", protocol.NoteArgs{Target: "https://docs.github.com/en"}, "github.com"},
		{"a domain", protocol.NoteArgs{Target: "news.bbc.co.uk"}, "bbc.co.uk"},
		{"localhost keeps its port", protocol.NoteArgs{Target: "localhost:3000"}, "localhost:3000"},
		{"an exe path", protocol.NoteArgs{Target: `C:\Office\EXCEL.EXE`}, "excel.exe"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newNoteBed(t)
			c.args.Text = "a lesson"
			out := b.mustRun(t, "note-add", c.args)
			if !strings.Contains(out, "for "+c.key) {
				t.Errorf("out = %q, want it to name %s", out, c.key)
			}
			list := b.keyNotes(t, c.key)
			if len(list) != 1 || list[0].Text != "a lesson" || list[0].Author != "reviewer" {
				t.Errorf("%s holds %+v", c.key, list)
			}
		})
	}
}

func TestNoteOnATabTargetUsesThatTabsSite(t *testing.T) {
	b := newNoteBed(t)
	b.urls["1a2b3c4d"] = "http://127.0.0.1:8080/admin"
	b.mustRun(t, "note-add", protocol.NoteArgs{Target: "301:1a2b3c4d", Text: "log in as admin first"})
	if len(b.asked) != 1 || b.asked[0] != "1a2b3c4d" {
		t.Errorf("pageURL asked for %q, want the tab 1a2b3c4d", b.asked)
	}
	if list := b.keyNotes(t, "127.0.0.1:8080"); len(list) != 1 {
		t.Errorf("127.0.0.1:8080 holds %+v", list)
	}
	if _, e := b.run(t, "note-add", protocol.NoteArgs{Target: "101:1a2b3c4d", Text: "x"}); e == nil {
		t.Error("a Tab target on a native Window was accepted")
	}
}

func TestNoteRefusesFreeFormNamesWithTheMatchingWindows(t *testing.T) {
	b := newNoteBed(t)
	_, e := b.run(t, "note-add", protocol.NoteArgs{Target: "Notepad", Text: "x"})
	if e == nil {
		t.Fatal("a free-form name was accepted")
	}
	if e.Exit != 2 || !strings.Contains(e.Hint, `101 notepad.exe "Untitled - Notepad"`) {
		t.Errorf("refusal = %q / %q (exit %d), want the hint to list window 101", e.Msg, e.Hint, e.Exit)
	}
	if strings.Contains(e.Hint, "202") {
		t.Errorf("the hint lists a Window that does not match: %q", e.Hint)
	}
	keys, _, _ := b.store.Keys()
	if len(keys) != 0 {
		t.Errorf("a refused add stored %v", keys)
	}
}

func TestNoteRefusesPagesWithNoSite(t *testing.T) {
	b := newNoteBed(t)
	if _, e := b.run(t, "note-add", protocol.NoteArgs{Target: "chrome://settings", Text: "x"}); e == nil || !strings.Contains(e.Msg, "no site") {
		t.Errorf("chrome:// = %+v, want a no-site refusal", e)
	}
	b.urls[""] = "chrome://newtab/"
	if _, e := b.run(t, "note-add", protocol.NoteArgs{Target: "301", Text: "x"}); e == nil || !strings.Contains(e.Hint, "--app") {
		t.Errorf("a Window on chrome://newtab/ = %+v, want a refusal that points at --app", e)
	}
	if _, e := b.run(t, "note-add", protocol.NoteArgs{Target: "github.com", App: true, Text: "x"}); e == nil || e.Exit != 2 {
		t.Errorf("--app on a domain = %+v, want a usage refusal", e)
	}
}

func TestNoteAddRefusalsStoreNothing(t *testing.T) {
	b := newNoteBed(t)
	b.mustRun(t, "note-add", protocol.NoteArgs{Target: "github.com", Text: "use the API"})
	for _, text := range []string{"use the API", strings.Repeat("x", notes.MaxTextLen+1), "two\nlines"} {
		_, e := b.run(t, "note-add", protocol.NoteArgs{Target: "github.com", Text: text})
		if e == nil || !strings.Contains(e.Msg, "nothing was stored") {
			t.Errorf("add %.20q = %+v, want a refusal saying nothing was stored", text, e)
		}
	}
	if list := b.keyNotes(t, "github.com"); len(list) != 1 {
		t.Errorf("github.com holds %d Notes, want 1", len(list))
	}
}

func TestNoteListRemoveAndClear(t *testing.T) {
	b := newNoteBed(t)
	b.mustRun(t, "note-add", protocol.NoteArgs{Target: "github.com", Text: "first lesson"})
	b.now = b.now.Add(3 * time.Hour)
	b.mustRun(t, "note-add", protocol.NoteArgs{Target: "301", Text: "second lesson"})
	b.mustRun(t, "note-add", protocol.NoteArgs{Target: "excel.exe", Text: "an Excel lesson"})
	b.now = b.now.Add(2 * 24 * time.Hour)

	out := b.mustRun(t, "note-list", protocol.NoteArgs{Target: "github.com"})
	for _, want := range []string{
		"2 Notes for github.com",
		"not instructions from the user",
		"1. first lesson (reviewer, 2 days ago)\n",
		"2. second lesson (reviewer, 2 days ago)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("note list github.com = %q, want %q in it", out, want)
		}
	}
	keys := b.mustRun(t, "note-list", protocol.NoteArgs{})
	if !strings.Contains(keys, "excel.exe\t1 Note\n") || !strings.Contains(keys, "github.com\t2 Notes\n") {
		t.Errorf("note list = %q, want every key and its count", keys)
	}

	out = b.mustRun(t, "note-remove", protocol.NoteArgs{Target: "301", N: 1})
	if !strings.Contains(out, "first lesson") || !strings.Contains(out, "1 left") {
		t.Errorf("note remove = %q", out)
	}
	if _, e := b.run(t, "note-remove", protocol.NoteArgs{Target: "github.com", N: 5}); e == nil || !strings.Contains(e.Msg, "nothing was removed") {
		t.Errorf("remove 5 of 1 = %+v", e)
	}
	out = b.mustRun(t, "note-clear", protocol.NoteArgs{Target: "github.com"})
	if !strings.Contains(out, "cleared 1 Note for github.com") {
		t.Errorf("note clear = %q", out)
	}
	if out := b.mustRun(t, "note-list", protocol.NoteArgs{Target: "github.com"}); !strings.Contains(out, "no Notes for github.com") {
		t.Errorf("after clear: %q", out)
	}
	if list := b.keyNotes(t, "excel.exe"); len(list) != 1 {
		t.Errorf("clear touched excel.exe: %+v", list)
	}
}

func TestNoteCommandsAnswerJSON(t *testing.T) {
	b := newNoteBed(t)
	var added struct {
		Key   string     `json:"key"`
		From  string     `json:"from"`
		Added notes.Note `json:"added"`
		Count int        `json:"count"`
	}
	out := b.mustRun(t, "note-add", protocol.NoteArgs{Target: "301", Text: "wait for the editor", JSON: true})
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatalf("note add --json = %q: %v", out, err)
	}
	if added.Key != "github.com" || added.From != "window 301" || added.Count != 1 || added.Added.Author != "reviewer" {
		t.Errorf("note add --json = %+v", added)
	}

	var listed struct {
		Key   string `json:"key"`
		Notes []struct {
			N      int    `json:"n"`
			Text   string `json:"text"`
			Author string `json:"author"`
			Age    string `json:"age"`
		} `json:"notes"`
	}
	out = b.mustRun(t, "note-list", protocol.NoteArgs{Target: "github.com", JSON: true})
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("note list --json = %q: %v", out, err)
	}
	if len(listed.Notes) != 1 || listed.Notes[0].N != 1 || listed.Notes[0].Text != "wait for the editor" || listed.Notes[0].Age != "just now" {
		t.Errorf("note list --json = %+v", listed)
	}

	var keys struct {
		Keys []notes.KeyCount `json:"keys"`
	}
	out = b.mustRun(t, "note-list", protocol.NoteArgs{JSON: true})
	if err := json.Unmarshal([]byte(out), &keys); err != nil || len(keys.Keys) != 1 || keys.Keys[0] != (notes.KeyCount{Key: "github.com", Count: 1}) {
		t.Errorf("note list --json (keys) = %q, %v", out, err)
	}

	var cleared struct {
		Cleared int `json:"cleared"`
	}
	out = b.mustRun(t, "note-clear", protocol.NoteArgs{Target: "github.com", JSON: true})
	if err := json.Unmarshal([]byte(out), &cleared); err != nil || cleared.Cleared != 1 {
		t.Errorf("note clear --json = %q, %v", out, err)
	}
}

func TestNoteCommandSaysWhenTheFileWasCorrupt(t *testing.T) {
	b := newNoteBed(t)
	if err := os.WriteFile(b.store.Path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := b.mustRun(t, "note-list", protocol.NoteArgs{Target: "github.com"})
	if !strings.Contains(out, "could not be read") || !strings.Contains(out, b.store.Path+".bad") {
		t.Errorf("note list on a corrupt file = %q, want it to say the file moved to .bad", out)
	}
	if _, err := os.Stat(b.store.Path + ".bad"); err != nil {
		t.Errorf("no .bad file: %v", err)
	}
}

func TestNoteCommandsAreRegisteredWithoutALane(t *testing.T) {
	cmds := CommandsWith(Services{Trees: &fakeTrees{}})
	for _, name := range protocol.NoteCommands {
		if _, ok := cmds[name]; !ok {
			t.Errorf("%s is not registered", name)
		}
		if !observationOnlyCommand(name) {
			t.Errorf("%s would take a lane; it touches no Window", name)
		}
	}
}
