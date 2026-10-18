package uia

// name:/id: reference resolution tests (ticket 11): the Client resolves
// references against the stored Window State (the Elements the latest
// read installed), with no COM and no worker, and a Window without a
// stored state fails as ErrNoState — the shared stale contract.

import (
	"context"
	"errors"
	"testing"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// installState seeds a Client's store with a Window State: the given
// elements back the given live pointers, through the real
// beginRead/install path.
func installState(c *Client, hwnd uint64, elems []comPtr, elements []wstate.Element) {
	c.store.install(hwnd, c.store.beginRead(), elems, elements)
}

// refState is the reference test tree: two Elements share a Name, one
// has a unique ID, one has an empty Name.
func refState() []wstate.Element {
	return []wstate.Element{
		{Depth: 0, Role: "window", Name: "Example", AutomationId: "w"},
		{Depth: 1, Role: "button", Name: "Twin", AutomationId: "twin-a"},
		{Depth: 1, Role: "button", Name: "Twin", AutomationId: "twin-b"},
		{Depth: 1, Role: "edit", Name: "", AutomationId: "searchBox"},
	}
}

func TestResolveElementRefAgainstStoredState(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	elements := refState()
	a, b, cc, d := 1, 2, 3, 4
	installState(c, 101, []comPtr{comPtr(&a), comPtr(&b), comPtr(&cc), comPtr(&d)}, elements)

	cases := []struct {
		ref       string
		wantIndex int
	}{
		{"name:Example", 0},
		{"id:twin-b", 2},
		{"id:searchBox", 3}, // an empty Name is still reachable by ID
	}
	for _, tc := range cases {
		index, err := c.ResolveElementRef(101, tc.ref)
		if err != nil {
			t.Errorf("ResolveElementRef(%q): %v", tc.ref, err)
			continue
		}
		if index != tc.wantIndex {
			t.Errorf("ResolveElementRef(%q) = %d, want %d", tc.ref, index, tc.wantIndex)
		}
		// The resolved Index must also be the one whose live Element the
		// store holds — the Element the Action's own step will probe.
		if _, ok := c.store.element(101, index); !ok {
			t.Errorf("the resolved index %d has no live element", index)
		}
	}
}

func TestResolveElementRefAmbiguousAndMissing(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	a, b, cc, d := 1, 2, 3, 4
	installState(c, 101, []comPtr{comPtr(&a), comPtr(&b), comPtr(&cc), comPtr(&d)}, refState())

	index, err := c.ResolveElementRef(101, "name:Twin")
	if index != 0 || err == nil {
		t.Fatalf("ambiguous reference resolved to %d, %v; it must never guess", index, err)
	}
	// The candidates come from the stored Window State's own lines.
	var amb *wstate.AmbiguousRefError
	if !errors.As(err, &amb) {
		t.Fatalf("error = %v (%T), want an AmbiguousRefError", err, err)
	}
	if len(amb.Candidates) != 2 || amb.Candidates[0] != "1 button Twin ID: twin-a" || amb.Candidates[1] != "2 button Twin ID: twin-b" {
		t.Errorf("candidates = %q", amb.Candidates)
	}

	index, err = c.ResolveElementRef(101, "name:OK")
	if index != 0 || err == nil {
		t.Fatalf("missing reference resolved to %d, %v; it must fail", index, err)
	}
	var miss *wstate.NoMatchRefError
	if !errors.As(err, &miss) {
		t.Fatalf("error = %v (%T), want a NoMatchRefError", err, err)
	}
}

func TestResolveElementRefNoStateAndReplacement(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()

	// A Window with no stored Window State: the shared no-state failure.
	if _, err := c.ResolveElementRef(101, "name:OK"); !errors.Is(err, ErrNoState) {
		t.Fatalf("error = %v, want ErrNoState", err)
	}

	// A new read replaces the Window State, so references resolve against
	// the Window State the agent now holds (ticket 03's replacement
	// contract, applied to the reference space).
	a, b := 1, 2
	installState(c, 101, []comPtr{comPtr(&a)}, []wstate.Element{
		{Depth: 0, Role: "window", Name: "Old", AutomationId: "w"},
	})
	installState(c, 101, []comPtr{comPtr(&a), comPtr(&b)}, []wstate.Element{
		{Depth: 0, Role: "window", Name: "New", AutomationId: "w"},
		{Depth: 1, Role: "button", Name: "OK", AutomationId: "ok"},
	})
	if index, err := c.ResolveElementRef(101, "name:OK"); err != nil || index != 1 {
		t.Errorf("ResolveElementRef(name:OK) after replacement = %d, %v, want 1, nil", index, err)
	}
	if _, err := c.ResolveElementRef(101, "name:Old"); err == nil {
		t.Error("a reference to the replaced Window State's name must fail")
	}
}

func TestResolveElementRefNeedsNoWorker(t *testing.T) {
	// Resolution is pure metadata: it must not need a live worker (a
	// wedged worker never blocks reference resolution).
	c := testClient(func() (*worker, error) { return nil, errors.New("no worker") })
	defer c.Close()
	a := 1
	installState(c, 101, []comPtr{comPtr(&a)}, []wstate.Element{
		{Depth: 0, Role: "window", Name: "Example", AutomationId: "w"},
	})
	if index, err := c.ResolveElementRef(101, "name:Example"); err != nil || index != 0 {
		t.Errorf("ResolveElementRef = %d, %v, want 0, nil with no worker at all", index, err)
	}
	// ElementPoint still needs one (the COM probe), proving the split.
	if _, err := c.ElementPoint(context.Background(), 101, 0); err == nil {
		t.Error("ElementPoint unexpectedly worked without a worker")
	}
}
