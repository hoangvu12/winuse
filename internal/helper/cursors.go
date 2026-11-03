package helper

// One Agent Cursor per Agent (multi-agent ticket 04, ADR 0014 Decision 4,
// like cua's agent cursor). Each Agent gets its own cursor windows, in its
// own colour (WINUSE_CURSOR_COLOR, or the next free one of the palette),
// with its name ahead of the action in the label pill, such as
// `reviewer: click "Save"`. The default Agent keeps the one cursor of
// before, in the colour of before and with no name. Every cursor sits
// just above the Window its Action targets rather than topmost
// (win.TravelAbove), fades after its idle delay like before, and closes
// when its Agent has been idle as long as the helper's idle period (the
// Agent is gone). winuse's own captures hide them all.
//
// The set is the Services.Cursor the helper runs with: as a Cursor it is
// the default Agent's cursor, and its HideWhile hides every Agent's. The
// Actions reach their own Agent's cursor through cursorFor.

import (
	"context"
	"image/color"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/win"
)

// agentCursor is one Agent's cursor windows: *win.AgentCursor, faked in
// the offline tests.
type agentCursor interface {
	TravelAbove(anchor uint64, origin, target win.Rect, label string)
	ClickAbove(anchor uint64, target win.Rect)
	CaretAbove(anchor uint64, x, top, bottom int32) // the insertion caret (multi-agent ticket 11)
	HideWhile(f func())
	Close()
}

var _ agentCursor = (*win.AgentCursor)(nil)

// maxAgentCursors caps the cursors alive at once; the least recently
// used Agent's closes first.
const maxAgentCursors = 16

// cursorSet holds the default Agent's cursor and one cursor per other
// Agent, created on the Agent's first Headless Action.
type cursorSet struct {
	base   Cursor                                       // the default Agent's cursor
	create func(accent color.RGBA) (agentCursor, error) // a new cursor's windows, on the UI thread
	now    func() time.Time
	idle   time.Duration // an Agent idle this long is gone: its cursor closes; 0 never

	mu     sync.Mutex
	agents map[string]*agentCursorEntry
}

type agentCursorEntry struct {
	cursor agentCursor
	color  color.RGBA
	label  string
	used   time.Time
}

// newCursorSet wraps the default Agent's cursor. create makes another
// Agent's cursor in its colour.
func newCursorSet(base Cursor, create func(color.RGBA) (agentCursor, error), idle time.Duration) *cursorSet {
	return &cursorSet{base: base, create: create, now: time.Now, idle: idle, agents: map[string]*agentCursorEntry{}}
}

var _ Cursor = (*cursorSet)(nil)

// Travel is the default Agent's (see Cursor).
func (s *cursorSet) Travel(origin, target win.Rect, label string) {
	s.base.Travel(origin, target, label)
}

// Click is the default Agent's.
func (s *cursorSet) Click(target win.Rect) { s.base.Click(target) }

// HideWhile runs f with every Agent's cursor hidden: winuse's own
// captures never show any of them (ADR 0006).
func (s *cursorSet) HideWhile(f func()) {
	s.mu.Lock()
	all := make([]agentCursor, 0, len(s.agents))
	for _, e := range s.agents {
		all = append(all, e.cursor)
	}
	s.mu.Unlock()
	run := f
	for _, c := range all {
		inner, c := run, c
		run = func() { c.HideWhile(inner) }
	}
	s.base.HideWhile(run)
}

// Close closes every Agent's cursor (the default one is the helper's own
// to close).
func (s *cursorSet) Close() {
	s.mu.Lock()
	all := s.agents
	s.agents = map[string]*agentCursorEntry{}
	s.mu.Unlock()
	for _, e := range all {
		e.cursor.Close()
	}
}

// cursorFor is the Cursor an Action of who on the Window anchor shows:
// who's own cursor, placed just above anchor and naming who in its label.
// The default Agent's is the cursor of before, placed the same way. When
// who's cursor cannot be made, the default one stands in.
func (s *cursorSet) cursorFor(who agent.Identity, anchor uint64) Cursor {
	if who.ID == agent.DefaultID {
		if c, ok := s.base.(agentCursor); ok {
			return anchoredCursor{c: c, anchor: anchor}
		}
		return s.base
	}
	e := s.entry(who)
	if e == nil {
		return s.cursorFor(agent.Identity{ID: agent.DefaultID}, anchor)
	}
	return anchoredCursor{c: e.cursor, anchor: anchor, name: e.label}
}

