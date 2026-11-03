package uia

// The Chromium route's read-back verdict (headless-max ticket 20).

import (
	"errors"
	"testing"
	"time"
)

func TestVerifyTyped(t *testing.T) {
	noSleep := func(time.Duration) {}
	reads := func(values ...string) func() (string, bool, error) {
		i := 0
		return func() (string, bool, error) {
			v := values[min(i, len(values)-1)]
			i++
			return v, true, nil
		}
	}
	if got := VerifyTyped("hello", reads("say hello"), noSleep); got != TypedShown {
		t.Errorf("shown at once = %d", got)
	}
	if got := VerifyTyped("hello", reads("", "", "hello"), noSleep); got != TypedShown {
		t.Errorf("shown after the page applied it = %d", got)
	}
	if got := VerifyTyped("a\r\nb", reads("a\nb"), noSleep); got != TypedShown {
		t.Errorf("newlines compare as one = %d", got)
	}
	if got := VerifyTyped("hello", reads("something else"), noSleep); got != TypedMissing {
		t.Errorf("never shown = %d, want TypedMissing", got)
	}
	none := func() (string, bool, error) { return "", false, nil }
	if got := VerifyTyped("hello", none, noSleep); got != TypedUnverified {
		t.Errorf("no field = %d, want TypedUnverified", got)
	}
	failing := func() (string, bool, error) { return "", true, errors.New("gone") }
	if got := VerifyTyped("hello", failing, noSleep); got != TypedUnverified {
		t.Errorf("a failed read = %d, want TypedUnverified", got)
	}
}
