package uia

// The point bounds (speed ticket 03), pure: a point Action matches the
// bounds the read cached and reads live only the Elements that decide the
// hit; when one of those moved, or the read cached none, it reads every
// Element live as before. Either way the hit is the one the live bounds
// give.

import (
	"context"
	"fmt"
	"testing"
)

// pointWindow is a Window of rows×cols buttons in a pane, each with a
// label inside: the read's bounds, one candidate per Element.
func pointWindow(rows, cols int) []pointCand {
	cands := []pointCand{
		{depth: 0, sig: "window"},
		{depth: 1, sig: "pane"},
	}
	cands[0].bounds = rect{0, 0, int32(cols*100 + 20), int32(rows*40 + 20)}
	cands[1].bounds = rect{10, 10, int32(cols*100 + 10), int32(rows*40 + 10)}
	for r := range rows {
		for c := range cols {
			l, t := int32(10+c*100), int32(10+r*40)
			cands = append(cands,
				pointCand{depth: 2, sig: "button", route: true, bounds: rect{l, t, l + 90, t + 30}},
				pointCand{depth: 3, sig: "text", bounds: rect{l + 5, t + 5, l + 60, t + 25}},
			)
		}
	}
	return cands
}

// boundsReader serves a Window's bounds and counts the reads.
type boundsReader struct {
	at    []rect
	reads int
}

func (b *boundsReader) read(i int) (rect, bool) {
	b.reads++
	return b.at[i], true
}

func boundsOf(cands []pointCand) []rect {
	out := make([]rect, len(cands))
	for i, c := range cands {
		out[i] = c.bounds
	}
	return out
}

// hitWith runs pointBounds over the read's and the live bounds, then the
// hit-test, as ElementAtPoint does.
func hitWith(t *testing.T, read, now []rect, shape []pointCand, x, y int32) (under, act int, err error, live int) {
	t.Helper()
	cands := make([]pointCand, len(shape))
	for i, c := range shape {
		cands[i] = pointCand{depth: c.depth, sig: c.sig, route: c.route}
	}
	cached := &boundsReader{at: read}
	current := &boundsReader{at: now}
	all := func(int) bool { return true }
	if e := pointBounds(context.Background(), cands, all, cached.read, current.read, x, y); e != nil {
		t.Fatalf("pointBounds: %v", e)
	}
	under, act, err = hitTest(cands, x, y)
	return under, act, err, current.reads
}

// liveHit is the hit the live bounds alone give: the old pass.
func liveHit(shape []pointCand, now []rect, x, y int32) (int, int, error) {
	cands := make([]pointCand, len(shape))
	for i, c := range shape {
		cands[i] = pointCand{depth: c.depth, sig: c.sig, route: c.route, bounds: now[i], readable: true}
	}
	return hitTest(cands, x, y)
}

func TestPointBoundsReadsLiveOnlyWhatDecidesTheHit(t *testing.T) {
	win := pointWindow(15, 10) // 302 Elements
	read := boundsOf(win)
	under, act, err, live := hitWith(t, read, read, win, 215, 65) // the label of row 1, column 2
	wu, wa, werr := liveHit(win, read, 215, 65)
	if err != nil || under != wu || act != wa || werr != nil {
		t.Fatalf("hit %d, %d, %v; the live bounds give %d, %d, %v", under, act, err, wu, wa, werr)
	}
	// The root, the pane, the button and its label: 4 round trips, not 302.
	if live != 4 {
		t.Errorf("%d live reads for a %d-Element Window, want 4 (the old pass read %d)", live, len(win), len(win))
	}
}

