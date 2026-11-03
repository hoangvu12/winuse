package win

import (
	"errors"
	"os"
	"testing"
)

// The Node debug signal (headless-max 15) is refused inside an offline
// test binary, and a process that is no Node process publishes no handler.
func TestNodeDebugOffline(t *testing.T) {
	if NodeDebugHandler(uint32(os.Getpid())) {
		t.Error("the test binary publishes a Node debug handler")
	}
	if err := SignalNodeDebug(uint32(os.Getpid())); !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("SignalNodeDebug in a test binary: %v, want the live desktop refusal", err)
	}
}
