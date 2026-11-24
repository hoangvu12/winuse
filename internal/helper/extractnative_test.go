package helper

// Offline tests of `winuse extract` on a native Window (ticket 27): the
// table read is a fake answering a recorded-format read
// (internal/extract/testdata/native), so the output is checked end to
// end from the helper; the read itself is internal/uia's, the rows
// internal/extract's. record-tree --table writes that format.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/extract"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// tableTrees is a tree reader with the native table read and name:/id:
// references.
type tableTrees struct {
	fakeTrees
	read   extract.NativeRead
	found  bool
	err    error
	calls  int
	hwnd   uint64
	root   int
	limit  int
	refs   map[string]int
	budget time.Duration
}

func (f *tableTrees) ExtractNative(ctx context.Context, hwnd uint64, root, limit int, budget time.Duration) (extract.NativeRead, bool, error) {
	f.calls++
	f.hwnd, f.root, f.limit, f.budget = hwnd, root, limit, budget
	return f.read, f.found, f.err
}

func (f *tableTrees) ElementPoint(ctx context.Context, hwnd uint64, index int) (uia.Point, error) {
	return uia.Point{}, nil
}

func (f *tableTrees) ResolveElementRef(ctx context.Context, hwnd uint64, ref string) (int, error) {
	if i, ok := f.refs[ref]; ok {
		return i, nil
	}
	return 0, &wstate.NoMatchRefError{Kind: wstate.RefName, Text: strings.TrimPrefix(ref, "name:")}
}

func tableServices(t *testing.T, fixture string) (Services, *tableTrees) {
	t.Helper()
	read, err := extract.LoadNative(filepath.Join("..", "extract", "testdata", "native", fixture+".json"))
	if err != nil {
		t.Fatal(err)
	}
	trees := &tableTrees{fakeTrees: fakeTrees{trees: map[uint64]*wstate.Tree{101: cannedTree()}}, read: read, found: true,
		refs: map[string]int{"name:Items View": 7}}
	s := testServices(trees)
	s.Actions = trees
	return s, trees
}

func TestExtractNativePrintsTheGridsRows(t *testing.T) {
	s, trees := tableServices(t, "wpf-datagrid")
	out, e := runCommand(t, CommandsWith(s), "extract", protocol.ExtractArgs{Window: "101"})
	want := `table: data grid: 3 of 1000 rows (partial: virtualized)
[
{"Id":"0","Name":"Item 0","Price":"0.00","Stock":"0"},
{"Id":"1","Name":"Item 1","Price":"1.50","Stock":"7"},
{"Id":"2","Name":"Item 2","Price":"3.00","Stock":"14"}
]
hint: if this is the wrong table, pass --root <index> of the table or of one item, from winuse get-window-state 101
`
	if e != nil || out != want {
		t.Fatalf("extract: %+v\n%s", e, out)
	}
	if trees.calls != 1 || trees.hwnd != 101 || trees.root != -1 || trees.limit != extract.NativeLimit || trees.budget != s.Budget {
		t.Errorf("read %d times: hwnd %d, root %d, limit %d, budget %s", trees.calls, trees.hwnd, trees.root, trees.limit, trees.budget)
	}
	if trees.fakeTrees.calls != 0 {
		t.Errorf("extract read the Window State %d times", trees.fakeTrees.calls)
	}
}

func TestExtractNativeRootAndLimit(t *testing.T) {
	s, trees := tableServices(t, "explorer-details")
	out, e := runCommand(t, CommandsWith(s), "extract", protocol.ExtractArgs{Window: "101", Root: "4", Limit: 2})
	if e != nil {
		t.Fatal(e.Error())
	}
	if trees.root != 4 || trees.limit != 2 {
		t.Errorf("root %d, limit %d", trees.root, trees.limit)
	}
	if head, _, _ := strings.Cut(out, "\n"); head != `table: list "Items View": 2 of 470 rows (partial: capped at 2)` || strings.Contains(out, "hint:") {
		t.Errorf("--root 4 --limit 2:\n%s", out)
	}
	if _, e := runCommand(t, CommandsWith(s), "extract", protocol.ExtractArgs{Window: "101", Root: "name:Items View"}); e != nil || trees.root != 7 {
		t.Errorf("a name: root resolved to %d: %+v", trees.root, e)
	}
	_, e = runCommandNoWait(t, CommandsWith(s), "extract", protocol.ExtractArgs{Window: "101", Root: "name:Nowhere"})
	if e == nil || !strings.Contains(e.Msg, "Nowhere") {
		t.Errorf("a root that matches nothing: %+v", e)
	}
}

