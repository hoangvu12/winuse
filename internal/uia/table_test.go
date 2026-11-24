package uia

// extract on native lists and grids (ticket 27), the parts that need no
// COM: detection reads the Window's tree but never below a list or grid,
// and the rows are read one at a time, stopping one data row past the
// limit (an unbounded read crashed Event Viewer at 22,915 rows, ticket
// 13), or at the time limit.

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/extract"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// fakeIsBox tells a list or grid by its control type, as comIsBox does by
// the Grid and Table patterns too.
func fakeIsBox(n *fakeNode) bool {
	switch n.data.ControlType {
	case wstate.ControlTypeList, wstate.ControlTypeDataGrid, wstate.ControlTypeTable:
		return true
	}
	return false
}

func TestFindBoxesNeverReadsBelowAList(t *testing.T) {
	big := hugeList(20000)
	drop := node(wstate.ControlTypeList, "drop-down", node(wstate.ControlTypeListItem, "Red"), node(wstate.ControlTypeListItem, "Green"))
	grid := node(wstate.ControlTypeDataGrid, "Orders", node(wstate.ControlTypeDataItem, "1"))
	root := node(wstate.ControlTypeWindow, "App",
		node(wstate.ControlTypePane, "Body", big, node(wstate.ControlTypeComboBox, "Color", drop), grid),
		node(wstate.ControlTypeButton, "OK"))
	src := newFakeSource(root)
	res, boxes, err := findBoxes[*fakeNode](src, fakeIsBox, WalkConfig{Now: src.now})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range src.expanded {
		if fakeIsBox(n) {
			t.Errorf("detection read the children of the %s %q", wstate.RoleName(n.data.ControlType), n.data.Name)
		}
	}
	var got []string
	for _, i := range boxes {
		got = append(got, res.Elements[i].Name)
	}
	// A combo box's drop-down list is no candidate.
	if want := []string{big.data.Name, "Orders"}; !slices.Equal(got, want) {
		t.Errorf("boxes %q, want %q", got, want)
	}
	if res.Elements[len(res.Elements)-1].Name != "OK" {
		t.Errorf("detection did not go on past the lists: %+v", res.Elements)
	}
}

// fakeRows steps through a container's children; every step costs step on
// the fake clock.
type fakeRows struct {
	kids     []extract.Node
	steps    int
	released int
	fail     int // the step (1-based) that fails, 0: none
	failErr  error
	elapsed  time.Duration
	step     time.Duration
	base     time.Time
}

func (f *fakeRows) now() time.Time { return f.base.Add(f.elapsed) }

func (f *fakeRows) row(i int) (int, extract.Node, bool, error) {
	f.steps++
	f.elapsed += f.step
	if f.fail > 0 && f.steps == f.fail {
		return 0, extract.Node{}, false, f.failErr
	}
	if i >= len(f.kids) {
		return 0, extract.Node{}, false, nil
	}
	return i, f.kids[i], true, nil
}

func (f *fakeRows) firstRow(box int, _ time.Duration) (int, extract.Node, bool, error) {
	return f.row(0)
}

func (f *fakeRows) nextRow(prev int, _ time.Duration) (int, extract.Node, bool, error) {
	return f.row(prev + 1)
}

func (f *fakeRows) release(int) { f.released++ }

func listRows(header bool, n int) []extract.Node {
	var kids []extract.Node
	if header {
		kids = append(kids, extract.Node{Role: "header", Children: []extract.Node{{Role: "header item", Name: "Name"}}})
	}
	for i := 0; i < n; i++ {
		kids = append(kids, extract.Node{Role: "list item", Name: "Item"})
	}
	return append(kids, extract.Node{Role: "scroll bar"})
}

func TestStepRowsStopsOneRowPastTheLimit(t *testing.T) {
	f := &fakeRows{kids: listRows(true, 22915), base: time.Now()}
	got, last, ok := stepRows[int](f, 0, 500, f.base.Add(time.Hour), f.now)
	if !ok || got.rows != 501 || len(got.children) != 502 || got.stopped != "" {
		t.Fatalf("rows %d, children %d, stopped %q", got.rows, len(got.children), got.stopped)
	}
	if f.steps != 502 {
		t.Errorf("%d steps for 501 rows and the header", f.steps)
	}
	if last != 501 || f.released != 501 {
		t.Errorf("last %d, released %d: every row but the last is released, the last is the caller's", last, f.released)
	}
}

func TestStepRowsReadsAShortListWhole(t *testing.T) {
	f := &fakeRows{kids: listRows(false, 8), base: time.Now()}
	got, _, _ := stepRows[int](f, 0, 500, f.base.Add(time.Hour), f.now)
	if got.rows != 8 || len(got.children) != 9 || got.stopped != "" {
		t.Errorf("rows %d, children %d, stopped %q", got.rows, len(got.children), got.stopped)
	}
}

func TestStepRowsStopsAtTheTimeLimit(t *testing.T) {
	f := &fakeRows{kids: listRows(false, 1000), base: time.Now(), step: 10 * time.Millisecond}
	got, _, _ := stepRows[int](f, 0, 500, f.base.Add(time.Second), f.now)
	if got.rows != 100 || got.stopped != stoppedTime {
		t.Errorf("rows %d, stopped %q", got.rows, got.stopped)
	}
}

func TestStepRowsStopsWhereARowDoesNotRead(t *testing.T) {
	f := &fakeRows{kids: listRows(false, 50), base: time.Now(), fail: 11, failErr: errors.New("updating the element cache failed (HRESULT 0x80004005)")}
	got, _, _ := stepRows[int](f, 0, 500, f.base.Add(time.Hour), f.now)
	if got.rows != 10 || got.stopped != stoppedUnread {
		t.Errorf("rows %d, stopped %q", got.rows, got.stopped)
	}
	f = &fakeRows{kids: listRows(false, 50), base: time.Now(), fail: 3, failErr: ErrChunkTimedOut}
	if got, _, _ := stepRows[int](f, 0, 500, f.base.Add(time.Hour), f.now); got.stopped != stoppedTime {
		t.Errorf("a step that timed out: stopped %q", got.stopped)
	}
}
