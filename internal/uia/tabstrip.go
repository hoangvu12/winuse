package uia

// The Headless tab switch on a launched or cloned browser (headless-max
// ticket 05, ADR 0013 rule 1): the browser Window's own tab strip is read
// through UI Automation, the Tab's item is selected through
// SelectionItem.Select, and its IsSelected is read back. Measured on a
// covered launched Chrome 156 (`.scratch/headless-gaps/probes-round4.md`
// section 4): the tab switched, the window title followed, and the
// foreground and cursor stayed put. The tab items offer no Invoke, so
// Select is the only route.
//
// The search is a bounded breadth-first walk of the Window's control-view
// tree for TabItem elements. It never descends into a Document (the web
// content, whose ARIA tabs are TabItems too) or into a TabItem (its
// children are its close button and the like). The caller picks the item
// by name; the driver (selectTab) runs over tabSteps, so the offline tests
// drive it with fakes, and the COM bindings below only run live.
//
// Chromium builds its accessibility tree lazily (ADR 0005), and a browser
// that winuse drove over CDP alone for a while shows no tab-strip items to
// the first walk (the 2026-11-03 live run of TestBrowserPages): an empty
// strip is walked again until tabStripSettle passes (settleTabStrip).
// Nothing was selected before a retry.

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"
)

// The tab strip's control types and the walk's bounds. Chrome's tab strip
// sits about ten levels below the frame.
const (
	ctTabItem  = 50019 // UIA_TabItemControlTypeId
	ctDocument = 50030 // UIA_DocumentControlTypeId

	propControlType = 30003 // UIA_ControlTypePropertyId

	uiaElementFromHandle = 6 // IUIAutomation::ElementFromHandle(hwnd, **el)

	maxTabDepth = 25
	maxTabNodes = 3000
	maxTabItems = 500

	// tabStripSettle bounds the walks of a strip that shows no items yet,
	// tabStripRetry spaces them.
	tabStripSettle = 3 * time.Second
	tabStripRetry  = 250 * time.Millisecond
)

// ErrNoTabStrip is a Window whose tree shows no tab-strip item: nothing
// was selected.
var ErrNoTabStrip = errors.New("the Window shows no tab strip through UI Automation")

// tabSteps are the live operations selectTab is driven through.
type tabSteps struct {
	names    func() ([]string, error) // the tab-strip items' names, in tree order
	selectAt func(i int) error        // SelectionItem.Select on item i
	selected func(i int) bool         // item i's IsSelected now
}

// selectTab lists the tab-strip items, lets pick choose one by its name
// (pick's error refuses: nothing was selected), selects it, and polls its
// IsSelected until it reads selected or verifyWait passes.
func selectTab(steps tabSteps, pick func(names []string) (int, error)) error {
	names, err := steps.names()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return ErrNoTabStrip
	}
	i, err := pick(names)
	if err != nil {
		return err
	}
	if i < 0 || i >= len(names) {
		return fmt.Errorf("tab-strip item %d is out of range", i)
	}
	if err := steps.selectAt(i); err != nil {
		return err
	}
	return pollVerify(fmt.Sprintf("selecting the tab %q", names[i]), "selected", func() (bool, string) {
		if steps.selected(i) {
			return true, ""
		}
		return false, "not selected"
	})
}

// SelectTab selects one item of the browser Window's tab strip: pick
// chooses it among the items' names, in tree order. A Window with no tab
// strip fails with ErrNoTabStrip and pick's own error is returned as is:
// both before anything was selected. A failed Select is a *WriteError and
// a Select that does not read back selected a *VerifyError.
func (c *Client) SelectTab(ctx context.Context, hwnd uint64, pick func(names []string) (int, error)) error {
	return settleTabStrip(ctx, tabStripSettle, tabStripRetry, sleepCtx, func() error {
		return c.selectTabOnce(ctx, hwnd, pick)
	})
}

// settleTabStrip runs attempt again, interval apart, while it fails with
// ErrNoTabStrip and budget (cut by ctx) lasts: a Chromium tab strip that
// is still being built. Any other answer is final.
func settleTabStrip(ctx context.Context, budget, interval time.Duration, sleep func(context.Context, time.Duration) error, attempt func() error) error {
	deadline := time.Now().Add(budget)
	for {
		err := attempt()
		if !errors.Is(err, ErrNoTabStrip) || time.Now().Add(interval).After(deadline) {
			return err
		}
		if dl, ok := ctx.Deadline(); ok && time.Now().Add(interval).After(dl) {
			return err
		}
		if sleep(ctx, interval) != nil {
			return err
		}
	}
}

// selectTabOnce is one walk of the tab strip and, when it shows items,
// the selection.
func (c *Client) selectTabOnce(ctx context.Context, hwnd uint64, pick func(names []string) (int, error)) error {
	var jobErr error
	err := c.RunFor(ctx, hwnd, func(w *worker, _ *run) {
		if w.thread == nil {
			jobErr = ErrNoTabStrip // fake workers carry no COM thread
			return
		}
		items, err := w.thread.tabItems(hwnd)
		if err != nil {
			jobErr = err
			return
		}
		defer releaseAll(items)
		jobErr = selectTab(tabSteps{
			names: func() ([]string, error) {
				names := make([]string, len(items))
				for i, it := range items {
					names[i] = currentString(it, propName)
				}
				return names, nil
			},
			selectAt: func(i int) error {
				pat, hr := currentPattern(items[i], patternSelectionItem, &iidSelectionItemPattern)
				if hrStale(hr) {
					return staleErr(hr)
				}
				if pat == nil {
					return errors.New("the tab-strip item offers no Select")
				}
				defer release(pat)
				if hr := call(pat, siSelect); hr.Failed() {
					return &WriteError{What: "selecting the tab", HR: hr}
				}
				return nil
			},
			selected: func(i int) bool { return currentBool(items[i], propSelectionItemIsSelected) },
		}, pick)
	})
	if err != nil {
		return err
	}
	return jobErr
}

// tabItems is the bounded walk: the TabItem elements of hwnd's control
// view, outside any Document and TabItem subtree. The caller releases
// them.
func (th *thread) tabItems(hwnd uint64) ([]comPtr, error) {
	var root comPtr
	if hr := call(th.auto, uiaElementFromHandle, uintptr(hwnd), uintptr(unsafe.Pointer(&root))); hr.Failed() || root == nil {
		return nil, fmt.Errorf("%w: reading the Window's tab strip failed (HRESULT %s)", ErrWindowGone, hr)
	}
	defer release(root)
	var cond comPtr
	if hr := call(th.auto, uiaGetControlViewCondition, uintptr(unsafe.Pointer(&cond))); hr.Failed() || cond == nil {
		return nil, fmt.Errorf("get_ControlViewCondition failed (HRESULT %s)", hr)
	}
	defer release(cond)

	var items []comPtr
	level := []comPtr{root} // root is released above; every other element is owned
	visited := 0
	for depth := 0; depth < maxTabDepth && len(level) > 0; depth++ {
		var next []comPtr
		for _, node := range level {
			var kids []comPtr
			if visited < maxTabNodes {
				kids, _ = findChildren(node, cond)
			}
			for _, k := range kids {
				visited++
				switch ct := currentState(k, propControlType); {
				case ct == ctTabItem && len(items) < maxTabItems:
					items = append(items, k)
				case ct != ctTabItem && ct != ctDocument && visited <= maxTabNodes:
					next = append(next, k)
				default:
					release(k)
				}
			}
			if node != root {
				release(node)
			}
		}
		level = next
	}
	for _, node := range level {
		if node != root {
			release(node)
		}
	}
	return items, nil
}
