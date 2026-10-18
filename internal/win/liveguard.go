package win

import (
	"errors"
	"os"
	"testing"
)

// ErrLiveDesktopInTest is returned by every function that would touch the
// live desktop (input injection, cursor moves, activation, screen capture,
// showing the overlay) when it runs inside a `go test` binary without the
// user's end-to-end opt-in.
var ErrLiveDesktopInTest = errors.New("live desktop access refused inside a go test binary: offline tests must fake every input, activation, capture and overlay side (the opt-in e2e suite drives the real winuse binary instead)")

// LiveDesktopGuard refuses live desktop access from offline tests. The
// default test command must never move the user's mouse, inject keys,
// steal focus, show the overlay or capture the screen — a test whose fake
// falls through to the real implementation fails here instead. The
// e2e suite (WINUSE_E2E=1, started only by the user) is exempt; the real
// winuse binary is not a test binary and is never affected.
func LiveDesktopGuard() error {
	if testing.Testing() && os.Getenv("WINUSE_E2E") != "1" {
		return ErrLiveDesktopInTest
	}
	return nil
}
