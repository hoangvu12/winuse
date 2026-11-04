package helper

// One Agent Cursor per Agent (multi-agent ticket 04): the default Agent
// keeps the cursor of before; every other Agent gets its own, in its own
// colour, naming it in the label and placed above the Action's Window;
// captures hide them all; an idle Agent's cursor closes. The cursors are
// fakes; no window is created.

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/win"
)

// fakeAgentCursor records what one Agent's cursor was asked to do.
type fakeAgentCursor struct {
	mu      sync.Mutex
	accent  color.RGBA
	log     []string
	anchors []uint64
	labels  []string
	carets  [][3]int32
	closed  bool
}

func (c *fakeAgentCursor) record(s string) { c.mu.Lock(); c.log = append(c.log, s); c.mu.Unlock() }

func (c *fakeAgentCursor) TravelAbove(anchor uint64, origin, target win.Rect, label string) {
	c.mu.Lock()
	c.anchors = append(c.anchors, anchor)
	c.labels = append(c.labels, label)
	c.mu.Unlock()
	c.record("travel")
}

func (c *fakeAgentCursor) ClickAbove(anchor uint64, target win.Rect) {
	c.mu.Lock()
	c.anchors = append(c.anchors, anchor)
	c.mu.Unlock()
	c.record("click")
}

func (c *fakeAgentCursor) HideWhile(f func()) { c.record("hide"); f(); c.record("show") }

func (c *fakeAgentCursor) CaretAbove(anchor uint64, x, top, bottom int32) {
	c.mu.Lock()
	c.anchors = append(c.anchors, anchor)
	c.carets = append(c.carets, [3]int32{x, top, bottom})
	c.mu.Unlock()
	c.record("caret")
}

func (c *fakeAgentCursor) Close() { c.mu.Lock(); c.closed = true; c.mu.Unlock() }

// Travel and Click make the fake usable as the default Cursor too.
func (c *fakeAgentCursor) Travel(origin, target win.Rect, label string) {
	c.TravelAbove(0, origin, target, label)
}
func (c *fakeAgentCursor) Click(target win.Rect) { c.ClickAbove(0, target) }

// fakeCursorSet is a cursorSet over fakes, with a clock the test moves.
type fakeCursorSet struct {
	*cursorSet
	base    *fakeAgentCursor
	made    []*fakeAgentCursor
	failFor map[color.RGBA]bool
	clock   time.Time
}

func newFakeCursorSet(idle time.Duration) *fakeCursorSet {
	f := &fakeCursorSet{base: &fakeAgentCursor{accent: win.DefaultCursorColor()}, clock: time.Unix(1000, 0), failFor: map[color.RGBA]bool{}}
	f.cursorSet = newCursorSet(f.base, func(accent color.RGBA) (agentCursor, error) {
		if f.failFor[accent] {
			return nil, errors.New("no UI thread")
		}
		c := &fakeAgentCursor{accent: accent}
		f.made = append(f.made, c)
		return c, nil
	}, idle)
	f.cursorSet.now = func() time.Time { return f.clock }
	return f
}

func named(name string) agent.Identity {
	return agent.Identity{ID: "name:" + name, Label: name}
}

// TestAgentTabGroup (browser-ux 10): each Agent's new Tabs join a group
// named like its Agent Cursor, in the tab group colour nearest its
// cursor's; the default Agent keeps the "winuse" group, cyan like its cursor.
func TestAgentTabGroup(t *testing.T) {
	f := newFakeCursorSet(time.Minute)
	s := Services{Cursor: f.cursorSet}
	ctx := context.Background()
	if g := s.agentTabGroup(ctx); g != (page.TabGroup{Title: "winuse", Color: "cyan"}) {
		t.Errorf("default Agent: %+v", g)
	}
	f.cursorFor(named("reviewer"), 1) // its first Action: purple, the palette's first
	if g := s.agentTabGroup(withAgent(ctx, named("reviewer"))); g != (page.TabGroup{Title: "reviewer", Color: "purple"}) {
		t.Errorf("reviewer: %+v", g)
	}
	claude := agent.Identity{ID: "proc:17048", Label: "claude 17048"}
	f.cursorFor(claude, 1) // pink, the palette's second
	if g := s.agentTabGroup(withAgent(ctx, claude)); g != (page.TabGroup{Title: "claude", Color: "pink"}) {
		t.Errorf("a Harness Agent: %+v", g)
	}
	// No cursor yet: the colour it asked for, else the default's.
	painter := agent.Identity{ID: "name:painter", Label: "painter", Color: "#F0524F"}
	if g := s.agentTabGroup(withAgent(ctx, painter)); g != (page.TabGroup{Title: "painter", Color: "red"}) {
		t.Errorf("painter: %+v", g)
	}
	if g := s.agentTabGroup(withAgent(ctx, named("new"))); g != (page.TabGroup{Title: "new", Color: "cyan"}) {
		t.Errorf("no colour yet: %+v", g)
	}
}

