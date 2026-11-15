package page

import (
	"encoding/json"
	"reflect"
	"testing"
)

func axValue(s string) *AXValue {
	raw, _ := json.Marshal(s)
	return &AXValue{Type: "string", Value: raw}
}

// TestOffscreenOfListsBoxesWhollyOutsideTheViewport: a viewport scrolled
// to y 1000, 800 px high; boxes above, inside, overlapping its edge, below,
// to the right, empty and without a layout entry.
func TestOffscreenOfListsBoxesWhollyOutsideTheViewport(t *testing.T) {
	var d snapDoc
	d.Nodes.BackendNodeID = []int64{10, 11, 12, 13, 14, 15, 16}
	d.Layout.NodeIndex = []int{0, 1, 2, 3, 4, 5, 1}
	d.Layout.Bounds = [][]float64{
		{0, 100, 200, 50},    // 10: above
		{0, 1200, 200, 50},   // 11: on screen
		{0, 990, 200, 20},    // 12: across the top edge
		{0, 1900, 200, 50},   // 13: below
		{2000, 1200, 50, 50}, // 14: right of it
		{0, 100, 0, 0},       // 15: empty, place unknown
		{0, 5000, 10, 10},    // a second entry of 11: its first counts
	}
	// 16 has no layout entry.
	got := offscreenOf(&d, viewport{X: 0, Y: 1000, W: 1280, H: 800})
	if want := []int64{10, 13, 14}; !reflect.DeepEqual(got, want) {
		t.Errorf("offscreen = %v, want %v", got, want)
	}
	if offscreenOf(&d, viewport{}) != nil {
		t.Error("an unknown viewport marks nothing")
	}
}

// TestBuildMarksOffscreenElements: the Marks' backendNodeIds reach the
// Elements built from those nodes.
func TestBuildMarksOffscreenElements(t *testing.T) {
	raw := &Raw{Main: Doc{FrameID: "F", Marks: &Marks{Offscreen: []int64{3}}, Nodes: []AXNode{
		{NodeID: "1", Role: axValue("RootWebArea"), ChildIDs: []string{"2", "3"}, BackendDOMNodeID: 1},
		{NodeID: "2", ParentID: "1", Role: axValue("button"), Name: axValue("Near"), BackendDOMNodeID: 2},
		{NodeID: "3", ParentID: "1", Role: axValue("button"), Name: axValue("Far"), BackendDOMNodeID: 3},
	}}}
	res := Build(raw, Limits{MaxNodes: 100, MaxDepth: 10, MaxChildren: 100, NodeLabel: 3000, DepthLabel: 48})
	if len(res.Elements) != 2 || res.Elements[0].Offscreen || !res.Elements[1].Offscreen {
		t.Errorf("elements = %+v", res.Elements)
	}
}
