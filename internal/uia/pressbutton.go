package uia

import (
	"context"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// PressButtonNamed walks hwnd's live tree and clicks its one enabled button
// named name exactly, through the Headless click route (headlessClick), and
// reports whether it pressed one. It reads and changes no Agent's Window
// State: the helper calls it on its own behalf (browser connect approving
// the browser's "Allow remote debugging?" prompt for winuse's own dial), not
// for an Agent's Action. No match, or several, presses nothing and is not
// an error.
func (c *Client) PressButtonNamed(ctx context.Context, hwnd uint64, name string, budget time.Duration) (bool, error) {
	pressed := false
	var jobErr error
	cfg := walkConfig(ctx, budget)
	err := c.RunFor(ctx, hwnd, func(w *worker, _ *run) {
		if w.thread == nil {
			return // fake workers carry no COM thread
		}
		src := &comSource{th: w.thread, hwnd: uintptr(hwnd)}
		defer src.dispose()
		res, err := Walk[comPtr](src, cfg)
		for _, p := range res.Dropped {
			release(p)
		}
		defer func() {
			for _, p := range res.Live {
				release(p)
			}
		}()
		if err != nil {
			jobErr = err
			return
		}
		i, ok := pickButton(res.Elements, name)
		if !ok {
			return
		}
		if _, err := w.thread.headlessClick(res.Live[i]); err != nil {
			jobErr = err
			return
		}
		pressed = true
	})
	if err != nil {
		return false, err
	}
	return pressed, jobErr
}

// pickButton is the index of the one enabled button named name exactly;
// false when there is none or more than one (never a guess).
func pickButton(els []wstate.Element, name string) (int, bool) {
	found := -1
	for i, e := range els {
		if e.Role != "button" || e.Name != name || e.Disabled {
			continue
		}
		if found >= 0 {
			return 0, false
		}
		found = i
	}
	return found, found >= 0
}
