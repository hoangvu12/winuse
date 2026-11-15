package uia

// A read's completeness and the name:/id: answers it allows (reliability
// council 03): the walk says it did not see the whole Window for every
// reason a read is partial (time, a chunk that timed out, the node,
// children and depth caps, unreadable children, a skeleton's hidden
// children), and not for a Document cut; the stored Window State carries
// it, a Page splice adds the Page read's; a partial cache answers only
// with ambiguity and otherwise goes live, and a partial fresh read refuses
// its one match and reports a miss as incomplete. Fakes only; no COM.

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

func TestWalkPartialForEveryReason(t *testing.T) {
	failOn := func(name string, err error) func(n *fakeNode) error {
		return func(n *fakeNode) error {
			if n.data.Name == name {
				return err
			}
			return nil
		}
	}
	for _, c := range []struct {
		name    string
		tree    *fakeNode
		set     func(src *fakeSource, cfg *WalkConfig)
		partial bool
	}{
		{"whole", sampleTree(), func(*fakeSource, *WalkConfig) {}, false},
		{"time limit", sampleTree(), func(src *fakeSource, cfg *WalkConfig) {
			src.step, cfg.Budget, cfg.Now = time.Second, 3*time.Second, src.now
		}, true},
		{"chunk timed out", sampleTree(), func(src *fakeSource, _ *WalkConfig) { src.childrenErr = failOn("Menu Bar", ErrChunkTimedOut) }, true},
		{"node cap", sampleTree(), func(_ *fakeSource, cfg *WalkConfig) { cfg.MaxNodes = 3 }, true},
		{"children cap", sampleTree(), func(_ *fakeSource, cfg *WalkConfig) { cfg.MaxChildren = 2 }, true},
		{"depth cap", sampleTree(), func(_ *fakeSource, cfg *WalkConfig) { cfg.MaxDepth = 1 }, true},
		{"unreadable children", sampleTree(), func(src *fakeSource, _ *WalkConfig) { src.childrenErr = failOn("Menu Bar", errors.New("gone")) }, true},
		{"skeleton with hidden children", sampleTree(), func(_ *fakeSource, cfg *WalkConfig) { cfg.MaxDepth, cfg.CountHidden = 1, true }, true},
		{"skeleton whose count failed", sampleTree(), func(src *fakeSource, cfg *WalkConfig) {
			cfg.MaxDepth, cfg.CountHidden = 1, true
			src.childrenErr = failOn("Menu Bar", errors.New("gone"))
		}, true},
		{"skeleton that saw every leaf", sampleTree(), func(_ *fakeSource, cfg *WalkConfig) { cfg.MaxDepth, cfg.CountHidden = 2, true }, false},
		{"Document cut", browserTree(), func(_ *fakeSource, cfg *WalkConfig) { cfg.Cut = cutNamed("Page") }, false},
	} {
		src := newFakeSource(c.tree)
		cfg := fullConfig()
		c.set(src, &cfg)
		res, err := Walk[*fakeNode](src, cfg)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.Partial != c.partial {
			t.Errorf("%s: Partial = %v, want %v (notes %q)", c.name, res.Partial, c.partial, res.Notes)
		}
	}
}

