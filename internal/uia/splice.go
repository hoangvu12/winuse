package uia

// One Window State for a browser Window (browser merge ticket 04): the
// descendants of the UIA document Element are replaced by the Page read
// over CDP, and the live-Element store follows, so Element Indexes are
// numbered across both parts. A Page Element has no UIA Element behind it
// (a nil handle) and an origin instead, an opaque value the helper owns
// (page.Origin). A newer read of the Window replaces both, so a Page
// Element Index goes stale with its Window State like any other.

import (
	"context"
	"errors"
	"fmt"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// ErrPageElement reports a UIA Action given an Element Index of the Page:
// there is no UIA Element behind it. The helper routes Page Elements over
// the Browser Connection instead.
var ErrPageElement = errors.New("this Element is in the Page and has no UI Automation Element; winuse acts on it through the Browser Connection")

// ErrSuperseded reports a splice whose Window State is no longer the
// stored one: a newer read of the Window replaced it in between.
var ErrSuperseded = errors.New("the Window was read again meanwhile")

// SplicePage replaces hwnd's stored Window State with the merged one: the
// frame read base, with the UIA descendants of the document Element
// replaced by Page Elements and the stale Tabs' documents left out
// (page.Merge). from gives, for each Element of merged, the index in base
// whose live Element it keeps, or -1 for a Page Element, in increasing
// order; origins holds each Page Element's origin by its new Element
// Index. base must be the Elements the latest Capture of hwnd returned,
// still stored: otherwise ErrSuperseded and nothing changes. The UIA
// Elements merged leaves out are released on the worker thread.
// pagePartial says the Page read was cut: the merged Window State is
// partial when it or the frame read was.
func (c *Client) SplicePage(ctx context.Context, hwnd uint64, base, merged []wstate.Element, from []int, origins map[int]any, pagePartial bool) error {
	var jobErr error
	err := c.RunFor(ctx, hwnd, func(w *worker, r *run) {
		jobErr = c.store.splice(storeAgent(ctx), hwnd, base, merged, from, origins, pagePartial)
	})
	if err != nil {
		return err
	}
	return jobErr
}

// ElementOrigin says where an Element Index of hwnd's latest Window State
// comes from: nil for a UIA Element, the stored origin for a Page Element.
// A Window with no stored Window State fails as ErrNoState and an index
// outside it as ErrStale (the read-the-State-again recovery). No COM.
func (c *Client) ElementOrigin(ctx context.Context, hwnd uint64, index int) (any, error) {
	return c.store.origin(storeAgent(ctx), hwnd, index)
}

// splice is SplicePage's store half; it runs on a COM thread.
func (s *store) splice(agent string, hwnd uint64, base, merged []wstate.Element, from []int, origins map[int]any, pagePartial bool) error {
	s.mu.Lock()
	st := s.lookupState(agent, hwnd)
	if st == nil || !sameElements(st.elements, base) || len(st.elems) != len(base) {
		s.mu.Unlock()
		return ErrSuperseded
	}
	if len(from) != len(merged) {
		s.mu.Unlock()
		return fmt.Errorf("splicing the Page: %d Elements with %d frame indexes", len(merged), len(from))
	}
	elems := make([]comPtr, len(merged))
	kept := make([]bool, len(base))
	last := -1
	for i, f := range from {
		if f < 0 {
			continue
		}
		if f <= last || f >= len(base) {
			s.mu.Unlock()
			return fmt.Errorf("splicing the Page: frame index %d is out of order or outside the Window State", f)
		}
		elems[i], kept[f], last = st.elems[f], true, f
	}
	var removed []comPtr
	for i, p := range st.elems {
		if !kept[i] && p != nil {
			removed = append(removed, p)
		}
	}
	st.elems = elems
	st.elements = merged
	st.origins = origins
	st.base = len(merged)
	st.added, st.doubted = nil, nil // the added Elements were not in the merge: released above
	st.partial = st.partial || pagePartial
	s.mu.Unlock()
	for _, p := range removed {
		s.release(p)
	}
	return nil
}

// origin is ElementOrigin's store half.
func (s *store) origin(agent string, hwnd uint64, index int) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.lookupState(agent, hwnd)
	if st == nil {
		return nil, s.noStateLocked(agent, hwnd)
	}
	if index < 0 || index >= len(st.elems) {
		return nil, fmt.Errorf("%w: element %d is not in the cached Window State of window %d", ErrStale, index, hwnd)
	}
	if st.elems[index] != nil {
		return nil, nil
	}
	if o, ok := st.origins[index]; ok {
		return o, nil
	}
	return nil, fmt.Errorf("%w: element %d of window %d", ErrPageElement, index, hwnd)
}

// sameElements reports whether two Element lists are the same list (one
// backing array), which is how a Capture's result is told from a newer
// one.
func sameElements(a, b []wstate.Element) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return &a[0] == &b[0]
}
