package helper

// Live re-resolution's helper tests (desktop-parity 17): a reference the
// cache answered acts with no note; one a fresh read found acts on the
// Element Index it was added under and says so once; a fresh read that
// finds nothing, or several, refuses with the wording and candidates of
// that read. The resolver is a fake over the headless spy.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// liveSpy is the headless spy with a live resolver.
type liveSpy struct {
	*headlessSpy
	res    uia.LiveResolution
	err    error
	refs   []string
	budget time.Duration
}

func (l *liveSpy) ResolveElementRefLive(ctx context.Context, hwnd uint64, ref string, budget time.Duration) (uia.LiveResolution, error) {
	l.refs = append(l.refs, ref)
	l.budget = budget
	return l.res, l.err
}

func newLiveSpy() *liveSpy {
	l := &liveSpy{headlessSpy: newHeadlessSpy()}
	l.svc.Actions = l
	return l
}

func TestLiveRefFoundInAFreshRead(t *testing.T) {
	l := newLiveSpy()
	l.res = uia.LiveResolution{Index: 245, Fresh: true}
	out, e := runCommand(t, CommandsWith(l.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Save"})
	if e != nil {
		t.Fatalf("click: %+v", e)
	}
	want := "ok\nNote: found name:Save in a fresh read of window 101, as Element Index 245; the Window State you have is unchanged\n"
	if out != want || l.hIndex != 245 {
		t.Errorf("output %q (clicked %d), want %q on 245", out, l.hIndex, want)
	}
	if l.budget <= 0 || l.budget > liveRefBudget {
		t.Errorf("fresh-read budget %s", l.budget)
	}

	// The cache answered: no note.
	l = newLiveSpy()
	l.res = uia.LiveResolution{Index: 7}
	if out, e := runCommand(t, CommandsWith(l.svc), "click", protocol.ClickArgs{Window: "101", Ref: "id:save"}); e != nil || out != "ok\n" || l.hIndex != 7 {
		t.Errorf("cache hit: %q, %+v, clicked %d", out, e, l.hIndex)
	}
}

func TestLiveRefNoteOncePerBatch(t *testing.T) {
	l := newLiveSpy()
	l.res = uia.LiveResolution{Index: 245, Fresh: true}
	out, e := runCommand(t, CommandsWith(l.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{Ref: "name:Save"}),
		stepOf("click", protocol.ClickArgs{Ref: "name:Save"}),
	}})
	if e != nil || strings.Count(out, "found name:Save in a fresh read") != 1 || len(l.refs) != 2 {
		t.Errorf("batch: %q, %+v, %d resolutions", out, e, len(l.refs))
	}
}

func TestLiveRefRefusals(t *testing.T) {
	l := newLiveSpy()
	l.err = &uia.LiveRefError{Err: &wstate.NoMatchRefError{Kind: wstate.RefName, Text: "Save"}}
	_, e := runCommand(t, CommandsWith(l.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Save"})
	if e == nil || e.Msg != `no element named "Save" in window 101, in its cached Window State or a fresh read; nothing was sent` || !strings.Contains(e.Hint, "winuse find 101") {
		t.Errorf("no match: %+v", e)
	}
	l = newLiveSpy()
	l.err = &uia.LiveRefError{Err: &wstate.AmbiguousRefError{Kind: wstate.RefName, Text: "Twin", Matches: []int{12, 13},
		Candidates: []string{"12 button Twin ID: twin-a", "13 button Twin ID: twin-b"}}}
	_, e = runCommand(t, CommandsWith(l.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Twin"})
	if e == nil || e.Msg != `name "Twin" matches 2 elements in a fresh read of window 101; nothing was sent` ||
		!strings.Contains(e.Hint, "12 button Twin ID: twin-a\n13 button Twin ID: twin-b") {
		t.Errorf("ambiguous: %+v", e)
	}
	if l.hIndex != 0 || strings.Contains(strings.Join(l.recorded(), ","), "headless-click") {
		t.Errorf("a refused reference acted: %v", l.recorded())
	}
	// The cache's own ambiguity keeps its wording.
	l = newLiveSpy()
	l.err = &wstate.AmbiguousRefError{Kind: wstate.RefName, Text: "Twin", Matches: []int{9, 10}, Candidates: []string{"9 button Twin", "10 button Twin"}}
	if _, e = runCommand(t, CommandsWith(l.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Twin"}); e == nil || !strings.Contains(e.Msg, "in the cached Window State for window 101") {
		t.Errorf("cache ambiguity: %+v", e)
	}
}

// storeSpy is a live resolver that remembers what a fresh read added, as
// the UIA client's store does: the first resolution of a reference is a
// fresh read, every later one a cache hit. It also answers the Page
// splicer's calls, so the lane classification routes on the reference
// (pageStep) before the command runs.
type storeSpy struct {
	*headlessSpy
	stored map[string]bool
}

func (s *storeSpy) ResolveElementRefLive(ctx context.Context, hwnd uint64, ref string, budget time.Duration) (uia.LiveResolution, error) {
	fresh := !s.stored[ref]
	s.stored[ref] = true
	return uia.LiveResolution{Index: 245, Fresh: fresh}, nil
}

// ResolveElementRef is the cache alone: what a fresh read stored.
func (s *storeSpy) ResolveElementRef(ctx context.Context, hwnd uint64, ref string) (int, error) {
	if s.stored[ref] {
		return 245, nil
	}
	return 0, &wstate.NoMatchRefError{}
}

func (s *storeSpy) SplicePage(ctx context.Context, hwnd uint64, base, merged []wstate.Element, from []int, origins map[int]any) error {
	return nil
}

func (s *storeSpy) ElementOrigin(ctx context.Context, hwnd uint64, index int) (any, error) {
	return nil, nil // a UIA Element, not the Page's
}

// The live TestNameReferenceAfterHelperRestart failure: the lane
// classification resolved the reference in a fresh read first, so the
// command found it cached and printed no note. The classification now
// resolves against the cache alone.
func TestLiveRefNoteSurvivesTheLaneClassification(t *testing.T) {
	s := &storeSpy{headlessSpy: newHeadlessSpy(), stored: map[string]bool{}}
	s.svc.Actions = s
	out, e := runCommand(t, CommandsWith(s.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Save"})
	want := "ok\nNote: found name:Save in a fresh read of window 101, as Element Index 245; the Window State you have is unchanged\n"
	if e != nil || out != want || s.hIndex != 245 {
		t.Errorf("click: %q, %+v (clicked %d), want %q on 245", out, e, s.hIndex, want)
	}
	if out, e := runCommand(t, CommandsWith(s.svc), "click", protocol.ClickArgs{Window: "101", Ref: "name:Save"}); e != nil || out != "ok\n" {
		t.Errorf("the second click, a cache hit: %q, %+v, want ok alone", out, e)
	}
}
