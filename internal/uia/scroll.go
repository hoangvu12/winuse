package uia

// Element scroll (headless-parity spec, "Element scroll"; ADR 0011): an
// Element scrolled by steps through ScrollPattern, or into view through
// ScrollItemPattern, with no foreground, pointer or wheel.
//
// By steps, the scroll goes to the Element's own ScrollPattern when it
// scrolls on the direction's axis, else to the nearest ancestor that does:
// the Window State lists the Elements inside a list or page, rarely the
// scroll container itself. Each step is one SmallIncrement or
// SmallDecrement on that axis. The scroll percent on the axis is read
// before and after: no change at all fails as delivered ("it may already be
// at the end"), a partial move is ok. Into view is one ScrollIntoView,
// read back through the Element's IsOffscreen property.
//
// The decisions (direction words, the steps bound, the amounts, the
// percent verdict and the run over the steps) are pure functions tested
// offline; the COM code below binds them to live Elements.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unsafe"
)

// ScrollDirection is the direction of an Element scroll by steps.
type ScrollDirection int

const (
	ScrollUp ScrollDirection = iota
	ScrollDown
	ScrollLeft
	ScrollRight
)

// scrollDirectionWords are the directions' spellings, in the constants'
// order.
var scrollDirectionWords = [...]string{"up", "down", "left", "right"}

// String is the direction's word: up, down, left or right.
func (d ScrollDirection) String() string {
	if d < 0 || int(d) >= len(scrollDirectionWords) {
		return fmt.Sprintf("ScrollDirection(%d)", int(d))
	}
	return scrollDirectionWords[d]
}

// Vertical reports whether the direction scrolls the vertical axis.
func (d ScrollDirection) Vertical() bool { return d == ScrollUp || d == ScrollDown }

// amounts are the ScrollPattern.Scroll arguments for one step: a small
// increment or decrement on the direction's axis, no amount on the other.
func (d ScrollDirection) amounts() (h, v uintptr) {
	switch d {
	case ScrollUp:
		return scrollNoAmount, scrollSmallDecrement
	case ScrollDown:
		return scrollNoAmount, scrollSmallIncrement
	case ScrollLeft:
		return scrollSmallDecrement, scrollNoAmount
	}
	return scrollSmallIncrement, scrollNoAmount
}

// axisWord names the direction's axis for messages.
func (d ScrollDirection) axisWord() string {
	if d.Vertical() {
		return "vertically"
	}
	return "horizontally"
}

// ErrScrollDirection reports a direction word that is not one of the four.
var ErrScrollDirection = errors.New("the scroll direction must be up, down, left or right")

// ParseScrollDirection reads a direction word (any case, surrounding
// space ignored).
func ParseScrollDirection(s string) (ScrollDirection, error) {
	word := strings.ToLower(strings.TrimSpace(s))
	for i, w := range scrollDirectionWords {
		if w == word {
			return ScrollDirection(i), nil
		}
	}
	return 0, fmt.Errorf("%w, got %q", ErrScrollDirection, s)
}

// The steps of an Element scroll: 3 when not given, at most 50.
const (
	DefaultScrollSteps = 3
	MaxScrollSteps     = 50
)

// ErrScrollSteps reports a steps count outside 1..MaxScrollSteps.
var ErrScrollSteps = fmt.Errorf("steps must be between 1 and %d", MaxScrollSteps)

// CheckScrollSteps refuses a steps count outside 1..MaxScrollSteps.
func CheckScrollSteps(n int) error {
	if n < 1 || n > MaxScrollSteps {
		return fmt.Errorf("%w, got %d", ErrScrollSteps, n)
	}
	return nil
}

// Element scroll refusals, raised before any pattern call: nothing was
// sent.
var (
	// ErrNotScrollable is a scroll by steps where neither the Element nor
	// any of its ancestors scrolls on the direction's axis.
	ErrNotScrollable = errors.New("neither the Element nor any of its ancestors scrolls")

	// ErrNoScrollItem is a scroll into view on an Element without the
	// ScrollItem pattern.
	ErrNoScrollItem = errors.New("the Element offers no ScrollItem pattern, so it cannot be scrolled into view headless")
)

// didNotScrollMsg is the delivered scroll whose percent did not move.
const didNotScrollMsg = "the Element did not scroll; it may already be at the end"

