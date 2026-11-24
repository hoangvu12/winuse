package helper

import (
	"context"
	"log"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

const (
	// approveBudget bounds one read of a prompt Window: a small dialog.
	approveBudget = 2 * time.Second
	// promptSettle is how long a prompt must have been seen before winuse
	// presses Allow, and how long it waits between presses. Chromium's
	// dialogs ignore a button press that comes too soon after the dialog
	// shows, or too soon after the last press (views'
	// InputEventActivationProtector, about the double-click time): a press
	// at once was accepted by UI Automation and dropped by Brave, checked
	// live; one after a second closed the prompt.
	promptSettle = time.Second
	// promptCloseWait is how long a press waits for the prompt to close.
	promptCloseWait = time.Second
)

// approvePrompt is browser.System.ApprovePrompt for the helper: it presses
// Allow, Headless, in the "Allow remote debugging?" prompt of browser
// process pid, and reports true once the prompt closed. The browser manager
// calls it only while its own dial to that process waits
// (browser/approve.go). A Window with web content (a browser Window whose
// Tab is titled about remote debugging) is skipped, so a page's own "Allow"
// button is never pressed. A test binary presses nothing
// (win.LiveDesktopGuard).
func approvePrompt(trees *uia.Client) func(ctx context.Context, pid uint32) bool {
	pacer := newPromptPacer(time.Now)
	return func(ctx context.Context, pid uint32) bool {
		if win.LiveDesktopGuard() != nil {
			return false
		}
		ws, err := win.Collect()
		if err != nil {
			return false
		}
		for _, h := range browser.PromptWindows(ws, pid) {
			if _, web := win.ChromiumContentRect(h); web {
				continue
			}
			if !pacer.ready(h) {
				continue // shown too recently, or pressed too recently
			}
			pressed, err := trees.PressButtonNamed(ctx, h, "Allow", approveBudget)
			if err != nil {
				log.Printf("approving the remote debugging prompt (window %d, pid %d): %v", h, pid, err)
				continue
			}
			if !pressed {
				continue
			}
			if promptClosed(ctx, h) {
				log.Printf("approved the remote debugging prompt (window %d, pid %d) for winuse's own connection", h, pid)
				return true
			}
			log.Printf("pressed Allow in the remote debugging prompt (window %d, pid %d), but it stayed open; pressing again in %s", h, pid, promptSettle)
		}
		return false
	}
}

// promptClosed waits up to promptCloseWait for a prompt Window to close
// (or hide) after a press.
func promptClosed(ctx context.Context, h uint64) bool {
	deadline := time.Now().Add(promptCloseWait)
	for {
		hw := windows.HWND(h)
		if !windows.IsWindow(hw) || !windows.IsWindowVisible(hw) {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// promptPacer spaces the presses on prompt Windows: a Window is pressed
// only once it has been seen for promptSettle, and again only promptSettle
// after its last press.
type promptPacer struct {
	mu   sync.Mutex
	now  func() time.Time
	seen map[uint64]time.Time // first seen, or last pressed
}

func newPromptPacer(now func() time.Time) *promptPacer {
	return &promptPacer{now: now, seen: map[uint64]time.Time{}}
}

// ready reports whether h may be pressed now, and when it may, restarts
// its wait: the caller presses it.
func (p *promptPacer) ready(h uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for w, t := range p.seen {
		if now.Sub(t) > 10*time.Minute {
			delete(p.seen, w) // a closed prompt's ID, long gone
		}
	}
	t, ok := p.seen[h]
	if !ok {
		p.seen[h] = now
		return false
	}
	if now.Sub(t) < promptSettle {
		return false
	}
	p.seen[h] = now
	return true
}