func TestPointBoundsFallsBackToLiveWhenTheHitMoved(t *testing.T) {
	win := pointWindow(3, 3)
	read := boundsOf(win)
	cases := map[string]func(now []rect){
		"the Window moved": func(now []rect) {
			for i := range now {
				now[i].Left, now[i].Right = now[i].Left+50, now[i].Right+50
			}
		},
		"the button under the point scrolled away": func(now []rect) {
			// Row 1, column 1: 2 + 2·(1·3+1), and its label.
			now[10], now[11] = rect{0, 0, 0, 0}, rect{0, 0, 0, 0}
		},
		"the pane resized": func(now []rect) { now[1].Bottom -= 100 },
	}
	for name, change := range cases {
		now := append([]rect(nil), read...)
		change(now)
		under, act, err, live := hitWith(t, read, now, win, 115, 65)
		wu, wa, werr := liveHit(win, now, 115, 65)
		if under != wu || act != wa || fmt.Sprint(err) != fmt.Sprint(werr) {
			t.Errorf("%s: hit %d, %d, %v; the live bounds give %d, %d, %v", name, under, act, err, wu, wa, werr)
		}
		if live <= len(win) {
			t.Errorf("%s: %d live reads, want the check and then every Element (%d)", name, live, len(win))
		}
	}
}

// An Element without cached bounds (a read from before the cache had
// them) reads every Element live.
func TestPointBoundsWithoutCachedBoundsReadsLive(t *testing.T) {
	win := pointWindow(2, 2)
	cands := make([]pointCand, len(win))
	for i, c := range win {
		cands[i] = pointCand{depth: c.depth, sig: c.sig, route: c.route}
	}
	current := &boundsReader{at: boundsOf(win)}
	none := func(int) (rect, bool) { return rect{}, false }
	if err := pointBounds(context.Background(), cands, func(int) bool { return true }, none, current.read, 15, 15); err != nil {
		t.Fatal(err)
	}
	if current.reads != len(win) {
		t.Errorf("%d live reads, want every Element (%d)", current.reads, len(win))
	}
}

// Elements with no live Element (a Page's) stay unreadable in both passes.
func TestPointBoundsSkipsElementsItCannotRead(t *testing.T) {
	win := pointWindow(1, 2)
	cands := make([]pointCand, len(win))
	for i, c := range win {
		cands[i] = pointCand{depth: c.depth, sig: c.sig, route: c.route}
	}
	read := &boundsReader{at: boundsOf(win)}
	current := &boundsReader{at: boundsOf(win)}
	kept := func(i int) bool { return i != 3 } // the first button's label
	if err := pointBounds(context.Background(), cands, kept, read.read, current.read, 20, 20); err != nil {
		t.Fatal(err)
	}
	if cands[3].readable {
		t.Error("an Element with no live Element got bounds")
	}
	if under, _, err := hitTest(cands, 20, 20); err != nil || under != 2 {
		t.Errorf("hit %d, %v; want the button (2), its label unreadable", under, err)
	}
}

// Every point of a Window that has not changed hits what the live bounds
// hit, with a handful of live reads.
func TestPointBoundsMatchesTheLivePassEverywhere(t *testing.T) {
	win := pointWindow(4, 4)
	read := boundsOf(win)
	most := 0
	for x := int32(0); x < 430; x += 7 {
		for y := int32(0); y < 190; y += 5 {
			under, act, err, live := hitWith(t, read, read, win, x, y)
			wu, wa, werr := liveHit(win, read, x, y)
			if under != wu || act != wa || fmt.Sprint(err) != fmt.Sprint(werr) {
				t.Fatalf("(%d,%d): hit %d, %d, %v; the live bounds give %d, %d, %v", x, y, under, act, err, wu, wa, werr)
			}
			most = max(most, live)
		}
	}
	if most > 4 {
		t.Errorf("up to %d live reads at a point, want at most 4", most)
	}
}

func TestPointBoundsStopsWithTheRequest(t *testing.T) {
	win := pointWindow(2, 2)
	cands := make([]pointCand, len(win))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	none := func(int) (rect, bool) { return rect{}, false }
	live := &boundsReader{at: boundsOf(win)}
	if err := pointBounds(ctx, cands, func(int) bool { return true }, none, live.read, 15, 15); err == nil {
		t.Error("an ended request read on")
	}
}