// entry is who's cursor, made on first use. It closes the cursors of the
// Agents idle past s.idle, and the least recently used one past the cap.
func (s *cursorSet) entry(who agent.Identity) *agentCursorEntry {
	now := s.now()
	s.mu.Lock()
	gone := s.sweep(now, who.ID)
	e := s.agents[who.ID]
	if e != nil {
		e.used = now
		s.mu.Unlock()
		closeAll(gone)
		return e
	}
	accent := s.pickColor(who.Color)
	s.mu.Unlock()
	closeAll(gone)

	c, err := s.create(accent)
	if err != nil {
		log.Printf("the Agent Cursor of %s is unavailable, its Actions show the default one: %v", who.Label, err)
		return nil
	}
	e = &agentCursorEntry{cursor: c, color: accent, label: cursorName(who), used: now}
	s.mu.Lock()
	if other := s.agents[who.ID]; other != nil {
		// Another request of the same Agent made one meanwhile: keep it.
		s.mu.Unlock()
		c.Close()
		return other
	}
	s.agents[who.ID] = e
	gone = s.capped(who.ID)
	s.mu.Unlock()
	closeAll(gone)
	return e
}

// sweep removes the Agents idle past s.idle (never keep) and returns
// their cursors to close. Callers hold s.mu.
func (s *cursorSet) sweep(now time.Time, keep string) []agentCursor {
	if s.idle <= 0 {
		return nil
	}
	var gone []agentCursor
	for id, e := range s.agents {
		if id != keep && now.Sub(e.used) >= s.idle {
			gone = append(gone, e.cursor)
			delete(s.agents, id)
		}
	}
	return gone
}

// capped removes the least recently used Agents past maxAgentCursors
// (never keep) and returns their cursors to close. Callers hold s.mu.
func (s *cursorSet) capped(keep string) []agentCursor {
	var gone []agentCursor
	for len(s.agents) > maxAgentCursors {
		oldest, found := "", false
		for id, e := range s.agents {
			if id != keep && (!found || e.used.Before(s.agents[oldest].used)) {
				oldest, found = id, true
			}
		}
		if !found {
			break
		}
		gone = append(gone, s.agents[oldest].cursor)
		delete(s.agents, oldest)
	}
	return gone
}

func closeAll(cs []agentCursor) {
	for _, c := range cs {
		c.Close()
	}
}

// pickColor is a new Agent's colour: the one it asked for
// (WINUSE_CURSOR_COLOR), else the first palette colour no live Agent
// has, else the palette's colours again in turn. Callers hold s.mu.
func (s *cursorSet) pickColor(asked string) color.RGBA {
	if c, ok := win.ParseCursorColor(asked); ok {
		return c
	}
	taken := map[color.RGBA]bool{}
	for _, e := range s.agents {
		taken[e.color] = true
	}
	for _, c := range win.CursorPalette {
		if !taken[c] {
			return c
		}
	}
	return win.CursorPalette[len(s.agents)%len(win.CursorPalette)]
}

// cursorName is the name an Agent's cursor shows ahead of each action:
// its WINUSE_AGENT name, or its Harness's program name ("claude" for
// "claude 17048"); its colour tells two sessions of one Harness apart.
func cursorName(who agent.Identity) string {
	if strings.HasPrefix(who.ID, "name:") {
		return who.Label
	}
	if f := strings.Fields(who.Label); len(f) > 0 {
		return f[0]
	}
	return who.Label
}

// anchoredCursor is one Agent's cursor for one Action: placed just above
// the Action's Window, with the Agent's name ahead of the label.
type anchoredCursor struct {
	c      agentCursor
	anchor uint64
	name   string // "" for the default Agent
}

func (a anchoredCursor) Travel(origin, target win.Rect, label string) {
	if a.name != "" && label != "" {
		label = a.name + ": " + label
	} else if a.name != "" {
		label = a.name
	}
	a.c.TravelAbove(a.anchor, origin, target, label)
}

func (a anchoredCursor) Click(target win.Rect) { a.c.ClickAbove(a.anchor, target) }

func (a anchoredCursor) HideWhile(f func()) { a.c.HideWhile(f) }

// Caret draws the insertion caret above the Action's Window (caret.go).
func (a anchoredCursor) Caret(x, top, bottom int32) { a.c.CaretAbove(a.anchor, x, top, bottom) }

// cursorFor is the Cursor the requesting Agent's Action on w shows (see
// cursorSet.cursorFor); without a cursor set (offline tests, a helper
// built without one) it is Services.Cursor as it is.
func (s Services) cursorFor(ctx context.Context, w win.WindowInfo) Cursor {
	if set, ok := s.Cursor.(*cursorSet); ok {
		return set.cursorFor(AgentOf(ctx), w.ID)
	}
	return s.Cursor
}
