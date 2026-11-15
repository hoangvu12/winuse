package uia

// Live re-resolution's offline tests (desktop-parity 17): the cache
// answers when its Element is alive; a missing Window State, no match or a
// gone Element falls back to the live walk; the cache's ambiguity and
// another Agent's read refuse without walking; a fresh read keeps one
// match, or every candidate of an ambiguous one, and answers with the
// indexes they were added under; the store never adds one Element twice.
// The walk and the probe are fakes; no COM.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/wstate"
)

func TestResolveCacheFirst(t *testing.T) {
	els := []wstate.Element{{Name: "Window"}, {Name: "Save"}, {Name: "Twin"}, {Name: "Twin"}}
	cached := cacheView{els: els, base: len(els), ok: true}
	walked := 0
	live := func() (LiveResolution, error) { walked++; return LiveResolution{Index: 9, Fresh: true}, nil }
	aliveAll := func(int) bool { return true }

	for _, c := range []struct {
		name   string
		cv     cacheView
		ref    string
		alive  func(int) bool
		want   LiveResolution
		walks  int
		errHas string
	}{
		{"cache hit", cached, "name:Save", aliveAll, LiveResolution{Index: 1}, 0, ""},
		{"stale, then found", cached, "name:Save", func(int) bool { return false }, LiveResolution{Index: 9, Fresh: true}, 1, ""},
		{"not in the cache", cached, "name:Open", aliveAll, LiveResolution{Index: 9, Fresh: true}, 1, ""},
		{"no Window State (helper restart)", cacheView{noState: errNoState(101)}, "name:Save", aliveAll, LiveResolution{Index: 9, Fresh: true}, 1, ""},
		{"ambiguous in the cache", cached, "name:Twin", aliveAll, LiveResolution{}, 0, "matches 2 elements"},
		{"another Agent read it", cacheView{noState: &NotReadByAgentError{WindowID: 101}}, "name:Save", aliveAll, LiveResolution{}, 0, "another agent read it"},
	} {
		walked = 0
		got, err := resolveCacheFirst(c.cv, c.ref, c.alive, live)
		if c.errHas != "" {
			if err == nil || !strings.Contains(err.Error(), c.errHas) {
				t.Errorf("%s: %v, want an error with %q", c.name, err, c.errHas)
			}
		} else if err != nil || got != c.want {
			t.Errorf("%s: %+v, %v; want %+v", c.name, got, err, c.want)
		}
		if walked != c.walks {
			t.Errorf("%s: walked %d times, want %d", c.name, walked, c.walks)
		}
	}
}

func TestLiveMatchesAndAnswer(t *testing.T) {
	read := []wstate.Element{{Role: "window", Name: "W"}, {Role: "button", Name: "Save"}, {Role: "button", Name: "Twin"}, {Role: "button", Name: "Twin"}}

	keep, err := liveMatches(read, "name:Save")
	if err != nil || !slices.Equal(keep, []bool{false, true, false, false}) {
		t.Errorf("one match: %v, %v", keep, err)
	}
	got, err := liveAnswer(err, []int{12}, read[1:2])
	if err != nil || got != (LiveResolution{Index: 12, Fresh: true}) {
		t.Errorf("one match: %+v, %v", got, err)
	}

	keep, err = liveMatches(read, "name:Twin")
	if !slices.Equal(keep, []bool{false, false, true, true}) {
		t.Errorf("ambiguous keeps %v", keep)
	}
	_, err = liveAnswer(err, []int{12, 13}, read[2:4])
	var live *LiveRefError
	var several *wstate.AmbiguousRefError
	if !errors.As(err, &live) || !errors.As(err, &several) || !slices.Equal(several.Matches, []int{12, 13}) ||
		several.Candidates[0] != "12 button Twin" || several.Candidates[1] != "13 button Twin" {
		t.Errorf("ambiguous: %v %+v", err, several)
	}

	keep, err = liveMatches(read, "id:none")
	if slices.Contains(keep, true) {
		t.Errorf("no match keeps %v", keep)
	}
	_, err = liveAnswer(err, nil, nil)
	var none *wstate.NoMatchRefError
	if !errors.As(err, &live) || !errors.As(err, &none) {
		t.Errorf("no match: %v", err)
	}
}

// The same Element (by RuntimeId) is added once: a second find or live
// resolution gets the index it already has, and its new handle is released.
func TestStoreAddsAnElementOnce(t *testing.T) {
	f := newFakeStore()
	f.install("A", 101, f.beginRead(), ptrs(2), []wstate.Element{{Name: "Window"}, {Name: "Pane"}}, false)
	first := ptrs(2)
	if idx := f.add("A", 101, first, []wstate.Element{{Name: "Save"}, {Name: "Open"}}, []string{"42.1", "42.2"}); !slices.Equal(idx, []int{2, 3}) {
		t.Fatalf("first add: %v", idx)
	}
	again := ptrs(3)
	idx := f.add("A", 101, again, []wstate.Element{{Name: "Open"}, {Name: "Close"}, {Name: "Unknown"}}, []string{"42.2", "42.3", ""})
	if !slices.Equal(idx, []int{3, 4, 5}) {
		t.Errorf("second add: %v, want Open kept at 3 and two new", idx)
	}
	if f.released[again[0]] != 1 || f.released[again[1]] != 0 {
		t.Errorf("released %v: the repeated handle is released, the new ones kept", f.released)
	}
	cv, _ := f.refState("A", 101)
	els, base := cv.els, cv.base
	if base != 2 || len(els) != 6 {
		t.Errorf("state: base %d, %d Elements", base, len(els))
	}
	// A full read starts the added set over.
	f.install("A", 101, f.beginRead(), ptrs(1), []wstate.Element{{Name: "Window"}}, false)
	if idx := f.add("A", 101, ptrs(1), []wstate.Element{{Name: "Save"}}, []string{"42.1"}); !slices.Equal(idx, []int{1}) {
		t.Errorf("after a full read: %v", idx)
	}
}

func TestRuntimeKey(t *testing.T) {
	if got := runtimeKey([]int32{42, 7, -13}); got != "42.7.-13" {
		t.Errorf("key %q", got)
	}
}
