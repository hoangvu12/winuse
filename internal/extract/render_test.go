package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func itoa(n int) string { return strconv.Itoa(n) }

func rows(n int) []json.RawMessage {
	out := make([]json.RawMessage, n)
	for i := range out {
		out[i] = json.RawMessage(`{"title":"Result ` + string(rune('A'+i%26)) + `","url":"https://a.test/` + string(rune('a'+i%26)) + `"}`)
	}
	return out
}

func TestRenderCompleteListWithRootHint(t *testing.T) {
	r := Result{Kind: "list", Container: "<ul>", Label: "Search results", Rows: rows(2), Found: 2, Limit: 50}
	got, err := Render(r, Options{RootHint: "winuse get-window-state 301"})
	if err != nil {
		t.Fatal(err)
	}
	want := `list: <ul> "Search results": 2 of 2 rows (complete)
[
{"title":"Result A","url":"https://a.test/a"},
{"title":"Result B","url":"https://a.test/b"}
]
hint: if this is the wrong list, pass --root <index> of the list or of one item, from winuse get-window-state 301
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRenderPartialReasons(t *testing.T) {
	r := Result{Kind: "list", Container: "<div class=\"grid\">", Rows: rows(50), Found: 312, Limit: 50,
		PageSays: &Claim{Total: 1234, Noun: "results"}}
	got, _ := Render(r, Options{})
	head, _, _ := strings.Cut(got, "\n")
	if want := `list: <div class="grid">: 50 of 312 rows (partial: capped at 50 / page says 1,234 results)`; head != want {
		t.Errorf("header %q, want %q", head, want)
	}
	// A page claim at or under what was found is no reason.
	r = Result{Kind: "table", Container: "<table>", Label: "Population", Rows: rows(3), Found: 3, Limit: 50, PageSays: &Claim{Total: 3, Noun: "results"}}
	got, _ = Render(r, Options{})
	if head, _, _ := strings.Cut(got, "\n"); head != `table: <table> "Population": 3 of 3 rows (complete)` {
		t.Errorf("header %q", head)
	}
	if strings.Contains(got, "hint:") {
		t.Errorf("no RootHint, yet a hint: %q", got)
	}
}

func TestRenderNoListAndWall(t *testing.T) {
	if got, _ := Render(Result{}, Options{RootHint: "x"}); got != "no list found\n" {
		t.Errorf("none: %q", got)
	}
	got, _ := Render(Result{Wall: "Cloudflare"}, Options{})
	if got != "no list found: the Page looks like a Cloudflare wall; ask the user to handle it\n" {
		t.Errorf("wall: %q", got)
	}
}

func TestRenderCutsWholeRowsToMaxChars(t *testing.T) {
	r := Result{Kind: "list", Container: "<ol>", Rows: rows(40), Found: 40, Limit: 50}
	got, _ := Render(r, Options{MaxChars: 600})
	if n := len([]rune(got)); n > 600 {
		t.Errorf("%d characters, over the cap", n)
	}
	head, body, _ := strings.Cut(got, "\n")
	var arr []map[string]string
	if err := json.Unmarshal([]byte(body), &arr); err != nil {
		t.Fatalf("the cut body is not a JSON array: %v\n%s", err, body)
	}
	want := "list: <ol>: " + itoa(len(arr)) + " of 40 rows (partial: cut to fit 600 characters; --out <file> writes every row)"
	if head != want {
		t.Errorf("header %q, want %q", head, want)
	}
}

func TestRenderOutWritesTheArrayAndPrintsTheHeader(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rows.json")
	r := Result{Kind: "list", Container: "<ol>", Rows: rows(3), Found: 3, Limit: 50}
	got, err := Render(r, Options{Out: file, MaxChars: 10, RootHint: "winuse get-window-state 7"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	var arr []map[string]string
	if err := json.Unmarshal(data, &arr); err != nil || len(arr) != 3 {
		t.Fatalf("file: %v %q", err, data)
	}
	want := "list: <ol>: 3 of 3 rows (complete)\nrows: " + file + " (3 rows, " + itoa(len([]rune(string(data)))) + " characters)\n" +
		"hint: if this is the wrong list, pass --root <index> of the list or of one item, from winuse get-window-state 7\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
