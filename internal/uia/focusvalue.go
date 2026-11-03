package uia

// The read-back of the Chromium message route (headless-max ticket 20,
// ADR 0013 rule 7): after text is typed into a Chromium or Electron Window
// through window messages, the field that has the keyboard focus is found
// among the Elements of the Window's latest Window State and its Value
// pattern is read. A field winuse cannot find or read leaves the text
// unverified, and the output says so; a password field is never read.

import (
	"context"
	"strings"
	"time"
)

// propHasKeyboardFocus is UIA_HasKeyboardFocusPropertyId.
const propHasKeyboardFocus = 30008

// FocusedValue reads the value of the Element that has the keyboard focus
// among the stored Elements of hwnd's latest Window State. found is false
// when no stored Element has the focus, it has no Value pattern, or it is
// a password field. It sends nothing to the Window.
func (c *Client) FocusedValue(ctx context.Context, hwnd uint64) (value string, found bool, err error) {
	err = c.RunFor(ctx, hwnd, func(w *worker, _ *run) {
		elems, _, ok := c.store.elements(storeAgent(ctx), hwnd)
		if !ok {
			return
		}
		defer func() {
			for _, el := range elems {
				if el != nil {
					c.store.release(el)
				}
			}
		}()
		if w.thread == nil {
			return // fake workers carry no COM thread
		}
		for i, el := range elems {
			if i%pointBatch == 0 && ctx.Err() != nil {
				return
			}
			if el == nil || !currentBool(el, propHasKeyboardFocus) {
				continue
			}
			if !currentHasValuePattern(el) || currentBool(el, propIsPassword) {
				return
			}
			value, found = currentString(el, propValueValue), true
			return
		}
	})
	return value, found, err
}

// TypedVerdict is the read-back of typed text: whether the field shows it.
type TypedVerdict int

const (
	TypedUnverified TypedVerdict = iota // no readable focused field
	TypedShown                          // the field's value holds the text
	TypedMissing                        // the field was read and does not hold the text
)

// typedReadBack is how long the read-back waits for the page to apply the
// text, and how often it reads.
const (
	typedReadBack = time.Second
	typedInterval = 100 * time.Millisecond
)

// VerifyTyped polls read until the value holds text, for up to a second
// (pure but for read and sleep). A first read that finds no field is
// final: TypedUnverified. A value that never shows the text is
// TypedMissing.
func VerifyTyped(text string, read func() (string, bool, error), sleep func(time.Duration)) TypedVerdict {
	want := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for waited := time.Duration(0); ; waited += typedInterval {
		value, found, err := read()
		if err != nil || !found {
			return TypedUnverified
		}
		got := strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
		if strings.Contains(got, want) {
			return TypedShown
		}
		if waited >= typedReadBack {
			return TypedMissing
		}
		sleep(typedInterval)
	}
}
