package helper

// The desktop waits (desktop-parity ticket 03): wait beyond Pages.
//
//	wait window <app or title> [--gone]   a Window of that App, or whose title holds the text, shows (or none is left)
//	wait <window> text <text>             the text shows in the Window's UI Automation tree
//	wait <window> <element> <state>       enabled, disabled, gone, value <v>, checked, unchecked, expanded, collapsed
//	wait <milliseconds>                   a plain pause
//	wait notification [--app] [--text]    a toast banner shows a matching notification (notifications.go, ticket 18)
//
// A browser Window keeps the Page's route for text (pageWait), decided by
// get-window-state's own rule: the Window names a browser (PageReader.
// Browser). selector, url, load and download stay Page waits.
//
// They poll. UI Automation events were measured as a cheaper signal (a
// WindowOpened 0.63 s after the launch, a value change within 55 ms), but
// winuse subscribes to no events at all since a Codex freeze (ADR 0004).
// A window wait polls the Window list (EnumWindows) every windowWaitPoll;
// a text wait reads the tree with the bounded walk every treeWaitPoll,
// without replacing the stored Window State (uia PeekTree), so the
// agent's Element Indexes keep naming what they named; an element wait
// resolves its Element once at the start, holds it (uia HoldElement), and
// reads its live state every treeWaitPoll. gone holds once the held
// Element is gone (stale), also when its Window closed; any other wait
// whose Window closes fails with "the Window closed".
//
// A desktop wait only reads: no input, no activation, no Agent Cursor. It
// skips the action gate like get-window-state (waitGated), so an Action,
// another Agent's included, runs while it waits; the Page waits keep the
// gate. Each is a passive Esc scope, bounded by the wait (10 s by default,
// --timeout up to 300 s) and the request's deadline less WaitMargin
// (pageWaitContext).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

const (
	// windowWaitPoll paces a window wait: the Window list is cheap, and a
	// laptop probe saw a new Window in it about 0.4 s after the launch.
	windowWaitPoll = 100 * time.Millisecond
	// treeWaitPoll is the pause between two reads of a text or element
	// wait: a bounded tree walk, or an Element's live properties.
	treeWaitPoll = 250 * time.Millisecond
	// pauseStep is how often a pause checks for Esc and the deadline.
	pauseStep = 100 * time.Millisecond
)

// TreePeeker reads a Window's tree without replacing its stored Window
// State (a text wait's poll). The real implementation is *uia.Client;
// Services.Trees is type-asserted to it, so tests fake it with the trees.
type TreePeeker interface {
	PeekTree(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error)
}

// ElementHolder holds the live Element behind an Element Index of a
// Window's latest Window State for the length of an element wait. The real
// implementation is *uia.Client; Services.Actions is type-asserted to it.
type ElementHolder interface {
	HoldElement(ctx context.Context, hwnd uint64, index int) (uia.Held, error)
}

// desktopWait reports whether a wait takes a desktop route: window,
// element and ms always, and text on a Window that names no browser. A
// text wait whose Window does not resolve keeps the Page's route, which
// fails with the same resolution error.
func (s Services) desktopWait(a protocol.WaitArgs) bool {
	switch strings.ToLower(strings.TrimSpace(a.For)) {
	case protocol.WaitWindow, protocol.WaitElement, protocol.WaitMS, protocol.WaitNotification:
		return true
	case page.WaitText:
		w, rerr := resolveWindow(s, a.Window)
		return rerr == nil && (s.Pages == nil || s.Pages.Browser(w) == "")
	}
	return false
}

// waitGated is wait in the registry: a Page wait runs on its Page's lane
// (lanes.go), a desktop wait takes none (it only reads). A Batch
// dispatches to the raw wait, inside its own lanes.
func (s Services) waitGated(cmd Command) Command {
	paged := s.laned("wait", cmd)
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.WaitArgs
		if json.Unmarshal(args, &a) == nil && s.desktopWait(a) {
			return cmd(ctx, args)
		}
		return paged(ctx, args)
	}
}

