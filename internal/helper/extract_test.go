package helper

// Offline tests of `winuse extract` (ticket 26) over the fake launched
// Chrome of pageonly_test.go: the Page's answer printed as a header and a
// JSON array, extract.js run in winuse's isolated world (never the Page's
// own, never with Runtime.enable), the refusals of bad arguments and of a
// Window that is no browser, and --out. extract.js itself is checked on
// real pages by internal/page's golden cases.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// extractRig is window 301 whose Page answers extract.js with reply.
func extractRig(t *testing.T, reply map[string]any) *winRig {
	t.Helper()
	r := newWinRig(t)
	r.browser.Srv.Handle("Page.createIsolatedWorld", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.note(m.Method, m.Params)
		return map[string]any{"executionContextId": 7}, nil
	})
	r.evalReply = map[string]any{"result": map[string]any{"type": "object", "value": reply}}
	return r
}

func twoRows() map[string]any {
	return map[string]any{"kind": "list", "container": "<ol>", "label": "Results", "found": 2,
		"rows":      []any{map[string]any{"title": "A", "url": "https://a.test/"}, map[string]any{"title": "B", "url": "https://b.test/"}},
		"page_says": map[string]any{"total": 1234, "noun": "results"}, "wall": ""}
}

func TestExtractPrintsTheRowsReadInAnIsolatedWorld(t *testing.T) {
	r := extractRig(t, twoRows())
	out, e := r.run(t, "extract", protocol.ExtractArgs{Window: "301"})
	want := `list: <ol> "Results": 2 of 2 rows (partial: page says 1,234 results)
[
{"title":"A","url":"https://a.test/"},
{"title":"B","url":"https://b.test/"}
]
hint: if this is the wrong list, pass --root <index> of the list or of one item, from winuse get-window-state 301
`
	if e != nil || out != want {
		t.Fatalf("extract: %+v\n%s", e, out)
	}
	worlds := r.sent("Page.createIsolatedWorld")
	if len(worlds) != 1 || !strings.Contains(worlds[0], `"worldName":"winuse-extract"`) || !strings.Contains(worlds[0], `"frameId":"F1"`) {
		t.Errorf("isolated worlds %v", worlds)
	}
	ran := 0
	for _, p := range r.sent("eval") {
		if strings.Contains(p, `"contextId":7`) {
			ran++
			var ev struct {
				Expression string `json:"expression"`
			}
			json.Unmarshal([]byte(p), &ev)
			if !strings.Contains(ev.Expression, `"limit":50`) || !strings.Contains(ev.Expression, "Apache License 2.0") {
				t.Errorf("the expression is not extract.js with the default limit: %.200s", ev.Expression)
			}
		}
	}
	if ran != 1 {
		t.Errorf("extract.js ran %d times in the isolated world; sent %v", ran, r.sent("eval"))
	}
	for _, m := range r.browser.Methods() {
		if m == "Runtime.enable" {
			t.Error("Runtime.enable was sent")
		}
	}
	if len(r.cursor.labels) != 0 {
		t.Errorf("extract showed the Agent Cursor: %v", r.cursor.labels)
	}
	r.passiveOnly(t)
}

// A wall extract names is the response's wall field too (--json), as
// read's and navigate's are.
func TestExtractRecordsTheWall(t *testing.T) {
	r := extractRig(t, map[string]any{"kind": "", "rows": []any{}, "found": 0, "wall": "DataDome"})
	out, e, f := runFields(t, r.cmds, "extract", protocol.ExtractArgs{Window: "301"})
	if e != nil || f.Wall != "DataDome" || !strings.Contains(out, "DataDome wall") {
		t.Errorf("extract of a wall: %q %+v %+v", out, f, e)
	}
}

// extract.js stops counting at its item ceiling and says so (at_least):
// the count prints as a lower bound.
func TestExtractCountsPastTheCeilingAsALowerBound(t *testing.T) {
	reply := twoRows()
	reply["found"], reply["at_least"], reply["page_says"] = 2000, true, nil
	r := extractRig(t, reply)
	out, e := r.run(t, "extract", protocol.ExtractArgs{Window: "301", Limit: 2})
	if e != nil || !strings.HasPrefix(out, `list: <ol> "Results": 2 of 2000+ rows (partial: capped at 2)`) {
		t.Errorf("extract past the ceiling: %q %+v", out, e)
	}
}

