package uia

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// spliceClient is a Client over a fake worker whose store records
// releases.
func spliceClient(t *testing.T) (*Client, func() []comPtr) {
	t.Helper()
	var mu sync.Mutex
	var released []comPtr
	c := &Client{
		factory: func() (*worker, error) { return startFakeWorker(), nil },
		store: newStore(func(comPtr) {}, func(p comPtr) {
			mu.Lock()
			released = append(released, p)
			mu.Unlock()
		}),
	}
	t.Cleanup(c.Close)
	return c, func() []comPtr {
		mu.Lock()
		defer mu.Unlock()
		return append([]comPtr(nil), released...)
	}
}

func TestSplicePageNumbersAcrossBothParts(t *testing.T) {
	c, released := spliceClient(t)
	var win, tab, doc, old1, old2, bar int
	base := []wstate.Element{
		{Depth: 0, Role: "window"}, {Depth: 1, Role: "tab"}, {Depth: 1, Role: "document"},
		{Depth: 2, Role: "link", Name: "old"}, {Depth: 2, Role: "button", Name: "old"}, {Depth: 1, Role: "group", Name: "infobar"},
	}
	ptrs := []comPtr{comPtr(&win), comPtr(&tab), comPtr(&doc), comPtr(&old1), comPtr(&old2), comPtr(&bar)}
	c.store.install(defaultAgent, 7, c.store.beginRead(), ptrs, base)

	merged := []wstate.Element{base[0], base[1], base[2],
		{Depth: 2, Role: "heading", Name: "page"}, {Depth: 2, Role: "edit", Name: "page"}, {Depth: 2, Role: "button", Name: "page"},
		base[5]}
	origins := map[int]any{3: "h", 4: "e", 5: "b"}
	if err := c.SplicePage(context.Background(), 7, base, merged, []int{0, 1, 2, -1, -1, -1, 5}, origins); err != nil {
		t.Fatal(err)
	}
	// The replaced UIA Elements are released, the others kept.
	if r := released(); len(r) != 2 || r[0] != comPtr(&old1) || r[1] != comPtr(&old2) {
		t.Errorf("released %v", r)
	}
	for i, want := range []any{nil, nil, nil, "h", "e", "b", nil} {
		got, err := c.ElementOrigin(context.Background(), 7, i)
		if err != nil || got != want {
			t.Errorf("origin %d = %v, %v; want %v", i, got, err, want)
		}
	}
	// A UIA Element after the Page keeps its live Element at its new index.
	if el, ok := c.store.element(defaultAgent, 7, 6); !ok || el != comPtr(&bar) {
		t.Errorf("index 6 holds %v", el)
	}
	// UIA Actions given a Page index fail plainly instead of using a nil
	// Element.
	if _, err := c.lookup(context.Background(), 7, 4); !errors.Is(err, ErrPageElement) {
		t.Errorf("lookup of a Page Element: %v", err)
	}
	if _, err := c.ElementPoint(context.Background(), 7, 4); !errors.Is(err, ErrPageElement) {
		t.Errorf("ElementPoint of a Page Element: %v", err)
	}
	// name: references resolve across both parts.
	if i, err := c.ResolveElementRef(context.Background(), 7, "name:infobar"); err != nil || i != 6 {
		t.Errorf("name:infobar = %d, %v", i, err)
	}
	if _, err := c.ElementOrigin(context.Background(), 7, 7); !errors.Is(err, ErrStale) {
		t.Errorf("out of range: %v", err)
	}
	if _, err := c.ElementOrigin(context.Background(), 8, 0); !errors.Is(err, ErrNoState) {
		t.Errorf("no state: %v", err)
	}

	// A new read replaces the Page origins with the rest.
	var fresh int
	c.store.install(defaultAgent, 7, c.store.beginRead(), []comPtr{comPtr(&fresh)}, []wstate.Element{{Role: "window"}})
	if o, err := c.ElementOrigin(context.Background(), 7, 0); err != nil || o != nil {
		t.Errorf("after a new read: %v, %v", o, err)
	}
	if _, err := c.ElementOrigin(context.Background(), 7, 3); !errors.Is(err, ErrStale) {
		t.Errorf("a Page index after a new read: %v", err)
	}
}

func TestSplicePageRefusesASupersededState(t *testing.T) {
	c, released := spliceClient(t)
	var a, b, d int
	first := []wstate.Element{{Role: "window"}, {Depth: 1, Role: "document"}, {Depth: 2, Role: "link"}}
	c.store.install(defaultAgent, 7, c.store.beginRead(), []comPtr{comPtr(&a), comPtr(&b), comPtr(&d)}, first)
	// A newer read of the Window lands before the splice.
	second := append([]wstate.Element(nil), first...)
	c.store.install(defaultAgent, 7, c.store.beginRead(), []comPtr{comPtr(&a), comPtr(&b), comPtr(&d)}, second)
	before := len(released())
	err := c.SplicePage(context.Background(), 7, first, []wstate.Element{first[0], first[1], {Depth: 2, Role: "button"}}, []int{0, 1, -1}, map[int]any{2: "x"})
	if !errors.Is(err, ErrSuperseded) {
		t.Fatalf("err = %v", err)
	}
	if len(released()) != before {
		t.Error("a refused splice released Elements")
	}
	if o, _ := c.ElementOrigin(context.Background(), 7, 2); o != nil {
		t.Error("a refused splice stored origins")
	}
}

