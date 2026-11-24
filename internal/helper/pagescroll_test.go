package helper

// Say when a Page scroll moved nothing (.scratch/bladebro-ideas ticket 03):
// the note after a Headless Page scroll's `ok`, from the wheel's watch
// (page.ArmScroll) on pageRig's cdptest fake, whose isolated world answers
// the watch's read with the scrollers before and after the wheel. No
// browser runs.

import (
	"image"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
)

// scrollRead is the watch's read answer with a scroll report.
func scrollRead(before, after map[string]any) map[string]any {
	return map[string]any{"scroll": map[string]any{"before": before, "after": after}}
}

// pagePos is a read of the top document only: y of maxY, x of maxX.
func pagePos(y, maxY, x, maxX float64) map[string]any {
	return map[string]any{"y": y, "maxY": maxY, "x": x, "maxX": maxX}
}

// withV adds a vertical scroller under the wheel point to a read.
func withV(pos map[string]any, el string, at, max float64) map[string]any {
	out := map[string]any{"v": map[string]any{"el": el, "at": at, "max": max}}
	for k, v := range pos {
		out[k] = v
	}
	return out
}

func withH(pos map[string]any, el string, at, max float64) map[string]any {
	out := map[string]any{"h": map[string]any{"el": el, "at": at, "max": max}}
	for k, v := range pos {
		out[k] = v
	}
	return out
}

func TestPageScrollSaysWhenNothingMoved(t *testing.T) {
	const list = `<div id="list">`
	cases := []struct {
		name  string
		dir   string
		final map[string]any
		want  string
	}{
		{"moved", "down", scrollRead(pagePos(0, 900, 0, 0), pagePos(300, 900, 0, 0)), "ok\n"},
		{"moved to the bottom", "down", scrollRead(pagePos(700, 900, 0, 0), pagePos(900, 900, 0, 0)),
			"ok\nNote: the Page is now at its bottom\n"},
		{"moved to the top", "up", scrollRead(pagePos(200, 900, 0, 0), pagePos(0, 900, 0, 0)),
			"ok\nNote: the Page is now at its top\n"},
		{"at the bottom", "down", scrollRead(pagePos(900, 900, 0, 0), pagePos(900, 900, 0, 0)),
			"ok\nNote: nothing scrolled; the Page is at its bottom\n"},
		{"at the bottom, a fraction off", "down", scrollRead(pagePos(899.5, 900.4, 0, 0), pagePos(899.5, 900.4, 0, 0)),
			"ok\nNote: nothing scrolled; the Page is at its bottom\n"},
		{"at the top", "up", scrollRead(pagePos(0, 900, 0, 0), pagePos(0, 900, 0, 0)),
			"ok\nNote: nothing scrolled; the Page is at its top\n"},
		{"a short Page", "down", scrollRead(pagePos(0, 0, 0, 0), pagePos(0, 0, 0, 0)),
			"ok\nNote: nothing scrolled; the Page does not scroll\n"},
		{"a nested scroller at its end, the Page held", "down",
			scrollRead(withV(pagePos(100, 900, 0, 0), list, 500, 500), withV(pagePos(100, 900, 0, 0), list, 500, 500)),
			"ok\nNote: nothing scrolled; " + list + " is at its end\n"},
		{"a nested scroller at its start", "up",
			scrollRead(withV(pagePos(100, 900, 0, 0), list, 0, 500), withV(pagePos(100, 900, 0, 0), list, 0, 500)),
			"ok\nNote: nothing scrolled; " + list + " is at its start\n"},
		{"a nested scroller and the Page both at their end", "down",
			scrollRead(withV(pagePos(900, 900, 0, 0), list, 500, 500), withV(pagePos(900, 900, 0, 0), list, 500, 500)),
			"ok\nNote: nothing scrolled; " + list + " is at its end, and the Page is at its bottom\n"},
		{"the nested scroller moved", "down",
			scrollRead(withV(pagePos(100, 900, 0, 0), list, 100, 500), withV(pagePos(100, 900, 0, 0), list, 400, 500)), "ok\n"},
		{"the nested scroller moved to its end", "down",
			scrollRead(withV(pagePos(100, 900, 0, 0), list, 300, 500), withV(pagePos(100, 900, 0, 0), list, 500, 500)),
			"ok\nNote: " + list + " is now at its end\n"},
		{"a carousel scrolled sideways", "right",
			scrollRead(withH(pagePos(0, 900, 0, 0), `<ul class="carousel">`, 0, 800), withH(pagePos(0, 900, 0, 0), `<ul class="carousel">`, 300, 800)), "ok\n"},
		{"at the right edge", "right", scrollRead(pagePos(0, 900, 400, 400), pagePos(0, 900, 400, 400)),
			"ok\nNote: nothing scrolled; the Page is at its right edge\n"},
		{"no sideways scrolling", "left", scrollRead(pagePos(0, 900, 0, 0), pagePos(0, 900, 0, 0)),
			"ok\nNote: nothing scrolled; the Page does not scroll sideways\n"},
		{"held by the Page itself", "down", scrollRead(pagePos(100, 900, 0, 0), pagePos(100, 900, 0, 0)),
			"ok\nNote: nothing scrolled where the wheel landed\n"},
		{"no report", "down", map[string]any{}, "ok\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newPageRig(t)
			ew := r.effectWorld(t, c.final, nil)
			out, e, effect := r.runEffect(t, "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: c.dir}, false)
			if e != nil || out != c.want {
				t.Errorf("output %q, %+v; want %q", out, e, c.want)
			}
			if effect != "" {
				t.Errorf("a scroll recorded the effect %q", effect)
			}
			// Single dispatch: one wheel, whatever was seen.
			if n := r.wheels(); n != 1 {
				t.Errorf("%d wheels sent: %v", n, r.sent())
			}
			if got := ew.point(); len(got) != 3 || got[0] != 35.0 || got[1] != 25.0 || got[2] != true {
				t.Errorf("the watch was armed with %v, want the wheel point 35,25 in scroll mode", got)
			}
			r.noForeground(t)
		})
	}
}

