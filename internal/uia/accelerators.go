package uia

// The shortcut lookup of press-key (headless-max ticket 06, like cua's
// hotkey tool): before a chord is refused for want of a background route,
// the Window's tree is searched for the command that shows that shortcut,
// and that one Element is invoked through its patterns (the Headless
// click's route). The labels are read from AcceleratorKey and from the
// Element's name (keys.LabelShortcuts); the caller passes the matcher.
//
// Only menu items, buttons and split buttons are read, and never inside a
// Document (web content), under a bounded walk. One enabled match is
// invoked; one or more matches that are all disabled are refused (a real
// key would do nothing either); several enabled matches, or none, leave
// the decision to the caller, which never guesses. For WinForms and WinUI
// 3 Windows, whose menu items exist only once their menu bar item is
// expanded (probes round 5, section 4), the caller may ask for the menu
// bar items to be expanded one at a time, searched and collapsed again;
// the expansion takes no foreground (measured).

import (
	"context"
	"fmt"
	"time"
	"unsafe"
)

const (
	ctButton      = 50000 // UIA_ButtonControlTypeId
	ctMenuBar     = 50010 // UIA_MenuBarControlTypeId
	ctMenuItem    = 50011 // UIA_MenuItemControlTypeId
	ctSplitButton = 50031 // UIA_SplitButtonControlTypeId

	propAcceleratorKey = 30006 // UIA_AcceleratorKeyPropertyId
	propIsEnabled      = 30010 // UIA_IsEnabledPropertyId

	maxAccelDepth     = 25
	maxAccelNodes     = 2000
	maxAccelMenus     = 12                     // menu bar items expanded at most
	accelExpandSettle = 150 * time.Millisecond // for an expanded menu's items to appear
	accelMenuBudget   = 2 * time.Second        // the expand-and-search fallback in all
)

// AccelResult is what the shortcut search found and did.
type AccelResult struct {
	Invoked  bool   // the one enabled match was invoked
	Name     string // its name, or the disabled match's
	Disabled bool   // every match is disabled: refused, nothing was sent
	Enabled  int    // the enabled matches found (two or more: nothing was done)
}

// accelCand is one matching Element as the picker sees it.
type accelCand struct {
	name    string
	enabled bool
}

// pickAccelerator is the pure choice: the index of the one enabled match
// to invoke, or -1 with the outcome.
func pickAccelerator(cands []accelCand) (int, AccelResult) {
	pick, enabled := -1, 0
	for i, c := range cands {
		if c.enabled {
			enabled++
			pick = i
		}
	}
	switch {
	case enabled == 1:
		return pick, AccelResult{Name: cands[pick].name, Enabled: 1}
	case enabled == 0 && len(cands) > 0:
		return -1, AccelResult{Name: cands[0].name, Disabled: true}
	}
	return -1, AccelResult{Enabled: enabled}
}

// InvokeAccelerator searches hwnd's tree for the Elements whose labels
// show the shortcut (shown) and invokes the one enabled match (see the
// file comment). With expandMenus, a search that finds nothing expands
// the menu bar items one at a time and searches again.
func (c *Client) InvokeAccelerator(ctx context.Context, hwnd uint64, shown func(acceleratorKey, name string, menuItem bool) bool, expandMenus bool) (AccelResult, error) {
	var res AccelResult
	var jobErr error
	err := c.RunFor(ctx, hwnd, func(w *worker, _ *run) {
		if w.thread == nil {
			return // fake workers carry no COM thread: nothing found
		}
		res, jobErr = w.thread.invokeAccelerator(hwnd, shown, expandMenus)
	})
	if err != nil {
		return AccelResult{}, err
	}
	return res, jobErr
}

