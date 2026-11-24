package uia

// Offline tests for scroll into-view's read-back (recap-trial 04): after
// ScrollItem.ScrollIntoView the Element's bounds must lie inside the
// visible part of the container that scrolls it; IsOffscreen is an extra
// signal, and the only one when bounds are empty. The trees are generic:
// a list's view and an item's bounds (virtualList). No COM is touched.

import (
	"errors"
	"strings"
	"testing"
)

// lyingIntoView drives into-view on item id of f through a provider whose
// IsOffscreen always reads false, as some providers report every item.
func lyingIntoView(f *virtualList, id int) (intoViewer, *int) {
	calls := new(int)
	read := func() intoViewState {
		return intoViewState{view: listView, el: f.bounds(id), offscreenKnown: true, offscreen: false}
	}
	return intoViewer{
		read:   read,
		scroll: func() error { *calls++; f.bringIntoView(id); return nil },
		settle: func(done func(intoViewState) bool) intoViewState { return read() },
	}, calls
}

func TestIntoViewProvesTheMoveByBounds(t *testing.T) {
	t.Run("an item far down is brought inside the view", func(t *testing.T) {
		f := newVirtualList(40, 50)
		s, calls := lyingIntoView(f, 30)
		already, err := scrollIntoViewChecked(s)
		if err != nil || already {
			t.Fatalf("= %v, %v; want ok, not already in view", already, err)
		}
		if *calls != 1 || f.offset != 30*50+50-200 {
			t.Errorf("calls %d, offset %d; want one call and item 30 at the bottom", *calls, f.offset)
		}
	})
	t.Run("a list that ignores the call is not in view, whatever IsOffscreen says", func(t *testing.T) {
		f := newVirtualList(40, 50)
		f.ignore = true
		s, _ := lyingIntoView(f, 30)
		_, err := scrollIntoViewChecked(s)
		var verify *VerifyError
		if !errors.As(err, &verify) {
			t.Fatalf("error = %v, want a delivered VerifyError", err)
		}
		if err.Error() != intoViewStillMsg {
			t.Errorf("message = %q, want %q", err.Error(), intoViewStillMsg)
		}
	})
	t.Run("a list that moves short of the item says it moved", func(t *testing.T) {
		f := newVirtualList(40, 50)
		s, _ := lyingIntoView(f, 30)
		s.scroll = func() error { f.offset += 100; return nil }
		_, err := scrollIntoViewChecked(s)
		var verify *VerifyError
		if !errors.As(err, &verify) || err.Error() != intoViewShortMsg {
			t.Fatalf("error = %v, want %q", err, intoViewShortMsg)
		}
	})
	t.Run("an item above the top is brought down into view", func(t *testing.T) {
		f := newVirtualList(40, 50)
		f.offset = 1000
		s, _ := lyingIntoView(f, 3)
		if already, err := scrollIntoViewChecked(s); err != nil || already {
			t.Fatalf("= %v, %v; want ok", already, err)
		}
		if f.offset != 150 {
			t.Errorf("offset = %d, want 150 (item 3 at the top)", f.offset)
		}
	})
	t.Run("an item inside the view before is already in view", func(t *testing.T) {
		f := newVirtualList(40, 50)
		s, calls := lyingIntoView(f, 1)
		already, err := scrollIntoViewChecked(s)
		if err != nil || !already {
			t.Fatalf("= %v, %v; want ok, already in view", already, err)
		}
		if *calls != 1 || f.offset != 0 {
			t.Errorf("calls %d, offset %d; want the call made and nothing moved", *calls, f.offset)
		}
	})
	t.Run("an item cut by the edge is not already in view", func(t *testing.T) {
		f := newVirtualList(40, 50)
		f.offset = 20 // item 4 (content 200..250) sits at 280..330, past the view's bottom at 300
		s, _ := lyingIntoView(f, 4)
		if already, err := scrollIntoViewChecked(s); err != nil || already {
			t.Fatalf("= %v, %v; want ok, not already in view", already, err)
		}
		if f.offset != 50 {
			t.Errorf("offset = %d, want 50", f.offset)
		}
	})
	t.Run("inside the view but reading offscreen is not in view", func(t *testing.T) {
		f := newVirtualList(40, 50)
		s, _ := lyingIntoView(f, 30)
		read := func() intoViewState {
			return intoViewState{view: listView, el: f.bounds(30), offscreenKnown: true, offscreen: true}
		}
		s.read = read
		s.settle = func(func(intoViewState) bool) intoViewState { return read() }
		if _, err := scrollIntoViewChecked(s); !errors.As(err, new(*VerifyError)) {
			t.Fatalf("error = %v, want a VerifyError", err)
		}
	})
	t.Run("the call failing is its own error", func(t *testing.T) {
		f := newVirtualList(40, 50)
		s, _ := lyingIntoView(f, 30)
		s.scroll = func() error { return errIntoView }
		if _, err := scrollIntoViewChecked(s); !errors.Is(err, errIntoView) {
			t.Fatalf("error = %v, want the call's error", err)
		}
	})
}

