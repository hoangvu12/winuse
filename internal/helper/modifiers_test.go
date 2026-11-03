package helper

// Modifiers on clicks and wheel scrolls (desktop-parity ticket 06): the
// route per form, the multi-select route and its refusal, the held keys
// released on every exit, and the posted key state. Everything is faked.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// modSpy is a postSpy whose element side also multi-selects and whose
// modifier hold is recorded.
type modSpy struct {
	*postSpy
	modes    []uia.MultiMode
	multiErr error
	held     []win.Modifiers
	mods     []win.Modifiers // the modifiers PostClick received
}

func (m *modSpy) MultiSelect(ctx context.Context, hwnd uint64, index int, mode uia.MultiMode) (int, error) {
	m.step("multi-select")
	m.modes = append(m.modes, mode)
	return 1, m.multiErr
}

func newModSpy() *modSpy {
	m := &modSpy{postSpy: newPostSpy()}
	m.svc.Actions = m
	m.svc.HoldModifiers = func(mods win.Modifiers) (func() error, error) {
		m.step("hold " + mods.String())
		m.held = append(m.held, mods)
		return func() error { m.step("release " + mods.String()); return nil }, nil
	}
	click := m.svc.PostClick
	m.svc.PostClick = func(t uint64, cx, cy int32, b win.MouseButton, n int, mods win.Modifiers) (int, error) {
		m.mods = append(m.mods, mods)
		return click(t, cx, cy, b, n, mods)
	}
	return m
}

func TestMultiSelectClicks(t *testing.T) {
	two := 2
	for _, tc := range []struct {
		mods []string
		mode uia.MultiMode
	}{{[]string{"ctrl"}, uia.MultiToggle}, {[]string{"shift"}, uia.MultiRange}} {
		m := newModSpy()
		out, e := runCommand(t, CommandsWith(m.svc), "click", protocol.ClickArgs{Window: "101", ElementIndex: &two, Modifiers: tc.mods})
		if e != nil || out != okOutput || len(m.modes) != 1 || m.modes[0] != tc.mode {
			t.Errorf("click 2 --modifiers %v = %q, %v, modes %v", tc.mods, out, e, m.modes)
		}
		m.noV1(t, "a multi-select click")
		if len(m.held) != 0 {
			t.Errorf("a Headless multi-select held keys: %v", m.held)
		}
	}

	m := newModSpy()
	m.multiErr = uia.ErrNotMultiSelect
	out, e := runCommand(t, CommandsWith(m.svc), "click", protocol.ClickArgs{Window: "101", ElementIndex: &two, Modifiers: []string{"ctrl"}})
	wantHeadedRefusal(t, "ctrl+click on a single-select item", out, e)
	if e != nil && !strings.HasSuffix(e.Hint, "winuse --headed click 101 2 --modifiers ctrl") {
		t.Errorf("hint %q, want the exact retry", e.Hint)
	}
}

func TestModifierClicksWithoutARouteAreRefused(t *testing.T) {
	two := 2
	cases := []struct {
		name  string
		args  protocol.ClickArgs
		retry string
	}{
		{"ctrl at a point", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Modifiers: []string{"ctrl"}}, "winuse --headed click 101 @40,50 --modifiers ctrl"},
		{"ctrl+shift on an item", protocol.ClickArgs{Window: "101", ElementIndex: &two, Modifiers: []string{"ctrl", "shift"}}, "winuse --headed click 101 2 --modifiers ctrl,shift"},
		{"alt on an item", protocol.ClickArgs{Window: "101", ElementIndex: &two, Modifiers: []string{"alt"}}, "winuse --headed click 101 2 --modifiers alt"},
		{"ctrl double click", protocol.ClickArgs{Window: "101", ElementIndex: &two, ClickCount: 2, Modifiers: []string{"ctrl"}}, "winuse --headed click 101 2 --count 2 --modifiers ctrl"},
	}
	for _, tc := range cases {
		m := newModSpy()
		out, e := runCommand(t, CommandsWith(m.svc), "click", tc.args)
		wantHeadedRefusal(t, tc.name, out, e)
		if e != nil && !strings.HasSuffix(e.Hint, tc.retry) {
			t.Errorf("%s: hint %q, want %q", tc.name, e.Hint, tc.retry)
		}
		if len(m.modes) != 0 || len(m.held) != 0 || len(m.asked) != 0 {
			t.Errorf("%s: ran multi-select %v, held %v, hit-tested %v", tc.name, m.modes, m.held, m.asked)
		}
	}
	m := newModSpy()
	_, e := runCommand(t, CommandsWith(m.svc), "click", protocol.ClickArgs{Window: "101", ElementIndex: &two, Modifiers: []string{"win"}})
	if e == nil || !strings.Contains(e.Msg, "unknown modifier") {
		t.Errorf("an unknown modifier = %v", e)
	}
}

