package browser

// winuse answers the approval prompt its own dial raises (approve.go): the
// dial to the user's browser through the inspect toggle connects without
// anyone clicking Allow, only that dial's browser process is looked at, and
// nothing is approved for a dial that raises no prompt or has ended.

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

// pressed records the ApprovePrompt calls of a fake System.
type pressed struct {
	mu   sync.Mutex
	pids []uint32
}

func (p *pressed) add(pid uint32) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pids = append(p.pids, pid)
	return len(p.pids)
}

func (p *pressed) calls() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint32(nil), p.pids...)
}

// toggleChrome is a fake machine whose running Chrome is reached through
// the inspect toggle and holds the handshake, as Chrome 144+ does while its
// "Allow remote debugging?" prompt is up; release lets it through.
func toggleChrome(t *testing.T) (f *fakeMachine, release func()) {
	t.Helper()
	f = newFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	s := f.serve(100)
	f.listen(s.Port(), "", 100) // the inspect toggle: no /json/version
	f.toggle("chrome", s.Port(), "/devtools/browser/fake")
	return f, s.Hold()
}

func TestManagerApprovesThePromptItsOwnDialRaised(t *testing.T) {
	f, release := toggleChrome(t)
	var p pressed
	var once sync.Once
	f.sys.ApprovePrompt = func(ctx context.Context, pid uint32) bool {
		p.add(pid)
		once.Do(release) // pressing Allow lets the handshake through
		return true
	}
	m := NewManager(f.sys)
	defer m.Close()

	c, err := m.Connection(context.Background(), "chrome")
	if err != nil {
		t.Fatalf("Connection with the prompt approved: %v", err)
	}
	if c.Mode != ModeToggle || c.PID != 100 {
		t.Errorf("connection = %+v", c)
	}
	if got := p.calls(); !reflect.DeepEqual(got, []uint32{100}) {
		t.Errorf("ApprovePrompt calls = %v, want one press for pid 100", got)
	}
}

func TestManagerKeepsLookingUntilThePromptShows(t *testing.T) {
	f, release := toggleChrome(t)
	var p pressed
	f.sys.ApprovePrompt = func(ctx context.Context, pid uint32) bool {
		if p.add(pid) < 3 {
			return false // the prompt has not opened yet
		}
		release()
		return true
	}
	m := NewManager(f.sys)
	defer m.Close()

	if _, err := m.Connection(context.Background(), "chrome"); err != nil {
		t.Fatalf("Connection: %v", err)
	}
	time.Sleep(3 * approveEvery) // a press ends the polling
	if n := len(p.calls()); n != 3 {
		t.Errorf("ApprovePrompt calls = %d, want 3 (two misses, then the press)", n)
	}
}

func TestManagerStopsLookingWhenTheDialEnds(t *testing.T) {
	f, release := toggleChrome(t)
	var p pressed
	f.sys.ApprovePrompt = func(ctx context.Context, pid uint32) bool {
		p.add(pid)
		return false // the prompt never shows; the user clicks Allow
	}
	m := NewManager(f.sys)
	m.ApprovalWait = 2 * approveEvery
	defer m.Close()

	if _, err := m.Connection(context.Background(), "chrome"); err == nil {
		t.Fatal("Connection succeeded while the handshake is held")
	}
	release() // the user answered the prompt
	if _, err := m.Connection(context.Background(), "chrome"); err != nil {
		t.Fatalf("Connection after the user's Allow: %v", err)
	}
	before := len(p.calls())
	time.Sleep(3 * approveEvery)
	if after := len(p.calls()); after != before {
		t.Errorf("ApprovePrompt called %d more times after the dial ended", after-before)
	}
}

func TestManagerApprovesNothingWhenAlreadyAllowed(t *testing.T) {
	f, release := toggleChrome(t)
	release() // allowed before: the handshake goes straight through
	var p pressed
	f.sys.ApprovePrompt = func(ctx context.Context, pid uint32) bool {
		p.add(pid)
		return true
	}
	m := NewManager(f.sys)
	defer m.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ { // the first dials, the rest reuse its connection
		if _, err := m.Connection(ctx, "chrome"); err != nil {
			t.Fatalf("Connection %d: %v", i, err)
		}
	}
	time.Sleep(3 * approveEvery)
	if got := p.calls(); len(got) != 0 {
		t.Errorf("ApprovePrompt calls = %v with no prompt up, want none", got)
	}
}

func TestManagerApprovesNothingForADialWithoutAPrompt(t *testing.T) {
	f, s, _ := launchedChrome(t)
	var p pressed
	f.sys.ApprovePrompt = func(ctx context.Context, pid uint32) bool {
		p.add(pid)
		return true
	}
	release := s.Hold()
	m := NewManager(f.sys)
	m.ApprovalWait = 3 * approveEvery
	defer m.Close()

	// A browser winuse launched raises no prompt: nothing is pressed while
	// its handshake is held.
	if _, err := m.Connection(context.Background(), "chrome"); err == nil {
		t.Fatal("Connection succeeded while the handshake is held")
	}
	release()
	if got := p.calls(); len(got) != 0 {
		t.Errorf("ApprovePrompt calls = %v for a launched browser, want none", got)
	}
}

func TestPromptWindowsMatchesOnlyThatProcess(t *testing.T) {
	ws := []win.WindowInfo{
		{ID: 1, PID: 100, Title: "Allow remote debugging?", Visible: true},
		{ID: 2, PID: 200, Title: "Allow remote debugging?", Visible: true},                // another process
		{ID: 3, PID: 100, Title: "New Tab - Google Chrome", Visible: true},                // the browser Window
		{ID: 4, PID: 100, Title: "Allow remote debugging?", Visible: false},               // hidden
		{ID: 5, PID: 100, Title: "Allow remote debugging?", Visible: true, Cloaked: true}, // another desktop
		{ID: 6, PID: 100, Title: "ALLOW REMOTE DEBUGGING?", Visible: true},
	}
	if got := PromptWindows(ws, 100); !reflect.DeepEqual(got, []uint64{1, 6}) {
		t.Errorf("PromptWindows = %v, want [1 6]", got)
	}
	if got := PromptWindows(ws, 300); got != nil {
		t.Errorf("PromptWindows for a process with no prompt = %v", got)
	}
}
