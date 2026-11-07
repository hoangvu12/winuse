package uia

// Window States per Agent (multi-agent ticket 03, ADR 0014 Decision 2):
// one Agent's read never changes another's Element Indexes, an Agent
// that has not read a Window is told so plainly, and the store drops idle
// Agents' states and the least recently used past its cap. Pure store
// logic: fake pointers, no COM.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// agentCtx is a request context of the named Agent.
func agentCtx(name string) context.Context {
	return agent.NewContext(context.Background(), agent.Identity{ID: name, Label: name})
}

// fakeStore is a store whose releases are counted per pointer and whose
// clock the test moves.
type fakeStore struct {
	*store
	released map[comPtr]int
	clock    time.Time
}

func newFakeStore() *fakeStore {
	f := &fakeStore{released: map[comPtr]int{}, clock: time.Unix(1000, 0)}
	f.store = newStore(func(comPtr) {}, func(p comPtr) { f.released[p]++ })
	f.store.now = func() time.Time { return f.clock }
	return f
}

func ptrs(n int) []comPtr {
	out := make([]comPtr, n)
	for i := range out {
		v := i
		out[i] = comPtr(&v)
	}
	return out
}

func TestEachAgentKeepsItsOwnWindowState(t *testing.T) {
	f := newFakeStore()
	c := &Client{store: f.store}
	a, b := ptrs(2), ptrs(2)
	f.install("A", 101, f.beginRead(), a, []wstate.Element{{Name: "Save"}, {Name: "Open"}}, false)
	f.install("B", 101, f.beginRead(), b, []wstate.Element{{Name: "Open"}, {Name: "Save"}}, false)

	// A reads, B reads, A clicks 1: A's Element, not B's.
	if el, err := c.lookup(agentCtx("A"), 101, 1); err != nil || el != a[1] {
		t.Errorf("A's element 1 = %v, %v, want A's own", el, err)
	}
	if el, err := c.lookup(agentCtx("B"), 101, 1); err != nil || el != b[1] {
		t.Errorf("B's element 1 = %v, %v, want B's own", el, err)
	}
	if i, err := c.ResolveElementRef(agentCtx("A"), 101, "name:Save"); err != nil || i != 0 {
		t.Errorf("A's name:Save = %d, %v, want 0", i, err)
	}
	if i, err := c.ResolveElementRef(agentCtx("B"), 101, "name:Save"); err != nil || i != 1 {
		t.Errorf("B's name:Save = %d, %v, want 1", i, err)
	}

	// B reading again releases only B's previous Elements.
	f.install("B", 101, f.beginRead(), ptrs(1), []wstate.Element{{Name: "New"}}, false)
	if f.released[a[0]] != 0 || f.released[a[1]] != 0 || f.released[b[0]] != 1 || f.released[b[1]] != 1 {
		t.Errorf("released = %v, want B's old Elements only", f.released)
	}
	if els, ok := c.StoredElements(agentCtx("A"), 101); !ok || len(els) != 2 {
		t.Errorf("A's Window State = %v, %v, want its two Elements", els, ok)
	}
}

func TestTheDefaultAgentIsTheOneStoreOfBefore(t *testing.T) {
	f := newFakeStore()
	c := &Client{store: f.store}
	p := ptrs(1)
	// A request with no Agent on its context is the default Agent.
	f.install(defaultAgent, 101, f.beginRead(), p, []wstate.Element{{Name: "OK"}}, false)
	if el, err := c.lookup(context.Background(), 101, 0); err != nil || el != p[0] {
		t.Errorf("lookup without an Agent = %v, %v, want the default Agent's Element", el, err)
	}
}

func TestAnAgentThatHasNotReadTheWindowIsToldSo(t *testing.T) {
	f := newFakeStore()
	c := &Client{store: f.store}
	_, err := c.lookup(agentCtx("B"), 101, 0)
	var other *NotReadByAgentError
	if !errors.Is(err, ErrNoState) || errors.As(err, &other) {
		t.Errorf("nobody read the Window: err = %v, want plain ErrNoState", err)
	}

	f.install("A", 101, f.beginRead(), ptrs(1), []wstate.Element{{Name: "OK"}}, false)
	for name, err := range map[string]error{
		"lookup":  func() error { _, err := c.lookup(agentCtx("B"), 101, 0); return err }(),
		"ref":     func() error { _, err := c.ResolveElementRef(agentCtx("B"), 101, "name:OK"); return err }(),
		"origin":  func() error { _, err := c.ElementOrigin(agentCtx("B"), 101, 0); return err }(),
		"default": func() error { _, err := c.lookup(context.Background(), 101, 0); return err }(),
	} {
		if !errors.Is(err, ErrNoState) || !errors.As(err, &other) || other.WindowID != 101 {
			t.Errorf("%s: err = %v, want a NotReadByAgentError for window 101 that matches ErrNoState", name, err)
		}
	}
}

func TestIdleAgentsLoseTheirWindowStates(t *testing.T) {
	f := newFakeStore()
	f.idle = 5 * time.Minute
	a, d := ptrs(1), ptrs(1)
	f.install("A", 101, f.beginRead(), a, []wstate.Element{{}}, false)
	f.install(defaultAgent, 101, f.beginRead(), d, []wstate.Element{{}}, false)

	f.clock = f.clock.Add(6 * time.Minute)
	f.install("B", 202, f.beginRead(), ptrs(1), []wstate.Element{{}}, false)
	if f.has("A", 101) || f.released[a[0]] != 1 {
		t.Errorf("A, idle 6 minutes, kept its Window State (released %d)", f.released[a[0]])
	}
	if !f.has(defaultAgent, 101) || f.released[d[0]] != 0 {
		t.Error("the default Agent lost its Window State: it never idles out")
	}

	// An Agent that keeps using its state is not idle.
	f.clock = f.clock.Add(4 * time.Minute)
	f.element("B", 202, 0)
	f.clock = f.clock.Add(4 * time.Minute)
	f.install("C", 303, f.beginRead(), ptrs(1), []wstate.Element{{}}, false)
	if !f.has("B", 202) {
		t.Error("B used its state 4 minutes ago and lost it")
	}
}

func TestTheStoreKeepsAtMostMaxStates(t *testing.T) {
	f := newFakeStore()
	f.maxStates = 2
	first := ptrs(1)
	f.install("A", 1, f.beginRead(), first, []wstate.Element{{}}, false)
	f.clock = f.clock.Add(time.Second)
	f.install("B", 2, f.beginRead(), ptrs(1), []wstate.Element{{}}, false)
	f.clock = f.clock.Add(time.Second)
	f.install("A", 3, f.beginRead(), ptrs(1), []wstate.Element{{}}, false)
	if f.has("A", 1) || f.released[first[0]] != 1 {
		t.Error("the least recently used Window State was kept past the cap")
	}
	if !f.has("B", 2) || !f.has("A", 3) {
		t.Error("the cap dropped a newer Window State")
	}
}