func (th *thread) invokeAccelerator(hwnd uint64, shown func(string, string, bool) bool, expandMenus bool) (AccelResult, error) {
	var root comPtr
	if hr := call(th.auto, uiaElementFromHandle, uintptr(hwnd), uintptr(unsafe.Pointer(&root))); hr.Failed() || root == nil {
		return AccelResult{}, fmt.Errorf("%w: reading the Window's shortcuts failed (HRESULT %s)", ErrWindowGone, hr)
	}
	defer release(root)
	var cond comPtr
	if hr := call(th.auto, uiaGetControlViewCondition, uintptr(unsafe.Pointer(&cond))); hr.Failed() || cond == nil {
		return AccelResult{}, fmt.Errorf("get_ControlViewCondition failed (HRESULT %s)", hr)
	}
	defer release(cond)

	res, found, err := th.searchAndInvoke(root, cond, shown)
	if found || err != nil || !expandMenus {
		return res, err
	}
	deadline := time.Now().Add(accelMenuBudget)
	menus := accelMenuItems(root, cond)
	defer releaseAll(menus)
	for _, m := range menus {
		if time.Now().After(deadline) {
			break
		}
		ec, _ := currentPattern(m, patternExpandCollapse, &iidExpandCollapsePattern)
		if ec == nil {
			continue
		}
		if hr := call(ec, ecExpand); hr.Failed() {
			release(ec)
			continue
		}
		time.Sleep(accelExpandSettle)
		res, found, err = th.searchAndInvoke(root, cond, shown)
		call(ec, ecCollapse) // best effort; an invoked item usually closed its menu already
		release(ec)
		if found || err != nil {
			return res, err
		}
	}
	return res, nil
}

// searchAndInvoke walks the tree once, picks, and invokes the one enabled
// match. found reports a decision: an invoke, or all matches disabled;
// several enabled matches are a decision to do nothing too.
func (th *thread) searchAndInvoke(root, cond comPtr, shown func(string, string, bool) bool) (AccelResult, bool, error) {
	els, cands := accelMatches(root, cond, shown)
	defer releaseAll(els)
	i, res := pickAccelerator(cands)
	if i < 0 {
		return res, res.Disabled || res.Enabled > 1, nil
	}
	if _, err := th.headlessClick(els[i]); err != nil {
		return res, true, err
	}
	res.Invoked = true
	return res, true, nil
}

// accelMatches is the bounded walk: the menu items, buttons and split
// buttons whose labels show the shortcut, outside any Document. The
// caller releases the elements.
func accelMatches(root, cond comPtr, shown func(string, string, bool) bool) ([]comPtr, []accelCand) {
	var els []comPtr
	var cands []accelCand
	walkControls(root, cond, func(el comPtr, ct int32) bool {
		switch ct {
		case ctMenuItem, ctButton, ctSplitButton:
			name := currentString(el, propName)
			if shown(currentString(el, propAcceleratorKey), name, ct == ctMenuItem) {
				addRef(el)
				els = append(els, el)
				cands = append(cands, accelCand{name: name, enabled: currentBool(el, propIsEnabled)})
			}
		}
		return ct != ctDocument
	})
	return els, cands
}

// accelMenuItems are the menu bar items of the Window (MenuItem children
// of a MenuBar), at most maxAccelMenus. The caller releases them.
func accelMenuItems(root, cond comPtr) []comPtr {
	var items []comPtr
	walkControls(root, cond, func(el comPtr, ct int32) bool {
		if ct == ctMenuBar {
			kids, _ := findChildren(el, cond)
			for _, k := range kids {
				if currentState(k, propControlType) == ctMenuItem && len(items) < maxAccelMenus {
					items = append(items, k)
				} else {
					release(k)
				}
			}
			return false
		}
		return ct != ctDocument
	})
	return items
}

// walkControls is a bounded breadth-first walk of root's control view:
// visit sees every element (root excluded) and says whether to descend
// into it. Elements are released after their visit; visit AddRefs what it
// keeps.
func walkControls(root, cond comPtr, visit func(el comPtr, ct int32) bool) {
	level := []comPtr{root}
	visited := 0
	for depth := 0; depth < maxAccelDepth && len(level) > 0; depth++ {
		var next []comPtr
		for _, node := range level {
			var kids []comPtr
			if visited < maxAccelNodes {
				kids, _ = findChildren(node, cond)
			}
			for _, k := range kids {
				visited++
				if visit(k, currentState(k, propControlType)) && visited <= maxAccelNodes {
					next = append(next, k)
				} else {
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
}