// The @x,y form arms the watch at the wheel's point in CSS px.
func TestPageScrollAtAPointSaysWhenNothingMoved(t *testing.T) {
	r := newPageRig(t)
	r.svc.Shots.Record(agent.DefaultID, browserWindowID, shot.Mapping{Scale: 1, Width: 2000, Height: 2000, Coverage: image.Rect(0, 0, 2000, 2000)})
	ew := r.effectWorld(t, scrollRead(pagePos(900, 900, 0, 0), pagePos(900, 900, 0, 0)), nil)
	out, e := r.run(t, "scroll", protocol.ScrollArgs{Window: "301", X: 400, Y: 500, ScrollY: 240})
	if want := "ok\nNote: nothing scrolled; the Page is at its bottom\n"; e != nil || out != want {
		t.Errorf("output %q, %+v; want %q", out, e, want)
	}
	// ((400 - 100) / 1.5, (500 - 200) / 1.5) = (200, 200).
	if got := ew.point(); len(got) != 3 || got[0] != 200.0 || got[1] != 200.0 || got[2] != true {
		t.Errorf("the watch was armed with %v, want the wheel point 200,200 in scroll mode", got)
	}
	if n := r.wheels(); n != 1 {
		t.Errorf("%d wheels sent: %v", n, r.sent())
	}
}

// into-view says when the Element was already in view: its box did not
// move.
func TestPageScrollIntoViewSaysAlreadyInView(t *testing.T) {
	r := newPageRig(t)
	r.set(func() { r.rects = [][]float64{{10, 20, 50, 10, 1.5}, {10, 20, 50, 10, 1.5}} })
	out, e := r.run(t, "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: "into-view"})
	if want := "ok\nNote: element 7 was already in view; nothing scrolled\n"; e != nil || out != want {
		t.Errorf("already in view: %q, %+v; want %q", out, e, want)
	}

	r = newPageRig(t)
	r.set(func() { r.rects = [][]float64{{10, 1900, 50, 10, 1.5}, {10, 20, 50, 10, 1.5}} })
	out, e = r.run(t, "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: "into-view"})
	if e != nil || out != okOutput {
		t.Errorf("scrolled into view: %q, %+v; want a plain ok", out, e)
	}
	if strings.Contains(out, "already") {
		t.Errorf("a scroll that moved the box said it was in view: %q", out)
	}
}

// wheels counts the wheel events the Page got.
func (r *pageRig) wheels() int {
	n := 0
	for _, s := range r.sent() {
		if strings.HasPrefix(s, "dispatchMouseEvent mouseWheel") {
			n++
		}
	}
	return n
}