func TestHeadedModifierClickHoldsAndReleases(t *testing.T) {
	m := newModSpy()
	two := 2
	if _, e := runCommand(t, CommandsWith(m.svc), "click", protocol.ClickArgs{Window: "101", ElementIndex: &two, Modifiers: []string{"ctrl"}, Headed: true}); e != nil {
		t.Fatal(e.Error())
	}
	steps := strings.Join(m.recorded(), ",")
	if !strings.Contains(steps, "activate,") || !strings.Contains(steps, "hold ctrl,click,release ctrl") {
		t.Errorf("steps %s, want the click between the hold and the release, after the activation", steps)
	}

	// The release comes even when the click fails.
	m = newModSpy()
	m.clickErr = errors.New("the pointer did not reach (1, 2)")
	if _, e := runCommand(t, CommandsWith(m.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Modifiers: []string{"shift"}, Headed: true}); e == nil {
		t.Fatal("a failed click must fail")
	}
	if steps := strings.Join(m.recorded(), ","); !strings.Contains(steps, "release shift") {
		t.Errorf("steps %s: the modifier was not released after a failed click", steps)
	}
}

func TestPostedModifierClicks(t *testing.T) {
	m := newModSpy()
	out, e := runCommand(t, CommandsWith(m.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Post: true, Modifiers: []string{"ctrl"}})
	if e != nil || out != postedClickOutput || len(m.mods) != 1 || !m.mods[0].Ctrl {
		t.Errorf("click --post --modifiers ctrl = %q, %v, mods %+v", out, e, m.mods)
	}
	m = newModSpy()
	out, e = runCommand(t, CommandsWith(m.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Post: true, Modifiers: []string{"alt"}})
	if out != "" || e == nil || !strings.Contains(e.Msg, "cannot carry Alt") || len(m.clicks) != 0 {
		t.Errorf("click --post --modifiers alt = %q, %v, posted %v", out, e, m.clicks)
	}
}

func TestModifierScroll(t *testing.T) {
	m := newModSpy()
	out, e := runCommand(t, CommandsWith(m.svc), "scroll", protocol.ScrollArgs{Window: "101", X: 5, Y: 6, ScrollY: -120, Modifiers: []string{"ctrl"}})
	wantHeadedRefusal(t, "scroll with ctrl", out, e)
	if e != nil && !strings.HasSuffix(e.Hint, "winuse --headed scroll 101 @5,6 0 -120 --modifiers ctrl") {
		t.Errorf("hint %q", e.Hint)
	}

	m = newModSpy()
	m.svc.Scroll = func(target uint64, x, y, dx, dy int32) error { m.step("wheel"); return nil }
	if _, e := runCommand(t, CommandsWith(m.svc), "scroll", protocol.ScrollArgs{Window: "101", X: 5, Y: 6, ScrollY: -120, Modifiers: []string{"ctrl"}, Headed: true}); e != nil {
		t.Fatal(e.Error())
	}
	if steps := strings.Join(m.recorded(), ","); !strings.Contains(steps, "hold ctrl,wheel,release ctrl") {
		t.Errorf("steps %s, want the wheel between the hold and the release", steps)
	}

	m = newModSpy()
	two := 2
	_, e = runCommand(t, CommandsWith(m.svc), "scroll", protocol.ScrollArgs{Window: "101", ElementIndex: &two, Direction: "down", Modifiers: []string{"ctrl"}})
	if e == nil || !strings.Contains(e.Msg, "--modifiers goes with an @x,y point") {
		t.Errorf("an Element scroll with modifiers = %v", e)
	}
}
