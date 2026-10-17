package win

// The overlay's offline tests (ticket 13): the layout functions (border
// strips, pill placement and clipping — pure functions of the client
// rectangle) and the UI thread's job mechanics. No window is created and
// nothing is shown: the thread test pumps only thread messages.

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestOverlayBorderRectsFrameTheClientArea(t *testing.T) {
	cr := Rect{Left: 100, Top: 200, Right: 900, Bottom: 670}
	strips := overlayBorderRects(cr)

	// The four strips tile the client area's edges.
	for i, want := range []Rect{
		{Left: 100, Top: 200, Right: 900, Bottom: 204}, // top
		{Left: 100, Top: 666, Right: 900, Bottom: 670}, // bottom
		{Left: 100, Top: 200, Right: 104, Bottom: 670}, // left
		{Left: 896, Top: 200, Right: 900, Bottom: 670}, // right
	} {
		if strips[i] != want {
			t.Errorf("strip %d = %+v, want %+v", i, strips[i], want)
		}
	}
	// The top strip sits exactly on the inner edge: the border is the
	// only thing outside the transparent interior.
	inner := Rect{
		Left:   cr.Left + overlayBorderWidth,
		Top:    cr.Top + overlayBorderWidth,
		Right:  cr.Right - overlayBorderWidth,
		Bottom: cr.Bottom - overlayBorderWidth,
	}
	if strips[0].Top != cr.Top || strips[0].Bottom != inner.Top {
		t.Errorf("top strip %+v does not line up with the client top and the inner rect %+v", strips[0], inner)
	}
	// Degenerate: no strips for an empty area.
	if overlayBorderRects(Rect{}) != [4]Rect{} {
		t.Error("an empty client area draws no border")
	}
}

func TestOverlayPillRectCentredAtTop(t *testing.T) {
	cr := Rect{Left: 0, Top: 0, Right: 800, Bottom: 470}
	got := overlayPillRect(cr, 280, 18)

	wantW, wantH := int32(280+2*overlayPillPadX), int32(18+2*overlayPillPadY)
	if got.Width() != wantW || got.Height() != wantH {
		t.Errorf("pill = %dx%d, want %dx%d (text plus padding)", got.Width(), got.Height(), wantW, wantH)
	}
	if wantLeft := (cr.Width() - wantW) / 2; got.Left != wantLeft {
		t.Errorf("pill left = %d, want %d (horizontally centred)", got.Left, wantLeft)
	}
	if got.Top != overlayPillTop {
		t.Errorf("pill top = %d, want %d", got.Top, overlayPillTop)
	}
}

func TestOverlayPillRectClipsToNarrowAndShortWindows(t *testing.T) {
	// A window narrower than the pill: the pill is clipped to the width
	// and stays centred, so the banner ellipsizes instead of overflowing.
	narrow := overlayPillRect(Rect{Left: 0, Top: 0, Right: 200, Bottom: 470}, 280, 18)
	if narrow.Width() > 200 {
		t.Errorf("narrow pill width = %d, must not exceed the window's 200", narrow.Width())
	}
	if narrow.Left != 0 || narrow.Right != 200 {
		t.Errorf("narrow pill = %+v, want it to span the window", narrow)
	}

	// A window shorter than the pill's top offset plus height: the pill is
	// pulled up to the top and clipped to the height.
	short := overlayPillRect(Rect{Left: 0, Top: 0, Right: 800, Bottom: 30}, 280, 18)
	if short.Top != 0 {
		t.Errorf("short pill top = %d, want 0", short.Top)
	}
	if short.Height() > 30 {
		t.Errorf("short pill height = %d, must not exceed the window's 30", short.Height())
	}

	// A zero-size window yields an empty pill, never a negative one.
	empty := overlayPillRect(Rect{}, 280, 18)
	if empty.Width() != 0 || empty.Height() != 0 {
		t.Errorf("empty-window pill = %+v, want empty", empty)
	}
}

func TestUIThreadRunsJobsInOrderOnLockedThread(t *testing.T) {
	th, err := startUIThread(func() error { return nil })
	if err != nil {
		t.Fatalf("starting the UI thread: %v", err)
	}
	defer th.close()

	// A job that stays busy for a moment, so the caller can observe its
	// own thread while the job provably occupies the UI thread (a thread
	// locked to the loop's goroutine can host no other).
	var jobTID atomic.Uint32
	started := make(chan struct{})
	busyDone := make(chan struct{})
	go func() {
		_ = th.do(func() {
			close(started)
			time.Sleep(50 * time.Millisecond)
			jobTID.Store(windows.GetCurrentThreadId())
		})
		close(busyDone)
	}()
	<-started
	callerTID := windows.GetCurrentThreadId()
	<-busyDone

	var ran atomic.Int32
	var mu sync.Mutex
	var order []uint32
	var threadIDs []uint32
	for i := 0; i < 3; i++ {
		err := th.do(func() {
			ran.Add(1)
			mu.Lock()
			order = append(order, uint32(i))
			threadIDs = append(threadIDs, windows.GetCurrentThreadId())
			mu.Unlock()
		})
		if err != nil {
			t.Fatalf("job %d: %v", i, err)
		}
	}
	if got := ran.Load(); got != 3 {
		t.Errorf("jobs run = %d, want 3 (do waits for completion)", got)
	}
	for i, v := range order {
		if v != uint32(i) {
			t.Errorf("job order = %v, want in-order execution", order)
			break
		}
	}
	for _, id := range threadIDs {
		if id != jobTID.Load() {
			t.Errorf("a job ran on thread %d, want the one locked UI thread %d", id, jobTID.Load())
		}
	}
	if jobTID.Load() == callerTID {
		t.Fatalf("the UI thread %d is the caller's thread; it must be a separate locked thread", jobTID.Load())
	}
}

func TestUIThreadCloseStopsJobs(t *testing.T) {
	th, err := startUIThread(func() error { return nil })
	if err != nil {
		t.Fatalf("starting the UI thread: %v", err)
	}
	th.close()
	// close is idempotent.
	th.close()

	if err := th.do(func() {}); err == nil {
		t.Error("a job posted after close must fail, not run")
	}
}

func TestUIThreadSetupFailureStopsTheThread(t *testing.T) {
	// A failing setup (class registration, window creation) must surface
	// the error and not leave a looping thread behind.
	_, err := startUIThread(func() error { return errUIThreadClosed })
	if err == nil {
		t.Fatal("a setup failure must be returned")
	}
}

// TestUIThreadTimeoutBoundsAWedgedJob keeps a job blocked past
// uiThreadTimeout and checks that do returns instead of hanging the
// caller. The wedged job then finishes (the thread recovers), which is
// the documented overlay behaviour: a late show or hide is harmless.
func TestUIThreadTimeoutBoundsAWedgedJob(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the uiThread timeout on purpose")
	}
	th, err := startUIThread(func() error { return nil })
	if err != nil {
		t.Fatalf("starting the UI thread: %v", err)
	}
	defer th.close()

	block := make(chan struct{})
	go func() { _ = th.do(func() { <-block }) }()
	// Give the wedging job a moment to start.
	time.Sleep(50 * time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- th.do(func() {}) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a job queued behind a wedged job must time out, not appear to succeed")
		}
	case <-time.After(uiThreadTimeout + 2*time.Second):
		t.Fatal("do did not return within the timeout while the thread was wedged")
	}
	close(block) // un-wedge; the deferred close then stops the thread
}
