package win

import (
	"testing"
	"time"
)

func TestCaretTiming(t *testing.T) {
	cases := map[time.Duration]float64{
		0:                                       1,
		cursorCaretDuration - cursorCaretFade:   1,
		cursorCaretDuration - cursorCaretFade/2: 0.5,
		cursorCaretDuration:                     0,
		2 * cursorCaretDuration:                 0,
	}
	for el, want := range cases {
		if got := caretOpacity(el); got < want-0.01 || got > want+0.01 {
			t.Errorf("caretOpacity(%v) = %v, want %v", el, got, want)
		}
	}
}

func TestCaretBar(t *testing.T) {
	if got := caretBar(100, 20, 40, 1); got != (Rect{Left: 99, Top: 20, Right: 101, Bottom: 40}) {
		t.Errorf("caretBar at 100%% = %+v", got)
	}
	if got := caretBar(100, 20, 40, 1.5); got.Width() != 3 || got.Height() != 20 {
		t.Errorf("caretBar at 150%% = %+v, want 3 px wide", got)
	}
	if got := caretBar(100, 40, 40, 1); got.Width() != 0 {
		t.Errorf("a caret with no height gives %+v, want none", got)
	}
}

func TestTheCaretShowsThenEnds(t *testing.T) {
	var m cursorModel
	bar := Rect{Left: 10, Top: 10, Right: 12, Bottom: 30}
	m.startCaret(t0, bar)
	if f := m.advance(t0.Add(100 * time.Millisecond)); !f.Caret || f.CaretBar != bar || f.CaretOp != 1 {
		t.Errorf("frame at 100ms = %+v, want the caret at full opacity", f)
	}
	if f := m.advance(t0); f.Cursor {
		t.Error("the caret alone must not show the dart")
	}
	if !m.animating() {
		t.Error("a showing caret must keep frames coming")
	}
	if f := m.advance(t0.Add(cursorCaretDuration)); f.Caret {
		t.Error("the caret still shows after its duration")
	}
	var empty cursorModel
	empty.startCaret(t0, Rect{})
	if empty.caret {
		t.Error("an empty bar must not start a caret")
	}
}

func TestCaretAboveReachesTheView(t *testing.T) {
	view := &fakeCursorView{}
	clock := &fakeClock{t: t0}
	c := newAgentCursor(inlineDo, view, clock.now)
	defer c.Close()
	c.CaretAbove(4242, 100, 20, 40)
	waitFor(t, "the caret", func() bool {
		var on bool
		view.with(func(s *fakeViewState) { on = s.caret })
		return on
	})
	view.with(func(s *fakeViewState) {
		if s.caretBar.Top != 20 || s.caretBar.Bottom != 40 || s.caretBar.Width() <= 0 {
			t.Errorf("caret bar = %+v", s.caretBar)
		}
		if len(s.anchors) == 0 || s.anchors[len(s.anchors)-1] != 4242 {
			t.Errorf("anchors = %v, want 4242", s.anchors)
		}
	})
	// A capture hides it too.
	c.HideWhile(func() {
		view.with(func(s *fakeViewState) {
			if s.caret {
				t.Error("the caret shows during a capture")
			}
		})
	})
}