// An Element or a container with empty bounds leaves the verdict to
// IsOffscreen, as before; with neither readable it is not judged.
func TestIntoViewWithoutBoundsFallsBackToIsOffscreen(t *testing.T) {
	for _, tc := range []struct {
		name       string
		view, el   rect
		known, off bool
		wantErr    bool
	}{
		{"empty Element bounds, on screen", listView, rect{}, true, false, false},
		{"empty Element bounds, offscreen", listView, rect{}, true, true, true},
		{"no container known, on screen", rect{}, rect{0, 900, 100, 950}, true, false, false},
		{"no container known, offscreen", rect{}, rect{0, 900, 100, 950}, true, true, true},
		{"nothing readable is not judged", rect{}, rect{}, false, false, false},
	} {
		st := intoViewState{view: tc.view, el: tc.el, offscreenKnown: tc.known, offscreen: tc.off}
		read := func() intoViewState { return st }
		already, err := scrollIntoViewChecked(intoViewer{
			read:   read,
			scroll: func() error { return nil },
			settle: func(func(intoViewState) bool) intoViewState { return read() },
		})
		if already {
			t.Errorf("%s: already in view without bounds to tell", tc.name)
		}
		var verify *VerifyError
		if got := errors.As(err, &verify); got != tc.wantErr {
			t.Errorf("%s: error = %v, want VerifyError %v", tc.name, err, tc.wantErr)
		}
		if tc.wantErr && !strings.Contains(err.Error(), "offscreen") {
			t.Errorf("%s: message = %q, want the IsOffscreen wording", tc.name, err.Error())
		}
	}
}

func TestInView(t *testing.T) {
	for _, tc := range []struct {
		name          string
		el            rect
		judged, sound bool
	}{
		{"inside", rect{0, 150, 100, 200}, true, true},
		{"a pixel of rounding", rect{0, 99, 100, 301}, true, true},
		{"cut by the bottom", rect{0, 280, 100, 330}, true, false},
		{"above the top", rect{0, 40, 100, 90}, true, false},
		{"past the right edge", rect{50, 150, 140, 200}, true, false},
		{"taller than the view and filling it", rect{0, 100, 100, 500}, true, true},
		{"taller than the view, half out", rect{0, 200, 100, 600}, true, false},
		{"empty bounds", rect{}, false, false},
	} {
		judged, in := inView(listView, tc.el)
		if judged != tc.judged || in != tc.sound {
			t.Errorf("%s: inView = %v, %v; want %v, %v", tc.name, judged, in, tc.judged, tc.sound)
		}
	}
	if judged, _ := inView(rect{}, rect{0, 150, 100, 200}); judged {
		t.Errorf("an empty view judged the Element")
	}
}
