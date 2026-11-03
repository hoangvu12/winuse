package uia

import (
	"context"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// store keeps the live Elements behind each Window's latest Window State,
// one per Agent and Window (multi-agent ticket 03, ADR 0014 Decision 2):
// another Agent's read of a Window never changes what this Agent's
// Element Indexes mean. Requests that name no Agent share the default
// Agent's Window States, which is the single store winuse had before. It
// is client-level (not worker-level): live UIA elements belong to the
// process's MTA, so they stay valid when a wedged worker is replaced. All
// COM calls on them still go through Run, which guarantees a live worker.
//
// Replacements are generation-aware: every read takes a sequence number
// when it starts (beginRead) and may install its Elements only when it is
// newer than what is stored (install). A read whose job completes after a
// newer read stored its Elements (the wedged-worker case, where an
// abandoned job's provider call eventually returns late) releases its own
// Elements instead, so it can neither clobber the newer Window State the
// agent actually saw nor release that state's Elements under a running
// Action resolving an Element Index. Abandoned runs are refused outright
// (installRead, finding 5).
//
// Hand-outs are AddRef'd (element): a newer install releases only the
// stored reference, so an Element an Action is using cannot be freed
// under it; the Action releases its own reference on the worker thread
// when it is done (finding 4).
//
// Memory (ADR 0014, Consequences): an Agent other than the default one
// that has not used the store for idle loses its Window States, and the
// store holds at most maxStates Window States, dropping the least
// recently used. Both happen on install, which runs on a COM thread, so
// the dropped Elements are released there.
type store struct {
	mu      sync.Mutex
	states  map[storeKey]*windowState
	used    map[string]time.Time // each Agent's last use of the store
	next    uint64               // read sequence counter
	addRef  func(comPtr)         // addRef, injectable for tests
	release func(comPtr)         // release, injectable for tests

	now       func() time.Time // the clock; injectable for tests
	idle      time.Duration    // an Agent idle this long loses its Window States; 0 never
	maxStates int              // the most Window States stored across Agents
}

// storeKey is one Agent's Window.
type storeKey struct {
	agent string
	hwnd  uint64
}

// defaultAgent is the store key of requests that name no Agent.
const defaultAgent = agent.DefaultID

// Store limits: an Agent idle this long loses its Window States (the
// helper's default idle period, after which a single-Agent helper exits
// anyway), and at most this many Window States are kept.
const (
	defaultAgentIdle = 5 * time.Minute
	defaultMaxStates = 256
)

// windowState is one Window's stored live Elements, the Window State
// they back (its Elements, the name:/id: reference space of ticket 11),
// and the read that produced them.
type windowState struct {
	seq      uint64
	elems    []comPtr
	elements []wstate.Element
	origins  map[int]any // the Page Elements' origins by Element Index (splice.go); nil for a UIA-only read
	used     time.Time   // the last time the Agent installed or used it
}

func newStore(addRef, release func(comPtr)) *store {
	return &store{
		states:    map[storeKey]*windowState{},
		used:      map[string]time.Time{},
		addRef:    addRef,
		release:   release,
		now:       time.Now,
		idle:      defaultAgentIdle,
		maxStates: defaultMaxStates,
	}
}

// storeAgent is the store key of the Agent a request comes from.
func storeAgent(ctx context.Context) string {
	return agent.Key(ctx)
}

// SetAgentIdle sets how long an Agent may stay idle before the store
// drops its Window States (the helper passes its own idle period); 0
// keeps them for the helper's lifetime. The default Agent never loses
// them this way.
func (c *Client) SetAgentIdle(d time.Duration) {
	c.store.mu.Lock()
	c.store.idle = d
	c.store.mu.Unlock()
}

// touch marks the Agent and its state as used now. Callers hold s.mu.
func (s *store) touch(agent string, st *windowState) time.Time {
	now := s.now()
	s.used[agent] = now
	if st != nil {
		st.used = now
	}
	return now
}

// beginRead allocates the sequence number of a tree read that is starting.
func (s *store) beginRead() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return s.next
}

// install stores elems (and the Window State elements they back) as the
// Agent's live Elements of hwnd when seq is newer than the stored read's,
// releasing the previous Elements. A superseded read (an abandoned
// worker's job completing after a newer read) has its elems released
// instead; the newer Window State is left untouched. Then the idle
// Agents' Window States and those past the cap are dropped. Callers run
// on a COM thread: the worker whose walk produced the Elements.
func (s *store) install(agent string, hwnd uint64, seq uint64, elems []comPtr, elements []wstate.Element) {
	key := storeKey{agent, hwnd}
	s.mu.Lock()
	st := s.states[key]
	if st != nil && seq <= st.seq {
		s.mu.Unlock()
		for _, p := range elems {
			s.release(p)
		}
		return
	}
	var old []comPtr
	if st != nil {
		old = st.elems
	}
	if len(elems) == 0 {
		delete(s.states, key)
		s.touch(agent, nil)
	} else {
		st = &windowState{seq: seq, elems: elems, elements: elements}
		s.states[key] = st
		s.touch(agent, st)
	}
	old = append(old, s.evict(key)...)
	s.mu.Unlock()
	for _, p := range old {
		s.release(p)
	}
}