// runDesktopWait runs a desktop wait in a passive Esc scope (pageWait
// hands it over).
func (s Services) runDesktopWait(ctx context.Context, a protocol.WaitArgs) (string, *Error) {
	if a.TimeoutMS < 0 {
		return "", Fail("wait: the timeout must be positive", waitUsageHint)
	}
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()
	kind := strings.ToLower(strings.TrimSpace(a.For))
	if kind == protocol.WaitMS {
		return s.waitPause(ctx, a, esc.Stopped)
	}
	want := pageWaitDefault
	if a.TimeoutMS > 0 {
		want = time.Duration(a.TimeoutMS) * time.Millisecond
	}
	if dl, ok := ctx.Deadline(); ok {
		// The request's deadline cuts it shorter (pageWaitContext's bound).
		want = min(want, max(time.Until(dl)-pageWaitMargin, 100*time.Millisecond))
	}
	wctx, cancel := pageWaitContext(ctx, want)
	defer cancel()
	// The polls' UIA calls end with the wait without abandoning the UIA
	// worker: a wait that times out is no overrun.
	wctx = uia.WithWaitPoll(wctx)
	switch kind {
	case protocol.WaitWindow:
		return s.waitWindow(wctx, a, want, esc.Stopped)
	case protocol.WaitNotification:
		return s.waitNotification(wctx, a, want, esc.Stopped) // notifications.go, desktop-parity ticket 18
	case page.WaitText:
		return s.waitTreeText(wctx, a, want, esc.Stopped)
	}
	return s.waitElement(wctx, a, want, esc.Stopped)
}

// waitEnd is how a poll ended.
type waitEnd int

const (
	waitHeld waitEnd = iota
	waitTimedOut
	waitStopped
)

// pollUntil runs check every interval until it holds, the wait passes,
// ctx ends or the user presses Esc; a check's *Error ends it at once, and
// last is what the last check that answered returned. The wait is counted
// in intervals slept, as pollWindow counts it, so a faked Sleep runs it
// instantly; on the real desktop ctx bounds it in time.
//
// Each check runs on a goroutine of its own, so a check that blocks (a
// Window list read or a UIA call that does not answer) still ends the
// wait when ctx does: it is left to finish, and what it returns is
// dropped. A check therefore hands its results back as its return values;
// state it shares with later checks is touched by checks alone, which run
// one at a time.
func pollUntil[T any](ctx context.Context, sleep func(time.Duration), want, interval time.Duration, stopped func() bool, check func() (T, bool, *Error)) (last T, end waitEnd, e *Error) {
	type answer struct {
		v    T
		held bool
		e    *Error
	}
	for waited := time.Duration(0); ; waited += interval {
		if stopped() {
			return last, waitStopped, nil
		}
		done := make(chan answer, 1)
		go func() {
			v, held, e := check()
			done <- answer{v, held, e}
		}()
		var a answer
		select {
		case a = <-done:
		case <-ctx.Done():
			return last, waitTimedOut, nil
		}
		last = a.v
		if a.e != nil {
			return last, waitTimedOut, a.e
		}
		if a.held {
			return last, waitHeld, nil
		}
		if waited >= want || ctx.Err() != nil {
			return last, waitTimedOut, nil
		}
		sleep(interval)
	}
}

// waitTimeoutError is a desktop wait whose condition did not hold in
// time; seen, when set, says what the wait read last.
func waitTimeoutError(what string, want time.Duration, seen, hint string) *Error {
	msg := "waiting for " + what + ": " + (&page.WaitTimeoutError{After: want}).Error()
	if seen != "" {
		msg += "; " + seen
	}
	return Fail(msg, hint+" (wait takes --timeout <seconds>, up to 300)")
}

// waitWindowClosed is a text or element wait whose Window closed.
func waitWindowClosed(what string) *Error {
	return Fail("waiting for "+what+": the Window closed", "run winuse list-windows")
}

