package notes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "winuse", "notes.json"))
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	return s
}

func TestAddThenListKeepsTextAuthorAndDate(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.Add("github.com", "  the comment box is a placeholder; click it first  ", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Add("github.com", "the Merge button needs a second", "codex"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Add("excel.exe", "Save As takes 3 s to open", "claude"); err != nil {
		t.Fatal(err)
	}
	got, bad, err := s.List("github.com")
	if err != nil || bad != "" {
		t.Fatalf("List: %v %q", err, bad)
	}
	if len(got) != 2 {
		t.Fatalf("List = %d Notes, want 2: %+v", len(got), got)
	}
	want := Note{Key: "github.com", Text: "the comment box is a placeholder; click it first", Author: "claude",
		Added: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	if !got[0].Added.Equal(want.Added) || got[0].Text != want.Text || got[0].Author != want.Author || got[0].Key != want.Key {
		t.Errorf("first Note = %+v, want %+v", got[0], want)
	}
	if got[1].Text != "the Merge button needs a second" || got[1].Author != "codex" {
		t.Errorf("second Note = %+v (oldest first)", got[1])
	}

	// A second store on the same file (a later helper) reads them back.
	again, _, err := NewStore(s.Path).List("excel.exe")
	if err != nil || len(again) != 1 || again[0].Text != "Save As takes 3 s to open" {
		t.Fatalf("reread = %+v, %v", again, err)
	}
}

func TestAddRefusesLongMultiLineEmptyAndDuplicateText(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.Add("github.com", "first", "a"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		text string
		want error
	}{
		{strings.Repeat("x", MaxTextLen+1), ErrTooLong},
		{"line one\nline two", ErrMultiLine},
		{"   ", ErrEmpty},
		{"first", ErrDuplicate},
		{" first ", ErrDuplicate},
	}
	for _, c := range cases {
		if _, _, err := s.Add("github.com", c.text, "a"); !errors.Is(err, c.want) {
			t.Errorf("Add(%.20q) = %v, want %v", c.text, err, c.want)
		}
	}
	// Nothing was stored by the refusals; the same text on another key is fine.
	if got, _, _ := s.List("github.com"); len(got) != 1 {
		t.Errorf("after refusals github.com has %d Notes, want 1", len(got))
	}
	if _, _, err := s.Add("example.com", "first", "a"); err != nil {
		t.Errorf("same text on another key: %v", err)
	}
	// Exactly MaxTextLen characters (not bytes) is accepted.
	if _, _, err := s.Add("github.com", strings.Repeat("é", MaxTextLen), "a"); err != nil {
		t.Errorf("a %d-character text: %v", MaxTextLen, err)
	}
	// A trailing newline (from --stdin) is no second line.
	if _, _, err := s.Add("github.com", "from stdin\r\n", "a"); err != nil {
		t.Errorf("trailing newline: %v", err)
	}
}

func TestKeysCountsEveryKey(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []struct{ key, text string }{
		{"github.com", "a"}, {"excel.exe", "b"}, {"github.com", "c"}, {"localhost:3000", "d"},
	} {
		if _, _, err := s.Add(n.key, n.text, "x"); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := s.Keys()
	if err != nil {
		t.Fatal(err)
	}
	want := []KeyCount{{"excel.exe", 1}, {"github.com", 2}, {"localhost:3000", 1}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Keys = %v, want %v", got, want)
	}
}

func TestRemoveAndClear(t *testing.T) {
	s := newTestStore(t)
	for _, text := range []string{"one", "two", "three"} {
		if _, _, err := s.Add("github.com", text, "x"); err != nil {
			t.Fatal(err)
		}
	}
	s.Add("excel.exe", "keep me", "x")

	removed, _, err := s.Remove("github.com", 2)
	if err != nil || removed.Text != "two" {
		t.Fatalf("Remove 2 = %+v, %v", removed, err)
	}
	got, _, _ := s.List("github.com")
	if len(got) != 2 || got[0].Text != "one" || got[1].Text != "three" {
		t.Errorf("after Remove: %+v", got)
	}
	for _, n := range []int{0, 3, -1} {
		var re *RangeError
		if _, _, err := s.Remove("github.com", n); !errors.As(err, &re) || re.Count != 2 {
			t.Errorf("Remove(%d) = %v, want a RangeError over 2 Notes", n, err)
		}
	}
	n, _, err := s.Clear("github.com")
	if err != nil || n != 2 {
		t.Fatalf("Clear = %d, %v", n, err)
	}
	if got, _, _ := s.List("github.com"); len(got) != 0 {
		t.Errorf("after Clear: %+v", got)
	}
	if got, _, _ := s.List("excel.exe"); len(got) != 1 {
		t.Errorf("Clear touched another key: %+v", got)
	}
	if n, _, err := s.Clear("nothing.example"); err != nil || n != 0 {
		t.Errorf("Clear of an empty key = %d, %v", n, err)
	}
}

func TestListOfAMissingFileIsEmpty(t *testing.T) {
	s := newTestStore(t)
	got, bad, err := s.List("github.com")
	if err != nil || bad != "" || len(got) != 0 {
		t.Fatalf("List on no file = %+v, %q, %v", got, bad, err)
	}
	if _, err := os.Stat(s.Path); !os.IsNotExist(err) {
		t.Errorf("a read created the file: %v", err)
	}
}

// TestConcurrentAddsFromTwoAgents adds from two goroutines on one store
// (two Agents on one helper) and two more on a second store over the same
// file (an old helper finishing its requests during an update): every
// Note lands.
func TestConcurrentAddsFromTwoAgents(t *testing.T) {
	s := newTestStore(t)
	other := NewStore(s.Path)
	const perAgent = 25
	var wg sync.WaitGroup
	errs := make(chan error, 4*perAgent)
	for i, st := range []*Store{s, s, other, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range perAgent {
				if _, _, err := st.Add("github.com", fmt.Sprintf("agent %d lesson %d", i, j), fmt.Sprintf("agent %d", i)); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	got, _, err := s.List("github.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4*perAgent {
		t.Errorf("stored %d Notes, want %d", len(got), 4*perAgent)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.Path), "notes.json.tmp*"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestCorruptFileIsMovedAsideAndReported(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	garbage := []byte(`{"version":1,"notes":[{"key":"github.com","te`)
	if err := os.WriteFile(s.Path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	got, bad, err := s.List("github.com")
	if err != nil {
		t.Fatalf("List on a corrupt file: %v", err)
	}
	if len(got) != 0 || bad != s.Path+".bad" {
		t.Fatalf("List = %+v, moved to %q; want none, moved to %s.bad", got, bad, s.Path)
	}
	kept, err := os.ReadFile(s.Path + ".bad")
	if err != nil || string(kept) != string(garbage) {
		t.Fatalf("the .bad file holds %q, %v; want the corrupt bytes", kept, err)
	}
	// The store works on: an add starts a new file, and says nothing more.
	if _, bad, err := s.Add("github.com", "fresh start", "x"); err != nil || bad != "" {
		t.Fatalf("Add after recovery = %q, %v", bad, err)
	}
	if got, _, _ := s.List("github.com"); len(got) != 1 {
		t.Errorf("after recovery: %+v", got)
	}

	// A second corruption never overwrites the older .bad file: it goes to
	// the next free numbered name, and so does a third.
	for i, want := range []string{s.Path + ".bad.2", s.Path + ".bad.3"} {
		body := fmt.Sprintf("not json %d", i)
		if err := os.WriteFile(s.Path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, bad, err := s.Add("github.com", "again", "x"); err != nil || bad != want {
			t.Fatalf("Add on a corrupt file = %q, %v; want it moved to %s and reported", bad, err, want)
		}
		if kept, _ := os.ReadFile(want); string(kept) != body {
			t.Errorf("%s holds %q, want %q", want, kept, body)
		}
	}
	if kept, _ := os.ReadFile(s.Path + ".bad"); string(kept) != string(garbage) {
		t.Errorf("the first .bad file now holds %q; want the first corrupt bytes kept", kept)
	}
}

func TestAgeIsShortAndReadable(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{-time.Minute, "just now"}, // a clock that went back
		{5 * time.Minute, "5 min ago"},
		{3 * time.Hour, "3 h ago"},
		{30 * time.Hour, "1 day ago"},
		{9 * 24 * time.Hour, "9 days ago"},
		{75 * 24 * time.Hour, "2 months ago"},
		{800 * 24 * time.Hour, "2 years ago"},
	}
	for _, c := range cases {
		if got := Age(now.Add(-c.ago), now); got != c.want {
			t.Errorf("Age(%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}

func TestAuthorIsTheAgentNameOrHarness(t *testing.T) {
	cases := []struct {
		id   agent.Identity
		want string
	}{
		{agent.Identity{ID: "name:reviewer", Label: "reviewer"}, "reviewer"},
		{agent.Identity{ID: "proc:17048:133", Label: "claude 17048"}, "claude"},
		{agent.Identity{ID: agent.DefaultID, Label: agent.DefaultID}, "an unnamed agent"},
	}
	for _, c := range cases {
		if got := Author(c.id); got != c.want {
			t.Errorf("Author(%+v) = %q, want %q", c.id, got, c.want)
		}
	}
}