// evict drops the Window States of the Agents idle past s.idle (never the
// default Agent's) and then the least recently used ones past
// s.maxStates, never keep's, and returns their Elements for release.
// Callers hold s.mu.
func (s *store) evict(keep storeKey) []comPtr {
	var dropped []comPtr
	drop := func(k storeKey) {
		dropped = append(dropped, s.states[k].elems...)
		delete(s.states, k)
	}
	if s.idle > 0 {
		cutoff := s.now().Add(-s.idle)
		for a, at := range s.used {
			if a == defaultAgent || a == keep.agent || !at.Before(cutoff) {
				continue
			}
			for k := range s.states {
				if k.agent == a {
					drop(k)
				}
			}
			delete(s.used, a)
		}
	}
	for s.maxStates > 0 && len(s.states) > s.maxStates {
		var oldest storeKey
		var oldestAt time.Time
		found := false
		for k, st := range s.states {
			if k == keep {
				continue
			}
			if !found || st.used.Before(oldestAt) {
				oldest, oldestAt, found = k, st.used, true
			}
		}
		if !found {
			break
		}
		drop(oldest)
	}
	return dropped
}

// lookupState is the Agent's stored state of hwnd, marked used; nil when
// the Agent has none. Callers hold s.mu.
func (s *store) lookupState(agent string, hwnd uint64) *windowState {
	st := s.states[storeKey{agent, hwnd}]
	s.touch(agent, st)
	return st
}

// element returns the live element for an Element Index of the Agent's
// latest Window State of hwnd, AddRef'd for the caller: a newer install
// releases the stored reference, and without a reference of its own the
// caller's Element could be freed while it still uses it. The caller
// releases it when done (on the worker thread, through the same store).
func (s *store) element(agent string, hwnd uint64, index int) (comPtr, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.lookupState(agent, hwnd)
	if st == nil || index < 0 || index >= len(st.elems) {
		return nil, false
	}
	el := st.elems[index]
	if el != nil {
		s.addRef(el)
	}
	return el, true
}

// elements returns every live element of the Agent's latest Window State
// of hwnd, each AddRef'd for the caller (nil entries stay nil), with the
// Window State Elements they back: the point hit-test's input (point.go,
// headless-max ticket 11). The caller releases each non-nil element.
func (s *store) elements(agent string, hwnd uint64) ([]comPtr, []wstate.Element, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.lookupState(agent, hwnd)
	if st == nil {
		return nil, nil, false
	}
	out := make([]comPtr, len(st.elems))
	for i, el := range st.elems {
		if el != nil {
			s.addRef(el)
			out[i] = el
		}
	}
	return out, st.elements, true
}

// state returns the Elements of the Agent's latest stored Window State of
// hwnd: the space name:/id: references resolve against (ticket 11). Pure
// metadata: no COM, no worker; the live Element behind a resolved Index
// is probed by the Action's own step.
func (s *store) state(agent string, hwnd uint64) ([]wstate.Element, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.lookupState(agent, hwnd)
	if st == nil {
		return nil, false
	}
	return st.elements, true
}

// has reports whether the Agent has a stored Window State of hwnd
// (distinguishing a Window with no state at all from an index beyond the
// stored one).
func (s *store) has(agent string, hwnd uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.states[storeKey{agent, hwnd}]
	return ok
}

// noState is the error for an Agent with no Window State of hwnd: plain
// ErrNoState, or a NotReadByAgentError (which matches ErrNoState too) when
// another Agent has read the Window (ADR 0014 Decision 2: each Agent
// reads a Window before it acts on it).
func (s *store) noState(agent string, hwnd uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.noStateLocked(agent, hwnd)
}

// noStateLocked is noState for callers that hold s.mu.
func (s *store) noStateLocked(agent string, hwnd uint64) error {
	for k := range s.states {
		if k.hwnd == hwnd && k.agent != agent {
			return &NotReadByAgentError{WindowID: hwnd}
		}
	}
	return errNoState(hwnd)
}

// releaseAll drops every stored Window of every Agent. Called on the
// worker thread.
func (s *store) releaseAll() {
	s.mu.Lock()
	all := s.states
	s.states = map[storeKey]*windowState{}
	s.used = map[string]time.Time{}
	s.mu.Unlock()
	for _, st := range all {
		for _, p := range st.elems {
			s.release(p)
		}
	}
}