// waitPause is `wait <milliseconds>`: a plain pause in pauseStep slices,
// stopped by Esc, and failed when the request's budget ends before it.
func (s Services) waitPause(ctx context.Context, a protocol.WaitArgs, stopped func() bool) (string, *Error) {
	limit := int(protocol.MaxWaitTimeout / time.Millisecond)
	if a.MS <= 0 || a.MS > limit {
		return "", Fail(fmt.Sprintf("wait: a pause is a number of milliseconds above 0 and at most %d, got %d", limit, a.MS), waitUsageHint)
	}
	// `wait <window>` with what to wait for left out reads as a pause:
	// refuse a number that is an open Window's ID rather than sit out
	// minutes.
	if ws, err := s.Windows(); err == nil {
		for _, w := range ws {
			if w.ID == uint64(a.MS) {
				return "", Fail(fmt.Sprintf("wait: %d is the Window ID of %s %q, so winuse did not pause that many milliseconds", a.MS, w.App, w.Title),
					fmt.Sprintf("to wait in that Window, name what for: winuse wait %d text <text>; to pause, pass a number of milliseconds that is no Window ID", a.MS))
			}
		}
	}
	pause := time.Duration(a.MS) * time.Millisecond
	for slept := time.Duration(0); slept < pause; {
		if stopped() {
			return "", escStopBeforeActError()
		}
		if ctx.Err() != nil {
			return "", Fail(fmt.Sprintf("the request's budget ended %s into the pause of %s", slept.Round(time.Millisecond), pause),
				"a pause inside a batch counts against the Batch's budget (batch --timeout <seconds>, up to 300)")
		}
		step := min(pauseStep, pause-slept)
		s.Sleep(step)
		slept += step
	}
	return okOutput, nil
}

// waitWindow is `wait window <app or title> [--gone]`: it prints the
// matching Windows' list-windows lines once one shows, or ok once none is
// left.
func (s Services) waitWindow(ctx context.Context, a protocol.WaitArgs, want time.Duration, stopped func() bool) (string, *Error) {
	target := strings.TrimSpace(a.Value)
	if target == "" {
		return "", Fail("wait window: an App name or a title is required", waitUsageHint)
	}
	found, end, e := pollUntil(ctx, s.Sleep, want, windowWaitPoll, stopped, func() ([]win.WindowInfo, bool, *Error) {
		ws, err := s.Windows()
		if err != nil {
			return nil, false, Fail("enumerating windows: "+err.Error(), "")
		}
		found := windowsMatching(ws, target)
		return found, (len(found) > 0) != a.Gone, nil
	})
	switch {
	case e != nil:
		return "", e
	case end == waitStopped:
		return "", escStopBeforeActError()
	case end == waitTimedOut && a.Gone:
		seen := ""
		if len(found) > 0 {
			seen = "still open:\n" + strings.TrimSuffix(win.FormatWindows(found), "\n")
		}
		return "", waitTimeoutError(fmt.Sprintf("no Window of %q to be left", target), want, seen,
			"the Window may be asking something first; read it with winuse get-window-state <window>")
	case end == waitTimedOut:
		return "", waitTimeoutError(fmt.Sprintf("a Window of %q", target), want, "",
			"an App matches by its App name or executable name, a title by any part of it; run winuse list-windows")
	case a.Gone:
		return okOutput, nil
	}
	return win.FormatWindows(found), nil
}

// windowsMatching is the Windows of ws whose App is target (as a Window
// reference names an App: display or executable name, case-insensitive)
// or whose title holds it (case-insensitive), in ws's z-order.
func windowsMatching(ws []win.WindowInfo, target string) []win.WindowInfo {
	apps := win.WindowsOfApp(ws, target)
	lower := strings.ToLower(target)
	var out []win.WindowInfo
	for _, w := range ws {
		byApp := slices.ContainsFunc(apps, func(x win.WindowInfo) bool { return x.ID == w.ID })
		if byApp || strings.Contains(strings.ToLower(w.Title), lower) {
			out = append(out, w)
		}
	}
	return out
}

