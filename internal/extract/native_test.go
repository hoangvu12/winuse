package extract

// Native lists and grids (ticket 27): a bounded read of a container's
// rows (NativeRead, the fixtures in testdata/native) turned into the
// shared Result and printed. The fixtures follow ticket 13's probe of
// Explorer, Services, a .NET 8 ListView, a WPF DataGrid and a .NET
// Framework DataGridView; `winuse record-tree <window> <file> --table`
// records real ones in the same format.

import (
	"strings"
	"testing"
)

func nativeFixture(t *testing.T, name string) NativeRead {
	t.Helper()
	n, err := LoadNative("testdata/native/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// printed is what extract prints for n read with limit, no hint, no cap.
func printed(t *testing.T, n NativeRead, limit int) string {
	t.Helper()
	out, err := Render(FromNative(n, limit), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNativeExplorerDetailsIsVirtualized(t *testing.T) {
	got := printed(t, nativeFixture(t, "explorer-details"), 500)
	want := `table: list "Items View": 3 of 470 rows (partial: virtualized)
[
{"Name":"Documents","Date modified":"11/20/2026 9:14 AM","Type":"File folder","Size":""},
{"Name":"notes.txt","Date modified":"11/21/2026 8:02 PM","Type":"Text Document","Size":"2 KB"},
{"Name":"photo.jpg","Date modified":"11/22/2026 7:45 AM","Type":"JPG File","Size":"1,204 KB"}
]
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestNativeServicesPlacesCellsByPosition(t *testing.T) {
	got := printed(t, nativeFixture(t, "services"), 500)
	want := `table: list: 3 of 3 rows (complete)
[
{"Name":"Application Information","Description":"Facilitates the running of interactive applications with additional administrative privileges.","Status":"Running","Startup Type":"Manual (Trigger Start)","Log On As":"Local System"},
{"Name":"Background Intelligent Transfer Service","Description":"Transfers files in the background using idle network bandwidth.","Status":"","Startup Type":"Manual","Log On As":"Local System"},
{"Name":"Windows Update","Description":"Enables the detection, download, and installation of updates for Windows and other programs.","Status":"Running","Startup Type":"Manual (Trigger Start)","Log On As":"Local System"}
]
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestNativeNet8ListViewDropsANameThatRepeatsItsColumn(t *testing.T) {
	got := printed(t, nativeFixture(t, "net8-listview"), 500)
	want := `table: list "listView1": 3 of 3 rows (complete)
[
{"Id":"1","Name":"Widget","Price":"9.99","Owner":"Ada"},
{"Id":"2","Name":"Gadget","Price":"24.50","Owner":""},
{"Id":"3","Name":"Gizmo","Price":"5.00","Owner":"Grace"}
]
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestNativeWPFDataGridReadsCellTextAndDropsRowHeaders(t *testing.T) {
	n := nativeFixture(t, "wpf-datagrid")
	got := printed(t, n, 500)
	want := `table: data grid: 3 of 1000 rows (partial: virtualized)
[
{"Id":"0","Name":"Item 0","Price":"0.00","Stock":"0"},
{"Id":"1","Name":"Item 1","Price":"1.50","Stock":"7"},
{"Id":"2","Name":"Item 2","Price":"3.00","Stock":"14"}
]
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Under the limit the rows read are capped, not virtualized.
	head, _, _ := strings.Cut(printed(t, n, 2), "\n")
	if head != `table: data grid: 2 of 1000 rows (partial: capped at 2)` {
		t.Errorf("limit 2: %q", head)
	}
}

func TestNativeDataGridViewFallsBackToItsHeaderRow(t *testing.T) {
	got := printed(t, nativeFixture(t, "netfx-datagridview"), 500)
	want := `table: table "dataGridView1": 2 of 2 rows (complete)
[
{"Id":"0","Name":"Item 0","Price":"0.00","Stock":""},
{"Id":"1","Name":"Item 1","Price":"1.50","Stock":"7"}
]
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// listOf is a plain list of n items named "Item 1" to "Item n", as a
// list box shows them.
func listOf(n int) NativeRead {
	r := NativeRead{Role: "list", Name: "Item List"}
	for i := 1; i <= n; i++ {
		r.Children = append(r.Children, Node{Role: "list item", Name: "Item " + itoa(i)})
	}
	return r
}

func TestNativePlainListPrintsNames(t *testing.T) {
	got := printed(t, listOf(3), 500)
	want := `list: list "Item List": 3 of 3 rows (complete)
[
{"name":"Item 1"},
{"name":"Item 2"},
{"name":"Item 3"}
]
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The live read stops one row past the limit; the total is the set size
// the last row gives, or else at least what was read.
func TestNativeLimitAndTotals(t *testing.T) {
	n := listOf(6)
	head, _, _ := strings.Cut(printed(t, n, 5), "\n")
	if head != `list: list "Item List": 5 of 6+ rows (partial: capped at 5)` {
		t.Errorf("no total: %q", head)
	}
	n.SetSize = 20000
	head, _, _ = strings.Cut(printed(t, n, 5), "\n")
	if head != `list: list "Item List": 5 of 20000 rows (partial: capped at 5)` {
		t.Errorf("set size: %q", head)
	}
	n = listOf(4)
	n.Stopped = "stopped at the time limit"
	head, _, _ = strings.Cut(printed(t, n, 5), "\n")
	if head != `list: list "Item List": 4 of 4+ rows (partial: stopped at the time limit)` {
		t.Errorf("stopped: %q", head)
	}
	head, _, _ = strings.Cut(printed(t, listOf(4), 5), "\n")
	if head != `list: list "Item List": 4 of 4 rows (complete)` {
		t.Errorf("complete: %q", head)
	}
	n = listOf(4)
	n.Items, n.ItemsCut = 9000, true
	head, _, _ = strings.Cut(printed(t, n, 5), "\n")
	if head != `list: list "Item List": 4 of 9000+ rows (partial: virtualized)` {
		t.Errorf("items counted in part: %q", head)
	}
}

// A grid whose RowCount counts its header row too is complete when every
// other row was read.
func TestNativeRowCountWithTheHeaderRow(t *testing.T) {
	n := nativeFixture(t, "services")
	n.RowCount = 4
	head, _, _ := strings.Cut(printed(t, n, 500), "\n")
	if head != `table: list: 3 of 3 rows (complete)` {
		t.Errorf("header %q", head)
	}
}

// Rows in groups (Explorer's Group by) are rows; empty and repeated header
// names get their own keys.
func TestNativeGroupsAndHeaderKeys(t *testing.T) {
	v := func(s string) *string { return &s }
	c := func(i int) *int { return &i }
	n := NativeRead{Role: "list", Table: true, Headers: []string{" Name", "", "Name"}, Children: []Node{
		{Role: "group", Name: "Today", Children: []Node{
			{Role: "list item", Name: "a", Children: []Node{
				{Role: "edit", Value: v("a"), Column: c(0)}, {Role: "edit", Value: v("x"), Column: c(1)}, {Role: "edit", Value: v("y"), Column: c(2)},
			}},
		}},
		{Role: "list item", Name: "b", Children: []Node{
			{Role: "edit", Value: v("b"), Column: c(0)}, {Role: "edit", Value: v("z"), Column: c(3)},
		}},
	}}
	got := printed(t, n, 500)
	want := `table: list: 2 of 2 rows (complete)
[
{"Name":"a","column 2":"x","Name 2":"y","column 4":""},
{"Name":"b","column 2":"","Name 2":"","column 4":"z"}
]
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestNativeRowsCountsDataRowsOnly(t *testing.T) {
	cases := []struct {
		n    Node
		want int
	}{
		{Node{Role: "list item", Name: "a"}, 1},
		{Node{Role: "data item"}, 1},
		{Node{Role: "header", Children: []Node{{Role: "header item", Name: "Id"}}}, 0},
		{Node{Role: "custom", Name: "Top Row", Children: []Node{{Role: "header item", Name: "Id"}}}, 0},
		{Node{Role: "scroll bar"}, 0},
		{Node{Role: "group", Children: []Node{{Role: "list item"}, {Role: "list item"}}}, 2},
	}
	for _, c := range cases {
		if got := Rows(c.n); got != c.want {
			t.Errorf("Rows(%+v) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestPickContainer(t *testing.T) {
	cases := []struct {
		name string
		c    []Candidate
		want int
	}{
		{"none", nil, -1},
		{"the only grid wins over a longer list", []Candidate{{Role: "list", Rows: 900}, {Role: "list", Table: true, Rows: 20}}, 1},
		{"no grid: the longest list", []Candidate{{Role: "list", Rows: 3}, {Role: "list", Rows: 8}, {Role: "list", Rows: 8}}, 1},
		{"two grids: the longest of all", []Candidate{{Role: "data grid", Table: true, Rows: 5}, {Role: "list", Rows: 50}, {Role: "data grid", Table: true, Rows: 40}}, 1},
		{"a count that did not come back is a long list", []Candidate{{Role: "list", Rows: 300}, {Role: "list", Rows: -1}}, 1},
	}
	for _, c := range cases {
		if got := PickContainer(c.c); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}