// A late cut drops what was read under the Document, and the partial mark
// an unreadable chunk there left goes with it, as its note does.
func TestWalkLateCutRestoresCompleteness(t *testing.T) {
	tree := browserTree()
	doc := tree.kids[0].kids[1]
	known := false
	src := newFakeSource(tree)
	src.childrenErr = func(n *fakeNode) error {
		if n == doc.kids[0] {
			known = true
			return errors.New("gone")
		}
		return nil
	}
	cfg := fullConfig()
	cfg.Cut = lateCut("Page", &known)
	res, err := Walk[*fakeNode](src, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Cut != 4 || res.Partial || len(res.Notes) != 0 {
		t.Errorf("cut %d, partial %v, notes %q; want the Document cut and a whole read", res.Cut, res.Partial, res.Notes)
	}
}

func TestResolveCacheFirstNeedsAWholeRead(t *testing.T) {
	els := []wstate.Element{{Name: "Window"}, {Name: "Save"}, {Name: "Twin"}, {Name: "Twin"}}
	partial := cacheView{els: els, base: len(els), partial: true, ok: true}
	walked := 0
	live := func() (LiveResolution, error) { walked++; return LiveResolution{Index: 9, Fresh: true}, nil }
	aliveAll := func(int) bool { return true }

	if got, err := resolveCacheFirst(partial, "name:Save", aliveAll, live); err != nil || got != (LiveResolution{Index: 9, Fresh: true}) || walked != 1 {
		t.Errorf("one match in a partial cache: %+v, %v, walked %d; want the live walk's answer", got, err, walked)
	}
	walked = 0
	var several *wstate.AmbiguousRefError
	if _, err := resolveCacheFirst(partial, "name:Twin", aliveAll, live); !errors.As(err, &several) || walked != 0 {
		t.Errorf("several in a partial cache: %v, walked %d; want the cache's ambiguity", err, walked)
	}
	walked = 0
	whole := partial
	whole.partial = false
	if got, err := resolveCacheFirst(whole, "name:Save", aliveAll, live); err != nil || got != (LiveResolution{Index: 1}) || walked != 0 {
		t.Errorf("one match in a whole cache: %+v, %v, walked %d", got, err, walked)
	}
}

func TestPartialFreshRead(t *testing.T) {
	read := []wstate.Element{{Role: "window", Name: "W"}, {Role: "button", Name: "Delete"}, {Role: "button", Name: "Twin"}, {Role: "button", Name: "Twin"}}

	// One match: kept (added), and refused with its new index.
	keep, err := liveMatches(read, "name:Delete")
	if !slices.Equal(keep, []bool{false, true, false, false}) {
		t.Errorf("one match keeps %v", keep)
	}
	_, err = liveAnswer(partialRead("name:Delete", err), []int{12}, read[1:2])
	var live *LiveRefError
	var one *wstate.PartialRefError
	if !errors.As(err, &live) || !errors.As(err, &one) || one.Match != 12 || one.Candidate != "12 button Delete" ||
		err.Error() != `name "Delete": 1 match in the part read; the read was partial` {
		t.Errorf("one match: %v %+v", err, one)
	}

	// None: incomplete, not absent.
	_, err = liveMatches(read, "id:none")
	_, err = liveAnswer(partialRead("id:none", err), nil, nil)
	var none *wstate.NoMatchRefError
	if !errors.As(err, &none) || !none.Partial || err.Error() != `no element with ID "none" in the part read; the read was partial` {
		t.Errorf("no match: %v", err)
	}

	// Several: the existing refusal.
	_, err = liveMatches(read, "name:Twin")
	_, err = liveAnswer(partialRead("name:Twin", err), []int{12, 13}, read[2:4])
	var several *wstate.AmbiguousRefError
	if !errors.As(err, &several) || !slices.Equal(several.Matches, []int{12, 13}) {
		t.Errorf("several: %v", err)
	}
}

// The stored Window State carries the read's completeness; the cache-only
// resolution then refuses a partial read's one match and marks its miss.
func TestStoredReadCompleteness(t *testing.T) {
	var a, b, cc, d int
	els := refState()
	for _, partial := range []bool{false, true} {
		c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
		c.store.install(defaultAgent, 101, c.store.beginRead(), []comPtr{comPtr(&a), comPtr(&b), comPtr(&cc), comPtr(&d)}, els, partial)
		i, err := c.ResolveElementRef(context.Background(), 101, "id:searchBox")
		var one *wstate.PartialRefError
		switch {
		case !partial && (err != nil || i != 3):
			t.Errorf("whole read: %d, %v", i, err)
		case partial && (!errors.As(err, &one) || one.Match != 3 || one.Candidate != wstate.Line(3, &els[3])):
			t.Errorf("partial read: %d, %v", i, err)
		}
		_, err = c.ResolveElementRef(context.Background(), 101, "name:Nope")
		var none *wstate.NoMatchRefError
		if !errors.As(err, &none) || none.Partial != partial {
			t.Errorf("partial %v: miss %v", partial, err)
		}
		var several *wstate.AmbiguousRefError
		if _, err := c.ResolveElementRef(context.Background(), 101, "name:Twin"); !errors.As(err, &several) {
			t.Errorf("partial %v: several %v", partial, err)
		}
		c.Close()
	}
}

// A Page splice keeps the frame read's completeness and adds the Page's.
func TestSplicePageCarriesCompleteness(t *testing.T) {
	for _, c := range []struct {
		frame, page, want bool
	}{{false, false, false}, {false, true, true}, {true, false, true}} {
		cl := testClient(func() (*worker, error) { return startFakeWorker(), nil })
		var w, doc int
		base := []wstate.Element{{Role: "window"}, {Depth: 1, Role: "document"}}
		cl.store.install(defaultAgent, 7, cl.store.beginRead(), []comPtr{comPtr(&w), comPtr(&doc)}, base, c.frame)
		merged := []wstate.Element{base[0], base[1], {Depth: 2, Role: "button", Name: "Go"}}
		if err := cl.SplicePage(context.Background(), 7, base, merged, []int{0, 1, -1}, map[int]any{2: "o"}, c.page); err != nil {
			t.Fatal(err)
		}
		if cv, _ := cl.store.refState(defaultAgent, 7); cv.partial != c.want {
			t.Errorf("frame %v, Page %v: partial %v, want %v", c.frame, c.page, cv.partial, c.want)
		}
		cl.Close()
	}
}

// A partial fresh read's one match is added so the refusal can name its
// index, and doubted: asking again, with a whole read stored or none, goes
// live again instead of taking it from the cache.
func TestDoubtedMatchNeverAnswersFromTheCache(t *testing.T) {
	f := newFakeStore()
	c := &Client{store: f.store}
	f.install("A", 101, f.beginRead(), ptrs(2), []wstate.Element{{Name: "Window"}, {Name: "Save"}}, false)
	idx := f.add("A", 101, ptrs(1), []wstate.Element{{Role: "button", Name: "Delete"}}, []string{"42.1"})
	f.doubt("A", 101, idx[0])

	var one *wstate.PartialRefError
	if _, err := c.ResolveElementRef(agentCtx("A"), 101, "name:Delete"); !errors.As(err, &one) || one.Match != 2 {
		t.Errorf("cache only: %v", err)
	}
	if i, err := c.ResolveElementRef(agentCtx("A"), 101, "name:Save"); err != nil || i != 1 {
		t.Errorf("the whole read still answers: %d, %v", i, err)
	}
	cv, _ := f.refState("A", 101)
	walked := 0
	live := func() (LiveResolution, error) { walked++; return LiveResolution{}, &LiveRefError{Err: one} }
	if _, err := resolveCacheFirst(cv, "name:Delete", func(int) bool { return true }, live); !errors.As(err, &one) || walked != 1 {
		t.Errorf("cache first: %v, walked %d; want the live walk", err, walked)
	}

	// With no Window State, the fresh read's add makes one; it is doubted too.
	f.add("A", 202, ptrs(1), []wstate.Element{{Role: "button", Name: "Delete"}}, []string{"43.1"})
	f.doubt("A", 202, 0)
	if _, err := c.ResolveElementRef(agentCtx("A"), 202, "name:Delete"); !errors.As(err, &one) {
		t.Errorf("added-only state: %v", err)
	}
	// A new read starts over.
	f.install("A", 101, f.beginRead(), ptrs(2), []wstate.Element{{Name: "Window"}, {Name: "Delete"}}, false)
	if i, err := c.ResolveElementRef(agentCtx("A"), 101, "name:Delete"); err != nil || i != 1 {
		t.Errorf("after a whole read: %d, %v", i, err)
	}
}