// TestSplicePageIntoACutRead: a read whose walk left the document's
// descendants out (WalkConfig.Cut) splices the Page right after the
// document's line, releasing nothing, and the Elements after it move on.
func TestSplicePageIntoACutRead(t *testing.T) {
	c, released := spliceClient(t)
	var win, doc, bar int
	base := []wstate.Element{{Depth: 0, Role: "window"}, {Depth: 1, Role: "document", Name: "Page"}, {Depth: 1, Role: "group", Name: "infobar"}}
	c.store.install(defaultAgent, 7, c.store.beginRead(), []comPtr{comPtr(&win), comPtr(&doc), comPtr(&bar)}, base)
	merged := []wstate.Element{base[0], base[1], {Depth: 2, Role: "edit", Name: "Email"}, {Depth: 2, Role: "button", Name: "Sign in"}, base[2]}
	if err := c.SplicePage(context.Background(), 7, base, merged, []int{0, 1, -1, -1, 2}, map[int]any{2: "e", 3: "b"}); err != nil {
		t.Fatal(err)
	}
	if len(released()) != 0 {
		t.Errorf("released %v", released())
	}
	for i, want := range []any{nil, nil, "e", "b", nil} {
		if got, err := c.ElementOrigin(context.Background(), 7, i); err != nil || got != want {
			t.Errorf("origin %d = %v, %v; want %v", i, got, err, want)
		}
	}
	if el, ok := c.store.element(defaultAgent, 7, 4); !ok || el != comPtr(&bar) {
		t.Errorf("index 4 holds %v", el)
	}
	if i, err := c.ResolveElementRef(context.Background(), 7, "name:Email"); err != nil || i != 2 {
		t.Errorf("name:Email = %d, %v", i, err)
	}
	if els, ok := c.StoredElements(context.Background(), 7); !ok || len(els) != 5 || els[4].Name != "infobar" {
		t.Errorf("stored %+v", els)
	}
}

// TestSplicePageLeavesAStaleDocumentOut: the merge leaves out a stale
// Tab's document with its descendants (page.DocRule); their live Elements
// are released, and the Elements around them keep theirs at their new
// indexes.
func TestSplicePageLeavesAStaleDocumentOut(t *testing.T) {
	c, released := spliceClient(t)
	var win, stale, old, bar, doc, edit int
	base := []wstate.Element{
		{Depth: 0, Role: "window"}, {Depth: 1, Role: "document", Name: "Example Domain"}, {Depth: 2, Role: "text", Name: "old"},
		{Depth: 1, Role: "toolbar"}, {Depth: 1, Role: "document"}, {Depth: 2, Role: "edit", Name: "address"},
	}
	ptrs := []comPtr{comPtr(&win), comPtr(&stale), comPtr(&old), comPtr(&bar), comPtr(&doc), comPtr(&edit)}
	c.store.install(defaultAgent, 7, c.store.beginRead(), ptrs, base)
	// The document at 4 is the Tab's, with one UIA descendant replaced; the
	// one at 1 is stale.
	merged := []wstate.Element{base[0], base[3], base[4], {Depth: 2, Role: "heading", Name: "page"}}
	if err := c.SplicePage(context.Background(), 7, base, merged, []int{0, 3, 4, -1}, map[int]any{3: "h"}); err != nil {
		t.Fatal(err)
	}
	if r := released(); len(r) != 3 || r[0] != comPtr(&stale) || r[1] != comPtr(&old) || r[2] != comPtr(&edit) {
		t.Errorf("released %v", r)
	}
	for i, want := range []comPtr{comPtr(&win), comPtr(&bar), comPtr(&doc)} {
		if el, ok := c.store.element(defaultAgent, 7, i); !ok || el != want {
			t.Errorf("index %d holds %v", i, el)
		}
	}
	if o, err := c.ElementOrigin(context.Background(), 7, 3); err != nil || o != "h" {
		t.Errorf("origin 3 = %v, %v", o, err)
	}
	if _, err := c.ResolveElementRef(context.Background(), 7, "name:Example Domain"); err == nil {
		t.Error("the stale document still resolves by name")
	}

	// A mapping out of order, or past the frame read, changes nothing.
	c.store.install(defaultAgent, 7, c.store.beginRead(), ptrs, base)
	before := len(released())
	for _, from := range [][]int{{0, 3, 3, -1}, {0, 4, 3, -1}, {0, 3, 6, -1}, {0, 3, 4}} {
		if err := c.SplicePage(context.Background(), 7, base, merged, from, nil); err == nil || errors.Is(err, ErrSuperseded) {
			t.Errorf("from %v: %v", from, err)
		}
	}
	if len(released()) != before {
		t.Error("a refused splice released Elements")
	}
}
