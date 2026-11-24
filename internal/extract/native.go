package extract

// The native backend (ticket 27): a list or grid of a native Window, read
// through UI Automation by internal/uia as one bounded read of its rows
// (NativeRead), turned into the shared Result here. Ticket 13's findings
// shape it:
//
//   - Table and Grid only detect the table and give its headers and
//     RowCount; GridPattern.GetItem is never used (broken in Services and
//     .NET 8, and it scrolls a WPF grid).
//   - Cells are placed by their GridItem column, or else by position;
//     header rows and header cells are left out.
//   - A cell's text is its Value, its Name only when it has no Value
//     pattern, and never a Name that only repeats its column (Explorer's
//     empty Size); U+200E and U+200F are stripped.
//   - Without the patterns (a .NET Framework DataGridView), the headers
//     come from a header row or a header Element.
//   - A virtualized list (Explorer, WPF) holds only the rows on screen:
//     RowCount, the ItemContainer count or the set size gives the total,
//     and the header says "(partial: virtualized)".

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// NativeLimit is --limit's default on a native Window.
const NativeLimit = 500

// NativeFormat is the version of a recorded NativeRead fixture
// (`winuse record-tree <window> <file> --table`); LoadNative refuses
// others.
const NativeFormat = 1

// NativeRead is one bounded read of a native list or grid: the container,
// what its patterns say, and its children with their subtrees, in order,
// up to one data row past the limit. It doubles as the fixture format.
type NativeRead struct {
	Format   int        `json:"format,omitempty"`
	Source   string     `json:"source,omitempty"`
	Captured *time.Time `json:"captured,omitempty"`

	// Role and Name are the container's (wstate.RoleName's English role).
	Role string `json:"role"`
	Name string `json:"name,omitempty"`
	// Table says it has the Table or Grid pattern.
	Table bool `json:"table,omitempty"`
	// RowCount is the Grid pattern's RowCount (0: none).
	RowCount int `json:"rowCount,omitempty"`
	// Headers are the names of the Table pattern's column headers, as
	// read (nil: none).
	Headers []string `json:"headers,omitempty"`
	// Items is how many items the ItemContainer pattern counted (0: not
	// counted); ItemsCut says the count stopped before the end.
	Items    int  `json:"items,omitempty"`
	ItemsCut bool `json:"itemsCut,omitempty"`
	// SetSize is the SizeOfSet the last row read gives (0: none), the total
	// of a list read up to the limit.
	SetSize int `json:"setSize,omitempty"`
	// Children are the container's children read, in order, each with its
	// subtree.
	Children []Node `json:"children,omitempty"`
	// Stopped is why the read stopped before the end or the limit ("" for
	// none), printed as a partial reason.
	Stopped string `json:"stopped,omitempty"`
}

// Node is one Element of the read.
type Node struct {
	Role string `json:"role"`
	Name string `json:"name,omitempty"`
	// Value is the Value pattern's value; nil when it has no Value pattern.
	Value *string `json:"value,omitempty"`
	// Column is the GridItem column; nil when it has no GridItem pattern.
	Column   *int   `json:"column,omitempty"`
	Children []Node `json:"children,omitempty"`
}

// LoadNative reads a NativeRead fixture file.
func LoadNative(path string) (NativeRead, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return NativeRead{}, err
	}
	var n NativeRead
	if err := json.Unmarshal(data, &n); err != nil {
		return NativeRead{}, fmt.Errorf("%s: %v", path, err)
	}
	if n.Format != NativeFormat {
		return NativeRead{}, fmt.Errorf("%s: format %d, want %d", path, n.Format, NativeFormat)
	}
	return n, nil
}

// The roles the read tells rows, headers and groups by (wstate.RoleName).
const (
	roleHeader     = "header"
	roleHeaderItem = "header item"
	roleGroup      = "group"
	roleImage      = "image"
)

// rowRoles are the roles a row has: DataItem, ListItem, Custom
// (DataGridView), TreeItem (tree lists).
var rowRoles = map[string]bool{"list item": true, "data item": true, "custom": true, "tree item": true}

// Candidate is one list or grid of the Window, as detection saw it.
type Candidate struct {
	Role  string
	Name  string
	Table bool // the Table or Grid pattern
	// Rows is its RowCount, or how many items it holds; -1 when the count
	// did not come back in time (a long list).
	Rows int
}

// PickContainer is the candidate extract reads without --root: the only
// Table or Grid, or else the one with the most rows (the first of equals);
// -1 when there is none.
func PickContainer(c []Candidate) int {
	grids, only := 0, -1
	for i, x := range c {
		if x.Table {
			grids++
			only = i
		}
	}
	if grids == 1 {
		return only
	}
	size := func(x Candidate) int {
		if x.Rows < 0 {
			return math.MaxInt
		}
		return x.Rows
	}
	best := -1
	for i, x := range c {
		if best < 0 || size(x) > size(c[best]) {
			best = i
		}
	}
	return best
}

// Rows is how many data rows a child of the container holds: 1 for a row,
// those of its rows for a group, 0 for a header, a header row or anything
// else (scroll bars). The live read counts with it to stop past the limit.
func Rows(n Node) int {
	switch {
	case n.Role == roleGroup:
		sum := 0
		for _, c := range n.Children {
			sum += Rows(c)
		}
		return sum
	case !rowRoles[n.Role] || headerRow(n):
		return 0
	}
	return 1
}

// headerRow says a row holds only header cells (a DataGridView's top row).
func headerRow(n Node) bool {
	if len(n.Children) == 0 {
		return false
	}
	for _, c := range n.Children {
		if c.Role != roleHeaderItem && c.Role != roleHeader {
			return false
		}
	}
	return true
}

