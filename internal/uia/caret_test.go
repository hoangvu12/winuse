package uia

import "testing"

func TestCaretEdgeOf(t *testing.T) {
	rects := []caretRect{{left: 100, top: 20, width: 8, height: 16}, {left: 10, top: 40, width: 8, height: 16}}
	// The character after the caret: its leading edge, on its line.
	if e, ok := caretEdgeOf(rects[:1], true); !ok || e != (CaretEdge{X: 100, Top: 20, Bottom: 36}) {
		t.Errorf("after = %+v, %v", e, ok)
	}
	// The character before it (the caret at the end): its trailing edge.
	if e, ok := caretEdgeOf(rects, false); !ok || e != (CaretEdge{X: 18, Top: 40, Bottom: 56}) {
		t.Errorf("before = %+v, %v", e, ok)
	}
	// No rectangle, or one with no height: no caret, never a guess.
	if _, ok := caretEdgeOf(nil, true); ok {
		t.Error("no rectangle must give no caret")
	}
	if _, ok := caretEdgeOf([]caretRect{{left: 5, top: 5, width: 4}}, true); ok {
		t.Error("a rectangle with no height must give no caret")
	}
}