// scrollNoScroll is UIA_ScrollPatternNoScroll (UIAutomationCore.h): the
// scroll percent of an axis that does not scroll. winuse also uses it for a
// percent it cannot read.
const scrollNoScroll = -1.0

// scrollVerdict is what the percent before and after a scroll says.
type scrollVerdict int

const (
	scrollUnreadable scrollVerdict = iota // either side unreadable: nothing to compare
	scrollStill                           // the same percent: it did not move
	scrollMoved                           // a different percent, however small the move
)

// scrollVerdictOf compares the axis's scroll percent before and after.
// A percent is readable when it lies in 0..100 (UIA_ScrollPatternNoScroll,
// -1, is not).
func scrollVerdictOf(before, after float64) scrollVerdict {
	readable := func(p float64) bool { return !math.IsNaN(p) && p >= 0 && p <= 100 }
	switch {
	case !readable(before) || !readable(after):
		return scrollUnreadable
	case math.Abs(after-before) < 1e-9:
		return scrollStill
	}
	return scrollMoved
}

// scrollerSteps are the live operations scrollBySteps drives: the COM code
// binds them to a ScrollPattern, the offline tests to a fake scroller.
type scrollerSteps struct {
	scroll  func(sent int) error         // one Scroll call; sent is how many went before it
	percent func() float64               // the axis's percent now (scrollNoScroll: unreadable)
	settle  func(before float64) float64 // the percent once it leaves before, or as it reads when verifyWait passes
}

// scrollBySteps scrolls steps times and verifies by the axis's percent: a
// steps count out of range sends nothing; a failed call ends the run with
// its error; afterwards an unchanged percent is a VerifyError (delivered,
// "it may already be at the end"), and an unreadable one is not judged.
func scrollBySteps(steps int, s scrollerSteps) error {
	if err := CheckScrollSteps(steps); err != nil {
		return err
	}
	before := s.percent()
	for sent := 0; sent < steps; sent++ {
		if err := s.scroll(sent); err != nil {
			return err
		}
	}
	if scrollVerdictOf(before, s.settle(before)) == scrollStill {
		return &VerifyError{Action: "scroll", Expected: "a changed scroll position", Msg: didNotScrollMsg}
	}
	return nil
}

// intoViewDone is the into-view read-back: done when IsOffscreen reads
// false. An unreadable IsOffscreen is not judged.
func intoViewDone(readable, offscreen bool) bool {
	return !readable || !offscreen
}

// ---- the COM side ----

// The vtable slots and ids Element scroll adds (verified against the SDK
// 10.0.26100.0 UIAutomationClient.h and UIAutomationCore.h).
const (
	uiaGetRawViewWalker = 16 // IUIAutomation::get_RawViewWalker(**walker)
	walkerGetParent     = 3  // IUIAutomationTreeWalker::GetParentElement(el, **parent)

	scrollCurrentHPercent = 5 // IUIAutomationScrollPattern::get_CurrentHorizontalScrollPercent(*double)
	scrollCurrentVPercent = 6 // IUIAutomationScrollPattern::get_CurrentVerticalScrollPercent(*double)

	propIsOffscreen = 30022 // UIA_IsOffscreenPropertyId
)

// maxScrollAncestors bounds the ancestor search: a Window's tree is never
// this deep between an Element and its scroll container.
const maxScrollAncestors = 64

// ScrollElement scrolls the Element behind index of hwnd's latest Window
// State by steps in the direction, through its own ScrollPattern or its
// nearest scrollable ancestor's, and verifies by the scroll percent.
func (c *Client) ScrollElement(ctx context.Context, hwnd uint64, index int, dir ScrollDirection, steps int) error {
	if err := CheckScrollSteps(steps); err != nil {
		return err
	}
	return c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		pat, err := th.nearestScroller(el, dir)
		if err != nil {
			return err
		}
		defer release(pat)
		return scrollPattern(pat, dir, steps)
	})
}

