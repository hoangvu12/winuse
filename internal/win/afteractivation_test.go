package win

import (
	"errors"
	"testing"
)

// A failure after the activation pair keeps its own message, still
// matches its cause, and is marked ErrAfterActivation; none is no error.
func TestAfterActivationMarksTheFailure(t *testing.T) {
	cause := errors.New("SendMessageTimeout(0x100) failed for window 7: Invalid window handle.")
	err := afterActivation(cause)
	if err.Error() != cause.Error() {
		t.Errorf("message %q, want %q", err, cause)
	}
	if !errors.Is(err, ErrAfterActivation) || !errors.Is(err, cause) {
		t.Errorf("errors.Is: after activation %v, cause %v", errors.Is(err, ErrAfterActivation), errors.Is(err, cause))
	}
	if errors.Is(cause, ErrAfterActivation) {
		t.Error("an unmarked failure reads as after the activation pair")
	}
	if afterActivation(nil) != nil {
		t.Error("no failure became one")
	}
}