// waitTreeText is `wait <window> text <text>` on a Window that names no
// browser: the text shows in an Element's name, value or description
// (case-sensitive, as a Page's text wait matches).
func (s Services) waitTreeText(ctx context.Context, a protocol.WaitArgs, want time.Duration, stopped func() bool) (string, *Error) {
	if a.Value == "" {
		return "", Fail("wait: wait text needs the text to wait for", waitUsageHint)
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return "", rerr
	}
	peeker, ok := s.Trees.(TreePeeker)
	if !ok {
		return "", Fail("this winuse helper cannot read a tree for a wait", "")
	}
	what := fmt.Sprintf("text %q in window %d", a.Value, w.ID)
	_, end, e := pollUntil(ctx, s.Sleep, want, treeWaitPoll, stopped, func() (struct{}, bool, *Error) {
		if !s.WindowOpen(w.ID) {
			return struct{}{}, false, waitWindowClosed(what)
		}
		tree, err := peeker.PeekTree(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
		switch {
		case err == nil:
			return struct{}{}, treeHasText(tree, a.Value), nil
		case errors.Is(err, uia.ErrWindowGone):
			return struct{}{}, false, waitWindowClosed(what)
		case ctx.Err() != nil, errors.Is(err, uia.ErrWorkerGone), transientTreeRead(err):
			return struct{}{}, false, nil // the wait's end, a replaced worker or a read that failed on an open Window: the next poll says
		}
		return struct{}{}, false, treeReadError(err)
	})
	switch {
	case e != nil:
		return "", e
	case end == waitStopped:
		return "", escStopBeforeActError()
	case end == waitTimedOut:
		return "", waitTimeoutError(what, want, "",
			"the Window may need another Action first; read winuse get-window-state "+strconv.FormatUint(w.ID, 10))
	}
	return okOutput, nil
}

// treeHasText reports whether an Element's name, value or description
// holds text.
func treeHasText(tree *wstate.Tree, text string) bool {
	for _, el := range tree.Elements {
		if strings.Contains(el.Name, text) || strings.Contains(el.Value, text) || strings.Contains(el.Description, text) {
			return true
		}
	}
	return false
}

// waitElement is `wait <window> <element> <state>`: the Element resolves
// once, against the Window's latest Window State, and its live state is
// read until it holds.
func (s Services) waitElement(ctx context.Context, a protocol.WaitArgs, want time.Duration, stopped func() bool) (string, *Error) {
	state := strings.ToLower(strings.TrimSpace(a.State))
	if !slices.Contains(protocol.WaitStates, state) {
		return "", Fail(fmt.Sprintf("wait: an element waits for one of %s, not %q", strings.Join(protocol.WaitStates, ", "), a.State), waitUsageHint)
	}
	if a.Value != "" && state != "value" {
		return "", Fail("wait: only value takes a value", waitUsageHint)
	}
	if strings.TrimSpace(a.Window) == "" {
		return "", Fail("wait: a window reference is required", waitUsageHint)
	}
	target, perr := parseElementTarget("wait", a.ElementIndex, a.Ref, nil, waitUsageHint)
	if perr != nil {
		return "", perr
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return "", rerr
	}
	index := 0
	if target.ref != "" {
		if index, rerr = resolveElementRef(ctx, s, w, target.ref); rerr != nil {
			return "", rerr
		}
	} else {
		index = *target.index
	}
	if splicer, ok := s.Actions.(PageSplicer); ok {
		if o, err := splicer.ElementOrigin(ctx, w.ID, index); errors.Is(err, uia.ErrPageElement) || (err == nil && o != nil) {
			return "", pageElementWaitError(w, index)
		}
	}
	holder, ok := s.Actions.(ElementHolder)
	if !ok {
		return "", Fail("element waits are unavailable: no UIA client", "")
	}
	held, err := holder.HoldElement(ctx, w.ID, index)
	if errors.Is(err, uia.ErrPageElement) {
		return "", pageElementWaitError(w, index)
	}
	if err != nil {
		return "", elementError(err, w, index)
	}
	defer held.Release()

	what := fmt.Sprintf("element %d of window %d to be %s", index, w.ID, state)
	if state == "value" {
		what = fmt.Sprintf("element %d of window %d to read %q", index, w.ID, a.Value)
	}
	// read is shared by the checks alone (pollUntil runs them one at a
	// time): the first read refuses a state the Element lacks.
	read := false
	last, end, e := pollUntil(ctx, s.Sleep, want, treeWaitPoll, stopped, func() (elementPoll, bool, *Error) {
		st, err := held.State(ctx)
		open := s.WindowOpen(w.ID)
		switch {
		case state == "gone" && (!open || errors.Is(err, uia.ErrStale)):
			// An Element of a closed Window is gone with it, whatever its
			// read answered: stale, a read that still succeeds, or another
			// failure, such as a replaced worker's (once live, a closed
			// classic dialog's Edit answered neither stale nor ok).
			return elementPoll{closed: !open}, true, nil
		case err == nil:
		case !open:
			return elementPoll{}, false, waitWindowClosed(what)
		case errors.Is(err, uia.ErrStale):
			return elementPoll{}, false, elementError(err, w, index)
		case ctx.Err() != nil, errors.Is(err, uia.ErrWorkerGone):
			return elementPoll{}, false, nil // the wait's end, or a replaced worker: the next poll says
		default:
			return elementPoll{}, false, Fail(fmt.Sprintf("reading element %d of window %d failed: %v", index, w.ID, err), "retry; read winuse get-window-state "+strconv.FormatUint(w.ID, 10))
		}
		if !open {
			return elementPoll{}, false, waitWindowClosed(what)
		}
		if !read {
			if e := elementStateRefusal(state, st, w, index); e != nil {
				return elementPoll{}, false, e
			}
		}
		read = true
		return elementPoll{st: st, read: true}, elementStateHolds(state, a.Value, st), nil
	})
	switch {
	case e != nil:
		return "", e
	case end == waitStopped:
		return "", escStopBeforeActError()
	case end == waitTimedOut:
		seen := ""
		if last.read {
			seen = "it reads " + elementStateWord(state, last.st)
		}
		return "", waitTimeoutError(what, want, seen,
			"the Window may need another Action first; read winuse get-window-state "+strconv.FormatUint(w.ID, 10))
	case last.closed:
		return "ok\nNote: the Window closed\n", nil
	}
	return okOutput, nil
}

// elementPoll is what one poll of an element wait read: the state, when it
// read one, and whether a gone Element went with its Window.
type elementPoll struct {
	st     uia.ElementState
	read   bool
	closed bool
}

// pageElementWaitError refuses an element wait on a Page Element: the Page
// is waited on by selector or text.
func pageElementWaitError(w win.WindowInfo, index int) *Error {
	id := strconv.FormatUint(w.ID, 10)
	return Fail(fmt.Sprintf("element %d is in the Page of window %d, and winuse waits on a Page by selector or text, not by Element", index, w.ID),
		"run winuse wait "+id+" selector <css> or winuse wait "+id+" text <text>")
}

// elementStateRefusal fails a wait for a state the Element does not have
// (read on its first poll), instead of waiting out the timeout.
func elementStateRefusal(state string, st uia.ElementState, w win.WindowInfo, index int) *Error {
	lacks := ""
	switch state {
	case "value":
		if !st.HasValue {
			lacks = "a value (its line shows no Value:)"
		}
	case "checked", "unchecked":
		if st.Toggle == "" && !st.Selectable {
			lacks = "a checked state (no Toggle or Select)"
		}
	case "expanded", "collapsed":
		if st.Expand == "" || st.Expand == "a leaf" {
			lacks = "an expanded state (it does not expand or collapse)"
		}
	}
	if lacks == "" {
		return nil
	}
	return Fail(fmt.Sprintf("element %d of window %d has no %s, so it cannot be waited on for %s", index, w.ID, lacks, state),
		"read winuse get-window-state "+strconv.FormatUint(w.ID, 10)+" and wait on an Element that shows that state")
}

// elementStateHolds reports whether a live state is the one waited for.
func elementStateHolds(state, value string, st uia.ElementState) bool {
	switch state {
	case "enabled":
		return st.Enabled
	case "disabled":
		return !st.Enabled
	case "value":
		return st.Value == value
	case "checked":
		return st.Toggle == "on" || (st.Toggle == "" && st.Selected)
	case "unchecked":
		return st.Toggle == "off" || (st.Toggle == "" && st.Selectable && !st.Selected)
	case "expanded":
		return st.Expand == "expanded" || st.Expand == "partially expanded"
	case "collapsed":
		return st.Expand == "collapsed"
	}
	return false // gone: only a gone Element holds
}

// elementStateWord says what a timed-out wait read last, in the Window
// State's words; a password field's value is never echoed.
func elementStateWord(state string, st uia.ElementState) string {
	switch state {
	case "enabled", "disabled":
		if st.Enabled {
			return "enabled"
		}
		return "disabled"
	case "value":
		if st.Secret {
			return "another value (a password field's value is not shown)"
		}
		return fmt.Sprintf("%q", st.Value)
	case "checked", "unchecked":
		if st.Toggle != "" {
			return st.Toggle
		}
		if st.Selected {
			return "selected"
		}
		return "not selected"
	case "expanded", "collapsed":
		return st.Expand
	}
	return "still there"
}