// FromNative is the Result of a read with limit (0: NativeLimit).
func FromNative(n NativeRead, limit int) Result {
	if limit <= 0 {
		limit = NativeLimit
	}
	var rows []Node
	var treeHeads []string
	headerSeen := gather(n.Children, &rows, &treeHeads)
	heads := names(n.Headers)
	if len(heads) == 0 {
		heads = treeHeads
	}

	placed := make([]map[int]string, len(rows))
	cols, multi := len(heads), false
	for i, r := range rows {
		placed[i] = cells(r, heads)
		if len(placed[i]) > 1 {
			multi = true
		}
		for c := range placed[i] {
			cols = max(cols, c+1)
		}
	}

	res := Result{Kind: KindList, Container: n.Role, Label: clean(n.Name), Limit: limit, Stopped: n.Stopped}
	read := len(rows)
	shown := min(read, limit)
	if len(heads) > 0 || multi {
		res.Kind = KindTable
		keys := keyNames(heads, max(cols, 1))
		for i := range shown {
			if len(placed[i]) == 0 {
				placed[i] = map[int]string{0: rowText(rows[i])}
			}
			res.Rows = append(res.Rows, object(keys, placed[i]))
		}
	} else {
		for i := range shown {
			text := rowText(rows[i])
			if text == "" {
				for _, t := range placed[i] {
					text = t
				}
			}
			res.Rows = append(res.Rows, object([]string{"name"}, map[int]string{0: text}))
		}
	}

	capped := read > limit
	found := read
	for _, k := range []int{n.RowCount, n.Items, n.SetSize} {
		found = max(found, k)
	}
	if !capped && n.Stopped == "" && n.Items == 0 && n.SetSize == 0 && headerSeen && n.RowCount == read+1 {
		found = read // the RowCount counted the header row
	}
	res.Found = found
	switch {
	case found == read && (capped || n.Stopped != ""):
		res.AtLeast = true // nothing says how many follow
	case n.ItemsCut && found == n.Items:
		res.AtLeast = true
	}
	res.Virtualized = !capped && n.Stopped == "" && found > read
	return res
}

// gather collects the data rows of children, groups flattened, and the
// first header names a header Element or a header row gives; it reports
// whether it saw a header.
func gather(children []Node, rows *[]Node, heads *[]string) bool {
	seen := false
	for _, c := range children {
		switch {
		case c.Role == roleHeader || rowRoles[c.Role] && headerRow(c):
			seen = true
			if len(*heads) == 0 {
				for _, h := range c.Children {
					*heads = append(*heads, clean(h.Name))
				}
			}
		case c.Role == roleGroup:
			seen = gather(c.Children, rows, heads) || seen
		case rowRoles[c.Role]:
			*rows = append(*rows, c)
		}
	}
	return seen
}

// cells places a row's cells by column: by GridItem column when any cell
// has one (other children, an icon, are left out), else by position
// (empty images left out). Header cells (a row header) are never cells.
func cells(r Node, heads []string) map[int]string {
	var cs []Node
	gridded := false
	for _, c := range r.Children {
		if c.Role == roleHeader || c.Role == roleHeaderItem {
			continue
		}
		cs = append(cs, c)
		gridded = gridded || c.Column != nil
	}
	out := map[int]string{}
	i := 0
	for _, c := range cs {
		col := i
		if gridded {
			if c.Column == nil || *c.Column < 0 {
				continue
			}
			col = *c.Column
		} else if c.Role == roleImage && cellText(c, "") == "" {
			continue
		}
		i++
		if _, taken := out[col]; !taken {
			out[col] = cellText(c, headAt(heads, col))
		}
	}
	return out
}

func headAt(heads []string, i int) string {
	if i < len(heads) {
		return heads[i]
	}
	return ""
}

// cellText is a cell's text: its Value when it has the Value pattern,
// else its Name unless that only repeats its column; failing both, the
// text of its children (a WPF cell holds a text Element).
func cellText(n Node, column string) string {
	if n.Value != nil {
		if t := clean(*n.Value); t != "" {
			return t
		}
		return childText(n)
	}
	if t := clean(n.Name); t != "" && t != column {
		return t
	}
	return childText(n)
}

func childText(n Node) string {
	var parts []string
	for _, c := range n.Children {
		if t := cellText(c, ""); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// rowText is a list row's own text: its Value, else its Name.
func rowText(r Node) string {
	if r.Value != nil {
		if t := clean(*r.Value); t != "" {
			return t
		}
	}
	return clean(r.Name)
}

// clean strips the direction marks (Explorer's dates) and trims.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '‎' || r == '‏' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func names(in []string) []string {
	var out []string
	for _, s := range in {
		out = append(out, clean(s))
	}
	return out
}

// keyNames are the row keys for cols columns: the header names, "column
// N" for a column with no name, and "Name 2" for a repeated one.
func keyNames(heads []string, cols int) []string {
	used := map[string]bool{}
	keys := make([]string, cols)
	for i := range keys {
		k := headAt(heads, i)
		if k == "" {
			k = "column " + strconv.Itoa(i+1)
		}
		for base, n := k, 2; used[k]; n++ {
			k = base + " " + strconv.Itoa(n)
		}
		used[k] = true
		keys[i] = k
	}
	return keys
}

// object writes one row as a JSON object, its keys in column order.
func object(keys []string, cells map[int]string) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsonString(k))
		b.WriteByte(':')
		b.WriteString(jsonString(cells[i]))
	}
	b.WriteByte('}')
	return json.RawMessage(b.Bytes())
}

// jsonString is s as a JSON string, < > & left as they are (as the Page
// backend's JSON.stringify writes them).
func jsonString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
