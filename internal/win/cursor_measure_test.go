package win

// The Agent Cursor's frame rate and CPU cost on the real desktop
// (cursor-look 03): opt-in, never part of the default run. It shows the
// cursor's windows near the primary monitor's top-left corner for about
// fifteen seconds — no input is injected, no window activated — and logs
// for each phase the frames drawn per second, the UI thread's drawing time
// per frame and the process's CPU time per second.
//
//	WINUSE_CURSOR_MEASURE=1 go test ./internal/win/ -run TestAgentCursorFrameRate -v -count=1

import (
	"bytes"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// processCPU is the process's user plus kernel time so far.
func processCPU(t *testing.T) time.Duration {
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exit, &kernel, &user); err != nil {
		t.Fatal(err)
	}
	ft := func(f windows.Filetime) time.Duration {
		return time.Duration(int64(f.HighDateTime)<<32|int64(f.LowDateTime)) * 100
	}
	return ft(kernel) + ft(user)
}

func TestAgentCursorFrameRate(t *testing.T) {
	if os.Getenv("WINUSE_CURSOR_MEASURE") != "1" {
		t.Skip("live measurement: set WINUSE_CURSOR_MEASURE=1 to show the Agent Cursor and measure it")
	}
	if err := SetPerMonitorV2DPIAwareness(); err != nil {
		t.Fatalf("DPI awareness: %v", err)
	}
	saved := cursorLiveGuard
	cursorLiveGuard = func() error { return nil }
	defer func() { cursorLiveGuard = saved }()

	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

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
	_ = th.do(func() { c.ui.trace = true })

	window := Rect{Left: 0, Top: 0, Right: 900, Bottom: 600}
	targets := []struct {
		r     Rect
		label string
	}{
		{Rect{Left: 700, Top: 80, Right: 780, Bottom: 112}, `click "Save"`},
		{Rect{Left: 60, Top: 420, Right: 140, Bottom: 452}, `click "Open"`},
		{Rect{Left: 520, Top: 500, Right: 600, Bottom: 532}, `click "Close"`},
	}
	phase := func(name string, d time.Duration, f func()) {
		cpu0, t0 := processCPU(t), time.Now()
		f()
		time.Sleep(time.Until(t0.Add(d)))
		cpu, el := processCPU(t)-cpu0, time.Since(t0)
		t.Logf("%s: CPU %.1f ms per second (%.2f %% of one core) over %v",
			name, float64(cpu.Microseconds())/1000/el.Seconds(), 100*cpu.Seconds()/el.Seconds(), el.Round(time.Millisecond))
	}
	travels := func() {
		for _, tg := range targets {
			c.Travel(window, tg.r, tg.label)
			c.Click(tg.r)
			time.Sleep(450 * time.Millisecond)
		}
	}
	phase("travel and click, three times, first (the arrow's images made)", 3600*time.Millisecond, travels)
	phase("travel and click, three times, again", 3600*time.Millisecond, travels)
	phase("resting, floating", 4*time.Second, func() {})
	_ = th.do(func() {
		c.ui.model.lastActive = time.Now().Add(-cursorIdleDelay) // fade now
		c.ui.render(time.Now())
	})
	time.Sleep(400 * time.Millisecond)
	phase("hidden after the idle fade", 3*time.Second, func() {})
	var fast int
	_ = th.do(func() { fast = fastFrames })
	if fast != 0 {
		t.Errorf("hidden, %d cursors still hold the raised timer resolution", fast)
	}
	re := regexp.MustCompile(`cursor trace: .*`)
	for _, l := range re.FindAllString(logs.String(), -1) {
		t.Log(strings.TrimSpace(l))
	}
}
