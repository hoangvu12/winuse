package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/pipe"
	"github.com/hoangvu12/winuse/internal/win"
)

// Found live (every e2e test failed with "winuse helper is not running"):
// the real dialer must chain cli.ErrNoHelper, or the client never spawns a
// helper.
func TestDialHelperNoPipeMatchesErrNoHelper(t *testing.T) {
	id := win.Identity{SID: "S-1-5-21-0-0-0-winuse-test-nopipe", Session: 0}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := dialHelper(ctx, id)
	if err == nil {
		conn.Close()
		t.Fatal("dial of a non-existent pipe succeeded")
	}
	if !errors.Is(err, ErrNoHelper) {
		t.Errorf("errors.Is(err, cli.ErrNoHelper) = false; err = %v", err)
	}
	if !errors.Is(err, pipe.ErrNoHelper) {
		t.Errorf("errors.Is(err, pipe.ErrNoHelper) = false; err = %v", err)
	}
	if got, want := err.Error(), "winuse helper is not running: "+pipe.Name(id); got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}