// ScrollElementIntoView scrolls the Element behind index into view through
// its ScrollItem pattern and reads IsOffscreen back.
func (c *Client) ScrollElementIntoView(ctx context.Context, hwnd uint64, index int) error {
	return c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		if err := scrollItemIntoView(el); err != nil {
			return err
		}
		return pollVerify("scroll into-view", "on screen", func() (bool, string) {
			v := currentProp(el, propIsOffscreen)
			defer clearVariant(&v)
			readable := v.has(vtBool)
			offscreen := readable && v.boolean()
			return intoViewDone(readable, offscreen), "offscreen"
		})
	})
}

// scrollItemIntoView is the one ScrollItem.ScrollIntoView call, shared by
// into-view and the Headed click's best-effort scroll (scrollIntoView). An
// Element without the pattern fails as ErrNoScrollItem before any call.
func scrollItemIntoView(el comPtr) error {
	pat, hr := currentPattern(el, patternScrollItem, &iidScrollItemPattern)
	if hrStale(hr) {
		return staleErr(hr)
	}
	if pat == nil {
		return ErrNoScrollItem
	}
	defer release(pat)
	if hr := call(pat, scrollItemScrollIntoView); hr.Failed() {
		return callErr(hr, "scrolling the Element into view")
	}
	return nil
}

// nearestScroller finds the ScrollPattern to scroll el in the direction:
// el's own when it scrolls on that axis, else the nearest ancestor's (the
// raw view, so no container is skipped). The caller releases the pattern.
// A stale el fails as stale; no scroller up the chain is ErrNotScrollable.
func (th *thread) nearestScroller(el comPtr, dir ScrollDirection) (comPtr, error) {
	axis := uintptr(scrollCurrentH)
	if dir.Vertical() {
		axis = scrollCurrentV
	}
	notScrollable := fmt.Errorf("%w %s", ErrNotScrollable, dir.axisWord())

	var walker comPtr
	if hr := call(th.auto, uiaGetRawViewWalker, uintptr(unsafe.Pointer(&walker))); hr.Failed() || walker == nil {
		walker = nil // search el alone
	} else {
		defer release(walker)
	}

	cur := el // borrowed; every ancestor is owned and released here
	for depth := 0; depth <= maxScrollAncestors; depth++ {
		pat, hr := currentPattern(cur, patternScroll, &iidScrollPattern)
		if hrStale(hr) && cur == el {
			return nil, staleErr(hr)
		}
		if pat != nil {
			if patternBool(pat, axis) {
				if cur != el {
					release(cur)
				}
				return pat, nil
			}
			release(pat)
		}
		if walker == nil {
			break
		}
		var parent comPtr
		hr = call(walker, walkerGetParent, uintptr(cur), uintptr(unsafe.Pointer(&parent)))
		if cur != el {
			release(cur)
		}
		if hr.Failed() || parent == nil {
			if hrStale(hr) && cur == el {
				return nil, staleErr(hr)
			}
			return nil, notScrollable
		}
		cur = parent
	}
	if cur != el {
		release(cur)
	}
	return nil, notScrollable
}

// scrollPattern runs scrollBySteps on a live ScrollPattern. A call that
// fails after an earlier one was delivered is always a WriteError: the
// earlier steps may have moved the content.
func scrollPattern(pat comPtr, dir ScrollDirection, steps int) error {
	h, v := dir.amounts()
	percentSlot := uintptr(scrollCurrentHPercent)
	if dir.Vertical() {
		percentSlot = scrollCurrentVPercent
	}
	percent := func() float64 {
		p := scrollNoScroll
		if hr := call(pat, percentSlot, uintptr(unsafe.Pointer(&p))); hr.Failed() {
			return scrollNoScroll
		}
		return p
	}
	return scrollBySteps(steps, scrollerSteps{
		scroll: func(sent int) error {
			hr := call(pat, scrollScroll, h, v)
			switch {
			case !hr.Failed():
				return nil
			case sent > 0:
				return &WriteError{What: fmt.Sprintf("scrolling %s (step %d of %d)", dir, sent+1, steps), HR: hr}
			}
			return callErr(hr, "scrolling "+dir.String())
		},
		percent: percent,
		settle: func(before float64) float64 {
			deadline := time.Now().Add(verifyWait)
			for {
				now := percent()
				if scrollVerdictOf(before, now) != scrollStill || time.Now().After(deadline) {
					return now
				}
				time.Sleep(verifyInterval)
			}
		},
	})
}