func TestTheDefaultAgentKeepsTheCursorOfBefore(t *testing.T) {
	f := newFakeCursorSet(time.Minute)
	c := f.cursorFor(agent.Identity{ID: agent.DefaultID, Label: agent.DefaultID}, 101)
	c.Travel(win.Rect{}, win.Rect{Right: 10, Bottom: 10}, `click "Save"`)
	c.Click(win.Rect{Right: 10, Bottom: 10})
	if len(f.made) != 0 {
		t.Errorf("the default Agent made %d new cursors, want its own of before", len(f.made))
	}
	if got := f.base.labels; len(got) != 1 || got[0] != `click "Save"` {
		t.Errorf("default labels = %q, want no Agent name", got)
	}
	if got := f.base.anchors; len(got) != 2 || got[0] != 101 || got[1] != 101 {
		t.Errorf("default anchors = %v, want the Action's Window both times", got)
	}

	// A plain Cursor (the offline fakes) stands as it is.
	spy := &cursorSpy{share: func(string) {}}
	set := newCursorSet(spy, nil, 0)
	if got := set.cursorFor(agent.Identity{ID: agent.DefaultID}, 101); got != Cursor(spy) {
		t.Errorf("cursorFor(default) over a plain Cursor = %T, want the Cursor itself", got)
	}
}

func TestEachAgentGetsItsOwnCursorColourAndName(t *testing.T) {
	f := newFakeCursorSet(time.Minute)
	f.cursorFor(named("reviewer"), 101).Travel(win.Rect{}, win.Rect{Right: 9, Bottom: 9}, `click "Save"`)
	f.cursorFor(named("reviewer"), 202).Click(win.Rect{Right: 9, Bottom: 9})
	f.cursorFor(agent.Identity{ID: "proc:17048:30", Label: "claude 17048"}, 303).Travel(win.Rect{}, win.Rect{Right: 9, Bottom: 9}, "scroll down")
	f.cursorFor(agent.Identity{ID: "name:painter", Label: "painter", Color: "#112233"}, 404).Travel(win.Rect{}, win.Rect{Right: 9, Bottom: 9}, "")

	if len(f.made) != 3 {
		t.Fatalf("made %d cursors, want one per Agent (3)", len(f.made))
	}
	reviewer, claude, painter := f.made[0], f.made[1], f.made[2]
	if reviewer.accent != win.CursorPalette[0] || claude.accent != win.CursorPalette[1] {
		t.Errorf("colours = %v, %v, want the palette's first two", reviewer.accent, claude.accent)
	}
	if want := (color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}); painter.accent != want {
		t.Errorf("WINUSE_CURSOR_COLOR #112233 gave %v", painter.accent)
	}
	if got := reviewer.labels; len(got) != 1 || got[0] != `reviewer: click "Save"` {
		t.Errorf("reviewer's labels = %q", got)
	}
	if got := reviewer.anchors; len(got) != 2 || got[0] != 101 || got[1] != 202 {
		t.Errorf("reviewer's anchors = %v, want each Action's Window", got)
	}
	if got := claude.labels; len(got) != 1 || got[0] != "claude: scroll down" {
		t.Errorf("an automatic Agent's labels = %q, want its program name ahead", got)
	}
	if got := painter.labels; len(got) != 1 || got[0] != "painter" {
		t.Errorf("an Action with no label shows %q, want the Agent's name", got)
	}
	if f.base.log != nil {
		t.Errorf("the default cursor moved for another Agent: %v", f.base.log)
	}
}

