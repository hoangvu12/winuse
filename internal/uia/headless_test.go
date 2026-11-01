package uia

// The Headless Actions' offline tests (ADR 0010): the guards and messages
// that run before any COM call. The pattern calls and read-backs run on
// real Windows only; the opt-in e2e suite covers them.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPerformSecondaryActionHeadlessRefusesRaise(t *testing.T) {
	// Raise calls SetFocus, which moves the desktop foreground: the
	// Headless path refuses it before touching the Element (a zero Client
	// proves nothing was looked up).
	err := (&Client{}).PerformSecondaryActionHeadless(context.Background(), 101, 0, "Raise")
	if !errors.Is(err, ErrRaiseHeadless) {
		t.Fatalf("error = %v, want ErrRaiseHeadless", err)
	}
}

func TestVerifyErrorWithheldObservedNamesNoValue(t *testing.T) {
	// A password read-back withholds both sides; the message still says
	// the Action was delivered.
	e := &VerifyError{Action: "set-value", Expected: "the value that was set"}
	if got := e.Error(); !strings.Contains(got, "was delivered") || strings.Contains(got, `"`) {
		t.Errorf("message = %q, want the delivered wording without a quoted value", got)
	}
}
