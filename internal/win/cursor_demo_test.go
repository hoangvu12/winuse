package win

// An opt-in visual demo of the Agent Cursor for a human to watch: it
// shows the real windows on the real desktop (no input is injected, no
// window is activated). Skipped unless WINUSE_CURSOR_DEMO=1; never part
// of the default run or the e2e suite.
//
//	WINUSE_CURSOR_DEMO=1 go test ./internal/win/ -run TestAgentCursorVisualDemo -v -count=1
//
// With WINUSE_CURSOR_DEMO_SHOTS=<dir> the cursor's capture exclusion is
// turned off and the demo saves screen crops of each moment there (mid
// travel, arrival, the click, after it, inside HideWhile, before and
// after the idle fade), so the drawing can be checked without watching.
// Two backdrop panels, one #202020 and one light, show the cursor on a
// dark-themed and a light app whatever the desktop shows.

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestAgentCursorVisualDemo(t *testing.T) {
	if os.Getenv("WINUSE_CURSOR_DEMO") != "1" {
		t.Skip("visual demo: set WINUSE_CURSOR_DEMO=1 to watch the Agent Cursor on the desktop")
	}
	if err := SetPerMonitorV2DPIAwareness(); err != nil {
		t.Fatalf("DPI awareness: %v", err)
	}
	saved := cursorLiveGuard
	cursorLiveGuard = func() error { return nil }
	defer func() { cursorLiveGuard = saved }()

	shots := os.Getenv("WINUSE_CURSOR_DEMO_SHOTS")
	if shots != "" {
		if err := os.MkdirAll(shots, 0o755); err != nil {
			t.Fatalf("creating %s: %v", shots, err)
		}
		cursorExcludeFromCapture = false
		defer func() { cursorExcludeFromCapture = true }()
	}

	th, err := startUIThread(func() error { return nil })
	if err != nil {
		t.Fatalf("starting the UI thread: %v", err)
	}
	defer th.close()
	c, err := NewAgentCursor(th.do)
	if err != nil {
		t.Fatalf("NewAgentCursor: %v", err)
	}
	defer c.Close()

	// The primary monitor (SM_CXSCREEN, SM_CYSCREEN) stands in for the
	// target Window.
	w, h := systemMetric(0), systemMetric(1)
	window := Rect{Right: w, Bottom: h}
	box := func(fx, fy float64, bw, bh int32) Rect {
		x, y := int32(fx*float64(w)), int32(fy*float64(h))
		return Rect{Left: x - bw/2, Top: y - bh/2, Right: x + bw/2, Bottom: y + bh/2}
	}
	area := func(fx0, fy0, fx1, fy1 float64) Rect {
		return Rect{Left: int32(fx0 * float64(w)), Top: int32(fy0 * float64(h)), Right: int32(fx1 * float64(w)), Bottom: int32(fy1 * float64(h))}
	}

	// Two backdrops — a dark-theme #202020 panel and a light one — so the
	// cursor is seen on both whatever the desktop shows. They are windows
	// like the cursor's own: click-through, never activated, under it.
	var backdrops []*layeredWin
	defer func() {
		_ = th.do(func() {
			for _, b := range backdrops {
				b.destroy()
			}
		})
	}()
	for _, bd := range []struct {
		r   Rect
		rgb uint32
	}{
		{area(0.04, 0.55, 0.40, 0.90), 0x202020},
		{area(0.60, 0.55, 0.92, 0.90), 0xF3F3F3},
	} {
		_ = th.do(func() {
			hMod, _, _ := procGetModuleHandleW.Call(0)
			cls, _ := windows.UTF16PtrFromString(AgentCursorClassName)
			l, err := newLayeredWin(hMod, cls)
			if err != nil || !l.resize(bd.r.Width(), bd.r.Height()) {
				t.Logf("backdrop: %v", err)
				return
			}
			for i := range l.pix.pix {
				l.pix.pix[i] = 0xFF000000 | bd.rgb
			}
			l.present(bd.r.Left, bd.r.Top, 1)
			backdrops = append(backdrops, l)
		})
	}

	// shot saves a crop around r (padded, clamped to the monitor).
	n := 0
	shot := func(name string, r Rect) {
		if shots == "" {
			return
		}
		const pad = 280
		crop := Rect{Left: max32(r.Left-pad, 0), Top: max32(r.Top-pad, 0), Right: min32(r.Right+pad, w), Bottom: min32(r.Bottom+pad, h)}
		img, err := captureScreenRegion(crop)
		if err != nil {
			t.Logf("capturing %s: %v", name, err)
			return
		}
		n++
		path := filepath.Join(shots, fmt.Sprintf("%02d-%s.png", n, name))
		f, err := os.Create(path)
		if err != nil {
			t.Logf("saving %s: %v", path, err)
			return
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Logf("encoding %s: %v", path, err)
		}
	}

	steps := []struct {
		target Rect
		label  string
	}{
		{box(0.25, 0.3, 120, 32), `click "Save"`},
		{box(0.6, 0.35, 260, 28), `set-value "File name"`},
		{box(0.45, 0.45, 24, 24), `toggle "Remember me"`},
		{box(0.18, 0.66, 140, 32), `click "Dark theme" (on #202020)`},
		{box(0.72, 0.66, 140, 32), `click "Light theme"`},
		{box(0.97, 0.5, 60, 30), `click "Near the right edge" (the pill flips left)`},
		{box(0.5, 0.97, 80, 24), `expand "Near the bottom" (the pill flips up)`},
		{box(0.5, 0.5, 600, 400), `select "A very long label that is cut short with an ellipsis because it is far wider than the pill may grow"`},
	}
	if cw, ok := c.ui.view.(*cursorWin); ok {
		_ = th.do(func() { t.Logf("label font: %q", cw.labelFace()) })
	}
	// at waits until d after since and takes a shot there, logging when
	// it really was (a capture takes a few milliseconds of its own).
	at := func(since time.Time, d time.Duration, name string, r Rect) {
		time.Sleep(time.Until(since.Add(d)))
		taken := time.Since(since)
		shot(name, r)
		t.Logf("%s: taken %v after its start", name, taken.Round(time.Millisecond))
	}
	for i, s := range steps {
		step := fmt.Sprintf("step%d", i+1)
		prev := window
		if i > 0 {
			prev = steps[i-1].target
		}
		// The first step shoots the two pops mid-way instead of the
		// travel, so no capture is in flight when they play.
		pops := i == 0
		mid := make(chan struct{})
		go func() {
			defer close(mid)
			if pops {
				return
			}
			time.Sleep(150 * time.Millisecond)
			shot(step+"-mid-travel", unionRect(prev, s.target))
		}()
		start := time.Now()
		c.Travel(window, s.target, s.label)
		arrived := time.Now() // Travel returns on the first frame after the arrival
		t.Logf("travel to %+v took %v", s.target, arrived.Sub(start))
		<-mid
		if pops {
			at(arrived, 40*time.Millisecond, step+"-label-pop-40ms", s.target)
			at(arrived, 100*time.Millisecond, step+"-label-pop-100ms", s.target)
		}
		at(arrived, 240*time.Millisecond, step+"-arrived", s.target)
		clicked := time.Now()
		c.Click(s.target)
		if pops {
			at(clicked, 40*time.Millisecond, step+"-highlight-pop-40ms", s.target)
			at(clicked, 120*time.Millisecond, step+"-highlight-pop-120ms", s.target)
			at(clicked, 400*time.Millisecond, step+"-click-settled", s.target)
		} else {
			at(clicked, 120*time.Millisecond, step+"-click", s.target)
		}
		at(clicked, 820*time.Millisecond, step+"-after", s.target)
	}
	last := steps[len(steps)-1].target
	t.Log("HideWhile: hidden for one second")
	c.HideWhile(func() {
		time.Sleep(100 * time.Millisecond)
		shot("hidden-during-capture", last)
		time.Sleep(900 * time.Millisecond)
	})
	time.Sleep(200 * time.Millisecond)
	shot("restored", last)
	t.Logf("restored; waiting for the %v idle fade", cursorIdleDelay)
	time.Sleep(cursorIdleDelay / 2)
	shot("idle-half-way", last)
	time.Sleep(cursorIdleDelay/2 + time.Second)
	shot("idle-faded", last)
	t.Log("faded; the next travel starts at the centre again")
	ok := box(0.8, 0.2, 100, 30)
	c.Travel(window, ok, `click "OK"`)
	c.Click(ok)
	time.Sleep(120 * time.Millisecond)
	shot("fresh-start-click", ok)
	time.Sleep(1500 * time.Millisecond)
}