func TestExtractNativeOutAndNoList(t *testing.T) {
	s, trees := tableServices(t, "services")
	file := filepath.Join(t.TempDir(), "rows.json")
	out, e := runCommand(t, CommandsWith(s), "extract", protocol.ExtractArgs{Window: "101", Out: file})
	if e != nil {
		t.Fatal(e.Error())
	}
	data, err := os.ReadFile(file)
	var rows []map[string]string
	if err != nil || json.Unmarshal(data, &rows) != nil || len(rows) != 3 || rows[2]["Name"] != "Windows Update" {
		t.Fatalf("file %q %v", data, err)
	}
	if !strings.HasPrefix(out, "table: list: 3 of 3 rows (complete)\nrows: "+file+" (3 rows, ") {
		t.Errorf("printed %q", out)
	}

	trees.found = false
	if out, e := runCommand(t, CommandsWith(s), "extract", protocol.ExtractArgs{Window: "101"}); e != nil || out != "no list found\n" {
		t.Errorf("no list: %q %+v", out, e)
	}
}

func TestExtractNativeRefusals(t *testing.T) {
	s, trees := tableServices(t, "services")
	if _, e := runCommand(t, CommandsWith(s), "extract", protocol.ExtractArgs{Window: "101", As: "jsonld"}); e == nil || !strings.Contains(e.Msg, "--as works on a browser Page only") {
		t.Errorf("--as on a native Window: %+v", e)
	}
	trees.err = uia.ErrStale
	_, e := runCommand(t, CommandsWith(s), "extract", protocol.ExtractArgs{Window: "101", Root: "9"})
	if e == nil || !strings.Contains(e.Msg, "element 9 is not available") {
		t.Errorf("a stale root: %+v", e)
	}
	trees.err = uia.ErrWindowGone
	if _, e := runCommand(t, CommandsWith(s), "extract", protocol.ExtractArgs{Window: "101"}); e == nil {
		t.Error("a failed read printed rows")
	}
	if trees.calls != 2 {
		t.Errorf("--as was refused after a read: %d reads", trees.calls)
	}
}

// record-tree --table records the table read, in the fixture format the
// tests above load.
func TestRecordTreeTable(t *testing.T) {
	s, trees := tableServices(t, "net8-listview")
	file := filepath.Join(t.TempDir(), "grid.json")
	out, e := runCommand(t, CommandsWith(s), "record-tree", recordArgs{Window: "101", File: file, Table: true})
	if e != nil {
		t.Fatal(e.Error())
	}
	if out != "recorded "+file+": list \"listView1\", 4 children\n" {
		t.Errorf("printed %q", out)
	}
	got, err := extract.LoadNative(file)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != `recorded by winuse record-tree --table from "Untitled - Notepad" (Window 101)` || got.Captured == nil ||
		len(got.Children) != 4 || got.Headers[3] != "Owner" {
		t.Errorf("recorded %+v", got)
	}
	if trees.limit != extract.NativeLimit || trees.root != -1 || trees.fakeTrees.calls != 0 {
		t.Errorf("record-tree --table read root %d, limit %d, and %d trees", trees.root, trees.limit, trees.fakeTrees.calls)
	}
	trees.found = false
	if _, e := runCommand(t, CommandsWith(s), "record-tree", recordArgs{Window: "101", File: file, Table: true}); e == nil || !strings.Contains(e.Msg, "no list or grid") {
		t.Errorf("no table: %+v", e)
	}
}
