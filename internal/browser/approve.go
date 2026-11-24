package browser

import (
	"context"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

// The approval prompt. Chrome 144+ holds the opening handshake of a
// connection through the inspect toggle until the user answers its "Allow
// remote debugging?" prompt. winuse answers it itself: while its own dial to
// a browser process waits, it presses Allow in that process's prompt
// (System.ApprovePrompt). It never approves a prompt it did not cause: it
// looks only while one of its dials to that process is pending, only at
// that process's Windows, and stops at the first press or when the dial
// ends.

// approveEvery is how often a pending dial looks for its prompt.
const approveEvery = 250 * time.Millisecond

// approves reports whether a dial to ep raises the approval prompt that
// winuse answers: the user's running browser through the inspect toggle,
// with a known browser process.
func (m *Manager) approves(ep Endpoint) bool {
	return m.sys.ApprovePrompt != nil && ep.Mode == ModeToggle && ep.PID != 0 && ep.Open == nil
}

// approve presses Allow in the prompt pd's dial raised, polling until it
// pressed it once, the dial ended or the approval window ran out.
func (m *Manager) approve(pd *pendingDial) {
	ctx, cancel := context.WithTimeout(context.Background(), m.approvalWindow)
	defer cancel()
	tick := time.NewTicker(approveEvery)
	defer tick.Stop()
	for {
		select {
		case <-pd.done:
			return
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if m.sys.ApprovePrompt(ctx, pd.ep.PID) {
			return
		}
	}
}

// PromptWindows lists the Windows of browser process pid that can be its
// "Allow remote debugging?" prompt: their title names remote debugging. A
// browser Window titled after its active Tab can match too (a page about
// remote debugging); the caller skips any Window with web content, so a
// page's own "Allow" button is never pressed.
func PromptWindows(ws []win.WindowInfo, pid uint32) []uint64 {
	var out []uint64
	for _, w := range ws {
		if w.PID != pid || !w.Visible || w.Cloaked {
			continue
		}
		if strings.Contains(strings.ToLower(w.Title), "remote debugging") {
			out = append(out, w.ID)
		}
	}
	return out
}