func TestPickColorSkipsTakenColoursAndIgnoresABadOption(t *testing.T) {
	f := newFakeCursorSet(time.Minute)
	f.cursorFor(agent.Identity{ID: "name:a", Label: "a", Color: "not a colour"}, 1)
	if f.made[0].accent != win.CursorPalette[0] {
		t.Errorf("an unreadable WINUSE_CURSOR_COLOR gave %v, want the palette's first", f.made[0].accent)
	}
	for i := range len(win.CursorPalette) + 2 {
		f.cursorFor(named(fmt.Sprintf("x%d", i)), 1)
	}
	seen := map[color.RGBA]int{}
	for _, c := range f.made[:len(win.CursorPalette)] {
		seen[c.accent]++
	}
	if len(seen) != len(win.CursorPalette) {
		t.Errorf("the first %d Agents share colours: %v", len(win.CursorPalette), seen)
	}
}

func TestCapturesHideEveryAgentsCursor(t *testing.T) {
	f := newFakeCursorSet(time.Minute)
	f.cursorFor(named("a"), 1)
	f.cursorFor(named("b"), 2)
	ran := 0
	f.HideWhile(func() {
		ran++
		for _, c := range append([]*fakeAgentCursor{f.base}, f.made...) {
			c.mu.Lock()
			hidden := len(c.log) > 0 && c.log[len(c.log)-1] == "hide"
			c.mu.Unlock()
			if !hidden {
				t.Errorf("a cursor was showing during the capture: %v", c.log)
			}
		}
	})
	if ran != 1 {
		t.Errorf("the capture ran %d times, want once", ran)
	}
}

func TestAnIdleAgentsCursorCloses(t *testing.T) {
	f := newFakeCursorSet(5 * time.Minute)
	f.cursorFor(named("a"), 1)
	f.clock = f.clock.Add(4 * time.Minute)
	f.cursorFor(named("b"), 2)
	if f.made[0].closed {
		t.Fatal("a's cursor closed after 4 minutes, under the idle period")
	}
	f.clock = f.clock.Add(2 * time.Minute)
	f.cursorFor(named("b"), 2)
	if !f.made[0].closed {
		t.Error("a, idle 6 minutes, still has its cursor")
	}
	if f.made[1].closed {
		t.Error("b's cursor closed while b works")
	}
	f.Close()
	if !f.made[1].closed || f.base.closed {
		t.Error("Close must close every Agent's cursor and leave the default one to the helper")
	}
}

func TestTheCursorCapClosesTheLeastRecentlyUsed(t *testing.T) {
	f := newFakeCursorSet(0)
	for i := range maxAgentCursors + 1 {
		f.clock = f.clock.Add(time.Second)
		f.cursorFor(named(fmt.Sprintf("a%d", i)), 1)
	}
	if !f.made[0].closed {
		t.Error("the least recently used Agent's cursor stayed open past the cap")
	}
	for _, c := range f.made[1:] {
		if c.closed {
			t.Error("a newer Agent's cursor closed")
		}
	}
}

func TestAnAgentWhoseCursorCannotBeMadeShowsTheDefaultOne(t *testing.T) {
	f := newFakeCursorSet(time.Minute)
	f.failFor[win.CursorPalette[0]] = true
	f.cursorFor(named("a"), 7).Travel(win.Rect{}, win.Rect{Right: 9, Bottom: 9}, "click")
	if got := f.base.labels; len(got) != 1 || got[0] != "click" {
		t.Errorf("default labels = %q, want the Action shown by the default cursor", got)
	}
}

func TestAHeadlessClickShowsItsAgentsCursor(t *testing.T) {
	spy := newHeadlessSpy()
	f := newFakeCursorSet(time.Minute)
	spy.svc.Cursor = f.cursorSet
	ctx := agent.NewContext(context.Background(), named("reviewer"))
	out, cmdErr := CommandsWith(spy.svc)["click"](ctx, clickArgs("101", 2, 1, "left"))
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("click: %q, %v", out, cmdErr)
	}
	if len(f.made) != 1 {
		t.Fatalf("made %d cursors, want reviewer's", len(f.made))
	}
	c := f.made[0]
	if strings.Join(c.log, " ") != "travel click" {
		t.Errorf("reviewer's cursor did %v, want travel then click", c.log)
	}
	if len(c.labels) != 1 || c.labels[0] != `reviewer: click "Save"` || c.anchors[0] != 101 {
		t.Errorf("labels %q anchors %v, want reviewer's click above window 101", c.labels, c.anchors)
	}
	if f.base.log != nil {
		t.Errorf("the default cursor moved for reviewer: %v", f.base.log)
	}
}