// TestBlockNoteOnEveryReadOfTheTab (bladebro-ideas 28): extract on a
// blocking Tab ends with the note, and so does a read through the Window ID
// of a Window that shows a blocking Tab (get-window-state, read, extract).
func TestBlockNoteOnEveryReadOfTheTab(t *testing.T) {
	r := extractRig(t, twoRows())
	(&readPage{md: map[string]any{"markdown": "# Docs", "main": "main", "title": "Docs", "url": "https://docs.test/"}}).handle(r.browser.Srv, func(string) any { return nil })
	r.svc.assetBlocks = newAssetBlocks()
	r.cmds = CommandsWith(r.svc)
	r.svc.assetBlocks.set(tDocs, []string{"images"}) // a Tab of a browser winuse launched may block
	const note = "Note: images are blocked in this Tab\n"
	for _, tc := range []struct {
		cmd  string
		args any
	}{
		{"extract", protocol.ExtractArgs{Window: "301:" + page.TabID(tDocs)}},
		{"get-window-state", protocol.GetWindowArgs{Window: "301"}},
		{"read", protocol.ReadArgs{Window: "301"}},
		{"extract", protocol.ExtractArgs{Window: "301"}},
	} {
		if out, e := r.run(t, tc.cmd, tc.args); e != nil || !strings.HasSuffix(out, note) || strings.Count(out, note) != 1 {
			t.Errorf("%s %+v: %q %+v", tc.cmd, tc.args, out, e)
		}
	}
	r.svc.assetBlocks.set(tDocs, nil)
	if out, e := r.run(t, "extract", protocol.ExtractArgs{Window: "301"}); e != nil || strings.Contains(out, "blocked") {
		t.Errorf("a Window whose Tab blocks nothing: %q %+v", out, e)
	}
}

func TestExtractNoListAndOptions(t *testing.T) {
	r := extractRig(t, map[string]any{"kind": "", "rows": []any{}, "found": 0, "wall": ""})
	if out, e := r.run(t, "extract", protocol.ExtractArgs{Window: "301", As: "table", Limit: 5}); e != nil || out != "no list found\n" {
		t.Fatalf("no list: %q %+v", out, e)
	}
	found := false
	for _, p := range r.sent("eval") {
		found = found || strings.Contains(p, `{\"as\":\"table\",\"limit\":5}`)
	}
	if !found {
		t.Errorf("--as and --limit did not reach extract.js: %v", r.sent("eval"))
	}
}

func TestExtractOutWritesTheArray(t *testing.T) {
	r := extractRig(t, twoRows())
	file := filepath.Join(t.TempDir(), "rows.json")
	out, e := r.run(t, "extract", protocol.ExtractArgs{Window: "301", Out: file})
	if e != nil {
		t.Fatal(e.Error())
	}
	data, err := os.ReadFile(file)
	var rows []map[string]string
	if err != nil || json.Unmarshal(data, &rows) != nil || len(rows) != 2 || rows[1]["title"] != "B" {
		t.Fatalf("file %q %v", data, err)
	}
	if !strings.Contains(out, "rows: "+file+" (2 rows, ") || strings.Contains(out, `"title"`) {
		t.Errorf("printed %q", out)
	}
}

func TestExtractRefusals(t *testing.T) {
	r := extractRig(t, twoRows())
	two := 2000
	cases := []struct {
		a    protocol.ExtractArgs
		want string
	}{
		{protocol.ExtractArgs{Window: "301", As: "grid"}, "--as takes list, table or jsonld"},
		{protocol.ExtractArgs{Window: "301", Limit: two}, "--limit takes 1 to 1000 rows"},
		{protocol.ExtractArgs{Window: "301", MaxChars: &two, Out: `C:\x.json`}, "pass --out or --max-chars"},
		{protocol.ExtractArgs{}, "a window reference is required"},
		// A native Window goes to the UIA client's table read
		// (extractnative_test.go); this rig's tree reader has none.
		{protocol.ExtractArgs{Window: "101"}, "extract is unavailable: no UIA client"},
		{protocol.ExtractArgs{Window: "301", Root: "list"}, "is not an Element Index, name: or"},
	}
	for _, c := range cases {
		_, e := r.run(t, "extract", c.a)
		if e == nil || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%+v: %+v, want %q", c.a, e, c.want)
		}
	}
	// No Window State holds a Page Element 3 yet.
	_, e := r.run(t, "extract", protocol.ExtractArgs{Window: "301", Root: "3"})
	if e == nil || !strings.Contains(e.Msg, "--root 3 is not an Element of the Page") {
		t.Errorf("a root that is no Page Element: %+v", e)
	}
}
